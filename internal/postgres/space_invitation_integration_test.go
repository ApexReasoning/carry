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
	manager := invitationManagerFixture(t, ctx, store)
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
	manager := invitationManagerFixture(t, ctx, store)
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
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID:   acceptedIssue.InvitationID,
		UserID:         acceptedUser,
		SessionID:      sessions[0],
		IdempotencyKey: "accept-terminal",
	}); err != nil {
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
	if _, err := pool.Exec(ctx, `insert into spaces (space_id, name, slug) values ($1::uuid, 'Space B', replace(($1::uuid)::text, '-', ''))`, spaceB); err != nil {
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
	manager := invitationManagerFixture(t, context.Background(), store)
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

func TestSpaceInvitationIssueReplayBuildsPayloadFromPersistedInvitationID(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	lossStore := &invitationPrepareResponseLossStore{Store: store}
	submitter := &acceptedInvitationSubmitter{}
	invitations := newTestInvitations(t, lossStore, submitter)
	request := space.IssueInvitationRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		RecipientEmail: "persisted-link@example.com",
		IdempotencyKey: "persisted-link",
	}
	if _, err := invitations.Issue(ctx, request); err == nil {
		t.Fatal("first prepared response was not lost")
	}
	persisted, err := invitations.Issue(ctx, request)
	if err != nil {
		t.Fatalf("replay issue: %v", err)
	}
	if len(submitter.messages) != 1 {
		t.Fatalf("submitted messages = %d", len(submitter.messages))
	}
	if len(lossStore.invitationIDs) != 2 {
		t.Fatalf("prepared invitation IDs = %#v", lossStore.invitationIDs)
	}
	if lossStore.invitationIDs[0] == lossStore.invitationIDs[1] {
		t.Fatalf("fresh replay ID was not regenerated: %#v", lossStore.invitationIDs)
	}
	if persisted.InvitationID != lossStore.invitationIDs[0] {
		t.Fatalf("persisted invitation ID = %q, commands %#v", persisted.InvitationID, lossStore.invitationIDs)
	}
	wantURL := "https://carry.example/invitations/" + persisted.InvitationID
	if submitter.messages[0].DestinationURL != wantURL {
		t.Fatalf("replayed payload URL = %q, want %q", submitter.messages[0].DestinationURL, wantURL)
	}
}

func TestSpaceInvitationSubmissionCommitResponseLossRecoversWithoutAnotherSend(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
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
	manager := invitationManagerFixture(t, ctx, store)
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
		IdempotencyKey: "accept-wrong",
	}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("wrong-email acceptance = %v", err)
	}
	var wrongMemberships int
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships where space_id=$1 and user_id=$2`, manager.SpaceID, wrongUserID).Scan(&wrongMemberships); err != nil || wrongMemberships != 0 {
		t.Fatalf("non-owner acceptance consequences = %d, %v", wrongMemberships, err)
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
		IdempotencyKey: "accept-invitee",
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
		IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, space.ErrInvitationProofRequired) {
		t.Fatalf("stale Email acceptance = %v", err)
	}
	emailSession = createIdentityTestSession(t, ctx, store, inviteeID, identity.EmailMethod)
	accepted, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		IdempotencyKey: "accept-invitee",
	})
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if !accepted.CanManageMembers || accepted.CanEnrollMachines || accepted.AlreadyMember {
		t.Fatalf("accepted = %#v", accepted)
	}
	replayed, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		IdempotencyKey: "accept-invitee",
	})
	if err != nil || replayed != accepted {
		t.Fatalf("accept replay = %#v, %v", replayed, err)
	}
	if _, err := pool.Exec(ctx, `update space_invitations set created_at=created_at-interval '8 days', expires_at=expires_at-interval '8 days' where invitation_id=$1`, issued.InvitationID); err != nil {
		t.Fatalf("expire committed invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `update browser_sessions set created_at=created_at-interval '11 minutes', identity_proved_at=identity_proved_at-interval '11 minutes' where session_id=$1`, emailSession); err != nil {
		t.Fatalf("age committed acceptance proof: %v", err)
	}
	replayed, err = invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		IdempotencyKey: "accept-invitee",
	})
	if err != nil || replayed != accepted {
		t.Fatalf("accepted replay after expiry and proof age = %#v, %v", replayed, err)
	}
	replayed, err = invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: googleSession,
		IdempotencyKey: "accept-invitee",
	})
	if err != nil || replayed != accepted {
		t.Fatalf("accepted replay from current non-Email session = %#v, %v", replayed, err)
	}
	if _, err := pool.Exec(ctx, `update browser_sessions set created_at=clock_timestamp()-interval '2 hours', expires_at=clock_timestamp()-interval '1 hour' where session_id=$1`, googleSession); err != nil {
		t.Fatalf("expire replay Browser Session: %v", err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: googleSession,
		IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("accepted replay with expired Browser Session = %v", err)
	}
	expectedName, err := identity.FallbackDisplayName(inviteeID)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := pool.QueryRow(ctx, `select display_name from carry_users where user_id = $1`, inviteeID).Scan(&name); err != nil || name != expectedName {
		t.Fatalf("display name = %q, want %q, err = %v", name, expectedName, err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = transaction_timestamp() where space_id = $1 and user_id = $2`, manager.SpaceID, inviteeID); err != nil {
		t.Fatalf("revoke resulting Membership: %v", err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID: issued.InvitationID, UserID: inviteeID, SessionID: emailSession,
		IdempotencyKey: "accept-invitee",
	}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("replay after Membership removal = %v", err)
	}
}

