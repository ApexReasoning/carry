package cli

import (
	"context"
	"fmt"
	"io"

	hostcmd "github.com/ApexReasoning/carry/internal/cli/host"
	"github.com/ApexReasoning/carry/internal/cli/login"
	workcmd "github.com/ApexReasoning/carry/internal/cli/work"
	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/spf13/cobra"
)

// Streams keeps terminal I/O explicit and replaceable for each CLI invocation.
type Streams struct {
	Input       io.Reader
	Output      io.Writer
	ErrorOutput io.Writer
}

// Run builds a fresh command tree with explicitly supplied native executors and executes one Carry CLI invocation.
func Run(
	ctx context.Context,
	arguments []string,
	version string,
	configDirectory string,
	streams Streams,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) int {
	root := newRoot(version, configDirectory, streams, piExecutor, codexExecutor)
	root.SetArgs(arguments)
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(streams.ErrorOutput, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRoot(
	version string,
	configDirectory string,
	streams Streams,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) *cobra.Command {
	root := &cobra.Command{
		Use:           "carry",
		Short:         "Carry keeps team Work moving",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetIn(streams.Input)
	root.SetOut(streams.Output)
	root.SetErr(streams.ErrorOutput)
	root.AddCommand(
		login.NewCommand(configDirectory, streams.Output),
		login.NewLogoutCommand(configDirectory, streams.Output),
		workcmd.NewCommand(configDirectory, streams.Output),
		hostcmd.NewCommand(configDirectory, streams.Output, piExecutor, codexExecutor),
	)
	return root
}
