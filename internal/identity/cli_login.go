package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	CLILoginLifetime        = 15 * time.Minute
	CLICredentialLifetime   = 90 * 24 * time.Hour
	CLIRedeemReplayLifetime = 5 * time.Minute
	CLILoginInitialInterval = 5 * time.Second
	CLILoginMaximumInterval = 30 * time.Second
	CLILabelMaximumBytes    = 128
)

var (
	ErrInvalidCLILogin          = errors.New("CLI login request is invalid")
	ErrCLILoginUnavailable      = errors.New("CLI login request is unavailable")
	ErrCLILoginRateLimited      = errors.New("CLI login attempts are temporarily limited")
	ErrCLILoginConflict         = errors.New("CLI login idempotency key was reused for a different request")
	ErrCLILoginPending          = errors.New("CLI login is awaiting Browser approval")
	ErrCLILoginDenied           = errors.New("CLI login was denied")
	ErrCLILoginCancelled        = errors.New("CLI login was cancelled")
	ErrCLILoginExpired          = errors.New("CLI login expired")
	ErrCLILoginSlowDown         = errors.New("CLI login polling is too frequent")
	ErrCLILoginAlreadyApproved  = errors.New("CLI login was already approved")
	ErrCLILoginRedeemed         = errors.New("CLI login was already redeemed")
	ErrCLIReplacementInvalid    = errors.New("replacement CLI credential is unavailable")
	ErrCLICredentialUnavailable = errors.New("CLI credential is unavailable")
)

type CLISlowDownError struct {
	RetryAfter time.Duration
}

func (err CLISlowDownError) Error() string        { return ErrCLILoginSlowDown.Error() }
func (err CLISlowDownError) Is(target error) bool { return target == ErrCLILoginSlowDown }

func NewCLISlowDownError(retryAfter time.Duration) error {
	return CLISlowDownError{RetryAfter: retryAfter}
}

// CLILoginPersistence is the complete PostgreSQL authority consumed by Identity's CLI ceremony.
type CLILoginPersistence interface {
	BeginCLILogin(context.Context, BeginCLILoginCommand) (CLILoginRequest, error)
	LookupCLILogin(context.Context, LookupCLILoginCommand) (CLILoginRequest, error)
	ApproveCLILogin(context.Context, ApproveCLILoginCommand) (CLILoginRequest, error)
	DenyCLILogin(context.Context, DenyCLILoginCommand) error
	PollCLILogin(context.Context, PollCLILoginCommand) (RedeemedCLICredential, error)
	CancelCLILogin(context.Context, CancelCLILoginCommand) error
	ListCLICredentials(context.Context, ListCLICredentialsCommand) ([]CLICredential, error)
	RevokeCLICredential(context.Context, RevokeCLICredentialCommand) error
}

type CLILogin struct {
	persistence CLILoginPersistence
	credentials Credentials
	origin      string
}

func NewCLILogin(persistence CLILoginPersistence, credentials Credentials, canonicalOrigin string) (*CLILogin, error) {
	if persistence == nil || !validCanonicalOrigin(canonicalOrigin) {
		return nil, errors.New("CLI login dependencies are required")
	}
	return &CLILogin{persistence: persistence, credentials: credentials, origin: canonicalOrigin}, nil
}

type BeginCLILoginRequest struct {
	RequestID, IdempotencyKey, Label, ProposedReplacementCredentialID, Source string
}

type BeginCLILoginCommand struct {
	RequestID, IdempotencyKey, Label, ProposedReplacementCredentialID string
	RequestDigest, SourceDigest, UserCodeDigest                       [sha256.Size]byte
	CodeGeneration                                                    int16
}

type CLILoginRequest struct {
	RequestID, Label, ProposedReplacementCredentialID string
	CodeGeneration                                    int16
	CreatedAt, ExpiresAt                              time.Time
	PollInterval                                      time.Duration
	ApprovedByUserID, ApprovedSpaceID                 string
	ApprovedAt, DeniedAt, CancelledAt, RedeemedAt     *time.Time
}

type BegunCLILogin struct {
	RequestID, Label, UserCode, PollSecret, VerificationPath string
	ExpiresAt                                                time.Time
	PollInterval                                             time.Duration
}

