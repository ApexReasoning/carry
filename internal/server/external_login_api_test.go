package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
)

func TestParseExternalOriginRequiresCanonicalHTTPSAuthority(t *testing.T) {
	t.Parallel()
	valid, err := ParseExternalOrigin("https://carry.example:8443")
	if err != nil || valid.CallbackURL(identity.GoogleLoginProvider) != "https://carry.example:8443/v1/auth/google/callback" {
		t.Fatalf("valid external origin = %#v, %v", valid, err)
	}
	for _, invalid := range []string{
		"", "http://carry.example", "https://carry.example/", "https://user@carry.example",
		"https://carry.example/path", "https://carry.example?query=1", "https://carry.example#fragment",
		"https://CARRY.example", " https://carry.example",
	} {
		if _, err := ParseExternalOrigin(invalid); err == nil {
			t.Errorf("invalid external origin %q was accepted", invalid)
		}
	}
}

func TestExternalLoginStartUnexpectedSessionReadUsesReadRecovery(t *testing.T) {
	t.Parallel()
	credentials := testIdentityCredentials(t)
	credential, err := credentials.BrowserSessionCredential("10000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	api := externalLoginAPI{
		login:       unavailableExternalLogin{},
		sessions:    failingExternalBrowserSessions{},
		credentials: credentials,
		origin:      testExternalOrigin(t),
	}
	request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/google/start", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	response := httptest.NewRecorder()

	api.startGoogle(response, request)

	assertUserFacingResponse(t, response, http.StatusInternalServerError, "Carry could not load this right now. Reload to try again.")
}

type failingExternalBrowserSessions struct{}

func (failingExternalBrowserSessions) AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error) {
	return identity.AuthenticatedUser{}, errors.New("database unavailable")
}

func (failingExternalBrowserSessions) RevokeBrowserSession(context.Context, string) error {
	return nil
}

func TestExternalLoginStartUsesCanonicalHostAndLaxBrowserBinding(t *testing.T) {
	t.Parallel()
	login := &recordingExternalLogin{start: identity.ExternalLoginStart{
		AuthorizationURL:  "https://accounts.google.com/o/oauth2/v2/auth?state=opaque",
		BrowserCredential: "browser-binding",
		ExpiresAt:         time.Now().Add(time.Minute),
	}}
	handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
	request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/google/start", nil)
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	request.Header.Set("X-Forwarded-Proto", "http")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != login.start.AuthorizationURL {
		t.Fatalf("status = %d, location = %q, body = %s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != externalLoginCookie || cookies[0].Value != "browser-binding" ||
		!cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatalf("external login cookie = %#v", cookies)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	if login.source != "192.0.2.1" {
		t.Fatalf("external login source = %q", login.source)
	}
}

func TestExternalLoginStartBoundsFormAndMapsAdmissionLimit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "unknown field", body: "unexpected=value", contentType: "application/x-www-form-urlencoded"},
		{name: "duplicate field", body: "invitation_id=&invitation_id=", contentType: "application/x-www-form-urlencoded"},
		{name: "oversized command", body: "invitation_id=" + strings.Repeat("x", maxCommandBytes), contentType: "application/x-www-form-urlencoded"},
		{name: "JSON body", body: `{"invitation_id":"40000000-0000-4000-8000-000000000001"}`, contentType: "application/json"},
		{name: "oversized non-form body", body: strings.Repeat("x", maxCommandBytes+1), contentType: "application/octet-stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			login := &recordingExternalLogin{}
			handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
			request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/google/start", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest || login.started != "" {
				t.Fatalf("status = %d, started = %q, body = %s", response.Code, login.started, response.Body.String())
			}
		})
	}

	login := &recordingExternalLogin{startErr: identity.ErrExternalLoginSourceAdmissionLimited}
	handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
	request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/github/start", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests ||
		!strings.Contains(response.Body.String(), identity.ErrExternalLoginRateLimited.Error()) ||
		strings.Contains(response.Body.String(), "source admission") {
		t.Fatalf("rate-limited status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestExternalLoginStartAndCallbackPreserveOnlyInvitationContinuation(t *testing.T) {
	t.Parallel()
	invitationID := "40000000-0000-4000-8000-000000000001"
	login := &recordingExternalLogin{start: identity.ExternalLoginStart{
		AuthorizationURL:  "https://accounts.google.com",
		BrowserCredential: "binding",
		ExpiresAt:         time.Now().Add(time.Minute),
	}}
	handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
	start := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/google/start", strings.NewReader("invitation_id="+invitationID))
	start.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, start)
	if response.Code != http.StatusSeeOther || login.started != "google:"+invitationID {
		t.Fatalf("start = %d, continuation %q", response.Code, login.started)
	}

	login.session = identity.BrowserSession{
		SessionID:    testSessionID,
		UserID:       "provider-user",
		ExpiresAt:    time.Now().Add(time.Hour),
		InvitationID: invitationID,
	}
	callback := httptest.NewRequest(http.MethodGet, "https://carry.example/v1/auth/google/callback?code=code&state=state", nil)
	callback.AddCookie(&http.Cookie{
		Name:  externalLoginCookie,
		Value: "binding",
	})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, callback)
	if response.Header().Get("Location") != "https://carry.example/invitations/"+invitationID {
		t.Fatalf("success location = %q", response.Header().Get("Location"))
	}

	login.completeErr = identity.ExternalProofFailure(identity.LoginPurpose, invitationID, identity.ErrExternalLoginDenied)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, callback)
	if response.Header().Get("Location") != "https://carry.example/invitations/"+invitationID+"?sign_in=cancelled" {
		t.Fatalf("denied location = %q", response.Header().Get("Location"))
	}

	malformed := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/google/start", strings.NewReader("invitation_id=wrong"))
	malformed.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, malformed)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed continuation status = %d", response.Code)
	}
}

