package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/go-chi/chi/v5"
)

const machineConnectionPollHeader = "X-Carry-Machine-Connection"

type MachineConnections interface {
	Begin(context.Context, machine.BeginConnectionRequest) (machine.BegunConnection, error)
	Lookup(context.Context, machine.LookupConnectionRequest) (machine.ConnectionPreview, error)
	Approve(context.Context, machine.DecideConnectionRequest) error
	Deny(context.Context, machine.DecideConnectionRequest) error
	Poll(context.Context, string) (machine.ConnectedMachine, error)
	Cancel(context.Context, string) error
	List(context.Context, string, string, string) (machine.MachinePage, []agent.InventoryRecord, error)
	RevokeFromBrowser(context.Context, string, string, string, string) (machine.MachineRecord, []agent.InventoryRecord, error)
	RevokeFromHost(context.Context, string, string, string) (machine.MachineRecord, error)
}

type machineConnectionAPI struct {
	connections    MachineConnections
	credentials    identity.Credentials
	origin         ExternalOrigin
	requestSources RequestSource
}

type machineConnectionPreviewWire struct {
	RequestID       string    `json:"request_id"`
	UserCode        string    `json:"user_code"`
	DisplayName     string    `json:"display_name"`
	Fingerprint     string    `json:"fingerprint"`
	Server          string    `json:"server"`
	Decision        string    `json:"decision,omitempty"`
	ApprovedSpaceID string    `json:"approved_space_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type agentRecordWire struct {
	AgentID      string          `json:"agent_id"`
	Name         string          `json:"name"`
	AvatarIndex  int             `json:"avatar_index"`
	OwnerUserID  string          `json:"owner_user_id"`
	OwnerName    string          `json:"owner_name"`
	State        agent.Lifecycle `json:"state"`
	Online       bool            `json:"online"`
	LastActiveAt *time.Time      `json:"last_active_at"`
}

type machineRecordWire struct {
	MachineID        string     `json:"machine_id"`
	SpaceID          string     `json:"space_id"`
	SpaceName        string     `json:"space_name"`
	DisplayName      string     `json:"display_name"`
	Fingerprint      string     `json:"fingerprint"`
	State            string     `json:"state"`
	EnrolledByUserID string     `json:"enrolled_by_user_id"`
	EnrolledByName   string     `json:"enrolled_by_name"`
	EnrolledAt       time.Time  `json:"enrolled_at"`
	RevocationActor  string     `json:"revocation_actor,omitempty"`
	RevokedByUserID  string     `json:"revoked_by_user_id,omitempty"`
	RevokedByName    string     `json:"revoked_by_name,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	CanRevoke        bool       `json:"can_revoke"`
}

type machineInventoryRecordWire struct {
	machineRecordWire
	Agents []agentRecordWire `json:"agents"`
}

func (api machineConnectionAPI) begin(response http.ResponseWriter, request *http.Request) {
	if !api.origin.matches(request) {
		writeAPIError(response, http.StatusBadRequest, "Machine connection server is invalid")
		return
	}
	var body struct {
		RequestID   string `json:"request_id"`
		DisplayName string `json:"display_name"`
		UserCode    string `json:"user_code"`
		PollSecret  string `json:"poll_secret"`
		PublicKey   string `json:"public_key"`
		KeyProof    string `json:"key_proof"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	publicKey, keyErr := base64.StdEncoding.DecodeString(body.PublicKey)
	proof, proofErr := base64.StdEncoding.DecodeString(body.KeyProof)
	if keyErr != nil || proofErr != nil {
		writeAPIError(response, http.StatusBadRequest, machine.ErrInvalidConnection.Error())
		return
	}
	source, err := api.requestSources.Resolve(request)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "request source is invalid")
		return
	}
	begun, err := api.connections.Begin(request.Context(), machine.BeginConnectionRequest{
		RequestID: body.RequestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		DisplayName: body.DisplayName, UserCode: body.UserCode, PollSecret: body.PollSecret,
		Source: source, Origin: api.origin.String(), PublicKeyDER: publicKey, KeyProof: proof,
	})
	if err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusCreated, struct {
		RequestID       string    `json:"request_id"`
		DisplayName     string    `json:"display_name"`
		UserCode        string    `json:"user_code"`
		PollSecret      string    `json:"poll_secret"`
		Fingerprint     string    `json:"fingerprint"`
		VerificationURL string    `json:"verification_url"`
		ExpiresAt       time.Time `json:"expires_at"`
		IntervalSeconds int       `json:"interval_seconds"`
	}{
		RequestID: begun.RequestID, DisplayName: begun.DisplayName, UserCode: begun.UserCode,
		PollSecret:      begun.PollSecret,
		Fingerprint:     begun.Fingerprint,
		VerificationURL: begun.VerificationURL,
		ExpiresAt:       begun.ExpiresAt,
		IntervalSeconds: int(begun.PollInterval / time.Second),
	})
}

func (api machineConnectionAPI) lookup(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusForbidden, "same-origin Browser approval is required")
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	var body struct {
		UserCode string `json:"user_code"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	source, err := api.requestSources.Resolve(request)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "request source is invalid")
		return
	}
	preview, err := api.connections.Lookup(request.Context(), machine.LookupConnectionRequest{
		BrowserSessionID: sessionID, UserCode: body.UserCode, Source: source,
	})
	if err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, machineConnectionPreviewWire{
		RequestID: preview.RequestID, UserCode: preview.UserCode, DisplayName: preview.DisplayName,
		Fingerprint: preview.Fingerprint, Server: preview.Server, Decision: preview.Decision,
		ApprovedSpaceID: preview.ApprovedSpaceID, CreatedAt: preview.CreatedAt, ExpiresAt: preview.ExpiresAt,
	})
}

