package host

import (
	"context"
	"fmt"
	"io"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
	"github.com/ApexReasoning/carry/internal/cli/userapi"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/spf13/cobra"
)

func newRevokeCommand(configDirectory string, output io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Revoke this Machine using member authority",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runRevoke(command.Context(), configDirectory, output)
		},
	}
}

func runRevoke(ctx context.Context, configDirectory string, output io.Writer) error {
	machine, serverConfirmed, err := machinefile.LoadForRevocation(configDirectory)
	if err != nil {
		return err
	}
	if !serverConfirmed {
		member, err := credentialfile.Load(configDirectory)
		if err != nil {
			return err
		}
		connection, err := userapi.FromCredential(member)
		if err != nil {
			return err
		}
		if err := connection.RevokeMachine(ctx, machine.SpaceID, machine.MachineID); err != nil {
			return err
		}
		if err := machinefile.MarkRevoked(configDirectory); err != nil {
			return err
		}
	}
	if err := machinefile.RemoveRevoked(configDirectory); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Revoked Machine %s and removed its local credential\n", machine.MachineID)
	return nil
}
