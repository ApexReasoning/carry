package host

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/machinefile"
)

type machineHTTP struct {
	client    *http.Client
	serverURL string
}

type runtimeWire struct {
	Kind             hostdomain.RuntimeKind      `json:"kind"`
	Detection        hostdomain.RuntimeDetection `json:"detection"`
	Executable       string                      `json:"executable,omitempty"`
	Version          string                      `json:"version,omitempty"`
	DiagnosticCode   string                      `json:"diagnostic_code,omitempty"`
	DiagnosticDetail string                      `json:"diagnostic_detail,omitempty"`
	ObservedAt       time.Time                   `json:"observed_at"`
}

type machineStatusWire struct {
	MachineID   string        `json:"machine_id"`
	SpaceID     string        `json:"space_id"`
	DisplayName string        `json:"display_name"`
	EnrolledAt  time.Time     `json:"enrolled_at"`
	Runtimes    []runtimeWire `json:"runtimes"`
}

func connectMachine(credential machinefile.Credential) (*machineHTTP, error) {
	serverURL, err := parseServerURL(credential.ServerURL)
	if err != nil {
		return nil, err
	}
	certificate, err := tls.X509KeyPair([]byte(credential.CertificatePEM), []byte(credential.PrivateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("load Machine key pair: %w", err)
	}
	client, err := newTLSClient(credential.CACertificatePEM, &certificate)
	if err != nil {
		return nil, err
	}
	return &machineHTTP{client: client, serverURL: serverURL}, nil
}

func (c *machineHTTP) reportRuntimes(ctx context.Context, observations []hostdomain.RuntimeObservation) error {
	body := struct {
		Runtimes []runtimeWire `json:"runtimes"`
	}{Runtimes: make([]runtimeWire, 0, len(observations))}
	for _, observation := range observations {
		body.Runtimes = append(body.Runtimes, runtimeWire{
			Kind: observation.Kind, Detection: observation.Detection,
			Executable: observation.Executable, Version: observation.Version,
			DiagnosticCode: observation.DiagnosticCode, DiagnosticDetail: observation.DiagnosticDetail,
			ObservedAt: observation.ObservedAt,
		})
	}
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/host/runtime-report", body)
	if err != nil {
		return err
	}
	return sendJSON(c.client, request, nil)
}

func (c *machineHTTP) loadStatus(ctx context.Context) (hostdomain.MachineStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serverURL+"/v1/host/status", nil)
	if err != nil {
		return hostdomain.MachineStatus{}, err
	}
	var wire machineStatusWire
	if err := sendJSON(c.client, request, &wire); err != nil {
		return hostdomain.MachineStatus{}, err
	}
	status := hostdomain.MachineStatus{
		MachineID: wire.MachineID, SpaceID: wire.SpaceID,
		DisplayName: wire.DisplayName, EnrolledAt: wire.EnrolledAt,
		Runtimes: make([]hostdomain.RuntimeObservation, 0, len(wire.Runtimes)),
	}
	for _, runtime := range wire.Runtimes {
		status.Runtimes = append(status.Runtimes, hostdomain.RuntimeObservation{
			Kind: runtime.Kind, Detection: runtime.Detection, Executable: runtime.Executable,
			Version: runtime.Version, DiagnosticCode: runtime.DiagnosticCode,
			DiagnosticDetail: runtime.DiagnosticDetail, ObservedAt: runtime.ObservedAt,
		})
	}
	return status, nil
}
