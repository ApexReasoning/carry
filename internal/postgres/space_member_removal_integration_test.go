//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListSpaceMembersPaginatesEveryActiveRemovalTarget(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	var finalTarget string
	for range 105 {
		finalTarget = seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := store.ListSpaceMembers(ctx, space.ListMembersCommand{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, AfterUserID: cursor})
		if err != nil {
			t.Fatalf("list member page: %v", err)
		}
		if len(page.Members) > space.MemberPageSize {
			t.Fatalf("member page size = %d", len(page.Members))
		}
		for _, member := range page.Members {
			if seen[member.UserID] {
				t.Fatalf("duplicate paginated member %s", member.UserID)
			}
			seen[member.UserID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 106 || !seen[finalTarget] {
		t.Fatalf("paginated members = %d, final target visible = %t", len(seen), seen[finalTarget])
	}
}

func TestListSpaceMembersRejectsFormerAndNeverMemberCursorsIdentically(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	formerMember := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	if err := store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: formerMember, IdempotencyKey: "remove-cursor-member",
	})); err != nil {
		t.Fatalf("remove cursor member: %v", err)
	}

	for name, cursor := range map[string]string{"former member": formerMember, "never member": uuid.NewString()} {
		t.Run(name, func(t *testing.T) {
			_, err := store.ListSpaceMembers(ctx, space.ListMembersCommand{
				SpaceID: manager.SpaceID, ActorUserID: manager.UserID, AfterUserID: cursor,
			})
			if !errors.Is(err, space.ErrInvalidMemberCursor) {
				t.Fatalf("cursor error = %v, want %v", err, space.ErrInvalidMemberCursor)
			}
		})
	}
}

