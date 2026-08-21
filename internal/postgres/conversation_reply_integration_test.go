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

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConversationReplyConcurrentClaimHasOneWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, machineID, sourceID := replyFixture(t, ctx, pool, store, "Claim Winner")

	const callers = 8
	results := make(chan conversation.ReplyClaim, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			claim, err := store.ClaimConversationReply(ctx, machineID)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- claim
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)

	claims := 0
	for claim := range results {
		claims++
		if claim.SourceMessageID != sourceID || claim.Fence != 1 || len(claim.Messages) != 1 {
			t.Fatalf("claim = %#v", claim)
		}
	}
	noReply := 0
	for err := range errorsFound {
		if !errors.Is(err, conversation.ErrNoReplyAvailable) {
			t.Fatalf("concurrent claim error = %v", err)
		}
		noReply++
	}
	if claims != 1 || noReply != callers-1 {
		t.Fatalf("claim/no-reply counts = %d/%d", claims, noReply)
	}

	crossUserID := uuid.NewString()
	crossSpaceID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, 'Cross Space Member')`, crossUserID); err != nil {
		t.Fatalf("create cross-Space user: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into spaces (space_id, name) values ($1, 'Other Private Space')`, crossSpaceID); err != nil {
		t.Fatalf("create cross Space: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into space_memberships (space_id, user_id, can_enroll_machines) values ($1, $2, true)`, crossSpaceID, crossUserID); err != nil {
		t.Fatalf("create cross-Space membership: %v", err)
	}
	crossMachineID := insertReplyMachine(t, ctx, pool, crossSpaceID, crossUserID)
	if _, err := store.ClaimConversationReply(ctx, crossMachineID); !errors.Is(err, conversation.ErrNoReplyAvailable) {
		t.Fatalf("cross-Space claim error = %v", err)
	}
	if _, err := store.CommitConversationReply(ctx, conversation.CommitReplyCommand{
		MachineID: crossMachineID, SourceMessageID: sourceID, Fence: 1,
		Candidate: conversation.ReplyCandidate{Reply: "Cross Space must not reply"},
	}); !errors.Is(err, conversation.ErrStaleReplyClaim) {
		t.Fatalf("cross-Space commit error = %v", err)
	}
	_ = bootstrap
}

func TestConversationReplyContextIsFixedAcrossRecoveryAndBounded(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		textBytes int
		wantCount int
	}{
		{name: "message count", textBytes: 16, wantCount: conversation.MaxContextMessages},
		{name: "text bytes", textBytes: 9 * 1024, wantCount: 28},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			pool := openMigratedTestPool(t, ctx)
			store := NewStore(pool)
			bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
				DisplayName: "Context Member", SpaceName: "Context Space",
			})
			if err != nil {
				t.Fatalf("bootstrap: %v", err)
			}
			firstMachineID := insertReplyMachine(t, ctx, pool, bootstrap.SpaceID, bootstrap.UserID)
			secondMachineID := insertReplyMachine(t, ctx, pool, bootstrap.SpaceID, bootstrap.UserID)
			sourceID := seedPrivateHistory(t, ctx, pool, bootstrap.SpaceID, bootstrap.UserID, 41, testCase.textBytes)

			first, err := store.ClaimConversationReply(ctx, firstMachineID)
			if err != nil {
				t.Fatalf("first claim: %v", err)
			}
			if len(first.Messages) != testCase.wantCount {
				t.Fatalf("context count = %d, want %d", len(first.Messages), testCase.wantCount)
			}
			textBytes := 0
			for _, message := range first.Messages {
				textBytes += len(message.Text)
			}
			if len(first.Messages) > conversation.MaxContextMessages || textBytes > conversation.MaxContextTextBytes ||
				first.SourceMessageID != sourceID {
				t.Fatalf("context limits = messages %d, bytes %d, source %s", len(first.Messages), textBytes, first.SourceMessageID)
			}

			if _, err := pool.Exec(ctx, `
				update conversation_reply_claims
				set lease_expires_at = clock_timestamp() - interval '1 second'
				where source_message_id = $1
			`, sourceID); err != nil {
				t.Fatalf("expire claim: %v", err)
			}
			recovered, err := store.ClaimConversationReply(ctx, secondMachineID)
			if err != nil {
				t.Fatalf("recover claim: %v", err)
			}
			if recovered.Fence != first.Fence+1 || !reflect.DeepEqual(recovered.Messages, first.Messages) {
				t.Fatalf("recovered claim = %#v, want byte-equivalent context and fence %d", recovered, first.Fence+1)
			}
			if _, err := store.RenewConversationReply(ctx, conversation.RenewReplyCommand{
				MachineID: firstMachineID, SourceMessageID: first.SourceMessageID, Fence: first.Fence,
			}); !errors.Is(err, conversation.ErrStaleReplyClaim) {
				t.Fatalf("old Machine renew after recovery error = %v", err)
			}
			if _, err := store.CommitConversationReply(ctx, conversation.CommitReplyCommand{
				MachineID: firstMachineID, SourceMessageID: first.SourceMessageID, Fence: first.Fence,
				Candidate: conversation.ReplyCandidate{Reply: "Old recovery authority must remain stale"},
			}); !errors.Is(err, conversation.ErrStaleReplyClaim) {
				t.Fatalf("old Machine commit after recovery error = %v", err)
			}
		})
	}
}

