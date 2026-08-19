package host

import (
	"io"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/spf13/cobra"
)

// NewCommand constructs the Host subtree with separate member and Machine paths.
func NewCommand(
	configDirectory string,
	output io.Writer,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) *cobra.Command {
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
		newStartCommand(configDirectory, output, piExecutor, codexExecutor),
		newRevokeCommand(configDirectory, output),
	)
	return command
}
