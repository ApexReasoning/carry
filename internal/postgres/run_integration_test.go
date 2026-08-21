//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
)

type runFixture struct {
	store     *Store
	bootstrap testMember
	work      work.Work
	machineID string
}

func newRunFixture(t *testing.T, ctx context.Context) runFixture {
	t.Helper()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Run Owner", SpaceName: "Coordination Space",
	})
	if err != nil {
		t.Fatalf("bootstrap Run fixture: %v", err)
	}
	created, err := store.CreateWork(ctx, work.CreateCommand{
		SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
		Goal: "Prepare the customer renewal brief", IdempotencyKey: "create-run-work",
	})
	if err != nil {
		t.Fatalf("create Run fixture Work: %v", err)
	}
	machineID := uuid.NewString()
	_, err = store.EnrollMachine(ctx, machine.EnrollMachineCommand{
		MachineID: machineID, SpaceID: bootstrap.SpaceID, DisplayName: "Run Host",
		PublicKeyDER: []byte("run-public-key"), CertificatePEM: []byte("run-certificate"),
		CertificateSerial: uuid.NewString(), EnrolledByUserID: bootstrap.UserID,
		IdempotencyKey: "enroll-run-host",
	})
	if err != nil {
		t.Fatalf("enroll Run fixture Machine: %v", err)
	}
	return runFixture{store: store, bootstrap: bootstrap, work: created, machineID: machineID}
}

func TestConcurrentClaimCreatesOneRunAttemptWithFixedMessages(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	message, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "Finance approved a twelve month term",
		IdempotencyKey: "finance-term",
	})
	if err != nil {
		t.Fatalf("append message: %v", err)
	}

	const callers = 8
	claims := make(chan run.Claim, callers)
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			claim, claimErr := fixture.store.ClaimRun(ctx, fixture.machineID)
			if claimErr == nil {
				claims <- claim
				return
			}
			errorsSeen <- claimErr
		}()
	}
	wait.Wait()
	close(claims)
	close(errorsSeen)
	if len(claims) != 1 {
		t.Fatalf("successful claims = %d, want 1", len(claims))
	}
	for claimErr := range errorsSeen {
		if !errors.Is(claimErr, run.ErrNoRunAvailable) {
			t.Fatalf("losing claim error = %v", claimErr)
		}
	}
	claim := <-claims
	if claim.Fence != 1 || claim.Goal != fixture.work.Goal || claim.InputEndSeq != message.InputSeq {
		t.Fatalf("claim = %#v", claim)
	}
	if len(claim.Messages) != 1 || claim.Messages[0].Text != "Finance approved a twelve month term" {
		t.Fatalf("claim messages = %#v", claim.Messages)
	}
}

func TestBoundedRunInputsContinueWithoutOmission(t *testing.T) {
	t.Run("message count", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		for index := range run.MaxInputMessages + 3 {
			if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
				WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
				AuthorUserID: fixture.bootstrap.UserID, Text: fmt.Sprintf("constraint %02d", index),
				IdempotencyKey: fmt.Sprintf("constraint-%02d", index),
			}); err != nil {
				t.Fatalf("append message %d: %v", index, err)
			}
		}
		first, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim first bounded Run: %v", err)
		}
		if len(first.Messages) != run.MaxInputMessages {
			t.Fatalf("first message count = %d, want %d", len(first.Messages), run.MaxInputMessages)
		}
		if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
			MachineID: fixture.machineID, RunID: first.RunID, AttemptID: first.AttemptID,
			Fence: first.Fence, BaseUnderstandingVersion: first.BaseUnderstandingVersion,
			InputEndSeq: first.InputEndSeq, Understanding: "The first bounded input is applied.",
			NextStep: "Apply the remaining constraints.",
		}); err != nil {
			t.Fatalf("commit first bounded Run: %v", err)
		}
		second, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim second bounded Run: %v", err)
		}
		if len(second.Messages) != 3 || second.Messages[0].Text != "constraint 32" {
			t.Fatalf("second messages = %#v", second.Messages)
		}
	})

	t.Run("text bytes", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		text := strings.Repeat("x", work.MaxMessageBytes)
		for index := range 5 {
			if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
				WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
				AuthorUserID: fixture.bootstrap.UserID, Text: text,
				IdempotencyKey: fmt.Sprintf("large-input-%d", index),
			}); err != nil {
				t.Fatalf("append large message %d: %v", index, err)
			}
		}
		first, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim byte-bounded Run: %v", err)
		}
		if len(first.Messages) != 4 {
			t.Fatalf("byte-bounded message count = %d, want 4", len(first.Messages))
		}
		if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
			MachineID: fixture.machineID, RunID: first.RunID, AttemptID: first.AttemptID,
			Fence: first.Fence, BaseUnderstandingVersion: first.BaseUnderstandingVersion,
			InputEndSeq: first.InputEndSeq, Understanding: "Four large inputs are applied.",
			NextStep: "Apply the final input.",
		}); err != nil {
			t.Fatalf("commit byte-bounded Run: %v", err)
		}
		second, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim remaining byte-bounded Run: %v", err)
		}
		if len(second.Messages) != 1 || second.InputEndSeq != first.InputEndSeq+1 {
			t.Fatalf("remaining claim = %#v", second)
		}
	})
}

