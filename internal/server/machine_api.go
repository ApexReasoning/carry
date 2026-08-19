package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
)

// MachineRuntimeStore handles only Machine-authenticated runtime reporting and
// status; it cannot use member enrollment authority.
type MachineRuntimeStore interface {
	ReplaceRuntimeObservations(context.Context, string, []host.RuntimeObservation) error
	LoadMachineStatus(context.Context, string) (host.MachineStatus, error)
}

type machineAPI struct {
	store MachineRuntimeStore
}

type machineContextKey struct{}

type runtimeReportRequest struct {
	Runtimes []runtimeObservationWire `json:"runtimes"`
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
	if err := api.store.ReplaceRuntimeObservations(request.Context(), machineID, observations); err != nil {
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
	status, err := api.store.LoadMachineStatus(request.Context(), machineID)
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
