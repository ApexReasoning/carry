package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateWork(ctx context.Context, command work.CreateCommand) (work.Work, error) {
	goal, err := work.NormalizeGoal(command.Goal)
	if err != nil {
		return work.Work{}, err
	}
	if strings.TrimSpace(command.SpaceID) == "" || strings.TrimSpace(command.CreatorUserID) == "" {
		return work.Work{}, errors.New("space and creator are required")
	}
	if err := work.ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return work.Work{}, err
	}
	digest := work.CreateDigest(command.SpaceID, command.CreatorUserID, goal)

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return work.Work{}, fmt.Errorf("begin work creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveWorkMembership(ctx, queries, command.SpaceID, command.CreatorUserID); err != nil {
		return work.Work{}, err
	}

	row, err := queries.CreateWork(ctx, dbsqlc.CreateWorkParams{
		WorkID: uuid.NewString(), SpaceID: command.SpaceID, Goal: goal,
		OwnerUserID: command.CreatorUserID, CreatorUserID: command.CreatorUserID,
		CreateIdempotencyKey: command.IdempotencyKey, CreateRequestDigest: digest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := queries.FindWorkByCreateIdempotency(ctx, dbsqlc.FindWorkByCreateIdempotencyParams{
			SpaceID: command.SpaceID, CreatorUserID: command.CreatorUserID,
			CreateIdempotencyKey: command.IdempotencyKey,
		})
		if loadErr != nil {
			return work.Work{}, fmt.Errorf("load idempotent work creation: %w", loadErr)
		}
		if !bytes.Equal(existing.CreateRequestDigest, digest[:]) {
			return work.Work{}, work.ErrIdempotencyConflict
		}
		result := workFromIdempotencyRow(existing)
		if err := transaction.Commit(ctx); err != nil {
			return work.Work{}, fmt.Errorf("commit idempotent work creation: %w", err)
		}
		return result, nil
	}
	if err != nil {
		return work.Work{}, fmt.Errorf("insert work: %w", err)
	}
	result := workFromCreateRow(row)
	if err := transaction.Commit(ctx); err != nil {
		return work.Work{}, fmt.Errorf("commit work creation: %w", err)
	}
	return result, nil
}

func (s *Store) ListWorks(ctx context.Context, userID string, spaceID string) ([]work.Work, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin work list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveWorkMembership(ctx, queries, spaceID, userID); err != nil {
		return nil, err
	}
	rows, err := queries.ListWorks(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("list works: %w", err)
	}
	works := make([]work.Work, 0, len(rows))
	for _, row := range rows {
		works = append(works, work.Work{
			WorkID: row.WorkID, SpaceID: row.SpaceID, Goal: row.Goal,
			Lifecycle: work.Lifecycle(row.Lifecycle), OwnerUserID: row.OwnerUserID,
			CreatorUserID: row.CreatorUserID,
			Understanding: textValue(row.Understanding), NextStep: textValue(row.NextStep),
			HasUnappliedInput: row.AppliedInputSeq < row.InputHeadSeq,
			CreatedAt:         row.CreatedAt.Time,
		})
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit work list: %w", err)
	}
	return works, nil
}

func (s *Store) LoadWork(ctx context.Context, userID string, spaceID string, workID string) (work.Details, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return work.Details{}, fmt.Errorf("begin work load: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveWorkMembership(ctx, queries, spaceID, userID); err != nil {
		return work.Details{}, err
	}
	row, err := queries.LoadWork(ctx, dbsqlc.LoadWorkParams{SpaceID: spaceID, WorkID: workID})
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Details{}, work.ErrNotFound
	}
	if err != nil {
		return work.Details{}, fmt.Errorf("load work: %w", err)
	}
	messageRows, err := queries.ListWorkMessages(ctx, workID)
	if err != nil {
		return work.Details{}, fmt.Errorf("list work messages: %w", err)
	}
	messages := make([]work.Message, 0, len(messageRows))
	for _, message := range messageRows {
		messages = append(messages, work.Message{
			MessageID: message.MessageID, WorkID: message.WorkID,
			AuthorUserID: message.AuthorUserID, Text: message.Text,
			InputSeq: message.InputSeq, CreatedAt: message.CreatedAt.Time,
		})
	}
	result := work.Details{Work: workFromLoadRow(row), Messages: messages}
	if err := transaction.Commit(ctx); err != nil {
		return work.Details{}, fmt.Errorf("commit work load: %w", err)
	}
	return result, nil
}

