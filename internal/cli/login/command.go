package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
	"github.com/ApexReasoning/carry/internal/cli/userapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type loginFlags struct {
	serverURL, label, caCertificatePath string
}

// NewCommand constructs the Browser-approved member login without process-global credentials.
func NewCommand(configDirectory string, output io.Writer) *cobra.Command {
	var flags loginFlags
	command := &cobra.Command{
		Use: "login", Short: "Log in through an approved Carry Browser Session", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return run(command.Context(), configDirectory, output, flags)
		},
	}
	command.Flags().StringVar(&flags.serverURL, "server", "", "exact Carry HTTPS server origin")
	command.Flags().StringVar(&flags.label, "name", "", "label shown for this CLI login")
	command.Flags().StringVar(&flags.caCertificatePath, "ca-cert", "", "optional private Carry CA certificate")
	return command
}

func run(ctx context.Context, configDirectory string, output io.Writer, flags loginFlags) error {
	if strings.TrimSpace(flags.serverURL) == "" {
		return errors.New("--server is required")
	}
	serverURL, err := userapi.ParseServerURL(flags.serverURL)
	if err != nil {
		return err
	}
	label := strings.TrimSpace(flags.label)
	if label == "" {
		label, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("read CLI hostname: %w", err)
		}
	}
	if label == "" || len([]byte(label)) > 128 {
		return errors.New("--name must contain at most 128 bytes")
	}
	var caCertificatePEM []byte
	if flags.caCertificatePath != "" {
		caCertificatePEM, err = os.ReadFile(flags.caCertificatePath)
		if err != nil {
			return fmt.Errorf("read CA certificate: %w", err)
		}
	}
	client, err := newLoginClient(serverURL, string(caCertificatePEM))
	if err != nil {
		return err
	}

	var begun begunLogin
	pending, pendingErr := credentialfile.LoadPending(configDirectory)
	if pendingErr == nil && !pending.ExpiresAt.After(time.Now()) {
		if err := credentialfile.RemovePending(configDirectory); err != nil {
			return err
		}
		pendingErr = os.ErrNotExist
	}
	if pendingErr == nil {
		if pending.ServerURL != serverURL.String() || pending.CACertificatePEM != string(caCertificatePEM) {
			return errors.New("a pending CLI login belongs to a different Carry server or CA")
		}
		begun = begunLogin{
			RequestID: pending.RequestID, UserCode: pending.UserCode, PollSecret: pending.PollSecret,
			VerificationPath: pending.VerificationPath, ExpiresAt: pending.ExpiresAt,
			IntervalSeconds: pending.IntervalSeconds,
		}
		label = pending.Label
	} else if !errors.Is(pendingErr, os.ErrNotExist) {
		return pendingErr
	} else {
		replacementID := ""
		if existing, loadErr := credentialfile.Load(configDirectory); loadErr == nil {
			if existing.ServerURL != serverURL.String() {
				return errors.New("a different Carry server is active; run carry logout before changing servers")
			}
			if existing.ExpiresAt.After(time.Now()) {
				replacementID = existing.CredentialID
			}
		} else if !errors.Is(loadErr, os.ErrNotExist) {
			return loadErr
		}

		requestID, key := uuid.NewString(), uuid.NewString()
		begun, err = client.begin(ctx, requestID, key, label, replacementID)
		if err != nil {
			return err
		}
		if err := credentialfile.SavePending(configDirectory, credentialfile.PendingLogin{
			ServerURL: serverURL.String(), CACertificatePEM: string(caCertificatePEM), RequestID: begun.RequestID,
			UserCode: begun.UserCode, PollSecret: begun.PollSecret, VerificationPath: begun.VerificationPath,
			Label: label, ProposedReplacementCredentialID: replacementID, ExpiresAt: begun.ExpiresAt,
			IntervalSeconds: begun.IntervalSeconds,
		}); err != nil {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = client.cancel(cancelCtx, begun.PollSecret)
			cancel()
			return fmt.Errorf("save pending CLI login before display: %w", err)
		}
	}
	_, _ = fmt.Fprintf(output,
		"Server: %s\nCLI label: %s\nSpace: choose and confirm in your Browser\nCode: %s\nOpen: %s%s\nExpires: %s\nWaiting for explicit approval…\n",
		serverURL.String(), label, begun.UserCode, serverURL.String(), begun.VerificationPath,
		begun.ExpiresAt.Local().Format(time.RFC1123),
	)

	interval := time.Duration(begun.IntervalSeconds) * time.Second
	for {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			cancelErr := client.cancel(cancelCtx, begun.PollSecret)
			cancel()
			if cancelErr != nil {
				_, _ = fmt.Fprintf(output, "Stopped waiting. Cancellation is unknown; the request may remain available until %s. No credential was saved.\n", begun.ExpiresAt.Local().Format(time.RFC1123))
			} else {
				if removeErr := credentialfile.RemovePending(configDirectory); removeErr != nil {
					return fmt.Errorf("login canceled but pending proof cleanup failed: %w", removeErr)
				}
				_, _ = fmt.Fprintln(output, "Login canceled. No credential was issued.")
			}
			return ctx.Err()
		case <-time.After(interval):
		}
		result, pollErr := client.poll(ctx, begun.PollSecret)
		if pollErr == nil {
			connection, connectErr := userapi.New(serverURL.String(), string(caCertificatePEM), result.Credential)
			if connectErr != nil {
				return connectErr
			}
			member, loadErr := connection.LoadMember(ctx)
			if loadErr != nil {
				return fmt.Errorf("validate approved CLI credential: %w", loadErr)
			}
			if member.UserID != result.UserID {
				return errors.New("approved CLI credential returned a different User")
			}
			spaceCurrent := false
			for _, membership := range member.Spaces {
				if membership.SpaceID == result.SpaceID {
					spaceCurrent = true
					break
				}
			}
			if !spaceCurrent {
				return errors.New("approved default Space is no longer available")
			}
			if err := credentialfile.Save(configDirectory, credentialfile.Credential{
				ServerURL: serverURL.String(), CACertificatePEM: string(caCertificatePEM), Credential: result.Credential,
				CredentialID: result.CredentialID, UserID: result.UserID, DefaultSpaceID: result.SpaceID,
				Label: result.Label, ExpiresAt: result.ExpiresAt,
			}); err != nil {
				return fmt.Errorf("save approved CLI credential (rerun carry login within five minutes to recover the same credential): %w", err)
			}
			if err := credentialfile.RemovePending(configDirectory); err != nil {
				return fmt.Errorf("CLI credential was saved but pending proof cleanup failed: %w", err)
			}
			_, _ = fmt.Fprintf(output, "Logged in to %s as %s. Default Space: %s. Credential expires %s.\n",
				serverURL.String(), member.DisplayName, result.SpaceID, result.ExpiresAt.Local().Format(time.RFC1123))
			return nil
		}
		var responseErr *loginHTTPError
		if !errors.As(pollErr, &responseErr) {
			interval = min(interval+5*time.Second, 30*time.Second)
			continue
		}
		switch {
		case responseErr.status == http.StatusAccepted:
			continue
		case responseErr.status == http.StatusTooManyRequests:
			if responseErr.retryAfter > interval {
				interval = responseErr.retryAfter
			} else {
				interval = min(interval+5*time.Second, 30*time.Second)
			}
			continue
		case responseErr.status >= 500:
			interval = min(interval+5*time.Second, 30*time.Second)
			continue
		case responseErr.status == http.StatusForbidden:
			_ = credentialfile.RemovePending(configDirectory)
			return errors.New("login denied in the Browser; no credential was saved")
		case responseErr.status == http.StatusGone:
			_ = credentialfile.RemovePending(configDirectory)
			return errors.New("CLI login request expired; run carry login to start a new one")
		case responseErr.status == http.StatusConflict:
			_ = credentialfile.RemovePending(configDirectory)
			return fmt.Errorf("CLI login ended without a credential: %w", pollErr)
		default:
			return pollErr
		}
	}
}
