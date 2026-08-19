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

	testAPI(t, authority, tokens, machines, &recordingMachineRuntime{}).ServeHTTP(response, request)

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

	testAPI(t, testAuthority(t), &recordingUserTokens{}, machines, &recordingMachineRuntime{}).
		ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if machines.command.MachineID != "" {
		t.Fatal("unauthenticated request reached enrollment")
	}
}

func TestRuntimeReportRequiresActiveMachineCertificate(t *testing.T) {
	t.Parallel()

	authority, machineCertificate := testMachineCertificate(t, "machine-7")
	runtimeStore := &recordingMachineRuntime{reportErr: host.ErrMachineRevoked}
	body := `{"runtimes":[{"kind":"pi","detection":"detected","executable":"/bin/pi","version":"1","observed_at":"2026-08-18T16:00:00Z"},{"kind":"codex","detection":"not_found","observed_at":"2026-08-18T16:00:00Z"}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/host/runtime-report", bytes.NewBufferString(body))
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{machineCertificate},
		VerifiedChains:   [][]*x509.Certificate{{machineCertificate}},
	}
	response := httptest.NewRecorder()

	testAPI(t, authority, &recordingUserTokens{}, &recordingMachineEnrollments{}, runtimeStore).
		ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if runtimeStore.reportMachineID != "machine-7" {
		t.Fatalf("report Machine = %q", runtimeStore.reportMachineID)
	}
}

func testAPI(
	t *testing.T,
	authority *host.CertificateAuthority,
	tokens UserTokenAuthenticator,
	machines MachineEnrollmentStore,
	runtimes MachineRuntimeStore,
) http.Handler {
	t.Helper()
	sessions := unavailableBrowserSessions{}
	member, err := NewMemberRoutes(
		tokens, sessions, emptyMemberships{}, machines,
		unavailableWorkCommands{}, unavailableWorkQueries{}, authority,
	)
	if err != nil {
		t.Fatalf("compose member routes: %v", err)
	}
	machine, err := NewMachineRoutes(runtimes)
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

func (s *recordingUserTokens) AuthenticateUserToken(_ context.Context, token string) (identity.AuthenticatedUser, error) {
	s.authenticatedToken = token
	if s.user.UserID == "" {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	return s.user, nil
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

func (s *recordingMachineEnrollments) EnrollMachine(_ context.Context, command host.EnrollMachineCommand) (host.MachineEnrollment, error) {
	s.command = command
	return s.enrollment, nil
}

func (*recordingMachineEnrollments) RevokeMachine(context.Context, string, string, string) error {
	return nil
}

type recordingMachineRuntime struct {
	reportMachineID string
	reportErr       error
}

func (s *recordingMachineRuntime) ReplaceRuntimeObservations(_ context.Context, machineID string, _ []host.RuntimeObservation) error {
	s.reportMachineID = machineID
	return s.reportErr
}

func (*recordingMachineRuntime) LoadMachineStatus(context.Context, string) (host.MachineStatus, error) {
	return host.MachineStatus{}, errors.New("not implemented")
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
