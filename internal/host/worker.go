package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/run"
)

// RunClient is the Host-side consumer contract for one Machine's Run authority.
type RunClient interface {
	Claim(context.Context) (run.Claim, error)
	Renew(context.Context, run.Claim) (time.Time, error)
	Commit(context.Context, run.Claim, UnderstandingUpdate) error
	Finish(context.Context, run.Claim, run.State) error
}

// ConversationClient is the Host-side consumer contract for one Machine's private reply authority.
type ConversationClient interface {
	ClaimConversation(context.Context) (conversation.ReplyClaim, error)
	RenewConversation(context.Context, conversation.ReplyClaim) (time.Time, error)
	CommitConversation(context.Context, conversation.ReplyClaim, conversation.ReplyCandidate) (conversation.CommitReplyResult, error)
}

// Worker serially alternates Work and private Conversation execution with one concrete Executor.
type Worker struct {
	Runs          RunClient
	Conversations ConversationClient
	Executor      Executor
	PollInterval  time.Duration
	RenewInterval time.Duration
}

func (worker Worker) Serve(ctx context.Context) error {
	if worker.Runs == nil || worker.Conversations == nil || worker.Executor == nil {
		return errors.New("Host worker dependencies are required")
	}
	if worker.PollInterval <= 0 || worker.RenewInterval <= 0 {
		return errors.New("Host worker intervals must be positive")
	}

	preferConversation := true
	for {
		if ctx.Err() != nil {
			return nil
		}
		var handled bool
		var err error
		if preferConversation {
			handled, err = worker.tryConversation(ctx)
			if handled || err != nil {
				preferConversation = false
			} else {
				handled, err = worker.tryRun(ctx)
				if handled {
					preferConversation = true
				}
			}
		} else {
			handled, err = worker.tryRun(ctx)
			if handled || err != nil {
				preferConversation = true
			} else {
				handled, err = worker.tryConversation(ctx)
				if handled {
					preferConversation = false
				}
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if handled {
			continue
		}
		timer := time.NewTimer(worker.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (worker Worker) tryRun(ctx context.Context) (bool, error) {
	claim, err := worker.Runs.Claim(ctx)
	if errors.Is(err, run.ErrNoRunAvailable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim Run: %w", err)
	}
	if err := worker.executeRun(ctx, claim); err != nil {
		return true, err
	}
	return true, nil
}

func (worker Worker) tryConversation(ctx context.Context) (bool, error) {
	claim, err := worker.Conversations.ClaimConversation(ctx)
	if errors.Is(err, conversation.ErrNoReplyAvailable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim private Conversation reply: %w", err)
	}
	if err := worker.executeConversation(ctx, claim); err != nil {
		return true, err
	}
	return true, nil
}

func (worker Worker) executeRun(ctx context.Context, claim run.Claim) error {
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
			if err := worker.Runs.Commit(ctx, claim, result.update); err != nil {
				return fmt.Errorf("commit current understanding: %w", err)
			}
			return nil
		case <-renewal.C:
			if _, err := worker.Runs.Renew(ctx, claim); err != nil {
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

func (worker Worker) executeConversation(ctx context.Context, claim conversation.ReplyClaim) error {
	executionCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	type executionResult struct {
		candidate conversation.ReplyCandidate
		err       error
	}
	completed := make(chan executionResult, 1)
	go func() {
		candidate, executeErr := worker.Executor.Reply(executionCtx, ConversationReplyRequest{Messages: claim.Messages})
		completed <- executionResult{candidate: candidate, err: executeErr}
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
				return fmt.Errorf("generate private Conversation reply: %w", result.err)
			}
			if _, err := worker.Conversations.CommitConversation(ctx, claim, result.candidate); err != nil {
				return fmt.Errorf("commit private Conversation reply: %w", err)
			}
			return nil
		case <-renewal.C:
			if _, err := worker.Conversations.RenewConversation(ctx, claim); err != nil {
				cancelExecution()
				<-completed
				return fmt.Errorf("renew private Conversation reply: %w", err)
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
	if err := worker.Runs.Finish(finishCtx, claim, outcome); err != nil {
		return errors.Join(executionErr, fmt.Errorf("record %s Attempt: %w", outcome, err))
	}
	return nil
}