func TestSpaceInvitationAlreadyMemberUnchangedAndDatabaseTimeExpiry(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
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
		IdempotencyKey: "accept-already",
	})
	if err != nil {
		t.Fatalf("accept as already member: %v", err)
	}
	if !accepted.AlreadyMember || accepted.CanManageMembers || accepted.CanEnrollMachines {
		t.Fatalf("already member result = %#v", accepted)
	}
	alreadyMemberProjection, err := invitations.LoadForUser(ctx, issued.InvitationID, userID, sessions[0])
	if err != nil {
		t.Fatalf("load already-member projection: %v", err)
	}
	if alreadyMemberProjection.State != space.InvitationAccepted {
		t.Fatalf("already-member state = %#v", alreadyMemberProjection)
	}
	if alreadyMemberProjection.AcceptResult != "already_member" {
		t.Fatalf("already-member result = %#v", alreadyMemberProjection)
	}
	if !alreadyMemberProjection.CurrentMember {
		t.Fatalf("already-member Membership = %#v", alreadyMemberProjection)
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
		IdempotencyKey: "accept-expired",
	}); !errors.Is(err, space.ErrInvitationExpired) {
		t.Fatalf("expired acceptance = %v", err)
	}
}

func TestInvitationResendCooldownReplayAndRevoke(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
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
	if len(submitter.messages) != 2 || submitter.messages[0].DestinationURL != submitter.messages[1].DestinationURL {
		t.Fatalf("resend URLs = %#v", submitter.messages)
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
	replayed, err = invitations.Resend(ctx, request)
	if err != nil || replayed.Submission.SubmissionID != resent.Submission.SubmissionID || submitter.calls != 2 {
		t.Fatalf("resend replay after terminal state = %#v, %v, calls = %d", replayed, err, submitter.calls)
	}
	freshAfterRevoke := request
	freshAfterRevoke.IdempotencyKey = "resend-after-revoke"
	if _, err := invitations.Resend(ctx, freshAfterRevoke); !errors.Is(err, space.ErrInvitationRevoked) {
		t.Fatalf("fresh resend after revoke = %v", err)
	}
	userID, sessions := seedIdentityUser(t, ctx, store, "resend@example.com", 1)
	inbox, err := invitations.ListForUser(ctx, userID, sessions[0])
	if err != nil || len(inbox.Invitations) != 0 {
		t.Fatalf("revoked inbox = %#v, %v", inbox, err)
	}
}

func TestInvitationResendReplayUsesFreshClockAfterAuthorityWait(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	submitter := &acceptedInvitationSubmitter{}
	invitations := newTestInvitations(t, store, submitter)
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		RecipientEmail: "resend-replay-clock@example.com",
		IdempotencyKey: "issue-resend-replay-clock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update space_invitation_submissions set created_at=created_at-interval '61 seconds' where invitation_id=$1`, issued.InvitationID); err != nil {
		t.Fatal(err)
	}
	requestDigest := sha256.Sum256([]byte(`{"invitation_id":"` + issued.InvitationID + `"}`))
	submissionID := uuid.NewString()
	providerKey := "space-invitation/" + submissionID
	payloadDigest, err := submitter.InvitationPayloadDigest(space.InvitationMessage{
		Recipient:      issued.RecipientEmail,
		DestinationURL: "https://carry.example/invitations/" + issued.InvitationID,
		IdempotencyKey: providerKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := space.PrepareInvitationResendCommand{
		SpaceID:                manager.SpaceID,
		InvitationID:           issued.InvitationID,
		ActorUserID:            manager.UserID,
		SubmissionID:           submissionID,
		IdempotencyKey:         "resend-replay-clock",
		RequestDigest:          requestDigest,
		ProviderIdempotencyKey: providerKey,
		PayloadDigest:          payloadDigest,
	}
	prepared, err := store.PrepareInvitationResend(ctx, command)
	if err != nil || !prepared.Submission.SubmitEligible {
		t.Fatalf("prepare resend = %#v, %v", prepared, err)
	}
	if _, err := pool.Exec(ctx, `
		update space_invitations
		set created_at=transaction_timestamp()-interval '7 days'+interval '400 milliseconds',
		    expires_at=transaction_timestamp()+interval '400 milliseconds'
		where invitation_id=$1
	`, issued.InvitationID); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `select user_id from space_memberships where space_id=$1 and user_id=$2 for update`, manager.SpaceID, manager.UserID); err != nil {
		t.Fatal(err)
	}
	type replayResult struct {
		invitation space.IssuedInvitation
		err        error
	}
	result := make(chan replayResult, 1)
	go func() {
		replayed, replayErr := store.PrepareInvitationResend(ctx, command)
		result <- replayResult{invitation: replayed, err: replayErr}
	}()
	time.Sleep(700 * time.Millisecond)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	replayed := <-result
	if replayed.err != nil {
		t.Fatalf("replay resend: %v", replayed.err)
	}
	if replayed.invitation.Submission.SubmitEligible {
		t.Fatalf("expired replay remained submit eligible: %#v", replayed.invitation)
	}
}

func TestTargetedInvitationProjectsOnlyExactOwnerAndTerminalTruth(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})
	issued, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		RecipientEmail: "targeted@example.com",
		IdempotencyKey: "issue-targeted",
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerID, _ := seedIdentityUser(t, ctx, store, "targeted@example.com", 0)
	providerSession := createIdentityTestSession(t, ctx, store, ownerID, identity.GoogleMethod)
	pending, err := invitations.LoadForUser(ctx, issued.InvitationID, ownerID, providerSession)
	if err != nil {
		t.Fatalf("load pending projection: %v", err)
	}
	if pending.State != space.InvitationPending {
		t.Fatalf("pending state = %#v", pending)
	}
	if !pending.ReauthenticationRequired {
		t.Fatalf("pending proof = %#v", pending)
	}
	if pending.SpaceName != "Invitation Space" {
		t.Fatalf("pending Space = %#v", pending)
	}
	wrongID, wrongSessions := seedIdentityUser(t, ctx, store, "other-targeted@example.com", 1)
	for _, unavailableID := range []string{issued.InvitationID, uuid.NewString()} {
		if _, err := invitations.LoadForUser(ctx, unavailableID, wrongID, wrongSessions[0]); !errors.Is(err, space.ErrInvitationUnavailable) {
			t.Fatalf("non-owner projection %s = %v", unavailableID, err)
		}
	}
	acceptedByOther, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		RecipientEmail: "accepted-by-other@example.com",
		IdempotencyKey: "issue-accepted-by-other",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherOwnerID, otherOwnerSessions := seedIdentityUser(t, ctx, store, "accepted-by-other@example.com", 1)
	if _, err := pool.Exec(ctx, `
		update space_invitations
		set accepted_by_user_id=$2, accepted_at=clock_timestamp(), accept_result='joined',
			accept_idempotency_key='accepted-by-other',
			accept_request_digest=decode(repeat('01',32),'hex')
		where invitation_id=$1
	`, acceptedByOther.InvitationID, manager.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := invitations.LoadForUser(ctx, acceptedByOther.InvitationID, otherOwnerID, otherOwnerSessions[0]); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("accepted-by-other owner projection = %v", err)
	}
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID:   acceptedByOther.InvitationID,
		UserID:         otherOwnerID,
		SessionID:      otherOwnerSessions[0],
		IdempotencyKey: "accept-as-later-email-owner",
	}); !errors.Is(err, space.ErrInvitationUnavailable) {
		t.Fatalf("accepted-by-other owner acceptance = %v", err)
	}
	var laterOwnerMemberships int
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships where space_id=$1 and user_id=$2`, manager.SpaceID, otherOwnerID).Scan(&laterOwnerMemberships); err != nil {
		t.Fatal(err)
	}
	if laterOwnerMemberships != 0 {
		t.Fatalf("later Email owner Memberships = %d", laterOwnerMemberships)
	}
	emailSession := createIdentityTestSession(t, ctx, store, ownerID, identity.EmailMethod)
	if _, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
		InvitationID:   issued.InvitationID,
		UserID:         ownerID,
		SessionID:      emailSession,
		IdempotencyKey: "accept-targeted",
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := invitations.LoadForUser(ctx, issued.InvitationID, ownerID, emailSession)
	if err != nil {
		t.Fatalf("load accepted projection: %v", err)
	}
	if accepted.State != space.InvitationAccepted {
		t.Fatalf("accepted state = %#v", accepted)
	}
	if accepted.AcceptResult != "joined" {
		t.Fatalf("accepted result = %#v", accepted)
	}
	if !accepted.CurrentMember {
		t.Fatalf("accepted Membership = %#v", accepted)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at=clock_timestamp() where space_id=$1 and user_id=$2`, manager.SpaceID, ownerID); err != nil {
		t.Fatal(err)
	}
	former, err := invitations.LoadForUser(ctx, issued.InvitationID, ownerID, emailSession)
	if err != nil || former.CurrentMember {
		t.Fatalf("former projection = %#v, %v", former, err)
	}

	for _, state := range []space.InvitationState{space.InvitationRevoked, space.InvitationExpired} {
		email := string(state) + "-targeted@example.com"
		terminal, issueErr := invitations.Issue(ctx, space.IssueInvitationRequest{
			SpaceID:        manager.SpaceID,
			ActorUserID:    manager.UserID,
			RecipientEmail: email,
			IdempotencyKey: "issue-" + string(state),
		})
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		terminalOwner, _ := seedIdentityUser(t, ctx, store, email, 0)
		terminalSession := createIdentityTestSession(t, ctx, store, terminalOwner, identity.EmailMethod)
		if state == space.InvitationRevoked {
			if err := invitations.Revoke(ctx, space.RevokeInvitationCommand{
				SpaceID:        manager.SpaceID,
				InvitationID:   terminal.InvitationID,
				ActorUserID:    manager.UserID,
				IdempotencyKey: "revoke-targeted",
			}); err != nil {
				t.Fatal(err)
			}
		} else if _, err := pool.Exec(ctx, `update space_invitations set created_at=created_at-interval '8 days', expires_at=expires_at-interval '8 days' where invitation_id=$1`, terminal.InvitationID); err != nil {
			t.Fatal(err)
		}
		projected, err := invitations.LoadForUser(ctx, terminal.InvitationID, terminalOwner, terminalSession)
		if err != nil || projected.State != state {
			t.Fatalf("%s projection = %#v, %v", state, projected, err)
		}
	}
}

