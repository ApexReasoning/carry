package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type recordingMachineConnections struct {
	unavailableMachineConnections
	begin      machine.BeginConnectionRequest
	decision   machine.DecideConnectionRequest
	selfID     string
	selfSerial string
	selfKey    string
	page       machine.MachinePage
	agents     []agent.InventoryRecord
}

func (store *recordingMachineConnections) Begin(_ context.Context, request machine.BeginConnectionRequest) (machine.BegunConnection, error) {
	store.begin = request
	return machine.BegunConnection{
		RequestID: request.RequestID, DisplayName: request.DisplayName, UserCode: request.UserCode,
		PollSecret:      request.PollSecret,
		Fingerprint:     "SHA256:exact",
		VerificationURL: "https://carry.example/machine-connect",
		ExpiresAt:       time.Date(2026, time.August, 21, 8, 15, 0, 0, time.UTC),
		PollInterval:    5 * time.Second,
	}, nil
}

func (store *recordingMachineConnections) Approve(_ context.Context, request machine.DecideConnectionRequest) error {
	store.decision = request
	return nil
}

func (store *recordingMachineConnections) List(context.Context, string, string, string) (machine.MachinePage, []agent.InventoryRecord, error) {
	return store.page, append([]agent.InventoryRecord(nil), store.agents...), nil
}

func (store *recordingMachineConnections) RevokeFromHost(_ context.Context, machineID, serial, key string) (machine.MachineRecord, error) {
	store.selfID = machineID
	store.selfSerial = serial
	store.selfKey = key
	now := time.Date(2026, time.August, 21, 8, 5, 0, 0, time.UTC)
	return machine.MachineRecord{
		MachineID:       machineID,
		State:           "Revoked",
		RevocationActor: "machine",
		RevokedAt:       &now,
	}, nil
}