func (api machineConnectionAPI) approve(response http.ResponseWriter, request *http.Request) {
	api.decide(response, request, "approved")
}

func (api machineConnectionAPI) deny(response http.ResponseWriter, request *http.Request) {
	api.decide(response, request, "denied")
}

func (api machineConnectionAPI) decide(response http.ResponseWriter, request *http.Request, decision string) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusForbidden, "same-origin Browser approval is required")
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	var body struct {
		RequestID string `json:"request_id"`
		UserCode  string `json:"user_code"`
		SpaceID   string `json:"space_id,omitempty"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	if chi.URLParam(request, "request_id") != body.RequestID {
		writeAPIError(response, http.StatusBadRequest, machine.ErrInvalidConnection.Error())
		return
	}
	command := machine.DecideConnectionRequest{
		BrowserSessionID: sessionID, RequestID: body.RequestID, UserCode: body.UserCode,
		SpaceID: body.SpaceID, Decision: decision, IdempotencyKey: request.Header.Get("Idempotency-Key"),
	}
	var err error
	if decision == "approved" {
		err = api.connections.Approve(request.Context(), command)
	} else {
		err = api.connections.Deny(request.Context(), command)
	}
	if err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api machineConnectionAPI) poll(response http.ResponseWriter, request *http.Request) {
	connected, err := api.connections.Poll(request.Context(), request.Header.Get(machineConnectionPollHeader))
	if err != nil {
		writeMachineConnectionPollError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, struct {
		MachineID      string    `json:"machine_id"`
		SpaceID        string    `json:"space_id"`
		DisplayName    string    `json:"display_name"`
		HostAPIOrigin  string    `json:"host_api_origin"`
		CertificatePEM string    `json:"certificate_pem"`
		RedeemedAt     time.Time `json:"redeemed_at"`
		ReplayUntil    time.Time `json:"replay_until"`
	}{
		MachineID:      connected.MachineID,
		SpaceID:        connected.SpaceID,
		DisplayName:    connected.DisplayName,
		HostAPIOrigin:  connected.HostAPIOrigin.String(),
		CertificatePEM: string(connected.CertificatePEM),
		RedeemedAt:     connected.RedeemedAt,
		ReplayUntil:    connected.ReplayUntil,
	})
}

func (api machineConnectionAPI) cancel(response http.ResponseWriter, request *http.Request) {
	if err := api.connections.Cancel(request.Context(), request.Header.Get(machineConnectionPollHeader)); err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api machineConnectionAPI) list(response http.ResponseWriter, request *http.Request) {
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	page, agentRecords, err := api.connections.List(request.Context(), sessionID, chi.URLParam(request, "space_id"), request.URL.Query().Get("after"))
	if err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	agentsByMachine := make(map[string][]agentRecordWire, len(page.Machines))
	for _, record := range agentRecords {
		agentsByMachine[record.MachineID] = append(agentsByMachine[record.MachineID], agentRecordResponse(record))
	}
	items := make([]machineInventoryRecordWire, 0, len(page.Machines))
	for _, record := range page.Machines {
		items = append(items, machineInventoryRecordResponse(record, agentsByMachine[record.MachineID]))
	}
	noStore(response)
	writeJSON(response, http.StatusOK, struct {
		Machines   []machineInventoryRecordWire `json:"machines"`
		NextCursor string                       `json:"next_cursor,omitempty"`
	}{
		Machines:   items,
		NextCursor: page.NextCursor,
	})
}

func (api machineConnectionAPI) revokeFromBrowser(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusForbidden, "same-origin Browser approval is required")
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	record, agentRecords, err := api.connections.RevokeFromBrowser(request.Context(), sessionID, chi.URLParam(request, "space_id"), chi.URLParam(request, "machine_id"), request.Header.Get("Idempotency-Key"))
	if err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, machineInventoryRecordResponse(record, agentRecordResponses(agentRecords)))
}

func (api machineConnectionAPI) revokeFromHost(response http.ResponseWriter, request *http.Request) {
	principal, ok := currentMachinePrincipal(response, request)
	if !ok {
		return
	}
	record, err := api.connections.RevokeFromHost(request.Context(), principal.machineID, principal.certificateSerial, request.Header.Get("Idempotency-Key"))
	if err != nil {
		writeMachineConnectionError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, machineRecordResponse(record))
}

func (api machineConnectionAPI) browserSessionID(response http.ResponseWriter, request *http.Request) (string, bool) {
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is required")
		return "", false
	}
	sessionID, ok := api.credentials.ParseBrowserSessionCredential(cookie.Value)
	if !ok {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is invalid")
		return "", false
	}
	return sessionID, true
}

func agentRecordResponses(records []agent.InventoryRecord) []agentRecordWire {
	result := make([]agentRecordWire, 0, len(records))
	for _, record := range records {
		result = append(result, agentRecordResponse(record))
	}
	return result
}

func agentRecordResponse(record agent.InventoryRecord) agentRecordWire {
	return agentRecordWire{
		AgentID:      record.AgentID,
		Name:         record.Name,
		AvatarIndex:  record.AvatarIndex,
		OwnerUserID:  record.OwnerUserID,
		OwnerName:    record.OwnerName,
		State:        record.Lifecycle,
		Online:       record.Online,
		LastActiveAt: record.LastActiveAt,
	}
}

func machineRecordResponse(record machine.MachineRecord) machineRecordWire {
	actor := record.RevocationActor
	if actor == "not_recorded" {
		actor = "Not recorded"
	} else if actor == "machine" {
		actor = "This Machine"
	} else if actor == "user" {
		actor = record.RevokedByName
	}
	return machineRecordWire{
		MachineID: record.MachineID, SpaceID: record.SpaceID, SpaceName: record.SpaceName,
		DisplayName: record.DisplayName, Fingerprint: record.Fingerprint, State: record.State, EnrolledByUserID: record.EnrolledByUserID,
		EnrolledByName: record.EnrolledByName, EnrolledAt: record.EnrolledAt,
		RevocationActor: actor, RevokedByUserID: record.RevokedByUserID,
		RevokedByName: record.RevokedByName, RevokedAt: record.RevokedAt, CanRevoke: record.CanRevoke,
	}
}

func machineInventoryRecordResponse(record machine.MachineRecord, agents []agentRecordWire) machineInventoryRecordWire {
	return machineInventoryRecordWire{
		machineRecordWire: machineRecordResponse(record),
		Agents:            append([]agentRecordWire(nil), agents...),
	}
}

func writeMachineConnectionPollError(response http.ResponseWriter, err error) {
	var slow machine.ConnectionSlowDownError
	switch {
	case errors.Is(err, machine.ErrConnectionPending):
		noStore(response)
		response.WriteHeader(http.StatusAccepted)
	case errors.As(err, &slow):
		response.Header().Set("Retry-After", strconv.Itoa(int(slow.RetryAfter/time.Second)))
		writeAPIError(response, http.StatusTooManyRequests, err.Error())
	default:
		writeMachineConnectionError(response, err)
	}
}

func writeMachineConnectionError(response http.ResponseWriter, err error) {
	noStore(response)
	switch {
	case errors.Is(err, machine.ErrInvalidConnection):
		writeAPIError(response, http.StatusBadRequest, err.Error())
	case errors.Is(err, machine.ErrMachineUnavailable):
		writeAPIError(response, http.StatusUnauthorized, err.Error())
	case errors.Is(err, machine.ErrConnectionUnavailable):
		writeAPIError(response, http.StatusNotFound, err.Error())
	case errors.Is(err, machine.ErrConnectionRateLimited), errors.Is(err, machine.ErrConnectionSlowDown):
		writeAPIError(response, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, machine.ErrConnectionExpired), errors.Is(err, machine.ErrConnectionReplayExpired):
		writeAPIError(response, http.StatusGone, err.Error())
	case errors.Is(err, machine.ErrConnectionDenied), errors.Is(err, machine.ErrMachineAuthority):
		writeAPIError(response, http.StatusForbidden, err.Error())
	case errors.Is(err, machine.ErrConnectionCancelled), errors.Is(err, machine.ErrConnectionConflict), errors.Is(err, machine.ErrConnectionAlreadyDecided):
		writeAPIError(response, http.StatusConflict, err.Error())
	default:
		writeAPIError(response, http.StatusInternalServerError, "Machine request failed")
	}
}
