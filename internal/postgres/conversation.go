package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) SendConversationMessage(
	ctx context.Context,
	command conversation.SendCommand,
) (conversation.Message, error) {
	if uuid.Validate(command.SpaceID) != nil || uuid.Validate(command.MemberUserID) != nil {
		return conversation.Message{}, errors.New("space and member identities are required")
	}
	text, err := conversation.NormalizeText(command.Text)
	if err != nil {
		return conversation.Message{}, err
	}
	idempotencyKey, err := conversation.NormalizeIdempotencyKey(command.IdempotencyKey)
	if err != nil {
		return conversation.Message{}, err
	}
	digest := conversation.MessageDigest(command.SpaceID, command.MemberUserID, text)

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return conversation.Message{}, fmt.Errorf("begin private message admission: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveConversationMembership(ctx, queries, command.SpaceID, command.MemberUserID); err != nil {
		return conversation.Message{}, err
	}
	stored, err := queries.EnsureConversation(ctx, dbsqlc.EnsureConversationParams{
		ConversationID: uuid.NewString(), SpaceID: command.SpaceID, MemberUserID: command.MemberUserID,
	})
	if err != nil {
		return conversation.Message{}, fmt.Errorf("ensure private Conversation: %w", err)
	}
	if _, err := queries.LockConversation(ctx, stored.ConversationID); err != nil {
		return conversation.Message{}, fmt.Errorf("lock private Conversation: %w", err)
	}

	existing, err := queries.FindConversationMemberRequest(ctx, dbsqlc.FindConversationMemberRequestParams{
		ConversationID: stored.ConversationID, MemberRequestID: &idempotencyKey,
	})
	if err == nil {
		if !bytes.Equal(existing.RequestDigest, digest[:]) {
			return conversation.Message{}, conversation.ErrIdempotencyConflict
		}
		result := conversation.Message{
			MessageID: existing.MessageID, Author: conversation.Author(existing.Author),
			Text: existing.Text, RequestID: stringValue(existing.MemberRequestID),
			Sequence: existing.MessageSeq, CreatedAt: existing.CreatedAt.Time,
		}
		if err := transaction.Commit(ctx); err != nil {
			return conversation.Message{}, fmt.Errorf("commit private message replay: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return conversation.Message{}, fmt.Errorf("load private message request: %w", err)
	}
	pending, err := queries.ConversationHasUnresolvedReply(ctx, stored.ConversationID)
	if err != nil {
		return conversation.Message{}, fmt.Errorf("check pending private reply: %w", err)
	}
	if pending {
		return conversation.Message{}, conversation.ErrReplyPending
	}
	sequence, err := queries.AdvanceConversationMessageHead(ctx, stored.ConversationID)
	if err != nil {
		return conversation.Message{}, fmt.Errorf("advance private message sequence: %w", err)
	}
	authorID, err := postgresUUID(command.MemberUserID)
	if err != nil {
		return conversation.Message{}, errors.New("member identity is invalid")
	}
	row, err := queries.CreateConversationMemberMessage(ctx, dbsqlc.CreateConversationMemberMessageParams{
		MessageID: uuid.NewString(), ConversationID: stored.ConversationID, MessageSeq: sequence,
		AuthorUserID: authorID, Text: text, MemberRequestID: &idempotencyKey, RequestDigest: digest[:],
	})
	if err != nil {
		return conversation.Message{}, fmt.Errorf("insert private member message: %w", err)
	}
	if err := queries.CreateConversationReplyClaim(ctx, dbsqlc.CreateConversationReplyClaimParams{
		SourceMessageID: row.MessageID, ConversationID: stored.ConversationID,
	}); err != nil {
		return conversation.Message{}, fmt.Errorf("create private reply obligation: %w", err)
	}
	result := conversation.Message{
		MessageID: row.MessageID, Author: conversation.Author(row.Author), Text: row.Text,
		RequestID: stringValue(row.MemberRequestID), Sequence: row.MessageSeq, CreatedAt: row.CreatedAt.Time,
	}
	if err := transaction.Commit(ctx); err != nil {
		return conversation.Message{}, fmt.Errorf("commit private message admission: %w", err)
	}
	return result, nil
}

func (s *Store) ListConversationMessages(
	ctx context.Context,
	command conversation.ListCommand,
) ([]conversation.Message, error) {
	if uuid.Validate(command.SpaceID) != nil || uuid.Validate(command.MemberUserID) != nil {
		return nil, errors.New("space and member identities are required")
	}
	if err := conversation.ValidateCursors(command.Before, command.After); err != nil {
		return nil, err
	}
	if (command.Before != "" && uuid.Validate(command.Before) != nil) ||
		(command.After != "" && uuid.Validate(command.After) != nil) {
		return nil, conversation.ErrInvalidCursor
	}

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin private Conversation read: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := lockActiveConversationMembership(ctx, queries, command.SpaceID, command.MemberUserID); err != nil {
		return nil, err
	}
	conversationID, err := queries.FindConversation(ctx, dbsqlc.FindConversationParams{
		SpaceID: command.SpaceID, MemberUserID: command.MemberUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if command.Before != "" || command.After != "" {
			return nil, conversation.ErrInvalidCursor
		}
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty private Conversation read: %w", err)
		}
		return []conversation.Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find private Conversation: %w", err)
	}

	messages := make([]conversation.Message, 0, conversation.MessagePageSize)
	switch {
	case command.Before != "":
		cursor, cursorErr := conversationCursor(ctx, queries, conversationID, command.Before)
		if cursorErr != nil {
			return nil, cursorErr
		}
		rows, listErr := queries.ListConversationMessagesBefore(ctx, dbsqlc.ListConversationMessagesBeforeParams{
			ConversationID: conversationID, CursorSequence: cursor,
		})
		if listErr != nil {
			return nil, fmt.Errorf("list private messages before cursor: %w", listErr)
		}
		for _, row := range rows {
			messages = append(messages, restoreConversationMessage(
				row.MessageID, row.Author, row.Text, row.MemberRequestID,
				row.CreatedWorkID, row.MessageSeq, row.CreatedAt,
			))
		}
	case command.After != "":
		cursor, cursorErr := conversationCursor(ctx, queries, conversationID, command.After)
		if cursorErr != nil {
			return nil, cursorErr
		}
		rows, listErr := queries.ListConversationMessagesAfter(ctx, dbsqlc.ListConversationMessagesAfterParams{
			ConversationID: conversationID, CursorSequence: cursor,
		})
		if listErr != nil {
			return nil, fmt.Errorf("list private messages after cursor: %w", listErr)
		}
		for _, row := range rows {
			messages = append(messages, restoreConversationMessage(
				row.MessageID, row.Author, row.Text, row.MemberRequestID,
				row.CreatedWorkID, row.MessageSeq, row.CreatedAt,
			))
		}
	default:
		rows, listErr := queries.ListNewestConversationMessages(ctx, conversationID)
		if listErr != nil {
			return nil, fmt.Errorf("list newest private messages: %w", listErr)
		}
		for _, row := range rows {
			messages = append(messages, restoreConversationMessage(
				row.MessageID, row.Author, row.Text, row.MemberRequestID,
				row.CreatedWorkID, row.MessageSeq, row.CreatedAt,
			))
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit private Conversation read: %w", err)
	}
	return messages, nil
}

func lockActiveConversationMembership(
	ctx context.Context,
	queries *dbsqlc.Queries,
	spaceID string,
	userID string,
) error {
	_, err := queries.LockActiveConversationMembership(ctx, dbsqlc.LockActiveConversationMembershipParams{
		SpaceID: spaceID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return space.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("lock active Conversation membership: %w", err)
	}
	return nil
}

func conversationCursor(
	ctx context.Context,
	queries *dbsqlc.Queries,
	conversationID string,
	messageID string,
) (int64, error) {
	sequence, err := queries.ConversationCursorSequence(ctx, dbsqlc.ConversationCursorSequenceParams{
		ConversationID: conversationID, MessageID: messageID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, conversation.ErrInvalidCursor
	}
	if err != nil {
		return 0, fmt.Errorf("load private message cursor: %w", err)
	}
	return sequence, nil
}

func restoreConversationMessage(
	messageID string,
	author string,
	text string,
	requestID *string,
	createdWorkID pgtype.UUID,
	sequence int64,
	createdAt pgtype.Timestamptz,
) conversation.Message {
	return conversation.Message{
		MessageID: messageID, Author: conversation.Author(author), Text: text,
		RequestID: stringValue(requestID), CreatedWorkID: uuidValue(createdWorkID),
		Sequence: sequence, CreatedAt: createdAt.Time,
	}
}

func postgresUUID(value string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func uuidValue(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
