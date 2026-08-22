package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
)

func TestUserErrorBoundaryKeepsRecoveryPublicAndMechanismsPrivate(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		err     error
		status  int
		message string
	}{
		"forbidden":                       {space.ErrForbidden, http.StatusForbidden, "You do not have access to this Space."},
		"missing Work":                    {work.ErrNotFound, http.StatusNotFound, "This Work is unavailable."},
		"Work saved request changed":      {work.ErrIdempotencyConflict, http.StatusConflict, "This action no longer matches the saved request. Reload before trying again."},
		"Conversation request changed":    {conversation.ErrIdempotencyConflict, http.StatusConflict, "This action no longer matches the saved request. Reload before trying again."},
		"reply pending":                   {conversation.ErrReplyPending, http.StatusConflict, "Wait for Carry's reply before sending another message."},
		"reply conflict":                  {conversation.ErrReplyConflict, http.StatusConflict, "Reload the Conversation before trying again."},
		"Work not open":                   {work.ErrNotOpen, http.StatusConflict, "Reload this Work before choosing again."},
		"Work retry not needed":           {work.ErrRetryNotNeeded, http.StatusConflict, "Reload this Work before choosing again."},
		"Work review not current":         {work.ErrReviewNotCurrent, http.StatusConflict, "Reload this Work before choosing again."},
		"invalid Work goal":               {work.ErrInvalidGoal, http.StatusBadRequest, "Check the entered value and try again."},
		"invalid Work message":            {work.ErrInvalidMessage, http.StatusBadRequest, "Check the entered value and try again."},
		"invalid Conversation text":       {conversation.ErrInvalidText, http.StatusBadRequest, "Check the entered value and try again."},
		"invalid Work retry information":  {work.ErrInvalidIdempotency, http.StatusBadRequest, "Reload before trying again."},
		"invalid Work cursor":             {work.ErrInvalidCursor, http.StatusBadRequest, "Reload before trying again."},
		"invalid Conversation retry fact": {conversation.ErrInvalidIdempotency, http.StatusBadRequest, "Reload before trying again."},
		"invalid Conversation cursor":     {conversation.ErrInvalidCursor, http.StatusBadRequest, "Reload before trying again."},
		"invalid Conversation context":    {conversation.ErrInvalidContext, http.StatusBadRequest, "Reload before trying again."},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeUserStoreError(response, test.err, userReadFailure, "test User request")
			assertUserFacingResponse(t, response, test.status, test.message)
		})
	}

	for name, recovery := range map[string]userFailureRecovery{
		"read":     userReadFailure,
		"mutation": userMutationFailure,
	} {
		t.Run("internal "+name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeUserInternalError(response, recovery, "private operation", errors.New("database cause"))
			message := "Carry could not load this right now. Reload to try again."
			if recovery == userMutationFailure {
				message = "Carry could not confirm whether this change finished. Check the current page before trying again."
			}
			assertUserFacingResponse(t, response, http.StatusInternalServerError, message)
			if strings.Contains(response.Body.String(), "private operation") || strings.Contains(response.Body.String(), "database cause") {
				t.Fatalf("private diagnostics entered User response: %s", response.Body.String())
			}
		})
	}
}

