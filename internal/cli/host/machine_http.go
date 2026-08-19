package host

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/machinefile"
	"github.com/ApexReasoning/carry/internal/run"
)

type machineHTTP struct {
	client      *http.Client
	agentClient *http.Client
	serverURL   string
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

type runClaimWire struct {
	RunID           string    `json:"run_id"`
	AttemptID       string    `json:"attempt_id"`
	Fence           int64     `json:"fence"`
	BaseRevision    int64     `json:"base_revision"`
	InputEndSeq     int64     `json:"input_end_seq"`
	WriterToken     string    `json:"writer_token"`
	AgentCredential string    `json:"agent_credential"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at"`
}

type attemptContextWire struct {
	RunID                string      `json:"run_id"`
	AttemptID            string      `json:"attempt_id"`
	WorkID               string      `json:"work_id"`
	SpaceID              string      `json:"space_id"`
	Goal                 string      `json:"goal"`
	CurrentUnderstanding string      `json:"current_understanding"`
	CurrentNextStep      string      `json:"current_next_step"`
	InputStartSeq        int64       `json:"input_start_seq"`
	InputEndSeq          int64       `json:"input_end_seq"`
	BaseRevision         int64       `json:"base_revision"`
	Fence                int64       `json:"fence"`
	Inputs               []run.Input `json:"inputs"`
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
	agentClient, err := newTLSClient(credential.CACertificatePEM, nil)
	if err != nil {
		return nil, err
	}
	return &machineHTTP{client: client, agentClient: agentClient, serverURL: serverURL}, nil
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

// Claim obtains generic Run authority with Machine mTLS and no adapter selection.
func (c *machineHTTP) Claim(ctx context.Context) (run.Claim, error) {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/host/runs/claim", struct{}{})
	if err != nil {
		return run.Claim{}, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return run.Claim{}, fmt.Errorf("claim coordinator Run: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return run.Claim{}, run.ErrNoPendingRun
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return run.Claim{}, fmt.Errorf(
			"POST %s returned %s: %s",
			request.URL,
			response.Status,
			strings.TrimSpace(string(limited)),
		)
	}
	var wire runClaimWire
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&wire); err != nil {
		return run.Claim{}, fmt.Errorf("decode Run claim: %w", err)
	}
	return run.Claim{
		Coordinator: run.Coordinator{
			RunID: wire.RunID, BaseRevision: wire.BaseRevision,
			InputEndSeq: wire.InputEndSeq, State: run.StateActive,
		},
		AttemptID: wire.AttemptID, Fence: wire.Fence, WriterToken: wire.WriterToken,
		AgentCredential: wire.AgentCredential, LeaseExpiresAt: wire.LeaseExpiresAt,
	}, nil
}

func (c *machineHTTP) Renew(ctx context.Context, claim run.Claim) (time.Time, error) {
	request, err := newJSONRequest(
		ctx,
		http.MethodPost,
		c.serverURL+attemptPath(claim)+"/renew",
		struct {
			Fence int64 `json:"fence"`
		}{Fence: claim.Fence},
	)
	if err != nil {
		return time.Time{}, err
	}
	var wire struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}
	if err := sendJSON(c.client, request, &wire); err != nil {
		return time.Time{}, err
	}
	return wire.LeaseExpiresAt, nil
}

func (c *machineHTTP) LoadContext(ctx context.Context, claim run.Claim) (run.Context, error) {
	requestURL := c.serverURL + "/v1/agent" + attemptPath(claim) + "/context?fence=" + strconv.FormatInt(claim.Fence, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return run.Context{}, fmt.Errorf("create Agent context request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+claim.AgentCredential)
	var wire attemptContextWire
	if err := sendJSON(c.agentClient, request, &wire); err != nil {
		return run.Context{}, err
	}
	return run.Context{
		RunID: wire.RunID, AttemptID: wire.AttemptID, WorkID: wire.WorkID, SpaceID: wire.SpaceID,
		Goal: wire.Goal, CurrentUnderstanding: wire.CurrentUnderstanding,
		CurrentNextStep: wire.CurrentNextStep, InputStartSeq: wire.InputStartSeq,
		InputEndSeq: wire.InputEndSeq, BaseRevision: wire.BaseRevision,
		Fence: wire.Fence, Inputs: wire.Inputs,
	}, nil
}

func (c *machineHTTP) Commit(ctx context.Context, claim run.Claim, draft hostdomain.Draft) error {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/agent"+attemptPath(claim)+"/revision", struct {
		Fence         int64  `json:"fence"`
		WriterToken   string `json:"writer_token"`
		BaseRevision  int64  `json:"base_revision"`
		InputEndSeq   int64  `json:"input_end_seq"`
		Understanding string `json:"understanding"`
		NextStep      string `json:"next_step"`
	}{
		Fence: claim.Fence, WriterToken: claim.WriterToken, BaseRevision: claim.BaseRevision,
		InputEndSeq: claim.InputEndSeq, Understanding: draft.Understanding, NextStep: draft.NextStep,
	})
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+claim.AgentCredential)
	return sendJSON(c.agentClient, request, nil)
}

func (c *machineHTTP) Finish(ctx context.Context, claim run.Claim, outcome run.State) error {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/agent"+attemptPath(claim)+"/outcome", struct {
		Fence       int64     `json:"fence"`
		WriterToken string    `json:"writer_token"`
		Outcome     run.State `json:"outcome"`
	}{Fence: claim.Fence, WriterToken: claim.WriterToken, Outcome: outcome})
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+claim.AgentCredential)
	return sendJSON(c.agentClient, request, nil)
}

func attemptPath(claim run.Claim) string {
	return "/runs/" + url.PathEscape(claim.RunID) + "/attempts/" + url.PathEscape(claim.AttemptID)
}

var _ hostdomain.RunClient = (*machineHTTP)(nil)
