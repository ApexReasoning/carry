package space

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

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
	ErrInvitationExpired            = errors.New("Space invitation has expired")
	ErrInvitationRevoked            = errors.New("Space invitation was revoked")
	ErrInvitationAccepted           = errors.New("Space invitation was already accepted")
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
	RevokeInvitation(context.Context, RevokeInvitationCommand) error
	AcceptInvitation(context.Context, AcceptInvitationCommand) (AcceptedInvitation, error)
}

type Invitations struct {
	persistence InvitationPersistence
	submitter   InvitationSubmitter
	origin      string
}

func NewInvitations(persistence InvitationPersistence, submitter InvitationSubmitter, origin string) (*Invitations, error) {
	if persistence == nil || submitter == nil {
		return nil, ErrInvalidInvitation
	}
	if _, err := InvitationURL(origin, uuid.NewString()); err != nil {
		return nil, err
	}
	return &Invitations{
		persistence: persistence,
		submitter:   submitter,
		origin:      origin,
	}, nil
}

func InvitationPath(invitationID string) (string, error) {
	if uuid.Validate(invitationID) != nil {
		return "", ErrInvalidInvitation
	}
	return "/invitations/" + url.PathEscape(invitationID), nil
}

func InvitationURL(origin, invitationID string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil ||
		strings.TrimSpace(origin) != origin ||
		origin == "" ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.Hostname() == "" ||
		parsed.User != nil ||
		parsed.Path != "" ||
		parsed.RawPath != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Opaque != "" ||
		parsed.Host != strings.ToLower(parsed.Host) ||
		origin != "https://"+parsed.Host {
		return "", ErrInvalidInvitation
	}
	path, err := InvitationPath(invitationID)
	if err != nil {
		return "", err
	}
	return origin + path, nil
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
	idempotencyKey, validKey := normalizeCommandKey(request.IdempotencyKey)
	if err != nil ||
		uuid.Validate(request.SpaceID) != nil ||
		uuid.Validate(request.ActorUserID) != nil ||
		!validKey {
		return IssuedInvitation{}, ErrInvalidInvitation
	}
	requestDigest, err := invitationIssueDigest(recipient, request.CanManageMembers, request.CanEnrollMachines)
	if err != nil {
		return IssuedInvitation{}, fmt.Errorf("digest invitation issue: %w", err)
	}
	invitationID, submissionID := uuid.NewString(), uuid.NewString()
	providerKey := "space-invitation/" + submissionID
	destinationURL, err := InvitationURL(invitations.origin, invitationID)
	if err != nil {
		return IssuedInvitation{}, err
	}
	message := InvitationMessage{
		Recipient:      recipient,
		DestinationURL: destinationURL,
		IdempotencyKey: providerKey,
	}
	payloadDigest, err := invitations.submitter.InvitationPayloadDigest(message)
	if err != nil {
		return IssuedInvitation{}, ErrInvitationSubmissionConflict
	}
	issued, err := invitations.persistence.PrepareInvitation(ctx, PrepareInvitationCommand{
		InvitationID: invitationID, SubmissionID: submissionID,
		SpaceID: request.SpaceID, ActorUserID: request.ActorUserID, RecipientEmail: recipient,
		CanManageMembers: request.CanManageMembers, CanEnrollMachines: request.CanEnrollMachines,
		IdempotencyKey: idempotencyKey, RequestDigest: requestDigest,
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
	idempotencyKey, validKey := normalizeCommandKey(request.IdempotencyKey)
	if uuid.Validate(request.SpaceID) != nil ||
		uuid.Validate(request.InvitationID) != nil ||
		uuid.Validate(request.ActorUserID) != nil ||
		!validKey {
		return IssuedInvitation{}, ErrInvalidInvitation
	}
	digestValue, err := invitationResendDigest(request.InvitationID)
	if err != nil {
		return IssuedInvitation{}, fmt.Errorf("digest invitation resend: %w", err)
	}
	submissionID := uuid.NewString()
	providerKey := "space-invitation/" + submissionID
	recipient, err := invitations.persistence.InvitationRecipient(ctx, request.SpaceID, request.InvitationID, request.ActorUserID)
	if err != nil {
		return IssuedInvitation{}, err
	}
	destinationURL, err := InvitationURL(invitations.origin, request.InvitationID)
	if err != nil {
		return IssuedInvitation{}, err
	}
	message := InvitationMessage{
		Recipient:      recipient,
		DestinationURL: destinationURL,
		IdempotencyKey: providerKey,
	}
	payloadDigest, err := invitations.submitter.InvitationPayloadDigest(message)
	if err != nil {
		return IssuedInvitation{}, ErrInvitationSubmissionConflict
	}
	issued, err := invitations.persistence.PrepareInvitationResend(ctx, PrepareInvitationResendCommand{
		SpaceID: request.SpaceID, InvitationID: request.InvitationID, ActorUserID: request.ActorUserID,
		SubmissionID: submissionID, IdempotencyKey: idempotencyKey, RequestDigest: digestValue,
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
	destinationURL, err := InvitationURL(invitations.origin, issued.InvitationID)
	if err != nil {
		return IssuedInvitation{}, err
	}
	message := InvitationMessage{
		Recipient:      issued.Submission.Recipient,
		DestinationURL: destinationURL,
		IdempotencyKey: issued.Submission.ProviderIdempotencyKey,
	}
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

func (invitations *Invitations) Revoke(ctx context.Context, command RevokeInvitationCommand) error {
	idempotencyKey, validKey := normalizeCommandKey(command.IdempotencyKey)
	if !validKey {
		return ErrInvalidInvitation
	}
	requestDigest, err := invitationRevokeDigest(command.InvitationID)
	if err != nil {
		return fmt.Errorf("digest invitation revoke: %w", err)
	}
	command.IdempotencyKey = idempotencyKey
	command.RequestDigest = requestDigest
	return invitations.persistence.RevokeInvitation(ctx, command)
}
func (invitations *Invitations) Accept(ctx context.Context, command AcceptInvitationCommand) (AcceptedInvitation, error) {
	idempotencyKey, validKey := normalizeCommandKey(command.IdempotencyKey)
	if !validKey {
		return AcceptedInvitation{}, ErrInvalidInvitation
	}
	requestDigest, err := invitationAcceptDigest(command.InvitationID)
	if err != nil {
		return AcceptedInvitation{}, fmt.Errorf("digest invitation acceptance: %w", err)
	}
	command.IdempotencyKey = idempotencyKey
	command.RequestDigest = requestDigest
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
	InvitationID, UserID, SessionID, IdempotencyKey string
	RequestDigest                                   [sha256.Size]byte
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
type InvitationState string

const (
	InvitationPending  InvitationState = "pending"
	InvitationAccepted InvitationState = "accepted"
	InvitationRevoked  InvitationState = "revoked"
	InvitationExpired  InvitationState = "expired"
)

type RecipientInvitation struct {
	InvitationID, SpaceID, SpaceName, InviterDisplayName string
	CanManageMembers, CanEnrollMachines                  bool
	CreatedAt, ExpiresAt                                 time.Time
	State                                                InvitationState
	AcceptResult                                         string
	CurrentMember                                        bool
	ReauthenticationRequired                             bool
}
type InvitationInbox struct {
	Invitations              []RecipientInvitation
	ReauthenticationRequired bool
}
type AcceptedInvitation struct {
	InvitationID, SpaceID, SpaceName                   string
	CanManageMembers, CanEnrollMachines, AlreadyMember bool
}

func invitationIssueDigest(recipientEmail string, canManageMembers, canEnrollMachines bool) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		RecipientEmail    string `json:"recipient_email"`
		CanManageMembers  bool   `json:"can_manage_members"`
		CanEnrollMachines bool   `json:"can_enroll_machines"`
	}{recipientEmail, canManageMembers, canEnrollMachines})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("marshal invitation issue digest: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func invitationResendDigest(invitationID string) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		InvitationID string `json:"invitation_id"`
	}{invitationID})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("marshal invitation resend digest: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func invitationRevokeDigest(invitationID string) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		InvitationID string `json:"invitation_id"`
	}{invitationID})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("marshal invitation revoke digest: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func invitationAcceptDigest(invitationID string) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		InvitationID string `json:"invitation_id"`
	}{invitationID})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("marshal invitation acceptance digest: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func normalizeCommandKey(value string) (string, bool) {
	key := strings.TrimSpace(value)
	return key, key != "" && len(key) <= 255 && utf8.ValidString(key) && !strings.ContainsRune(key, 0)
}