func TestMachineErrorBoundaryRetainsAuthorityRecovery(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		err     error
		status  int
		message string
	}{
		"Space forbidden":                 {space.ErrForbidden, http.StatusForbidden, space.ErrForbidden.Error()},
		"Machine revoked":                 {machine.ErrMachineRevoked, http.StatusForbidden, machine.ErrMachineRevoked.Error()},
		"Machine missing":                 {machine.ErrMachineNotFound, http.StatusNotFound, machine.ErrMachineNotFound.Error()},
		"Work missing":                    {work.ErrNotFound, http.StatusNotFound, work.ErrNotFound.Error()},
		"stale Attempt":                   {run.ErrStaleAttempt, http.StatusConflict, "Run Attempt is stale or expired"},
		"stale claim":                     {conversation.ErrStaleReplyClaim, http.StatusConflict, "private Conversation reply claim is stale or expired"},
		"Work request conflict":           {work.ErrIdempotencyConflict, http.StatusConflict, work.ErrIdempotencyConflict.Error()},
		"Conversation request conflict":   {conversation.ErrIdempotencyConflict, http.StatusConflict, conversation.ErrIdempotencyConflict.Error()},
		"reply pending":                   {conversation.ErrReplyPending, http.StatusConflict, conversation.ErrReplyPending.Error()},
		"reply conflict":                  {conversation.ErrReplyConflict, http.StatusConflict, conversation.ErrReplyConflict.Error()},
		"Work not open":                   {work.ErrNotOpen, http.StatusConflict, work.ErrNotOpen.Error()},
		"Work retry not needed":           {work.ErrRetryNotNeeded, http.StatusConflict, work.ErrRetryNotNeeded.Error()},
		"Work review not current":         {work.ErrReviewNotCurrent, http.StatusConflict, work.ErrReviewNotCurrent.Error()},
		"invalid Run update":              {run.ErrInvalidUpdate, http.StatusBadRequest, run.ErrInvalidUpdate.Error()},
		"invalid Run outcome":             {run.ErrInvalidOutcome, http.StatusBadRequest, run.ErrInvalidOutcome.Error()},
		"invalid Work goal":               {work.ErrInvalidGoal, http.StatusBadRequest, work.ErrInvalidGoal.Error()},
		"invalid Work message":            {work.ErrInvalidMessage, http.StatusBadRequest, work.ErrInvalidMessage.Error()},
		"invalid Work retry information":  {work.ErrInvalidIdempotency, http.StatusBadRequest, work.ErrInvalidIdempotency.Error()},
		"invalid Work cursor":             {work.ErrInvalidCursor, http.StatusBadRequest, work.ErrInvalidCursor.Error()},
		"invalid Conversation text":       {conversation.ErrInvalidText, http.StatusBadRequest, conversation.ErrInvalidText.Error()},
		"invalid Conversation retry fact": {conversation.ErrInvalidIdempotency, http.StatusBadRequest, conversation.ErrInvalidIdempotency.Error()},
		"invalid Conversation cursor":     {conversation.ErrInvalidCursor, http.StatusBadRequest, conversation.ErrInvalidCursor.Error()},
		"invalid Conversation context":    {conversation.ErrInvalidContext, http.StatusBadRequest, conversation.ErrInvalidContext.Error()},
		"unexpected":                      {errors.New("database unavailable"), http.StatusInternalServerError, "request failed"},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeMachineStoreError(response, test.err)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("Machine response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUserWireAndSpecializedErrorsUsePublicLanguage(t *testing.T) {
	t.Parallel()

	invalidJSON := httptest.NewRecorder()
	if decodeJSON(invalidJSON, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{")), &struct{}{}) {
		t.Fatal("invalid JSON decoded")
	}
	assertUserFacingResponse(t, invalidJSON, http.StatusBadRequest, "Carry could not read this request.")

	trailingJSON := httptest.NewRecorder()
	var trailingDestination struct {
		Value string `json:"value"`
	}
	if decodeJSON(trailingJSON, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"first"} {"value":"second"}`)), &trailingDestination) {
		t.Fatal("trailing JSON decoded")
	}
	assertUserFacingResponse(t, trailingJSON, http.StatusBadRequest, "Carry could not read this request.")

	missingRetry := httptest.NewRecorder()
	if _, ok := requireIdempotencyKey(missingRetry, httptest.NewRequest(http.MethodPost, "/", nil)); ok {
		t.Fatal("missing retry information accepted")
	}
	assertUserFacingResponse(t, missingRetry, http.StatusBadRequest, "This action needs saved retry information. Reload before trying again.")

	writers := map[string]struct {
		write   func(http.ResponseWriter)
		status  int
		message string
	}{
		"email conflict": {
			func(response http.ResponseWriter) {
				writeEmailRequestError(response, identity.ErrIdempotencyConflict, "request email code")
			},
			http.StatusConflict, "This action no longer matches the saved request.",
		},
		"email unexpected": {
			func(response http.ResponseWriter) {
				writeEmailRequestError(response, errors.New("database unavailable"), "request email code")
			},
			http.StatusInternalServerError, "Carry could not confirm whether this change finished.",
		},
		"identity read": {
			func(response http.ResponseWriter) {
				writeIdentityMethodError(response, errors.New("database unavailable"), userReadFailure, "list sign-in methods")
			},
			http.StatusInternalServerError, "Carry could not load this right now.",
		},
		"identity conflict": {
			func(response http.ResponseWriter) {
				writeIdentityMethodError(response, identity.ErrIdempotencyConflict, userMutationFailure, "remove sign-in method")
			},
			http.StatusConflict, "This action no longer matches the saved request.",
		},
		"identity mutation": {
			func(response http.ResponseWriter) {
				writeIdentityMethodError(response, errors.New("database unavailable"), userMutationFailure, "remove sign-in method")
			},
			http.StatusInternalServerError, "Carry could not confirm whether this change finished.",
		},
		"member forbidden": {
			func(response http.ResponseWriter) {
				writeMemberError(response, space.ErrForbidden, userReadFailure, "list members")
			},
			http.StatusForbidden, "You do not have access to this Space.",
		},
		"member cursor": {
			func(response http.ResponseWriter) {
				writeMemberError(response, space.ErrInvalidMemberCursor, userReadFailure, "list members")
			},
			http.StatusBadRequest, "Reload before trying again.",
		},
		"member read": {
			func(response http.ResponseWriter) {
				writeMemberError(response, errors.New("database unavailable"), userReadFailure, "list members")
			},
			http.StatusInternalServerError, "Carry could not load this right now.",
		},
		"member conflict": {
			func(response http.ResponseWriter) {
				writeMemberError(response, space.ErrIdempotencyConflict, userMutationFailure, "remove member")
			},
			http.StatusConflict, "This action no longer matches the saved request.",
		},
		"member mutation": {
			func(response http.ResponseWriter) {
				writeMemberError(response, errors.New("database unavailable"), userMutationFailure, "remove member")
			},
			http.StatusInternalServerError, "Carry could not confirm whether this change finished.",
		},
		"invitation forbidden": {
			func(response http.ResponseWriter) {
				writeInvitationError(response, space.ErrForbidden, userReadFailure, "load invitation")
			},
			http.StatusForbidden, "You do not have access to this Space.",
		},
		"invitation recipient": {
			func(response http.ResponseWriter) {
				writeInvitationError(response, space.ErrInvalidInvitationRecipient, userMutationFailure, "issue invitation")
			},
			http.StatusBadRequest, "Check the Email address and try again.",
		},
		"invitation command": {
			func(response http.ResponseWriter) {
				writeInvitationError(response, space.ErrInvalidInvitation, userMutationFailure, "issue invitation")
			},
			http.StatusBadRequest, "Reload before trying again.",
		},
		"invitation conflict": {
			func(response http.ResponseWriter) {
				writeInvitationError(response, space.ErrIdempotencyConflict, userMutationFailure, "accept invitation")
			},
			http.StatusConflict, "This action no longer matches the saved request.",
		},
		"invitation read": {
			func(response http.ResponseWriter) {
				writeInvitationError(response, errors.New("database unavailable"), userReadFailure, "load invitation")
			},
			http.StatusInternalServerError, "Carry could not load this right now.",
		},
		"invitation mutation": {
			func(response http.ResponseWriter) {
				writeInvitationError(response, errors.New("database unavailable"), userMutationFailure, "accept invitation")
			},
			http.StatusInternalServerError, "Carry could not confirm whether this change finished.",
		},
	}
	for name, test := range writers {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.write(response)
			assertUserFacingResponse(t, response, test.status, test.message)
		})
	}
}

func assertUserFacingResponse(t *testing.T, response *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	if message != "" && !strings.Contains(response.Body.String(), message) {
		t.Fatalf("body = %s, want %q", response.Body.String(), message)
	}
	lower := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"run", "attempt", "claim", "lease", "fence", "version", "digest", "credential", "socket", "session handle", "idempotency key", "idempotency-key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("User response contains forbidden term %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestUserReadHandlersUseReadRecovery(t *testing.T) {
	t.Parallel()

	missingContext := httptest.NewRecorder()
	if _, ok := currentUser(missingContext, httptest.NewRequest(http.MethodGet, "/v1/me", nil)); ok {
		t.Fatal("missing User context accepted")
	}
	assertUserFacingResponse(t, missingContext, http.StatusInternalServerError, "Carry could not load this right now. Reload to try again.")

	authentication := httptest.NewRecorder()
	if _, ok := authenticatedUserResult(authentication, identity.AuthenticatedUser{}, errors.New("database unavailable")); ok {
		t.Fatal("failed authentication accepted")
	}
	assertUserFacingResponse(t, authentication, http.StatusInternalServerError, "Carry could not load this right now. Reload to try again.")

	membershipRequest := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	membershipRequest = membershipRequest.WithContext(context.WithValue(membershipRequest.Context(), userContextKey{}, identity.AuthenticatedUser{
		UserID: "10000000-0000-4000-8000-000000000001",
	}))
	membershipResponse := httptest.NewRecorder()
	(userAPI{memberships: failingMemberships{}}).me(membershipResponse, membershipRequest)
	assertUserFacingResponse(t, membershipResponse, http.StatusInternalServerError, "Carry could not load this right now. Reload to try again.")
}

type failingMemberships struct{}

func (failingMemberships) ListMemberships(context.Context, string) ([]space.Membership, error) {
	return nil, errors.New("database unavailable")
}

func TestMachineClaimReturnsCompleteWorkContextWithoutSecondCredential(t *testing.T) {
	t.Parallel()

	authority, machineCertificate := testMachineCertificate(t, "machine-12")
	runs := &recordingMachineRuns{claim: run.Claim{
		RunID: "run-4", AttemptID: "attempt-4", WorkID: "work-4", Fence: 3,
		LeaseExpiresAt: time.Date(2026, time.August, 18, 17, 0, 0, 0, time.UTC),
		Goal:           "Prepare renewal", CurrentUnderstanding: "Finance approved",
		BaseUnderstandingVersion: 2, InputEndSeq: 4,
		Messages: []run.Message{{AuthorUserID: "member-1", Text: "Legal supplied wording"}},
	}}
	request := httptest.NewRequest(http.MethodPost, "/v1/host/runs/claim", nil)
	request.TLS = verifiedMachineTLS(machineCertificate)
	response := httptest.NewRecorder()

	testAPI(t, authority, &recordingCLICredentials{}, &recordingMachineConnectionStub{}, runs).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if runs.claimMachineID != "machine-12" {
		t.Fatalf("claim Machine = %q", runs.claimMachineID)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"goal":"Prepare renewal"`)) ||
		bytes.Contains(response.Body.Bytes(), []byte("credential")) ||
		bytes.Contains(response.Body.Bytes(), []byte("writer")) {
		t.Fatalf("claim body = %s", response.Body.String())
	}
}

