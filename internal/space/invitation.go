package space

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/google/uuid"
)

const (
	InvitationLifetime       = 7 * 24 * time.Hour
	InvitationResendCooldown = time.Minute
	invitationRecordTimeout  = 5 * time.Second
)

var (
	ErrInvalidInvitation            = errors.New("Space invitation is invalid")
	ErrInvitationConflict           = errors.New("a current invitation already exists")
	ErrInvitationUnavailable        = errors.New("Space invitation is unavailable")
	ErrInvitationAlreadyMember      = errors.New("User already belongs to the Space")
	ErrInvitationProofRequired      = errors.New("recent proof of the invited Email is required")
	ErrInvitationResendCooldown     = errors.New("Space invitation resend is cooling down")
	ErrInvitationSubmissionConflict = errors.New("invitation submission outcome conflicts")
)

type InvitationSubmissionState string

const (
	InvitationSubmissionPrepared InvitationSubmissionState = "prepared"
	InvitationSubmissionAccepted InvitationSubmissionState = "accepted"
	InvitationSubmissionRejected InvitationSubmissionState = "rejected"
	InvitationSubmissionUnknown  InvitationSubmissionState = "unknown"
)

type InvitationMessage struct {
	Recipient      string
	DestinationURL string
	IdempotencyKey string
}

func InvitationMessageText(destinationURL string) string {
	return "You have a Carry Space invitation. Sign in and review it at " + destinationURL + ". This email does not grant access."
}

type InvitationSubmitter interface {
	InvitationPayloadDigest(InvitationMessage) ([sha256.Size]byte, error)
	SubmitInvitation(context.Context, InvitationMessage, [sha256.Size]byte) InvitationSubmission
}

type InvitationPersistence interface {
	PrepareInvitation(context.Context, PrepareInvitationCommand) (IssuedInvitation, error)
	InvitationRecipient(context.Context, string, string, string) (string, error)
	PrepareInvitationResend(context.Context, PrepareInvitationResendCommand) (IssuedInvitation, error)
	RecordInvitationSubmission(context.Context, RecordInvitationSubmissionCommand) (InvitationSubmission, error)
	ListSpaceInvitations(context.Context, string, string) ([]ManagedInvitation, error)
	ListUserInvitations(context.Context, string, string) (InvitationInbox, error)
	RevokeInvitation(context.Context, RevokeInvitationCommand) error
	AcceptInvitation(context.Context, AcceptInvitationCommand) (AcceptedInvitation, error)
}

type Invitations struct {
	persistence    InvitationPersistence
	submitter      InvitationSubmitter
	destinationURL string
}

func NewInvitations(persistence InvitationPersistence, submitter InvitationSubmitter, destinationURL string) (*Invitations, error) {
	parsed, err := url.Parse(strings.TrimSpace(destinationURL))
	if persistence == nil || submitter == nil || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "/invitations" {
		return nil, ErrInvalidInvitation
	}
	return &Invitations{persistence: persistence, submitter: submitter, destinationURL: parsed.String()}, nil
}

type IssueInvitationRequest struct {
	SpaceID           string
	ActorUserID       string
	RecipientEmail    string
	CanManageMembers  bool
	CanEnrollMachines bool
	IdempotencyKey    string
}

