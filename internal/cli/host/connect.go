package host

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type connectFlags struct {
	serverURL, displayName, caCertificatePath string
}

func newConnectCommand(configDirectory string, output io.Writer) *cobra.Command {
	var flags connectFlags
	command := &cobra.Command{
		Use: "connect", Short: "Connect this Machine through Browser approval", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runConnect(command.Context(), configDirectory, output, flags)
		},
	}
	command.Flags().StringVar(&flags.serverURL, "server", "", "exact Carry HTTPS server origin")
	command.Flags().StringVar(&flags.displayName, "name", "", "Machine name shown for Browser approval")
	command.Flags().StringVar(&flags.caCertificatePath, "ca-cert", "", "optional private Carry CA certificate")
	return command
}

func runConnect(ctx context.Context, configDirectory string, output io.Writer, flags connectFlags) error {
	if strings.TrimSpace(flags.serverURL) == "" {
		return errors.New("--server is required")
	}
	serverURL, err := parseServerURL(flags.serverURL)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(flags.displayName)
	if name == "" {
		name, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("read Machine hostname: %w", err)
		}
	}
	if name == "" || len([]byte(name)) > machine.DisplayNameMaximumBytes {
		return errors.New("--name must contain at most 128 bytes")
	}
	var caCertificatePEM []byte
	if flags.caCertificatePath != "" {
		caCertificatePEM, err = os.ReadFile(flags.caCertificatePath)
		if err != nil {
			return fmt.Errorf("read CA certificate: %w", err)
		}
	}
	if _, err := machinefile.Load(configDirectory); err == nil {
		return machinefile.ErrAlreadyEnrolled
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	client, err := newConnectionClient(serverURL, string(caCertificatePEM))
	if err != nil {
		return err
	}
	pending, err := loadOrCreatePendingConnection(configDirectory, serverURL, string(caCertificatePEM), name)
	if err != nil {
		return err
	}
	begun, err := client.begin(ctx, pending)
	if err != nil {
		return fmt.Errorf("begin Machine connection (pending proof retained for exact retry): %w", err)
	}
	pending.ExpiresAt, pending.IntervalSeconds = begun.ExpiresAt, begun.IntervalSeconds
	if err := machinefile.SavePending(configDirectory, pending); err != nil {
		return fmt.Errorf("save server-confirmed Machine connection: %w", err)
	}
	_, _ = fmt.Fprintf(output,
		"Server: %s\nMachine name: %s\nPublic key: %s\nCode: %s\nOpen: %s%s\nExpires: %s\nWaiting for explicit Browser approval…\n",
		serverURL, pending.DisplayName, pending.Fingerprint, pending.UserCode, serverURL, begun.VerificationPath,
		begun.ExpiresAt.Local().Format(time.RFC1123),
	)
	interval := time.Duration(pending.IntervalSeconds) * time.Second
	for {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			cancelErr := client.cancel(cancelCtx, pending.PollSecret)
			cancel()
			if cancelErr != nil {
				_, _ = fmt.Fprintf(output, "Stopped waiting. Cancellation is unknown; the pending key and proof were retained until %s for exact retry.\n", pending.ExpiresAt.Local().Format(time.RFC1123))
			} else {
				if removeErr := machinefile.RemovePending(configDirectory); removeErr != nil {
					return fmt.Errorf("connection canceled but pending key cleanup failed: %w", removeErr)
				}
				_, _ = fmt.Fprintln(output, "Machine connection canceled. No Machine certificate was installed.")
			}
			return ctx.Err()
		case <-time.After(interval):
		}
		connected, pollErr := client.poll(ctx, pending.PollSecret)
		if pollErr == nil {
			if connected.DisplayName != pending.DisplayName {
				return errors.New("approved Machine returned a different display name")
			}
			if err := validateConnectedMachine(pending, connected, caCertificatePEM); err != nil {
				return err
			}
			if err := machinefile.Save(configDirectory, machinefile.Credential{
				MachineID: connected.MachineID, SpaceID: connected.SpaceID, ServerURL: pending.ServerURL,
				CACertificatePEM: pending.CACertificatePEM, CertificatePEM: connected.CertificatePEM,
				PrivateKeyPEM: pending.PrivateKeyPEM,
			}); err != nil {
				return fmt.Errorf("save approved Machine credential (rerun carry host connect before %s to recover it): %w", connected.ReplayUntil.Local().Format(time.RFC1123), err)
			}
			if err := machinefile.RemovePending(configDirectory); err != nil {
				return fmt.Errorf("Machine credential was installed but pending proof cleanup failed: %w", err)
			}
			_, _ = fmt.Fprintf(output, "Machine %s connected to Space %s as %s. Run `carry host start` to begin serving Work.\n", connected.MachineID, connected.SpaceID, connected.DisplayName)
			return nil
		}
		var responseErr *connectionHTTPError
		if !errors.As(pollErr, &responseErr) {
			var transient *transientConnectionError
			if errors.As(pollErr, &transient) {
				interval = min(interval+5*time.Second, machine.ConnectionMaximumInterval)
				continue
			}
			return pollErr
		}
		switch {
		case responseErr.status == http.StatusAccepted:
			continue
		case responseErr.status == http.StatusTooManyRequests:
			if responseErr.retryAfter > interval {
				interval = responseErr.retryAfter
			} else {
				interval = min(interval+5*time.Second, machine.ConnectionMaximumInterval)
			}
			continue
		case responseErr.status >= 500:
			interval = min(interval+5*time.Second, machine.ConnectionMaximumInterval)
			continue
		case responseErr.status == http.StatusForbidden:
			if removeErr := machinefile.RemovePending(configDirectory); removeErr != nil {
				return fmt.Errorf("Machine connection was denied, but pending key cleanup failed; rerun carry host connect to finish cleanup: %w", removeErr)
			}
			return errors.New("Machine connection was denied in the Browser; the pending key was removed")
		case responseErr.status == http.StatusGone:
			if removeErr := machinefile.RemovePending(configDirectory); removeErr != nil {
				return fmt.Errorf("Machine connection expired, but pending key cleanup failed; rerun carry host connect to finish cleanup: %w", removeErr)
			}
			return errors.New("Machine connection or certificate retrieval expired; start a fresh connection")
		case responseErr.status == http.StatusConflict:
			if removeErr := machinefile.RemovePending(configDirectory); removeErr != nil {
				return fmt.Errorf("Machine connection ended without a certificate and pending key cleanup failed: %w", errors.Join(pollErr, removeErr))
			}
			return fmt.Errorf("Machine connection ended without a certificate: %w", pollErr)
		default:
			return pollErr
		}
	}
}

