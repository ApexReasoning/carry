package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
)

const testSessionID = "55555555-5555-4555-8555-555555555555"

func TestBrowserTokenExchangeWasRemoved(t *testing.T) {
	t.Parallel()
	handler := browserTestAPI(t, &recordingBrowserSessions{})
	request := httptest.NewRequest(http.MethodPost, "/v1/browser/sessions", nil)
	request.Header.Set("Authorization", "Bearer member-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestBrowserSessionRevocationKeepsCookieWhenDurableRevokeFails(t *testing.T) {
	t.Parallel()
	sessions := &recordingBrowserSessions{revokeErr: errors.New("database unavailable")}
	handler := browserTestAPI(t, sessions)
	credential, err := testIdentityCredentials(t).BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatalf("create session credential: %v", err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/v1/browser/sessions/current", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("cookies after failed revoke = %#v", cookies)
	}
	if sessions.revokedSessionID != testSessionID {
		t.Fatalf("revoked session = %q", sessions.revokedSessionID)
	}
}

func TestBrowserSessionRevocationClearsCookieAfterDurableRevoke(t *testing.T) {
	t.Parallel()
	sessions := &recordingBrowserSessions{}
	handler := browserTestAPI(t, sessions)
	credential, err := testIdentityCredentials(t).BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatalf("create session credential: %v", err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/v1/browser/sessions/current", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	cookies := response.Result().Cookies()
	if response.Code != http.StatusNoContent || len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("status = %d, cookies = %#v", response.Code, cookies)
	}
}

func TestTamperedBrowserSessionCookieIsRejectedBeforeStore(t *testing.T) {
	t.Parallel()
	sessions := &recordingBrowserSessions{user: identity.AuthenticatedUser{UserID: "user-5"}}
	handler := browserTestAPI(t, sessions)
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: testSessionID + ".tampered"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || sessions.authenticatedSessionID != "" {
		t.Fatalf("status = %d, session reached store = %q", response.Code, sessions.authenticatedSessionID)
	}
}

func TestBrowserSessionAuthenticatesUserWithoutMembership(t *testing.T) {
	t.Parallel()
	sessions := &recordingBrowserSessions{
		authenticatedSessionID: testSessionID,
		user:                   identity.AuthenticatedUser{UserID: "user-5"},
	}
	handler := browserTestAPI(t, sessions)
	credential, err := testIdentityCredentials(t).BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatalf("create session credential: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"user_id":"user-5"`) ||
		!strings.Contains(response.Body.String(), `"display_name":null`) {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

func TestUserSurfaceRejectsMachineCertificateBeforeCredentials(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, "machine-18")
	tokens := &recordingUserTokens{user: identity.AuthenticatedUser{UserID: "user-18"}}
	sessions := &recordingBrowserSessions{}
	handler := memberSurfaceTestAPI(t, authority, tokens, sessions)
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.Header.Set("Authorization", "Bearer member-token")
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}},
	}
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || tokens.authenticatedToken != "" {
		t.Fatalf("status = %d, token reached store = %q", response.Code, tokens.authenticatedToken)
	}
}

func TestMachineRouteRejectsUserCredentialsEvenWithValidCertificate(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, "machine-11")
	for _, test := range []struct {
		name string
		add  func(*http.Request)
	}{
		{name: "bearer", add: func(request *http.Request) { request.Header.Set("Authorization", "Bearer member-token") }},
		{name: "browser session", add: func(request *http.Request) {
			request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "credential"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runStore := &recordingMachineRuns{}
			member := testUserRoutes(t, authority)
			machine, err := NewMachineRoutes(runStore, unavailableMachineConversations{})
			if err != nil {
				t.Fatalf("compose Machine routes: %v", err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/host/runs/claim", strings.NewReader(`{}`))
			test.add(request)
			request.TLS = &tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}},
			}
			response := httptest.NewRecorder()
			mustAPI(t, member, machine).ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || runStore.claimMachineID != "" {
				t.Fatalf("status = %d, Machine store reached by %q", response.Code, runStore.claimMachineID)
			}
		})
	}
}

func memberSurfaceTestAPI(
	t *testing.T,
	authority *machine.CertificateAuthority,
	tokens UserTokenAuthenticator,
	sessions BrowserSessions,
) http.Handler {
	t.Helper()
	credentials := testIdentityCredentials(t)
	emailLogin, err := identity.NewEmailLogin(unavailableEmailLogins{}, unavailableEmailSender{}, credentials)
	if err != nil {
		t.Fatalf("compose test email login: %v", err)
	}
	member := testUserRoutes(t, authority)
	authentication, err := NewUserAuthentication(tokens, sessions, credentials)
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	identityRoutes, err := NewUserIdentityRoutes(
		emailLogin,
		unavailableExternalLogin{},
		sessions,
		credentials,
		testExternalOrigin(t),
		NewRequestSource(nil),
		emptyMemberships{},
	)
	if err != nil {
		t.Fatalf("compose User identity routes: %v", err)
	}
	member.authentication = authentication
	member.identity = identityRoutes
	machine, err := NewMachineRoutes(&recordingMachineRuns{}, unavailableMachineConversations{})
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

func browserTestAPI(t *testing.T, sessions BrowserSessions) http.Handler {
	t.Helper()
	return memberSurfaceTestAPI(t, testAuthority(t), &recordingUserTokens{}, sessions)
}

type recordingBrowserSessions struct {
	authenticatedSessionID string
	revokedSessionID       string
	revokeErr              error
	user                   identity.AuthenticatedUser
}

func (s *recordingBrowserSessions) AuthenticateBrowserSession(
	_ context.Context,
	sessionID string,
) (identity.AuthenticatedUser, error) {
	s.authenticatedSessionID = sessionID
	if s.user.UserID == "" {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	return s.user, nil
}

func (s *recordingBrowserSessions) RevokeBrowserSession(_ context.Context, sessionID string) error {
	s.revokedSessionID = sessionID
	return s.revokeErr
}