func TestConversationReplyRenewAndFirstCommitRejectLostAuthority(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(context.Context, *testing.T, *pgxpool.Pool, replyAuthorityFixture) conversation.RenewReplyCommand
		want   error
	}{
		{
			name: "wrong Machine",
			mutate: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture replyAuthorityFixture) conversation.RenewReplyCommand {
				return conversation.RenewReplyCommand{
					MachineID:       insertReplyMachine(t, ctx, pool, fixture.spaceID, fixture.memberID),
					SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence,
				}
			},
			want: conversation.ErrStaleReplyClaim,
		},
		{
			name: "stale fence",
			mutate: func(_ context.Context, _ *testing.T, _ *pgxpool.Pool, fixture replyAuthorityFixture) conversation.RenewReplyCommand {
				return conversation.RenewReplyCommand{
					MachineID: fixture.machineID, SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence + 1,
				}
			},
			want: conversation.ErrStaleReplyClaim,
		},
		{
			name: "expired lease",
			mutate: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture replyAuthorityFixture) conversation.RenewReplyCommand {
				if _, err := pool.Exec(ctx, `update conversation_reply_claims set lease_expires_at = clock_timestamp() - interval '1 second' where source_message_id = $1`, fixture.claim.SourceMessageID); err != nil {
					t.Fatalf("expire lease: %v", err)
				}
				return conversation.RenewReplyCommand{MachineID: fixture.machineID, SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence}
			},
			want: conversation.ErrStaleReplyClaim,
		},
		{
			name: "revoked Machine",
			mutate: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture replyAuthorityFixture) conversation.RenewReplyCommand {
				if _, err := pool.Exec(ctx, `update machines set revoked_at = clock_timestamp() where machine_id = $1`, fixture.machineID); err != nil {
					t.Fatalf("revoke Machine: %v", err)
				}
				return conversation.RenewReplyCommand{MachineID: fixture.machineID, SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence}
			},
			want: machine.ErrMachineRevoked,
		},
		{
			name: "inactive member",
			mutate: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture replyAuthorityFixture) conversation.RenewReplyCommand {
				if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = clock_timestamp(), version = version + 1 where space_id = $1 and user_id = $2`, fixture.spaceID, fixture.memberID); err != nil {
					t.Fatalf("revoke member: %v", err)
				}
				return conversation.RenewReplyCommand{MachineID: fixture.machineID, SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence}
			},
			want: conversation.ErrStaleReplyClaim,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			pool := openMigratedTestPool(t, ctx)
			store := NewStore(pool)
			fixture := claimedReplyFixture(t, ctx, pool, store, testCase.name)
			command := testCase.mutate(ctx, t, pool, fixture)
			if _, err := store.RenewConversationReply(ctx, command); !errors.Is(err, testCase.want) {
				t.Fatalf("renew error = %v, want %v", err, testCase.want)
			}
			if _, err := store.CommitConversationReply(ctx, conversation.CommitReplyCommand{
				MachineID: command.MachineID, SourceMessageID: command.SourceMessageID, Fence: command.Fence,
				Candidate: conversation.ReplyCandidate{Reply: "This authority must not commit"},
			}); !errors.Is(err, testCase.want) {
				t.Fatalf("commit error = %v, want %v", err, testCase.want)
			}
			var replyCount int
			var workCount int
			if err := pool.QueryRow(ctx, `select count(*) from conversation_messages where author = 'carry'`).Scan(&replyCount); err != nil {
				t.Fatalf("count replies: %v", err)
			}
			if err := pool.QueryRow(ctx, `select count(*) from works`).Scan(&workCount); err != nil {
				t.Fatalf("count Works: %v", err)
			}
			if replyCount != 0 || workCount != 0 {
				t.Fatalf("reply/Work counts = %d/%d, want 0/0", replyCount, workCount)
			}
		})
	}
}

func TestRevokedMachineCannotClaimPrivateConversationReply(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	_, machineID, _ := replyFixture(t, ctx, pool, store, "revoked-claim")
	if err := revokeMachineForTest(ctx, store, machineID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimConversationReply(ctx, machineID); !errors.Is(err, machine.ErrMachineRevoked) {
		t.Fatalf("revoked private reply claim error = %v", err)
	}
}

func TestConversationReplyConcurrentCommitAndCompletedReplayAreReplyOnce(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	fixture := claimedReplyFixture(t, ctx, pool, store, "Reply Once")
	command := conversation.CommitReplyCommand{
		MachineID: fixture.machineID, SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence,
		Candidate: conversation.ReplyCandidate{Reply: "Start with the confirmed renewal date."},
	}

	const callers = 8
	results := make(chan conversation.CommitReplyResult, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			result, err := store.CommitConversationReply(ctx, command)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent commit: %v", err)
	}
	var durable conversation.CommitReplyResult
	for result := range results {
		if durable.ReplyMessageID == "" {
			durable = result
		}
		if result != durable || result.CreatedWorkID != "" {
			t.Fatalf("commit result = %#v, want %#v without Work", result, durable)
		}
	}
	var replies, works int
	if err := pool.QueryRow(ctx, `select count(*) from conversation_messages where author = 'carry'`).Scan(&replies); err != nil {
		t.Fatalf("count replies: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from works`).Scan(&works); err != nil {
		t.Fatalf("count Works: %v", err)
	}
	if replies != 1 || works != 0 {
		t.Fatalf("reply/Work counts = %d/%d, want 1/0", replies, works)
	}

	if _, err := pool.Exec(ctx, `update conversation_reply_claims set lease_expires_at = clock_timestamp() - interval '1 second' where source_message_id = $1`, fixture.claim.SourceMessageID); err != nil {
		t.Fatalf("expire completed lease: %v", err)
	}
	replayed, err := store.CommitConversationReply(ctx, command)
	if err != nil || replayed != durable {
		t.Fatalf("completed replay = %#v, %v, want %#v", replayed, err, durable)
	}
	altered := command
	altered.Candidate.Reply = "A changed reply must not replay"
	if _, err := store.CommitConversationReply(ctx, altered); !errors.Is(err, conversation.ErrReplyConflict) {
		t.Fatalf("altered replay error = %v", err)
	}
	if _, err := pool.Exec(ctx, `update machines set revoked_at = clock_timestamp() where machine_id = $1`, fixture.machineID); err != nil {
		t.Fatalf("revoke Machine: %v", err)
	}
	if _, err := store.CommitConversationReply(ctx, command); !errors.Is(err, machine.ErrMachineRevoked) {
		t.Fatalf("revoked completed replay error = %v", err)
	}
}