func TestMachineCommitBindsCertificateIdentity(t *testing.T) {
	t.Parallel()

	const (
		runID     = "d293609c-6c02-4c70-97b2-e6ec5f8d96ac"
		attemptID = "3b22d497-233c-4dcc-b496-a4a3f1c82f37"
	)
	authority, machineCertificate := testMachineCertificate(t, "machine-19")
	runs := &recordingMachineRuns{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/host/runs/"+runID+"/attempts/"+attemptID+"/understanding",
		bytes.NewBufferString(`{"fence":4,"base_understanding_version":2,"input_end_seq":5,"understanding":"Known","next_step":"Continue","review_required":true}`),
	)
	request.TLS = verifiedMachineTLS(machineCertificate)
	response := httptest.NewRecorder()

	testAPI(t, authority, &recordingCLICredentials{}, &recordingMachineConnectionStub{}, runs).
		ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if runs.commit.MachineID != "machine-19" || runs.commit.RunID != runID ||
		runs.commit.BaseUnderstandingVersion != 2 || !runs.commit.ReviewRequired {
		t.Fatalf("commit = %#v", runs.commit)
	}
	assertNoStore(t, response)
}

func TestMachineMutationRejectsMalformedAuthorityPath(t *testing.T) {
	t.Parallel()
	authority, machineCertificate := testMachineCertificate(t, "machine-20")
	runs := &recordingMachineRuns{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/host/runs/not-a-uuid/attempts/also-not-a-uuid/renew",
		bytes.NewBufferString(`{"fence":1}`),
	)
	request.TLS = verifiedMachineTLS(machineCertificate)
	response := httptest.NewRecorder()
	testAPI(t, authority, &recordingCLICredentials{}, &recordingMachineConnectionStub{}, runs).
		ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || runs.renewRunID != "" {
		t.Fatalf("malformed authority response = %d %s; renew Run = %q", response.Code, response.Body.String(), runs.renewRunID)
	}
	assertNoStore(t, response)
}

