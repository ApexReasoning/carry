//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
)

func TestConversationAdmissionIsIdempotentAndSerial(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
		DisplayName: "Nora", SpaceName: "Renewal Team", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	command := conversation.SendCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
		Text: "How should I prepare the renewal?", IdempotencyKey: "private-renewal-question",
	}

	const callers = 8
	results := make(chan conversation.Message, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			message, sendErr := store.SendConversationMessage(ctx, command)
			if sendErr != nil {
				errorsFound <- sendErr
				return
			}
			results <- message
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for sendErr := range errorsFound {
		t.Fatalf("send private message: %v", sendErr)
	}

	var messageID string
	for message := range results {
		if messageID == "" {
			messageID = message.MessageID
		}
		if message.MessageID != messageID || message.Sequence != 1 || message.Author != conversation.AuthorMember {
			t.Fatalf("idempotent private message = %#v", message)
		}
	}
	var messageCount int
	var claimCount int
	if err := pool.QueryRow(ctx, `select count(*) from conversation_messages`).Scan(&messageCount); err != nil {
		t.Fatalf("count private messages: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from conversation_reply_claims`).Scan(&claimCount); err != nil {
		t.Fatalf("count private reply claims: %v", err)
	}
	if messageCount != 1 || claimCount != 1 {
		t.Fatalf("private message/claim counts = %d/%d, want 1/1", messageCount, claimCount)
	}

	conflict := command
	conflict.Text = "This key must not change its private input"
	if _, err := store.SendConversationMessage(ctx, conflict); !errors.Is(err, conversation.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	pending := command
	pending.Text = "A second question must wait"
	pending.IdempotencyKey = "another-private-question"
	if _, err := store.SendConversationMessage(ctx, pending); !errors.Is(err, conversation.ErrReplyPending) {
		t.Fatalf("second pending message error = %v", err)
	}
}

func TestConversationReadRequiresTheExactCurrentMember(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
		DisplayName: "Mina", SpaceName: "Private Planning", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	empty, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
	})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty private Conversation = %#v, %v", empty, err)
	}
	original, err := store.SendConversationMessage(ctx, conversation.SendCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
		Text: "Keep this acquisition question private", IdempotencyKey: "private-acquisition",
	})
	if err != nil {
		t.Fatalf("send private message: %v", err)
	}

	otherUserID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into carry_users (user_id, display_name) values ($1, 'Other Space Member')
	`, otherUserID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_enroll_machines)
		values ($1, $2, false)
	`, bootstrap.SpaceID, otherUserID); err != nil {
		t.Fatalf("create other membership: %v", err)
	}
	var sourceConversationID string
	if err := pool.QueryRow(ctx, `
		select conversation_id from conversations where space_id = $1 and member_user_id = $2
	`, bootstrap.SpaceID, bootstrap.UserID).Scan(&sourceConversationID); err != nil {
		t.Fatalf("load source Conversation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into conversation_messages (
			message_id, conversation_id, message_seq, author, author_user_id,
			text, member_request_id, request_digest
		) values ($1, $2, 2, 'member', $3, 'forged private actor', 'forged-actor', decode(repeat('01', 32), 'hex'))
	`, uuid.NewString(), sourceConversationID, otherUserID); err == nil {
		t.Fatal("different Space member was accepted as the private message author")
	}
	otherMessages, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: otherUserID,
	})
	if err != nil || len(otherMessages) != 0 {
		t.Fatalf("other member private Conversation = %#v, %v", otherMessages, err)
	}
	if _, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: otherUserID, After: original.MessageID,
	}); !errors.Is(err, conversation.ErrInvalidCursor) {
		t.Fatalf("other member's foreign cursor error = %v", err)
	}

	if _, err := pool.Exec(ctx, `
		update space_memberships
		set revoked_at = transaction_timestamp(), version = version + 1
		where space_id = $1 and user_id = $2
	`, bootstrap.SpaceID, bootstrap.UserID); err != nil {
		t.Fatalf("revoke source member: %v", err)
	}
	if _, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former member read error = %v", err)
	}
}

func TestConversationReadUsesBoundedNewestBeforeAndAfterPages(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
		DisplayName: "Aya", SpaceName: "Long Research", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	conversationID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into conversations (conversation_id, space_id, member_user_id, message_head_seq)
		values ($1, $2, $3, 120)
	`, conversationID, bootstrap.SpaceID, bootstrap.UserID); err != nil {
		t.Fatalf("create pagination Conversation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into conversation_messages (
			message_id, conversation_id, message_seq, author, author_user_id,
			text, member_request_id, request_digest
		)
		select
			gen_random_uuid(), $1, sequence, 'member', $2,
			'Private fixture ' || lpad(sequence::text, 3, '0'),
			'pagination-' || sequence,
			decode(repeat('01', 32), 'hex')
		from generate_series(1, 120) as sequence
	`, conversationID, bootstrap.UserID); err != nil {
		t.Fatalf("insert pagination messages: %v", err)
	}
	messageIDs := make([]string, 121)
	rows, err := pool.Query(ctx, `
		select message_seq, message_id::text
		from conversation_messages
		where conversation_id = $1
		order by message_seq
	`, conversationID)
	if err != nil {
		t.Fatalf("load pagination message identities: %v", err)
	}
	for rows.Next() {
		var sequence int
		var messageID string
		if err := rows.Scan(&sequence, &messageID); err != nil {
			rows.Close()
			t.Fatalf("scan pagination message: %v", err)
		}
		messageIDs[sequence] = messageID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("read pagination message identities: %v", err)
	}
	rows.Close()

	newest, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
	})
	if err != nil {
		t.Fatalf("list newest private page: %v", err)
	}
	assertConversationSequences(t, newest, 71, 120)

	before, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID, Before: messageIDs[71],
	})
	if err != nil {
		t.Fatalf("list private page before cursor: %v", err)
	}
	assertConversationSequences(t, before, 21, 70)

	after, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID, After: messageIDs[70],
	})
	if err != nil {
		t.Fatalf("list private page after cursor: %v", err)
	}
	assertConversationSequences(t, after, 71, 120)

	foreignConversationID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into conversations (conversation_id, space_id, member_user_id, message_head_seq)
		values ($1, $2, $3, 1)
	`, foreignConversationID, bootstrap.SpaceID, bootstrap.UserID); err == nil {
		t.Fatal("second Conversation for one member was accepted")
	}

	otherUserID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into carry_users (user_id, display_name) values ($1, 'Cursor Owner')
	`, otherUserID); err != nil {
		t.Fatalf("create cursor owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_enroll_machines)
		values ($1, $2, false)
	`, bootstrap.SpaceID, otherUserID); err != nil {
		t.Fatalf("create cursor owner membership: %v", err)
	}
	foreignConversationID = uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into conversations (conversation_id, space_id, member_user_id, message_head_seq)
		values ($1, $2, $3, 1)
	`, foreignConversationID, bootstrap.SpaceID, otherUserID); err != nil {
		t.Fatalf("create foreign Conversation: %v", err)
	}
	foreignMessageID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into conversation_messages (
			message_id, conversation_id, message_seq, author, author_user_id,
			text, member_request_id, request_digest
		) values ($1, $2, 1, 'member', $3, 'Foreign cursor', 'foreign-cursor', decode(repeat('01', 32), 'hex'))
	`, foreignMessageID, foreignConversationID, otherUserID); err != nil {
		t.Fatalf("create foreign cursor: %v", err)
	}
	if _, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID, Before: foreignMessageID,
	}); !errors.Is(err, conversation.ErrInvalidCursor) {
		t.Fatalf("foreign cursor error = %v", err)
	}
	if _, err := store.ListConversationMessages(ctx, conversation.ListCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
		Before: messageIDs[2], After: messageIDs[3],
	}); !errors.Is(err, conversation.ErrInvalidCursor) {
		t.Fatalf("two cursor error = %v", err)
	}
}

func assertConversationSequences(
	t *testing.T,
	messages []conversation.Message,
	first int64,
	last int64,
) {
	t.Helper()
	if len(messages) != int(last-first+1) {
		t.Fatalf("message count = %d, want %d", len(messages), last-first+1)
	}
	for index, message := range messages {
		if want := first + int64(index); message.Sequence != want {
			t.Fatalf("sequence[%d] = %d, want %d", index, message.Sequence, want)
		}
	}
}