func TestIdempotentWorkCreateReplayReturnsCurrentFacts(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim Run: %v", err)
	}
	commit := run.CommitCommand{
		MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
		Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
		InputEndSeq: claim.InputEndSeq, Understanding: "The renewal is confirmed.",
		NextStep: "Prepare the recommendation.",
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, commit); err != nil {
		t.Fatalf("commit Work: %v", err)
	}
	replayed, err := fixture.store.CreateWork(ctx, work.CreateCommand{
		SpaceID: fixture.bootstrap.SpaceID, CreatorUserID: fixture.bootstrap.UserID,
		Goal: fixture.work.Goal, IdempotencyKey: "create-run-work",
	})
	if err != nil {
		t.Fatalf("replay Work create: %v", err)
	}
	if replayed.WorkID != fixture.work.WorkID || replayed.Understanding != commit.Understanding ||
		replayed.NextStep != commit.NextStep || replayed.HasUnappliedInput || replayed.NeedsRetry {
		t.Fatalf("replayed Work = %#v", replayed)
	}
}

func TestCommitUpdatesWorkDirectlyAndLeavesLateMessageForNextRun(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim first Run: %v", err)
	}
	late, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "Legal supplied final wording",
		IdempotencyKey: "legal-wording",
	})
	if err != nil {
		t.Fatalf("append late message: %v", err)
	}

	command := run.CommitCommand{
		MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
		Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
		InputEndSeq:   claim.InputEndSeq,
		Understanding: "The renewal goal is understood.", NextStep: "Apply legal wording.",
	}
	for _, testCase := range []struct {
		name   string
		change func(*run.CommitCommand)
	}{
		{name: "Machine", change: func(value *run.CommitCommand) { value.MachineID = uuid.NewString() }},
		{name: "fence", change: func(value *run.CommitCommand) { value.Fence++ }},
		{name: "base version", change: func(value *run.CommitCommand) { value.BaseUnderstandingVersion++ }},
		{name: "input end", change: func(value *run.CommitCommand) { value.InputEndSeq++ }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stale := command
			testCase.change(&stale)
			if err := fixture.store.CommitWorkUnderstanding(ctx, stale); !errors.Is(err, run.ErrStaleAttempt) {
				t.Fatalf("stale commit error = %v", err)
			}
		})
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, command); err != nil {
		t.Fatalf("commit understanding: %v", err)
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, command); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("second commit error = %v", err)
	}

	details, err := fixture.store.LoadWork(ctx, work.LoadCommand{UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID, WorkID: fixture.work.WorkID})
	if err != nil {
		t.Fatalf("load committed Work: %v", err)
	}
	if details.Work.Understanding != command.Understanding || details.Work.NextStep != command.NextStep ||
		!details.Work.HasUnappliedInput {
		t.Fatalf("committed Work = %#v", details.Work)
	}
	next, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim late message Run: %v", err)
	}
	if next.RunID == claim.RunID || next.BaseUnderstandingVersion != 1 || next.InputEndSeq != late.InputSeq ||
		len(next.Messages) != 1 || next.Messages[0].Text != late.Text {
		t.Fatalf("next claim = %#v", next)
	}
}

