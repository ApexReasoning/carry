package space

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInvitationIssueCanonicalizesRecipientAndRecordsSubmission(t *testing.T) {
	persistence := &invitationPersistenceStub{}
	submitter := &invitationSubmitterStub{result: InvitationSubmission{
		State: InvitationSubmissionAccepted, ProviderMessageID: "email_123",
	}}
	invitations, err := NewInvitations(persistence, submitter, "https://carry.example/invitations")
	if err != nil {
		t.Fatalf("new invitations: %v", err)
	}

	issued, err := invitations.Issue(context.Background(), IssueInvitationRequest{
		SpaceID: "10000000-0000-0000-0000-000000000001", ActorUserID: "20000000-0000-0000-0000-000000000001",
		RecipientEmail: "  Teammate@Example.COM ", CanManageMembers: true,
		IdempotencyKey: "issue-1",
	})
	if err != nil {
		t.Fatalf("issue invitation: %v", err)
	}
	if persistence.prepared.RecipientEmail != "teammate@example.com" {
		t.Fatalf("recipient = %q", persistence.prepared.RecipientEmail)
	}
	if persistence.prepared.ExpiresIn != InvitationLifetime {
		t.Fatalf("lifetime = %s", persistence.prepared.ExpiresIn)
	}
	if submitter.message.DestinationURL != "https://carry.example/invitations" || submitter.message.Recipient != "teammate@example.com" {
		t.Fatalf("message = %#v", submitter.message)
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
	invitations, err := NewInvitations(persistence, submitter, "https://carry.example/invitations")
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

func TestInvitationRejectsUnsafeDestinationAndInvalidRequest(t *testing.T) {
	if _, err := NewInvitations(&invitationPersistenceStub{}, &invitationSubmitterStub{}, "http://carry.example/invitations"); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("destination error = %v", err)
	}
	invitations, err := NewInvitations(&invitationPersistenceStub{}, &invitationSubmitterStub{}, "https://carry.example/invitations")
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
func (stub *invitationPersistenceStub) RevokeInvitation(context.Context, RevokeInvitationCommand) error {
	return nil
}
func (stub *invitationPersistenceStub) AcceptInvitation(context.Context, AcceptInvitationCommand) (AcceptedInvitation, error) {
	return AcceptedInvitation{}, nil
}

type invitationSubmitterStub struct {
	message InvitationMessage
	result  InvitationSubmission
}

func (stub *invitationSubmitterStub) InvitationPayloadDigest(message InvitationMessage) ([32]byte, error) {
	return digest(message), nil
}
func (stub *invitationSubmitterStub) SubmitInvitation(_ context.Context, message InvitationMessage, _ [32]byte) InvitationSubmission {
	stub.message = message
	return stub.result
}
