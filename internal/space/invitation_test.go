package space

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestInvitationIssueCanonicalizesRecipientAndRecordsSubmission(t *testing.T) {
	persistence := &invitationPersistenceStub{}
	submitter := &invitationSubmitterStub{result: InvitationSubmission{
		State: InvitationSubmissionAccepted, ProviderMessageID: "email_123",
	}}
	invitations, err := NewInvitations(persistence, submitter, "https://carry.example")
	if err != nil {
		t.Fatalf("new invitations: %v", err)
	}

	issued, err := invitations.Issue(context.Background(), IssueInvitationRequest{
		SpaceID: "10000000-0000-0000-0000-000000000001", ActorUserID: "20000000-0000-0000-0000-000000000001",
		RecipientEmail: "  Teammate@Example.COM ", CanManageMembers: true,
		IdempotencyKey: "  issue-1  ",
	})
	if err != nil {
		t.Fatalf("issue invitation: %v", err)
	}
	if persistence.prepared.RecipientEmail != "teammate@example.com" || persistence.prepared.IdempotencyKey != "issue-1" {
		t.Fatalf("prepared invitation = %#v", persistence.prepared)
	}
	if persistence.prepared.ExpiresIn != InvitationLifetime {
		t.Fatalf("lifetime = %s", persistence.prepared.ExpiresIn)
	}
	wantURL := "https://carry.example/invitations/" + issued.InvitationID
	if submitter.message.DestinationURL != wantURL || submitter.message.Recipient != "teammate@example.com" {
		t.Fatalf("message = %#v, want URL %q", submitter.message, wantURL)
	}
	if persistence.recorded.State != InvitationSubmissionAccepted || persistence.recorded.ProviderMessageID != "email_123" {
		t.Fatalf("recorded = %#v", persistence.recorded)
	}
	if issued.Submission.State != InvitationSubmissionAccepted {
		t.Fatalf("state = %q", issued.Submission.State)
	}
}

func TestInvitationIssuePreservesUnknown(t *testing.T) {
	persistence := &invitationPersistenceStub{}
	submitter := &invitationSubmitterStub{result: InvitationSubmission{State: InvitationSubmissionUnknown}}
	invitations, err := NewInvitations(persistence, submitter, "https://carry.example")
	if err != nil {
		t.Fatalf("new invitations: %v", err)
	}
	issued, err := invitations.Issue(context.Background(), IssueInvitationRequest{
		SpaceID: "10000000-0000-0000-0000-000000000001", ActorUserID: "20000000-0000-0000-0000-000000000001",
		RecipientEmail: "person@example.com", IdempotencyKey: "issue-unknown",
	})
	if err != nil {
		t.Fatalf("issue invitation: %v", err)
	}
	if issued.Submission.State != InvitationSubmissionUnknown {
		t.Fatalf("state = %q", issued.Submission.State)
	}
}

func TestInvitationPathAndURLAreExactAndRejectUnsafeInputs(t *testing.T) {
	invitationID := "10000000-0000-4000-8000-000000000001"
	path, err := InvitationPath(invitationID)
	if err != nil || path != "/invitations/"+invitationID {
		t.Fatalf("path = %q, %v", path, err)
	}
	exact, err := InvitationURL("https://carry.example", invitationID)
	if err != nil || exact != "https://carry.example"+path {
		t.Fatalf("URL = %q, %v", exact, err)
	}
	for _, invalid := range []struct{ origin, id string }{
		{"http://carry.example", invitationID},
		{"https://carry.example/", invitationID},
		{"https://carry.example/invitations", invitationID},
		{"https://CARRY.example", invitationID},
		{"https://carry.example", "not-a-uuid"},
	} {
		if _, err := InvitationURL(invalid.origin, invalid.id); !errors.Is(err, ErrInvalidInvitation) {
			t.Errorf("unsafe URL %q / %q = %v", invalid.origin, invalid.id, err)
		}
	}
}

func TestInvitationMutationsCanonicalizeTheReplayIdentity(t *testing.T) {
	persistence := &invitationPersistenceStub{}
	invitations, err := NewInvitations(persistence, &invitationSubmitterStub{}, "https://carry.example")
	if err != nil {
		t.Fatalf("new invitations: %v", err)
	}
	invitationID := "10000000-0000-4000-8000-000000000001"
	if err := invitations.Revoke(context.Background(), RevokeInvitationCommand{
		SpaceID:        "20000000-0000-4000-8000-000000000001",
		InvitationID:   invitationID,
		ActorUserID:    "30000000-0000-4000-8000-000000000001",
		IdempotencyKey: "  revoke-1  ",
	}); err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	if persistence.revoked.IdempotencyKey != "revoke-1" || persistence.revoked.RequestDigest == ([32]byte{}) {
		t.Fatalf("revoke command = %#v", persistence.revoked)
	}
	if _, err := invitations.Accept(context.Background(), AcceptInvitationCommand{
		InvitationID:   invitationID,
		UserID:         "40000000-0000-4000-8000-000000000001",
		SessionID:      "50000000-0000-4000-8000-000000000001",
		IdempotencyKey: "  accept-1  ",
	}); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if persistence.accepted.IdempotencyKey != "accept-1" || persistence.accepted.RequestDigest == ([32]byte{}) {
		t.Fatalf("accept command = %#v", persistence.accepted)
	}
}

