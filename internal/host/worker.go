package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/run"
)

// RunClient is the Host-side consumer contract for one Machine's Run authority.
type RunClient interface {
	Claim(context.Context) (run.Claim, error)
	Renew(context.Context, run.Claim) (time.Time, error)
	Commit(context.Context, run.Claim, UnderstandingUpdate) error
	Finish(context.Context, run.Claim, run.State) error
}

// Worker claims and executes Work with one concrete Executor selected before serving.
type Worker struct {
	Client        RunClient
	Executor      Executor
	PollInterval  time.Duration
	RenewInterval time.Duration
}

func (worker Worker) Serve(ctx context.Context) error {
	if worker.Client == nil || worker.Executor == nil {
		return errors.New("Host worker dependencies are required")
	}
	if worker.PollInterval <= 0 || worker.RenewInterval <= 0 {
		return errors.New("Host worker intervals must be positive")
	}
	ticker := time.NewTicker(worker.PollInterval)
	defer ticker.Stop()
	for {
		claim, err := worker.Client.Claim(ctx)
		if errors.Is(err, run.ErrNoRunAvailable) {
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
			return fmt.Errorf("claim Run: %w", err)
		}
		if err := worker.executeClaim(ctx, claim); err != nil {
			return err
		}
	}
}

func (worker Worker) executeClaim(ctx context.Context, claim run.Claim) error {
	request := ExecutionRequest{
		Goal: claim.Goal, CurrentUnderstanding: claim.CurrentUnderstanding,
		CurrentNextStep: claim.CurrentNextStep, Messages: claim.Messages,
	}
	executionCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	type executionResult struct {
		update UnderstandingUpdate
		err    error
	}
	completed := make(chan executionResult, 1)
	go func() {
		update, executeErr := worker.Executor.Execute(executionCtx, request)
		completed <- executionResult{update: update, err: executeErr}
	}()
	renewal := time.NewTicker(worker.RenewInterval)
	defer renewal.Stop()
	for {
		select {
		case result := <-completed:
			if result.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return worker.finishExecution(ctx, claim, result.err)
			}
			if err := worker.Client.Commit(ctx, claim, result.update); err != nil {
				return fmt.Errorf("commit current understanding: %w", err)
			}
			return nil
		case <-renewal.C:
			if _, err := worker.Client.Renew(ctx, claim); err != nil {
				cancelExecution()
				<-completed
				return fmt.Errorf("renew active Attempt: %w", err)
			}
		case <-ctx.Done():
			cancelExecution()
			<-completed
			return nil
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
