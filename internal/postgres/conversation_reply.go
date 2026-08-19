package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ClaimConversationReply grants one Machine a bounded fixed private context.
func (s *Store) ClaimConversationReply(ctx context.Context, machineID string) (conversation.ReplyClaim, error) {
	if uuid.Validate(machineID) != nil {
		return conversation.ReplyClaim{}, host.ErrMachineNotFound
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return conversation.ReplyClaim{}, fmt.Errorf("begin private reply claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	spaceID, err := lockActiveReplyMachine(ctx, queries, machineID)
	if err != nil {
		return conversation.ReplyClaim{}, err
	}
	locked, err := queries.LockConversationReplyForClaim(ctx, spaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ReplyClaim{}, conversation.ErrNoReplyAvailable
	}
	if err != nil {
		return conversation.ReplyClaim{}, fmt.Errorf("lock private reply for claim: %w", err)
	}

	contextStart, contextEnd, err := fixedReplyContextRange(ctx, queries, locked)
	if err != nil {
		return conversation.ReplyClaim{}, err
	}
	machineUUID, err := postgresUUID(machineID)
	if err != nil {
		return conversation.ReplyClaim{}, host.ErrMachineNotFound
	}
	assignment, err := queries.AssignConversationReply(ctx, dbsqlc.AssignConversationReplyParams{
		MachineID: machineUUID, ContextStartSeq: &contextStart, ContextEndSeq: &contextEnd,
		SourceMessageID: locked.SourceMessageID, ExpectedFence: locked.CurrentFence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.ReplyClaim{}, conversation.ErrNoReplyAvailable
	}
	if err != nil {
		return conversation.ReplyClaim{}, fmt.Errorf("assign private reply claim: %w", err)
	}
	messages, err := loadFixedReplyContext(ctx, queries, locked.ConversationID, contextStart, contextEnd)
	if err != nil {
		return conversation.ReplyClaim{}, err
	}
	claim := conversation.ReplyClaim{
		SourceMessageID: locked.SourceMessageID,
		Fence:           assignment.CurrentFence,
		LeaseExpiresAt:  assignment.LeaseExpiresAt.Time,
		Messages:        messages,
	}
	if err := transaction.Commit(ctx); err != nil {
		return conversation.ReplyClaim{}, fmt.Errorf("commit private reply claim: %w", err)
	}
	return claim, nil
}

// RenewConversationReply extends only the exact current unexpired private claim.
func (s *Store) RenewConversationReply(
	ctx context.Context,
	command conversation.RenewReplyCommand,
) (time.Time, error) {
	if uuid.Validate(command.MachineID) != nil || uuid.Validate(command.SourceMessageID) != nil || command.Fence <= 0 {
		return time.Time{}, conversation.ErrStaleReplyClaim
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin private reply renewal: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	spaceID, err := lockActiveReplyMachine(ctx, queries, command.MachineID)
	if err != nil {
		return time.Time{}, err
	}
	machineID, err := postgresUUID(command.MachineID)
	if err != nil {
		return time.Time{}, conversation.ErrStaleReplyClaim
	}
	if _, err := queries.LockConversationReplyForRenew(ctx, dbsqlc.LockConversationReplyForRenewParams{
		SourceMessageID: command.SourceMessageID, SpaceID: spaceID,
		MachineID: machineID, Fence: command.Fence,
	}); errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, conversation.ErrStaleReplyClaim
	} else if err != nil {
		return time.Time{}, fmt.Errorf("lock private reply renewal: %w", err)
	}
	lease, err := queries.ExtendConversationReplyLease(ctx, dbsqlc.ExtendConversationReplyLeaseParams{
		SourceMessageID: command.SourceMessageID, MachineID: machineID, Fence: command.Fence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, conversation.ErrStaleReplyClaim
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("extend private reply lease: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit private reply renewal: %w", err)
	}
	return lease.Time, nil
}

// CommitConversationReply atomically records one private reply and its optional
// shared Work consequence. Exact completed replays return the original result.
func (s *Store) CommitConversationReply(
	ctx context.Context,
	command conversation.CommitReplyCommand,
) (conversation.CommitReplyResult, error) {
	if uuid.Validate(command.MachineID) != nil || uuid.Validate(command.SourceMessageID) != nil || command.Fence <= 0 {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	candidate, outputDigest, err := conversation.NormalizeReplyCandidate(command.SourceMessageID, command.Candidate)
	if err != nil {
		return conversation.CommitReplyResult{}, err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("begin private reply commit: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	spaceID, err := lockActiveReplyMachine(ctx, queries, command.MachineID)
	if err != nil {
		return conversation.CommitReplyResult{}, err
	}
	locked, err := queries.LockConversationReplyForCommit(ctx, dbsqlc.LockConversationReplyForCommitParams{
		SourceMessageID: command.SourceMessageID, SpaceID: spaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	if err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("lock private reply commit: %w", err)
	}
	if uuidValue(locked.CurrentMachineID) != command.MachineID || locked.CurrentFence != command.Fence {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	if locked.CommittedReplyMessageID.Valid {
		if !bytes.Equal(locked.OutputDigest, outputDigest[:]) {
			return conversation.CommitReplyResult{}, conversation.ErrReplyConflict
		}
		result := conversation.CommitReplyResult{
			ReplyMessageID: uuidValue(locked.CommittedReplyMessageID),
			CreatedWorkID:  uuidValue(locked.CreatedWorkID),
		}
		if err := transaction.Commit(ctx); err != nil {
			return conversation.CommitReplyResult{}, fmt.Errorf("commit private reply replay: %w", err)
		}
		return result, nil
	}
	if locked.MessageHeadSeq != locked.SourceMessageSeq {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	leaseCurrent, err := queries.ConversationReplyLeaseIsCurrent(ctx, command.SourceMessageID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !leaseCurrent) {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	if err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("check private reply lease: %w", err)
	}

	createdWorkID := ""
	if candidate.DelegationGoal != nil {
		createdWorkID, err = createDelegatedWork(ctx, queries, locked.SpaceID, locked.MemberUserID, *candidate.DelegationGoal)
		if err != nil {
			return conversation.CommitReplyResult{}, err
		}
	}
	replySequence, err := queries.AdvanceConversationMessageHead(ctx, locked.ConversationID)
	if err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("advance private reply sequence: %w", err)
	}
	if replySequence != locked.SourceMessageSeq+1 {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	replyMessageID := uuid.NewString()
	sourceMessageID, err := postgresUUID(command.SourceMessageID)
	if err != nil {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	if _, err := queries.CreateConversationCarryReply(ctx, dbsqlc.CreateConversationCarryReplyParams{
		MessageID: replyMessageID, ConversationID: locked.ConversationID,
		MessageSeq: replySequence, Text: candidate.Reply, SourceMessageID: sourceMessageID,
	}); err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("insert private Carry reply: %w", err)
	}
	machineID, err := postgresUUID(command.MachineID)
	if err != nil {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	replyID, err := postgresUUID(replyMessageID)
	if err != nil {
		return conversation.CommitReplyResult{}, errors.New("generated private reply identity is invalid")
	}
	createdWorkUUID := pgtype.UUID{}
	if createdWorkID != "" {
		createdWorkUUID, err = postgresUUID(createdWorkID)
		if err != nil {
			return conversation.CommitReplyResult{}, errors.New("generated delegated Work identity is invalid")
		}
	}
	rows, err := queries.CompleteConversationReply(ctx, dbsqlc.CompleteConversationReplyParams{
		OutputDigest: outputDigest[:], ReplyMessageID: replyID,
		CreatedWorkID: createdWorkUUID, SourceMessageID: command.SourceMessageID,
		MachineID: machineID, Fence: command.Fence,
	})
	if err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("complete private reply claim: %w", err)
	}
	if rows != 1 {
		return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
	}
	result := conversation.CommitReplyResult{ReplyMessageID: replyMessageID, CreatedWorkID: createdWorkID}
	if err := transaction.Commit(ctx); err != nil {
		return conversation.CommitReplyResult{}, fmt.Errorf("commit private reply transaction: %w", err)
	}
	return result, nil
}

func lockActiveReplyMachine(ctx context.Context, queries *dbsqlc.Queries, machineID string) (string, error) {
	machine, err := queries.LockClaimingMachine(ctx, machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", host.ErrMachineNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock private reply Machine: %w", err)
	}
	if machine.RevokedAt.Valid {
		return "", host.ErrMachineRevoked
	}
	return machine.SpaceID, nil
}

func fixedReplyContextRange(
	ctx context.Context,
	queries *dbsqlc.Queries,
	locked dbsqlc.LockConversationReplyForClaimRow,
) (int64, int64, error) {
	if locked.ContextStartSeq != nil || locked.ContextEndSeq != nil {
		if locked.ContextStartSeq == nil || locked.ContextEndSeq == nil || *locked.ContextEndSeq != locked.SourceMessageSeq {
			return 0, 0, conversation.ErrInvalidContext
		}
		return *locked.ContextStartSeq, *locked.ContextEndSeq, nil
	}
	rows, err := queries.ListConversationContextCandidates(ctx, dbsqlc.ListConversationContextCandidatesParams{
		ConversationID: locked.ConversationID, ContextEndSeq: locked.SourceMessageSeq,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("load private reply context candidates: %w", err)
	}
	candidates := make([]conversation.ContextMessage, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, conversation.ContextMessage{Author: conversation.Author(row.Author), Text: row.Text})
	}
	fixed, err := conversation.FixedContextSuffix(candidates)
	if err != nil {
		return 0, 0, err
	}
	startIndex := len(rows) - len(fixed)
	return rows[startIndex].MessageSeq, locked.SourceMessageSeq, nil
}

func loadFixedReplyContext(
	ctx context.Context,
	queries *dbsqlc.Queries,
	conversationID string,
	contextStart int64,
	contextEnd int64,
) ([]conversation.ContextMessage, error) {
	rows, err := queries.ListFixedConversationReplyContext(ctx, dbsqlc.ListFixedConversationReplyContextParams{
		ConversationID: conversationID, ContextStartSeq: contextStart, ContextEndSeq: contextEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("load fixed private reply context: %w", err)
	}
	messages := make([]conversation.ContextMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, conversation.ContextMessage{Author: conversation.Author(row.Author), Text: row.Text})
	}
	fixed, err := conversation.FixedContextSuffix(messages)
	if err != nil || len(fixed) != len(messages) {
		return nil, conversation.ErrInvalidContext
	}
	return messages, nil
}

func createDelegatedWork(
	ctx context.Context,
	queries *dbsqlc.Queries,
	spaceID string,
	memberUserID string,
	goal string,
) (string, error) {
	workID := uuid.NewString()
	idempotencyKey := uuid.NewString()
	digest := work.CreateDigest(spaceID, memberUserID, goal)
	row, err := queries.CreateWork(ctx, dbsqlc.CreateWorkParams{
		WorkID: workID, SpaceID: spaceID, Goal: goal,
		OwnerUserID: memberUserID, CreatorUserID: memberUserID,
		CreateIdempotencyKey: idempotencyKey, CreateRequestDigest: digest[:],
	})
	if err != nil {
		return "", fmt.Errorf("create delegated Work: %w", err)
	}
	if row.WorkID != workID {
		return "", errors.New("delegated Work identity mismatch")
	}
	return workID, nil
}