func (login *CLILogin) Begin(ctx context.Context, request BeginCLILoginRequest) (BegunCLILogin, error) {
	label := strings.TrimSpace(request.Label)
	if uuid.Validate(request.RequestID) != nil || !validIdempotencyKey(request.IdempotencyKey) ||
		label == "" || len([]byte(label)) > CLILabelMaximumBytes ||
		(request.ProposedReplacementCredentialID != "" && uuid.Validate(request.ProposedReplacementCredentialID) != nil) ||
		strings.TrimSpace(request.Source) == "" {
		return BegunCLILogin{}, ErrInvalidCLILogin
	}
	digest := cliRequestDigest(request.RequestID, label, request.ProposedReplacementCredentialID)
	for generation := range int16(5) {
		code := login.credentials.CLIUserCode(request.RequestID, generation)
		created, err := login.persistence.BeginCLILogin(ctx, BeginCLILoginCommand{
			RequestID: request.RequestID, IdempotencyKey: request.IdempotencyKey, Label: label,
			ProposedReplacementCredentialID: request.ProposedReplacementCredentialID,
			RequestDigest:                   digest, SourceDigest: login.credentials.SourceDigest(request.Source),
			UserCodeDigest: login.credentials.CLIUserCodeDigest(code), CodeGeneration: generation,
		})
		if errors.Is(err, errCLIUserCodeCollision) {
			continue
		}
		if err != nil {
			return BegunCLILogin{}, err
		}
		actualCode := login.credentials.CLIUserCode(created.RequestID, created.CodeGeneration)
		pollSecret, credentialErr := login.credentials.CLIPollCredential(created.RequestID, login.origin)
		if credentialErr != nil {
			return BegunCLILogin{}, credentialErr
		}
		return BegunCLILogin{
			RequestID: created.RequestID, Label: created.Label, UserCode: actualCode,
			PollSecret: pollSecret, VerificationPath: "/cli-login",
			ExpiresAt: created.ExpiresAt, PollInterval: created.PollInterval,
		}, nil
	}
	return BegunCLILogin{}, ErrCLILoginRateLimited
}

// errCLIUserCodeCollision is returned only by the persistence adapter so Identity can derive another code.
var errCLIUserCodeCollision = errors.New("CLI user code collision")

func CLIUserCodeCollision() error { return errCLIUserCodeCollision }

type LookupCLILoginRequest struct {
	BrowserSessionID, UserCode, Source string
}

type LookupCLILoginCommand struct {
	BrowserSessionID             string
	SourceDigest, UserCodeDigest [sha256.Size]byte
}

type CLILoginPreview struct {
	RequestID, UserCode, Label, ProposedReplacementCredentialID string
	CreatedAt, ExpiresAt                                        time.Time
	Approved, Denied, Cancelled, Redeemed                       bool
	ApprovedSpaceID                                             string
}

func (login *CLILogin) Lookup(ctx context.Context, request LookupCLILoginRequest) (CLILoginPreview, error) {
	code, ok := NormalizeCLIUserCode(request.UserCode)
	if !ok || uuid.Validate(request.BrowserSessionID) != nil || strings.TrimSpace(request.Source) == "" {
		return CLILoginPreview{}, ErrInvalidCLILogin
	}
	found, err := login.persistence.LookupCLILogin(ctx, LookupCLILoginCommand{
		BrowserSessionID: request.BrowserSessionID,
		SourceDigest:     login.credentials.SourceDigest(request.Source),
		UserCodeDigest:   login.credentials.CLIUserCodeDigest(code),
	})
	if err != nil {
		return CLILoginPreview{}, err
	}
	return CLILoginPreview{
		RequestID: found.RequestID, UserCode: code, Label: found.Label,
		ProposedReplacementCredentialID: found.ProposedReplacementCredentialID,
		CreatedAt:                       found.CreatedAt, ExpiresAt: found.ExpiresAt,
		Approved: found.ApprovedAt != nil, Denied: found.DeniedAt != nil,
		Cancelled: found.CancelledAt != nil, Redeemed: found.RedeemedAt != nil,
		ApprovedSpaceID: found.ApprovedSpaceID,
	}, nil
}

type ApproveCLILoginRequest struct {
	BrowserSessionID, RequestID, UserCode, SpaceID, ReplacementCredentialID, IdempotencyKey string
}

type ApproveCLILoginCommand struct {
	BrowserSessionID, RequestID, SpaceID, ReplacementCredentialID, IdempotencyKey, CredentialID string
	UserCodeDigest, RequestDigest                                                               [sha256.Size]byte
}

