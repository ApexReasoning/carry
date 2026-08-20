//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
)

func TestSpaceInvitationAuthorityReplayProjectionAndSubmission(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	submitter := &acceptedInvitationSubmitter{}
	invitations := newTestInvitations(t, store, submitter)

	ordinaryUser, _ := seedIdentityUser(t, ctx, store, "", 0)
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines)
		values ($1, $2, false, true)
	`, manager.SpaceID, ordinaryUser); err != nil {
		t.Fatalf("seed ordinary member: %v", err)
	}
	if _, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: ordinaryUser, RecipientEmail: "unauthorized@example.com", IdempotencyKey: "unauthorized",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("unauthorized issue = %v", err)
	}

	limitedUser, _ := seedIdentityUser(t, ctx, store, "", 0)
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines)
		values ($1, $2, true, false)
	`, manager.SpaceID, limitedUser); err != nil {
		t.Fatalf("seed limited manager: %v", err)
	}
	if _, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: limitedUser, RecipientEmail: "attenuated@example.com",
		CanEnrollMachines: true, IdempotencyKey: "attenuation",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("attenuation error = %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into email_identities (canonical_email, user_id) values ('limited@example.com', $1)`, limitedUser); err != nil {
		t.Fatalf("seed active member Email: %v", err)
	}
	if _, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "limited@example.com", IdempotencyKey: "already-active",
	}); !errors.Is(err, space.ErrInvitationAlreadyMember) {
		t.Fatalf("already-active issue = %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = transaction_timestamp() where space_id = $1 and user_id = $2`, manager.SpaceID, limitedUser); err != nil {
		t.Fatalf("revoke limited manager: %v", err)
	}
	if _, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: limitedUser, RecipientEmail: "former@example.com", IdempotencyKey: "former-manager",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former manager issue = %v", err)
	}

	request := space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "  New.Member@Example.COM ",
		CanManageMembers: true, IdempotencyKey: "issue-member",
	}
	issued, err := invitations.Issue(ctx, request)
	if err != nil {
		t.Fatalf("issue invitation: %v", err)
	}
	if issued.RecipientEmail != "new.member@example.com" || issued.Submission.State != space.InvitationSubmissionAccepted || submitter.calls != 1 {
		t.Fatalf("issued = %#v, calls = %d", issued, submitter.calls)
	}
	replayed, err := invitations.Issue(ctx, request)
	if err != nil {
		t.Fatalf("replay invitation: %v", err)
	}
	if replayed.InvitationID != issued.InvitationID || replayed.Submission.SubmissionID != issued.Submission.SubmissionID || submitter.calls != 1 {
		t.Fatalf("replay = %#v, calls = %d", replayed, submitter.calls)
	}
	request.RecipientEmail = "changed@example.com"
	if _, err := invitations.Issue(ctx, request); !errors.Is(err, space.ErrIdempotencyConflict) {
		t.Fatalf("changed issue replay = %v", err)
	}
	managed, err := invitations.ListForSpace(ctx, manager.UserID, manager.SpaceID)
	if err != nil {
		t.Fatalf("list managed invitations: %v", err)
	}
	if len(managed) != 1 || managed[0].RecipientEmail != "new.member@example.com" || managed[0].Submission.State != space.InvitationSubmissionAccepted {
		t.Fatalf("managed = %#v", managed)
	}
	if _, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "NEW.MEMBER@example.com", IdempotencyKey: "duplicate",
	}); !errors.Is(err, space.ErrInvitationConflict) {
		t.Fatalf("duplicate invitation = %v", err)
	}
}