func TestFailedAndUnknownRunsRequireExplicitRetry(t *testing.T) {
	for _, outcome := range []run.State{run.StateFailed, run.StateUnknown} {
		t.Run(string(outcome), func(t *testing.T) {
			ctx := context.Background()
			fixture := newRunFixture(t, ctx)
			claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
			if err != nil {
				t.Fatalf("claim Run: %v", err)
			}
			if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
				MachineID: fixture.machineID, RunID: claim.RunID,
				AttemptID: claim.AttemptID, Fence: claim.Fence, Outcome: outcome,
			}); err != nil {
				t.Fatalf("finish %s Run: %v", outcome, err)
			}
			if _, err := fixture.store.ClaimRun(ctx, fixture.machineID); !errors.Is(err, run.ErrNoRunAvailable) {
				t.Fatalf("claim after %s error = %v", outcome, err)
			}
			if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
				WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
				AuthorUserID: fixture.bootstrap.UserID, Text: "Please include the newest figures",
				IdempotencyKey: "new-figures",
			}); err != nil {
				t.Fatalf("append after terminal Run: %v", err)
			}
			if _, err := fixture.store.ClaimRun(ctx, fixture.machineID); !errors.Is(err, run.ErrNoRunAvailable) {
				t.Fatalf("new input bypassed explicit retry: %v", err)
			}
			details, err := fixture.store.LoadWork(ctx, work.LoadCommand{UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID, WorkID: fixture.work.WorkID})
			if err != nil || !details.Work.NeedsRetry {
				t.Fatalf("Work retry fact = %#v, error = %v", details.Work, err)
			}
			retry := work.RetryCommand{
				WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
				RequestedBy: fixture.bootstrap.UserID, IdempotencyKey: "retry-terminal-run",
			}
			if err := fixture.store.RequestWorkRetry(ctx, retry); err != nil {
				t.Fatalf("request retry: %v", err)
			}
			if err := fixture.store.RequestWorkRetry(ctx, retry); err != nil {
				t.Fatalf("replay retry: %v", err)
			}
			fresh, err := fixture.store.ClaimRun(ctx, fixture.machineID)
			if err != nil {
				t.Fatalf("claim explicitly retried Work: %v", err)
			}
			if fresh.RunID == claim.RunID || fresh.Fence != 1 {
				t.Fatalf("fresh Run = %#v", fresh)
			}
		})
	}
}

func TestConcurrentWorkRetryHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim Run: %v", err)
	}
	if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
		MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
		Fence: claim.Fence, Outcome: run.StateFailed,
	}); err != nil {
		t.Fatalf("finish Run: %v", err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, key := range []string{"retry-a", "retry-b"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- fixture.store.RequestWorkRetry(ctx, work.RetryCommand{
				WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
				RequestedBy: fixture.bootstrap.UserID, IdempotencyKey: key,
			})
		}()
	}
	wait.Wait()
	close(results)
	var succeeded, rejected int
	for result := range results {
		if result == nil {
			succeeded++
		} else if errors.Is(result, work.ErrRetryNotNeeded) {
			rejected++
		} else {
			t.Fatalf("unexpected retry result: %v", result)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("retry outcomes = %d succeeded, %d rejected", succeeded, rejected)
	}

	claims := make(chan run.Claim, 6)
	claimErrors := make(chan error, 6)
	for range 6 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			fresh, claimErr := fixture.store.ClaimRun(ctx, fixture.machineID)
			if claimErr != nil {
				claimErrors <- claimErr
				return
			}
			claims <- fresh
		}()
	}
	wait.Wait()
	close(claims)
	close(claimErrors)
	if len(claims) != 1 {
		t.Fatalf("fresh Run claim winners = %d, want 1", len(claims))
	}
	for claimErr := range claimErrors {
		if !errors.Is(claimErr, run.ErrNoRunAvailable) {
			t.Fatalf("fresh Run losing claim error = %v", claimErr)
		}
	}
}

