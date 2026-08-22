package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestExternalLoginBindingsUseIndependentDomainsAndProviders(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	transactionID := "11111111-1111-4111-8111-111111111111"
	googleState, googleCookie, err := credentials.externalLoginBindings(transactionID, GoogleLoginProvider)
	if err != nil {
		t.Fatalf("derive Google bindings: %v", err)
	}
	githubState, githubCookie, err := credentials.externalLoginBindings(transactionID, GitHubLoginProvider)
	if err != nil {
		t.Fatalf("derive GitHub bindings: %v", err)
	}
	if googleState == googleCookie || googleState == githubState || googleCookie == githubCookie {
		t.Fatal("external login bindings reused a MAC domain")
	}
	if parsed, ok := credentials.ParseExternalLoginState(googleState, GoogleLoginProvider); !ok || parsed != transactionID {
		t.Fatalf("Google state parsed as %q, %t", parsed, ok)
	}
	if _, ok := credentials.ParseExternalLoginState(googleState, GitHubLoginProvider); ok {
		t.Fatal("Google state was accepted by GitHub")
	}
	if credentials.ValidExternalLoginBrowserCredential(googleState, transactionID, GoogleLoginProvider) {
		t.Fatal("state was accepted as the browser credential")
	}
	if !credentials.ValidExternalLoginBrowserCredential(googleCookie, transactionID, GoogleLoginProvider) {
		t.Fatal("valid browser credential was rejected")
	}
	tampered := googleState[:len(googleState)-1] + "A"
	if _, ok := credentials.ParseExternalLoginState(tampered, GoogleLoginProvider); ok {
		t.Fatal("tampered state was accepted")
	}
}

func TestGoogleNonceAndPKCEAreTransactionAndProviderBound(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	first := "11111111-1111-4111-8111-111111111111"
	second := "22222222-2222-4222-8222-222222222222"
	if credentials.GoogleNonce(first) == credentials.GoogleNonce(second) {
		t.Fatal("Google nonce did not change with the transaction")
	}
	googleVerifier := credentials.PKCEVerifier(first, GoogleLoginProvider)
	githubVerifier := credentials.PKCEVerifier(first, GitHubLoginProvider)
	if googleVerifier == githubVerifier || len(googleVerifier) < 43 {
		t.Fatal("PKCE verifier was not provider-bound or long enough")
	}
	expected := sha256.Sum256([]byte(googleVerifier))
	if got := credentials.PKCEChallenge(first, GoogleLoginProvider); got == googleVerifier || got == "" {
		t.Fatalf("PKCE challenge = %q", got)
	} else if !bytes.Equal(mustDecodeRawURL(t, got), expected[:]) {
		t.Fatal("PKCE challenge is not S256")
	}
}

func TestExternalLoginInvitationContinuationSurvivesSuccessDenialAndReplay(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{}
	google := &recordingGoogleLogin{}
	login, err := NewExternalLogin(persistence, google, &recordingGitHubLogin{}, credentials)
	if err != nil {
		t.Fatal(err)
	}
	invitationID := "33333333-3333-4333-8333-333333333333"
	start, err := login.StartGoogle(context.Background(), invitationID, "198.51.100.10")
	if err != nil || persistence.loginCreate.InvitationID != invitationID {
		t.Fatalf("start continuation = %#v, %v", persistence.loginCreate, err)
	}
	state := queryParameter(t, start.AuthorizationURL, "state")
	callback := ExternalLoginCallback{
		State:             state,
		BrowserCredential: start.BrowserCredential,
		Code:              "provider-code",
		ExactResponse:     "code=provider-code&state=" + state,
		Outcome:           ExternalCallbackCode,
	}
	completed, err := login.CompleteGoogle(context.Background(), callback)
	if err != nil || completed.InvitationID != invitationID {
		t.Fatalf("completed continuation = %#v, %v", completed, err)
	}
	persistence.replay = true
	replayed, err := login.CompleteGoogle(context.Background(), callback)
	if err != nil || replayed.InvitationID != invitationID || google.calls != 1 {
		t.Fatalf("replayed continuation = %#v, calls %d, %v", replayed, google.calls, err)
	}

	deniedPersistence := &externalLoginMemoryPersistence{}
	deniedLogin, _ := NewExternalLogin(deniedPersistence, &recordingGoogleLogin{}, &recordingGitHubLogin{}, credentials)
	deniedStart, _ := deniedLogin.StartGoogle(context.Background(), invitationID, "198.51.100.10")
	deniedState := queryParameter(t, deniedStart.AuthorizationURL, "state")
	_, err = deniedLogin.CompleteGoogle(context.Background(), ExternalLoginCallback{
		State:             deniedState,
		BrowserCredential: deniedStart.BrowserCredential,
		ExactResponse:     "error=access_denied&state=" + deniedState,
		Outcome:           ExternalCallbackDenied,
	})
	if !errors.Is(err, ErrExternalLoginDenied) || ExternalProofFailureInvitationID(err) != invitationID {
		t.Fatalf("denied continuation = %q, %v", ExternalProofFailureInvitationID(err), err)
	}
	if _, err := login.StartGoogle(context.Background(), "not-a-uuid", "198.51.100.10"); !errors.Is(err, ErrExternalLoginInvalid) {
		t.Fatalf("malformed continuation = %v", err)
	}
}