func TestSpaceInvitationReplayEligibilityAndExactSpaceAuthority(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	submitter := &acceptedInvitationSubmitter{}
	invitations := newTestInvitations(t, store, submitter)
	request := space.IssueInvitationRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "eligibility@example.com", IdempotencyKey: "eligibility"}
	issued, err := invitations.Issue(ctx, request)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_invitation_submissions set state = 'prepared', provider_message_id = null, recorded_at = null, created_at = transaction_timestamp() - interval '24 hours' where submission_id = $1`, issued.Submission.SubmissionID); err != nil {
		t.Fatalf("age submission: %v", err)
	}
	if _, err := invitations.Issue(ctx, request); err != nil {
		t.Fatalf("aged replay: %v", err)
	}
	if submitter.calls != 1 {
		t.Fatalf("aged replay calls = %d", submitter.calls)
	}

	unknownSubmitter := &acceptedInvitationSubmitter{state: space.InvitationSubmissionUnknown}
	unknownInvitations := newTestInvitations(t, store, unknownSubmitter)
	unknownRequest := space.IssueInvitationRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "unknown@example.com", IdempotencyKey: "unknown"}
	if _, err := unknownInvitations.Issue(ctx, unknownRequest); err != nil {
		t.Fatalf("unknown issue: %v", err)
	}
	if _, err := unknownInvitations.Issue(ctx, unknownRequest); err != nil {
		t.Fatalf("unknown replay: %v", err)
	}
	if unknownSubmitter.calls != 1 {
		t.Fatalf("unknown replay calls = %d", unknownSubmitter.calls)
	}

	acceptedRequest := space.IssueInvitationRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "terminal-accepted@example.com", IdempotencyKey: "terminal-accepted"}
	acceptedIssue, err := invitations.Issue(ctx, acceptedRequest)
	if err != nil {
		t.Fatalf("issue accepted terminal: %v", err)
	}
	acceptedUser, sessions := seedIdentityUser(t, ctx, store, "terminal-accepted@example.com", 1)
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{InvitationID: acceptedIssue.InvitationID, UserID: acceptedUser, SessionID: sessions[0], DisplayName: "Accepted", IdempotencyKey: "accept-terminal"}); err != nil {
		t.Fatalf("accept terminal: %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_invitation_submissions set state = 'prepared', provider_message_id = null, recorded_at = null where submission_id = $1`, acceptedIssue.Submission.SubmissionID); err != nil {
		t.Fatalf("prepare accepted submission: %v", err)
	}
	beforeAccepted := submitter.calls
	if _, err := invitations.Issue(ctx, acceptedRequest); err != nil {
		t.Fatalf("accepted replay: %v", err)
	}
	if submitter.calls != beforeAccepted {
		t.Fatalf("accepted replay calls = %d, want %d", submitter.calls, beforeAccepted)
	}

	revokedRequest := space.IssueInvitationRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "terminal-revoked@example.com", IdempotencyKey: "terminal-revoked"}
	revokedIssue, err := invitations.Issue(ctx, revokedRequest)
	if err != nil {
		t.Fatalf("issue revoked terminal: %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_invitation_submissions set state = 'prepared', provider_message_id = null, recorded_at = null where submission_id = $1`, revokedIssue.Submission.SubmissionID); err != nil {
		t.Fatalf("prepare revoked submission: %v", err)
	}
	if err := invitations.Revoke(ctx, space.RevokeInvitationCommand{SpaceID: manager.SpaceID, InvitationID: revokedIssue.InvitationID, ActorUserID: manager.UserID, IdempotencyKey: "revoke-terminal"}); err != nil {
		t.Fatalf("revoke terminal: %v", err)
	}
	beforeRevoked := submitter.calls
	if _, err := invitations.Issue(ctx, revokedRequest); err != nil {
		t.Fatalf("revoked replay: %v", err)
	}
	if submitter.calls != beforeRevoked {
		t.Fatalf("revoked replay calls = %d, want %d", submitter.calls, beforeRevoked)
	}

	if _, err := invitations.Resend(ctx, space.ResendInvitationRequest{SpaceID: manager.SpaceID, InvitationID: issued.InvitationID, ActorUserID: manager.UserID, IdempotencyKey: "cross-space"}); err != nil {
		t.Fatalf("prepare Space A resend: %v", err)
	}
	spaceB := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into spaces (space_id, name) values ($1, 'Space B')`, spaceB); err != nil {
		t.Fatalf("create Space B: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines) values ($1, $2, true, false)`, spaceB, manager.UserID); err != nil {
		t.Fatalf("grant Space B: %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = transaction_timestamp() where space_id = $1 and user_id = $2`, manager.SpaceID, manager.UserID); err != nil {
		t.Fatalf("revoke Space A: %v", err)
	}
	before := submitter.calls
	if _, err := invitations.Resend(ctx, space.ResendInvitationRequest{SpaceID: spaceB, InvitationID: issued.InvitationID, ActorUserID: manager.UserID, IdempotencyKey: "cross-space"}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("cross-Space resend = %v", err)
	}
	if submitter.calls != before {
		t.Fatalf("cross-Space calls = %d, want %d", submitter.calls, before)
	}
}

