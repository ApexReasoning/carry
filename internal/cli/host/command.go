package host

import (
	"context"
	"io"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/spf13/cobra"
)

type runtimeDetector func(context.Context) []hostdomain.RuntimeObservation

// NewCommand constructs the Host subtree with separate member and Machine paths.
func NewCommand(configDirectory string, output io.Writer, detectRuntimes runtimeDetector) *cobra.Command {
	command := &cobra.Command{
		Use:   "host",
		Short: "Enroll and run this Machine as a Carry Host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newEnrollCommand(configDirectory, output),
		newStartCommand(configDirectory, output, detectRuntimes),
		newStatusCommand(configDirectory, output, detectRuntimes),
		newRevokeCommand(configDirectory, output),
	)
	return command
}