func (invitations *Invitations) Issue(ctx context.Context, request IssueInvitationRequest) (IssuedInvitation, error) {
	recipient, err := identity.CanonicalEmail(request.RecipientEmail)
	if err != nil || uuid.Validate(request.SpaceID) != nil || uuid.Validate(request.ActorUserID) != nil || !validCommandKey(request.IdempotencyKey) {
		return IssuedInvitation{}, ErrInvalidInvitation
	}
	requestDigest := digest(struct {
		RecipientEmail    string `json:"recipient_email"`
		CanManageMembers  bool   `json:"can_manage_members"`
		CanEnrollMachines bool   `json:"can_enroll_machines"`
	}{recipient, request.CanManageMembers, request.CanEnrollMachines})
	invitationID, submissionID := uuid.NewString(), uuid.NewString()
	providerKey := "space-invitation/" + submissionID
	message := InvitationMessage{Recipient: recipient, DestinationURL: invitations.destinationURL, IdempotencyKey: providerKey}
	payloadDigest, err := invitations.submitter.InvitationPayloadDigest(message)
	if err != nil {
		return IssuedInvitation{}, ErrInvitationSubmissionConflict
	}
	issued, err := invitations.persistence.PrepareInvitation(ctx, PrepareInvitationCommand{
		InvitationID: invitationID, SubmissionID: submissionID,
		SpaceID: request.SpaceID, ActorUserID: request.ActorUserID, RecipientEmail: recipient,
		CanManageMembers: request.CanManageMembers, CanEnrollMachines: request.CanEnrollMachines,
		IdempotencyKey: request.IdempotencyKey, RequestDigest: requestDigest,
		ProviderIdempotencyKey: providerKey, PayloadDigest: payloadDigest,
		ExpiresIn: InvitationLifetime,
	})
	if err != nil {
		return IssuedInvitation{}, err
	}
	return invitations.submit(ctx, issued)
}

type ResendInvitationRequest struct {
	SpaceID, InvitationID, ActorUserID, IdempotencyKey string
}

func (invitations *Invitations) Resend(ctx context.Context, request ResendInvitationRequest) (IssuedInvitation, error) {
	if uuid.Validate(request.SpaceID) != nil || uuid.Validate(request.InvitationID) != nil || uuid.Validate(request.ActorUserID) != nil || !validCommandKey(request.IdempotencyKey) {
		return IssuedInvitation{}, ErrInvalidInvitation
	}
	digestValue := digest(struct {
		InvitationID string `json:"invitation_id"`
	}{request.InvitationID})
	submissionID := uuid.NewString()
	providerKey := "space-invitation/" + submissionID
	recipient, err := invitations.persistence.InvitationRecipient(ctx, request.SpaceID, request.InvitationID, request.ActorUserID)
	if err != nil {
		return IssuedInvitation{}, err
	}
	message := InvitationMessage{Recipient: recipient, DestinationURL: invitations.destinationURL, IdempotencyKey: providerKey}
	payloadDigest, err := invitations.submitter.InvitationPayloadDigest(message)
	if err != nil {
		return IssuedInvitation{}, ErrInvitationSubmissionConflict
	}
	issued, err := invitations.persistence.PrepareInvitationResend(ctx, PrepareInvitationResendCommand{
		SpaceID: request.SpaceID, InvitationID: request.InvitationID, ActorUserID: request.ActorUserID,
		SubmissionID: submissionID, IdempotencyKey: request.IdempotencyKey, RequestDigest: digestValue,
		ProviderIdempotencyKey: providerKey, PayloadDigest: payloadDigest,
	})
	if err != nil {
		return IssuedInvitation{}, err
	}
	return invitations.submit(ctx, issued)
}

func (invitations *Invitations) submit(ctx context.Context, issued IssuedInvitation) (IssuedInvitation, error) {
	if !issued.Submission.SubmitEligible {
		return issued, nil
	}
	message := InvitationMessage{Recipient: issued.Submission.Recipient, DestinationURL: invitations.destinationURL, IdempotencyKey: issued.Submission.ProviderIdempotencyKey}
	actualDigest, err := invitations.submitter.InvitationPayloadDigest(message)
	if err != nil || actualDigest != issued.Submission.PayloadDigest {
		return IssuedInvitation{}, ErrInvitationSubmissionConflict
	}
	observed := invitations.submitter.SubmitInvitation(ctx, message, issued.Submission.PayloadDigest)
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), invitationRecordTimeout)
	defer cancel()
	recorded, err := invitations.persistence.RecordInvitationSubmission(recordCtx, RecordInvitationSubmissionCommand{
		SubmissionID: issued.Submission.SubmissionID, PayloadDigest: issued.Submission.PayloadDigest,
		State: observed.State, ProviderMessageID: observed.ProviderMessageID,
	})
	if err != nil {
		return IssuedInvitation{}, err
	}
	issued.Submission = recorded
	return issued, nil
}