func TestExternalLoginExactReplaySkipsProviderAndReturnsCommittedSession(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{}
	google := &recordingGoogleLogin{}
	github := &recordingGitHubLogin{}
	login, err := NewExternalLogin(persistence, google, github, credentials)
	if err != nil {
		t.Fatalf("compose external login: %v", err)
	}
	start, err := login.StartGoogle(context.Background(), "", "198.51.100.10")
	if err != nil {
		t.Fatalf("start Google login: %v", err)
	}
	state := queryParameter(t, start.AuthorizationURL, "state")
	callback := ExternalLoginCallback{
		State: state, BrowserCredential: start.BrowserCredential, Code: "provider-code",
		ExactResponse: "state=" + state + "&code=provider-code", Outcome: ExternalCallbackCode,
	}
	first, err := login.CompleteGoogle(context.Background(), callback)
	if err != nil {
		t.Fatalf("complete Google login: %v", err)
	}
	persistence.replay = true
	replayed, err := login.CompleteGoogle(context.Background(), callback)
	if err != nil {
		t.Fatalf("replay Google login: %v", err)
	}
	if replayed != first || google.calls != 1 {
		t.Fatalf("replayed session = %#v, first = %#v, provider calls = %d", replayed, first, google.calls)
	}
}

func TestExternalLoginCompletionResponseLossReconcilesCommittedSession(t *testing.T) {
	t.Parallel()
	for _, provider := range []ExternalLoginProvider{GoogleLoginProvider, GitHubLoginProvider} {
		t.Run(provider.String(), func(t *testing.T) {
			t.Parallel()
			credentials := externalLoginTestCredentials(t)
			persistence := &externalLoginMemoryPersistence{
				completionErr: errors.New("commit response was lost"), commitOnCompletionError: true,
			}
			google := &recordingGoogleLogin{}
			github := &recordingGitHubLogin{}
			login, err := NewExternalLogin(persistence, google, github, credentials)
			if err != nil {
				t.Fatalf("compose external login: %v", err)
			}
			var start ExternalLoginStart
			if provider == GoogleLoginProvider {
				start, err = login.StartGoogle(context.Background(), "", "198.51.100.10")
			} else {
				start, err = login.StartGitHub(context.Background(), "", "198.51.100.10")
			}
			if err != nil {
				t.Fatalf("start %s login: %v", provider, err)
			}
			state := queryParameter(t, start.AuthorizationURL, "state")
			callback := ExternalLoginCallback{
				State: state, BrowserCredential: start.BrowserCredential, Code: "provider-code",
				ExactResponse: "code=provider-code&state=" + state, Outcome: ExternalCallbackCode,
			}
			var recovered BrowserSession
			if provider == GoogleLoginProvider {
				recovered, err = login.CompleteGoogle(context.Background(), callback)
			} else {
				recovered, err = login.CompleteGitHub(context.Background(), callback)
			}
			if err != nil {
				t.Fatalf("reconcile %s completion: %v", provider, err)
			}
			if recovered != persistence.session || persistence.unknown || persistence.claimCalls != 2 {
				t.Fatalf("recovered = %#v, committed = %#v, unknown = %t, claims = %d", recovered, persistence.session, persistence.unknown, persistence.claimCalls)
			}
			if google.calls+github.calls != 1 {
				t.Fatalf("provider calls = %d", google.calls+github.calls)
			}
		})
	}
}

