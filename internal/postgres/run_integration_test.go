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

func TestCoordinatorAndClaimHaveOneConcurrentWinnerAndFixedInputRange(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	message, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "Finance approved a twelve month term",
		IdempotencyKey: "renewal-finance-input",
	})
	if err != nil {
		t.Fatalf("append fixed input: %v", err)
	}

	const callers = 8
	createdRuns := make(chan run.Coordinator, callers)
	creationErrors := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			created, createErr := fixture.store.CreateCoordinatorRun(ctx)
			if createErr != nil {
				creationErrors <- createErr
				return
			}
			createdRuns <- created
		}()
	}
	wait.Wait()
	close(createdRuns)
	close(creationErrors)
	if len(createdRuns) != 1 {
		t.Fatalf("coordinator winners = %d, want 1", len(createdRuns))
	}
	for createErr := range creationErrors {
		if !errors.Is(createErr, run.ErrNoCoordinatorNeeded) {
			t.Fatalf("concurrent coordinator error = %v", createErr)
		}
	}
	coordinator := <-createdRuns
	if coordinator.InputStartSeq != 1 || coordinator.InputEndSeq != message.InputSeq || coordinator.BaseRevision != 0 {
		t.Fatalf("fixed coordinator range = %#v", coordinator)
	}

	claims := make(chan run.Claim, callers)
	claimErrors := make(chan error, callers)
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			claim, claimErr := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID)
			if claimErr != nil {
				claimErrors <- claimErr
				return
			}
			claims <- claim
		}()
	}
	wait.Wait()
	close(claims)
	close(claimErrors)
	if len(claims) != 1 {
		t.Fatalf("claim winners = %d, want 1", len(claims))
	}
	for claimErr := range claimErrors {
		if !errors.Is(claimErr, run.ErrNoPendingRun) {
			t.Fatalf("concurrent claim error = %v", claimErr)
		}
	}
	claim := <-claims
	if claim.Fence != 1 || claim.AgentCredential == "" || claim.WriterToken == "" {
		t.Fatalf("claim authority = %#v", claim)
	}

	lateMessage, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "Legal requested one wording change",
		IdempotencyKey: "renewal-late-input",
	})
	if err != nil {
		t.Fatalf("append input after claim: %v", err)
	}
	attemptContext, err := fixture.store.LoadAttemptContext(ctx, claim.RunID, claim.AttemptID, claim.Fence, claim.AgentCredential)
	if err != nil {
		t.Fatalf("load fixed Attempt context: %v", err)
	}
	if len(attemptContext.Inputs) != 2 || attemptContext.Inputs[0].Kind != run.InputGoal ||
		attemptContext.Inputs[1].Sequence != message.InputSeq || attemptContext.InputEndSeq >= lateMessage.InputSeq {
		t.Fatalf("Attempt inputs are not fixed: %#v", attemptContext)
	}

	var storedDigest []byte
	if err := fixture.store.pool.QueryRow(ctx,
		`select agent_credential_digest from run_attempts where attempt_id = $1`, claim.AttemptID,
	).Scan(&storedDigest); err != nil {
		t.Fatalf("load stored Agent credential digest: %v", err)
	}
	if string(storedDigest) == claim.AgentCredential || len(storedDigest) != 32 {
		t.Fatalf("stored credential is not a 32 byte digest: %x", storedDigest)
	}
}

