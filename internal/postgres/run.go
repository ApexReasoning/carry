package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ClaimRun atomically creates or recovers a Run and its sole active Attempt.
func (s *Store) ClaimRun(ctx context.Context, machineID string) (run.Claim, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return run.Claim{}, fmt.Errorf("begin Run claim: %w", err)
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

	var claim run.Claim
	var inputStartSeq int64
	expired, recoveryErr := queries.LockExpiredRunForClaim(ctx, machine.SpaceID)
	switch {
	case recoveryErr == nil:
		rows, expireErr := queries.ExpireRunAttempt(ctx, dbsqlc.ExpireRunAttemptParams{
			AttemptID: expired.ExpiredAttemptID, RunID: expired.RunID, Fence: expired.CurrentFence,
		})
		if expireErr != nil {
			return run.Claim{}, fmt.Errorf("expire old Run Attempt: %w", expireErr)
		}
		if rows != 1 {
			return run.Claim{}, run.ErrStaleAttempt
		}
		fence, rotateErr := queries.RotateRunFence(ctx, dbsqlc.RotateRunFenceParams{
			RunID: expired.RunID, CurrentFence: expired.CurrentFence,
		})
		if rotateErr != nil {
			return run.Claim{}, fmt.Errorf("rotate Run fence: %w", rotateErr)
		}
		claim = run.Claim{
			RunID: expired.RunID, WorkID: expired.WorkID,
			Fence: fence, Goal: expired.Goal,
			CurrentUnderstanding:     textValue(expired.Understanding),
			CurrentNextStep:          textValue(expired.NextStep),
			BaseUnderstandingVersion: expired.BaseUnderstandingVersion,
			InputEndSeq:              expired.InputEndSeq,
		}
		inputStartSeq = expired.InputStartSeq
	case errors.Is(recoveryErr, pgx.ErrNoRows):
		work, workErr := queries.LockWorkForRunClaim(ctx, machine.SpaceID)
		if errors.Is(workErr, pgx.ErrNoRows) {
			return run.Claim{}, run.ErrNoRunAvailable
		}
		if workErr != nil {
			return run.Claim{}, fmt.Errorf("lock Work for Run claim: %w", workErr)
		}
		inputStartSeq = work.AppliedInputSeq + 1
		claim = run.Claim{
			RunID: uuid.NewString(), WorkID: work.WorkID, Fence: 1,
			Goal: work.Goal, CurrentUnderstanding: textValue(work.Understanding),
			CurrentNextStep:          textValue(work.NextStep),
			BaseUnderstandingVersion: work.UnderstandingVersion,
			InputEndSeq:              work.InputHeadSeq,
		}
		if _, createErr := queries.CreateRun(ctx, dbsqlc.CreateRunParams{
			RunID: claim.RunID, WorkID: claim.WorkID,
			InputStartSeq: inputStartSeq, InputEndSeq: work.InputHeadSeq,
			BaseUnderstandingVersion: work.UnderstandingVersion,
		}); createErr != nil {
			return run.Claim{}, fmt.Errorf("create Run: %w", createErr)
		}
	default:
		return run.Claim{}, fmt.Errorf("lock expired Run: %w", recoveryErr)
	}

	claim.AttemptID = uuid.NewString()
	lease, err := queries.CreateRunAttempt(ctx, dbsqlc.CreateRunAttemptParams{
		AttemptID: claim.AttemptID, RunID: claim.RunID, MachineID: machineID, Fence: claim.Fence,
	})
	if err != nil {
		return run.Claim{}, fmt.Errorf("create Run Attempt: %w", err)
	}
	claim.LeaseExpiresAt = lease.Time

	messageRows, err := queries.ListRunInputMessages(ctx, dbsqlc.ListRunInputMessagesParams{
		WorkID: claim.WorkID, InputStartSeq: inputStartSeq, InputEndSeq: claim.InputEndSeq,
	})
	if err != nil {
		return run.Claim{}, fmt.Errorf("load Run messages: %w", err)
	}
	claim.Messages = make([]run.Message, 0, len(messageRows))
	for _, message := range messageRows {
		claim.Messages = append(claim.Messages, run.Message{
			AuthorUserID: message.AuthorUserID, Text: message.Text,
		})
	}

	if err := transaction.Commit(ctx); err != nil {
		return run.Claim{}, fmt.Errorf("commit Run claim: %w", err)
	}
	return claim, nil
}

