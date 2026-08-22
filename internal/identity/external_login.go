package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrExternalLoginInvalid     = errors.New("external sign-in is invalid or expired")
	ErrExternalLoginConflict    = errors.New("external sign-in response conflicts with an earlier response")
	ErrExternalLoginDenied      = errors.New("external sign-in was cancelled")
	ErrExternalLoginUnavailable = errors.New("external sign-in could not be confirmed")
	ErrExternalLoginRejected    = errors.New("sign-in method could not be changed")
	ErrExternalLoginRateLimited = errors.New("external sign-in attempts are temporarily limited")
	// These diagnostic causes preserve operator recovery while unwrapping to the
	// one user recovery above. They never enter the public response body.
	ErrExternalLoginSourceAdmissionLimited = fmt.Errorf("external sign-in source admission limit: %w", ErrExternalLoginRateLimited)
	ErrExternalLoginGlobalAdmissionLimited = fmt.Errorf("external sign-in global admission limit: %w", ErrExternalLoginRateLimited)
)

const (
	// ExternalLoginSourceAdmissionLimit bounds live anonymous starts from one derived source.
	ExternalLoginSourceAdmissionLimit = 20
	// ExternalLoginGlobalAdmissionLimit bounds all live anonymous starts.
	ExternalLoginGlobalAdmissionLimit = 10_000
)

type ExternalLoginProvider string

const (
	GoogleLoginProvider ExternalLoginProvider = "google"
	GitHubLoginProvider ExternalLoginProvider = "github"
)

type ExternalCallbackOutcome string

const (
	ExternalCallbackCode        ExternalCallbackOutcome = "code"
	ExternalCallbackDenied      ExternalCallbackOutcome = "denied"
	ExternalCallbackUnavailable ExternalCallbackOutcome = "unavailable"
)

// GoogleLoginClient is the exact Google OIDC capability consumed by Identity.
type GoogleLoginClient interface {
	AuthorizationURL(state string, nonce string, codeChallenge string) string
	Authenticate(context.Context, string, string, string) (GoogleIdentityProof, error)
}

// GitHubLoginClient is the exact GitHub OAuth capability consumed by Identity.
type GitHubLoginClient interface {
	AuthorizationURL(state string, codeChallenge string) string
	Authenticate(context.Context, string, string) (GitHubIdentityProof, error)
}

type GoogleIdentityProof struct {
	Issuer  string
	Subject string
}

type GitHubIdentityProof struct {
	UserID int64
}

// ExternalLoginPersistence owns the short-lived login transaction and the
// atomic provider-identity, User, and Browser Session commit.
type ExternalLoginPersistence interface {
	CreateExternalLogin(context.Context, CreateExternalLoginCommand) (time.Time, error)
	CreateExternalIdentityProof(context.Context, CreateExternalIdentityProofCommand) (time.Time, error)
	ClaimExternalLogin(context.Context, ClaimExternalLoginCommand) (ExternalLoginClaim, error)
	CompleteGoogleLogin(context.Context, CompleteGoogleLoginCommand) (BrowserSession, error)
	CompleteGitHubLogin(context.Context, CompleteGitHubLoginCommand) (BrowserSession, error)
	RejectExternalLogin(context.Context, MarkExternalLoginUnknownCommand) error
	MarkExternalLoginUnknown(context.Context, MarkExternalLoginUnknownCommand) error
}

type ExternalLogin struct {
	persistence ExternalLoginPersistence
	google      GoogleLoginClient
	github      GitHubLoginClient
	credentials Credentials
}

func NewExternalLogin(
	persistence ExternalLoginPersistence,
	google GoogleLoginClient,
	github GitHubLoginClient,
	credentials Credentials,
) (*ExternalLogin, error) {
	if persistence == nil || google == nil || github == nil {
		return nil, errors.New("external login dependencies are required")
	}
	return &ExternalLogin{
		persistence: persistence,
		google:      google,
		github:      github,
		credentials: credentials,
	}, nil
}

type ExternalLoginStart struct {
	AuthorizationURL  string
	BrowserCredential string
	ExpiresAt         time.Time
}

