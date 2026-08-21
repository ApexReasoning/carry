//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	carryserver "github.com/ApexReasoning/carry/internal/server"
	"github.com/ApexReasoning/carry/internal/space"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExternalLoginBrowserJourneyWithConcreteProviders(t *testing.T) {
	ctx := context.Background()
	pool := openExternalLoginTestPool(t, ctx)
	store := carrypostgres.NewStore(pool)
	credentials, err := identity.NewCredentials(bytes.Repeat([]byte{7}, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}

	googleFixture := newGoogleProviderFixture(t)
	defer googleFixture.Close()
	githubFixture := newGitHubProviderFixture(t)
	defer githubFixture.Close()

	carry := httptest.NewUnstartedServer(nil)
	origin, err := carryserver.ParseExternalOrigin("https://" + carry.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse test Carry origin: %v", err)
	}
	google, err := newGoogleLoginAt(
		"google-client", "google-secret", origin.CallbackURL(identity.GoogleLoginProvider),
		googleEndpoints{
			authorization: googleFixture.URL + "/authorize",
			token:         googleFixture.URL + "/token",
			jwks:          googleFixture.URL + "/jwks",
		},
	)
	if err != nil {
		t.Fatalf("configure concrete Google client: %v", err)
	}
	github, err := newGitHubLoginAt(
		"github-client", "github-secret", origin.CallbackURL(identity.GitHubLoginProvider),
		githubEndpoints{
			authorization: githubFixture.URL + "/authorize",
			token:         githubFixture.URL + "/token",
			user:          githubFixture.URL + "/user",
		},
	)
	if err != nil {
		t.Fatalf("configure concrete GitHub client: %v", err)
	}
	responseLoss := newCommitResponseLossStore(store)
	handler := composeExternalLoginTestAPI(t, pool, store, responseLoss, credentials, origin, google, github)
	carry.Config.Handler = handler
	carry.StartTLS()
	defer carry.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create Browser cookie jar: %v", err)
	}
	browser := carry.Client()
	browser.Jar = jar
	browser.CheckRedirect = noRedirect

	googleJourney := browserProviderJourney{
		name: "Google", provider: identity.GoogleLoginProvider,
		startPath: "/v1/auth/google/start", callbackPath: "/v1/auth/google/callback",
		prepare: func(authorization *url.URL) {
			googleFixture.Expect(authorization.Query().Get("nonce"), authorization.Query().Get("code_challenge"))
		},
		providerCalls: func() int64 { return googleFixture.tokenCalls.Load() },
	}
	githubJourney := browserProviderJourney{
		name: "GitHub", provider: identity.GitHubLoginProvider,
		startPath: "/v1/auth/github/start", callbackPath: "/v1/auth/github/callback",
		prepare: func(authorization *url.URL) {
			githubFixture.Expect(authorization.Query().Get("code_challenge"))
		},
		providerCalls: func() int64 { return githubFixture.tokenCalls.Load() + githubFixture.userCalls.Load() },
	}

	googleUser := exerciseProviderBrowserJourney(t, browser, carry.URL, googleJourney, "Google Space")
	githubUser := exerciseProviderBrowserJourney(t, browser, carry.URL, githubJourney, "GitHub Space")
	if googleUser == githubUser {
		t.Fatalf("Google and GitHub unexpectedly selected the same User %q", googleUser)
	}
	if googleFixture.tokenCalls.Load() != 2 {
		t.Fatalf("Google token calls after first/repeat/replay = %d", googleFixture.tokenCalls.Load())
	}
	if githubFixture.tokenCalls.Load() != 2 || githubFixture.userCalls.Load() != 2 {
		t.Fatalf("GitHub calls after first/repeat/replay = token %d, User %d", githubFixture.tokenCalls.Load(), githubFixture.userCalls.Load())
	}
	lostSessions := responseLoss.LostSessions()
	if len(lostSessions) != 2 {
		t.Fatalf("committed response-loss sessions = %#v", lostSessions)
	}
	for provider, session := range lostSessions {
		var status, sessionID string
		if err := pool.QueryRow(ctx, `
			select status, browser_session_id::text
			from external_login_transactions
			where provider = $1 and browser_session_id = $2
		`, provider.String(), session.SessionID).Scan(&status, &sessionID); err != nil {
			t.Fatalf("load reconciled %s completion: %v", provider, err)
		}
		if status != "succeeded" || sessionID != session.SessionID {
			t.Fatalf("reconciled %s status = %q, session = %q/%q", provider, status, sessionID, session.SessionID)
		}
	}

	t.Run("denial and invalid bindings never reach provider", func(t *testing.T) {
		before := googleFixture.tokenCalls.Load()
		started := startBrowserLogin(t, browser, carry.URL, googleJourney)
		deniedURL := carry.URL + googleJourney.callbackPath + "?error=access_denied&state=" + url.QueryEscape(started.state)
		response := browserRequest(t, browser, http.MethodGet, deniedURL, "", nil)
		defer response.Body.Close()
		if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != carry.URL+"/?sign_in=cancelled" {
			t.Fatalf("denial status = %d, location = %q", response.StatusCode, response.Header.Get("Location"))
		}

		started = startBrowserLogin(t, browser, carry.URL, googleJourney)
		invalidStateURL := carry.URL + googleJourney.callbackPath + "?code=unused&state=" + url.QueryEscape(started.state+"x")
		response = browserRequest(t, browser, http.MethodGet, invalidStateURL, "", nil)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != carry.URL+"/?sign_in=invalid" {
			t.Fatalf("invalid state status = %d, location = %q", response.StatusCode, response.Header.Get("Location"))
		}

		started = startBrowserLogin(t, browser, carry.URL, googleJourney)
		wrongBindingClient := carry.Client()
		wrongBindingClient.CheckRedirect = noRedirect
		callbackURL := carry.URL + googleJourney.callbackPath + "?code=unused&state=" + url.QueryEscape(started.state)
		request, err := http.NewRequest(http.MethodGet, callbackURL, nil)
		if err != nil {
			t.Fatalf("create wrong-binding callback: %v", err)
		}
		request.AddCookie(&http.Cookie{Name: "__Host-carry_oauth", Value: "wrong-browser-binding"})
		response, err = wrongBindingClient.Do(request)
		if err != nil {
			t.Fatalf("send wrong-binding callback: %v", err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != carry.URL+"/?sign_in=invalid" {
			t.Fatalf("wrong binding status = %d, location = %q", response.StatusCode, response.Header.Get("Location"))
		}
		if googleFixture.tokenCalls.Load() != before {
			t.Fatalf("provider calls changed from %d to %d", before, googleFixture.tokenCalls.Load())
		}
	})

	t.Run("provider outage becomes Unknown and a fresh start succeeds", func(t *testing.T) {
		started := startBrowserLogin(t, browser, carry.URL, githubJourney)
		transactionID, ok := credentials.ParseExternalLoginState(started.state, identity.GitHubLoginProvider)
		if !ok {
			t.Fatal("parse GitHub transaction state")
		}
		githubFixture.outage.Store(true)
		response, _ := completeBrowserLogin(t, browser, carry.URL, githubJourney, started, "outage")
		githubFixture.outage.Store(false)
		if response.Header.Get("Location") != carry.URL+"/?sign_in=unavailable" {
			t.Fatalf("outage redirect = %q", response.Header.Get("Location"))
		}
		var status string
		if err := pool.QueryRow(ctx, `select status from external_login_transactions where transaction_id = $1`, transactionID).Scan(&status); err != nil {
			t.Fatalf("load outage transaction: %v", err)
		}
		if status != "unknown" {
			t.Fatalf("outage transaction status = %q", status)
		}

		fresh := startBrowserLogin(t, browser, carry.URL, githubJourney)
		response, _ = completeBrowserLogin(t, browser, carry.URL, githubJourney, fresh, "fresh-code")
		if response.Header.Get("Location") != carry.URL+"/" {
			t.Fatalf("fresh login redirect = %q", response.Header.Get("Location"))
		}
		logoutBrowser(t, browser, carry.URL)
	})
	if githubFixture.tokenCalls.Load() != 4 || githubFixture.userCalls.Load() != 3 {
		t.Fatalf("final GitHub calls = token %d, User %d", githubFixture.tokenCalls.Load(), githubFixture.userCalls.Load())
	}
}

func TestIdentityMethodBrowserJourneyWithConcreteProviders(t *testing.T) {
	ctx := context.Background()
	pool := openExternalLoginTestPool(t, ctx)
	store := carrypostgres.NewStore(pool)
	credentials, err := identity.NewCredentials(bytes.Repeat([]byte{8}, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}
	googleFixture := newGoogleProviderFixture(t)
	defer googleFixture.Close()
	githubFixture := newGitHubProviderFixture(t)
	defer githubFixture.Close()
	carry := httptest.NewUnstartedServer(nil)
	origin, err := carryserver.ParseExternalOrigin("https://" + carry.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse test Carry origin: %v", err)
	}
	google, err := newGoogleLoginAt(
		"google-client", "google-secret", origin.CallbackURL(identity.GoogleLoginProvider),
		googleEndpoints{authorization: googleFixture.URL + "/authorize", token: googleFixture.URL + "/token", jwks: googleFixture.URL + "/jwks"},
	)
	if err != nil {
		t.Fatalf("configure concrete Google client: %v", err)
	}
	github, err := newGitHubLoginAt(
		"github-client", "github-secret", origin.CallbackURL(identity.GitHubLoginProvider),
		githubEndpoints{authorization: githubFixture.URL + "/authorize", token: githubFixture.URL + "/token", user: githubFixture.URL + "/user"},
	)
	if err != nil {
		t.Fatalf("configure concrete GitHub client: %v", err)
	}
	carry.Config.Handler = composeExternalLoginTestAPI(t, pool, store, store, credentials, origin, google, github)
	carry.StartTLS()
	defer carry.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create Browser cookie jar: %v", err)
	}
	browser := carry.Client()
	browser.Jar = jar
	browser.CheckRedirect = noRedirect

	userID := "11111111-1111-4111-8111-111111111111"
	initialSessionID := "22222222-2222-4222-8222-222222222222"
	if _, err := pool.Exec(ctx, `insert into carry_users (user_id) values ($1)`, userID); err != nil {
		t.Fatalf("seed Browser User: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into email_identities (canonical_email, user_id) values ('browser-methods@example.com', $1)`, userID); err != nil {
		t.Fatalf("seed Browser email method: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at)
		values ($2, $1, 'email', transaction_timestamp() + interval '30 days')
	`, userID, initialSessionID); err != nil {
		t.Fatalf("seed Browser Session: %v", err)
	}
	initialCredential, _ := credentials.BrowserSessionCredential(initialSessionID)
	carryOrigin, _ := url.Parse(carry.URL)
	jar.SetCookies(carryOrigin, []*http.Cookie{{
		Name: "__Host-carry_session", Value: initialCredential, Path: "/", Secure: true,
	}})
	createFirstSpace(t, browser, carry.URL, "Identity User", "Identity Space")
	assertIdentityMethodsHTTP(t, browser, carry.URL, []string{"email"}, "browser-methods@example.com")
	bearerRequest, err := http.NewRequest(http.MethodGet, carry.URL+"/v1/identity/methods", nil)
	if err != nil {
		t.Fatalf("create bearer Identity Settings request: %v", err)
	}
	bearerRequest.Header.Set("Authorization", "Bearer transitional-member-token")
	bearerResponse, err := browser.Do(bearerRequest)
	if err != nil {
		t.Fatalf("send bearer Identity Settings request: %v", err)
	}
	_ = bearerResponse.Body.Close()
	if bearerResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bearer Identity Settings status = %d", bearerResponse.StatusCode)
	}

	googleLink := browserProviderJourney{
		name: "Google link", provider: identity.GoogleLoginProvider,
		startPath: "/v1/identity/methods/google/start", callbackPath: "/v1/auth/google/callback",
		prepare: func(authorization *url.URL) {
			googleFixture.Expect(authorization.Query().Get("nonce"), authorization.Query().Get("code_challenge"))
		},
		providerCalls: func() int64 { return googleFixture.tokenCalls.Load() },
	}
	oldCredential := currentBrowserCredential(t, browser, carry.URL)
	startedGoogleLink := startBrowserLogin(t, browser, carry.URL, googleLink)
	linkedResponse, googleLinkedSession := completeBrowserLogin(t, browser, carry.URL, googleLink, startedGoogleLink, "link-google")
	if linkedResponse.Header.Get("Location") != carry.URL+"/?identity_change=linked" {
		t.Fatalf("Google link redirect = %q", linkedResponse.Header.Get("Location"))
	}
	callsAfterLink := googleFixture.tokenCalls.Load()
	replayedGoogleLink := replayProviderCallback(t, browser, carry.URL, googleLink, startedGoogleLink, "link-google")
	if replayedGoogleLink.Value != googleLinkedSession.Value || googleFixture.tokenCalls.Load() != callsAfterLink {
		t.Fatalf("Google link replay Session = %q/%q, calls = %d/%d", replayedGoogleLink.Value, googleLinkedSession.Value, googleFixture.tokenCalls.Load(), callsAfterLink)
	}
	assertCredentialStatus(t, carry.Client(), carry.URL, oldCredential, http.StatusUnauthorized)
	if gotUser, spaces := loadCurrentUser(t, browser, carry.URL); gotUser != userID || spaces != 1 {
		t.Fatalf("after Google link User = %s/%s, Spaces = %d", gotUser, userID, spaces)
	}
	assertIdentityMethodsHTTP(t, browser, carry.URL, []string{"email", "google"}, "stable-google-subject")

	googleReauthentication := googleLink
	googleReauthentication.name = "Google reauthentication"
	googleReauthentication.startPath = "/v1/identity/reauthentication/google/start"
	oldCredential = currentBrowserCredential(t, browser, carry.URL)
	startedReauthentication := startBrowserLogin(t, browser, carry.URL, googleReauthentication)
	reauthenticatedResponse, _ := completeBrowserLogin(t, browser, carry.URL, googleReauthentication, startedReauthentication, "reauthenticate-google")
	if reauthenticatedResponse.Header.Get("Location") != carry.URL+"/?identity_change=confirmed" {
		t.Fatalf("Google reauthentication redirect = %q", reauthenticatedResponse.Header.Get("Location"))
	}
	assertCredentialStatus(t, carry.Client(), carry.URL, oldCredential, http.StatusUnauthorized)

	githubLink := browserProviderJourney{
		name: "GitHub link", provider: identity.GitHubLoginProvider,
		startPath: "/v1/identity/methods/github/start", callbackPath: "/v1/auth/github/callback",
		prepare:       func(authorization *url.URL) { githubFixture.Expect(authorization.Query().Get("code_challenge")) },
		providerCalls: func() int64 { return githubFixture.tokenCalls.Load() + githubFixture.userCalls.Load() },
	}
	oldCredential = currentBrowserCredential(t, browser, carry.URL)
	startedGitHubLink := startBrowserLogin(t, browser, carry.URL, githubLink)
	githubResponse, _ := completeBrowserLogin(t, browser, carry.URL, githubLink, startedGitHubLink, "link-github")
	if githubResponse.Header.Get("Location") != carry.URL+"/?identity_change=linked" {
		t.Fatalf("GitHub link redirect = %q", githubResponse.Header.Get("Location"))
	}
	assertCredentialStatus(t, carry.Client(), carry.URL, oldCredential, http.StatusUnauthorized)
	assertIdentityMethodsHTTP(t, browser, carry.URL, []string{"email", "google", "github"}, "424242")

	unlinkInitiatingCredential := currentBrowserCredential(t, browser, carry.URL)
	unlinkResponse := identityMethodRequest(
		t, browser, http.MethodDelete, carry.URL+"/v1/identity/methods/email", carry.URL,
		unlinkInitiatingCredential, "remove-email",
	)
	unlinkReplacement := responseCookie(unlinkResponse, "__Host-carry_session")
	if unlinkResponse.StatusCode != http.StatusNoContent || unlinkReplacement == nil {
		defer unlinkResponse.Body.Close()
		t.Fatalf("unlink email status = %d, cookie = %#v, body = %s", unlinkResponse.StatusCode, unlinkReplacement, readBody(unlinkResponse))
	}
	_ = unlinkResponse.Body.Close()
	replayResponse := identityMethodRequest(
		t, carry.Client(), http.MethodDelete, carry.URL+"/v1/identity/methods/email", carry.URL,
		unlinkInitiatingCredential, "remove-email",
	)
	replayedReplacement := responseCookie(replayResponse, "__Host-carry_session")
	if replayResponse.StatusCode != http.StatusNoContent || replayedReplacement == nil || replayedReplacement.Value != unlinkReplacement.Value {
		defer replayResponse.Body.Close()
		t.Fatalf("unlink replay status = %d, replacement = %#v/%#v, body = %s", replayResponse.StatusCode, replayedReplacement, unlinkReplacement, readBody(replayResponse))
	}
	_ = replayResponse.Body.Close()
	changedReplay := identityMethodRequest(
		t, carry.Client(), http.MethodDelete, carry.URL+"/v1/identity/methods/google", carry.URL,
		unlinkInitiatingCredential, "remove-email",
	)
	if changedReplay.StatusCode != http.StatusConflict {
		defer changedReplay.Body.Close()
		t.Fatalf("changed unlink replay status = %d, body = %s", changedReplay.StatusCode, readBody(changedReplay))
	}
	_ = changedReplay.Body.Close()
	assertIdentityMethodsHTTP(t, browser, carry.URL, []string{"google", "github"}, "browser-methods@example.com")

	replacementID, ok := credentials.ParseBrowserSessionCredential(unlinkReplacement.Value)
	if !ok {
		t.Fatal("parse unlink replacement credential")
	}
	if err := store.RevokeBrowserSession(ctx, replacementID); err != nil {
		t.Fatalf("revoke unlink replacement Session: %v", err)
	}
	inactiveReplay := identityMethodRequest(
		t, carry.Client(), http.MethodDelete, carry.URL+"/v1/identity/methods/email", carry.URL,
		unlinkInitiatingCredential, "remove-email",
	)
	if inactiveReplay.StatusCode != http.StatusUnauthorized {
		defer inactiveReplay.Body.Close()
		t.Fatalf("inactive replacement replay status = %d, body = %s", inactiveReplay.StatusCode, readBody(inactiveReplay))
	}
	_ = inactiveReplay.Body.Close()

	googleLogin := googleLink
	googleLogin.name = "Google login after link"
	googleLogin.startPath = "/v1/auth/google/start"
	startedGoogleLogin := startBrowserLogin(t, browser, carry.URL, googleLogin)
	_, _ = completeBrowserLogin(t, browser, carry.URL, googleLogin, startedGoogleLogin, "login-linked-google")
	if gotUser, spaces := loadCurrentUser(t, browser, carry.URL); gotUser != userID || spaces != 1 {
		t.Fatalf("linked Google login User = %s/%s, Spaces = %d", gotUser, userID, spaces)
	}
	logoutBrowser(t, browser, carry.URL)
	githubLogin := githubLink
	githubLogin.name = "GitHub login after link"
	githubLogin.startPath = "/v1/auth/github/start"
	startedGitHubLogin := startBrowserLogin(t, browser, carry.URL, githubLogin)
	_, _ = completeBrowserLogin(t, browser, carry.URL, githubLogin, startedGitHubLogin, "login-linked-github")
	if gotUser, spaces := loadCurrentUser(t, browser, carry.URL); gotUser != userID || spaces != 1 {
		t.Fatalf("linked GitHub login User = %s/%s, Spaces = %d", gotUser, userID, spaces)
	}
}

func assertIdentityMethodsHTTP(t *testing.T, browser *http.Client, carryURL string, expected []string, forbidden string) {
	t.Helper()
	response := browserRequest(t, browser, http.MethodGet, carryURL+"/v1/identity/methods", "", nil)
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("load Identity methods status = %d, body = %s", response.StatusCode, body)
	}
	if forbidden != "" && strings.Contains(string(body), forbidden) {
		t.Fatalf("Identity methods exposed forbidden metadata %q: %s", forbidden, body)
	}
	var payload struct {
		Methods []string `json:"methods"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode Identity methods: %v", err)
	}
	if fmt.Sprint(payload.Methods) != fmt.Sprint(expected) {
		t.Fatalf("Identity methods = %#v, want %#v", payload.Methods, expected)
	}
}

func currentBrowserCredential(t *testing.T, browser *http.Client, carryURL string) string {
	t.Helper()
	parsed, _ := url.Parse(carryURL)
	for _, cookie := range browser.Jar.Cookies(parsed) {
		if cookie.Name == "__Host-carry_session" {
			return cookie.Value
		}
	}
	t.Fatal("Browser has no Carry Session credential")
	return ""
}

func replayProviderCallback(
	t *testing.T,
	browser *http.Client,
	carryURL string,
	journey browserProviderJourney,
	started startedBrowserLogin,
	code string,
) *http.Cookie {
	t.Helper()
	callbackURL := carryURL + journey.callbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(started.state)
	request, err := http.NewRequest(http.MethodGet, callbackURL, nil)
	if err != nil {
		t.Fatalf("create %s replay: %v", journey.name, err)
	}
	request.AddCookie(&http.Cookie{Name: "__Host-carry_oauth", Value: started.binding})
	response, err := browser.Do(request)
	if err != nil {
		t.Fatalf("replay %s callback: %v", journey.name, err)
	}
	defer response.Body.Close()
	cookie := responseCookie(response, "__Host-carry_session")
	if response.StatusCode != http.StatusSeeOther || cookie == nil {
		t.Fatalf("replay %s status = %d, cookie = %#v, body = %s", journey.name, response.StatusCode, cookie, readBody(response))
	}
	return cookie
}

func identityMethodRequest(
	t *testing.T,
	client *http.Client,
	method string,
	requestURL string,
	origin string,
	credential string,
	idempotencyKey string,
) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		t.Fatalf("create Identity method request: %v", err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.AddCookie(&http.Cookie{Name: "__Host-carry_session", Value: credential})
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send Identity method request: %v", err)
	}
	return response
}

func assertCredentialStatus(t *testing.T, client *http.Client, carryURL string, credential string, expected int) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, carryURL+"/v1/me", nil)
	if err != nil {
		t.Fatalf("create stale credential request: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "__Host-carry_session", Value: credential})
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send stale credential request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		t.Fatalf("credential /me status = %d, want %d, body = %s", response.StatusCode, expected, readBody(response))
	}
}

type browserProviderJourney struct {
	name          string
	provider      identity.ExternalLoginProvider
	startPath     string
	callbackPath  string
	prepare       func(*url.URL)
	providerCalls func() int64
}

type startedBrowserLogin struct {
	state   string
	binding string
}

func exerciseProviderBrowserJourney(
	t *testing.T,
	browser *http.Client,
	carryURL string,
	journey browserProviderJourney,
	spaceName string,
) string {
	t.Helper()
	started := startBrowserLogin(t, browser, carryURL, journey)
	before := journey.providerCalls()
	response, firstSession := completeBrowserLogin(t, browser, carryURL, journey, started, "first-code")
	if response.Header.Get("Location") != carryURL+"/" || journey.providerCalls() <= before {
		t.Fatalf("%s first callback location = %q, provider calls = %d -> %d", journey.name, response.Header.Get("Location"), before, journey.providerCalls())
	}
	afterFirst := journey.providerCalls()

	replayURL := carryURL + journey.callbackPath + "?code=first-code&state=" + url.QueryEscape(started.state)
	request, err := http.NewRequest(http.MethodGet, replayURL, nil)
	if err != nil {
		t.Fatalf("create %s replay: %v", journey.name, err)
	}
	request.AddCookie(&http.Cookie{Name: "__Host-carry_oauth", Value: started.binding})
	replayed, err := browser.Do(request)
	if err != nil {
		t.Fatalf("replay %s callback: %v", journey.name, err)
	}
	defer replayed.Body.Close()
	replayedSession := responseCookie(replayed, "__Host-carry_session")
	if replayed.StatusCode != http.StatusSeeOther || replayedSession == nil || replayedSession.Value != firstSession.Value || journey.providerCalls() != afterFirst {
		t.Fatalf("%s replay status = %d, session = %#v, calls = %d", journey.name, replayed.StatusCode, replayedSession, journey.providerCalls())
	}

	userID, memberships := loadCurrentUser(t, browser, carryURL)
	if memberships != 0 {
		t.Fatalf("%s first login Memberships = %d", journey.name, memberships)
	}
	createFirstSpace(t, browser, carryURL, journey.name+" User", spaceName)
	logoutBrowser(t, browser, carryURL)

	started = startBrowserLogin(t, browser, carryURL, journey)
	before = journey.providerCalls()
	_, repeatSession := completeBrowserLogin(t, browser, carryURL, journey, started, "repeat-code")
	if repeatSession.Value == firstSession.Value || journey.providerCalls() <= before {
		t.Fatalf("%s repeat session = %q, first = %q, calls = %d -> %d", journey.name, repeatSession.Value, firstSession.Value, before, journey.providerCalls())
	}
	repeatUserID, memberships := loadCurrentUser(t, browser, carryURL)
	if repeatUserID != userID || memberships != 1 {
		t.Fatalf("%s repeat User = %q/%q, Memberships = %d", journey.name, repeatUserID, userID, memberships)
	}
	logoutBrowser(t, browser, carryURL)
	return userID
}

func startBrowserLogin(t *testing.T, browser *http.Client, carryURL string, journey browserProviderJourney) startedBrowserLogin {
	t.Helper()
	response := browserRequest(t, browser, http.MethodPost, carryURL+journey.startPath, carryURL, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("start %s status = %d, body = %s", journey.name, response.StatusCode, readBody(response))
	}
	authorization, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse %s authorization URL: %v", journey.name, err)
	}
	state := authorization.Query().Get("state")
	binding := responseCookie(response, "__Host-carry_oauth")
	if state == "" || binding == nil || binding.Value == "" {
		t.Fatalf("%s start state = %q, binding = %#v", journey.name, state, binding)
	}
	journey.prepare(authorization)
	return startedBrowserLogin{state: state, binding: binding.Value}
}

func completeBrowserLogin(
	t *testing.T,
	browser *http.Client,
	carryURL string,
	journey browserProviderJourney,
	started startedBrowserLogin,
	code string,
) (*http.Response, *http.Cookie) {
	t.Helper()
	callbackURL := carryURL + journey.callbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(started.state)
	response := browserRequest(t, browser, http.MethodGet, callbackURL, "", nil)
	if response.StatusCode != http.StatusSeeOther {
		defer response.Body.Close()
		t.Fatalf("complete %s status = %d, body = %s", journey.name, response.StatusCode, readBody(response))
	}
	session := responseCookie(response, "__Host-carry_session")
	if code != "outage" && (session == nil || session.Value == "") {
		_ = response.Body.Close()
		t.Fatalf("%s callback has no Carry Session", journey.name)
	}
	_ = response.Body.Close()
	return response, session
}

func loadCurrentUser(t *testing.T, browser *http.Client, carryURL string) (string, int) {
	t.Helper()
	response := browserRequest(t, browser, http.MethodGet, carryURL+"/v1/me", "", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("load current User status = %d, body = %s", response.StatusCode, readBody(response))
	}
	var payload struct {
		UserID string `json:"user_id"`
		Spaces []any  `json:"spaces"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode current User: %v", err)
	}
	return payload.UserID, len(payload.Spaces)
}

func createFirstSpace(t *testing.T, browser *http.Client, carryURL string, displayName string, spaceName string) {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"display_name":%q,"name":%q}`, displayName, spaceName))
	request, err := http.NewRequest(http.MethodPost, carryURL+"/v1/spaces", body)
	if err != nil {
		t.Fatalf("create first Space request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "space-"+strings.ToLower(strings.ReplaceAll(spaceName, " ", "-")))
	request.Header.Set("Origin", carryURL)
	response, err := browser.Do(request)
	if err != nil {
		t.Fatalf("create first Space: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create first Space status = %d, body = %s", response.StatusCode, readBody(response))
	}
}

func logoutBrowser(t *testing.T, browser *http.Client, carryURL string) {
	t.Helper()
	response := browserRequest(t, browser, http.MethodDelete, carryURL+"/v1/browser/sessions/current", carryURL, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", response.StatusCode, readBody(response))
	}
	response = browserRequest(t, browser, http.MethodGet, carryURL+"/v1/me", "", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked Browser Session /me status = %d", response.StatusCode)
	}
}

func browserRequest(
	t *testing.T,
	browser *http.Client,
	method string,
	requestURL string,
	origin string,
	body io.Reader,
) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		t.Fatalf("create Browser request: %v", err)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := browser.Do(request)
	if err != nil {
		t.Fatalf("send Browser request: %v", err)
	}
	return response
}

func responseCookie(response *http.Response, name string) *http.Cookie {
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func readBody(response *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return string(body)
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func composeExternalLoginTestAPI(
	t *testing.T,
	pool *pgxpool.Pool,
	store *carrypostgres.Store,
	externalPersistence identity.ExternalLoginPersistence,
	credentials identity.Credentials,
	origin carryserver.ExternalOrigin,
	google identity.GoogleLoginClient,
	github identity.GitHubLoginClient,
	extraSubmitters ...any,
) http.Handler {
	t.Helper()
	var emailSubmitter identity.EmailCodeSubmitter = unavailableEmailCodeSubmitter{}
	var invitationSubmitter space.InvitationSubmitter = acceptedInvitationFixture{}
	for _, dependency := range extraSubmitters {
		matched := false
		if submitter, ok := dependency.(identity.EmailCodeSubmitter); ok {
			emailSubmitter = submitter
			matched = true
		}
		if submitter, ok := dependency.(space.InvitationSubmitter); ok {
			invitationSubmitter = submitter
			matched = true
		}
		if !matched {
			t.Fatalf("unsupported test submitter %T", dependency)
		}
	}
	emailLogin, err := identity.NewEmailLogin(store, emailSubmitter, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	externalLogin, err := identity.NewExternalLogin(externalPersistence, google, github, credentials)
	if err != nil {
		t.Fatalf("compose external login: %v", err)
	}
	identityMethods, err := identity.NewMethods(store, credentials)
	if err != nil {
		t.Fatalf("compose Identity methods: %v", err)
	}
	firstSpace, err := space.NewFirstSpace(store)
	if err != nil {
		t.Fatalf("compose first Space: %v", err)
	}
	cliLogin, err := identity.NewCLILogin(store, credentials, origin.String())
	if err != nil {
		t.Fatalf("compose CLI login: %v", err)
	}
	authentication, err := carryserver.NewUserAuthentication(store, store, credentials, origin)
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	identityRoutes, err := carryserver.NewUserIdentityRoutes(
		emailLogin, externalLogin, identityMethods, store, cliLogin, credentials,
		origin, carryserver.NewRequestSource(nil), store,
	)
	if err != nil {
		t.Fatalf("compose User identity routes: %v", err)
	}
	spaceInvitations, err := space.NewInvitations(store, invitationSubmitter, origin.InvitationsURL())
	if err != nil {
		t.Fatalf("compose Space invitations: %v", err)
	}
	spaceRoutes, err := carryserver.NewUserSpaceRoutesWithInvitations(firstSpace, spaceInvitations, store, credentials, origin)
	if err != nil {
		t.Fatalf("compose User Space routes: %v", err)
	}
	bundle, err := machine.CreateCertificateBundle([]string{"localhost"}, time.Now())
	if err != nil {
		t.Fatalf("create Machine test PKI: %v", err)
	}
	authority, err := machine.LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
	if err != nil {
		t.Fatalf("load Machine test authority: %v", err)
	}
	connectionRoot, err := machine.ParseConnectionRoot(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatalf("create Machine connection credentials: %v", err)
	}
	connections, err := machine.NewConnections(store, connectionRoot, authority, origin.String())
	if err != nil {
		t.Fatalf("compose Machine connections: %v", err)
	}
	machineRoutes, err := carryserver.NewUserMachineRoutes(connections, credentials, origin, carryserver.NewRequestSource(nil))
	if err != nil {
		t.Fatalf("compose User Machine routes: %v", err)
	}
	conversationRoutes, err := carryserver.NewConversationRoutes(store, store)
	if err != nil {
		t.Fatalf("compose Conversation routes: %v", err)
	}
	workRoutes, err := carryserver.NewWorkRoutes(store, store)
	if err != nil {
		t.Fatalf("compose Work routes: %v", err)
	}
	userRoutes, err := carryserver.NewUserRoutes(
		authentication, identityRoutes, spaceRoutes, machineRoutes, conversationRoutes, workRoutes,
	)
	if err != nil {
		t.Fatalf("compose User routes: %v", err)
	}
	hostRoutes, err := carryserver.NewMachineRoutes(store, store, connections)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	api, err := carryserver.NewAPI(pool, userRoutes, hostRoutes)
	if err != nil {
		t.Fatalf("compose API: %v", err)
	}
	return api.Handler()
}

type commitResponseLossStore struct {
	*carrypostgres.Store
	mu         sync.Mutex
	loseGoogle bool
	loseGitHub bool
	lost       map[identity.ExternalLoginProvider]identity.BrowserSession
}

func newCommitResponseLossStore(store *carrypostgres.Store) *commitResponseLossStore {
	return &commitResponseLossStore{
		Store: store, loseGoogle: true, loseGitHub: true,
		lost: make(map[identity.ExternalLoginProvider]identity.BrowserSession),
	}
}

func (store *commitResponseLossStore) CompleteGoogleLogin(
	ctx context.Context,
	command identity.CompleteGoogleLoginCommand,
) (identity.BrowserSession, error) {
	session, err := store.Store.CompleteGoogleLogin(ctx, command)
	if err != nil {
		return identity.BrowserSession{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loseGoogle {
		store.loseGoogle = false
		store.lost[identity.GoogleLoginProvider] = session
		return identity.BrowserSession{}, errors.New("simulated Google commit response loss")
	}
	return session, nil
}

func (store *commitResponseLossStore) CompleteGitHubLogin(
	ctx context.Context,
	command identity.CompleteGitHubLoginCommand,
) (identity.BrowserSession, error) {
	session, err := store.Store.CompleteGitHubLogin(ctx, command)
	if err != nil {
		return identity.BrowserSession{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loseGitHub {
		store.loseGitHub = false
		store.lost[identity.GitHubLoginProvider] = session
		return identity.BrowserSession{}, errors.New("simulated GitHub commit response loss")
	}
	return session, nil
}

func (store *commitResponseLossStore) LostSessions() map[identity.ExternalLoginProvider]identity.BrowserSession {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make(map[identity.ExternalLoginProvider]identity.BrowserSession, len(store.lost))
	for provider, session := range store.lost {
		result[provider] = session
	}
	return result
}

type acceptedInvitationFixture struct{}

func (acceptedInvitationFixture) InvitationPayloadDigest(message space.InvitationMessage) ([sha256.Size]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.DestinationURL + "\x00" + message.IdempotencyKey)), nil
}

func (acceptedInvitationFixture) SubmitInvitation(context.Context, space.InvitationMessage, [sha256.Size]byte) space.InvitationSubmission {
	return space.InvitationSubmission{State: space.InvitationSubmissionAccepted, ProviderMessageID: "fixture-invitation"}
}

type unavailableEmailCodeSubmitter struct{}

func (unavailableEmailCodeSubmitter) PayloadDigest(message identity.EmailCodeMessage) ([sha256.Size]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.IdempotencyKey)), nil
}

func (unavailableEmailCodeSubmitter) SubmitEmailCode(
	context.Context,
	identity.EmailCodeMessage,
	[sha256.Size]byte,
) identity.EmailSubmission {
	return identity.EmailSubmission{State: identity.EmailSubmissionRejected}
}

type googleProviderFixture struct {
	*httptest.Server
	privateKey *rsa.PrivateKey
	tokenCalls atomic.Int64
	mu         sync.Mutex
	nonce      string
	challenge  string
}

func newGoogleProviderFixture(t *testing.T) *googleProviderFixture {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate Google fixture key: %v", err)
	}
	fixture := &googleProviderFixture{privateKey: privateKey}
	fixture.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			fixture.tokenCalls.Add(1)
			if err := request.ParseForm(); err != nil {
				http.Error(response, "invalid form", http.StatusBadRequest)
				return
			}
			fixture.mu.Lock()
			nonce, challenge := fixture.nonce, fixture.challenge
			fixture.mu.Unlock()
			if pkceChallenge(request.Form.Get("code_verifier")) != challenge {
				http.Error(response, "invalid PKCE", http.StatusBadRequest)
				return
			}
			token := signedGoogleIDToken(t, privateKey, "google-client", nonce, time.Now(), time.Now().Add(5*time.Minute))
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(map[string]any{
				"access_token": "transient-google-token", "token_type": "Bearer", "expires_in": 300, "id_token": token,
			})
		case "/jwks":
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(map[string]any{"keys": []jose.JSONWebKey{{
				Key: &privateKey.PublicKey, KeyID: "carry-test-key", Algorithm: "RS256", Use: "sig",
			}}})
		default:
			http.NotFound(response, request)
		}
	}))
	return fixture
}

