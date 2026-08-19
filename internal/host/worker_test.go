package host

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/run"
)

func TestWorkerCommitsOneValidatedUnderstandingUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	claim := run.Claim{
		RunID: "run-1", AttemptID: "attempt-1", Fence: 1,
		Goal: "Prepare the renewal brief", CurrentUnderstanding: "Finance approved the term.",
		Messages: []run.Message{{Text: "Legal supplied wording"}},
	}
	runs := &recordingRunClient{claims: []run.Claim{claim}, onCommit: cancel}
	executor := &recordingExecutor{update: UnderstandingUpdate{
		Understanding: "Finance approved the term and legal supplied wording.",
		NextStep:      "Ask the owner to verify the brief.",
	}}
	worker := testWorker(runs, &recordingConversationClient{}, executor)

	if err := worker.Serve(ctx); err != nil {
		t.Fatalf("serve Host worker: %v", err)
	}
	if runs.committed.Understanding != executor.update.Understanding || runs.finished != "" {
		t.Fatalf("commit = %#v, finish = %q", runs.committed, runs.finished)
	}
	if executor.workRequest.CurrentUnderstanding != claim.CurrentUnderstanding || len(executor.workRequest.Messages) != 1 {
		t.Fatalf("execution request = %#v", executor.workRequest)
	}
}

func TestWorkerCommitsPrivateReplyAndDelegationCandidate(t *testing.T) {
	for _, testCase := range []struct {
		name string
		goal *string
	}{
		{name: "ordinary reply"},
		{name: "delegation", goal: stringPointer("Prepare the renewal packet")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			claim := privateClaim("source-" + testCase.name)
			conversations := &recordingConversationClient{claims: []conversation.ReplyClaim{claim}, onCommit: cancel}
			executor := &recordingExecutor{reply: conversation.ReplyCandidate{
				Reply: "I can help with that.", DelegationGoal: testCase.goal,
			}}
			worker := testWorker(&recordingRunClient{}, conversations, executor)

			if err := worker.Serve(ctx); err != nil {
				t.Fatalf("serve private reply: %v", err)
			}
			if !reflect.DeepEqual(conversations.committed, executor.reply) {
				t.Fatalf("committed candidate = %#v, want %#v", conversations.committed, executor.reply)
			}
			if !reflect.DeepEqual(executor.replyRequest.Messages, claim.Messages) {
				t.Fatalf("private request = %#v", executor.replyRequest)
			}
		})
	}
}

func TestWorkerRenewsPrivateReplyWhileAgentRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	conversations := &recordingConversationClient{
		claims:  []conversation.ReplyClaim{privateClaim("source-renew")},
		onRenew: func() { close(release) }, onCommit: cancel,
	}
	executor := &recordingExecutor{
		replyWait: release,
		reply:     conversation.ReplyCandidate{Reply: "Renewed before commit."},
	}
	worker := testWorker(&recordingRunClient{}, conversations, executor)
	worker.RenewInterval = time.Millisecond

	if err := worker.Serve(ctx); err != nil {
		t.Fatalf("serve renewing private reply: %v", err)
	}
	if conversations.renewals != 1 || conversations.committed.Reply == "" {
		t.Fatalf("renewals = %d, committed = %#v", conversations.renewals, conversations.committed)
	}
}

func TestWorkerLeavesPrivateReplyUnresolvedWhenHostStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	conversations := &recordingConversationClient{claims: []conversation.ReplyClaim{privateClaim("source-cancel")}}
	executor := &recordingExecutor{replyStarted: started, replyWait: make(chan struct{})}
	worker := testWorker(&recordingRunClient{}, conversations, executor)
	done := make(chan error, 1)
	go func() { done <- worker.Serve(ctx) }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve cancelled private reply: %v", err)
	}
	if conversations.commits != 0 {
		t.Fatalf("cancelled private reply commits = %d", conversations.commits)
	}
}

func TestWorkerLeavesPrivateReplyUnresolvedWhenRenewalFails(t *testing.T) {
	renewFailure := errors.New("private lease authority lost")
	conversations := &recordingConversationClient{
		claims: []conversation.ReplyClaim{privateClaim("source-renew-failure")}, renewErr: renewFailure,
	}
	worker := testWorker(
		&recordingRunClient{},
		conversations,
		&recordingExecutor{replyWait: make(chan struct{})},
	)
	worker.RenewInterval = time.Millisecond
	if err := worker.Serve(context.Background()); !errors.Is(err, renewFailure) {
		t.Fatalf("private renewal failure = %v, want %v", err, renewFailure)
	}
	if conversations.commits != 0 {
		t.Fatalf("lost private renewal commits = %d", conversations.commits)
	}
}