func TestExternalLoginUncommittedCompletionErrorConvergesToUnknown(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{completionErr: errors.New("commit failed")}
	google := &recordingGoogleLogin{}
	login, err := NewExternalLogin(persistence, google, &recordingGitHubLogin{}, credentials)
	if err != nil {
		t.Fatalf("compose external login: %v", err)
	}
	start, err := login.StartGoogle(context.Background(), "", "198.51.100.10")
	if err != nil {
		t.Fatalf("start Google login: %v", err)
	}
	state := queryParameter(t, start.AuthorizationURL, "state")
	_, err = login.CompleteGoogle(context.Background(), ExternalLoginCallback{
		State: state, BrowserCredential: start.BrowserCredential, Code: "provider-code",
		ExactResponse: "code=provider-code&state=" + state, Outcome: ExternalCallbackCode,
	})
	if !errors.Is(err, ErrExternalLoginUnavailable) || !persistence.unknown || persistence.claimCalls != 2 || google.calls != 1 {
		t.Fatalf("error = %v, unknown = %t, claims = %d, provider calls = %d", err, persistence.unknown, persistence.claimCalls, google.calls)
	}
}

func TestExternalLoginProviderFailureBecomesUnknownWithoutCompletion(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{}
	google := &recordingGoogleLogin{err: errors.New("token response was lost")}
	login, err := NewExternalLogin(persistence, google, &recordingGitHubLogin{}, credentials)
	if err != nil {
		t.Fatalf("compose external login: %v", err)
	}
	start, err := login.StartGoogle(context.Background(), "", "198.51.100.10")
	if err != nil {
		t.Fatalf("start Google login: %v", err)
	}
	state := queryParameter(t, start.AuthorizationURL, "state")
	_, err = login.CompleteGoogle(context.Background(), ExternalLoginCallback{
		State: state, BrowserCredential: start.BrowserCredential, Code: "ambiguous-code",
		ExactResponse: "code=ambiguous-code&state=" + state, Outcome: ExternalCallbackCode,
	})
	if !errors.Is(err, ErrExternalLoginUnavailable) {
		t.Fatalf("ambiguous exchange error = %v", err)
	}
	if persistence.googleCompleted || !persistence.unknown {
		t.Fatalf("completed = %t, unknown = %t", persistence.googleCompleted, persistence.unknown)
	}
}

func TestExternalLoginMethodFailuresRetainStoredPurpose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		outcome       ExternalCallbackOutcome
		providerError error
		completionErr error
		want          error
	}{
		{name: "denied", outcome: ExternalCallbackDenied, want: ErrExternalLoginDenied},
		{name: "unavailable", outcome: ExternalCallbackCode, providerError: errors.New("provider response was lost"), want: ErrExternalLoginUnavailable},
		{name: "rejected", outcome: ExternalCallbackCode, completionErr: ErrIdentityMethodOccupied, want: ErrExternalLoginRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			credentials := externalLoginTestCredentials(t)
			persistence := &externalLoginMemoryPersistence{completionErr: test.completionErr}
			login, err := NewExternalLogin(
				persistence,
				&recordingGoogleLogin{err: test.providerError},
				&recordingGitHubLogin{},
				credentials,
			)
			if err != nil {
				t.Fatalf("compose external login: %v", err)
			}
			start, err := login.StartGoogleLink(
				context.Background(),
				"11111111-1111-4111-8111-111111111111",
				"22222222-2222-4222-8222-222222222222",
			)
			if err != nil {
				t.Fatalf("start Google link: %v", err)
			}
			state := queryParameter(t, start.AuthorizationURL, "state")
			callback := ExternalLoginCallback{
				State: state, BrowserCredential: start.BrowserCredential,
				ExactResponse: "code=provider-code&state=" + state, Outcome: test.outcome,
			}
			if test.outcome == ExternalCallbackCode {
				callback.Code = "provider-code"
			} else {
				callback.ExactResponse = "error=access_denied&state=" + state
			}
			_, err = login.CompleteGoogle(context.Background(), callback)
			purpose, hasPurpose := ExternalProofFailurePurpose(err)
			if !errors.Is(err, test.want) || !hasPurpose || purpose != LinkPurpose {
				t.Fatalf("method failure = %v, purpose = %q/%t", err, purpose, hasPurpose)
			}
		})
	}
}