func TestConversationDelegationCreatesOneSharedWorkWithoutPrivateSource(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	fixture := claimedReplyFixture(t, ctx, pool, store, "Delegation")
	goal := "Prepare the renewal packet"
	command := conversation.CommitReplyCommand{
		MachineID: fixture.machineID, SourceMessageID: fixture.claim.SourceMessageID, Fence: fixture.claim.Fence,
		Candidate: conversation.ReplyCandidate{Reply: "I will prepare it and keep you posted.", DelegationGoal: &goal},
	}
	result, err := store.CommitConversationReply(ctx, command)
	if err != nil {
		t.Fatalf("commit delegation: %v", err)
	}
	if result.ReplyMessageID == "" || result.CreatedWorkID == "" {
		t.Fatalf("delegation result = %#v", result)
	}
	details, err := store.LoadWork(ctx, work.LoadCommand{UserID: fixture.memberID, SpaceID: fixture.spaceID, WorkID: result.CreatedWorkID})
	if err != nil {
		t.Fatalf("load delegated Work: %v", err)
	}
	if details.Work.Goal != goal || details.Work.CreatorUserID != fixture.memberID ||
		details.Work.OwnerUserID != fixture.memberID || len(details.Messages) != 0 {
		t.Fatalf("delegated Work = %#v", details)
	}
	listed, err := store.ListWorks(ctx, work.ListCommand{UserID: fixture.memberID, SpaceID: fixture.spaceID})
	if err != nil || len(listed.Works) != 1 || listed.Works[0].WorkID != result.CreatedWorkID {
		t.Fatalf("listed delegated Work = %#v, %v", listed, err)
	}
	for _, privateValue := range []string{fixture.claim.SourceMessageID, "private source text"} {
		var leaked bool
		if err := pool.QueryRow(ctx, `
			select exists(
				select 1 from works
				where work_id = $1 and (
					goal like '%' || $2 || '%'
					or create_idempotency_key like '%' || $2 || '%'
					or encode(create_request_digest, 'hex') like '%' || $2 || '%'
				)
			)
		`, result.CreatedWorkID, privateValue).Scan(&leaked); err != nil {
			t.Fatalf("check Work private leak: %v", err)
		}
		if leaked {
			t.Fatalf("delegated Work exposed private value %q", privateValue)
		}
	}
	claim, err := store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim delegated Work Run: %v", err)
	}
	if claim.WorkID != result.CreatedWorkID || claim.Goal != goal || len(claim.Messages) != 0 {
		t.Fatalf("delegated Run claim = %#v", claim)
	}
	if strings.Contains(fmt.Sprintf("%#v", claim), fixture.claim.SourceMessageID) || strings.Contains(fmt.Sprintf("%#v", claim), "private source text") {
		t.Fatalf("Run claim exposed private source: %#v", claim)
	}

	if _, err := pool.Exec(ctx, `update conversation_reply_claims set lease_expires_at = clock_timestamp() - interval '1 second' where source_message_id = $1`, fixture.claim.SourceMessageID); err != nil {
		t.Fatalf("expire completed lease: %v", err)
	}
	replayed, err := store.CommitConversationReply(ctx, command)
	if err != nil || replayed != result {
		t.Fatalf("delegation replay = %#v, %v, want %#v", replayed, err, result)
	}
	var workCount int
	if err := pool.QueryRow(ctx, `select count(*) from works`).Scan(&workCount); err != nil {
		t.Fatalf("count delegated Works: %v", err)
	}
	if workCount != 1 {
		t.Fatalf("delegated Work count = %d, want 1", workCount)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = clock_timestamp(), version = version + 1 where space_id = $1 and user_id = $2`, fixture.spaceID, fixture.memberID); err != nil {
		t.Fatalf("revoke member: %v", err)
	}
	if _, err := store.CommitConversationReply(ctx, command); !errors.Is(err, conversation.ErrStaleReplyClaim) {
		t.Fatalf("inactive member completed replay error = %v", err)
	}
}

type replyAuthorityFixture struct {
	spaceID   string
	memberID  string
	machineID string
	claim     conversation.ReplyClaim
}

func claimedReplyFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *Store,
	name string,
) replyAuthorityFixture {
	t.Helper()
	bootstrap, machineID, _ := replyFixture(t, ctx, pool, store, name)
	claim, err := store.ClaimConversationReply(ctx, machineID)
	if err != nil {
		t.Fatalf("claim private reply: %v", err)
	}
	return replyAuthorityFixture{
		spaceID: bootstrap.SpaceID, memberID: bootstrap.UserID, machineID: machineID, claim: claim,
	}
}

func replyFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *Store,
	name string,
) (testMember, string, string) {
	t.Helper()
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: name + " Member", SpaceName: name + " Space",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	machineID := insertReplyMachine(t, ctx, pool, bootstrap.SpaceID, bootstrap.UserID)
	message, err := store.SendConversationMessage(ctx, conversation.SendCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
		Text: "private source text that must not enter Work", IdempotencyKey: "private-source-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("send private source: %v", err)
	}
	return bootstrap, machineID, message.MessageID
}

func insertReplyMachine(t *testing.T, ctx context.Context, pool *pgxpool.Pool, spaceID string, userID string) string {
	t.Helper()
	machineID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into machines (
			machine_id, space_id, display_name, public_key_der, certificate_pem,
			certificate_serial, enrolled_by_user_id
		) values ($1, $2, 'private-reply-host', decode('01', 'hex'), decode('02', 'hex'), $3, $4)
	`, machineID, spaceID, uuid.NewString(), userID); err != nil {
		t.Fatalf("insert reply Machine: %v", err)
	}
	return machineID
}

