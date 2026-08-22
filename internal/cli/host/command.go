package host

import (
	"io"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/spf13/cobra"
)

// NewCommand constructs the Machine-only Host paths after setup.
func NewCommand(
	configDirectory string,
	output io.Writer,
	errorOutput io.Writer,
	adapters hostdomain.AdapterSet,
) *cobra.Command {
	command := &cobra.Command{
		Use:   "host",
		Short: "Run or disconnect this Machine as a Carry Host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newStartCommand(configDirectory, output, errorOutput, adapters),
		newDisconnectCommand(configDirectory, output),
	)
	return command
}