func TestWorkerAlternatesWorkAndConversationWithoutStarvation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var commits int
	var commitLock sync.Mutex
	onCommit := func() {
		commitLock.Lock()
		defer commitLock.Unlock()
		commits++
		if commits == 6 {
			cancel()
		}
	}
	runs := &recordingRunClient{
		claims: []run.Claim{workClaim("run-1"), workClaim("run-2"), workClaim("run-3")}, onCommit: onCommit,
	}
	conversations := &recordingConversationClient{
		claims: []conversation.ReplyClaim{privateClaim("source-1"), privateClaim("source-2"), privateClaim("source-3")}, onCommit: onCommit,
	}
	executor := &recordingExecutor{
		update: UnderstandingUpdate{Understanding: "Known", NextStep: "Continue"},
		reply:  conversation.ReplyCandidate{Reply: "Private answer"},
	}
	if err := testWorker(runs, conversations, executor).Serve(ctx); err != nil {
		t.Fatalf("serve alternating worker: %v", err)
	}
	if want := []string{"conversation", "work", "conversation", "work", "conversation", "work"}; !reflect.DeepEqual(executor.order, want) {
		t.Fatalf("execution order = %#v, want %#v", executor.order, want)
	}
	if runs.commits != 3 || conversations.commits != 3 {
		t.Fatalf("Work/private commits = %d/%d, want 3/3", runs.commits, conversations.commits)
	}
}

func TestWorkerFallsBackWhenPreferredClassIsEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runs := &recordingRunClient{
		claims: []run.Claim{workClaim("run-fallback-1"), workClaim("run-fallback-2")},
	}
	runs.onCommit = func() {
		if runs.commits == 2 {
			cancel()
		}
	}
	executor := &recordingExecutor{update: UnderstandingUpdate{Understanding: "Known", NextStep: "Continue"}}
	if err := testWorker(runs, &recordingConversationClient{}, executor).Serve(ctx); err != nil {
		t.Fatalf("serve fallback worker: %v", err)
	}
	if want := []string{"work", "work"}; !reflect.DeepEqual(executor.order, want) {
		t.Fatalf("fallback order = %#v, want %#v", executor.order, want)
	}
}

func TestWorkerPreservesWorkFailureOutcomes(t *testing.T) {
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
			runs := &recordingRunClient{claims: []run.Claim{workClaim("run-outcome")}, onFinish: cancel}
			worker := testWorker(runs, &recordingConversationClient{}, &recordingExecutor{workErr: testCase.err})
			if err := worker.Serve(ctx); err != nil {
				t.Fatalf("serve Host worker: %v", err)
			}
			if runs.finished != testCase.outcome || runs.commits != 0 {
				t.Fatalf("finish = %q, commits = %d", runs.finished, runs.commits)
			}
		})
	}
}

func TestWorkerLeavesWorkAttemptActiveOnCancellationAndRenewLoss(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		runs := &recordingRunClient{claims: []run.Claim{workClaim("run-cancel")}}
		worker := testWorker(runs, &recordingConversationClient{}, &recordingExecutor{
			workStarted: started, workWait: make(chan struct{}),
		})
		done := make(chan error, 1)
		go func() { done <- worker.Serve(ctx) }()
		<-started
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("serve cancelled Host worker: %v", err)
		}
		if runs.finished != "" || runs.commits != 0 {
			t.Fatalf("cancelled Work persisted finish %q or %d commits", runs.finished, runs.commits)
		}
	})

	t.Run("renew loss", func(t *testing.T) {
		renewFailure := errors.New("lease authority lost")
		runs := &recordingRunClient{claims: []run.Claim{workClaim("run-renew-failure")}, renewErr: renewFailure}
		worker := testWorker(runs, &recordingConversationClient{}, &recordingExecutor{workWait: make(chan struct{})})
		worker.RenewInterval = time.Millisecond
		if err := worker.Serve(context.Background()); !errors.Is(err, renewFailure) {
			t.Fatalf("renewal failure = %v, want %v", err, renewFailure)
		}
		if runs.finished != "" || runs.commits != 0 {
			t.Fatalf("lost renewal persisted finish %q or %d commits", runs.finished, runs.commits)
		}
	})
}