func TestRemoveSpaceMemberWorklessTransferReplayAndRetention(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	target := seedRemovalMember(t, ctx, pool, manager.SpaceID, true, true)
	successor := seedRemovalMember(t, ctx, pool, manager.SpaceID, true, true)
	otherSpace := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into spaces (space_id, name) values ($1, 'Retained Space')`, otherSpace); err != nil {
		t.Fatalf("seed retained Space: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines) values ($1, $2, false, false)`, otherSpace, target); err != nil {
		t.Fatalf("seed retained Membership: %v", err)
	}

	enrolledMachine, err := enrollMachineForTest(ctx, store, testMachineCommand{
		MachineID: uuid.NewString(), SpaceID: manager.SpaceID, DisplayName: "Independent Machine",
		PublicKeyDER: []byte{1}, CertificatePEM: []byte("certificate"), CertificateSerial: uuid.NewString(),
		EnrolledByUserID: target,
	})
	if err != nil {
		t.Fatalf("seed independent Machine: %v", err)
	}
	privateMessage, err := store.SendConversationMessage(ctx, conversation.SendCommand{
		SpaceID: manager.SpaceID, MemberUserID: target, Text: "Private retained context", IdempotencyKey: "private-before-removal",
	})
	if err != nil {
		t.Fatalf("seed private Conversation: %v", err)
	}
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})
	pending, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: target, RecipientEmail: "retained-invitation@example.com", IdempotencyKey: "retained-invitation",
	})
	if err != nil {
		t.Fatalf("seed pending invitation: %v", err)
	}

	command := removalCommand(t, space.RemoveMemberRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: target, IdempotencyKey: "remove-workless",
	})
	if err := store.RemoveSpaceMember(ctx, command); err != nil {
		t.Fatalf("remove workless member: %v", err)
	}
	if err := store.RemoveSpaceMember(ctx, command); err != nil {
		t.Fatalf("replay workless removal: %v", err)
	}
	changed := removalCommand(t, space.RemoveMemberRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: successor, IdempotencyKey: "remove-workless",
	})
	if err := store.RemoveSpaceMember(ctx, changed); !errors.Is(err, space.ErrIdempotencyConflict) {
		t.Fatalf("changed target replay = %v", err)
	}
	if _, err := store.ListSpaceMembers(ctx, space.ListMembersCommand{SpaceID: manager.SpaceID, ActorUserID: target}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former member list = %v", err)
	}
	if page, err := store.ListSpaceMembers(ctx, space.ListMembersCommand{SpaceID: otherSpace, ActorUserID: target}); err != nil || len(page.Members) != 1 {
		t.Fatalf("retained other-Space access = %#v, %v", page, err)
	}
	formerSessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1,$2,'email',transaction_timestamp()+interval '1 hour')`, formerSessionID, target); err != nil {
		t.Fatalf("seed former member Browser Session: %v", err)
	}
	requestID := uuid.NewString()
	codeDigest, pollDigest, sourceDigest, requestDigest := [32]byte{1}, [32]byte{2}, [32]byte{3}, [32]byte{4}
	if _, err := store.BeginMachineConnection(ctx, machine.BeginConnectionCommand{
		RequestID: requestID, IdempotencyKey: uuid.NewString(), DisplayName: "Former Machine",
		PublicKeyDER: []byte{1}, KeyProof: make([]byte, 64), CodeDigest: codeDigest, PollDigest: pollDigest,
		SourceDigest: sourceDigest, RequestDigest: requestDigest,
	}); err != nil {
		t.Fatalf("seed former member Machine request: %v", err)
	}
	if _, err := store.DecideMachineConnection(ctx, machine.DecideConnectionCommand{
		BrowserSessionID: formerSessionID, RequestID: requestID, SpaceID: manager.SpaceID, Decision: "approved",
		IdempotencyKey: uuid.NewString(), PreparedMachineID: uuid.NewString(), CodeDigest: codeDigest, RequestDigest: [32]byte{5},
	}); !errors.Is(err, machine.ErrMachineAuthority) {
		t.Fatalf("former member Machine approval = %v", err)
	}
	if err := invitations.Revoke(ctx, space.RevokeInvitationCommand{
		SpaceID: manager.SpaceID, InvitationID: pending.InvitationID, ActorUserID: target, IdempotencyKey: "former-revoke",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former member invitation revoke = %v", err)
	}
	if _, err := store.CreateWork(ctx, work.CreateCommand{SpaceID: manager.SpaceID, CreatorUserID: target, Goal: "Former access", IdempotencyKey: "former-create"}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former member Work create = %v", err)
	}
	if _, err := store.ListConversationMessages(ctx, conversation.ListCommand{SpaceID: manager.SpaceID, MemberUserID: target}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former member private read = %v", err)
	}
	var machineActive bool
	if err := pool.QueryRow(ctx, `select revoked_at is null from machines where machine_id = $1`, enrolledMachine.MachineID).Scan(&machineActive); err != nil {
		t.Fatalf("load independent Machine: %v", err)
	}
	if !machineActive {
		t.Fatal("member removal revoked an independent Space Machine")
	}
	var privateRows, invitationRows int
	if err := pool.QueryRow(ctx, `select count(*) from conversation_messages where message_id = $1`, privateMessage.MessageID).Scan(&privateRows); err != nil {
		t.Fatalf("count retained private message: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from space_invitations where invitation_id = $1 and accepted_at is null and revoked_at is null`, pending.InvitationID).Scan(&invitationRows); err != nil {
		t.Fatalf("count retained invitation: %v", err)
	}
	if privateRows != 1 || invitationRows != 1 {
		t.Fatalf("retained private rows = %d, invitations = %d", privateRows, invitationRows)
	}
}

func TestRemoveSpaceMemberTransfersAllOpenWorkAtomically(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	target := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	successor := seedRemovalMember(t, ctx, pool, manager.SpaceID, true, true)
	works := make([]work.Work, 2)
	for index := range works {
		created, err := store.CreateWork(ctx, work.CreateCommand{SpaceID: manager.SpaceID, CreatorUserID: target, Goal: "Owned continuity", IdempotencyKey: "owned-work-" + string(rune('a'+index))})
		if err != nil {
			t.Fatalf("seed target Work: %v", err)
		}
		works[index] = created
	}
	historicalMessage, err := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: works[0].WorkID, SpaceID: manager.SpaceID, AuthorUserID: target,
		Text: "Retained historical authorship", IdempotencyKey: "historical-author",
	})
	if err != nil {
		t.Fatalf("seed historical Work message: %v", err)
	}
	missing := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: target, IdempotencyKey: "missing-successor"})
	if err := store.RemoveSpaceMember(ctx, missing); !errors.Is(err, space.ErrRemovalSuccessorRequired) {
		t.Fatalf("missing successor = %v", err)
	}
	assertRemovalState(t, ctx, pool, manager.SpaceID, target, target, true, len(works))

	command := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: target, SuccessorUserID: successor, IdempotencyKey: "transfer-all"})
	if err := store.RemoveSpaceMember(ctx, command); err != nil {
		t.Fatalf("remove with transfer: %v", err)
	}
	assertRemovalState(t, ctx, pool, manager.SpaceID, target, successor, false, len(works))
	var retainedCreators, retainedAuthors int
	if err := pool.QueryRow(ctx, `select count(*) from works where creator_user_id = $1 and work_id = any($2::uuid[])`, target, []string{works[0].WorkID, works[1].WorkID}).Scan(&retainedCreators); err != nil {
		t.Fatalf("count retained Work creators: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from work_messages where message_id = $1 and author_user_id = $2`, historicalMessage.MessageID, target).Scan(&retainedAuthors); err != nil {
		t.Fatalf("count retained Work authors: %v", err)
	}
	if retainedCreators != len(works) || retainedAuthors != 1 {
		t.Fatalf("retained creators = %d, authors = %d", retainedCreators, retainedAuthors)
	}
	if err := store.RemoveSpaceMember(ctx, command); err != nil {
		t.Fatalf("exact transfer replay: %v", err)
	}
	changedSuccessor := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: target, SuccessorUserID: manager.UserID, IdempotencyKey: "transfer-all"})
	if err := store.RemoveSpaceMember(ctx, changedSuccessor); !errors.Is(err, space.ErrIdempotencyConflict) {
		t.Fatalf("changed successor replay = %v", err)
	}
}

