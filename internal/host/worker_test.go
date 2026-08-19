package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/run"
)

func TestWorkerCommitsOneValidatedUnderstandingUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &recordingRunClient{
		claim: run.Claim{
			RunID: "run-1", AttemptID: "attempt-1", Fence: 1,
			Goal: "Prepare the renewal brief", CurrentUnderstanding: "Finance approved the term.",
			Messages: []run.Message{{Text: "Legal supplied wording"}},
		},
		onCommit: cancel,
	}
	executor := &recordingExecutor{update: UnderstandingUpdate{
		Understanding: "Finance approved the term and legal supplied wording.",
		NextStep:      "Ask the owner to verify the brief.",
	}}
	worker := Worker{Client: client, Executor: executor, PollInterval: time.Hour, RenewInterval: time.Hour}

	if err := worker.Serve(ctx); err != nil {
		t.Fatalf("serve Host worker: %v", err)
	}
	if client.committed.Understanding != executor.update.Understanding || client.finished != "" {
		t.Fatalf("commit = %#v, finish = %q", client.committed, client.finished)
	}
	if executor.request.CurrentUnderstanding != client.claim.CurrentUnderstanding || len(executor.request.Messages) != 1 {
		t.Fatalf("execution request = %#v", executor.request)
	}
}

func TestWorkerRecordsKnownAndUnknownAgentOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		err     error
		outcome run.State
	}{
		{name: "known failure", err: ErrAgentFailed, outcome: run.StateFailed},
		{name: "lost outcome", err: ErrAgentOutcomeLost, outcome: run.StateUnknown},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			client := &recordingRunClient{
				claim: run.Claim{
					RunID: "run-outcome", AttemptID: "attempt-outcome", Fence: 1,
					Goal: "Prepare the renewal brief",
				},
				onFinish: cancel,
			}
			worker := Worker{
				Client: client, Executor: &recordingExecutor{err: testCase.err},
				PollInterval: time.Hour, RenewInterval: time.Hour,
			}

			if err := worker.Serve(ctx); err != nil {
				t.Fatalf("serve Host worker: %v", err)
			}
			if client.finished != testCase.outcome || client.committed.Understanding != "" {
				t.Fatalf("finish = %q, commit = %#v", client.finished, client.committed)
			}
		})
	}
}

func TestWorkerLeavesAttemptActiveWhenHostStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	client := &recordingRunClient{claim: run.Claim{
		RunID: "run-cancel", AttemptID: "attempt-cancel", Fence: 1,
		Goal: "Prepare the renewal brief",
	}}
	worker := Worker{
		Client: client, Executor: &recordingExecutor{started: started, wait: make(chan struct{})},
		PollInterval: time.Hour, RenewInterval: time.Hour,
	}
	done := make(chan error, 1)
	go func() { done <- worker.Serve(ctx) }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve cancelled Host worker: %v", err)
	}
	if client.finished != "" || client.committed.Understanding != "" {
		t.Fatalf("cancelled Host persisted finish %q or commit %#v", client.finished, client.committed)
	}
}

func TestWorkerLeavesAttemptActiveWhenRenewalFails(t *testing.T) {
	renewFailure := errors.New("lease authority lost")
	client := &recordingRunClient{
		claim: run.Claim{
			RunID: "run-renew-failure", AttemptID: "attempt-renew-failure", Fence: 1,
			Goal: "Prepare the renewal brief",
		},
		renewErr: renewFailure,
	}
	worker := Worker{
		Client: client, Executor: &recordingExecutor{wait: make(chan struct{})},
		PollInterval: time.Hour, RenewInterval: time.Millisecond,
	}
	if err := worker.Serve(context.Background()); !errors.Is(err, renewFailure) {
		t.Fatalf("renewal failure = %v, want %v", err, renewFailure)
	}
	if client.finished != "" || client.committed.Understanding != "" {
		t.Fatalf("lost renewal persisted finish %q or commit %#v", client.finished, client.committed)
	}
}

func TestWorkerRenewsWhileAgentIsRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	client := &recordingRunClient{
		claim: run.Claim{
			RunID: "run-renew", AttemptID: "attempt-renew", Fence: 1,
			Goal: "Prepare the renewal brief",
		},
		onRenew: func() { close(release) }, onCommit: cancel,
	}
	executor := &recordingExecutor{
		wait:   release,
		update: UnderstandingUpdate{Understanding: "Known", NextStep: "Continue"},
	}
	worker := Worker{Client: client, Executor: executor, PollInterval: time.Hour, RenewInterval: time.Millisecond}

	if err := worker.Serve(ctx); err != nil {
		t.Fatalf("serve renewing Host worker: %v", err)
	}
	if client.renewals != 1 {
		t.Fatalf("renewals = %d, want 1", client.renewals)
	}
}

type recordingExecutor struct {
	request ExecutionRequest
	update  UnderstandingUpdate
	err     error
	wait    <-chan struct{}
	started chan<- struct{}
}

func (*recordingExecutor) Diagnose(context.Context) error { return nil }

func (executor *recordingExecutor) Execute(ctx context.Context, request ExecutionRequest) (UnderstandingUpdate, error) {
	executor.request = request
	if executor.started != nil {
		executor.started <- struct{}{}
	}
	if executor.wait != nil {
		select {
		case <-ctx.Done():
			return UnderstandingUpdate{}, ErrAgentOutcomeLost
		case <-executor.wait:
		}
	}
	return executor.update, executor.err
}

type recordingRunClient struct {
	claim      run.Claim
	claimCalls int
	renewals   int
	renewErr   error
	committed  UnderstandingUpdate
	finished   run.State
	onRenew    func()
	onCommit   func()
	onFinish   func()
}

func (client *recordingRunClient) Claim(context.Context) (run.Claim, error) {
	client.claimCalls++
	if client.claimCalls == 1 {
		return client.claim, nil
	}
	return run.Claim{}, run.ErrNoRunAvailable
}

func (client *recordingRunClient) Renew(context.Context, run.Claim) (time.Time, error) {
	client.renewals++
	if client.onRenew != nil {
		client.onRenew()
		client.onRenew = nil
	}
	if client.renewErr != nil {
		return time.Time{}, client.renewErr
	}
	return time.Now().Add(time.Minute), nil
}

func (client *recordingRunClient) Commit(_ context.Context, _ run.Claim, update UnderstandingUpdate) error {
	client.committed = update
	if client.onCommit != nil {
		client.onCommit()
	}
	return nil
}

func (client *recordingRunClient) Finish(_ context.Context, _ run.Claim, outcome run.State) error {
	client.finished = outcome
	if client.onFinish != nil {
		client.onFinish()
	}
	return nil
}

var _ Executor = (*recordingExecutor)(nil)
var _ RunClient = (*recordingRunClient)(nil)