func verifiedMachineTLS(certificate *x509.Certificate) *tls.ConnectionState {
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
}

func testAPI(
	t *testing.T,
	authority *machine.CertificateAuthority,
	tokens CLICredentialAuthenticator,
	machines MachineConnections,
	runs *recordingMachineRuns,
) http.Handler {
	t.Helper()
	user := testUserRoutes(t, authority)
	authentication, err := NewUserAuthentication(tokens, unavailableBrowserSessions{}, testIdentityCredentials(t), testExternalOrigin(t))
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	userMachines, err := NewUserMachineRoutes(
		machines, testIdentityCredentials(t), testExternalOrigin(t), NewRequestSource(nil),
	)
	if err != nil {
		t.Fatalf("compose User Machine routes: %v", err)
	}
	user.authentication = authentication
	user.machines = userMachines
	machine, err := NewMachineRoutes(runs, unavailableMachineConversations{}, machines)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, user, machine)
}

func mustAPI(t *testing.T, user *UserRoutes, machine *MachineRoutes) http.Handler {
	t.Helper()
	return mustAPIWithReadiness(t, nil, user, machine)
}

func mustAPIWithReadiness(
	t *testing.T,
	readiness Readiness,
	user *UserRoutes,
	machine *MachineRoutes,
) http.Handler {
	t.Helper()
	apiServer, err := NewAPI(readiness, user, machine)
	if err != nil {
		t.Fatalf("compose API: %v", err)
	}
	return apiServer.Handler()
}

