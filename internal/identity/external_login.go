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
	ClaimExternalLogin(context.Context, ClaimExternalLoginCommand) (ExternalLoginClaim, error)
	CompleteGoogleLogin(context.Context, CompleteGoogleLoginCommand) (BrowserSession, error)
	CompleteGitHubLogin(context.Context, CompleteGitHubLoginCommand) (BrowserSession, error)
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

func (login *ExternalLogin) StartGoogle(ctx context.Context) (ExternalLoginStart, error) {
	transactionID := uuid.NewString()
	expiresAt, err := login.persistence.CreateExternalLogin(ctx, CreateExternalLoginCommand{
		TransactionID: transactionID,
		Provider:      GoogleLoginProvider,
	})
	if err != nil {
		return ExternalLoginStart{}, err
	}
	state, cookie, err := login.credentials.externalLoginBindings(transactionID, GoogleLoginProvider)
	if err != nil {
		return ExternalLoginStart{}, err
	}
	return ExternalLoginStart{
		AuthorizationURL: login.google.AuthorizationURL(
			state,
			login.credentials.GoogleNonce(transactionID),
			login.credentials.PKCEChallenge(transactionID, GoogleLoginProvider),
		),
		BrowserCredential: cookie,
		ExpiresAt:         expiresAt,
	}, nil
}

func (login *ExternalLogin) StartGitHub(ctx context.Context) (ExternalLoginStart, error) {
	transactionID := uuid.NewString()
	expiresAt, err := login.persistence.CreateExternalLogin(ctx, CreateExternalLoginCommand{
		TransactionID: transactionID,
		Provider:      GitHubLoginProvider,
	})
	if err != nil {
		return ExternalLoginStart{}, err
	}
	state, cookie, err := login.credentials.externalLoginBindings(transactionID, GitHubLoginProvider)
	if err != nil {
		return ExternalLoginStart{}, err
	}
	return ExternalLoginStart{
		AuthorizationURL: login.github.AuthorizationURL(
			state,
			login.credentials.PKCEChallenge(transactionID, GitHubLoginProvider),
		),
		BrowserCredential: cookie,
		ExpiresAt:         expiresAt,
	}, nil
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
		return BrowserSession{}, err
	}
	if claim.IsReplay {
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
			return BrowserSession{}, ErrExternalLoginUnavailable
		}
		session, completionErr = login.completeGoogle(ctx, transactionID, callbackDigest, proof)
	case GitHubLoginProvider:
		proof, err := login.github.Authenticate(
			ctx, callback.Code, login.credentials.PKCEVerifier(transactionID, provider),
		)
		if err != nil {
			login.markUnknown(ctx, transactionID, provider, callbackDigest)
			return BrowserSession{}, ErrExternalLoginUnavailable
		}
		session, completionErr = login.completeGitHub(ctx, transactionID, callbackDigest, proof)
	default:
		return BrowserSession{}, ErrExternalLoginInvalid
	}
	if completionErr == nil {
		return session, nil
	}
	if reconciled, ok := login.reconcileCompletion(transactionID, provider, callbackDigest); ok {
		return reconciled, nil
	}
	login.markUnknown(ctx, transactionID, provider, callbackDigest)
	return BrowserSession{}, ErrExternalLoginUnavailable
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
}

type ClaimExternalLoginCommand struct {
	TransactionID  string
	Provider       ExternalLoginProvider
	CallbackDigest [sha256.Size]byte
	Outcome        ExternalCallbackOutcome
}

type ExternalLoginClaim struct {
	IsReplay bool
	Session  BrowserSession
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