func TestRemoveSpaceMemberRejectsInvalidSuccessorAndFinalAuthorities(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	ordinary := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	otherSpace := uuid.NewString()
	crossSpace := seedRemovalSpaceMember(t, ctx, pool, otherSpace, false, false)
	unauthorized := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: ordinary, TargetUserID: manager.UserID, IdempotencyKey: "ordinary-actor"})
	if err := store.RemoveSpaceMember(ctx, unauthorized); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("ordinary actor = %v", err)
	}
	crossSpaceActor := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: crossSpace, TargetUserID: ordinary, IdempotencyKey: "cross-space-actor"})
	if err := store.RemoveSpaceMember(ctx, crossSpaceActor); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("cross-Space actor = %v", err)
	}
	unexpected := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: ordinary, SuccessorUserID: manager.UserID, IdempotencyKey: "unexpected"})
	if err := store.RemoveSpaceMember(ctx, unexpected); !errors.Is(err, space.ErrRemovalSuccessorUnexpected) {
		t.Fatalf("unneeded successor = %v", err)
	}
	owned, err := store.CreateWork(ctx, work.CreateCommand{SpaceID: manager.SpaceID, CreatorUserID: ordinary, Goal: "Needs valid successor", IdempotencyKey: "invalid-successor-work"})
	if err != nil {
		t.Fatalf("seed Work: %v", err)
	}
	_ = owned
	for name, successor := range map[string]string{"target": ordinary, "cross-space": crossSpace, "missing": uuid.NewString()} {
		t.Run(name, func(t *testing.T) {
			command := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: ordinary, SuccessorUserID: successor, IdempotencyKey: "invalid-" + name})
			if err := store.RemoveSpaceMember(ctx, command); !errors.Is(err, space.ErrRemovalSuccessorInvalid) {
				t.Fatalf("invalid successor = %v", err)
			}
		})
	}
	assertRemovalState(t, ctx, pool, manager.SpaceID, ordinary, ordinary, true, 1)
	soleManager := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: manager.UserID, IdempotencyKey: "last-manager"})
	if err := store.RemoveSpaceMember(ctx, soleManager); !errors.Is(err, space.ErrLastMemberManager) {
		t.Fatalf("last manager = %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set can_manage_members = true where space_id = $1 and user_id = $2`, manager.SpaceID, ordinary); err != nil {
		t.Fatalf("grant alternate manager: %v", err)
	}
	soleEnroller := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: ordinary, TargetUserID: manager.UserID, IdempotencyKey: "last-enroller"})
	if err := store.RemoveSpaceMember(ctx, soleEnroller); !errors.Is(err, space.ErrLastMachineEnroller) {
		t.Fatalf("last enroller = %v", err)
	}
}