func testAuthority(t *testing.T) *machine.CertificateAuthority {
	t.Helper()
	authority, _ := testMachineCertificate(t, "authority-probe")
	return authority
}

func testMachineCertificate(t *testing.T, machineID string) (*machine.CertificateAuthority, *x509.Certificate) {
	t.Helper()
	now := time.Date(2026, time.August, 18, 16, 0, 0, 0, time.UTC)
	bundle, err := machine.CreateCertificateBundle([]string{"localhost"}, now)
	if err != nil {
		t.Fatalf("create PKI: %v", err)
	}
	authority, err := machine.LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
	if err != nil {
		t.Fatalf("load authority: %v", err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	issued, err := authority.IssueMachineCertificate(machineID, publicKeyDER, now)
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	block, _ := pem.Decode(issued.CertificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return authority, certificate
}

type recordingCLICredentials struct {
	user                      identity.AuthenticatedUser
	authenticatedCredentialID string
}

func (store *recordingCLICredentials) AuthenticateCLICredential(_ context.Context, credentialID string) (identity.AuthenticatedUser, error) {
	store.authenticatedCredentialID = credentialID
	if store.user.UserID == "" {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	return store.user, nil
}

type unavailableBrowserSessions struct{}

func (unavailableBrowserSessions) AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error) {
	return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
}

func (unavailableBrowserSessions) RevokeBrowserSession(context.Context, string) error { return nil }

type unavailableEmailLogins struct{}

func (unavailableEmailLogins) PrepareEmailChallenge(context.Context, identity.PrepareEmailChallengeCommand) (identity.EmailChallenge, error) {
	return identity.EmailChallenge{}, errors.New("not implemented")
}

func (unavailableEmailLogins) RecordEmailSubmission(
	context.Context,
	string,
	[sha256.Size]byte,
	identity.EmailSubmission,
) (identity.EmailChallenge, error) {
	return identity.EmailChallenge{}, errors.New("not implemented")
}

func (unavailableEmailLogins) VerifyEmailChallenge(context.Context, identity.VerifyEmailChallengeCommand) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("not implemented")
}

func (unavailableEmailLogins) EmailForReauthentication(context.Context, string, string) (string, error) {
	return "", errors.New("not implemented")
}

type unavailableEmailSender struct{}

func (unavailableEmailSender) PayloadDigest(identity.EmailCodeMessage) ([sha256.Size]byte, error) {
	return [sha256.Size]byte{}, nil
}

func (unavailableEmailSender) SubmitEmailCode(
	context.Context,
	identity.EmailCodeMessage,
	[sha256.Size]byte,
) identity.EmailSubmission {
	return identity.EmailSubmission{State: identity.EmailSubmissionRejected}
}

func testIdentityCredentials(t *testing.T) identity.Credentials {
	t.Helper()
	credentials, err := identity.NewCredentials(bytes.Repeat([]byte{7}, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("create test Identity credentials: %v", err)
	}
	return credentials
}

const testCLICredentialID = "24c970b5-2929-4875-8882-6c392f5cf4a5"

func testCLIBearer(t *testing.T) string {
	t.Helper()
	credential, err := testIdentityCredentials(t).CLICredential(testCLICredentialID, testExternalOrigin(t).String())
	if err != nil {
		t.Fatalf("create test CLI credential: %v", err)
	}
	return credential
}

func testUserRoutes(t *testing.T, authority *machine.CertificateAuthority) *UserRoutes {
	t.Helper()
	credentials := testIdentityCredentials(t)
	sessions := unavailableBrowserSessions{}
	emailLogin, err := identity.NewEmailLogin(unavailableEmailLogins{}, unavailableEmailSender{}, credentials)
	if err != nil {
		t.Fatalf("compose test email login: %v", err)
	}
	authentication, err := NewUserAuthentication(&recordingCLICredentials{}, sessions, credentials, testExternalOrigin(t))
	if err != nil {
		t.Fatalf("compose test User authentication: %v", err)
	}
	identityRoutes, err := NewUserIdentityRoutes(
		emailLogin,
		unavailableExternalLogin{},
		unavailableIdentityMethods{},
		sessions,
		&recordingCLILogins{},
		credentials,
		testExternalOrigin(t),
		NewRequestSource(nil),
		emptyMemberships{},
	)
	if err != nil {
		t.Fatalf("compose test User identity routes: %v", err)
	}
	spaceRoutes, err := NewUserSpaceRoutes(unavailableSpaceCreation{})
	if err != nil {
		t.Fatalf("compose test User Space routes: %v", err)
	}
	machineRoutes, err := NewUserMachineRoutes(unavailableMachineConnections{}, credentials, testExternalOrigin(t), NewRequestSource(nil))
	if err != nil {
		t.Fatalf("compose test User Machine routes: %v", err)
	}
	conversationRoutes, err := NewConversationRoutes(
		unavailableConversationCommands{},
		unavailableConversationQueries{},
	)
	if err != nil {
		t.Fatalf("compose test Conversation routes: %v", err)
	}
	workRoutes, err := NewWorkRoutes(unavailableWorkCommands{}, unavailableWorkQueries{})
	if err != nil {
		t.Fatalf("compose test Work routes: %v", err)
	}
	routes, err := NewUserRoutes(
		authentication,
		identityRoutes,
		spaceRoutes,
		machineRoutes,
		conversationRoutes,
		workRoutes,
	)
	if err != nil {
		t.Fatalf("compose test User routes: %v", err)
	}
	return routes
}

type unavailableExternalLogin struct{}

func (unavailableExternalLogin) StartGoogle(context.Context, string, string) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) StartGitHub(context.Context, string, string) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) StartGoogleReauthentication(context.Context, string, string) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) StartGitHubReauthentication(context.Context, string, string) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) StartGoogleLink(context.Context, string, string) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) StartGitHubLink(context.Context, string, string) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) CompleteGoogle(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("not implemented")
}