func seedPrivateHistory(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	spaceID string,
	memberID string,
	messageCount int,
	textBytes int,
) string {
	t.Helper()
	conversationID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into conversations (conversation_id, space_id, member_user_id, message_head_seq)
		values ($1, $2, $3, $4)
	`, conversationID, spaceID, memberID, messageCount); err != nil {
		t.Fatalf("insert context Conversation: %v", err)
	}
	var latestMemberMessageID string
	for sequence := 1; sequence <= messageCount; sequence++ {
		messageID := uuid.NewString()
		text := fmt.Sprintf("%04d", sequence) + strings.Repeat("x", textBytes-4)
		if sequence%2 == 1 {
			latestMemberMessageID = messageID
			if _, err := pool.Exec(ctx, `
				insert into conversation_messages (
					message_id, conversation_id, message_seq, author, author_user_id,
					text, member_request_id, request_digest
				) values ($1, $2, $3, 'member', $4, $5, $6, decode(repeat('01', 32), 'hex'))
			`, messageID, conversationID, sequence, memberID, text, "history-"+uuid.NewString()); err != nil {
				t.Fatalf("insert member context message %d: %v", sequence, err)
			}
			continue
		}
		if _, err := pool.Exec(ctx, `
			insert into conversation_messages (
				message_id, conversation_id, message_seq, author, text, reply_to_member_message_id
			) values ($1, $2, $3, 'carry', $4, $5)
		`, messageID, conversationID, sequence, text, latestMemberMessageID); err != nil {
			t.Fatalf("insert Carry context message %d: %v", sequence, err)
		}
	}
	if messageCount%2 == 0 {
		t.Fatal("context fixture must end with a member source")
	}
	if _, err := pool.Exec(ctx, `
		insert into conversation_reply_claims (source_message_id, conversation_id)
		values ($1, $2)
	`, latestMemberMessageID, conversationID); err != nil {
		t.Fatalf("insert context reply claim: %v", err)
	}
	return latestMemberMessageID
}