func TestSpaceInvitationRecordsObservedOutcomeAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool := openMigratedTestPool(t, context.Background())
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, context.Background(), store)
	submitter := &acceptedInvitationSubmitter{cancel: cancel}
	invitations := newTestInvitations(t, store, submitter)
	request := space.IssueInvitationRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "cancelled@example.com", IdempotencyKey: "cancelled"}
	issued, err := invitations.Issue(ctx, request)
	if err != nil || issued.Submission.State != space.InvitationSubmissionAccepted {
		t.Fatalf("cancelled issue = %#v, %v", issued, err)
	}
	replayed, err := invitations.Issue(context.Background(), request)
	if err != nil || replayed.Submission.State != space.InvitationSubmissionAccepted || submitter.calls != 1 {
		t.Fatalf("cancelled replay = %#v, calls %d, %v", replayed, submitter.calls, err)
	}
}

func TestSpaceInvitationSubmissionCommitResponseLossRecoversWithoutAnotherSend(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	lossStore := &invitationRecordResponseLossStore{Store: store}
	submitter := &acceptedInvitationSubmitter{}
	invitations := newTestInvitations(t, lossStore, submitter)
	request := space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID,
		RecipientEmail: "response-loss@example.com", IdempotencyKey: "issue-response-loss",
	}
	if _, err := invitations.Issue(ctx, request); err == nil {
		t.Fatal("first issue unexpectedly observed recorded response")
	}
	recovered, err := invitations.Issue(ctx, request)
	if err != nil {
		t.Fatalf("recover issue after response loss: %v", err)
	}
	if recovered.Submission.State != space.InvitationSubmissionAccepted || submitter.calls != 1 {
		t.Fatalf("recovered = %#v, calls = %d", recovered, submitter.calls)
	}
}