func TestExternalLoginStartRejectsWrongHostCrossSiteAndAuthenticatedPrincipals(t *testing.T) {
	t.Parallel()
	credentials := testIdentityCredentials(t)
	validSession, err := credentials.BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatalf("create Browser Session credential: %v", err)
	}
	tests := []struct {
		name      string
		configure func(*http.Request)
	}{
		{name: "wrong host", configure: func(request *http.Request) { request.Host = "attacker.example" }},
		{name: "cross site", configure: func(request *http.Request) {
			request.Header.Set("Origin", "https://attacker.example")
			request.Header.Set("Sec-Fetch-Site", "cross-site")
		}},
		{name: "bearer", configure: func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+testCLIBearer(t)) }},
		{name: "Browser Session", configure: func(request *http.Request) {
			request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: validSession})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			login := &recordingExternalLogin{start: identity.ExternalLoginStart{
				AuthorizationURL: "https://accounts.google.com", BrowserCredential: "binding", ExpiresAt: time.Now().Add(time.Minute),
			}}
			sessions := &recordingBrowserSessions{user: identity.AuthenticatedUser{UserID: "active-user"}}
			handler := externalLoginTestAPI(t, login, sessions)
			request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/google/start", nil)
			test.configure(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code < 400 || response.Code >= 500 || login.started != "" {
				t.Fatalf("status = %d, started = %q", response.Code, login.started)
			}
		})
	}
}

func TestExternalLoginCallbackRejectsAmbiguityBeforeIdentity(t *testing.T) {
	t.Parallel()
	for _, rawQuery := range []string{
		"code=one&code=two&state=state",
		"code=one&error=access_denied&state=state",
		"code=one",
		"error=access_denied&state=state&state=other",
	} {
		login := &recordingExternalLogin{}
		handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
		request := httptest.NewRequest(http.MethodGet, "https://carry.example/v1/auth/google/callback?"+rawQuery, nil)
		request.AddCookie(&http.Cookie{Name: externalLoginCookie, Value: "browser-binding"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://carry.example/?sign_in=invalid" || login.completed {
			t.Fatalf("query %q: status = %d, location = %q, completed = %t", rawQuery, response.Code, response.Header().Get("Location"), login.completed)
		}
	}
}

func TestExternalLoginCallbackSetsOneCarrySessionAndClearsTemporaryCookie(t *testing.T) {
	t.Parallel()
	login := &recordingExternalLogin{session: identity.BrowserSession{
		SessionID: testSessionID, UserID: "provider-user", ExpiresAt: time.Now().Add(time.Hour),
	}}
	handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
	request := httptest.NewRequest(
		http.MethodGet,
		"https://carry.example/v1/auth/github/callback?code=provider-code&state=opaque-state",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: externalLoginCookie, Value: "browser-binding"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://carry.example/" {
		t.Fatalf("status = %d, location = %q", response.Code, response.Header().Get("Location"))
	}
	if !login.completed || login.callback.Code != "provider-code" || login.callback.State != "opaque-state" ||
		login.callback.BrowserCredential != "browser-binding" || login.callback.Outcome != identity.ExternalCallbackCode ||
		login.callback.ExactResponse != "code=provider-code&state=opaque-state" {
		t.Fatalf("callback = %#v", login.callback)
	}
	var temporary, session *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		switch cookie.Name {
		case externalLoginCookie:
			temporary = cookie
		case browserSessionCookie:
			session = cookie
		}
	}
	if temporary == nil || temporary.MaxAge != -1 || temporary.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expired temporary cookie = %#v", temporary)
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("Carry Browser Session cookie = %#v", session)
	}
}