func (login *CLILogin) Approve(ctx context.Context, request ApproveCLILoginRequest) error {
	code, ok := NormalizeCLIUserCode(request.UserCode)
	if !ok || uuid.Validate(request.BrowserSessionID) != nil || uuid.Validate(request.RequestID) != nil ||
		uuid.Validate(request.SpaceID) != nil || !validIdempotencyKey(request.IdempotencyKey) ||
		(request.ReplacementCredentialID != "" && uuid.Validate(request.ReplacementCredentialID) != nil) {
		return ErrInvalidCLILogin
	}
	digest := cliRequestDigest(request.RequestID, code, request.SpaceID, request.ReplacementCredentialID)
	_, err := login.persistence.ApproveCLILogin(ctx, ApproveCLILoginCommand{
		BrowserSessionID: request.BrowserSessionID, RequestID: request.RequestID,
		SpaceID: request.SpaceID, ReplacementCredentialID: request.ReplacementCredentialID,
		IdempotencyKey: request.IdempotencyKey, CredentialID: uuid.NewString(),
		UserCodeDigest: login.credentials.CLIUserCodeDigest(code), RequestDigest: digest,
	})
	return err
}

type DenyCLILoginRequest struct {
	BrowserSessionID, RequestID, UserCode, IdempotencyKey string
}

type DenyCLILoginCommand struct {
	BrowserSessionID, RequestID, IdempotencyKey string
	UserCodeDigest, RequestDigest               [sha256.Size]byte
}

func (login *CLILogin) Deny(ctx context.Context, request DenyCLILoginRequest) error {
	code, ok := NormalizeCLIUserCode(request.UserCode)
	if !ok || uuid.Validate(request.BrowserSessionID) != nil || uuid.Validate(request.RequestID) != nil || !validIdempotencyKey(request.IdempotencyKey) {
		return ErrInvalidCLILogin
	}
	return login.persistence.DenyCLILogin(ctx, DenyCLILoginCommand{
		BrowserSessionID: request.BrowserSessionID, RequestID: request.RequestID,
		IdempotencyKey: request.IdempotencyKey, UserCodeDigest: login.credentials.CLIUserCodeDigest(code),
		RequestDigest: cliRequestDigest(request.RequestID, code, "deny"),
	})
}

type PollCLILoginCommand struct{ RequestID string }

type RedeemedCLICredential struct {
	CredentialID, UserID, SpaceID, Label string
	ExpiresAt                            time.Time
}

type CLICredentialResult struct {
	RedeemedCLICredential
	Credential string
}

func (login *CLILogin) Poll(ctx context.Context, pollSecret string) (CLICredentialResult, error) {
	requestID, ok := login.credentials.ParseCLIPollCredential(pollSecret, login.origin)
	if !ok {
		return CLICredentialResult{}, ErrUnauthenticated
	}
	result, err := login.persistence.PollCLILogin(ctx, PollCLILoginCommand{RequestID: requestID})
	if err != nil {
		return CLICredentialResult{}, err
	}
	credential, err := login.credentials.CLICredential(result.CredentialID, login.origin)
	if err != nil {
		return CLICredentialResult{}, err
	}
	return CLICredentialResult{RedeemedCLICredential: result, Credential: credential}, nil
}

type CancelCLILoginCommand struct{ RequestID string }

func (login *CLILogin) Cancel(ctx context.Context, pollSecret string) error {
	requestID, ok := login.credentials.ParseCLIPollCredential(pollSecret, login.origin)
	if !ok {
		return ErrUnauthenticated
	}
	return login.persistence.CancelCLILogin(ctx, CancelCLILoginCommand{RequestID: requestID})
}

type ListCLICredentialsCommand struct{ BrowserSessionID string }

type CLICredential struct {
	CredentialID, Label, ApprovedSpaceID, ApprovedSpaceName string
	CreatedAt, ExpiresAt                                    time.Time
}

func (login *CLILogin) ListCredentials(ctx context.Context, browserSessionID string) ([]CLICredential, error) {
	if uuid.Validate(browserSessionID) != nil {
		return nil, ErrInvalidCLILogin
	}
	return login.persistence.ListCLICredentials(ctx, ListCLICredentialsCommand{BrowserSessionID: browserSessionID})
}

type RevokeCLICredentialCommand struct {
	BrowserSessionID, CredentialID, IdempotencyKey string
	RequestDigest                                  [sha256.Size]byte
	Self                                           bool
}

