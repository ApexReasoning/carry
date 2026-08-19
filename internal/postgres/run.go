package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateCoordinatorRun(ctx context.Context) (run.Coordinator, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return run.Coordinator{}, fmt.Errorf("begin coordinator creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	candidate, err := queries.LockWorkForCoordination(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Coordinator{}, run.ErrNoCoordinatorNeeded
	}
	if err != nil {
		return run.Coordinator{}, fmt.Errorf("lock Work for coordination: %w", err)
	}
	row, err := queries.CreateCoordinatorRun(ctx, dbsqlc.CreateCoordinatorRunParams{
		RunID: uuid.NewString(), WorkID: candidate.WorkID,
		InputStartSeq: candidate.AppliedInputSeq + 1, InputEndSeq: candidate.InputHeadSeq,
		BaseRevision: candidate.CurrentRevision, WriterToken: uuid.NewString(),
	})
	if err != nil {
		return run.Coordinator{}, fmt.Errorf("insert coordinator Run: %w", err)
	}
	result := coordinatorFromRow(row, candidate.SpaceID)
	if err := transaction.Commit(ctx); err != nil {
		return run.Coordinator{}, fmt.Errorf("commit coordinator creation: %w", err)
	}
	return result, nil
}

func (s *Store) ClaimCoordinatorRun(ctx context.Context, machineID string) (run.Claim, error) {
	credential, err := run.NewAgentCredential()
	if err != nil {
		return run.Claim{}, err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return run.Claim{}, fmt.Errorf("begin coordinator claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	machine, err := queries.LockClaimingMachine(ctx, machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Claim{}, host.ErrMachineNotFound
	}
	if err != nil {
		return run.Claim{}, fmt.Errorf("lock claiming Machine: %w", err)
	}
	if machine.RevokedAt.Valid {
		return run.Claim{}, host.ErrMachineRevoked
	}
	pending, err := queries.LockPendingCoordinatorRun(ctx, machine.SpaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Claim{}, run.ErrNoPendingRun
	}
	if err != nil {
		return run.Claim{}, fmt.Errorf("lock pending coordinator Run: %w", err)
	}
	fence, err := queries.ActivateCoordinatorRun(ctx, pending.RunID)
	if err != nil {
		return run.Claim{}, fmt.Errorf("activate coordinator Run: %w", err)
	}
	attempt, err := queries.CreateRunAttempt(ctx, dbsqlc.CreateRunAttemptParams{
		AttemptID: uuid.NewString(), RunID: pending.RunID, MachineID: machineID,
		Fence: fence, AgentCredentialDigest: credential.Digest[:],
	})
	if err != nil {
		return run.Claim{}, fmt.Errorf("insert Run Attempt: %w", err)
	}
	claim := run.Claim{
		Coordinator: run.Coordinator{
			RunID: pending.RunID, WorkID: pending.WorkID, SpaceID: pending.SpaceID,
			InputStartSeq: pending.InputStartSeq, InputEndSeq: pending.InputEndSeq,
			BaseRevision: pending.BaseRevision, State: run.StateActive,
			CreatedAt: pending.CreatedAt.Time,
		},
		AttemptID: attempt.AttemptID, Fence: attempt.Fence, WriterToken: pending.WriterToken,
		AgentCredential: credential.Secret, LeaseExpiresAt: attempt.LeaseExpiresAt.Time,
	}
	if err := transaction.Commit(ctx); err != nil {
		return run.Claim{}, fmt.Errorf("commit coordinator claim: %w", err)
	}
	return claim, nil
}

// RenewRunAttempt extends only the current fenced Attempt owned by an active Machine.
func (s *Store) RenewRunAttempt(
	ctx context.Context,
	machineID string,
	runID string,
	attemptID string,
	fence int64,
) (time.Time, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin Run Attempt renewal: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	machine, err := queries.LockClaimingMachine(ctx, machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, host.ErrMachineNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock renewing Machine: %w", err)
	}
	if machine.RevokedAt.Valid {
		return time.Time{}, host.ErrMachineRevoked
	}
	leaseExpiresAt, err := queries.ExtendRunAttemptLease(ctx, dbsqlc.ExtendRunAttemptLeaseParams{
		AttemptID: attemptID, RunID: runID, Fence: fence, ClaimingMachineID: machineID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, run.ErrStaleAttempt
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("renew Run Attempt: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit Run Attempt renewal: %w", err)
	}
	return leaseExpiresAt.Time, nil
}

// LoadAttemptContext projects the immutable base revision and fixed input range authorized by one active Attempt.
func (s *Store) LoadAttemptContext(ctx context.Context, runID string, attemptID string, fence int64, agentCredential string) (run.Context, error) {
	digest := run.DigestAgentCredential(agentCredential)
	row, err := s.queries.LoadActiveAttemptContext(ctx, dbsqlc.LoadActiveAttemptContextParams{
		RunID: runID, AttemptID: attemptID, Fence: fence, AgentCredentialDigest: digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Context{}, run.ErrStaleAttempt
	}
	if err != nil {
		return run.Context{}, fmt.Errorf("load Attempt context: %w", err)
	}
	if row.BaseRevision == 0 && (row.CurrentUnderstanding != nil || row.CurrentNextStep != nil) {
		return run.Context{}, errors.New("initial Attempt context unexpectedly has a Work revision")
	}
	if row.BaseRevision > 0 && (row.CurrentUnderstanding == nil || row.CurrentNextStep == nil) {
		return run.Context{}, errors.New("Attempt base Work revision is missing")
	}
	messageRows, err := s.queries.ListRunInputMessages(ctx, dbsqlc.ListRunInputMessagesParams{
		WorkID: row.WorkID, InputStartSeq: row.InputStartSeq, InputEndSeq: row.InputEndSeq,
	})
	if err != nil {
		return run.Context{}, fmt.Errorf("load Attempt input messages: %w", err)
	}
	inputs := make([]run.Input, 0, len(messageRows)+1)
	if row.InputStartSeq == 1 {
		inputs = append(inputs, run.Input{Sequence: 1, Kind: run.InputGoal, Text: row.Goal})
	}
	for _, message := range messageRows {
		inputs = append(inputs, run.Input{
			Sequence: message.InputSeq, Kind: run.InputMessage,
			AuthorUserID: message.AuthorUserID, Text: message.Text,
		})
	}
	return run.Context{
		RunID: row.RunID, AttemptID: row.AttemptID, WorkID: row.WorkID, SpaceID: row.SpaceID,
		Goal: row.Goal, CurrentUnderstanding: valueOrEmpty(row.CurrentUnderstanding),
		CurrentNextStep: valueOrEmpty(row.CurrentNextStep), InputStartSeq: row.InputStartSeq,
		InputEndSeq: row.InputEndSeq, BaseRevision: row.BaseRevision, Fence: row.Fence, Inputs: inputs,
	}, nil
}

func (s *Store) CommitWorkUnderstanding(ctx context.Context, command run.CommitCommand) error {
	understanding, nextStep, err := run.ValidateDraft(command.Understanding, command.NextStep)
	if err != nil {
		return err
	}
	digest := run.DigestAgentCredential(command.AgentCredential)
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Work understanding commit: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	locked, err := queries.LockAttemptForCommit(ctx, dbsqlc.LockAttemptForCommitParams{
		RunID: command.RunID, AttemptID: command.AttemptID, Fence: command.Fence,
		WriterToken: command.WriterToken, AgentCredentialDigest: digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ErrStaleAttempt
	}
	if err != nil {
		return fmt.Errorf("lock Attempt for Work commit: %w", err)
	}
	if locked.BaseRevision != command.BaseRevision || locked.InputEndSeq != command.InputEndSeq ||
		locked.CurrentRevision != locked.BaseRevision || locked.AppliedInputSeq != locked.InputStartSeq-1 ||
		locked.InputHeadSeq < locked.InputEndSeq || locked.Lifecycle != "open" {
		return run.ErrStaleAttempt
	}
	nextRevision := locked.BaseRevision + 1
	if err := queries.CreateWorkUnderstandingRevision(ctx, dbsqlc.CreateWorkUnderstandingRevisionParams{
		WorkID: locked.WorkID, Revision: nextRevision, SourceRunID: command.RunID,
		Understanding: understanding, NextStep: nextStep, AppliedInputSeq: locked.InputEndSeq,
	}); err != nil {
		return fmt.Errorf("insert Work understanding revision: %w", err)
	}
	updated, err := queries.CommitWorkRevision(ctx, dbsqlc.CommitWorkRevisionParams{
		AppliedInputSeq: locked.InputEndSeq, CurrentRevision: nextRevision, WorkID: locked.WorkID,
		ExpectedAppliedInputSeq: locked.InputStartSeq - 1, ExpectedCurrentRevision: locked.BaseRevision,
	})
	if err != nil {
		return fmt.Errorf("advance Work revision: %w", err)
	}
	if updated != 1 {
		return run.ErrStaleAttempt
	}
	attempts, err := queries.SucceedRunAttempt(ctx, command.AttemptID)
	if err != nil {
		return fmt.Errorf("succeed Run Attempt: %w", err)
	}
	runs, err := queries.SucceedCoordinatorRun(ctx, command.RunID)
	if err != nil {
		return fmt.Errorf("succeed coordinator Run: %w", err)
	}
	if attempts != 1 || runs != 1 {
		return run.ErrStaleAttempt
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Work understanding: %w", err)
	}
	return nil
}

func (s *Store) FinishUnresolvedAttempt(ctx context.Context, command run.FinishCommand) error {
	if err := run.ValidateUnresolvedOutcome(command.Outcome); err != nil {
		return err
	}
	digest := run.DigestAgentCredential(command.AgentCredential)
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unresolved Attempt finish: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	_, err = queries.LockAttemptForCommit(ctx, dbsqlc.LockAttemptForCommitParams{
		RunID: command.RunID, AttemptID: command.AttemptID, Fence: command.Fence,
		WriterToken: command.WriterToken, AgentCredentialDigest: digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ErrStaleAttempt
	}
	if err != nil {
		return fmt.Errorf("lock unresolved Attempt: %w", err)
	}
	attempts, err := queries.FinishUnresolvedRunAttempt(ctx, dbsqlc.FinishUnresolvedRunAttemptParams{
		Outcome: string(command.Outcome), AttemptID: command.AttemptID,
	})
	if err != nil {
		return fmt.Errorf("finish unresolved Run Attempt: %w", err)
	}
	runs, err := queries.FinishUnresolvedCoordinatorRun(ctx, dbsqlc.FinishUnresolvedCoordinatorRunParams{
		Outcome: string(command.Outcome), RunID: command.RunID,
	})
	if err != nil {
		return fmt.Errorf("finish unresolved coordinator Run: %w", err)
	}
	if attempts != 1 || runs != 1 {
		return run.ErrStaleAttempt
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit unresolved Attempt finish: %w", err)
	}
	return nil
}

func coordinatorFromRow(row dbsqlc.CreateCoordinatorRunRow, spaceID string) run.Coordinator {
	return run.Coordinator{
		RunID: row.RunID, WorkID: row.WorkID, SpaceID: spaceID,
		InputStartSeq: row.InputStartSeq, InputEndSeq: row.InputEndSeq,
		BaseRevision: row.BaseRevision, State: run.State(row.State), CreatedAt: row.CreatedAt.Time,
	}
}
