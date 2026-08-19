package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ApexReasoning/carry/internal/identity/memberfile"
	"github.com/spf13/cobra"
)

type loginFlags struct {
	serverURL         string
	token             string
	caCertificatePath string
}

// NewCommand constructs the member login command without retaining process-global state.
func NewCommand(configDirectory string, output io.Writer) *cobra.Command {
	flags := loginFlags{token: os.Getenv("CARRY_USER_TOKEN")}
	command := &cobra.Command{
		Use:   "login",
		Short: "Log in as a Carry member",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return run(command.Context(), configDirectory, output, flags)
		},
	}
	command.Flags().StringVar(&flags.serverURL, "server", "", "Carry server URL")
	command.Flags().StringVar(&flags.token, "token", flags.token, "member token")
	command.Flags().StringVar(&flags.caCertificatePath, "ca-cert", "", "Carry CA certificate")
	return command
}

func run(ctx context.Context, configDirectory string, output io.Writer, flags loginFlags) error {
	if flags.serverURL == "" || flags.token == "" || flags.caCertificatePath == "" {
		return errors.New("--server, --ca-cert, and --token are required")
	}
	serverURL, err := parseServerURL(flags.serverURL)
	if err != nil {
		return err
	}
	caCertificatePEM, err := os.ReadFile(flags.caCertificatePath)
	if err != nil {
		return fmt.Errorf("read CA certificate: %w", err)
	}
	connection, err := newMemberHTTP(serverURL, string(caCertificatePEM), flags.token)
	if err != nil {
		return err
	}
	info, err := connection.loadInfo(ctx)
	if err != nil {
		return err
	}
	if err := memberfile.Save(configDirectory, memberfile.Credential{
		ServerURL: serverURL, Token: flags.token, CACertificatePEM: string(caCertificatePEM),
		UserID: info.UserID, Spaces: info.Spaces,
	}); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Logged in as %s with %d Space(s)\n", info.UserID, len(info.Spaces))
	return nil
}