func TestMachineConnectionBeginUsesSeparatePublicCeremonyWithoutBrowserCredential(t *testing.T) {
	t.Parallel()
	connections := &recordingMachineConnections{}
	requestID := uuid.NewString()
	request := httptest.NewRequest(http.MethodPost, "/v1/machine-connections", bytes.NewBufferString(`{
		"request_id":"`+requestID+`","display_name":"Desk Mac","user_code":"BCDF-GHJ-KLM",
		"poll_secret":"poll-only","public_key":"cHVibGlj","key_proof":"cHJvb2Y="
	}`))
	request.Host = "carry.example"
	request.Header.Set("Idempotency-Key", "begin-key")
	response := httptest.NewRecorder()
	testAPI(t, testAuthority(t), &recordingCLICredentials{}, connections, &recordingMachineRuns{}).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if connections.begin.RequestID != requestID || connections.begin.IdempotencyKey != "begin-key" ||
		connections.begin.Origin != "https://carry.example" || connections.begin.Source == "" {
		t.Fatalf("begin request = %#v", connections.begin)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"verification_url":"https://carry.example/machine-connect"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"interval_seconds":5`)) {
		t.Fatalf("begin body = %s", response.Body.String())
	}
	assertNoStore(t, response)
}

func TestMachineApprovalRequiresSameOriginBrowserSession(t *testing.T) {
	t.Parallel()
	connections := &recordingMachineConnections{}
	credentials := testIdentityCredentials(t)
	sessionID, requestID, spaceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	cookie, err := credentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/machine-connections/"+requestID+"/approve", bytes.NewBufferString(`{
		"request_id":"`+requestID+`","user_code":"BCDF-GHJ-KLM","space_id":"`+spaceID+`"
	}`))
	request.Host = "carry.example"
	request.Header.Set("Origin", "https://carry.example")
	request.Header.Set("Idempotency-Key", "approve-key")
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: cookie})
	response := httptest.NewRecorder()
	machineBrowserRoutes(t, connections, credentials).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if connections.decision.BrowserSessionID != sessionID || connections.decision.SpaceID != spaceID || connections.decision.IdempotencyKey != "approve-key" {
		t.Fatalf("approval = %#v", connections.decision)
	}

	crossOrigin := httptest.NewRequest(http.MethodPost, "/v1/machine-connections/"+requestID+"/approve", bytes.NewBufferString(`{}`))
	crossOrigin.Host = "carry.example"
	crossOrigin.Header.Set("Origin", "https://evil.example")
	crossOrigin.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: cookie})
	crossResponse := httptest.NewRecorder()
	machineBrowserRoutes(t, connections, credentials).ServeHTTP(crossResponse, crossOrigin)
	if crossResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", crossResponse.Code)
	}
}

func TestMachineInventoryGroupsSeparateAgentOwnerRecordsOnlyAtWire(t *testing.T) {
	t.Parallel()
	credentials := testIdentityCredentials(t)
	sessionID, spaceID, machineID, agentID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	cookie, err := credentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	lastActive := time.Date(2026, time.August, 21, 8, 4, 0, 0, time.UTC)
	connections := &recordingMachineConnections{
		page: machine.MachinePage{Machines: []machine.MachineRecord{{
			MachineID:        machineID,
			SpaceID:          spaceID,
			SpaceName:        "Research",
			DisplayName:      "Desk Host",
			Fingerprint:      "SHA256:exact",
			State:            "Active",
			EnrolledByUserID: ownerID,
			EnrolledByName:   "Ada",
			EnrolledAt:       lastActive,
			CanRevoke:        true,
		}}},
		agents: []agent.InventoryRecord{{
			AgentID:      agentID,
			MachineID:    machineID,
			Name:         "Pi",
			AvatarIndex:  3,
			OwnerUserID:  ownerID,
			OwnerName:    "Ada",
			Lifecycle:    agent.LifecycleActive,
			Online:       true,
			LastActiveAt: &lastActive,
		}},
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/machines", nil)
	request.AddCookie(&http.Cookie{Name: browserSessionCookie,
		Value: cookie})
	response := httptest.NewRecorder()
	machineBrowserRoutes(t, connections, credentials).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"agents":[{"agent_id":"`+agentID+`","name":"Pi","avatar_index":3,"owner_user_id":"`+ownerID+`","owner_name":"Ada","state":"active","online":true,"last_active_at":"2026-08-21T08:04:00Z"}]`)) {
		t.Fatalf("grouped Machine Agent inventory = %d %s", response.Code, response.Body.String())
	}
}

func machineBrowserRoutes(t *testing.T, connections MachineConnections, credentials identity.Credentials) http.Handler {
	t.Helper()
	routes, err := NewUserMachineRoutes(connections, credentials, testExternalOrigin(t), NewRequestSource(nil))
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Route("/v1", routes.mountBrowser)
	return router
}

func TestMachineSelfRevocationUsesExactCertificateIdentityAndSerial(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, uuid.NewString())
	machineID, err := machine.MachineIDFromCertificate(certificate)
	if err != nil {
		t.Fatal(err)
	}
	connections := &recordingMachineConnections{}
	request := httptest.NewRequest(http.MethodPost, "/v1/host/machine/revoke", nil)
	request.TLS = verifiedMachineTLS(certificate)
	request.Header.Set("Idempotency-Key", "disconnect-key")
	response := httptest.NewRecorder()
	testAPI(t, authority, &recordingCLICredentials{}, connections, &recordingMachineRuns{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || connections.selfID != machineID || connections.selfSerial != certificate.SerialNumber.String() || connections.selfKey != "disconnect-key" ||
		bytes.Contains(response.Body.Bytes(), []byte(`"agents"`)) || bytes.Contains(response.Body.Bytes(), []byte(`"owner_name"`)) {
		t.Fatalf("self revoke status=%d identity=%q serial=%q key=%q body=%s", response.Code, connections.selfID, connections.selfSerial, connections.selfKey, response.Body.String())
	}
	assertNoStore(t, response)
}
