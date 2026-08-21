package host

import (
	"io"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/spf13/cobra"
)

// NewCommand constructs the Browser connection and Machine-only Host paths.
func NewCommand(
	configDirectory string,
	output io.Writer,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) *cobra.Command {
	command := &cobra.Command{
		Use:   "host",
		Short: "Connect and run this Machine as a Carry Host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newConnectCommand(configDirectory, output),
		newStartCommand(configDirectory, output, piExecutor, codexExecutor),
		newDisconnectCommand(configDirectory, output),
	)
	return command
}
