package login

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
	"github.com/ApexReasoning/carry/internal/cli/userapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func NewLogoutCommand(configDirectory string, output io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "logout", Short: "Revoke the current CLI credential", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			credential, err := credentialfile.Load(configDirectory)
			if err != nil {
				return err
			}
			origin, err := userapi.ParseServerURL(credential.ServerURL)
			if err != nil {
				return err
			}
			client, err := newLoginClient(origin, credential.CACertificatePEM)
			if err != nil {
				return err
			}
			if credential.LogoutIdempotencyKey == "" {
				credential.LogoutIdempotencyKey = uuid.NewString()
				if err := credentialfile.Save(configDirectory, credential); err != nil {
					return fmt.Errorf("save logout retry identity: %w", err)
				}
			}
			err = client.revoke(command.Context(), credential.Credential, credential.LogoutIdempotencyKey)
			var responseErr *loginHTTPError
			if err != nil && !(errors.As(err, &responseErr) && responseErr.status == http.StatusUnauthorized) {
				return fmt.Errorf("could not confirm CLI credential revocation; local credential retained for exact retry: %w", err)
			}
			if err := credentialfile.Remove(configDirectory); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(output, "Revoked CLI access to %s and removed the local credential.\n", credential.ServerURL)
			return nil
		},
	}
	return command
}