func TestRetryIdempotencyCannotAuthorizeALaterTerminalRun(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	first, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim first Run: %v", err)
	}
	if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
		MachineID: fixture.machineID, RunID: first.RunID, AttemptID: first.AttemptID,
		Fence: first.Fence, Outcome: run.StateFailed,
	}); err != nil {
		t.Fatalf("finish first Run: %v", err)
	}
	oldRetry := work.RetryCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		RequestedBy: fixture.bootstrap.UserID, IdempotencyKey: "first-terminal-retry",
	}
	if err := fixture.store.RequestWorkRetry(ctx, oldRetry); err != nil {
		t.Fatalf("request first retry: %v", err)
	}
	second, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim second Run: %v", err)
	}
	if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
		MachineID: fixture.machineID, RunID: second.RunID, AttemptID: second.AttemptID,
		Fence: second.Fence, Outcome: run.StateUnknown,
	}); err != nil {
		t.Fatalf("finish second Run: %v", err)
	}
	if err := fixture.store.RequestWorkRetry(ctx, oldRetry); err != nil {
		t.Fatalf("replay old retry identity: %v", err)
	}
	if _, err := fixture.store.ClaimRun(ctx, fixture.machineID); !errors.Is(err, run.ErrNoRunAvailable) {
		t.Fatalf("old retry identity authorized later terminal Run: %v", err)
	}
	details, err := fixture.store.LoadWork(ctx, work.LoadCommand{UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID, WorkID: fixture.work.WorkID})
	if err != nil || !details.Work.NeedsRetry {
		t.Fatalf("later terminal retry fact = %#v, error = %v", details.Work, err)
	}
	if err := fixture.store.RequestWorkRetry(ctx, work.RetryCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		RequestedBy: fixture.bootstrap.UserID, IdempotencyKey: "second-terminal-retry",
	}); err != nil {
		t.Fatalf("request second retry with new identity: %v", err)
	}
	third, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil || third.RunID == first.RunID || third.RunID == second.RunID {
		t.Fatalf("claim third Run = %#v, error = %v", third, err)
	}
	var firstRunState, firstAttemptState, secondRunState, secondAttemptState string
	if err := fixture.store.pool.QueryRow(ctx, `
		select first_run.state, first_attempt.state, second_run.state, second_attempt.state
		from runs as first_run
		join run_attempts as first_attempt on first_attempt.run_id = first_run.run_id
		join runs as second_run on second_run.run_id = $2
		join run_attempts as second_attempt on second_attempt.run_id = second_run.run_id
		where first_run.run_id = $1
	`, first.RunID, second.RunID).Scan(
		&firstRunState, &firstAttemptState, &secondRunState, &secondAttemptState,
	); err != nil {
		t.Fatalf("load old terminal facts: %v", err)
	}
	if firstRunState != "failed" || firstAttemptState != "failed" ||
		secondRunState != "unknown" || secondAttemptState != "unknown" {
		t.Fatalf("old terminal facts = %s/%s and %s/%s", firstRunState, firstAttemptState, secondRunState, secondAttemptState)
	}
}

func TestRevokedMembershipCannotRequestWorkRetry(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim Run: %v", err)
	}
	if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
		MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
		Fence: claim.Fence, Outcome: run.StateFailed,
	}); err != nil {
		t.Fatalf("finish Run: %v", err)
	}
	if _, err := fixture.store.pool.Exec(ctx, `
		update space_memberships
		set revoked_at = clock_timestamp()
		where space_id = $1 and user_id = $2
	`, fixture.bootstrap.SpaceID, fixture.bootstrap.UserID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if err := fixture.store.RequestWorkRetry(ctx, work.RetryCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		RequestedBy: fixture.bootstrap.UserID, IdempotencyKey: "revoked-member-retry",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("revoked member retry error = %v", err)
	}
}