func TestSpaceInvitationRequiresExactRecentEmailProofAndReplaysAcceptance(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "invitee@example.com",
		CanManageMembers: true, IdempotencyKey: "issue-invitee",
	})
	if err != nil {
		t.Fatalf("issue invitation: %v", err)
	}
	providerOnlyUserID, _ := seedIdentityUser(t, ctx, store, "", 0)
	providerOnlySession := createIdentityTestSession(t, ctx, store, providerOnlyUserID, identity.GoogleMethod)
	if providerInbox, err := invitations.ListForUser(ctx, providerOnlyUserID, providerOnlySession); err != nil || len(providerInbox.Invitations) != 0 {
		t.Fatalf("provider-only inbox = %#v, %v", providerInbox, err)
	}

	wrongUserID, wrongSessions := seedIdentityUser(t, ctx, store, "wrong@example.com", 1)
	if wrongInbox, err := invitations.ListForUser(ctx, wrongUserID, wrongSessions[0]); err != nil || len(wrongInbox.Invitations) != 0 {
		t.Fatalf("wrong-email inbox = %#v, %v", wrongInbox, err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: wrongUserID, SessionID: wrongSessions[0],
		DisplayName: "Wrong User", IdempotencyKey: "accept-wrong",
	}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("wrong-email acceptance = %v", err)
	}

	inviteeID, _ := seedIdentityUser(t, ctx, store, "invitee@example.com", 0)
	googleSession := createIdentityTestSession(t, ctx, store, inviteeID, identity.GoogleMethod)
	inbox, err := invitations.ListForUser(ctx, inviteeID, googleSession)
	if err != nil {
		t.Fatalf("list Google inbox: %v", err)
	}
	if len(inbox.Invitations) != 1 || !inbox.ReauthenticationRequired {
		t.Fatalf("Google inbox = %#v", inbox)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: googleSession,
		DisplayName: "Invited Member", IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, space.ErrInvitationProofRequired) {
		t.Fatalf("Google acceptance = %v", err)
	}
	var memberships int
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships where space_id = $1 and user_id = $2`, manager.SpaceID, inviteeID).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("pre-proof memberships = %d, err = %v", memberships, err)
	}
	emailSession := createIdentityTestSession(t, ctx, store, inviteeID, identity.EmailMethod)
	if _, err := pool.Exec(ctx, `update browser_sessions set created_at = created_at - interval '11 minutes', identity_proved_at = identity_proved_at - interval '11 minutes' where session_id = $1`, emailSession); err != nil {
		t.Fatalf("age Email proof: %v", err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		DisplayName: "Invited Member", IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, space.ErrInvitationProofRequired) {
		t.Fatalf("stale Email acceptance = %v", err)
	}
	emailSession = createIdentityTestSession(t, ctx, store, inviteeID, identity.EmailMethod)
	accepted, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		DisplayName: "Invited Member", IdempotencyKey: "accept-invitee",
	})
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if !accepted.CanManageMembers || accepted.CanEnrollMachines || accepted.AlreadyMember {
		t.Fatalf("accepted = %#v", accepted)
	}
	replayed, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		DisplayName: "Invited Member", IdempotencyKey: "accept-invitee",
	})
	if err != nil || replayed != accepted {
		t.Fatalf("accept replay = %#v, %v", replayed, err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		DisplayName: "Changed Name", IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, space.ErrIdempotencyConflict) {
		t.Fatalf("changed accept replay = %v", err)
	}
	var name *string
	if err := pool.QueryRow(ctx, `select display_name from carry_users where user_id = $1`, inviteeID).Scan(&name); err != nil || name == nil || *name != "Invited Member" {
		t.Fatalf("display name = %#v, err = %v", name, err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = transaction_timestamp() where space_id = $1 and user_id = $2`, manager.SpaceID, inviteeID); err != nil {
		t.Fatalf("revoke resulting Membership: %v", err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		DisplayName: "Invited Member", IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("replay after Membership removal = %v", err)
	}
}

func TestSpaceInvitationAlreadyMemberUnchangedAndDatabaseTimeExpiry(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "race-member@example.com",
		CanManageMembers: true, CanEnrollMachines: true, IdempotencyKey: "issue-race-member",
	})
	if err != nil {
		t.Fatalf("issue already-member race invitation: %v", err)
	}
	userID, sessions := seedIdentityUser(t, ctx, store, "race-member@example.com", 1)
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines)
		values ($1, $2, false, false)
	`, manager.SpaceID, userID); err != nil {
		t.Fatalf("seed concurrent Membership: %v", err)
	}
	accepted, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: userID, SessionID: sessions[0],
		DisplayName: "Already Member", IdempotencyKey: "accept-already",
	})
	if err != nil {
		t.Fatalf("accept as already member: %v", err)
	}
	if !accepted.AlreadyMember || accepted.CanManageMembers || accepted.CanEnrollMachines {
		t.Fatalf("already member result = %#v", accepted)
	}

	expired, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "expired@example.com", IdempotencyKey: "issue-expired",
	})
	if err != nil {
		t.Fatalf("issue expiring invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update space_invitations
		set created_at = created_at - interval '8 days', expires_at = expires_at - interval '8 days'
		where invitation_id = $1
	`, expired.InvitationID); err != nil {
		t.Fatalf("age invitation: %v", err)
	}
	expiredUser, expiredSessions := seedIdentityUser(t, ctx, store, "expired@example.com", 1)
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: expired.InvitationID, UserID: expiredUser, SessionID: expiredSessions[0],
		DisplayName: "Expired User", IdempotencyKey: "accept-expired",
	}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("expired acceptance = %v", err)
	}
}

