package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	EmailCodeDigits       = 6
	EmailCodeLifetime     = 5 * time.Minute
	EmailCodeAttemptLimit = 5
	EmailCodeResendDelay  = time.Minute
	IdentityRootBytes     = 32
)

var (
	ErrInvalidEmail            = errors.New("email address is invalid")
	ErrInvalidCode             = errors.New("email code is invalid or expired")
	ErrEmailRateLimited        = errors.New("email code requests are temporarily limited")
	ErrEmailSubmissionRejected = errors.New("email code could not be submitted")
	ErrEmailPayloadChanged     = errors.New("email submission payload changed")
	ErrIdempotencyConflict     = errors.New("identity idempotency key was reused for a different request")
)

type Credentials struct {
	root [IdentityRootBytes]byte
}

func NewCredentials(root []byte) (Credentials, error) {
	if len(root) != IdentityRootBytes {
		return Credentials{}, fmt.Errorf("identity root must contain %d bytes", IdentityRootBytes)
	}
	var credentials Credentials
	copy(credentials.root[:], root)
	return credentials, nil
}

func ParseIdentityRoot(encoded string) (Credentials, error) {
	root, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return Credentials{}, errors.New("identity root must be raw URL-safe base64")
	}
	return NewCredentials(root)
}

func CanonicalEmail(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len([]byte(trimmed)) > 254 || strings.ContainsAny(trimmed, "\r\n") {
		return "", ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Name != "" || parsed.Address != trimmed || !strings.Contains(trimmed, "@") {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(trimmed), nil
}

func (c Credentials) EmailCode(challengeID string, canonicalEmail string) (string, error) {
	if uuid.Validate(challengeID) != nil {
		return "", errors.New("challenge identity is invalid")
	}
	mac := c.mac("carry/email-code/v1", challengeID, canonicalEmail)
	value := binary.BigEndian.Uint64(mac[:8]) % 1_000_000
	return fmt.Sprintf("%0*d", EmailCodeDigits, value), nil
}

func (c Credentials) CodeDigest(challengeID string, code string) [sha256.Size]byte {
	return c.mac("carry/email-code-digest/v1", challengeID, code)
}

func (c Credentials) RequestDigest(parts ...string) [sha256.Size]byte {
	return c.mac("carry/identity-request/v1", parts...)
}

func (c Credentials) SourceDigest(remoteAddress string) [sha256.Size]byte {
	return c.mac("carry/email-source/v1", remoteAddress)
}

func (c Credentials) BrowserSessionCredential(sessionID string) (string, error) {
	if uuid.Validate(sessionID) != nil {
		return "", errors.New("browser session identity is invalid")
	}
	mac := c.mac("carry/browser-session/v1", sessionID)
	return "carry_session_" + sessionID + "." + base64.RawURLEncoding.EncodeToString(mac[:]), nil
}

func (c Credentials) ParseBrowserSessionCredential(value string) (string, bool) {
	const prefix = "carry_session_"
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(value, prefix), ".")
	if len(parts) != 2 || uuid.Validate(parts[0]) != nil {
		return "", false
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(provided) != sha256.Size {
		return "", false
	}
	expected := c.mac("carry/browser-session/v1", parts[0])
	if subtle.ConstantTimeCompare(provided, expected[:]) != 1 {
		return "", false
	}
	return parts[0], true
}

func (c Credentials) mac(label string, parts ...string) [sha256.Size]byte {
	mac := hmac.New(sha256.New, c.root[:])
	_, _ = mac.Write([]byte(label))
	for _, part := range parts {
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write([]byte(part))
	}
	var digest [sha256.Size]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}

// EmailLoginPersistence is the narrow transactional capability consumed by Identity.
type EmailLoginPersistence interface {
	PrepareEmailChallenge(context.Context, PrepareEmailChallengeCommand) (EmailChallenge, error)
	RecordEmailSubmission(context.Context, string, [sha256.Size]byte, EmailSubmission) (EmailChallenge, error)
	VerifyEmailChallenge(context.Context, VerifyEmailChallengeCommand) (BrowserSession, error)
}

// EmailCodeSubmitter is the one external consequence consumed by Identity.
type EmailCodeSubmitter interface {
	PayloadDigest(EmailCodeMessage) ([sha256.Size]byte, error)
	SubmitEmailCode(context.Context, EmailCodeMessage, [sha256.Size]byte) EmailSubmission
}

type EmailLogin struct {
	persistence EmailLoginPersistence
	submitter   EmailCodeSubmitter
	credentials Credentials
}

func NewEmailLogin(persistence EmailLoginPersistence, submitter EmailCodeSubmitter, credentials Credentials) (*EmailLogin, error) {
	if persistence == nil || submitter == nil {
		return nil, errors.New("email login dependencies are required")
	}
	return &EmailLogin{persistence: persistence, submitter: submitter, credentials: credentials}, nil
}

type RequestEmailCodeCommand struct {
	ChallengeID    string
	Email          string
	Source         string
	IdempotencyKey string
}

type EmailCodeMessage struct {
	Recipient      string
	Code           string
	IdempotencyKey string
}

func (login *EmailLogin) RequestCode(ctx context.Context, command RequestEmailCodeCommand) (EmailChallenge, error) {
	canonicalEmail, err := CanonicalEmail(command.Email)
	if err != nil {
		return EmailChallenge{}, err
	}
	code, err := login.credentials.EmailCode(command.ChallengeID, canonicalEmail)
	if err != nil {
		return EmailChallenge{}, err
	}
	message := EmailCodeMessage{
		Recipient: canonicalEmail, Code: code, IdempotencyKey: "carry-email-" + command.ChallengeID,
	}
	payloadDigest, err := login.submitter.PayloadDigest(message)
	if err != nil {
		return EmailChallenge{}, fmt.Errorf("prepare email submission payload: %w", err)
	}
	challenge, err := login.persistence.PrepareEmailChallenge(ctx, PrepareEmailChallengeCommand{
		ChallengeID: command.ChallengeID, CanonicalEmail: canonicalEmail,
		CodeDigest:     login.credentials.CodeDigest(command.ChallengeID, code),
		SourceDigest:   login.credentials.SourceDigest(command.Source),
		PayloadDigest:  payloadDigest,
		IdempotencyKey: command.IdempotencyKey,
		RequestDigest: login.credentials.RequestDigest(
			"request-email-code", command.ChallengeID, canonicalEmail,
		),
	})
	if err != nil {
		return EmailChallenge{}, err
	}
	if challenge.SubmissionState == EmailSubmissionRejected {
		return EmailChallenge{}, ErrEmailSubmissionRejected
	}
	if challenge.SubmissionState == EmailSubmissionAccepted || !challenge.CanSubmit {
		return challenge, nil
	}
	code, err = login.credentials.EmailCode(challenge.ChallengeID, challenge.CanonicalEmail)
	if err != nil {
		return EmailChallenge{}, err
	}
	message = EmailCodeMessage{
		Recipient: challenge.CanonicalEmail, Code: code, IdempotencyKey: "carry-email-" + challenge.ChallengeID,
	}
	submission := login.submitter.SubmitEmailCode(ctx, message, challenge.PayloadDigest)
	challenge, err = login.persistence.RecordEmailSubmission(
		ctx, challenge.ChallengeID, challenge.PayloadDigest, submission,
	)
	if err != nil {
		return EmailChallenge{}, err
	}
	if challenge.SubmissionState == EmailSubmissionRejected {
		return EmailChallenge{}, ErrEmailSubmissionRejected
	}
	return challenge, nil
}

type VerifyEmailCodeCommand struct {
	ChallengeID    string
	Code           string
	IdempotencyKey string
}

func (login *EmailLogin) VerifyCode(ctx context.Context, command VerifyEmailCodeCommand) (BrowserSession, error) {
	if len(command.Code) != EmailCodeDigits {
		return BrowserSession{}, ErrInvalidCode
	}
	for _, character := range command.Code {
		if character < '0' || character > '9' {
			return BrowserSession{}, ErrInvalidCode
		}
	}
	return login.persistence.VerifyEmailChallenge(ctx, VerifyEmailChallengeCommand{
		ChallengeID:    command.ChallengeID,
		CodeDigest:     login.credentials.CodeDigest(command.ChallengeID, command.Code),
		IdempotencyKey: command.IdempotencyKey,
		RequestDigest:  login.credentials.RequestDigest("verify-email-code", command.ChallengeID, command.Code),
		SessionID:      uuid.NewString(),
	})
}

type PrepareEmailChallengeCommand struct {
	ChallengeID    string
	CanonicalEmail string
	CodeDigest     [sha256.Size]byte
	SourceDigest   [sha256.Size]byte
	PayloadDigest  [sha256.Size]byte
	IdempotencyKey string
	RequestDigest  [sha256.Size]byte
}

type EmailChallenge struct {
	ChallengeID       string
	CanonicalEmail    string
	ExpiresAt         time.Time
	SubmissionState   EmailSubmissionState
	ProviderMessageID string
	PayloadDigest     [sha256.Size]byte
	CanSubmit         bool
}

type VerifyEmailChallengeCommand struct {
	ChallengeID    string
	CodeDigest     [sha256.Size]byte
	IdempotencyKey string
	RequestDigest  [sha256.Size]byte
	SessionID      string
}

type EmailSubmissionState string

const (
	EmailSubmissionPrepared EmailSubmissionState = "prepared"
	EmailSubmissionAccepted EmailSubmissionState = "accepted"
	EmailSubmissionRejected EmailSubmissionState = "rejected"
	EmailSubmissionUnknown  EmailSubmissionState = "unknown"
)

type EmailSubmission struct {
	State             EmailSubmissionState
	ProviderMessageID string
}