func testWorker(runs RunClient, conversations ConversationClient, executor Executor) Worker {
	return Worker{
		Runs: runs, Conversations: conversations, Executor: executor,
		PollInterval: time.Hour, RenewInterval: time.Hour,
	}
}

func workClaim(id string) run.Claim {
	return run.Claim{RunID: id, AttemptID: "attempt-" + id, Fence: 1, Goal: "Advance Work"}
}

func privateClaim(sourceID string) conversation.ReplyClaim {
	return conversation.ReplyClaim{
		SourceMessageID: sourceID, Fence: 1,
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "Please help privately."}},
	}
}

func stringPointer(value string) *string { return &value }

type recordingExecutor struct {
	workRequest  ExecutionRequest
	replyRequest ConversationReplyRequest
	update       UnderstandingUpdate
	reply        conversation.ReplyCandidate
	workErr      error
	replyErr     error
	workWait     <-chan struct{}
	replyWait    <-chan struct{}
	workStarted  chan<- struct{}
	replyStarted chan<- struct{}
	order        []string
}

func (*recordingExecutor) Diagnose(context.Context) error { return nil }

func (executor *recordingExecutor) Execute(ctx context.Context, request ExecutionRequest) (UnderstandingUpdate, error) {
	executor.order = append(executor.order, "work")
	executor.workRequest = request
	if executor.workStarted != nil {
		executor.workStarted <- struct{}{}
	}
	if executor.workWait != nil {
		select {
		case <-ctx.Done():
			return UnderstandingUpdate{}, ErrAgentOutcomeLost
		case <-executor.workWait:
		}
	}
	return executor.update, executor.workErr
}

func (executor *recordingExecutor) Reply(ctx context.Context, request ConversationReplyRequest) (conversation.ReplyCandidate, error) {
	executor.order = append(executor.order, "conversation")
	executor.replyRequest = request
	if executor.replyStarted != nil {
		executor.replyStarted <- struct{}{}
	}
	if executor.replyWait != nil {
		select {
		case <-ctx.Done():
			return conversation.ReplyCandidate{}, ErrAgentOutcomeLost
		case <-executor.replyWait:
		}
	}
	return executor.reply, executor.replyErr
}

type recordingRunClient struct {
	claims     []run.Claim
	claimCalls int
	renewals   int
	renewErr   error
	committed  UnderstandingUpdate
	commits    int
	finished   run.State
	onCommit   func()
	onFinish   func()
}

func (client *recordingRunClient) Claim(context.Context) (run.Claim, error) {
	client.claimCalls++
	if len(client.claims) == 0 {
		return run.Claim{}, run.ErrNoRunAvailable
	}
	claim := client.claims[0]
	client.claims = client.claims[1:]
	return claim, nil
}

func (client *recordingRunClient) Renew(context.Context, run.Claim) (time.Time, error) {
	client.renewals++
	if client.renewErr != nil {
		return time.Time{}, client.renewErr
	}
	return time.Now().Add(time.Minute), nil
}

func (client *recordingRunClient) Commit(_ context.Context, _ run.Claim, update UnderstandingUpdate) error {
	client.committed = update
	client.commits++
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

type recordingConversationClient struct {
	claims    []conversation.ReplyClaim
	renewals  int
	renewErr  error
	committed conversation.ReplyCandidate
	commits   int
	onRenew   func()
	onCommit  func()
}

func (client *recordingConversationClient) ClaimConversation(context.Context) (conversation.ReplyClaim, error) {
	if len(client.claims) == 0 {
		return conversation.ReplyClaim{}, conversation.ErrNoReplyAvailable
	}
	claim := client.claims[0]
	client.claims = client.claims[1:]
	return claim, nil
}

func (client *recordingConversationClient) RenewConversation(context.Context, conversation.ReplyClaim) (time.Time, error) {
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

func (client *recordingConversationClient) CommitConversation(
	_ context.Context,
	_ conversation.ReplyClaim,
	candidate conversation.ReplyCandidate,
) (conversation.CommitReplyResult, error) {
	client.committed = candidate
	client.commits++
	if client.onCommit != nil {
		client.onCommit()
	}
	return conversation.CommitReplyResult{ReplyMessageID: "reply"}, nil
}

var _ Executor = (*recordingExecutor)(nil)
var _ RunClient = (*recordingRunClient)(nil)
var _ ConversationClient = (*recordingConversationClient)(nil)