func (unavailableExternalLogin) CompleteGitHub(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("not implemented")
}

type unavailableIdentityMethods struct{}

func (unavailableIdentityMethods) List(context.Context, string, string) (identity.IdentityMethods, error) {
	return identity.IdentityMethods{}, errors.New("not implemented")
}

func (unavailableIdentityMethods) Unlink(context.Context, identity.UnlinkMethodCommand) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("not implemented")
}

func testExternalOrigin(t *testing.T) ExternalOrigin {
	t.Helper()
	origin, err := ParseExternalOrigin("https://carry.example")
	if err != nil {
		t.Fatalf("parse external origin: %v", err)
	}
	return origin
}

type unavailableSpaceCreation struct{}

func (unavailableSpaceCreation) CreateSpace(context.Context, space.CreateSpaceCommand) (space.CreatedSpace, error) {
	return space.CreatedSpace{}, errors.New("not implemented")
}

type emptyMemberships struct{}

func (emptyMemberships) ListMemberships(context.Context, string) ([]space.Membership, error) {
	return nil, nil
}

type unavailableMachineConnections struct{}

func (unavailableMachineConnections) Begin(context.Context, machine.BeginConnectionRequest) (machine.BegunConnection, error) {
	return machine.BegunConnection{}, errors.New("not implemented")
}
func (unavailableMachineConnections) Lookup(context.Context, machine.LookupConnectionRequest) (machine.ConnectionPreview, error) {
	return machine.ConnectionPreview{}, errors.New("not implemented")
}
func (unavailableMachineConnections) Approve(context.Context, machine.DecideConnectionRequest) error {
	return errors.New("not implemented")
}
func (unavailableMachineConnections) Deny(context.Context, machine.DecideConnectionRequest) error {
	return errors.New("not implemented")
}
func (unavailableMachineConnections) Poll(context.Context, string) (machine.ConnectedMachine, error) {
	return machine.ConnectedMachine{}, errors.New("not implemented")
}
func (unavailableMachineConnections) Cancel(context.Context, string) error {
	return errors.New("not implemented")
}
func (unavailableMachineConnections) List(context.Context, string, string, string) (machine.MachinePage, error) {
	return machine.MachinePage{}, errors.New("not implemented")
}
func (unavailableMachineConnections) RevokeFromBrowser(context.Context, string, string, string, string) (machine.MachineRecord, error) {
	return machine.MachineRecord{}, errors.New("not implemented")
}
func (unavailableMachineConnections) RevokeFromHost(context.Context, string, string, string) (machine.MachineRecord, error) {
	return machine.MachineRecord{}, errors.New("not implemented")
}