func TestRemoveSpaceMemberSelfReplayAndConcurrentRemovalSafety(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	other := seedRemovalMember(t, ctx, pool, manager.SpaceID, true, true)
	self := removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: manager.UserID, IdempotencyKey: "self-remove"})
	if err := store.RemoveSpaceMember(ctx, self); err != nil {
		t.Fatalf("self removal: %v", err)
	}
	if err := store.RemoveSpaceMember(ctx, self); err != nil {
		t.Fatalf("self removal replay: %v", err)
	}
	if _, err := store.ListMemberships(ctx, manager.UserID); err != nil {
		t.Fatalf("former manager still identifies as User: %v", err)
	}

	third := seedRemovalMember(t, ctx, pool, manager.SpaceID, true, true)
	target := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, actor := range []string{other, third} {
		actor := actor
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{SpaceID: manager.SpaceID, ActorUserID: actor, TargetUserID: target, IdempotencyKey: "same-target-" + actor}))
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, space.ErrMemberUnavailable) {
			t.Fatalf("same-target removal = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("same-target successes = %d", successes)
	}

	crossStart := make(chan struct{})
	crossResults := make(chan error, 2)
	wait = sync.WaitGroup{}
	for _, pair := range [][2]string{{other, third}, {third, other}} {
		wait.Add(1)
		go func(actorID, targetID string) {
			defer wait.Done()
			<-crossStart
			crossResults <- store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{
				SpaceID: manager.SpaceID, ActorUserID: actorID, TargetUserID: targetID, IdempotencyKey: "cross-remove-" + actorID,
			}))
		}(pair[0], pair[1])
	}
	close(crossStart)
	wait.Wait()
	close(crossResults)
	successes = 0
	for err := range crossResults {
		if err == nil {
			successes++
		} else if !errors.Is(err, space.ErrForbidden) && !errors.Is(err, space.ErrMemberUnavailable) {
			t.Fatalf("cross-removal = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("cross-removal successes = %d", successes)
	}
	var activeManagers, activeEnrollers int
	if err := pool.QueryRow(ctx, `select count(*) filter (where can_manage_members), count(*) filter (where can_enroll_machines) from space_memberships where space_id = $1 and revoked_at is null`, manager.SpaceID).Scan(&activeManagers, &activeEnrollers); err != nil {
		t.Fatalf("count retained authorities: %v", err)
	}
	if activeManagers < 1 || activeEnrollers < 1 {
		t.Fatalf("retained managers = %d, enrollers = %d", activeManagers, activeEnrollers)
	}
}

func TestWorkReviewAcceptanceAndRemovalHaveOneValidOrder(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	reviewID := commitReviewableWork(t, ctx, fixture)
	successor := seedRemovalMember(t, ctx, fixture.store.pool, fixture.bootstrap.SpaceID, true, true)
	start := make(chan struct{})
	var acceptErr, removeErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		acceptErr = fixture.store.AcceptWorkReview(ctx, work.AcceptReviewCommand{
			WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID, ReviewID: reviewID,
			AcceptedBy: fixture.bootstrap.UserID, IdempotencyKey: "review-removal-race",
		})
	}()
	go func() {
		defer wait.Done()
		<-start
		removeErr = fixture.store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{
			SpaceID: fixture.bootstrap.SpaceID, ActorUserID: successor, TargetUserID: fixture.bootstrap.UserID,
			SuccessorUserID: successor, IdempotencyKey: "remove-review-owner",
		}))
	}()
	close(start)
	wait.Wait()
	if removeErr != nil {
		t.Fatalf("review/removal removal = %v", removeErr)
	}
	if acceptErr != nil && !errors.Is(acceptErr, space.ErrForbidden) {
		t.Fatalf("review/removal acceptance = %v", acceptErr)
	}
	assertRemovalState(t, ctx, fixture.store.pool, fixture.bootstrap.SpaceID, fixture.bootstrap.UserID, successor, false, 1)
}

func TestPrivateMessageAndRemovalHaveOneValidOrder(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	target := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	_ = seedRemovalMember(t, ctx, pool, manager.SpaceID, true, true)
	start := make(chan struct{})
	var messageErr, removeErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, messageErr = store.SendConversationMessage(ctx, conversation.SendCommand{
			SpaceID: manager.SpaceID, MemberUserID: target, Text: "Concurrent retained private message", IdempotencyKey: "message-removal-race",
		})
	}()
	go func() {
		defer wait.Done()
		<-start
		removeErr = store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{
			SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: target, IdempotencyKey: "remove-private-member",
		}))
	}()
	close(start)
	wait.Wait()
	if removeErr != nil {
		t.Fatalf("message/removal removal = %v", removeErr)
	}
	if messageErr != nil && !errors.Is(messageErr, space.ErrForbidden) {
		t.Fatalf("message/removal message = %v", messageErr)
	}
	var messages int
	if err := pool.QueryRow(ctx, `select count(*) from conversation_messages where author_user_id = $1`, target).Scan(&messages); err != nil {
		t.Fatalf("count race private messages: %v", err)
	}
	wantMessages := 0
	if messageErr == nil {
		wantMessages = 1
	}
	if messages != wantMessages {
		t.Fatalf("retained race messages = %d, want %d", messages, wantMessages)
	}
}