func (login *ExternalLogin) StartGoogle(ctx context.Context, invitationID string, source string) (ExternalLoginStart, error) {
	return login.startLogin(ctx, GoogleLoginProvider, invitationID, source)
}

func (login *ExternalLogin) StartGoogleReauthentication(ctx context.Context, userID string, sessionID string) (ExternalLoginStart, error) {
	return login.startIdentityProof(ctx, GoogleLoginProvider, ReauthenticatePurpose, userID, sessionID)
}

func (login *ExternalLogin) StartGoogleLink(ctx context.Context, userID string, sessionID string) (ExternalLoginStart, error) {
	return login.startIdentityProof(ctx, GoogleLoginProvider, LinkPurpose, userID, sessionID)
}

func (login *ExternalLogin) StartGitHub(ctx context.Context, invitationID string, source string) (ExternalLoginStart, error) {
	return login.startLogin(ctx, GitHubLoginProvider, invitationID, source)
}

func (login *ExternalLogin) StartGitHubReauthentication(ctx context.Context, userID string, sessionID string) (ExternalLoginStart, error) {
	return login.startIdentityProof(ctx, GitHubLoginProvider, ReauthenticatePurpose, userID, sessionID)
}

func (login *ExternalLogin) StartGitHubLink(ctx context.Context, userID string, sessionID string) (ExternalLoginStart, error) {
	return login.startIdentityProof(ctx, GitHubLoginProvider, LinkPurpose, userID, sessionID)
}

func (login *ExternalLogin) startLogin(
	ctx context.Context,
	provider ExternalLoginProvider,
	invitationID string,
	source string,
) (ExternalLoginStart, error) {
	if (invitationID != "" && uuid.Validate(invitationID) != nil) || strings.TrimSpace(source) == "" {
		return ExternalLoginStart{}, ErrExternalLoginInvalid
	}
	transactionID := uuid.NewString()
	expiresAt, err := login.persistence.CreateExternalLogin(ctx, CreateExternalLoginCommand{
		TransactionID: transactionID,
		Provider:      provider,
		InvitationID:  invitationID,
		SourceDigest:  login.credentials.externalLoginSourceDigest(source),
	})
	if err != nil {
		return ExternalLoginStart{}, err
	}
	return login.authorizationStart(provider, transactionID, expiresAt)
}

func (login *ExternalLogin) startIdentityProof(
	ctx context.Context,
	provider ExternalLoginProvider,
	purpose ProofPurpose,
	userID string,
	sessionID string,
) (ExternalLoginStart, error) {
	if purpose != ReauthenticatePurpose && purpose != LinkPurpose {
		return ExternalLoginStart{}, ErrExternalLoginInvalid
	}
	transactionID := uuid.NewString()
	expiresAt, err := login.persistence.CreateExternalIdentityProof(ctx, CreateExternalIdentityProofCommand{
		TransactionID:       transactionID,
		Provider:            provider,
		Purpose:             purpose,
		TargetUserID:        userID,
		InitiatingSessionID: sessionID,
	})
	if err != nil {
		return ExternalLoginStart{}, err
	}
	return login.authorizationStart(provider, transactionID, expiresAt)
}

func (login *ExternalLogin) authorizationStart(
	provider ExternalLoginProvider,
	transactionID string,
	expiresAt time.Time,
) (ExternalLoginStart, error) {
	state, cookie, err := login.credentials.externalLoginBindings(transactionID, provider)
	if err != nil {
		return ExternalLoginStart{}, err
	}
	return ExternalLoginStart{
		AuthorizationURL:  login.authorizationURL(provider, transactionID, state),
		BrowserCredential: cookie,
		ExpiresAt:         expiresAt,
	}, nil
}

func (login *ExternalLogin) authorizationURL(provider ExternalLoginProvider, transactionID string, state string) string {
	switch provider {
	case GoogleLoginProvider:
		return login.google.AuthorizationURL(
			state, login.credentials.GoogleNonce(transactionID),
			login.credentials.PKCEChallenge(transactionID, provider),
		)
	case GitHubLoginProvider:
		return login.github.AuthorizationURL(
			state, login.credentials.PKCEChallenge(transactionID, provider),
		)
	default:
		return ""
	}
}

