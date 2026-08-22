package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/google/uuid"
)

type recordingMachineAgentReports struct {
	request machine.AgentReportRequest
	result  machine.AgentReportResult
	err     error
}

func (reports *recordingMachineAgentReports) Report(_ context.Context, request machine.AgentReportRequest) (machine.AgentReportResult, error) {
	reports.request = request
	return reports.result, reports.err
}

func TestMachineAgentReportUsesExactMTLSPrincipalAndStrictWire(t *testing.T) {
	t.Parallel()
	machineID := uuid.NewString()
	authority, certificate := testMachineCertificate(t, machineID)
	reports := &recordingMachineAgentReports{result: machine.AgentReportResult{
		Revision:                 3,
		UnsupportedAdapterKeys:   []agent.AdapterKey{"future"},
		SetupRequiredAdapterKeys: []agent.AdapterKey{"pi"},
	}}
	handler := machineAgentTestAPI(t, authority, reports)
	request := httptest.NewRequest(http.MethodPost, "/v1/host/agents/observations", strings.NewReader(`{
		"report_id":"11111111-1111-4111-8111-111111111111","base_revision":2,
		"observations":[{"adapter_key":"pi","occurrence_key":"default","present":false}]
	}`))
	request.TLS = verifiedMachineTLS(certificate)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"revision\":3,\"unsupported_adapter_keys\":[\"future\"],\"setup_required_adapter_keys\":[\"pi\"]}\n" {
		t.Fatalf("report response = %d %s", response.Code, response.Body.String())
	}
	if reports.request.MachineID != machineID || reports.request.CertificateSerial != certificate.SerialNumber.String() ||
		len(reports.request.Observations) != 1 || reports.request.Observations[0].Present {
		t.Fatalf("report command = %#v", reports.request)
	}
	assertNoStore(t, response)

	for name, body := range map[string]string{
		"required base revision": `{"report_id":"11111111-1111-4111-8111-111111111111","observations":[]}`,
		"required observations":  `{"report_id":"11111111-1111-4111-8111-111111111111","base_revision":0}`,
		"required present":       `{"report_id":"11111111-1111-4111-8111-111111111111","base_revision":0,"observations":[{"adapter_key":"pi","occurrence_key":"default"}]}`,
		"unknown field":          `{"report_id":"11111111-1111-4111-8111-111111111111","base_revision":0,"observations":[],"machine_id":"forbidden"}`,
		"trailing JSON":          `{"report_id":"11111111-1111-4111-8111-111111111111","base_revision":0,"observations":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			reports.request = machine.AgentReportRequest{}
			request := httptest.NewRequest(http.MethodPost, "/v1/host/agents/observations", strings.NewReader(body))
			request.TLS = verifiedMachineTLS(certificate)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || reports.request.MachineID != "" {
				t.Fatalf("strict response = %d %s, command=%#v", response.Code, response.Body.String(), reports.request)
			}
		})
	}
}

func TestMachineAgentReportMapsOnlyFrozenRecoveries(t *testing.T) {
	t.Parallel()
	machineID := uuid.NewString()
	authority, certificate := testMachineCertificate(t, machineID)
	cases := []struct {
		name, body string
		err        error
		status     int
	}{
		{name: "invalid",
			err:    machine.ErrInvalidAgentReport,
			status: http.StatusBadRequest,
			body:   `{"code":"invalid_report"}`},
		{name: "revoked",
			err:    machine.ErrMachineRevoked,
			status: http.StatusForbidden,
			body:   `{"code":"machine_revoked"}`},
		{name: "unavailable",
			err:    machine.ErrMachineUnavailable,
			status: http.StatusNotFound,
			body:   `{"code":"machine_unavailable"}`},
		{name: "stale",
			err:    machine.AgentReportStaleError{CurrentRevision: 8},
			status: http.StatusConflict,
			body:   `{"code":"stale_report","current_revision":8}`},
		{name: "conflict",
			err:    machine.ErrAgentReportConflict,
			status: http.StatusConflict,
			body:   `{"code":"report_conflict"}`},
		{name: "known no write",
			err:    machine.ErrAgentReportTemporarilyUnavailable,
			status: http.StatusServiceUnavailable,
			body:   `{"code":"temporarily_unavailable"}`},
		{name: "unknown",
			err:    errors.New("commit outcome lost"),
			status: http.StatusInternalServerError,
			body:   `{"code":"unknown_outcome"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			reports := &recordingMachineAgentReports{err: testCase.err}
			request := httptest.NewRequest(http.MethodPost, "/v1/host/agents/observations", bytes.NewBufferString(`{"report_id":"11111111-1111-4111-8111-111111111111","base_revision":0,"observations":[]}`))
			request.TLS = verifiedMachineTLS(certificate)
			response := httptest.NewRecorder()
			machineAgentTestAPI(t, authority, reports).ServeHTTP(response, request)
			if response.Code != testCase.status || strings.TrimSpace(response.Body.String()) != testCase.body {
				t.Fatalf("recovery response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMachineAgentReportRejectsMixedUserPrincipal(t *testing.T) {
	t.Parallel()
	machineID := uuid.NewString()
	authority, certificate := testMachineCertificate(t, machineID)
	reports := &recordingMachineAgentReports{}
	request := httptest.NewRequest(http.MethodPost, "/v1/host/agents/observations", strings.NewReader(`{"report_id":"11111111-1111-4111-8111-111111111111","base_revision":0,"observations":[]}`))
	request.TLS = verifiedMachineTLS(certificate)
	request.Header.Set("Authorization", "Bearer member-proof")
	response := httptest.NewRecorder()
	machineAgentTestAPI(t, authority, reports).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || reports.request.MachineID != "" {
		t.Fatalf("mixed principal response = %d %s", response.Code, response.Body.String())
	}
}

func machineAgentTestAPI(t *testing.T, authority *machine.CertificateAuthority, reports MachineAgentReports) http.Handler {
	t.Helper()
	user := testUserRoutes(t, authority)
	routes, err := NewMachineRoutes(&recordingMachineRuns{}, unavailableMachineConversations{}, unavailableMachineConnections{}, reports)
	if err != nil {
		t.Fatalf("compose Machine Agent routes: %v", err)
	}
	return mustAPI(t, user, routes)
}
