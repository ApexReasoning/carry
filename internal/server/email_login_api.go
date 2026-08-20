package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/google/uuid"
)

// EmailLogin is the Identity behavior consumed by HTTP.
type EmailLogin interface {
	RequestCode(context.Context, identity.RequestEmailCodeCommand) (identity.EmailChallenge, error)
	VerifyCode(context.Context, identity.VerifyEmailCodeCommand) (identity.BrowserSession, error)
}

type emailLoginAPI struct {
	login          EmailLogin
	credentials    identity.Credentials
	requestSources RequestSource
}

func (api emailLoginAPI) requestCode(response http.ResponseWriter, request *http.Request) {
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body struct {
		ChallengeID string `json:"challenge_id"`
		Email       string `json:"email"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	if uuid.Validate(body.ChallengeID) != nil {
		writeAPIError(response, http.StatusBadRequest, "challenge identity is invalid")
		return
	}
	requestSource, err := api.requestSources.Resolve(request)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "request source is invalid")
		return
	}
	challenge, err := api.login.RequestCode(request.Context(), identity.RequestEmailCodeCommand{
		ChallengeID: body.ChallengeID, Email: body.Email, Source: requestSource,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, identity.ErrInvalidEmail) {
		writeAPIError(response, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, identity.ErrEmailRateLimited) {
		writeAPIError(response, http.StatusTooManyRequests, err.Error())
		return
	}
	if errors.Is(err, identity.ErrIdempotencyConflict) {
		writeAPIError(response, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, identity.ErrEmailSubmissionRejected) {
		writeAPIError(response, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "request email code")
		return
	}
	writeJSON(response, http.StatusAccepted, struct {
		ChallengeID string    `json:"challenge_id"`
		ExpiresAt   time.Time `json:"expires_at"`
	}{ChallengeID: challenge.ChallengeID, ExpiresAt: challenge.ExpiresAt})
}

func (api emailLoginAPI) verifyCode(response http.ResponseWriter, request *http.Request) {
	challengeID, ok := pathUUID(response, request, "challenge_id")
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	session, err := api.login.VerifyCode(request.Context(), identity.VerifyEmailCodeCommand{
		ChallengeID: challengeID, Code: body.Code, IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, identity.ErrInvalidCode) || errors.Is(err, identity.ErrUnauthenticated) {
		writeAPIError(response, http.StatusUnauthorized, "email code is invalid or expired")
		return
	}
	if errors.Is(err, identity.ErrIdempotencyConflict) {
		writeAPIError(response, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "verify email code")
		return
	}
	credential, err := api.credentials.BrowserSessionCredential(session.SessionID)
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "create browser session credential")
		return
	}
	setBrowserSessionCookie(response, credential, session.ExpiresAt)
	response.WriteHeader(http.StatusNoContent)
}
