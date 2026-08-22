package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
)

// MachineAgentReports is the Machine consumer's complete Agent observation behavior.
type MachineAgentReports interface {
	Report(context.Context, machine.AgentReportRequest) (machine.AgentReportResult, error)
}

type machineAgentAPI struct {
	reports MachineAgentReports
}

type agentObservationRequest struct {
	AdapterKey    agent.AdapterKey    `json:"adapter_key"`
	OccurrenceKey agent.OccurrenceKey `json:"occurrence_key"`
	Present       *bool               `json:"present"`
}

type agentReportRequest struct {
	ReportID     string                     `json:"report_id"`
	BaseRevision *int64                     `json:"base_revision"`
	Observations *[]agentObservationRequest `json:"observations"`
}

type agentReportResponse struct {
	Revision                 int64              `json:"revision"`
	UnsupportedAdapterKeys   []agent.AdapterKey `json:"unsupported_adapter_keys"`
	SetupRequiredAdapterKeys []agent.AdapterKey `json:"setup_required_adapter_keys"`
}

func (api machineAgentAPI) report(response http.ResponseWriter, request *http.Request) {
	principal, ok := currentMachinePrincipal(response, request)
	if !ok {
		return
	}
	var body agentReportRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if body.BaseRevision == nil || body.Observations == nil {
		writeAgentReportError(response, machine.ErrInvalidAgentReport)
		return
	}
	observations := make([]machine.AgentObservation, 0, len(*body.Observations))
	for _, observation := range *body.Observations {
		if observation.Present == nil {
			writeAgentReportError(response, machine.ErrInvalidAgentReport)
			return
		}
		observations = append(observations, machine.AgentObservation{
			AdapterKey:    observation.AdapterKey,
			OccurrenceKey: observation.OccurrenceKey,
			Present:       *observation.Present,
		})
	}
	result, err := api.reports.Report(request.Context(), machine.AgentReportRequest{
		MachineID:         principal.machineID,
		CertificateSerial: principal.certificateSerial,
		ReportID:          body.ReportID,
		BaseRevision:      *body.BaseRevision,
		Observations:      observations,
	})
	if err != nil {
		writeAgentReportError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, agentReportResponse{
		Revision: result.Revision,
		UnsupportedAdapterKeys: append(
			make([]agent.AdapterKey, 0, len(result.UnsupportedAdapterKeys)),
			result.UnsupportedAdapterKeys...,
		),
		SetupRequiredAdapterKeys: append(
			make([]agent.AdapterKey, 0, len(result.SetupRequiredAdapterKeys)),
			result.SetupRequiredAdapterKeys...,
		),
	})
}

func writeAgentReportError(response http.ResponseWriter, err error) {
	noStore(response)
	var stale machine.AgentReportStaleError
	switch {
	case errors.Is(err, machine.ErrInvalidAgentReport):
		writeJSON(response, http.StatusBadRequest, struct {
			Code string `json:"code"`
		}{Code: "invalid_report"})
	case errors.Is(err, machine.ErrMachineRevoked):
		writeJSON(response, http.StatusForbidden, struct {
			Code string `json:"code"`
		}{Code: "machine_revoked"})
	case errors.Is(err, machine.ErrMachineUnavailable):
		writeJSON(response, http.StatusNotFound, struct {
			Code string `json:"code"`
		}{Code: "machine_unavailable"})
	case errors.As(err, &stale):
		writeJSON(response, http.StatusConflict, struct {
			Code            string `json:"code"`
			CurrentRevision int64  `json:"current_revision"`
		}{Code: "stale_report",
			CurrentRevision: stale.CurrentRevision})
	case errors.Is(err, machine.ErrAgentReportConflict):
		writeJSON(response, http.StatusConflict, struct {
			Code string `json:"code"`
		}{Code: "report_conflict"})
	case errors.Is(err, machine.ErrAgentReportTemporarilyUnavailable):
		writeJSON(response, http.StatusServiceUnavailable, struct {
			Code string `json:"code"`
		}{Code: "temporarily_unavailable"})
	default:
		writeJSON(response, http.StatusInternalServerError, struct {
			Code string `json:"code"`
		}{Code: "unknown_outcome"})
	}
}