func TestExpiredAttemptRecoversOnceAndRejectsOldAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	seed, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim seed Run: %v", err)
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
		MachineID: fixture.machineID, RunID: seed.RunID, AttemptID: seed.AttemptID,
		Fence: seed.Fence, BaseUnderstandingVersion: seed.BaseUnderstandingVersion,
		InputEndSeq: seed.InputEndSeq, Understanding: "The current renewal facts are confirmed.",
		NextStep: "Apply the member's latest constraint.",
	}); err != nil {
		t.Fatalf("commit seed understanding: %v", err)
	}
	if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "Do not change the approved budget.",
		IdempotencyKey: "approved-budget-constraint",
	}); err != nil {
		t.Fatalf("append recovery message: %v", err)
	}
	old, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim recovery Run: %v", err)
	}
	if old.CurrentUnderstanding == "" || old.CurrentNextStep == "" || len(old.Messages) != 1 {
		t.Fatalf("recovery seed context = %#v", old)
	}
	if _, err := fixture.store.pool.Exec(ctx, `
		update run_attempts
		set claimed_at = transaction_timestamp() - interval '2 minutes',
		    lease_expires_at = transaction_timestamp() - interval '1 minute'
		where attempt_id = $1
	`, old.AttemptID); err != nil {
		t.Fatalf("expire Attempt: %v", err)
	}
	if _, err := fixture.store.RenewRunAttempt(ctx, fixture.machineID, old.RunID, old.AttemptID, old.Fence); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("expired renew error = %v", err)
	}

	recoveryMachineID := uuid.NewString()
	if _, err := fixture.store.EnrollMachine(ctx, machine.EnrollMachineCommand{
		MachineID: recoveryMachineID, SpaceID: fixture.bootstrap.SpaceID, DisplayName: "Replacement Run Host",
		PublicKeyDER: []byte("replacement-public-key"), CertificatePEM: []byte("replacement-certificate"),
		CertificateSerial: uuid.NewString(), EnrolledByUserID: fixture.bootstrap.UserID,
		IdempotencyKey: "enroll-replacement-run-host",
	}); err != nil {
		t.Fatalf("enroll replacement Machine: %v", err)
	}

	const callers = 6
	claims := make(chan run.Claim, callers)
	errorsSeen := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			claim, claimErr := fixture.store.ClaimRun(ctx, recoveryMachineID)
			if claimErr == nil {
				claims <- claim
				return
			}
			errorsSeen <- claimErr
		}()
	}
	wait.Wait()
	close(claims)
	close(errorsSeen)
	if len(claims) != 1 {
		t.Fatalf("recovery winners = %d, want 1", len(claims))
	}
	for claimErr := range errorsSeen {
		if !errors.Is(claimErr, run.ErrNoRunAvailable) {
			t.Fatalf("losing recovery error = %v", claimErr)
		}
	}
	recovered := <-claims
	var recoveredBy string
	if err := fixture.store.pool.QueryRow(ctx, `select machine_id from run_attempts where attempt_id = $1`, recovered.AttemptID).Scan(&recoveredBy); err != nil {
		t.Fatalf("load recovery Machine: %v", err)
	}
	if recoveredBy != recoveryMachineID {
		t.Fatalf("recovery Machine = %s, want %s", recoveredBy, recoveryMachineID)
	}
	if recovered.RunID != old.RunID || recovered.AttemptID == old.AttemptID || recovered.Fence != old.Fence+1 {
		t.Fatalf("recovered claim = %#v; old = %#v", recovered, old)
	}
	if recovered.WorkID != old.WorkID || recovered.Goal != old.Goal ||
		recovered.CurrentUnderstanding != old.CurrentUnderstanding ||
		recovered.CurrentNextStep != old.CurrentNextStep ||
		recovered.BaseUnderstandingVersion != old.BaseUnderstandingVersion ||
		recovered.InputEndSeq != old.InputEndSeq || !reflect.DeepEqual(recovered.Messages, old.Messages) {
		t.Fatalf("recovered Work context changed: recovered = %#v; old = %#v", recovered, old)
	}
	oldCommit := run.CommitCommand{
		MachineID: fixture.machineID, RunID: old.RunID, AttemptID: old.AttemptID,
		Fence: old.Fence, BaseUnderstandingVersion: old.BaseUnderstandingVersion,
		InputEndSeq: old.InputEndSeq, Understanding: "Late", NextStep: "Do not commit",
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, oldCommit); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("late commit error = %v", err)
	}
	if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
		MachineID: fixture.machineID, RunID: old.RunID, AttemptID: old.AttemptID,
		Fence: old.Fence, Outcome: run.StateUnknown,
	}); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("late finish error = %v", err)
	}
}