func TestWorkUnderstandingCommitRejectsEveryStaleAuthorityAndAdvancesExactRange(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "The renewal owner prefers option B",
		IdempotencyKey: "renewal-option-input",
	}); err != nil {
		t.Fatalf("append commit input: %v", err)
	}
	coordinator, err := fixture.store.CreateCoordinatorRun(ctx)
	if err != nil {
		t.Fatalf("create coordinator: %v", err)
	}
	claim, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim coordinator: %v", err)
	}
	if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: fixture.bootstrap.UserID, Text: "A later pricing note must remain pending",
		IdempotencyKey: "renewal-pending-input",
	}); err != nil {
		t.Fatalf("append later input: %v", err)
	}

	valid := run.CommitCommand{
		RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: claim.Fence,
		WriterToken: claim.WriterToken, AgentCredential: claim.AgentCredential,
		BaseRevision: coordinator.BaseRevision, InputEndSeq: coordinator.InputEndSeq,
		Understanding: "The owner selected option B after finance approval.",
		NextStep:      "Incorporate legal wording and preserve the later pricing note for the next pass.",
	}
	staleCases := []struct {
		name   string
		change func(*run.CommitCommand)
	}{
		{name: "credential", change: func(command *run.CommitCommand) { command.AgentCredential += "stale" }},
		{name: "fence", change: func(command *run.CommitCommand) { command.Fence++ }},
		{name: "writer", change: func(command *run.CommitCommand) { command.WriterToken = uuid.NewString() }},
		{name: "base revision", change: func(command *run.CommitCommand) { command.BaseRevision++ }},
		{name: "input bound", change: func(command *run.CommitCommand) { command.InputEndSeq++ }},
	}
	for _, testCase := range staleCases {
		t.Run(testCase.name, func(t *testing.T) {
			command := valid
			testCase.change(&command)
			if err := fixture.store.CommitWorkUnderstanding(ctx, command); !errors.Is(err, run.ErrStaleAttempt) {
				t.Fatalf("stale commit error = %v", err)
			}
		})
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, valid); err != nil {
		t.Fatalf("commit current understanding: %v", err)
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, valid); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("late repeat commit error = %v", err)
	}

	details, err := fixture.store.LoadWork(ctx, fixture.bootstrap.UserID, fixture.bootstrap.SpaceID, fixture.work.WorkID)
	if err != nil {
		t.Fatalf("load revised Work: %v", err)
	}
	if details.Work.AppliedInputSeq != coordinator.InputEndSeq || details.Work.InputHeadSeq != coordinator.InputEndSeq+1 ||
		details.Work.CurrentRevision != 1 || details.Work.Understanding != valid.Understanding || details.Work.NextStep != valid.NextStep {
		t.Fatalf("revised Work = %#v", details.Work)
	}
	var revisions int
	if err := fixture.store.pool.QueryRow(ctx,
		`select count(*) from work_understanding_revisions where work_id = $1`, fixture.work.WorkID,
	).Scan(&revisions); err != nil {
		t.Fatalf("count Work revisions: %v", err)
	}
	if revisions != 1 {
		t.Fatalf("Work revisions = %d, want 1", revisions)
	}

	next, err := fixture.store.CreateCoordinatorRun(ctx)
	if err != nil {
		t.Fatalf("create coordinator for later input: %v", err)
	}
	if next.InputStartSeq != coordinator.InputEndSeq+1 || next.InputEndSeq != details.Work.InputHeadSeq || next.BaseRevision != 1 {
		t.Fatalf("next coordinator range = %#v", next)
	}
	nextClaim, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim next coordinator: %v", err)
	}
	nextContext, err := fixture.store.LoadAttemptContext(
		ctx,
		nextClaim.RunID,
		nextClaim.AttemptID,
		nextClaim.Fence,
		nextClaim.AgentCredential,
	)
	if err != nil {
		t.Fatalf("load next Attempt context: %v", err)
	}
	if nextContext.CurrentUnderstanding != valid.Understanding || nextContext.CurrentNextStep != valid.NextStep {
		t.Fatalf("next Attempt base understanding = %q / %q", nextContext.CurrentUnderstanding, nextContext.CurrentNextStep)
	}
}

func TestFailedAndUnknownRunsBlockDuplicateCoordination(t *testing.T) {
	for _, outcome := range []run.State{run.StateFailed, run.StateUnknown} {
		t.Run(string(outcome), func(t *testing.T) {
			ctx := context.Background()
			fixture := newRunFixture(t, ctx)
			coordinator, err := fixture.store.CreateCoordinatorRun(ctx)
			if err != nil {
				t.Fatalf("create coordinator: %v", err)
			}
			claim, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID)
			if err != nil {
				t.Fatalf("claim coordinator: %v", err)
			}
			if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
				RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: claim.Fence,
				WriterToken: claim.WriterToken, AgentCredential: claim.AgentCredential, Outcome: outcome,
			}); err != nil {
				t.Fatalf("finish %s Attempt: %v", outcome, err)
			}
			if _, err := fixture.store.CreateCoordinatorRun(ctx); !errors.Is(err, run.ErrNoCoordinatorNeeded) {
				t.Fatalf("duplicate coordinator after %s = %v", outcome, err)
			}
			if _, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID); !errors.Is(err, run.ErrNoPendingRun) {
				t.Fatalf("claim after %s = %v", outcome, err)
			}
			var state string
			if err := fixture.store.pool.QueryRow(ctx,
				`select state from coordinator_runs where run_id = $1`, coordinator.RunID,
			).Scan(&state); err != nil {
				t.Fatalf("load unresolved Run state: %v", err)
			}
			if state != string(outcome) {
				t.Fatalf("Run state = %q, want %q", state, outcome)
			}
		})
	}
}

