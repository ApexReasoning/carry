package host

import (
	"context"
	"fmt"
	"io"

	"github.com/ApexReasoning/carry/internal/host/machinefile"
	"github.com/ApexReasoning/carry/internal/identity/memberfile"
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
	member, err := memberfile.Load(configDirectory)
	if err != nil {
		return err
	}
	machine, err := machinefile.Load(configDirectory)
	if err != nil {
		return err
	}
	connection, err := connectMember(member)
	if err != nil {
		return err
	}
	if err := connection.revokeMachine(ctx, machine.SpaceID, machine.MachineID); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Revoked Machine %s\n", machine.MachineID)
	return nil
}