type ExternalLoginCallback struct {
	State             string
	BrowserCredential string
	Code              string
	ExactResponse     string
	Outcome           ExternalCallbackOutcome
}

func (login *ExternalLogin) CompleteGoogle(ctx context.Context, callback ExternalLoginCallback) (BrowserSession, error) {
	return login.complete(ctx, GoogleLoginProvider, callback)
}

func (login *ExternalLogin) CompleteGitHub(ctx context.Context, callback ExternalLoginCallback) (BrowserSession, error) {
	return login.complete(ctx, GitHubLoginProvider, callback)
}

func (login *ExternalLogin) complete(
	ctx context.Context,
	provider ExternalLoginProvider,
	callback ExternalLoginCallback,
) (BrowserSession, error) {
	transactionID, ok := login.credentials.ParseExternalLoginState(callback.State, provider)
	if !ok || !login.credentials.ValidExternalLoginBrowserCredential(callback.BrowserCredential, transactionID, provider) {
		return BrowserSession{}, ErrExternalLoginInvalid
	}
	if callback.ExactResponse == "" || !validExternalCallback(callback) {
		return BrowserSession{}, ErrExternalLoginInvalid
	}
	callbackDigest := login.credentials.RequestDigest(
		"external-login-callback", string(provider), transactionID, callback.ExactResponse,
	)
	claim, err := login.persistence.ClaimExternalLogin(ctx, ClaimExternalLoginCommand{
		TransactionID:  transactionID,
		Provider:       provider,
		CallbackDigest: callbackDigest,
		Outcome:        callback.Outcome,
	})
	if err != nil {
		return BrowserSession{}, ExternalProofFailure(claim.Purpose, claim.InvitationID, err)
	}
	if claim.IsReplay {
		claim.Session.InvitationID = claim.InvitationID
		return claim.Session, nil
	}

	var session BrowserSession
	var completionErr error
	switch provider {
	case GoogleLoginProvider:
		proof, err := login.google.Authenticate(
			ctx,
			callback.Code,
			login.credentials.PKCEVerifier(transactionID, provider),
			login.credentials.GoogleNonce(transactionID),
		)
		if err != nil {
			login.markUnknown(ctx, transactionID, provider, callbackDigest)
			return BrowserSession{}, ExternalProofFailure(claim.Purpose, claim.InvitationID, ErrExternalLoginUnavailable)
		}
		session, completionErr = login.completeGoogle(ctx, transactionID, callbackDigest, proof)
	case GitHubLoginProvider:
		proof, err := login.github.Authenticate(
			ctx, callback.Code, login.credentials.PKCEVerifier(transactionID, provider),
		)
		if err != nil {
			login.markUnknown(ctx, transactionID, provider, callbackDigest)
			return BrowserSession{}, ExternalProofFailure(claim.Purpose, claim.InvitationID, ErrExternalLoginUnavailable)
		}
		session, completionErr = login.completeGitHub(ctx, transactionID, callbackDigest, proof)
	default:
		return BrowserSession{}, ErrExternalLoginInvalid
	}
	if completionErr == nil {
		session.InvitationID = claim.InvitationID
		return session, nil
	}
	if reconciled, ok := login.reconcileCompletion(transactionID, provider, callbackDigest); ok {
		return reconciled, nil
	}
	if knownExternalProofRejection(completionErr) && login.reject(ctx, transactionID, provider, callbackDigest) {
		return BrowserSession{}, ExternalProofFailure(claim.Purpose, claim.InvitationID, ErrExternalLoginRejected)
	}
	login.markUnknown(ctx, transactionID, provider, callbackDigest)
	return BrowserSession{}, ExternalProofFailure(claim.Purpose, claim.InvitationID, ErrExternalLoginUnavailable)
}