func (invitations *Invitations) ListForSpace(ctx context.Context, userID, spaceID string) ([]ManagedInvitation, error) {
	return invitations.persistence.ListSpaceInvitations(ctx, userID, spaceID)
}
func (invitations *Invitations) ListForUser(ctx context.Context, userID, sessionID string) (InvitationInbox, error) {
	return invitations.persistence.ListUserInvitations(ctx, userID, sessionID)
}
func (invitations *Invitations) Revoke(ctx context.Context, command RevokeInvitationCommand) error {
	command.RequestDigest = digest(struct {
		InvitationID string `json:"invitation_id"`
	}{command.InvitationID})
	return invitations.persistence.RevokeInvitation(ctx, command)
}
func (invitations *Invitations) Accept(ctx context.Context, command AcceptInvitationCommand) (AcceptedInvitation, error) {
	command.DisplayName = strings.TrimSpace(command.DisplayName)
	command.RequestDigest = digest(struct {
		InvitationID string `json:"invitation_id"`
		DisplayName  string `json:"display_name"`
	}{command.InvitationID, command.DisplayName})
	return invitations.persistence.AcceptInvitation(ctx, command)
}

type PrepareInvitationCommand struct {
	InvitationID, SubmissionID, SpaceID, ActorUserID, RecipientEmail string
	CanManageMembers, CanEnrollMachines                              bool
	IdempotencyKey                                                   string
	RequestDigest                                                    [sha256.Size]byte
	ProviderIdempotencyKey                                           string
	PayloadDigest                                                    [sha256.Size]byte
	ExpiresIn                                                        time.Duration
}
type PrepareInvitationResendCommand struct {
	SpaceID, InvitationID, ActorUserID, SubmissionID, IdempotencyKey string
	RequestDigest                                                    [sha256.Size]byte
	ProviderIdempotencyKey                                           string
	PayloadDigest                                                    [sha256.Size]byte
}
type RecordInvitationSubmissionCommand struct {
	SubmissionID      string
	PayloadDigest     [sha256.Size]byte
	State             InvitationSubmissionState
	ProviderMessageID string
}
type RevokeInvitationCommand struct {
	SpaceID, InvitationID, ActorUserID, IdempotencyKey string
	RequestDigest                                      [sha256.Size]byte
}
type AcceptInvitationCommand struct {
	InvitationID, UserID, SessionID, DisplayName, IdempotencyKey string
	RequestDigest                                                [sha256.Size]byte
}

type InvitationSubmission struct {
	SubmissionID, Recipient, ProviderIdempotencyKey, ProviderMessageID string
	PayloadDigest                                                      [sha256.Size]byte
	State                                                              InvitationSubmissionState
	CreatedAt                                                          time.Time
	SubmitEligible                                                     bool
}
type IssuedInvitation struct {
	InvitationID, SpaceID, RecipientEmail, InviterDisplayName string
	CanManageMembers, CanEnrollMachines                       bool
	CreatedAt, ExpiresAt                                      time.Time
	Submission                                                InvitationSubmission
}
type ManagedInvitation = IssuedInvitation
type RecipientInvitation struct {
	InvitationID, SpaceID, SpaceName, InviterDisplayName string
	CanManageMembers, CanEnrollMachines                  bool
	CreatedAt, ExpiresAt                                 time.Time
}
type InvitationInbox struct {
	Invitations              []RecipientInvitation
	ReauthenticationRequired bool
}
type AcceptedInvitation struct {
	InvitationID, SpaceID, SpaceName                   string
	CanManageMembers, CanEnrollMachines, AlreadyMember bool
}

func digest(value any) [sha256.Size]byte {
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}
func validCommandKey(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 255
}