func TestRunAttemptRenewalRequiresCurrentMachineAndFence(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	if _, err := fixture.store.CreateCoordinatorRun(ctx); err != nil {
		t.Fatalf("create coordinator: %v", err)
	}
	claim, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim coordinator: %v", err)
	}
	leaseExpiresAt, err := fixture.store.RenewRunAttempt(
		ctx,
		fixture.machineID,
		claim.RunID,
		claim.AttemptID,
		claim.Fence,
	)
	if err != nil {
		t.Fatalf("renew current Attempt: %v", err)
	}
	if !leaseExpiresAt.After(claim.LeaseExpiresAt) {
		t.Fatalf("renewed lease = %s, initial = %s", leaseExpiresAt, claim.LeaseExpiresAt)
	}
	if _, err := fixture.store.RenewRunAttempt(
		ctx,
		fixture.machineID,
		claim.RunID,
		claim.AttemptID,
		claim.Fence+1,
	); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("wrong fence renewal error = %v", err)
	}

	otherMachineID := uuid.NewString()
	if _, err := fixture.store.EnrollMachine(ctx, host.EnrollMachineCommand{
		MachineID: otherMachineID, SpaceID: fixture.bootstrap.SpaceID, DisplayName: "Other Run Host",
		PublicKeyDER: []byte("other-run-public-key"), CertificatePEM: []byte("other-run-certificate"),
		CertificateSerial: uuid.NewString(), EnrolledByUserID: fixture.bootstrap.UserID,
		IdempotencyKey: "enroll-other-run-host",
	}); err != nil {
		t.Fatalf("enroll other Machine: %v", err)
	}
	if _, err := fixture.store.RenewRunAttempt(
		ctx,
		otherMachineID,
		claim.RunID,
		claim.AttemptID,
		claim.Fence,
	); !errors.Is(err, run.ErrStaleAttempt) {
		t.Fatalf("wrong Machine renewal error = %v", err)
	}
}

func TestMachineRevocationRejectsClaimAndActiveAttemptCommit(t *testing.T) {
	t.Run("before claim", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		if _, err := fixture.store.CreateCoordinatorRun(ctx); err != nil {
			t.Fatalf("create coordinator: %v", err)
		}
		if _, err := fixture.store.pool.Exec(ctx,
			`update machines set revoked_at = transaction_timestamp() where machine_id = $1`, fixture.machineID,
		); err != nil {
			t.Fatalf("revoke Machine: %v", err)
		}
		if _, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID); !errors.Is(err, host.ErrMachineRevoked) {
			t.Fatalf("revoked Machine claim error = %v", err)
		}
	})

	t.Run("after claim", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		coordinator, err := fixture.store.CreateCoordinatorRun(ctx)
		if err != nil {
			t.Fatalf("create coordinator: %v", err)
		}
		claim, err := fixture.store.ClaimCoordinatorRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim coordinator: %v", err)
		}
		if _, err := fixture.store.pool.Exec(ctx,
			`update machines set revoked_at = transaction_timestamp() where machine_id = $1`, fixture.machineID,
		); err != nil {
			t.Fatalf("revoke active Machine: %v", err)
		}
		if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
			RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: claim.Fence,
			WriterToken: claim.WriterToken, AgentCredential: claim.AgentCredential,
			BaseRevision: coordinator.BaseRevision, InputEndSeq: coordinator.InputEndSeq,
			Understanding: "This must not commit.", NextStep: "This must remain pending.",
		}); !errors.Is(err, run.ErrStaleAttempt) {
			t.Fatalf("revoked Machine commit error = %v", err)
		}
	})
}
