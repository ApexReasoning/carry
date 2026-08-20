package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
)

func TestEnrollMachineUsesAuthenticatedMemberAuthority(t *testing.T) {
	t.Parallel()
	const (
		memberID  = "76fa247e-e9ef-4036-ac5d-87463cabb2ff"
		spaceID   = "a30f0a9a-8cb2-4ae4-9a7e-ae85e207788a"
		machineID = "38a0e783-2f61-4de4-a264-91fe1c099893"
	)

	authority := testAuthority(t)
	tokens := &recordingUserTokens{user: identity.AuthenticatedUser{UserID: memberID}}
	machines := &recordingMachineEnrollments{enrollment: machine.MachineEnrollment{
		MachineID: machineID, SpaceID: spaceID, CertificatePEM: []byte("machine-certificate"),
	}}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Machine key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal Machine public key: %v", err)
	}
	body := fmt.Sprintf(
		`{"space_id":"`+spaceID+`","display_name":"research-mac","public_key":%q}`,
		base64.StdEncoding.EncodeToString(publicKeyDER),
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/machines/enroll", bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer member-token")
	request.Header.Set("Idempotency-Key", "enroll-research-mac")
	response := httptest.NewRecorder()

	testAPI(t, authority, tokens, machines, &recordingMachineRuns{}).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if tokens.authenticatedToken != "member-token" {
		t.Fatalf("authenticated token = %q", tokens.authenticatedToken)
	}
	if machines.command.EnrolledByUserID != memberID || machines.command.SpaceID != spaceID {
		t.Fatalf("enrollment command = %#v", machines.command)
	}
	if machines.command.IdempotencyKey != "enroll-research-mac" {
		t.Fatalf("idempotency key = %q", machines.command.IdempotencyKey)
	}
}

func TestEnrollMachineRejectsMissingMemberToken(t *testing.T) {
	t.Parallel()

	machines := &recordingMachineEnrollments{}
	request := httptest.NewRequest(http.MethodPost, "/v1/machines/enroll", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()

	testAPI(t, testAuthority(t), &recordingUserTokens{}, machines, &recordingMachineRuns{}).
		ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if machines.command.MachineID != "" {
		t.Fatal("unauthenticated request reached enrollment")
	}
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

	testAPI(t, authority, &recordingUserTokens{}, &recordingMachineEnrollments{}, runs).
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

	testAPI(t, authority, &recordingUserTokens{}, &recordingMachineEnrollments{}, runs).
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
	testAPI(t, authority, &recordingUserTokens{}, &recordingMachineEnrollments{}, runs).
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
	tokens UserTokenAuthenticator,
	machines machine.EnrollmentPersistence,
	runs *recordingMachineRuns,
) http.Handler {
	t.Helper()
	user := testUserRoutes(t, authority)
	authentication, err := NewUserAuthentication(tokens, unavailableBrowserSessions{}, testIdentityCredentials(t))
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	userMachines, err := NewUserMachineRoutes(
		testMachineEnrollment(t, machines, authority),
		machines.(MachineRevocation),
	)
	if err != nil {
		t.Fatalf("compose User Machine routes: %v", err)
	}
	user.authentication = authentication
	user.machines = userMachines
	machine, err := NewMachineRoutes(runs, unavailableMachineConversations{})
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

type recordingUserTokens struct {
	user               identity.AuthenticatedUser
	authenticatedToken string
}

func (store *recordingUserTokens) AuthenticateUserToken(_ context.Context, token string) (identity.AuthenticatedUser, error) {
	store.authenticatedToken = token
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

func testUserRoutes(t *testing.T, authority *machine.CertificateAuthority) *UserRoutes {
	t.Helper()
	credentials := testIdentityCredentials(t)
	sessions := unavailableBrowserSessions{}
	emailLogin, err := identity.NewEmailLogin(unavailableEmailLogins{}, unavailableEmailSender{}, credentials)
	if err != nil {
		t.Fatalf("compose test email login: %v", err)
	}
	firstSpace, err := space.NewFirstSpace(unavailableFirstSpaces{})
	if err != nil {
		t.Fatalf("compose test first Space: %v", err)
	}
	machines := &recordingMachineEnrollments{}
	authentication, err := NewUserAuthentication(&recordingUserTokens{}, sessions, credentials)
	if err != nil {
		t.Fatalf("compose test User authentication: %v", err)
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
		t.Fatalf("compose test User identity routes: %v", err)
	}
	spaceRoutes, err := NewUserSpaceRoutes(firstSpace)
	if err != nil {
		t.Fatalf("compose test User Space routes: %v", err)
	}
	machineRoutes, err := NewUserMachineRoutes(testMachineEnrollment(t, machines, authority), machines)
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

func testMachineEnrollment(t *testing.T, persistence machine.EnrollmentPersistence, authority *machine.CertificateAuthority) *machine.Enrollment {
	t.Helper()
	enrollment, err := machine.NewEnrollment(persistence, authority)
	if err != nil {
		t.Fatalf("compose test Machine enrollment: %v", err)
	}
	return enrollment
}

type unavailableExternalLogin struct{}

func (unavailableExternalLogin) StartGoogle(context.Context) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) StartGitHub(context.Context) (identity.ExternalLoginStart, error) {
	return identity.ExternalLoginStart{}, errors.New("not implemented")
}

func (unavailableExternalLogin) CompleteGoogle(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("not implemented")
}

func (unavailableExternalLogin) CompleteGitHub(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error) {
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

type unavailableFirstSpaces struct{}

func (unavailableFirstSpaces) CreateFirstSpace(context.Context, space.CreateFirstCommand) (space.CreatedSpace, error) {
	return space.CreatedSpace{}, errors.New("not implemented")
}

type emptyMemberships struct{}

func (emptyMemberships) ListMemberships(context.Context, string) ([]space.Membership, error) {
	return nil, nil
}

type recordingMachineEnrollments struct {
	enrollment machine.MachineEnrollment
	command    machine.EnrollMachineCommand
}

func (store *recordingMachineEnrollments) EnrollMachine(_ context.Context, command machine.EnrollMachineCommand) (machine.MachineEnrollment, error) {
	store.command = command
	return store.enrollment, nil
}

func (*recordingMachineEnrollments) RevokeMachine(context.Context, string, string, string) error {
	return nil
}

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