func (s *Store) AppendWorkMessage(ctx context.Context, command work.AppendMessageCommand) (work.Message, error) {
	if strings.TrimSpace(command.WorkID) == "" || strings.TrimSpace(command.SpaceID) == "" ||
		strings.TrimSpace(command.AuthorUserID) == "" {
		return work.Message{}, errors.New("work, space, and author are required")
	}
	if err := work.ValidateMessage(command.Text); err != nil {
		return work.Message{}, err
	}
	if err := work.ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return work.Message{}, err
	}
	digest := work.MessageDigest(command.WorkID, command.AuthorUserID, command.Text)

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return work.Message{}, fmt.Errorf("begin work message append: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveWorkMembership(ctx, queries, command.SpaceID, command.AuthorUserID); err != nil {
		return work.Message{}, err
	}
	locked, err := queries.LockWork(ctx, dbsqlc.LockWorkParams{SpaceID: command.SpaceID, WorkID: command.WorkID})
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Message{}, work.ErrNotFound
	}
	if err != nil {
		return work.Message{}, fmt.Errorf("lock work: %w", err)
	}

	existing, err := queries.FindWorkMessageByIdempotency(ctx, dbsqlc.FindWorkMessageByIdempotencyParams{
		WorkID: command.WorkID, AuthorUserID: command.AuthorUserID,
		IdempotencyKey: command.IdempotencyKey,
	})
	if err == nil {
		if !bytes.Equal(existing.RequestDigest, digest[:]) {
			return work.Message{}, work.ErrIdempotencyConflict
		}
		result := work.Message{
			MessageID: existing.MessageID, WorkID: existing.WorkID,
			AuthorUserID: existing.AuthorUserID, Text: existing.Text,
			InputSeq: existing.InputSeq, CreatedAt: existing.CreatedAt.Time,
		}
		if err := transaction.Commit(ctx); err != nil {
			return work.Message{}, fmt.Errorf("commit idempotent work message: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return work.Message{}, fmt.Errorf("load idempotent work message: %w", err)
	}
	if work.Lifecycle(locked.Lifecycle) != work.LifecycleOpen {
		return work.Message{}, work.ErrNotOpen
	}
	inputSeq, err := queries.AdvanceWorkInputHead(ctx, command.WorkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Message{}, work.ErrNotOpen
	}
	if err != nil {
		return work.Message{}, fmt.Errorf("advance work input: %w", err)
	}
	row, err := queries.CreateWorkMessage(ctx, dbsqlc.CreateWorkMessageParams{
		MessageID: uuid.NewString(), WorkID: command.WorkID,
		AuthorUserID: command.AuthorUserID, Text: command.Text, InputSeq: inputSeq,
		IdempotencyKey: command.IdempotencyKey, RequestDigest: digest[:],
	})
	if err != nil {
		return work.Message{}, fmt.Errorf("insert work message: %w", err)
	}
	result := work.Message{
		MessageID: row.MessageID, WorkID: row.WorkID, AuthorUserID: row.AuthorUserID,
		Text: row.Text, InputSeq: row.InputSeq, CreatedAt: row.CreatedAt.Time,
	}
	if err := transaction.Commit(ctx); err != nil {
		return work.Message{}, fmt.Errorf("commit work message: %w", err)
	}
	return result, nil
}

func lockActiveWorkMembership(ctx context.Context, queries *dbsqlc.Queries, spaceID string, userID string) error {
	_, err := queries.LockActiveWorkMembership(ctx, dbsqlc.LockActiveWorkMembershipParams{
		SpaceID: spaceID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return space.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("lock active work membership: %w", err)
	}
	return nil
}

func workFromCreateRow(row dbsqlc.CreateWorkRow) work.Work {
	return work.Work{
		WorkID: row.WorkID, SpaceID: row.SpaceID, Goal: row.Goal,
		Lifecycle: work.Lifecycle(row.Lifecycle), OwnerUserID: row.OwnerUserID,
		CreatorUserID:     row.CreatorUserID,
		HasUnappliedInput: true,
		CreatedAt:         row.CreatedAt.Time,
	}
}

func workFromIdempotencyRow(row dbsqlc.FindWorkByCreateIdempotencyRow) work.Work {
	return work.Work{
		WorkID: row.WorkID, SpaceID: row.SpaceID, Goal: row.Goal,
		Lifecycle: work.Lifecycle(row.Lifecycle), OwnerUserID: row.OwnerUserID,
		CreatorUserID:     row.CreatorUserID,
		HasUnappliedInput: true,
		CreatedAt:         row.CreatedAt.Time,
	}
}

func workFromLoadRow(row dbsqlc.LoadWorkRow) work.Work {
	return work.Work{
		WorkID: row.WorkID, SpaceID: row.SpaceID, Goal: row.Goal,
		Lifecycle: work.Lifecycle(row.Lifecycle), OwnerUserID: row.OwnerUserID,
		CreatorUserID: row.CreatorUserID,
		Understanding: textValue(row.Understanding), NextStep: textValue(row.NextStep),
		HasUnappliedInput: row.AppliedInputSeq < row.InputHeadSeq,
		CreatedAt:         row.CreatedAt.Time,
	}
}