func TestInvitationResendCooldownReplayAndRevoke(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	submitter := &acceptedInvitationSubmitter{}
	invitations := newTestInvitations(t, store, submitter)
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "resend@example.com", IdempotencyKey: "issue-resend",
	})
	if err != nil {
		t.Fatalf("issue resend invitation: %v", err)
	}
	ordinaryUser, _ := seedIdentityUser(t, ctx, store, "", 0)
	if _, err := pool.Exec(ctx, `insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines) values ($1, $2, false, false)`, manager.SpaceID, ordinaryUser); err != nil {
		t.Fatalf("seed ordinary member: %v", err)
	}
	if _, err := invitations.Resend(ctx, space.ResendInvitationRequest{SpaceID: manager.SpaceID, InvitationID: issued.InvitationID, ActorUserID: ordinaryUser, IdempotencyKey: "unauthorized-resend"}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("unauthorized resend = %v", err)
	}
	if err := invitations.Revoke(ctx, space.RevokeInvitationCommand{SpaceID: manager.SpaceID, InvitationID: issued.InvitationID, ActorUserID: ordinaryUser, IdempotencyKey: "unauthorized-revoke"}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("unauthorized revoke = %v", err)
	}
	request := space.ResendInvitationRequest{SpaceID: manager.SpaceID, InvitationID: issued.InvitationID, ActorUserID: manager.UserID, IdempotencyKey: "resend-one"}
	if _, err := invitations.Resend(ctx, request); !errors.Is(err, space.ErrInvitationResendCooldown) {
		t.Fatalf("early resend = %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_invitation_submissions set created_at = created_at - interval '61 seconds' where invitation_id = $1`, issued.InvitationID); err != nil {
		t.Fatalf("age initial submission: %v", err)
	}
	resent, err := invitations.Resend(ctx, request)
	if err != nil {
		t.Fatalf("resend invitation: %v", err)
	}
	if resent.ExpiresAt != issued.ExpiresAt || submitter.calls != 2 {
		t.Fatalf("resent = %#v, calls = %d", resent, submitter.calls)
	}
	replayed, err := invitations.Resend(ctx, request)
	if err != nil || replayed.Submission.SubmissionID != resent.Submission.SubmissionID || submitter.calls != 2 {
		t.Fatalf("resend replay = %#v, %v, calls = %d", replayed, err, submitter.calls)
	}
	revoke := space.RevokeInvitationCommand{SpaceID: manager.SpaceID, InvitationID: issued.InvitationID, ActorUserID: manager.UserID, IdempotencyKey: "revoke-resend"}
	if err := invitations.Revoke(ctx, revoke); err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	if err := invitations.Revoke(ctx, revoke); err != nil {
		t.Fatalf("revoke replay: %v", err)
	}
	userID, sessions := seedIdentityUser(t, ctx, store, "resend@example.com", 1)
	inbox, err := invitations.ListForUser(ctx, userID, sessions[0])
	if err != nil || len(inbox.Invitations) != 0 {
		t.Fatalf("revoked inbox = %#v, %v", inbox, err)
	}
}

func TestConcurrentInvitationAcceptsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "two-accepts@example.com", IdempotencyKey: "issue-two-accepts"})
	if err != nil {
		t.Fatalf("issue two-accept invitation: %v", err)
	}
	userID, sessions := seedIdentityUser(t, ctx, store, "two-accepts@example.com", 1)
	start := make(chan struct{})
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for _, key := range []string{"accept-one", "accept-two"} {
		key := key
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := invitations.Accept(ctx, space.AcceptInvitationCommand{InvitationID: issued.InvitationID, UserID: userID, SessionID: sessions[0], DisplayName: "Two Accepts", IdempotencyKey: key})
			errorsSeen <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	var successes, conflicts int
	for err := range errorsSeen {
		if err == nil {
			successes++
		} else if errors.Is(err, space.ErrIdempotencyConflict) {
			conflicts++
		} else {
			t.Fatalf("accept error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d", successes, conflicts)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships where space_id = $1 and user_id = $2 and revoked_at is null`, manager.SpaceID, userID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Membership count = %d, err = %v", count, err)
	}
}

func TestConcurrentInvitationAcceptAndRevokeHaveOneTerminalWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := bootstrapInvitationManager(t, ctx, store)
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID: manager.SpaceID, ActorUserID: manager.UserID, RecipientEmail: "concurrent@example.com", IdempotencyKey: "issue-concurrent",
	})
	if err != nil {
		t.Fatalf("issue concurrent invitation: %v", err)
	}
	userID, sessions := seedIdentityUser(t, ctx, store, "concurrent@example.com", 1)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	var acceptErr, revokeErr error
	go func() {
		defer wait.Done()
		<-start
		_, acceptErr = invitations.Accept(ctx, space.AcceptInvitationCommand{InvitationID: issued.InvitationID, UserID: userID, SessionID: sessions[0], DisplayName: "Concurrent", IdempotencyKey: "accept-concurrent"})
	}()
	go func() {
		defer wait.Done()
		<-start
		revokeErr = invitations.Revoke(ctx, space.RevokeInvitationCommand{SpaceID: manager.SpaceID, InvitationID: issued.InvitationID, ActorUserID: manager.UserID, IdempotencyKey: "revoke-concurrent"})
	}()
	close(start)
	wait.Wait()
	if (acceptErr == nil) == (revokeErr == nil) {
		t.Fatalf("accept err = %v, revoke err = %v", acceptErr, revokeErr)
	}
	var acceptedAt, revokedAt any
	if err := pool.QueryRow(ctx, `select accepted_at, revoked_at from space_invitations where invitation_id = $1`, issued.InvitationID).Scan(&acceptedAt, &revokedAt); err != nil {
		t.Fatalf("load terminal invitation: %v", err)
	}
	if (acceptedAt == nil) == (revokedAt == nil) {
		t.Fatalf("accepted_at = %#v, revoked_at = %#v", acceptedAt, revokedAt)
	}
}

func bootstrapInvitationManager(t *testing.T, ctx context.Context, store *Store) BootstrapResult {
	t.Helper()
	result, err := bootstrapForTest(ctx, store, BootstrapCommand{
		DisplayName: "Invitation Manager", SpaceName: "Invitation Space", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap invitation manager: %v", err)
	}
	return result
}

func newTestInvitations(t *testing.T, persistence space.InvitationPersistence, submitter space.InvitationSubmitter) *space.Invitations {
	t.Helper()
	invitations, err := space.NewInvitations(persistence, submitter, "https://carry.example/invitations")
	if err != nil {
		t.Fatalf("create invitation behavior: %v", err)
	}
	return invitations
}

type invitationRecordResponseLossStore struct {
	*Store
	lost bool
}

func (store *invitationRecordResponseLossStore) RecordInvitationSubmission(ctx context.Context, command space.RecordInvitationSubmissionCommand) (space.InvitationSubmission, error) {
	recorded, err := store.Store.RecordInvitationSubmission(ctx, command)
	if err == nil && !store.lost {
		store.lost = true
		return recorded, errors.New("simulated committed response loss")
	}
	return recorded, err
}

type acceptedInvitationSubmitter struct {
	calls  int
	state  space.InvitationSubmissionState
	cancel context.CancelFunc
}

func (submitter *acceptedInvitationSubmitter) InvitationPayloadDigest(message space.InvitationMessage) ([32]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.DestinationURL + "\x00" + message.IdempotencyKey)), nil
}

func (submitter *acceptedInvitationSubmitter) SubmitInvitation(_ context.Context, _ space.InvitationMessage, _ [32]byte) space.InvitationSubmission {
	submitter.calls++
	if submitter.cancel != nil {
		submitter.cancel()
	}
	if submitter.state == space.InvitationSubmissionUnknown {
		return space.InvitationSubmission{State: space.InvitationSubmissionUnknown}
	}
	return space.InvitationSubmission{State: space.InvitationSubmissionAccepted, ProviderMessageID: "accepted-invitation"}
}