func TestConversationReplyCommitBeforeRemovalIsRetainedAndTransferred(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, machineID, _ := replyFixture(t, ctx, pool, store, "Reply First Removal")
	manager := seedRemovalMember(t, ctx, pool, bootstrap.SpaceID, true, true)
	claim, err := store.ClaimConversationReply(ctx, machineID)
	if err != nil {
		t.Fatalf("claim reply before ordered removal: %v", err)
	}

	spaceBlocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin ordered Space blocker: %v", err)
	}
	defer func() { _ = spaceBlocker.Rollback(context.Background()) }()
	var blockerPID int32
	if err := spaceBlocker.QueryRow(ctx, `select pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("load ordered Space blocker backend: %v", err)
	}
	if _, err := spaceBlocker.Exec(ctx, `select space_id from spaces where space_id = $1 for no key update`, bootstrap.SpaceID); err != nil {
		t.Fatalf("hold Space before ordered removal: %v", err)
	}

	removeResult := make(chan error, 1)
	removeCommand := removalCommand(t, space.RemoveMemberRequest{
		SpaceID: bootstrap.SpaceID, ActorUserID: manager, TargetUserID: bootstrap.UserID,
		SuccessorUserID: manager, IdempotencyKey: "remove-after-reply",
	})
	go func() { removeResult <- store.RemoveSpaceMember(ctx, removeCommand) }()
	waitForBlockedSQL(t, ctx, pool, blockerPID, "FROM spaces")

	goal := "Retain a reply-first delegated consequence"
	committed, err := store.CommitConversationReply(ctx, conversation.CommitReplyCommand{
		MachineID: machineID, SourceMessageID: claim.SourceMessageID, Fence: claim.Fence,
		Candidate: conversation.ReplyCandidate{Reply: "This reply completed first.", DelegationGoal: &goal},
	})
	if err != nil {
		t.Fatalf("commit reply while removal waits: %v", err)
	}
	if committed.ReplyMessageID == "" || committed.CreatedWorkID == "" {
		t.Fatalf("reply-first result = %#v", committed)
	}
	if err := spaceBlocker.Commit(ctx); err != nil {
		t.Fatalf("release ordered Space blocker: %v", err)
	}
	if err := <-removeResult; err != nil {
		t.Fatalf("remove after reply commit: %v", err)
	}

	var retainedReplies, transferredWorks int
	if err := pool.QueryRow(ctx, `select count(*) from conversation_messages where message_id = $1 and author = 'carry'`, committed.ReplyMessageID).Scan(&retainedReplies); err != nil {
		t.Fatalf("count retained reply-first message: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from works where work_id = $1 and owner_user_id = $2`, committed.CreatedWorkID, manager).Scan(&transferredWorks); err != nil {
		t.Fatalf("count transferred reply-first Work: %v", err)
	}
	if retainedReplies != 1 || transferredWorks != 1 {
		t.Fatalf("retained reply/Work = %d/%d, want 1/1", retainedReplies, transferredWorks)
	}
}

