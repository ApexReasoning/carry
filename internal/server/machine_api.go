package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/go-chi/chi/v5"
)

// MachineRuntimeStore handles only Machine-authenticated runtime reporting and
// status; it cannot use member enrollment authority.
type MachineRuntimeStore interface {
	ReplaceRuntimeObservations(context.Context, string, []host.RuntimeObservation) error
	LoadMachineStatus(context.Context, string) (host.MachineStatus, error)
}

// MachineRunStore claims generic pending Runs without selecting an Agent.
type MachineRunStore interface {
	ClaimCoordinatorRun(context.Context, string) (run.Claim, error)
	RenewRunAttempt(context.Context, string, string, string, int64) (time.Time, error)
}

type machineAPI struct {
	runtimeStore MachineRuntimeStore
	runStore     MachineRunStore
}

type machineContextKey struct{}

type runtimeReportRequest struct {
	Runtimes []runtimeObservationWire `json:"runtimes"`
}

type renewRunAttemptRequest struct {
	Fence int64 `json:"fence"`
}

type runtimeObservationWire struct {
	Kind             host.RuntimeKind      `json:"kind"`
	Detection        host.RuntimeDetection `json:"detection"`
	Executable       string                `json:"executable,omitempty"`
	Version          string                `json:"version,omitempty"`
	DiagnosticCode   string                `json:"diagnostic_code,omitempty"`
	DiagnosticDetail string                `json:"diagnostic_detail,omitempty"`
	ObservedAt       time.Time             `json:"observed_at"`
}

func requireMachine(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// A member bearer is never accepted as an additional or fallback Machine
		// authority on the Host-only surface.
		_, browserCookieErr := request.Cookie(browserSessionCookie)
		if strings.TrimSpace(request.Header.Get("Authorization")) != "" || browserCookieErr == nil {
			writeAPIError(response, http.StatusUnauthorized, "Machine route does not accept member authentication")
			return
		}
		if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.PeerCertificates) == 0 {
			writeAPIError(response, http.StatusUnauthorized, "Machine certificate is required")
			return
		}
		machineID, err := host.MachineIDFromCertificate(request.TLS.PeerCertificates[0])
		if err != nil {
			writeAPIError(response, http.StatusUnauthorized, "Machine certificate is invalid")
			return
		}
		ctx := context.WithValue(request.Context(), machineContextKey{}, machineID)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func currentMachine(response http.ResponseWriter, request *http.Request) (string, bool) {
	machineID, ok := request.Context().Value(machineContextKey{}).(string)
	if !ok || machineID == "" {
		writeAPIError(response, http.StatusInternalServerError, "Machine authentication context is missing")
		return "", false
	}
	return machineID, true
}

func (api machineAPI) reportRuntimes(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	var body runtimeReportRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	observations := make([]host.RuntimeObservation, 0, len(body.Runtimes))
	for _, runtime := range body.Runtimes {
		observations = append(observations, host.RuntimeObservation{
			Kind: runtime.Kind, Detection: runtime.Detection, Executable: runtime.Executable,
			Version: runtime.Version, DiagnosticCode: runtime.DiagnosticCode,
			DiagnosticDetail: runtime.DiagnosticDetail, ObservedAt: runtime.ObservedAt,
		})
	}
	if err := api.runtimeStore.ReplaceRuntimeObservations(request.Context(), machineID, observations); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "reported"})
}

func (api machineAPI) status(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	status, err := api.runtimeStore.LoadMachineStatus(request.Context(), machineID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if status.RevokedAt != nil {
		writeAPIError(response, http.StatusForbidden, "Machine is revoked")
		return
	}
	runtimes := make([]runtimeObservationWire, 0, len(status.Runtimes))
	for _, runtime := range status.Runtimes {
		runtimes = append(runtimes, runtimeObservationWire{
			Kind: runtime.Kind, Detection: runtime.Detection, Executable: runtime.Executable,
			Version: runtime.Version, DiagnosticCode: runtime.DiagnosticCode,
			DiagnosticDetail: runtime.DiagnosticDetail, ObservedAt: runtime.ObservedAt,
		})
	}
	writeJSON(response, http.StatusOK, struct {
		MachineID   string                   `json:"machine_id"`
		SpaceID     string                   `json:"space_id"`
		DisplayName string                   `json:"display_name"`
		EnrolledAt  time.Time                `json:"enrolled_at"`
		Runtimes    []runtimeObservationWire `json:"runtimes"`
	}{
		MachineID: status.MachineID, SpaceID: status.SpaceID, DisplayName: status.DisplayName,
		EnrolledAt: status.EnrolledAt, Runtimes: runtimes,
	})
}

func (api machineAPI) renewRun(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	var body renewRunAttemptRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	leaseExpiresAt, err := api.runStore.RenewRunAttempt(
		request.Context(),
		machineID,
		chi.URLParam(request, "run_id"),
		chi.URLParam(request, "attempt_id"),
		body.Fence,
	)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}{LeaseExpiresAt: leaseExpiresAt})
}

func (api machineAPI) claimRun(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	claim, err := api.runStore.ClaimCoordinatorRun(request.Context(), machineID)
	if errors.Is(err, run.ErrNoPendingRun) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, struct {
		RunID           string    `json:"run_id"`
		AttemptID       string    `json:"attempt_id"`
		Fence           int64     `json:"fence"`
		BaseRevision    int64     `json:"base_revision"`
		InputEndSeq     int64     `json:"input_end_seq"`
		WriterToken     string    `json:"writer_token"`
		AgentCredential string    `json:"agent_credential"`
		LeaseExpiresAt  time.Time `json:"lease_expires_at"`
	}{
		RunID: claim.RunID, AttemptID: claim.AttemptID, Fence: claim.Fence,
		BaseRevision: claim.BaseRevision, InputEndSeq: claim.InputEndSeq,
		WriterToken: claim.WriterToken, AgentCredential: claim.AgentCredential,
		LeaseExpiresAt: claim.LeaseExpiresAt,
	})
}
