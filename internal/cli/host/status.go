package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/machinefile"
	"github.com/spf13/cobra"
)

func newStatusCommand(configDirectory string, output io.Writer, detectRuntimes runtimeDetector) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show this Machine and its last reported Runtime status",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStatus(command.Context(), configDirectory, output, detectRuntimes)
		},
	}
}

func runStatus(ctx context.Context, configDirectory string, output io.Writer, detectRuntimes runtimeDetector) error {
	credential, err := machinefile.Load(configDirectory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			printLocalStatus(output, detectRuntimes(ctx))
			return nil
		}
		return err
	}
	connection, err := connectMachine(credential)
	if err != nil {
		return err
	}
	status, err := connection.loadStatus(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Machine: %s (%s)\n", status.MachineID, status.DisplayName)
	for _, observation := range status.Runtimes {
		writeRuntimeStatus(output, observation)
	}
	return nil
}

func printLocalStatus(output io.Writer, observations []hostdomain.RuntimeObservation) {
	_, _ = fmt.Fprintln(output, "Machine: not enrolled")
	for _, observation := range observations {
		writeRuntimeStatus(output, observation)
	}
}

func writeRuntimeStatus(output io.Writer, observation hostdomain.RuntimeObservation) {
	switch observation.Detection {
	case hostdomain.RuntimeDetected:
		_, _ = fmt.Fprintf(output, "Runtime %s: detected (%s)\n", observation.Kind, observation.Version)
	case hostdomain.RuntimeNotFound:
		_, _ = fmt.Fprintf(output, "Runtime %s: not found\n", observation.Kind)
	case hostdomain.RuntimeProbeFailed:
		_, _ = fmt.Fprintf(output, "Runtime %s: probe failed (%s)\n", observation.Kind, observation.DiagnosticCode)
	}
}