func TestAttemptAuthorityCannotCrossLeaseExpiryWhileWaitingForLock(t *testing.T) {
	operations := []struct {
		name string
		run  func(context.Context, runFixture, run.Claim) error
	}{
		{
			name: "renew",
			run: func(ctx context.Context, fixture runFixture, claim run.Claim) error {
				_, err := fixture.store.RenewRunAttempt(
					ctx, fixture.machineID, claim.RunID, claim.AttemptID, claim.Fence,
				)
				return err
			},
		},
		{
			name: "commit",
			run: func(ctx context.Context, fixture runFixture, claim run.Claim) error {
				return fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
					MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
					Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
					InputEndSeq: claim.InputEndSeq, Understanding: "An expired update.",
					NextStep: "This must not commit.",
				})
			},
		},
		{
			name: "finish",
			run: func(ctx context.Context, fixture runFixture, claim run.Claim) error {
				return fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
					MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
					Fence: claim.Fence, Outcome: run.StateFailed,
				})
			},
		},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRunFixture(t, ctx)
			claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
			if err != nil {
				t.Fatalf("claim Run: %v", err)
			}
			if _, err := fixture.store.pool.Exec(ctx, `
				update run_attempts
				set lease_expires_at = clock_timestamp() + interval '100 milliseconds'
				where attempt_id = $1
			`, claim.AttemptID); err != nil {
				t.Fatalf("shorten Attempt lease: %v", err)
			}
			blocker, err := fixture.store.pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin blocking transaction: %v", err)
			}
			if _, err := blocker.Exec(ctx, `
				select attempt_id from run_attempts where attempt_id = $1 for update
			`, claim.AttemptID); err != nil {
				_ = blocker.Rollback(ctx)
				t.Fatalf("lock Attempt: %v", err)
			}

			result := make(chan error, 1)
			go func() { result <- operation.run(ctx, fixture, claim) }()
			select {
			case actionErr := <-result:
				_ = blocker.Rollback(ctx)
				t.Fatalf("authority action did not wait for lock: %v", actionErr)
			case <-time.After(200 * time.Millisecond):
			}
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatalf("release Attempt lock: %v", err)
			}
			select {
			case actionErr := <-result:
				if !errors.Is(actionErr, run.ErrStaleAttempt) {
					t.Fatalf("authority after lease expiry = %v, want %v", actionErr, run.ErrStaleAttempt)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("authority action remained blocked")
			}
		})
	}
}

func TestMachineRevocationRejectsClaimAndCommit(t *testing.T) {
	t.Run("before claim", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		if err := fixture.store.RevokeMachine(ctx, fixture.bootstrap.UserID, fixture.bootstrap.SpaceID, fixture.machineID); err != nil {
			t.Fatalf("revoke Machine: %v", err)
		}
		if _, err := fixture.store.ClaimRun(ctx, fixture.machineID); !errors.Is(err, machine.ErrMachineRevoked) {
			t.Fatalf("revoked claim error = %v", err)
		}
	})

	t.Run("active Attempt", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim Run: %v", err)
		}
		if err := fixture.store.RevokeMachine(ctx, fixture.bootstrap.UserID, fixture.bootstrap.SpaceID, fixture.machineID); err != nil {
			t.Fatalf("revoke Machine: %v", err)
		}
		if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
			MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
			Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
			InputEndSeq: claim.InputEndSeq, Understanding: "Known", NextStep: "Continue",
		}); !errors.Is(err, run.ErrStaleAttempt) {
			t.Fatalf("revoked commit error = %v", err)
		}
	})
}