type recordingMachineConnectionStub struct{ unavailableMachineConnections }

type recordingMachineRuns struct {
	claimMachineID string
	claim          run.Claim
	claimErr       error
	renewRunID     string
	commit         run.CommitCommand
	finish         run.FinishCommand
}

func (store *recordingMachineRuns) ClaimRun(_ context.Context, machineID string) (run.Claim, error) {
	store.claimMachineID = machineID
	if store.claimErr != nil {
		return run.Claim{}, store.claimErr
	}
	if store.claim.RunID == "" {
		return run.Claim{}, run.ErrNoRunAvailable
	}
	return store.claim, nil
}

func (store *recordingMachineRuns) RenewRunAttempt(
	_ context.Context,
	_ string,
	runID string,
	_ string,
	_ int64,
) (time.Time, error) {
	store.renewRunID = runID
	return time.Time{}, run.ErrStaleAttempt
}

func (store *recordingMachineRuns) CommitWorkUnderstanding(_ context.Context, command run.CommitCommand) error {
	store.commit = command
	return nil
}

func (store *recordingMachineRuns) FinishUnresolvedAttempt(_ context.Context, command run.FinishCommand) error {
	store.finish = command
	return nil
}

type unavailableMachineConversations struct{}

func (unavailableMachineConversations) ClaimConversationReply(context.Context, string) (conversation.ReplyClaim, error) {
	return conversation.ReplyClaim{}, conversation.ErrNoReplyAvailable
}

func (unavailableMachineConversations) RenewConversationReply(context.Context, conversation.RenewReplyCommand) (time.Time, error) {
	return time.Time{}, conversation.ErrStaleReplyClaim
}

func (unavailableMachineConversations) CommitConversationReply(context.Context, conversation.CommitReplyCommand) (conversation.CommitReplyResult, error) {
	return conversation.CommitReplyResult{}, conversation.ErrStaleReplyClaim
}

type unavailableConversationCommands struct{}

func (unavailableConversationCommands) SendConversationMessage(context.Context, conversation.SendCommand) (conversation.Message, error) {
	return conversation.Message{}, errors.New("not implemented")
}

type unavailableConversationQueries struct{}

func (unavailableConversationQueries) ListConversationMessages(context.Context, conversation.ListCommand) ([]conversation.Message, error) {
	return nil, errors.New("not implemented")
}

type unavailableWorkCommands struct{}

func (unavailableWorkCommands) CreateWork(context.Context, work.CreateCommand) (work.Work, error) {
	return work.Work{}, errors.New("not implemented")
}

func (unavailableWorkCommands) AppendWorkMessage(context.Context, work.AppendMessageCommand) (work.Message, error) {
	return work.Message{}, errors.New("not implemented")
}

func (unavailableWorkCommands) RequestWorkRetry(context.Context, work.RetryCommand) error {
	return errors.New("not implemented")
}

func (unavailableWorkCommands) AcceptWorkReview(context.Context, work.AcceptReviewCommand) error {
	return errors.New("not implemented")
}

type unavailableWorkQueries struct{}

func (unavailableWorkQueries) ListWorks(context.Context, work.ListCommand) (work.Page, error) {
	return work.Page{}, errors.New("not implemented")
}

func (unavailableWorkQueries) LoadWork(context.Context, work.LoadCommand) (work.Details, error) {
	return work.Details{}, errors.New("not implemented")
}