func (login *CLILogin) RevokeFromBrowser(ctx context.Context, browserSessionID, credentialID, idempotencyKey string) error {
	if uuid.Validate(browserSessionID) != nil || uuid.Validate(credentialID) != nil || !validIdempotencyKey(idempotencyKey) {
		return ErrInvalidCLILogin
	}
	return login.persistence.RevokeCLICredential(ctx, RevokeCLICredentialCommand{
		BrowserSessionID: browserSessionID, CredentialID: credentialID, IdempotencyKey: idempotencyKey,
		RequestDigest: cliRequestDigest(credentialID, "browser-revoke"),
	})
}

func (login *CLILogin) RevokeCurrent(ctx context.Context, credential, idempotencyKey string) error {
	credentialID, ok := login.credentials.ParseCLICredential(credential, login.origin)
	if !ok || !validIdempotencyKey(idempotencyKey) {
		return ErrCLICredentialUnavailable
	}
	return login.persistence.RevokeCLICredential(ctx, RevokeCLICredentialCommand{
		CredentialID: credentialID, IdempotencyKey: idempotencyKey, Self: true,
		RequestDigest: cliRequestDigest(credentialID, "self-revoke"),
	})
}

func (c Credentials) CLIUserCode(requestID string, generation int16) string {
	mac := c.mac("carry/cli-user-code/v1", requestID, fmt.Sprintf("%d", generation))
	alphabet := "BCDFGHJKLMNPQRSTVWXZ"
	var value uint64
	for _, b := range mac[:8] {
		value = value<<8 | uint64(b)
	}
	characters := make([]byte, 10)
	for index := len(characters) - 1; index >= 0; index-- {
		characters[index] = alphabet[value%uint64(len(alphabet))]
		value /= uint64(len(alphabet))
	}
	return string(characters[:4]) + "-" + string(characters[4:7]) + "-" + string(characters[7:])
}

func NormalizeCLIUserCode(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "", " ", "").Replace(value)
	if len(value) != 10 {
		return "", false
	}
	const alphabet = "BCDFGHJKLMNPQRSTVWXZ"
	for _, character := range value {
		if !strings.ContainsRune(alphabet, character) {
			return "", false
		}
	}
	return value[:4] + "-" + value[4:7] + "-" + value[7:], true
}

func (c Credentials) CLIUserCodeDigest(code string) [sha256.Size]byte {
	return c.mac("carry/cli-user-code-digest/v1", code)
}

func (c Credentials) CLIPollCredential(requestID, origin string) (string, error) {
	if uuid.Validate(requestID) != nil || !validCanonicalOrigin(origin) {
		return "", ErrInvalidCLILogin
	}
	mac := c.mac("carry/cli-poll/v1", requestID, origin)
	return "carry_cli_poll_" + requestID + "." + base64.RawURLEncoding.EncodeToString(mac[:]), nil
}

func (c Credentials) ParseCLIPollCredential(value, origin string) (string, bool) {
	return c.parseCLIMACCredential(value, "carry_cli_poll_", "carry/cli-poll/v1", origin)
}

func (c Credentials) CLICredential(credentialID, origin string) (string, error) {
	if uuid.Validate(credentialID) != nil || !validCanonicalOrigin(origin) {
		return "", ErrInvalidCLILogin
	}
	mac := c.mac("carry/cli-credential/v1", credentialID, origin)
	return "carry_cli_" + credentialID + "." + base64.RawURLEncoding.EncodeToString(mac[:]), nil
}

func (c Credentials) ParseCLICredential(value, origin string) (string, bool) {
	return c.parseCLIMACCredential(value, "carry_cli_", "carry/cli-credential/v1", origin)
}

func (c Credentials) parseCLIMACCredential(value, prefix, label, origin string) (string, bool) {
	if !validCanonicalOrigin(origin) || !strings.HasPrefix(value, prefix) {
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
	expected := c.mac(label, parts[0], origin)
	if !equalMAC(provided, expected[:]) {
		return "", false
	}
	return parts[0], true
}

func equalMAC(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func cliRequestDigest(parts ...string) [sha256.Size]byte {
	encoded, _ := json.Marshal(parts)
	return sha256.Sum256(encoded)
}

func validIdempotencyKey(value string) bool {
	return strings.TrimSpace(value) != "" && len([]byte(value)) <= 255
}

func validCanonicalOrigin(value string) bool {
	return strings.HasPrefix(value, "https://") && !strings.Contains(strings.TrimPrefix(value, "https://"), "/")
}
