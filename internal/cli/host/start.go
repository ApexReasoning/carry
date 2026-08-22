package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/spf13/cobra"
)

func newStartCommand(
	configDirectory string,
	output io.Writer,
	errorOutput io.Writer,
	adapters hostdomain.AdapterSet,
) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run this Machine as a Carry Host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runStart(command.Context(), configDirectory, output, errorOutput, adapters)
		},
	}
}

func runStart(
	ctx context.Context,
	configDirectory string,
	output io.Writer,
	errorOutput io.Writer,
	adapters hostdomain.AdapterSet,
) error {
	credential, err := machinefile.Load(configDirectory)
	if err != nil {
		return fmt.Errorf("load Machine credential: %w", err)
	}
	return serveHost(ctx, credential, output, errorOutput, adapters)
}

func serveHost(
	ctx context.Context,
	credential machinefile.Credential,
	output io.Writer,
	errorOutput io.Writer,
	adapters hostdomain.AdapterSet,
) error {
	connection, err := connectMachine(credential)
	if err != nil {
		return fmt.Errorf("connect Machine: %w", err)
	}
	reporter, err := hostdomain.NewAgentReporter(adapters, connection, func(error) {
		_, _ = fmt.Fprintln(errorOutput, "Carry cannot confirm this Agent report yet; retrying.")
	})
	if err != nil {
		return fmt.Errorf("compose Agent reporter: %w", err)
	}
	snapshot, result, err := reporter.Report(ctx)
	if err != nil {
		return agentReportTerminalError(err)
	}
	writeAgentReportWarnings(errorOutput, result)

	executor, executionAvailable := snapshot.Executor(agent.AdapterKey("pi"), agent.OccurrenceKey("default"))
	if _, err := fmt.Fprintf(output, "Started Carry Host %s\n", credential.MachineID); err != nil {
		return fmt.Errorf("write Host start status: %w", err)
	}
	if !executionAvailable {
		_, _ = fmt.Fprintln(errorOutput, "Legacy Work and Conversation execution is unavailable until Pi is online; Agent presence reporting will continue.")
		if err := reporter.Serve(ctx, func(result machine.AgentReportResult) {
			writeAgentReportWarnings(errorOutput, result)
		}); err != nil {
			return agentReportTerminalError(err)
		}
		return nil
	}

	serveContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		results <- reporter.Serve(serveContext, func(result machine.AgentReportResult) {
			writeAgentReportWarnings(errorOutput, result)
		})
	}()
	go func() {
		defer wait.Done()
		results <- newMachineWorker(connection, executor).Serve(serveContext)
	}()
	first := <-results
	cancel()
	second := <-results
	wait.Wait()
	if first != nil {
		return fmt.Errorf("serve Carry Host: %w", first)
	}
	if second != nil {
		return fmt.Errorf("serve Carry Host: %w", second)
	}
	return nil
}

func writeAgentReportWarnings(output io.Writer, result machine.AgentReportResult) {
	for _, key := range result.UnsupportedAdapterKeys {
		_, _ = fmt.Fprintf(output, "Adapter %q is not supported by this carry-server. Update carry-server.\n", key)
	}
	for _, key := range result.SetupRequiredAdapterKeys {
		_, _ = fmt.Fprintf(output, "Carry cannot add %q on this Host because its approving member is no longer active. Revoke this Host and run carry setup again.\n", key)
	}
}

func agentReportTerminalError(err error) error {
	switch {
	case errors.Is(err, machine.ErrMachineRevoked):
		return errors.New("this Host is revoked; run carry setup to connect a new Host")
	case errors.Is(err, machine.ErrMachineUnavailable):
		return errors.New("this Host is unavailable; run carry setup again")
	default:
		return fmt.Errorf("report Host Agents: %w", err)
	}
}

func newMachineWorker(connection *machineHTTP, executor hostdomain.Executor) hostdomain.Worker {
	return hostdomain.Worker{
		Runs:          connection,
		Conversations: connection,
		Executor:      executor,
		PollInterval:  time.Second,
		RenewInterval: time.Minute,
	}
}