func (login *ExternalLogin) completeGoogle(
	ctx context.Context,
	transactionID string,
	callbackDigest [sha256.Size]byte,
	proof GoogleIdentityProof,
) (BrowserSession, error) {
	if proof.Issuer != "https://accounts.google.com" || strings.TrimSpace(proof.Subject) == "" || len(proof.Subject) > 255 {
		return BrowserSession{}, ErrExternalLoginUnavailable
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return login.persistence.CompleteGoogleLogin(commitCtx, CompleteGoogleLoginCommand{
		TransactionID:  transactionID,
		CallbackDigest: callbackDigest,
		Issuer:         proof.Issuer,
		Subject:        proof.Subject,
		SessionID:      uuid.NewString(),
	})
}

func (login *ExternalLogin) completeGitHub(
	ctx context.Context,
	transactionID string,
	callbackDigest [sha256.Size]byte,
	proof GitHubIdentityProof,
) (BrowserSession, error) {
	if proof.UserID <= 0 {
		return BrowserSession{}, ErrExternalLoginUnavailable
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return login.persistence.CompleteGitHubLogin(commitCtx, CompleteGitHubLoginCommand{
		TransactionID:  transactionID,
		CallbackDigest: callbackDigest,
		GitHubUserID:   proof.UserID,
		SessionID:      uuid.NewString(),
	})
}

func (login *ExternalLogin) reconcileCompletion(
	transactionID string,
	provider ExternalLoginProvider,
	callbackDigest [sha256.Size]byte,
) (BrowserSession, bool) {
	reconcileCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	claim, err := login.persistence.ClaimExternalLogin(reconcileCtx, ClaimExternalLoginCommand{
		TransactionID:  transactionID,
		Provider:       provider,
		CallbackDigest: callbackDigest,
		Outcome:        ExternalCallbackCode,
	})
	if err != nil || !claim.IsReplay {
		return BrowserSession{}, false
	}
	return claim.Session, true
}

func (login *ExternalLogin) reject(
	ctx context.Context,
	transactionID string,
	provider ExternalLoginProvider,
	callbackDigest [sha256.Size]byte,
) bool {
	rejectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return login.persistence.RejectExternalLogin(rejectCtx, MarkExternalLoginUnknownCommand{
		TransactionID: transactionID, Provider: provider, CallbackDigest: callbackDigest,
	}) == nil
}

func knownExternalProofRejection(err error) bool {
	return errors.Is(err, ErrIdentityMethodOccupied) ||
		errors.Is(err, ErrIdentityMethodAlreadyLinked) ||
		errors.Is(err, ErrIdentityMethodNotLinked) ||
		errors.Is(err, ErrRecentIdentityProofRequired) ||
		errors.Is(err, ErrUnauthenticated)
}

func (login *ExternalLogin) markUnknown(
	ctx context.Context,
	transactionID string,
	provider ExternalLoginProvider,
	callbackDigest [sha256.Size]byte,
) {
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = login.persistence.MarkExternalLoginUnknown(markCtx, MarkExternalLoginUnknownCommand{
		TransactionID:  transactionID,
		Provider:       provider,
		CallbackDigest: callbackDigest,
	})
}

func validExternalCallback(callback ExternalLoginCallback) bool {
	switch callback.Outcome {
	case ExternalCallbackCode:
		return callback.Code != "" && len(callback.Code) <= 4096
	case ExternalCallbackDenied, ExternalCallbackUnavailable:
		return callback.Code == ""
	default:
		return false
	}
}

func (c Credentials) externalLoginBindings(
	transactionID string,
	provider ExternalLoginProvider,
) (string, string, error) {
	if uuid.Validate(transactionID) != nil || !validExternalLoginProvider(provider) {
		return "", "", errors.New("external login transaction is invalid")
	}
	stateMAC := c.mac("carry/external-login-state/v1", string(provider), transactionID)
	cookieMAC := c.mac("carry/external-login-browser/v1", string(provider), transactionID)
	state := "carry_oauth_state_" + transactionID + "." + base64.RawURLEncoding.EncodeToString(stateMAC[:])
	cookie := "carry_oauth_browser_" + transactionID + "." + base64.RawURLEncoding.EncodeToString(cookieMAC[:])
	return state, cookie, nil
}

func (c Credentials) ParseExternalLoginState(value string, provider ExternalLoginProvider) (string, bool) {
	return c.parseExternalLoginBinding(value, "carry_oauth_state_", "carry/external-login-state/v1", provider)
}

func (c Credentials) ValidExternalLoginBrowserCredential(
	value string,
	transactionID string,
	provider ExternalLoginProvider,
) bool {
	parsed, ok := c.parseExternalLoginBinding(
		value, "carry_oauth_browser_", "carry/external-login-browser/v1", provider,
	)
	return ok && parsed == transactionID
}

func (c Credentials) parseExternalLoginBinding(
	value string,
	prefix string,
	label string,
	provider ExternalLoginProvider,
) (string, bool) {
	if !validExternalLoginProvider(provider) || !strings.HasPrefix(value, prefix) {
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
	expected := c.mac(label, string(provider), parts[0])
	if subtle.ConstantTimeCompare(provided, expected[:]) != 1 {
		return "", false
	}
	return parts[0], true
}

func (c Credentials) GoogleNonce(transactionID string) string {
	digest := c.mac("carry/google-oidc-nonce/v1", transactionID)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (c Credentials) externalLoginSourceDigest(source string) [sha256.Size]byte {
	return c.mac("carry/external-login-source/v1", source)
}

func (c Credentials) PKCEVerifier(transactionID string, provider ExternalLoginProvider) string {
	digest := c.mac("carry/external-login-pkce/v1", string(provider), transactionID)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (c Credentials) PKCEChallenge(transactionID string, provider ExternalLoginProvider) string {
	digest := sha256.Sum256([]byte(c.PKCEVerifier(transactionID, provider)))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validExternalLoginProvider(provider ExternalLoginProvider) bool {
	return provider == GoogleLoginProvider || provider == GitHubLoginProvider
}

type CreateExternalLoginCommand struct {
	TransactionID string
	Provider      ExternalLoginProvider
	InvitationID  string
	SourceDigest  [sha256.Size]byte
}

type CreateExternalIdentityProofCommand struct {
	TransactionID       string
	Provider            ExternalLoginProvider
	Purpose             ProofPurpose
	TargetUserID        string
	InitiatingSessionID string
}

type ClaimExternalLoginCommand struct {
	TransactionID  string
	Provider       ExternalLoginProvider
	CallbackDigest [sha256.Size]byte
	Outcome        ExternalCallbackOutcome
}

type ExternalLoginClaim struct {
	IsReplay     bool
	Session      BrowserSession
	Purpose      ProofPurpose
	InvitationID string
}

type externalProofFailure struct {
	purpose      ProofPurpose
	invitationID string
	cause        error
}

func (failure externalProofFailure) Error() string { return failure.cause.Error() }
func (failure externalProofFailure) Unwrap() error { return failure.cause }

func ExternalProofFailure(purpose ProofPurpose, invitationID string, cause error) error {
	if !validProofPurpose(purpose) || cause == nil {
		return cause
	}
	return externalProofFailure{
		purpose:      purpose,
		invitationID: invitationID,
		cause:        cause,
	}
}

func ExternalProofFailurePurpose(err error) (ProofPurpose, bool) {
	var failure externalProofFailure
	if !errors.As(err, &failure) {
		return "", false
	}
	return failure.purpose, true
}

func ExternalProofFailureInvitationID(err error) string {
	var failure externalProofFailure
	if !errors.As(err, &failure) {
		return ""
	}
	return failure.invitationID
}

type CompleteGoogleLoginCommand struct {
	TransactionID  string
	CallbackDigest [sha256.Size]byte
	Issuer         string
	Subject        string
	SessionID      string
}

type CompleteGitHubLoginCommand struct {
	TransactionID  string
	CallbackDigest [sha256.Size]byte
	GitHubUserID   int64
	SessionID      string
}

type MarkExternalLoginUnknownCommand struct {
	TransactionID  string
	Provider       ExternalLoginProvider
	CallbackDigest [sha256.Size]byte
}

func (provider ExternalLoginProvider) String() string {
	return string(provider)
}

func (command ClaimExternalLoginCommand) Validate() error {
	if uuid.Validate(command.TransactionID) != nil || !validExternalLoginProvider(command.Provider) {
		return ErrExternalLoginInvalid
	}
	switch command.Outcome {
	case ExternalCallbackCode, ExternalCallbackDenied, ExternalCallbackUnavailable:
		return nil
	default:
		return fmt.Errorf("%w: callback outcome is invalid", ErrExternalLoginInvalid)
	}
}
