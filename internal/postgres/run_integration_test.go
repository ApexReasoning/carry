//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
)

type runFixture struct {
	store     *Store
	bootstrap BootstrapResult
	work      work.Work
	machineID string
}

func newRunFixture(t *testing.T, ctx context.Context) runFixture {
	t.Helper()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Run Owner", SpaceName: "Coordination Space", TokenExpiresAt: time.Now().Add(time.Hour),
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
	_, err = store.EnrollMachine(ctx, host.EnrollMachineCommand{
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

	details, err := fixture.store.LoadWork(ctx, fixture.bootstrap.UserID, fixture.bootstrap.SpaceID, fixture.work.WorkID)
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

func TestFailedAndUnknownRunsBlockAutomaticReplay(t *testing.T) {
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
		})
	}
}

func TestExpiredAttemptRecoversOnceAndRejectsOldAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	old, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim initial Run: %v", err)
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

	const callers = 6
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
		t.Fatalf("recovery winners = %d, want 1", len(claims))
	}
	for claimErr := range errorsSeen {
		if !errors.Is(claimErr, run.ErrNoRunAvailable) {
			t.Fatalf("losing recovery error = %v", claimErr)
		}
	}
	recovered := <-claims
	if recovered.RunID != old.RunID || recovered.AttemptID == old.AttemptID || recovered.Fence != old.Fence+1 {
		t.Fatalf("recovered claim = %#v; old = %#v", recovered, old)
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
		if _, err := fixture.store.ClaimRun(ctx, fixture.machineID); !errors.Is(err, host.ErrMachineRevoked) {
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