func TestInvitationAcceptAndRevokeUseClockAfterInvitationLockWait(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	invitations := newTestInvitations(t, store, &acceptedInvitationSubmitter{})

	acceptIssue, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		RecipientEmail: "wait-accept@example.com",
		IdempotencyKey: "issue-wait-accept",
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptUser, acceptSessions := seedIdentityUser(t, ctx, store, "wait-accept@example.com", 1)
	if _, err := pool.Exec(ctx, `update space_invitations set created_at=boundary.expires_at-interval '7 days', expires_at=boundary.expires_at from (select clock_timestamp()+interval '400 milliseconds' as expires_at) as boundary where invitation_id=$1`, acceptIssue.InvitationID); err != nil {
		t.Fatal(err)
	}
	acceptLock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acceptLock.Exec(ctx, `select invitation_id from space_invitations where invitation_id=$1 for update`, acceptIssue.InvitationID); err != nil {
		t.Fatal(err)
	}
	acceptResult := make(chan error, 1)
	go func() {
		_, acceptErr := invitations.Accept(ctx, space.AcceptInvitationCommand{
			InvitationID:   acceptIssue.InvitationID,
			UserID:         acceptUser,
			SessionID:      acceptSessions[0],
			IdempotencyKey: "accept-after-wait",
		})
		acceptResult <- acceptErr
	}()
	time.Sleep(700 * time.Millisecond)
	if err := acceptLock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-acceptResult; !errors.Is(err, space.ErrInvitationExpired) {
		t.Fatalf("post-wait accept = %v", err)
	}
	var membershipCount int
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships where space_id=$1 and user_id=$2`, manager.SpaceID, acceptUser).Scan(&membershipCount); err != nil || membershipCount != 0 {
		t.Fatalf("expired accept consequences = %d, %v", membershipCount, err)
	}

	revokeIssue, err := invitations.Issue(ctx, space.IssueInvitationRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		RecipientEmail: "wait-revoke@example.com",
		IdempotencyKey: "issue-wait-revoke",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update space_invitations set created_at=boundary.expires_at-interval '7 days', expires_at=boundary.expires_at from (select clock_timestamp()+interval '400 milliseconds' as expires_at) as boundary where invitation_id=$1`, revokeIssue.InvitationID); err != nil {
		t.Fatal(err)
	}
	revokeLock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revokeLock.Exec(ctx, `select invitation_id from space_invitations where invitation_id=$1 for update`, revokeIssue.InvitationID); err != nil {
		t.Fatal(err)
	}
	revokeResult := make(chan error, 1)
	go func() {
		revokeResult <- invitations.Revoke(ctx, space.RevokeInvitationCommand{
			SpaceID:        manager.SpaceID,
			InvitationID:   revokeIssue.InvitationID,
			ActorUserID:    manager.UserID,
			IdempotencyKey: "revoke-after-wait",
		})
	}()
	time.Sleep(700 * time.Millisecond)
	if err := revokeLock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-revokeResult; !errors.Is(err, space.ErrInvitationExpired) {
		t.Fatalf("post-wait revoke = %v", err)
	}
	var revokedAt *time.Time
	if err := pool.QueryRow(ctx, `select revoked_at from space_invitations where invitation_id=$1`, revokeIssue.InvitationID).Scan(&revokedAt); err != nil || revokedAt != nil {
		t.Fatalf("expired revoke terminal = %v, %v", revokedAt, err)
	}
}

func TestConcurrentInvitationAcceptsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
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
			_, err := invitations.Accept(ctx, space.AcceptInvitationCommand{
				InvitationID:   issued.InvitationID,
				UserID:         userID,
				SessionID:      sessions[0],
				IdempotencyKey: key,
			})
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
		} else if errors.Is(err, space.ErrInvitationAccepted) {
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
	manager := invitationManagerFixture(t, ctx, store)
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
		_, acceptErr = invitations.Accept(ctx, space.AcceptInvitationCommand{
			InvitationID:   issued.InvitationID,
			UserID:         userID,
			SessionID:      sessions[0],
			IdempotencyKey: "accept-concurrent",
		})
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

func invitationManagerFixture(t *testing.T, ctx context.Context, store *Store) testMember {
	t.Helper()
	result, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Invitation Manager", SpaceName: "Invitation Space",
	})
	if err != nil {
		t.Fatalf("bootstrap invitation manager: %v", err)
	}
	return result
}

func newTestInvitations(t *testing.T, persistence space.InvitationPersistence, submitter space.InvitationSubmitter) *space.Invitations {
	t.Helper()
	invitations, err := space.NewInvitations(persistence, submitter, "https://carry.example")
	if err != nil {
		t.Fatalf("create invitation behavior: %v", err)
	}
	return invitations
}

