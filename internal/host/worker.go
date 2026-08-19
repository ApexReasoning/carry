package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/run"
)

// RunClient is the Host-side consumer contract for one Machine's generic Run authority.
type RunClient interface {
	Claim(context.Context) (run.Claim, error)
	Renew(context.Context, run.Claim) (time.Time, error)
	LoadContext(context.Context, run.Claim) (run.Context, error)
	Commit(context.Context, run.Claim, Draft) error
	Finish(context.Context, run.Claim, run.State) error
}

// Worker runs the same claim loop for one concrete Executor.
type Worker struct {
	Client        RunClient
	Executor      Executor
	PollInterval  time.Duration
	RenewInterval time.Duration
}

// Serve claims generic Runs until cancellation; the caller owns this goroutine.
func (worker Worker) Serve(ctx context.Context) error {
	if worker.Client == nil || worker.Executor == nil {
		return errors.New("Host worker dependencies are required")
	}
	if worker.PollInterval <= 0 || worker.RenewInterval <= 0 {
		return errors.New("Host worker intervals must be positive")
	}
	if err := worker.Executor.Diagnose(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(worker.PollInterval)
	defer ticker.Stop()
	for {
		claim, err := worker.Client.Claim(ctx)
		if errors.Is(err, run.ErrNoPendingRun) {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("claim coordinator Run: %w", err)
		}
		if err := worker.executeClaim(ctx, claim); err != nil {
			return err
		}
	}
}

func (worker Worker) executeClaim(ctx context.Context, claim run.Claim) error {
	attemptContext, err := worker.Client.LoadContext(ctx, claim)
	if err != nil {
		return fmt.Errorf("load claimed Attempt context: %w", err)
	}
	request := ExecutionRequest{
		Goal: attemptContext.Goal, CurrentUnderstanding: attemptContext.CurrentUnderstanding,
		CurrentNextStep: attemptContext.CurrentNextStep, Inputs: attemptContext.Inputs,
	}
	executionCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	type executionResult struct {
		draft Draft
		err   error
	}
	completed := make(chan executionResult, 1)
	go func() {
		draft, executeErr := worker.Executor.Execute(executionCtx, request)
		completed <- executionResult{draft: draft, err: executeErr}
	}()
	renewal := time.NewTicker(worker.RenewInterval)
	defer renewal.Stop()
	for {
		select {
		case result := <-completed:
			if result.err != nil {
				return worker.finishExecution(ctx, claim, result.err)
			}
			if err := worker.Client.Commit(ctx, claim, result.draft); err != nil {
				return fmt.Errorf("commit Agent current understanding: %w", err)
			}
			return nil
		case <-renewal.C:
			if _, err := worker.Client.Renew(ctx, claim); err != nil {
				cancelExecution()
				<-completed
				return worker.finishExecution(
					ctx,
					claim,
					errors.Join(ErrAgentOutcomeLost, fmt.Errorf("renew active Attempt: %w", err)),
				)
			}
		case <-ctx.Done():
			cancelExecution()
			<-completed
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelCleanup()
			return worker.finishExecution(cleanupCtx, claim, ErrAgentOutcomeLost)
		}
	}
}

func (worker Worker) finishExecution(ctx context.Context, claim run.Claim, executionErr error) error {
	outcome := run.StateFailed
	if errors.Is(executionErr, ErrAgentOutcomeLost) || ctx.Err() != nil {
		outcome = run.StateUnknown
	}
	finishCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		finishCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	}
	defer cancel()
	if err := worker.Client.Finish(finishCtx, claim, outcome); err != nil {
		return errors.Join(executionErr, fmt.Errorf("record %s Attempt: %w", outcome, err))
	}
	return nil
}