func TestExternalLoginStartsFixedReauthenticationAndLinkPurposes(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{}
	login, err := NewExternalLogin(persistence, &recordingGoogleLogin{}, &recordingGitHubLogin{}, credentials)
	if err != nil {
		t.Fatalf("compose external login: %v", err)
	}
	userID := "11111111-1111-4111-8111-111111111111"
	sessionID := "22222222-2222-4222-8222-222222222222"
	if _, err := login.StartGoogleReauthentication(context.Background(), userID, sessionID); err != nil {
		t.Fatalf("start Google reauthentication: %v", err)
	}
	if persistence.proofCreate.Purpose != ReauthenticatePurpose || persistence.proofCreate.Provider != GoogleLoginProvider ||
		persistence.proofCreate.TargetUserID != userID || persistence.proofCreate.InitiatingSessionID != sessionID {
		t.Fatalf("Google reauthentication command = %#v", persistence.proofCreate)
	}
	if _, err := login.StartGitHubLink(context.Background(), userID, sessionID); err != nil {
		t.Fatalf("start GitHub link: %v", err)
	}
	if persistence.proofCreate.Purpose != LinkPurpose || persistence.proofCreate.Provider != GitHubLoginProvider ||
		persistence.proofCreate.TargetUserID != userID || persistence.proofCreate.InitiatingSessionID != sessionID {
		t.Fatalf("GitHub link command = %#v", persistence.proofCreate)
	}
}

func TestExternalLoginTwoTabsKeepSecondContinuationAndRejectDisplacedFirst(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{}
	google := &recordingGoogleLogin{}
	login, err := NewExternalLogin(persistence, google, &recordingGitHubLogin{}, credentials)
	if err != nil {
		t.Fatal(err)
	}
	firstID := "11111111-1111-4111-8111-111111111111"
	secondID := "22222222-2222-4222-8222-222222222222"
	first, _ := login.StartGoogle(context.Background(), firstID, "198.51.100.10")
	firstState := queryParameter(t, first.AuthorizationURL, "state")
	second, _ := login.StartGoogle(context.Background(), secondID, "198.51.100.10")
	secondState := queryParameter(t, second.AuthorizationURL, "state")
	if _, err := login.CompleteGoogle(context.Background(), ExternalLoginCallback{
		State:             firstState,
		BrowserCredential: second.BrowserCredential,
		Code:              "first-code",
		ExactResponse:     "code=first-code&state=" + firstState,
		Outcome:           ExternalCallbackCode,
	}); !errors.Is(err, ErrExternalLoginInvalid) {
		t.Fatalf("displaced first callback = %v", err)
	}
	completed, err := login.CompleteGoogle(context.Background(), ExternalLoginCallback{
		State:             secondState,
		BrowserCredential: second.BrowserCredential,
		Code:              "second-code",
		ExactResponse:     "code=second-code&state=" + secondState,
		Outcome:           ExternalCallbackCode,
	})
	if err != nil || completed.InvitationID != secondID || google.calls != 1 {
		t.Fatalf("second continuation = %#v, calls %d, %v", completed, google.calls, err)
	}
}

func TestExternalLoginRejectsWrongBrowserBindingBeforeProvider(t *testing.T) {
	t.Parallel()
	credentials := externalLoginTestCredentials(t)
	persistence := &externalLoginMemoryPersistence{}
	google := &recordingGoogleLogin{}
	login, err := NewExternalLogin(persistence, google, &recordingGitHubLogin{}, credentials)
	if err != nil {
		t.Fatalf("compose external login: %v", err)
	}
	start, err := login.StartGoogle(context.Background(), "", "198.51.100.10")
	if err != nil {
		t.Fatalf("start Google login: %v", err)
	}
	state := queryParameter(t, start.AuthorizationURL, "state")
	_, err = login.CompleteGoogle(context.Background(), ExternalLoginCallback{
		State: state, BrowserCredential: strings.Replace(start.BrowserCredential, "browser", "state", 1),
		Code: "code", ExactResponse: "state=" + state + "&code=code", Outcome: ExternalCallbackCode,
	})
	if !errors.Is(err, ErrExternalLoginInvalid) || google.calls != 0 || persistence.claimed {
		t.Fatalf("wrong-browser outcome = %v, provider calls = %d, claimed = %t", err, google.calls, persistence.claimed)
	}
}

type externalLoginMemoryPersistence struct {
	loginCreate             CreateExternalLoginCommand
	proofCreate             CreateExternalIdentityProofCommand
	createdPurpose          ProofPurpose
	createdInvitationID     string
	transactionID           string
	provider                ExternalLoginProvider
	claimed                 bool
	claimCalls              int
	replay                  bool
	unknown                 bool
	rejected                bool
	googleCompleted         bool
	completionErr           error
	commitOnCompletionError bool
	session                 BrowserSession
}