// RenewRunAttempt renews only an unexpired current Attempt owned by the Machine.
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
	if _, err := queries.LockAttemptForRenew(ctx, dbsqlc.LockAttemptForRenewParams{
		RunID: runID, AttemptID: attemptID, ClaimingMachineID: machineID, Fence: fence,
	}); errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, run.ErrStaleAttempt
	} else if err != nil {
		return time.Time{}, fmt.Errorf("lock Run Attempt for renewal: %w", err)
	}
	lease, err := queries.ExtendRunAttemptLease(ctx, dbsqlc.ExtendRunAttemptLeaseParams{
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
	return lease.Time, nil
}

func (s *Store) CommitWorkUnderstanding(ctx context.Context, command run.CommitCommand) error {
	understanding, nextStep, err := run.ValidateUnderstandingUpdate(command.Understanding, command.NextStep)
	if err != nil {
		return err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Work understanding commit: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	locked, err := queries.LockAttemptForCommit(ctx, dbsqlc.LockAttemptForCommitParams{
		RunID: command.RunID, AttemptID: command.AttemptID,
		ClaimingMachineID: command.MachineID, Fence: command.Fence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ErrStaleAttempt
	}
	if err != nil {
		return fmt.Errorf("lock Attempt for commit: %w", err)
	}
	leaseCurrent, err := queries.RunAttemptLeaseIsCurrent(ctx, command.AttemptID)
	if err != nil {
		return fmt.Errorf("check Run Attempt lease for commit: %w", err)
	}
	if !leaseCurrent {
		return run.ErrStaleAttempt
	}
	if locked.BaseUnderstandingVersion != command.BaseUnderstandingVersion ||
		locked.InputEndSeq != command.InputEndSeq ||
		locked.UnderstandingVersion != locked.BaseUnderstandingVersion ||
		locked.AppliedInputSeq != locked.InputStartSeq-1 ||
		locked.InputHeadSeq < locked.InputEndSeq ||
		locked.Lifecycle != "open" {
		return run.ErrStaleAttempt
	}

	rows, err := queries.CommitCurrentUnderstanding(ctx, dbsqlc.CommitCurrentUnderstandingParams{
		Understanding: &understanding, NextStep: &nextStep,
		AppliedInputSeq: locked.InputEndSeq, WorkID: locked.WorkID,
		ExpectedAppliedInputSeq:      locked.InputStartSeq - 1,
		ExpectedUnderstandingVersion: locked.BaseUnderstandingVersion,
	})
	if err != nil {
		return fmt.Errorf("commit Work understanding: %w", err)
	}
	if rows != 1 {
		return run.ErrStaleAttempt
	}
	attempts, err := queries.SucceedRunAttempt(ctx, command.AttemptID)
	if err != nil {
		return fmt.Errorf("succeed Run Attempt: %w", err)
	}
	runs, err := queries.SucceedRun(ctx, command.RunID)
	if err != nil {
		return fmt.Errorf("succeed Run: %w", err)
	}
	if attempts != 1 || runs != 1 {
		return run.ErrStaleAttempt
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Work understanding transaction: %w", err)
	}
	return nil
}

func (s *Store) FinishUnresolvedAttempt(ctx context.Context, command run.FinishCommand) error {
	if err := run.ValidateUnresolvedOutcome(command.Outcome); err != nil {
		return err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unresolved Attempt finish: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	if _, err := queries.LockAttemptForFinish(ctx, dbsqlc.LockAttemptForFinishParams{
		RunID: command.RunID, AttemptID: command.AttemptID,
		ClaimingMachineID: command.MachineID, Fence: command.Fence,
	}); errors.Is(err, pgx.ErrNoRows) {
		return run.ErrStaleAttempt
	} else if err != nil {
		return fmt.Errorf("lock Attempt for finish: %w", err)
	}
	leaseCurrent, err := queries.RunAttemptLeaseIsCurrent(ctx, command.AttemptID)
	if err != nil {
		return fmt.Errorf("check Run Attempt lease for finish: %w", err)
	}
	if !leaseCurrent {
		return run.ErrStaleAttempt
	}
	attempts, err := queries.FinishRunAttempt(ctx, dbsqlc.FinishRunAttemptParams{
		Outcome: string(command.Outcome), AttemptID: command.AttemptID,
	})
	if err != nil {
		return fmt.Errorf("finish Run Attempt: %w", err)
	}
	runs, err := queries.FinishRun(ctx, dbsqlc.FinishRunParams{
		Outcome: string(command.Outcome), RunID: command.RunID,
	})
	if err != nil {
		return fmt.Errorf("finish Run: %w", err)
	}
	if attempts != 1 || runs != 1 {
		return run.ErrStaleAttempt
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit unresolved Attempt finish: %w", err)
	}
	return nil
}

// RequestWorkRetry records an active member's explicit permission to create a fresh Run.
func (s *Store) RequestWorkRetry(ctx context.Context, command work.RetryCommand) error {
	if strings.TrimSpace(command.WorkID) == "" || strings.TrimSpace(command.SpaceID) == "" ||
		strings.TrimSpace(command.RequestedBy) == "" {
		return errors.New("work, space, and requesting member are required")
	}
	if err := work.ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Work retry request: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveWorkMembership(ctx, queries, command.SpaceID, command.RequestedBy); err != nil {
		return err
	}
	lifecycle, err := queries.LockWorkForRetry(ctx, dbsqlc.LockWorkForRetryParams{
		WorkID: command.WorkID, SpaceID: command.SpaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return work.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock Work for retry: %w", err)
	}
	if work.Lifecycle(lifecycle) != work.LifecycleOpen {
		return work.ErrNotOpen
	}

	existingRequester, err := queries.FindRunRetryByIdempotency(ctx, dbsqlc.FindRunRetryByIdempotencyParams{
		WorkID: command.WorkID, RetryIdempotencyKey: command.IdempotencyKey,
	})
	if err == nil {
		if existingRequester != command.RequestedBy {
			return work.ErrIdempotencyConflict
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit idempotent Work retry: %w", err)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load idempotent Work retry: %w", err)
	}

	retryableRunID, err := queries.LockRetryableRun(ctx, command.WorkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return work.ErrRetryNotNeeded
	}
	if err != nil {
		return fmt.Errorf("lock retryable Run: %w", err)
	}
	rows, err := queries.RequestRunRetry(ctx, dbsqlc.RequestRunRetryParams{
		RequestedByUserID: command.RequestedBy, RetryIdempotencyKey: command.IdempotencyKey,
		RunID: retryableRunID,
	})
	if err != nil {
		return fmt.Errorf("request Run retry: %w", err)
	}
	if rows != 1 {
		return work.ErrRetryNotNeeded
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Work retry request: %w", err)
	}
	return nil
}

func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