func loadOrCreatePendingConnection(configDirectory, serverURL, caCertificatePEM, displayName string) (machinefile.PendingConnection, error) {
	pending, err := machinefile.LoadPending(configDirectory)
	if err == nil {
		if pending.ServerURL != serverURL || pending.CACertificatePEM != caCertificatePEM || pending.DisplayName != displayName {
			return machinefile.PendingConnection{}, errors.New("connect flags do not match the pending Machine connection")
		}
		return pending, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return machinefile.PendingConnection{}, err
	}
	requestID, key := uuid.NewString(), uuid.NewString()
	publicKeyDER, privateKeyPEM, err := machinefile.GenerateKey()
	if err != nil {
		return machinefile.PendingConnection{}, err
	}
	pollSecret, err := machinefile.NewPollSecret(requestID)
	if err != nil {
		return machinefile.PendingConnection{}, err
	}
	userCode, err := machinefile.NewUserCode()
	if err != nil {
		return machinefile.PendingConnection{}, err
	}
	proof, err := machinefile.SignConnectionProof(string(privateKeyPEM), serverURL, requestID, displayName, publicKeyDER, userCode, pollSecret)
	if err != nil {
		return machinefile.PendingConnection{}, err
	}
	pending = machinefile.PendingConnection{
		ServerURL: serverURL, CACertificatePEM: caCertificatePEM, RequestID: requestID, IdempotencyKey: key,
		DisplayName: displayName, UserCode: userCode, PollSecret: pollSecret,
		PublicKeyDER: publicKeyDER, PrivateKeyPEM: string(privateKeyPEM), KeyProof: proof,
		Fingerprint: machine.PublicKeyFingerprint(publicKeyDER), ExpiresAt: time.Now().Add(machine.ConnectionLifetime),
		IntervalSeconds: int(machine.ConnectionInitialInterval / time.Second),
	}
	if err := machinefile.SavePending(configDirectory, pending); err != nil {
		return machinefile.PendingConnection{}, err
	}
	return pending, nil
}

func validateConnectedMachine(pending machinefile.PendingConnection, connected connectedMachine, caCertificatePEM []byte) error {
	pair, err := tls.X509KeyPair([]byte(connected.CertificatePEM), []byte(pending.PrivateKeyPEM))
	if err != nil || pair.Leaf == nil {
		return errors.New("approved Machine certificate does not match the pending private key")
	}
	machineID, err := machine.MachineIDFromCertificate(pair.Leaf)
	if err != nil || machineID != connected.MachineID {
		return errors.New("approved Machine certificate identity is invalid")
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(pair.Leaf.PublicKey)
	if err != nil || !bytes.Equal(publicKeyDER, pending.PublicKeyDER) {
		return errors.New("approved Machine certificate public key differs from Browser approval")
	}
	if len(caCertificatePEM) != 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caCertificatePEM) {
			return errors.New("CA certificate is invalid")
		}
		if _, err := pair.Leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			return fmt.Errorf("verify approved Machine certificate: %w", err)
		}
	}
	return nil
}