type invitationPrepareResponseLossStore struct {
	*Store
	lost          bool
	invitationIDs []string
}

func (store *invitationPrepareResponseLossStore) PrepareInvitation(ctx context.Context, command space.PrepareInvitationCommand) (space.IssuedInvitation, error) {
	store.invitationIDs = append(store.invitationIDs, command.InvitationID)
	issued, err := store.Store.PrepareInvitation(ctx, command)
	if err == nil && !store.lost {
		store.lost = true
		return space.IssuedInvitation{}, errors.New("simulated prepared response loss")
	}
	return issued, err
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
	calls    int
	state    space.InvitationSubmissionState
	cancel   context.CancelFunc
	messages []space.InvitationMessage
}

func (submitter *acceptedInvitationSubmitter) InvitationPayloadDigest(message space.InvitationMessage) ([32]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.DestinationURL + "\x00" + message.IdempotencyKey)), nil
}

func (submitter *acceptedInvitationSubmitter) SubmitInvitation(_ context.Context, message space.InvitationMessage, _ [32]byte) space.InvitationSubmission {
	submitter.calls++
	submitter.messages = append(submitter.messages, message)
	if submitter.cancel != nil {
		submitter.cancel()
	}
	if submitter.state == space.InvitationSubmissionUnknown {
		return space.InvitationSubmission{State: space.InvitationSubmissionUnknown}
	}
	return space.InvitationSubmission{State: space.InvitationSubmissionAccepted, ProviderMessageID: "accepted-invitation"}
}