func TestRemovalCompletionPreventsConversationReplyClaimRenewAndFirstCommit(t *testing.T) {
	for _, operation := range []string{"claim", "renew", "first commit"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pool := openMigratedTestPool(t, ctx)
			store := NewStore(pool)
			bootstrap, machineID, _ := replyFixture(t, ctx, pool, store, "Removal First "+operation)
			manager := seedRemovalMember(t, ctx, pool, bootstrap.SpaceID, true, true)

			var claim conversation.ReplyClaim
			var err error
			if operation != "claim" {
				claim, err = store.ClaimConversationReply(ctx, machineID)
				if err != nil {
					t.Fatalf("seed claimed reply: %v", err)
				}
			}
			machineBlocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin ordered Machine blocker: %v", err)
			}
			defer func() { _ = machineBlocker.Rollback(context.Background()) }()
			var blockerPID int32
			if err := machineBlocker.QueryRow(ctx, `select pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatalf("load ordered Machine blocker backend: %v", err)
			}
			if _, err := machineBlocker.Exec(ctx, `select machine_id from machines where machine_id = $1 for update`, machineID); err != nil {
				t.Fatalf("hold Machine before reply operation: %v", err)
			}

			replyResult := make(chan error, 1)
			goal := "This post-removal Work must not exist"
			switch operation {
			case "claim":
				go func() {
					_, operationErr := store.ClaimConversationReply(ctx, machineID)
					replyResult <- operationErr
				}()
			case "renew":
				go func() {
					_, operationErr := store.RenewConversationReply(ctx, conversation.RenewReplyCommand{
						MachineID: machineID, SourceMessageID: claim.SourceMessageID, Fence: claim.Fence,
					})
					replyResult <- operationErr
				}()
			case "first commit":
				go func() {
					_, operationErr := store.CommitConversationReply(ctx, conversation.CommitReplyCommand{
						MachineID: machineID, SourceMessageID: claim.SourceMessageID, Fence: claim.Fence,
						Candidate: conversation.ReplyCandidate{Reply: "Must not commit after removal.", DelegationGoal: &goal},
					})
					replyResult <- operationErr
				}()
			}
			waitForBlockedSQL(t, ctx, pool, blockerPID, "FROM machines")

			if err := store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{
				SpaceID: bootstrap.SpaceID, ActorUserID: manager, TargetUserID: bootstrap.UserID,
				IdempotencyKey: "remove-before-" + operation,
			})); err != nil {
				t.Fatalf("complete removal before reply operation: %v", err)
			}
			if err := machineBlocker.Commit(ctx); err != nil {
				t.Fatalf("release ordered Machine blocker: %v", err)
			}
			operationErr := <-replyResult
			if operation == "claim" {
				if !errors.Is(operationErr, conversation.ErrNoReplyAvailable) {
					t.Fatalf("post-removal claim error = %v", operationErr)
				}
			} else if !errors.Is(operationErr, conversation.ErrStaleReplyClaim) {
				t.Fatalf("post-removal %s error = %v", operation, operationErr)
			}

			if _, err := store.ClaimConversationReply(ctx, machineID); !errors.Is(err, conversation.ErrNoReplyAvailable) {
				t.Fatalf("subsequent claim error = %v", err)
			}
			if claim.SourceMessageID != "" {
				if _, err := store.RenewConversationReply(ctx, conversation.RenewReplyCommand{
					MachineID: machineID, SourceMessageID: claim.SourceMessageID, Fence: claim.Fence,
				}); !errors.Is(err, conversation.ErrStaleReplyClaim) {
					t.Fatalf("subsequent renew error = %v", err)
				}
				if _, err := store.CommitConversationReply(ctx, conversation.CommitReplyCommand{
					MachineID: machineID, SourceMessageID: claim.SourceMessageID, Fence: claim.Fence,
					Candidate: conversation.ReplyCandidate{Reply: "Still must not commit.", DelegationGoal: &goal},
				}); !errors.Is(err, conversation.ErrStaleReplyClaim) {
					t.Fatalf("subsequent commit error = %v", err)
				}
			}
			var replies, works int
			if err := pool.QueryRow(ctx, `select count(*) from conversation_messages where author = 'carry'`).Scan(&replies); err != nil {
				t.Fatalf("count post-removal replies: %v", err)
			}
			if err := pool.QueryRow(ctx, `select count(*) from works`).Scan(&works); err != nil {
				t.Fatalf("count post-removal delegated Works: %v", err)
			}
			if replies != 0 || works != 0 {
				t.Fatalf("post-removal reply/Work counts = %d/%d, want 0/0", replies, works)
			}
		})
	}
}

func TestWorkCreationCanFinishWhileRemovalWaitsForMembership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	target := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)

	creation, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Work creation ordering transaction: %v", err)
	}
	defer func() { _ = creation.Rollback(context.Background()) }()
	var creationPID int32
	if err := creation.QueryRow(ctx, `select pg_backend_pid()`).Scan(&creationPID); err != nil {
		t.Fatalf("load Work creation backend: %v", err)
	}
	if _, err := creation.Exec(ctx, `set local lock_timeout = '500ms'`); err != nil {
		t.Fatalf("bound Work creation lock: %v", err)
	}
	if _, err := creation.Exec(ctx, `
		select user_id from space_memberships
		where space_id = $1 and user_id = $2
		for share
	`, manager.SpaceID, target); err != nil {
		t.Fatalf("hold target Membership for Work creation: %v", err)
	}

	removeResult := make(chan error, 1)
	command := removalCommand(t, space.RemoveMemberRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, TargetUserID: target, IdempotencyKey: "ordered-create-remove",
	})
	go func() { removeResult <- store.RemoveSpaceMember(ctx, command) }()
	waitForBlockedSQL(t, ctx, pool, creationPID, "FROM space_memberships")

	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Space lock probe: %v", err)
	}
	if _, err := probe.Exec(ctx, `set local lock_timeout = '100ms'`); err != nil {
		_ = probe.Rollback(ctx)
		t.Fatalf("bound Space lock probe: %v", err)
	}
	_, probeErr := probe.Exec(ctx, `select space_id from spaces where space_id = $1 for no key update`, manager.SpaceID)
	_ = probe.Rollback(ctx)
	var postgresError *pgconn.PgError
	if !errors.As(probeErr, &postgresError) || postgresError.Code != "55P03" {
		t.Fatalf("Space lock probe error = %v, want lock_not_available", probeErr)
	}

	if _, err := creation.Exec(ctx, `
		insert into works (
			work_id, space_id, goal, owner_user_id, creator_user_id,
			create_idempotency_key, create_request_digest
		) values ($1, $2, 'Ordered concurrent ownership', $3, $3, $4, decode(repeat('00', 32), 'hex'))
	`, uuid.NewString(), manager.SpaceID, target, "ordered-create"); err != nil {
		t.Fatalf("insert target Work while removal holds Space lock: %v", err)
	}
	if err := creation.Commit(ctx); err != nil {
		t.Fatalf("commit ordered Work creation: %v", err)
	}
	if err := <-removeResult; !errors.Is(err, space.ErrRemovalSuccessorRequired) {
		t.Fatalf("removal after ordered Work creation = %v", err)
	}
	assertRemovalState(t, ctx, pool, manager.SpaceID, target, target, true, 1)
}

func waitForBlockedSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blockerPID int32, queryFragment string) {
	t.Helper()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `
			select exists (
				select 1 from pg_stat_activity as activity
				where activity.datname = current_database()
					and $1 = any(pg_blocking_pids(activity.pid))
					and activity.query like '%' || $2 || '%'
			)
		`, blockerPID, queryFragment).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked PostgreSQL query: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for blocked PostgreSQL query %q: %v", queryFragment, ctx.Err())
		case <-poll.C:
		}
	}
}

func seedRemovalMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, spaceID string, manage, enroll bool) string {
	t.Helper()
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, 'Removal Member')`, userID); err != nil {
		t.Fatalf("seed removal User: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines)
		values ($1, $2, $3, $4)
	`, spaceID, userID, manage, enroll); err != nil {
		t.Fatalf("seed removal Membership: %v", err)
	}
	return userID
}

func seedRemovalSpaceMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, spaceID string, manage, enroll bool) string {
	t.Helper()
	if _, err := pool.Exec(ctx, `insert into spaces (space_id, name) values ($1, 'Other Space')`, spaceID); err != nil {
		t.Fatalf("seed removal Space: %v", err)
	}
	return seedRemovalMember(t, ctx, pool, spaceID, manage, enroll)
}

func removalCommand(t *testing.T, request space.RemoveMemberRequest) space.RemoveMemberCommand {
	t.Helper()
	command, err := space.NewRemoveMemberCommand(request)
	if err != nil {
		t.Fatalf("build removal command: %v", err)
	}
	return command
}

func assertRemovalState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, spaceID, target, owner string, active bool, workCount int) {
	t.Helper()
	var actualActive bool
	if err := pool.QueryRow(ctx, `select revoked_at is null from space_memberships where space_id = $1 and user_id = $2`, spaceID, target).Scan(&actualActive); err != nil {
		t.Fatalf("load target Membership: %v", err)
	}
	var actualWorks int
	if err := pool.QueryRow(ctx, `select count(*) from works where space_id = $1 and owner_user_id = $2`, spaceID, owner).Scan(&actualWorks); err != nil {
		t.Fatalf("count owned Work: %v", err)
	}
	if actualActive != active || actualWorks != workCount {
		t.Fatalf("active = %t, works for %s = %d; want %t and %d", actualActive, owner, actualWorks, active, workCount)
	}
}