func (fixture *googleProviderFixture) Expect(nonce string, challenge string) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.nonce = nonce
	fixture.challenge = challenge
}

type githubProviderFixture struct {
	*httptest.Server
	tokenCalls atomic.Int64
	userCalls  atomic.Int64
	outage     atomic.Bool
	mu         sync.Mutex
	challenge  string
}

func newGitHubProviderFixture(t *testing.T) *githubProviderFixture {
	t.Helper()
	fixture := &githubProviderFixture{}
	fixture.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			fixture.tokenCalls.Add(1)
			if fixture.outage.Load() {
				http.Error(response, "provider unavailable", http.StatusServiceUnavailable)
				return
			}
			if err := request.ParseForm(); err != nil {
				http.Error(response, "invalid form", http.StatusBadRequest)
				return
			}
			fixture.mu.Lock()
			challenge := fixture.challenge
			fixture.mu.Unlock()
			if pkceChallenge(request.Form.Get("code_verifier")) != challenge {
				http.Error(response, "invalid PKCE", http.StatusBadRequest)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"access_token":"transient-github-token","token_type":"bearer","scope":""}`))
		case "/user":
			fixture.userCalls.Add(1)
			if request.Header.Get("Authorization") != "Bearer transient-github-token" {
				http.Error(response, "missing access token", http.StatusUnauthorized)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":424242,"login":"rename-does-not-matter","email":"same@example.com"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	return fixture
}

func (fixture *githubProviderFixture) Expect(challenge string) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.challenge = challenge
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func openExternalLoginTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("CARRY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("CARRY_TEST_DATABASE_URL is required")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if host != "localhost" && (address == nil || !address.IsLoopback()) {
		t.Fatalf("refusing non-local PostgreSQL host %q", host)
	}
	pool, err := carrypostgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	var databaseName string
	if err := pool.QueryRow(ctx, `select current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("inspect PostgreSQL database: %v", err)
	}
	if !strings.HasPrefix(databaseName, "carry_test_") || !strings.HasSuffix(databaseName, "_postgres") {
		t.Fatalf("refusing PostgreSQL database %q", databaseName)
	}
	if err := carrypostgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		truncate external_login_transactions, google_identities, github_identities,
		email_login_attempts, email_login_challenges, browser_sessions,
		space_memberships, spaces, carry_users cascade
	`); err != nil {
		t.Fatalf("reset external login facts: %v", err)
	}
	return pool
}
