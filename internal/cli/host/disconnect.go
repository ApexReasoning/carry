package host

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newDisconnectCommand(configDirectory string, output io.Writer) *cobra.Command {
	var localOnly bool
	command := &cobra.Command{
		Use: "disconnect", Short: "Revoke this Machine and remove its local credential", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runDisconnect(command.Context(), configDirectory, output, localOnly)
		},
	}
	command.Flags().BoolVar(&localOnly, "local-only", false, "erase local Machine material without claiming remote revocation")
	return command
}

func runDisconnect(ctx context.Context, configDirectory string, output io.Writer, localOnly bool) error {
	if localOnly {
		if err := machinefile.RemoveLocalOnly(configDirectory); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(output, "Local Machine credentials were erased. Carry could not confirm remote revocation; this Machine may still appear Active. Revoke it in Web when service access returns.")
		return nil
	}
	credential, serverConfirmed, err := machinefile.LoadForDisconnection(configDirectory)
	if err != nil {
		return err
	}
	if !serverConfirmed {
		if credential.DisconnectIdempotencyKey == "" {
			credential.DisconnectIdempotencyKey = uuid.NewString()
			if err := machinefile.Save(configDirectory, credential); err != nil {
				return fmt.Errorf("save Machine disconnect identity: %w", err)
			}
		}
		if err := revokeCurrentMachine(ctx, credential); err != nil {
			return fmt.Errorf("remote Machine revocation is unknown; local credential retained for exact retry: %w", err)
		}
		if err := machinefile.MarkRevoked(configDirectory); err != nil {
			return err
		}
	}
	if err := machinefile.RemoveRevoked(configDirectory); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Carry confirmed Machine %s is Revoked. Its local credential was removed. This does not prove a process stopped or copied files were deleted.\n", credential.MachineID)
	return nil
}

func revokeCurrentMachine(ctx context.Context, credential machinefile.Credential) error {
	pair, err := tls.X509KeyPair([]byte(credential.CertificatePEM), []byte(credential.PrivateKeyPEM))
	if err != nil {
		return errors.New("Machine credential is invalid")
	}
	client, err := newTLSClient(credential.CACertificatePEM, &pair)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(credential.HostAPIOrigin, "/")+"/v1/host/machine/revoke", nil)
	if err != nil {
		return fmt.Errorf("build Machine disconnect request: %w", err)
	}
	request.Header.Set("Idempotency-Key", credential.DisconnectIdempotencyKey)
	response, err := client.Do(request)
	if err != nil {
		return controlPlaneRequestError("send Machine disconnect request", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
		if failure.Error != "" {
			return fmt.Errorf("Machine disconnect failed (%d): %s", response.StatusCode, failure.Error)
		}
		return fmt.Errorf("Machine disconnect failed (%d)", response.StatusCode)
	}
	return nil
}