func (p *externalLoginMemoryPersistence) CreateExternalLogin(
	_ context.Context,
	command CreateExternalLoginCommand,
) (time.Time, error) {
	p.loginCreate = command
	p.createdPurpose = LoginPurpose
	p.createdInvitationID = command.InvitationID
	p.transactionID = command.TransactionID
	p.provider = command.Provider
	return time.Now().Add(30 * time.Minute), nil
}

func (p *externalLoginMemoryPersistence) CreateExternalIdentityProof(
	_ context.Context,
	command CreateExternalIdentityProofCommand,
) (time.Time, error) {
	p.proofCreate = command
	p.createdPurpose = command.Purpose
	p.createdInvitationID = ""
	p.transactionID = command.TransactionID
	p.provider = command.Provider
	return time.Now().Add(30 * time.Minute), nil
}

func (p *externalLoginMemoryPersistence) ClaimExternalLogin(
	_ context.Context,
	command ClaimExternalLoginCommand,
) (ExternalLoginClaim, error) {
	p.claimed = true
	p.claimCalls++
	if command.TransactionID != p.transactionID || command.Provider != p.provider {
		return ExternalLoginClaim{}, ErrExternalLoginInvalid
	}
	claim := ExternalLoginClaim{
		Purpose:      p.createdPurpose,
		InvitationID: p.createdInvitationID,
	}
	if command.Outcome == ExternalCallbackDenied {
		return claim, ErrExternalLoginDenied
	}
	if command.Outcome == ExternalCallbackUnavailable {
		p.unknown = true
		return claim, ErrExternalLoginUnavailable
	}
	if p.replay {
		claim.IsReplay = true
		claim.Session = p.session
		return claim, nil
	}
	return claim, nil
}

func (p *externalLoginMemoryPersistence) CompleteGoogleLogin(
	_ context.Context,
	command CompleteGoogleLoginCommand,
) (BrowserSession, error) {
	p.googleCompleted = true
	p.session = BrowserSession{SessionID: command.SessionID, UserID: "google-user", ExpiresAt: time.Now().Add(time.Hour)}
	if p.completionErr != nil {
		p.replay = p.commitOnCompletionError
		return BrowserSession{}, p.completionErr
	}
	return p.session, nil
}

func (p *externalLoginMemoryPersistence) CompleteGitHubLogin(
	_ context.Context,
	command CompleteGitHubLoginCommand,
) (BrowserSession, error) {
	p.session = BrowserSession{SessionID: command.SessionID, UserID: "github-user", ExpiresAt: time.Now().Add(time.Hour)}
	if p.completionErr != nil {
		p.replay = p.commitOnCompletionError
		return BrowserSession{}, p.completionErr
	}
	return p.session, nil
}

func (p *externalLoginMemoryPersistence) RejectExternalLogin(
	_ context.Context,
	_ MarkExternalLoginUnknownCommand,
) error {
	p.rejected = true
	return nil
}

func (p *externalLoginMemoryPersistence) MarkExternalLoginUnknown(
	_ context.Context,
	_ MarkExternalLoginUnknownCommand,
) error {
	p.unknown = true
	return nil
}

type recordingGoogleLogin struct {
	calls int
	err   error
}

func (*recordingGoogleLogin) AuthorizationURL(state string, nonce string, challenge string) string {
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state + "&nonce=" + nonce + "&code_challenge=" + challenge
}

func (client *recordingGoogleLogin) Authenticate(context.Context, string, string, string) (GoogleIdentityProof, error) {
	client.calls++
	if client.err != nil {
		return GoogleIdentityProof{}, client.err
	}
	return GoogleIdentityProof{Issuer: "https://accounts.google.com", Subject: "google-subject"}, nil
}

type recordingGitHubLogin struct {
	calls int
}

func (*recordingGitHubLogin) AuthorizationURL(state string, challenge string) string {
	return "https://github.com/login/oauth/authorize?state=" + state + "&code_challenge=" + challenge
}

func (client *recordingGitHubLogin) Authenticate(context.Context, string, string) (GitHubIdentityProof, error) {
	client.calls++
	return GitHubIdentityProof{UserID: 42}, nil
}

func externalLoginTestCredentials(t *testing.T) Credentials {
	t.Helper()
	credentials, err := NewCredentials(bytes.Repeat([]byte{9}, IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}
	return credentials
}

func queryParameter(t *testing.T, rawURL string, name string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	value := parsed.Query().Get(name)
	if value == "" {
		t.Fatalf("authorization URL has no %s", name)
	}
	return value
}

func mustDecodeRawURL(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode raw URL value: %v", err)
	}
	return decoded
}