func TestInvitationRejectsUnsafeDestinationAndInvalidRequest(t *testing.T) {
	if _, err := NewInvitations(&invitationPersistenceStub{}, &invitationSubmitterStub{}, "http://carry.example"); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("destination error = %v", err)
	}
	invitations, err := NewInvitations(&invitationPersistenceStub{}, &invitationSubmitterStub{}, "https://carry.example")
	if err != nil {
		t.Fatalf("new invitations: %v", err)
	}
	if _, err := invitations.Issue(context.Background(), IssueInvitationRequest{RecipientEmail: "not an email"}); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("issue error = %v", err)
	}
}

var _ InvitationPersistence = (*invitationPersistenceStub)(nil)

type invitationPersistenceStub struct {
	prepared PrepareInvitationCommand
	recorded RecordInvitationSubmissionCommand
	revoked  RevokeInvitationCommand
	accepted AcceptInvitationCommand
}

func (stub *invitationPersistenceStub) PrepareInvitation(_ context.Context, command PrepareInvitationCommand) (IssuedInvitation, error) {
	stub.prepared = command
	return IssuedInvitation{
		InvitationID: command.InvitationID, SpaceID: command.SpaceID, RecipientEmail: command.RecipientEmail,
		CanManageMembers: command.CanManageMembers, CanEnrollMachines: command.CanEnrollMachines,
		ExpiresAt: time.Now().Add(command.ExpiresIn),
		Submission: InvitationSubmission{
			SubmissionID: command.SubmissionID, Recipient: command.RecipientEmail,
			ProviderIdempotencyKey: command.ProviderIdempotencyKey,
			PayloadDigest:          command.PayloadDigest,
			State:                  InvitationSubmissionPrepared, CreatedAt: time.Now(), SubmitEligible: true,
		},
	}, nil
}

func (stub *invitationPersistenceStub) InvitationRecipient(context.Context, string, string, string) (string, error) {
	return "teammate@example.com", nil
}
func (stub *invitationPersistenceStub) PrepareInvitationResend(context.Context, PrepareInvitationResendCommand) (IssuedInvitation, error) {
	return IssuedInvitation{}, errors.New("not implemented")
}
func (stub *invitationPersistenceStub) RecordInvitationSubmission(_ context.Context, command RecordInvitationSubmissionCommand) (InvitationSubmission, error) {
	stub.recorded = command
	return InvitationSubmission{
		SubmissionID: command.SubmissionID, State: command.State, ProviderMessageID: command.ProviderMessageID,
	}, nil
}
func (stub *invitationPersistenceStub) ListSpaceMembers(context.Context, string, string) ([]SpaceMember, error) {
	return nil, nil
}
func (stub *invitationPersistenceStub) ListSpaceInvitations(context.Context, string, string) ([]ManagedInvitation, error) {
	return nil, nil
}
func (stub *invitationPersistenceStub) ListUserInvitations(context.Context, string, string) (InvitationInbox, error) {
	return InvitationInbox{}, nil
}
func (stub *invitationPersistenceStub) LoadInvitationForUser(context.Context, string, string, string) (RecipientInvitation, error) {
	return RecipientInvitation{}, nil
}
func (stub *invitationPersistenceStub) RevokeInvitation(_ context.Context, command RevokeInvitationCommand) error {
	stub.revoked = command
	return nil
}
func (stub *invitationPersistenceStub) AcceptInvitation(_ context.Context, command AcceptInvitationCommand) (AcceptedInvitation, error) {
	stub.accepted = command
	return AcceptedInvitation{InvitationID: command.InvitationID}, nil
}

type invitationSubmitterStub struct {
	message InvitationMessage
	result  InvitationSubmission
}

func (stub *invitationSubmitterStub) InvitationPayloadDigest(message InvitationMessage) ([32]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.DestinationURL + "\x00" + message.IdempotencyKey)), nil
}
func (stub *invitationSubmitterStub) SubmitInvitation(_ context.Context, message InvitationMessage, _ [32]byte) InvitationSubmission {
	stub.message = message
	return stub.result
}
