package host

import (
	"context"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/run"
)

func TestWorkerCommitsOneValidatedAgentDraft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &recordingRunClient{
		claim: run.Claim{Coordinator: run.Coordinator{RunID: "run-1"}, AttemptID: "attempt-1", Fence: 1},
		attemptContext: run.Context{
			Goal: "Prepare the renewal brief", CurrentUnderstanding: "Finance approved the term.",
			Inputs: []run.Input{{Sequence: 2, Kind: run.InputMessage, Text: "Legal supplied wording"}},
		},
		onCommit: cancel,
	}
	executor := &recordingExecutor{draft: Draft{
		Understanding: "Finance approved the term and legal supplied wording.",
		NextStep:      "Ask the owner to verify the brief.",
	}}
	worker := Worker{Client: client, Executor: executor, PollInterval: time.Hour, RenewInterval: time.Hour}

	if err := worker.Serve(ctx); err != nil {
		t.Fatalf("serve Host worker: %v", err)
	}
	if client.committed.Understanding != executor.draft.Understanding || client.finished != "" {
		t.Fatalf("commit = %#v, finish = %q", client.committed, client.finished)
	}
	if executor.request.CurrentUnderstanding != client.attemptContext.CurrentUnderstanding || len(executor.request.Inputs) != 1 {
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
				claim:          run.Claim{Coordinator: run.Coordinator{RunID: "run-outcome"}, AttemptID: "attempt-outcome", Fence: 1},
				attemptContext: run.Context{Goal: "Prepare the renewal brief"},
				onFinish:       cancel,
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

func TestWorkerRenewsWhileAgentIsRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	client := &recordingRunClient{
		claim:          run.Claim{Coordinator: run.Coordinator{RunID: "run-renew"}, AttemptID: "attempt-renew", Fence: 1},
		attemptContext: run.Context{Goal: "Prepare the renewal brief"},
		onRenew:        func() { close(release) }, onCommit: cancel,
	}
	executor := &recordingExecutor{wait: release, draft: Draft{Understanding: "Known", NextStep: "Continue"}}
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
	draft   Draft
	err     error
	wait    <-chan struct{}
}

func (*recordingExecutor) Diagnose(context.Context) error { return nil }

func (e *recordingExecutor) Execute(ctx context.Context, request ExecutionRequest) (Draft, error) {
	e.request = request
	if e.wait != nil {
		select {
		case <-ctx.Done():
			return Draft{}, ErrAgentOutcomeLost
		case <-e.wait:
		}
	}
	return e.draft, e.err
}

type recordingRunClient struct {
	claim          run.Claim
	attemptContext run.Context
	claimCalls     int
	renewals       int
	committed      Draft
	finished       run.State
	onRenew        func()
	onCommit       func()
	onFinish       func()
}

func (c *recordingRunClient) Claim(context.Context) (run.Claim, error) {
	c.claimCalls++
	if c.claimCalls == 1 {
		return c.claim, nil
	}
	return run.Claim{}, run.ErrNoPendingRun
}

func (c *recordingRunClient) Renew(context.Context, run.Claim) (time.Time, error) {
	c.renewals++
	if c.onRenew != nil {
		c.onRenew()
		c.onRenew = nil
	}
	return time.Now().Add(time.Minute), nil
}

func (c *recordingRunClient) LoadContext(context.Context, run.Claim) (run.Context, error) {
	return c.attemptContext, nil
}

func (c *recordingRunClient) Commit(_ context.Context, _ run.Claim, draft Draft) error {
	c.committed = draft
	if c.onCommit != nil {
		c.onCommit()
	}
	return nil
}

func (c *recordingRunClient) Finish(_ context.Context, _ run.Claim, outcome run.State) error {
	c.finished = outcome
	if c.onFinish != nil {
		c.onFinish()
	}
	return nil
}

var _ Executor = (*recordingExecutor)(nil)
var _ RunClient = (*recordingRunClient)(nil)
