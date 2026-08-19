package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
)

func TestEnrollMachineUsesAuthenticatedMemberAuthority(t *testing.T) {
	t.Parallel()

	authority := testAuthority(t)
	tokens := &recordingUserTokens{user: identity.AuthenticatedUser{UserID: "member-1"}}
	machines := &recordingMachineEnrollments{enrollment: host.MachineEnrollment{
		MachineID: "machine-1", SpaceID: "space-1", CertificatePEM: []byte("machine-certificate"),
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
		`{"space_id":"space-1","display_name":"research-mac","public_key":%q}`,
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
	if machines.command.EnrolledByUserID != "member-1" || machines.command.SpaceID != "space-1" {
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
		RunID: "run-4", AttemptID: "attempt-4", WorkID: "work-4", SpaceID: "space-1", Fence: 3,
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

	authority, machineCertificate := testMachineCertificate(t, "machine-19")
	runs := &recordingMachineRuns{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/host/runs/run-7/attempts/attempt-7/understanding",
		bytes.NewBufferString(`{"fence":4,"base_understanding_version":2,"input_end_seq":5,"understanding":"Known","next_step":"Continue"}`),
	)
	request.TLS = verifiedMachineTLS(machineCertificate)
	response := httptest.NewRecorder()

	testAPI(t, authority, &recordingUserTokens{}, &recordingMachineEnrollments{}, runs).
		ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if runs.commit.MachineID != "machine-19" || runs.commit.RunID != "run-7" ||
		runs.commit.BaseUnderstandingVersion != 2 {
		t.Fatalf("commit = %#v", runs.commit)
	}
}

func verifiedMachineTLS(certificate *x509.Certificate) *tls.ConnectionState {
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
}

func testAPI(
	t *testing.T,
	authority *host.CertificateAuthority,
	tokens UserTokenAuthenticator,
	machines MachineEnrollmentStore,
	runs MachineRunStore,
) http.Handler {
	t.Helper()
	member, err := NewMemberRoutes(
		tokens, unavailableBrowserSessions{}, emptyMemberships{}, machines,
		unavailableWorkCommands{}, unavailableWorkQueries{}, authority,
	)
	if err != nil {
		t.Fatalf("compose member routes: %v", err)
	}
	machine, err := NewMachineRoutes(runs)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

func mustAPI(t *testing.T, member *MemberRoutes, machine *MachineRoutes) http.Handler {
	t.Helper()
	return mustAPIWithReadiness(t, nil, member, machine)
}

func mustAPIWithReadiness(
	t *testing.T,
	readiness Readiness,
	member *MemberRoutes,
	machine *MachineRoutes,
) http.Handler {
	t.Helper()
	apiServer, err := NewAPI(readiness, member, machine)
	if err != nil {
		t.Fatalf("compose API: %v", err)
	}
	return apiServer.Handler()
}

func testAuthority(t *testing.T) *host.CertificateAuthority {
	t.Helper()
	authority, _ := testMachineCertificate(t, "authority-probe")
	return authority
}

func testMachineCertificate(t *testing.T, machineID string) (*host.CertificateAuthority, *x509.Certificate) {
	t.Helper()
	now := time.Date(2026, time.August, 18, 16, 0, 0, 0, time.UTC)
	bundle, err := host.CreatePKI([]string{"localhost"}, now)
	if err != nil {
		t.Fatalf("create PKI: %v", err)
	}
	authority, err := host.LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
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

func (unavailableBrowserSessions) CreateBrowserSession(context.Context, string, time.Time) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("not implemented")
}

func (unavailableBrowserSessions) AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error) {
	return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
}

func (unavailableBrowserSessions) RevokeBrowserSession(context.Context, string) error { return nil }

type emptyMemberships struct{}

func (emptyMemberships) ListMemberships(context.Context, string) ([]space.Membership, error) {
	return nil, nil
}

type recordingMachineEnrollments struct {
	enrollment host.MachineEnrollment
	command    host.EnrollMachineCommand
}

func (store *recordingMachineEnrollments) EnrollMachine(_ context.Context, command host.EnrollMachineCommand) (host.MachineEnrollment, error) {
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

func (*recordingMachineRuns) RenewRunAttempt(context.Context, string, string, string, int64) (time.Time, error) {
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

type unavailableWorkCommands struct{}

func (unavailableWorkCommands) CreateWork(context.Context, work.CreateCommand) (work.Work, error) {
	return work.Work{}, errors.New("not implemented")
}

func (unavailableWorkCommands) AppendWorkMessage(context.Context, work.AppendMessageCommand) (work.Message, error) {
	return work.Message{}, errors.New("not implemented")
}

type unavailableWorkQueries struct{}

func (unavailableWorkQueries) ListWorks(context.Context, string, string) ([]work.Work, error) {
	return nil, errors.New("not implemented")
}

func (unavailableWorkQueries) LoadWork(context.Context, string, string, string) (work.Details, error) {
	return work.Details{}, errors.New("not implemented")
}
