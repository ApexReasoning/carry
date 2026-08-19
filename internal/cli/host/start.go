package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/machinefile"
	"github.com/spf13/cobra"
)

type startFlags struct {
	once     bool
	interval time.Duration
}

func newStartCommand(
	configDirectory string,
	output io.Writer,
	detectRuntimes runtimeDetector,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) *cobra.Command {
	flags := startFlags{interval: 30 * time.Second}
	command := &cobra.Command{
		Use:   "start",
		Short: "Run this Machine as a Carry Host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStart(
				command.Context(),
				configDirectory,
				output,
				detectRuntimes,
				piExecutor,
				codexExecutor,
				flags,
			)
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
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
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
	observations := detectRuntimes(ctx)
	// This path intentionally has no member credential dependency. A Host must
	// continue reporting after the member who enrolled it logs out locally.
	report := func(observed []hostdomain.RuntimeObservation) error {
		if err := connection.reportRuntimes(ctx, observed); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(output, "Reported Runtime status for Machine %s\n", credential.MachineID)
		return nil
	}
	if err := report(observations); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if flags.once {
		return nil
	}

	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	workerResult := make(chan error, 2)
	workerCount := 0
	startWorker := func(executor hostdomain.Executor, label string) {
		workerCount++
		go func() {
			err := (hostdomain.Worker{
				Client: connection, Executor: executor,
				PollInterval: time.Second, RenewInterval: time.Minute,
			}).Serve(workerCtx)
			if err != nil {
				err = fmt.Errorf("%s worker: %w", label, err)
			}
			workerResult <- err
		}()
	}
	if runtimeDetected(observations, hostdomain.RuntimePi) {
		startWorker(piExecutor, "Pi")
	}
	if runtimeDetected(observations, hostdomain.RuntimeCodex) {
		startWorker(codexExecutor, "Codex")
	}

	ticker := time.NewTicker(flags.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			stopWorkers()
			waitForWorkers(workerResult, workerCount)
			return nil
		case err := <-workerResult:
			stopWorkers()
			waitForWorkers(workerResult, workerCount-1)
			return err
		case <-ticker.C:
			if err := report(detectRuntimes(ctx)); err != nil {
				stopWorkers()
				waitForWorkers(workerResult, workerCount)
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func runtimeDetected(observations []hostdomain.RuntimeObservation, kind hostdomain.RuntimeKind) bool {
	for _, observation := range observations {
		if observation.Kind == kind {
			return observation.Detection == hostdomain.RuntimeDetected
		}
	}
	return false
}

func waitForWorkers(results <-chan error, count int) {
	for range count {
		<-results
	}
}
