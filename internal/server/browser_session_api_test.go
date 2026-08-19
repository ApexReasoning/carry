package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/identity"
)

func TestBrowserSessionExchangeSetsHostOnlyOpaqueCookie(t *testing.T) {
	t.Parallel()

	sessions := &recordingBrowserSessions{created: identity.BrowserSession{
		Secret: "opaque-session-secret", UserID: "member-5", ExpiresAt: time.Now().Add(time.Hour),
	}}
	handler := browserTestAPI(t, sessions)
	request := httptest.NewRequest(http.MethodPost, "/v1/browser/sessions", nil)
	request.Header.Set("Authorization", "Bearer member-token")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if sessions.sourceToken != "member-token" {
		t.Fatalf("source token = %q", sessions.sourceToken)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != browserSessionCookie || cookie.Value != "opaque-session-secret" ||
		!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("browser session cookie = %#v", cookie)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestCrossSiteBrowserSessionExchangeIsRejectedBeforeStore(t *testing.T) {
	t.Parallel()

	sessions := &recordingBrowserSessions{}
	handler := browserTestAPI(t, sessions)
	request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/browser/sessions", nil)
	request.Host = "carry.example"
	request.Header.Set("Authorization", "Bearer member-token")
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if sessions.sourceToken != "" {
		t.Fatal("cross-site request reached browser session store")
	}
}

func TestBrowserSessionAuthenticatesMemberRoute(t *testing.T) {
	t.Parallel()

	sessions := &recordingBrowserSessions{
		authenticatedSecret: "opaque-session-secret",
		user:                identity.AuthenticatedUser{UserID: "member-5"},
	}
	handler := browserTestAPI(t, sessions)
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "opaque-session-secret"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"user_id":"member-5"`) {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

func TestMemberSurfaceRejectsMachineCertificateWithMemberCredentials(t *testing.T) {
	t.Parallel()

	authority, certificate := testMachineCertificate(t, "machine-18")
	for _, testCase := range []struct {
		name    string
		method  string
		path    string
		add     func(*http.Request)
		reached func(*recordingUserTokens, *recordingBrowserSessions) bool
	}{
		{
			name: "member bearer", method: http.MethodGet, path: "/v1/me",
			add: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer member-token")
			},
			reached: func(tokens *recordingUserTokens, _ *recordingBrowserSessions) bool {
				return tokens.authenticatedToken != ""
			},
		},
		{
			name: "browser session exchange", method: http.MethodPost, path: "/v1/browser/sessions",
			add: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer member-token")
			},
			reached: func(_ *recordingUserTokens, sessions *recordingBrowserSessions) bool {
				return sessions.sourceToken != ""
			},
		},
		{
			name: "browser session revocation", method: http.MethodDelete, path: "/v1/browser/sessions/current",
			add: func(request *http.Request) {
				request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "opaque-session-secret"})
			},
			reached: func(_ *recordingUserTokens, sessions *recordingBrowserSessions) bool {
				return sessions.revokedSecret != ""
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens := &recordingUserTokens{user: identity.AuthenticatedUser{UserID: "member-18"}}
			sessions := &recordingBrowserSessions{created: identity.BrowserSession{Secret: "opaque-session-secret"}}
			handler := memberSurfaceTestAPI(t, authority, tokens, sessions)
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			testCase.add(request)
			request.TLS = &tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{certificate},
				VerifiedChains:   [][]*x509.Certificate{{certificate}},
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if testCase.reached(tokens, sessions) {
				t.Fatal("mixed Machine/member principal reached member authority store")
			}
		})
	}
}

func TestMachineRouteRejectsMemberCredentialsEvenWithValidCertificate(t *testing.T) {
	t.Parallel()

	authority, certificate := testMachineCertificate(t, "machine-11")
	tests := []struct {
		name      string
		addMember func(*http.Request)
	}{
		{
			name: "bearer",
			addMember: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer member-token")
			},
		},
		{
			name: "browser session",
			addMember: func(request *http.Request) {
				request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "opaque-session-secret"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runStore := &recordingMachineRuns{}
			member, err := NewMemberRoutes(
				&recordingUserTokens{}, unavailableBrowserSessions{}, emptyMemberships{},
				&recordingMachineEnrollments{}, unavailableWorkCommands{}, unavailableWorkQueries{}, authority,
			)
			if err != nil {
				t.Fatalf("compose member routes: %v", err)
			}
			machine, err := NewMachineRoutes(runStore)
			if err != nil {
				t.Fatalf("compose Machine routes: %v", err)
			}
			handler := mustAPI(t, member, machine)
			request := httptest.NewRequest(http.MethodPost, "/v1/host/runs/claim", strings.NewReader(`{}`))
			test.addMember(request)
			request.TLS = &tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{certificate},
				VerifiedChains:   [][]*x509.Certificate{{certificate}},
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if runStore.claimMachineID != "" {
				t.Fatal("member credential reached Machine Run store")
			}
		})
	}
}

func memberSurfaceTestAPI(
	t *testing.T,
	authority *host.CertificateAuthority,
	tokens UserTokenAuthenticator,
	sessions BrowserSessionStore,
) http.Handler {
	t.Helper()
	member, err := NewMemberRoutes(
		tokens, sessions, emptyMemberships{}, &recordingMachineEnrollments{},
		unavailableWorkCommands{}, unavailableWorkQueries{}, authority,
	)
	if err != nil {
		t.Fatalf("compose member routes: %v", err)
	}
	runStore := &recordingMachineRuns{}
	machine, err := NewMachineRoutes(runStore)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

func browserTestAPI(t *testing.T, sessions BrowserSessionStore) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	member, err := NewMemberRoutes(
		&recordingUserTokens{}, sessions, emptyMemberships{}, &recordingMachineEnrollments{},
		unavailableWorkCommands{}, unavailableWorkQueries{}, authority,
	)
	if err != nil {
		t.Fatalf("compose member routes: %v", err)
	}
	runStore := &recordingMachineRuns{}
	machine, err := NewMachineRoutes(runStore)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

type recordingBrowserSessions struct {
	created             identity.BrowserSession
	sourceToken         string
	authenticatedSecret string
	revokedSecret       string
	user                identity.AuthenticatedUser
}

func (s *recordingBrowserSessions) CreateBrowserSession(
	_ context.Context,
	token string,
	_ time.Time,
) (identity.BrowserSession, error) {
	s.sourceToken = token
	return s.created, nil
}

func (s *recordingBrowserSessions) AuthenticateBrowserSession(
	_ context.Context,
	secret string,
) (identity.AuthenticatedUser, error) {
	s.authenticatedSecret = secret
	if s.user.UserID == "" {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	return s.user, nil
}

func (s *recordingBrowserSessions) RevokeBrowserSession(_ context.Context, secret string) error {
	s.revokedSecret = secret
	return nil
}

var _ MachineRunStore = (*recordingMachineRuns)(nil)
