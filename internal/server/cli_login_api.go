package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/go-chi/chi/v5"
)

const cliPollHeader = "X-Carry-CLI-Poll"

type CLILogins interface {
	Begin(context.Context, identity.BeginCLILoginRequest) (identity.BegunCLILogin, error)
	Lookup(context.Context, identity.LookupCLILoginRequest) (identity.CLILoginPreview, error)
	Approve(context.Context, identity.ApproveCLILoginRequest) error
	Deny(context.Context, identity.DenyCLILoginRequest) error
	Poll(context.Context, string) (identity.CLICredentialResult, error)
	Cancel(context.Context, string) error
	ListCredentials(context.Context, string) ([]identity.CLICredential, error)
	RevokeFromBrowser(context.Context, string, string, string) error
	RevokeCurrent(context.Context, string, string) error
}

type cliLoginAPI struct {
	logins         CLILogins
	credentials    identity.Credentials
	origin         ExternalOrigin
	requestSources RequestSource
}

func (api cliLoginAPI) begin(response http.ResponseWriter, request *http.Request) {
	if !api.origin.matches(request) {
		writeAPIError(response, http.StatusBadRequest, "CLI login server is invalid")
		return
	}
	var body struct {
		RequestID                       string `json:"request_id"`
		Label                           string `json:"label"`
		ProposedReplacementCredentialID string `json:"proposed_replacement_credential_id"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	source, err := api.requestSources.Resolve(request)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "request source is invalid")
		return
	}
	begun, err := api.logins.Begin(request.Context(), identity.BeginCLILoginRequest{
		RequestID: body.RequestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		Label: body.Label, ProposedReplacementCredentialID: body.ProposedReplacementCredentialID, Source: source,
	})
	if err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusCreated, struct {
		RequestID        string    `json:"request_id"`
		UserCode         string    `json:"user_code"`
		PollSecret       string    `json:"poll_secret"`
		VerificationPath string    `json:"verification_path"`
		ExpiresAt        time.Time `json:"expires_at"`
		IntervalSeconds  int       `json:"interval_seconds"`
	}{
		RequestID: begun.RequestID, UserCode: begun.UserCode, PollSecret: begun.PollSecret,
		VerificationPath: begun.VerificationPath, ExpiresAt: begun.ExpiresAt,
		IntervalSeconds: int(begun.PollInterval / time.Second),
	})
}

func (api cliLoginAPI) lookup(response http.ResponseWriter, request *http.Request) {
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
	preview, err := api.logins.Lookup(request.Context(), identity.LookupCLILoginRequest{
		BrowserSessionID: sessionID, UserCode: body.UserCode, Source: source,
	})
	if err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, struct {
		RequestID                       string    `json:"request_id"`
		UserCode                        string    `json:"user_code"`
		Label                           string    `json:"label"`
		Server                          string    `json:"server"`
		ProposedReplacementCredentialID string    `json:"proposed_replacement_credential_id,omitempty"`
		ApprovedSpaceID                 string    `json:"approved_space_id,omitempty"`
		CreatedAt                       time.Time `json:"created_at"`
		ExpiresAt                       time.Time `json:"expires_at"`
		Approved                        bool      `json:"approved"`
		Denied                          bool      `json:"denied"`
		Cancelled                       bool      `json:"cancelled"`
		Redeemed                        bool      `json:"redeemed"`
	}{
		RequestID: preview.RequestID, UserCode: preview.UserCode, Label: preview.Label, Server: api.origin.String(),
		ProposedReplacementCredentialID: preview.ProposedReplacementCredentialID,
		ApprovedSpaceID:                 preview.ApprovedSpaceID, CreatedAt: preview.CreatedAt, ExpiresAt: preview.ExpiresAt,
		Approved: preview.Approved, Denied: preview.Denied, Cancelled: preview.Cancelled, Redeemed: preview.Redeemed,
	})
}

func (api cliLoginAPI) approve(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusForbidden, "same-origin Browser approval is required")
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	var body struct {
		RequestID               string `json:"request_id"`
		UserCode                string `json:"user_code"`
		SpaceID                 string `json:"space_id"`
		ReplacementCredentialID string `json:"replacement_credential_id,omitempty"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	err := api.logins.Approve(request.Context(), identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: body.RequestID, UserCode: body.UserCode,
		SpaceID: body.SpaceID, ReplacementCredentialID: body.ReplacementCredentialID,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api cliLoginAPI) deny(response http.ResponseWriter, request *http.Request) {
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
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	err := api.logins.Deny(request.Context(), identity.DenyCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: body.RequestID, UserCode: body.UserCode,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api cliLoginAPI) poll(response http.ResponseWriter, request *http.Request) {
	result, err := api.logins.Poll(request.Context(), request.Header.Get(cliPollHeader))
	if err != nil {
		writeCLILoginPollError(response, err)
		return
	}
	noStore(response)
	writeJSON(response, http.StatusOK, struct {
		CredentialID string    `json:"credential_id"`
		Credential   string    `json:"credential"`
		UserID       string    `json:"user_id"`
		SpaceID      string    `json:"space_id"`
		Label        string    `json:"label"`
		ExpiresAt    time.Time `json:"expires_at"`
	}{
		CredentialID: result.CredentialID, Credential: result.Credential, UserID: result.UserID,
		SpaceID: result.SpaceID, Label: result.Label, ExpiresAt: result.ExpiresAt,
	})
}

func (api cliLoginAPI) cancel(response http.ResponseWriter, request *http.Request) {
	if err := api.logins.Cancel(request.Context(), request.Header.Get(cliPollHeader)); err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api cliLoginAPI) listCredentials(response http.ResponseWriter, request *http.Request) {
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	credentials, err := api.logins.ListCredentials(request.Context(), sessionID)
	if err != nil {
		writeCLILoginError(response, err)
		return
	}
	type credentialWire struct {
		CredentialID      string    `json:"credential_id"`
		Label             string    `json:"label"`
		ApprovedSpaceID   string    `json:"approved_space_id"`
		ApprovedSpaceName string    `json:"approved_space_name"`
		CreatedAt         time.Time `json:"created_at"`
		ExpiresAt         time.Time `json:"expires_at"`
	}
	items := make([]credentialWire, 0, len(credentials))
	for _, credential := range credentials {
		items = append(items, credentialWire{
			CredentialID: credential.CredentialID, Label: credential.Label,
			ApprovedSpaceID: credential.ApprovedSpaceID, ApprovedSpaceName: credential.ApprovedSpaceName,
			CreatedAt: credential.CreatedAt, ExpiresAt: credential.ExpiresAt,
		})
	}
	noStore(response)
	writeJSON(response, http.StatusOK, struct {
		Credentials []credentialWire `json:"credentials"`
	}{Credentials: items})
}

func (api cliLoginAPI) revokeFromBrowser(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusForbidden, "same-origin Browser approval is required")
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	if err := api.logins.RevokeFromBrowser(request.Context(), sessionID, chi.URLParam(request, "credential_id"), request.Header.Get("Idempotency-Key")); err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api cliLoginAPI) revokeCurrent(response http.ResponseWriter, request *http.Request) {
	credential, ok := bearerToken(request)
	if !ok {
		writeAPIError(response, http.StatusUnauthorized, "CLI credential is required")
		return
	}
	if _, err := request.Cookie(browserSessionCookie); err == nil {
		writeAPIError(response, http.StatusUnauthorized, "CLI credential authentication is ambiguous")
		return
	}
	if err := api.logins.RevokeCurrent(request.Context(), credential, request.Header.Get("Idempotency-Key")); err != nil {
		writeCLILoginError(response, err)
		return
	}
	noStore(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api cliLoginAPI) browserSessionID(response http.ResponseWriter, request *http.Request) (string, bool) {
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

func writeCLILoginPollError(response http.ResponseWriter, err error) {
	var slow identity.CLISlowDownError
	switch {
	case errors.Is(err, identity.ErrCLILoginPending):
		noStore(response)
		response.WriteHeader(http.StatusAccepted)
	case errors.As(err, &slow):
		response.Header().Set("Retry-After", strconv.Itoa(int(slow.RetryAfter/time.Second)))
		writeAPIError(response, http.StatusTooManyRequests, err.Error())
	default:
		writeCLILoginError(response, err)
	}
}

func writeCLILoginError(response http.ResponseWriter, err error) {
	noStore(response)
	switch {
	case errors.Is(err, identity.ErrInvalidCLILogin):
		writeAPIError(response, http.StatusBadRequest, err.Error())
	case errors.Is(err, identity.ErrUnauthenticated), errors.Is(err, identity.ErrCLICredentialUnavailable):
		writeAPIError(response, http.StatusUnauthorized, err.Error())
	case errors.Is(err, identity.ErrCLILoginUnavailable):
		writeAPIError(response, http.StatusNotFound, err.Error())
	case errors.Is(err, identity.ErrCLILoginRateLimited), errors.Is(err, identity.ErrCLILoginSlowDown):
		writeAPIError(response, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, identity.ErrCLILoginExpired):
		writeAPIError(response, http.StatusGone, err.Error())
	case errors.Is(err, identity.ErrCLILoginDenied):
		writeAPIError(response, http.StatusForbidden, err.Error())
	case errors.Is(err, identity.ErrCLILoginCancelled), errors.Is(err, identity.ErrCLILoginConflict),
		errors.Is(err, identity.ErrCLILoginAlreadyApproved), errors.Is(err, identity.ErrCLILoginRedeemed),
		errors.Is(err, identity.ErrCLIReplacementInvalid):
		writeAPIError(response, http.StatusConflict, err.Error())
	default:
		writeAPIError(response, http.StatusInternalServerError, "CLI login request failed")
	}
}

func noStore(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Referrer-Policy", "no-referrer")
}
