package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ApexReasoning/carry/internal/host/machinefile"
	"github.com/spf13/cobra"
)

type startFlags struct {
	once     bool
	interval time.Duration
}

func newStartCommand(configDirectory string, output io.Writer, detectRuntimes runtimeDetector) *cobra.Command {
	flags := startFlags{interval: 30 * time.Second}
	command := &cobra.Command{
		Use:   "start",
		Short: "Report local Runtime status as this Machine",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStart(command.Context(), configDirectory, output, detectRuntimes, flags)
		},
	}
	command.Flags().BoolVar(&flags.once, "once", false, "report once and exit")
	command.Flags().DurationVar(&flags.interval, "interval", flags.interval, "Runtime report interval")
	return command
}

func runStart(
	ctx context.Context,
	configDirectory string,
	output io.Writer,
	detectRuntimes runtimeDetector,
	flags startFlags,
) error {
	if flags.interval <= 0 {
		return errors.New("--interval must be greater than zero")
	}
	credential, err := machinefile.Load(configDirectory)
	if err != nil {
		return err
	}
	connection, err := connectMachine(credential)
	if err != nil {
		return err
	}
	// This path intentionally has no member credential dependency. A Host must
	// continue reporting after the member who enrolled it logs out locally.
	report := func() error {
		if err := connection.reportRuntimes(ctx, detectRuntimes(ctx)); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(output, "Reported Runtime status for Machine %s\n", credential.MachineID)
		return nil
	}
	if err := report(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if flags.once {
		return nil
	}
	ticker := time.NewTicker(flags.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := report(); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}
