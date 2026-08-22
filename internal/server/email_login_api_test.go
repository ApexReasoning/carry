package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
)

const testChallengeID = "11111111-1111-4111-8111-111111111111"

func TestEmailRequestSubmitsExactChallengeAndRecipient(t *testing.T) {
	t.Parallel()
	store := &recordingEmailLoginStore{}
	sender := &recordingEmailSender{}
	handler := emailTestAPI(t, store, sender, unavailableSpaceCreation{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/email/challenges",
		strings.NewReader(`{"challenge_id":"`+testChallengeID+`","email":"  Person@Example.COM "}`),
	)
	request.RemoteAddr = "203.0.113.18:49152"
	request.Header.Set("X-Forwarded-For", "198.51.100.99")
	request.Header.Set("Idempotency-Key", "request-code-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	expectedSource := testIdentityCredentials(t).SourceDigest("203.0.113.18")
	if store.prepared.CanonicalEmail != "person@example.com" || store.prepared.ChallengeID != testChallengeID ||
		store.prepared.IdempotencyKey != "request-code-1" || store.prepared.SourceDigest != expectedSource {
		t.Fatalf("prepared challenge = %#v", store.prepared)
	}
	if sender.message.Recipient != "person@example.com" || sender.message.IdempotencyKey != "carry-email-"+testChallengeID ||
		len(sender.message.Code) != identity.EmailCodeDigits {
		t.Fatalf("email message = %#v", sender.message)
	}
	if store.submission.State != identity.EmailSubmissionAccepted || store.submission.ProviderMessageID != "resend-message" {
		t.Fatalf("recorded submission = %#v", store.submission)
	}
}

func TestEmailRequestMapsIdempotencyConflictToHTTP409(t *testing.T) {
	t.Parallel()
	store := &recordingEmailLoginStore{prepareErr: identity.ErrIdempotencyConflict}
	handler := emailTestAPI(t, store, &recordingEmailSender{}, unavailableSpaceCreation{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/email/challenges",
		strings.NewReader(`{"challenge_id":"`+testChallengeID+`","email":"person@example.com"}`),
	)
	request.RemoteAddr = "203.0.113.18:49152"
	request.Header.Set("Idempotency-Key", "reused-request-key")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), identity.ErrIdempotencyConflict.Error()) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEmailRequestKeepsAdmissionCauseOutOfTheUserResponse(t *testing.T) {
	t.Parallel()
	store := &recordingEmailLoginStore{prepareErr: identity.ErrEmailSourceAdmissionLimited}
	handler := emailTestAPI(t, store, &recordingEmailSender{}, unavailableSpaceCreation{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/email/challenges",
		strings.NewReader(`{"challenge_id":"`+testChallengeID+`","email":"person@example.com"}`),
	)
	request.RemoteAddr = "203.0.113.18:49152"
	request.Header.Set("Idempotency-Key", "source-limited-request")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests ||
		!strings.Contains(response.Body.String(), identity.ErrEmailRateLimited.Error()) ||
		strings.Contains(response.Body.String(), "source admission") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEmailVerifySetsStableHostOnlyCookie(t *testing.T) {
	t.Parallel()
	store := &recordingEmailLoginStore{session: identity.BrowserSession{
		SessionID: testSessionID, UserID: "user-5", ExpiresAt: time.Now().Add(time.Hour),
	}}
	handler := emailTestAPI(t, store, &recordingEmailSender{}, unavailableSpaceCreation{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/email/challenges/"+testChallengeID+"/verify",
		strings.NewReader(`{"code":"123456"}`),
	)
	request.Header.Set("Idempotency-Key", "verify-code-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != browserSessionCookie || !cookies[0].HttpOnly ||
		!cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatalf("Browser Session cookie = %#v", cookies)
	}
	sessionID, ok := testIdentityCredentials(t).ParseBrowserSessionCredential(cookies[0].Value)
	if !ok || sessionID != testSessionID {
		t.Fatalf("session credential resolved to %q, %t", sessionID, ok)
	}
}

func TestSpaceCreationUsesAuthenticatedUserAndNarrowAuthority(t *testing.T) {
	t.Parallel()
	spaces := &recordingSpaceCreation{created: space.CreatedSpace{
		SpaceID: "22222222-2222-4222-8222-222222222222", Name: "Research", Slug: "research",
		CanManageMembers: true, CanEnrollMachines: true,
	}}
	handler := emailTestAPI(t, &recordingEmailLoginStore{}, &recordingEmailSender{}, spaces)
	request := httptest.NewRequest(http.MethodPost, "/v1/spaces", bytes.NewBufferString(
		`{"name":"Research"}`,
	))
	credential, err := testIdentityCredentials(t).BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatalf("create Browser Session credential: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	request.Header.Set("Idempotency-Key", "create-space")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if spaces.command.UserID != "50000000-0000-4000-8000-000000000005" || spaces.command.Name != "Research" || spaces.command.Slug != "research" {
		t.Fatalf("Space command = %#v", spaces.command)
	}
	if !strings.Contains(response.Body.String(), `"slug":"research"`) ||
		!strings.Contains(response.Body.String(), `"can_manage_members":true`) ||
		!strings.Contains(response.Body.String(), `"can_enroll_machines":true`) {
		t.Fatalf("Space response = %s", response.Body.String())
	}
}

func TestSpaceCreationRejectsCallerFactsAndMapsRecovery(t *testing.T) {
	t.Parallel()
	credentials := testIdentityCredentials(t)
	credential, err := credentials.BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatalf("create Browser Session credential: %v", err)
	}
	serve := func(body string, spaces *recordingSpaceCreation) *httptest.ResponseRecorder {
		handler := emailTestAPI(t, &recordingEmailLoginStore{}, &recordingEmailSender{}, spaces)
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/spaces",
			strings.NewReader(body),
		)
		request.AddCookie(&http.Cookie{
			Name:  browserSessionCookie,
			Value: credential,
		})
		request.Header.Set("Idempotency-Key", "create-space-recovery")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	t.Run("caller cannot nominate creator", func(t *testing.T) {
		spaces := &recordingSpaceCreation{}
		response := serve(`{"name":"Research","user_id":"attacker"}`, spaces)
		if response.Code != http.StatusBadRequest || spaces.command.UserID != "" {
			t.Fatalf("status = %d, command = %#v", response.Code, spaces.command)
		}
	})

	t.Run("invalid slug maps one stable reason", func(t *testing.T) {
		spaces := &recordingSpaceCreation{}
		response := serve(`{"name":"cаrry"}`, spaces)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), space.ErrSpaceSlugMixedScripts.Error()) || spaces.command.UserID != "" {
			t.Fatalf("status = %d, body = %s, command = %#v", response.Code, response.Body.String(), spaces.command)
		}
	})

	t.Run("slug conflict carries one unreserved suggestion", func(t *testing.T) {
		spaces := &recordingSpaceCreation{
			createError: func(command space.CreateSpaceCommand) error {
				return space.NewSlugConflictError(command)
			},
		}
		response := serve(`{"name":"Research"}`, spaces)
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"slug":"research"`) {
			t.Fatalf("conflicting slug body = %s", response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"suggested_slug":"research-2"`) {
			t.Fatalf("suggested slug body = %s", response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"suggested_suffix":2`) {
			t.Fatalf("suggested suffix body = %s", response.Body.String())
		}
	})
}

func TestSpaceCreationRejectsTransitionalBearerBeforeSpaceBehavior(t *testing.T) {
	t.Parallel()
	spaces := &recordingSpaceCreation{}
	handler := emailTestAPI(t, &recordingEmailLoginStore{}, &recordingEmailSender{}, spaces)
	request := httptest.NewRequest(http.MethodPost, "/v1/spaces", bytes.NewBufferString(
		`{"name":"Research"}`,
	))
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "bearer-space")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || spaces.command.UserID != "" {
		t.Fatalf("status = %d, Space command = %#v", response.Code, spaces.command)
	}
}

func TestMalformedTrustedProxyChainIsRejectedBeforeChallenge(t *testing.T) {
	t.Parallel()
	store := &recordingEmailLoginStore{}
	handler := emailTestAPIWithSources(
		t, store, &recordingEmailSender{}, unavailableSpaceCreation{},
		NewRequestSource([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}),
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/email/challenges", strings.NewReader(
		`{"challenge_id":"`+testChallengeID+`","email":"person@example.com"}`,
	))
	request.RemoteAddr = "10.0.0.8:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.17, malformed")
	request.Header.Set("Idempotency-Key", "malformed-forwarding-chain")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || store.prepared.ChallengeID != "" {
		t.Fatalf("status = %d, prepared = %#v", response.Code, store.prepared)
	}
}

func TestCrossSiteEmailRequestIsRejectedBeforeChallenge(t *testing.T) {
	t.Parallel()
	store := &recordingEmailLoginStore{}
	handler := emailTestAPI(t, store, &recordingEmailSender{}, unavailableSpaceCreation{})
	request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/auth/email/challenges", strings.NewReader(
		`{"challenge_id":"`+testChallengeID+`","email":"person@example.com"}`,
	))
	request.Host = "carry.example"
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.Header.Set("Idempotency-Key", "cross-site")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || store.prepared.ChallengeID != "" {
		t.Fatalf("status = %d, prepared = %#v", response.Code, store.prepared)
	}
}

func emailTestAPI(
	t *testing.T,
	emailPersistence identity.EmailLoginPersistence,
	sender identity.EmailCodeSubmitter,
	spacePersistence space.SpaceCreationPersistence,
) http.Handler {
	t.Helper()
	return emailTestAPIWithSources(t, emailPersistence, sender, spacePersistence, NewRequestSource(nil))
}

func emailTestAPIWithSources(
	t *testing.T,
	emailPersistence identity.EmailLoginPersistence,
	sender identity.EmailCodeSubmitter,
	spacePersistence space.SpaceCreationPersistence,
	requestSources RequestSource,
) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	credentials := testIdentityCredentials(t)
	emailLogin, err := identity.NewEmailLogin(emailPersistence, sender, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	spaceCreator, err := space.NewCreator(spacePersistence)
	if err != nil {
		t.Fatalf("compose Space creator: %v", err)
	}
	sessions := &recordingBrowserSessions{user: identity.AuthenticatedUser{
		UserID:      "50000000-0000-4000-8000-000000000005",
		DisplayName: "Member 50000000",
	}}
	member := testUserRoutes(t, authority)
	authentication, err := NewUserAuthentication(&recordingCLICredentials{}, sessions, credentials, testExternalOrigin(t))
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	identityRoutes, err := NewUserIdentityRoutes(
		emailLogin,
		unavailableExternalLogin{},
		unavailableIdentityMethods{},
		sessions,
		&recordingCLILogins{},
		credentials,
		testExternalOrigin(t),
		requestSources,
		emptyMemberships{},
	)
	if err != nil {
		t.Fatalf("compose User identity routes: %v", err)
	}
	spaceRoutes, err := NewUserSpaceRoutes(spaceCreator)
	if err != nil {
		t.Fatalf("compose User Space routes: %v", err)
	}
	member.authentication = authentication
	member.identity = identityRoutes
	member.spaces = spaceRoutes
	machine, err := NewMachineRoutes(&recordingMachineRuns{}, unavailableMachineConversations{}, unavailableMachineConnections{})
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

type recordingEmailLoginStore struct {
	prepared   identity.PrepareEmailChallengeCommand
	prepareErr error
	submission identity.EmailSubmission
	verified   identity.VerifyEmailChallengeCommand
	session    identity.BrowserSession
}

func (store *recordingEmailLoginStore) PrepareEmailChallenge(
	_ context.Context,
	command identity.PrepareEmailChallengeCommand,
) (identity.EmailChallenge, error) {
	store.prepared = command
	if store.prepareErr != nil {
		return identity.EmailChallenge{}, store.prepareErr
	}
	return identity.EmailChallenge{
		ChallengeID: command.ChallengeID, CanonicalEmail: command.CanonicalEmail,
		ExpiresAt: time.Now().Add(identity.EmailCodeLifetime), PayloadDigest: command.PayloadDigest,
		SubmissionState: identity.EmailSubmissionPrepared, CanSubmit: true,
	}, nil
}

func (store *recordingEmailLoginStore) RecordEmailSubmission(
	_ context.Context,
	challengeID string,
	payloadDigest [sha256.Size]byte,
	submission identity.EmailSubmission,
) (identity.EmailChallenge, error) {
	store.submission = submission
	return identity.EmailChallenge{
		ChallengeID: challengeID, CanonicalEmail: store.prepared.CanonicalEmail,
		ExpiresAt: time.Now().Add(identity.EmailCodeLifetime), SubmissionState: submission.State,
		ProviderMessageID: submission.ProviderMessageID, PayloadDigest: payloadDigest,
	}, nil
}

func (store *recordingEmailLoginStore) VerifyEmailChallenge(
	_ context.Context,
	command identity.VerifyEmailChallengeCommand,
) (identity.BrowserSession, error) {
	store.verified = command
	return store.session, nil
}

func (*recordingEmailLoginStore) EmailForReauthentication(context.Context, string, string) (string, error) {
	return "person@example.com", nil
}

type recordingEmailSender struct {
	message identity.EmailCodeMessage
}

func (*recordingEmailSender) PayloadDigest(message identity.EmailCodeMessage) ([sha256.Size]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.Code + "\x00" + message.IdempotencyKey)), nil
}

func (sender *recordingEmailSender) SubmitEmailCode(
	_ context.Context,
	message identity.EmailCodeMessage,
	_ [sha256.Size]byte,
) identity.EmailSubmission {
	sender.message = message
	return identity.EmailSubmission{State: identity.EmailSubmissionAccepted, ProviderMessageID: "resend-message"}
}

type recordingSpaceCreation struct {
	command     space.CreateSpaceCommand
	created     space.CreatedSpace
	createError func(space.CreateSpaceCommand) error
}

func (store *recordingSpaceCreation) CreateSpace(
	_ context.Context,
	command space.CreateSpaceCommand,
) (space.CreatedSpace, error) {
	store.command = command
	if store.createError != nil {
		return space.CreatedSpace{}, store.createError(command)
	}
	return store.created, nil
}
