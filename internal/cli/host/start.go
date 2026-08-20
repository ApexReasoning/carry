package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/spf13/cobra"
)

func newStartCommand(
	configDirectory string,
	output io.Writer,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run this Machine as a Carry Host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStart(command.Context(), configDirectory, output, piExecutor, codexExecutor)
		},
	}
}

func runStart(
	ctx context.Context,
	configDirectory string,
	output io.Writer,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) error {
	credential, err := machinefile.Load(configDirectory)
	if err != nil {
		return fmt.Errorf("load Machine credential: %w", err)
	}
	connection, err := connectMachine(credential)
	if err != nil {
		return fmt.Errorf("connect Machine: %w", err)
	}
	executor, label, err := selectExecutor(ctx, piExecutor, codexExecutor)
	if err != nil {
		return fmt.Errorf("select local executor: %w", err)
	}
	if _, err := fmt.Fprintf(output, "Started Carry Host %s with %s\n", credential.MachineID, label); err != nil {
		return fmt.Errorf("write Host start status: %w", err)
	}
	if err := newMachineWorker(connection, executor).Serve(ctx); err != nil {
		return fmt.Errorf("serve Carry Host: %w", err)
	}
	return nil
}

func newMachineWorker(connection *machineHTTP, executor hostdomain.Executor) hostdomain.Worker {
	return hostdomain.Worker{
		Runs: connection, Conversations: connection, Executor: executor,
		PollInterval: time.Second, RenewInterval: time.Minute,
	}
}

func selectExecutor(
	ctx context.Context,
	piExecutor hostdomain.Executor,
	codexExecutor hostdomain.Executor,
) (hostdomain.Executor, string, error) {
	if piExecutor == nil || codexExecutor == nil {
		return nil, "", errors.New("Pi and Codex executors are required")
	}
	piErr := piExecutor.Diagnose(ctx)
	if piErr == nil {
		return piExecutor, "Pi", nil
	}
	codexErr := codexExecutor.Diagnose(ctx)
	if codexErr == nil {
		return codexExecutor, "Codex", nil
	}
	return nil, "", errors.Join(
		fmt.Errorf("diagnose Pi: %w", piErr),
		fmt.Errorf("diagnose Codex: %w", codexErr),
	)
}