func TestExternalLoginDenialIsBoundAndRedirectsWithoutAuthority(t *testing.T) {
	t.Parallel()
	login := &recordingExternalLogin{completeErr: identity.ErrExternalLoginDenied}
	handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
	request := httptest.NewRequest(
		http.MethodGet,
		"https://carry.example/v1/auth/google/callback?error=access_denied&error_description=secret-provider-text&state=opaque",
		nil,
	)
	request.AddCookie(&http.Cookie{Name: externalLoginCookie, Value: "binding"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://carry.example/?sign_in=cancelled" ||
		strings.Contains(response.Body.String(), "secret-provider-text") || login.callback.Outcome != identity.ExternalCallbackDenied {
		t.Fatalf("status = %d, location = %q, callback = %#v, body = %s", response.Code, response.Header().Get("Location"), login.callback, response.Body.String())
	}
}

func TestExternalMethodCallbackFailuresReturnToSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		purpose  identity.ProofPurpose
		cause    error
		expected string
	}{
		{name: "link rejected", purpose: identity.LinkPurpose, cause: identity.ErrExternalLoginRejected, expected: "link_failed"},
		{name: "link denied", purpose: identity.LinkPurpose, cause: identity.ErrExternalLoginDenied, expected: "link_cancelled"},
		{name: "link unavailable", purpose: identity.LinkPurpose, cause: identity.ErrExternalLoginUnavailable, expected: "link_unavailable"},
		{name: "confirmation rejected", purpose: identity.ReauthenticatePurpose, cause: identity.ErrExternalLoginRejected, expected: "confirmation_failed"},
		{name: "confirmation denied", purpose: identity.ReauthenticatePurpose, cause: identity.ErrExternalLoginDenied, expected: "confirmation_cancelled"},
		{name: "confirmation unavailable", purpose: identity.ReauthenticatePurpose, cause: identity.ErrExternalLoginUnavailable, expected: "confirmation_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			login := &recordingExternalLogin{
				completeErr: identity.ExternalProofFailure(test.purpose, "", test.cause),
			}
			handler := externalLoginTestAPI(t, login, &recordingBrowserSessions{})
			request := httptest.NewRequest(
				http.MethodGet,
				"https://carry.example/v1/auth/google/callback?code=provider-code&state=opaque",
				nil,
			)
			request.AddCookie(&http.Cookie{Name: externalLoginCookie, Value: "binding"})
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			expected := "https://carry.example/?identity_change=" + test.expected
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != expected {
				t.Fatalf("status = %d, location = %q, want %q", response.Code, response.Header().Get("Location"), expected)
			}
		})
	}
}

func externalLoginTestAPI(t *testing.T, external ExternalLogin, sessions BrowserSessions) http.Handler {
	t.Helper()
	credentials := testIdentityCredentials(t)
	emailLogin, err := identity.NewEmailLogin(unavailableEmailLogins{}, unavailableEmailSender{}, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	member := testUserRoutes(t, testAuthority(t))
	authentication, err := NewUserAuthentication(&recordingCLICredentials{}, sessions, credentials, testExternalOrigin(t))
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	identityRoutes, err := NewUserIdentityRoutes(
		emailLogin, external, unavailableIdentityMethods{}, sessions, &recordingCLILogins{}, credentials,
		testExternalOrigin(t), NewRequestSource(nil), emptyMemberships{},
	)
	if err != nil {
		t.Fatalf("compose User identity routes: %v", err)
	}
	member.authentication = authentication
	member.identity = identityRoutes
	machine, err := NewMachineRoutes(&recordingMachineRuns{}, unavailableMachineConversations{}, unavailableMachineConnections{})
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

type recordingExternalLogin struct {
	start       identity.ExternalLoginStart
	startErr    error
	started     string
	source      string
	callback    identity.ExternalLoginCallback
	session     identity.BrowserSession
	completeErr error
	completed   bool
}

func (login *recordingExternalLogin) StartGoogle(_ context.Context, invitationID string, source string) (identity.ExternalLoginStart, error) {
	login.started = "google:" + invitationID
	login.source = source
	return login.start, login.startErr
}

func (login *recordingExternalLogin) StartGitHub(_ context.Context, invitationID string, source string) (identity.ExternalLoginStart, error) {
	login.started = "github:" + invitationID
	login.source = source
	return login.start, login.startErr
}

func (login *recordingExternalLogin) StartGoogleReauthentication(context.Context, string, string) (identity.ExternalLoginStart, error) {
	login.started = "google-reauthenticate"
	return login.start, login.startErr
}

func (login *recordingExternalLogin) StartGitHubReauthentication(context.Context, string, string) (identity.ExternalLoginStart, error) {
	login.started = "github-reauthenticate"
	return login.start, login.startErr
}

func (login *recordingExternalLogin) StartGoogleLink(context.Context, string, string) (identity.ExternalLoginStart, error) {
	login.started = "google-link"
	return login.start, login.startErr
}

func (login *recordingExternalLogin) StartGitHubLink(context.Context, string, string) (identity.ExternalLoginStart, error) {
	login.started = "github-link"
	return login.start, login.startErr
}

func (login *recordingExternalLogin) CompleteGoogle(_ context.Context, callback identity.ExternalLoginCallback) (identity.BrowserSession, error) {
	login.completed = true
	login.callback = callback
	return login.session, login.completeErr
}

func (login *recordingExternalLogin) CompleteGitHub(_ context.Context, callback identity.ExternalLoginCallback) (identity.BrowserSession, error) {
	login.completed = true
	login.callback = callback
	return login.session, login.completeErr
}
