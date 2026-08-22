package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/google/uuid"
)

// EmailLogin is the Identity behavior consumed by HTTP.
type EmailLogin interface {
	RequestCode(context.Context, identity.RequestEmailCodeCommand) (identity.EmailChallenge, error)
	RequestReauthenticationCode(context.Context, identity.RequestEmailMethodCodeCommand) (identity.EmailChallenge, error)
	RequestLinkCode(context.Context, identity.RequestEmailMethodCodeCommand) (identity.EmailChallenge, error)
	VerifyCode(context.Context, identity.VerifyEmailCodeCommand) (identity.BrowserSession, error)
	VerifyReauthenticationCode(context.Context, identity.VerifyEmailCodeCommand) (identity.BrowserSession, error)
	VerifyLinkCode(context.Context, identity.VerifyEmailCodeCommand) (identity.BrowserSession, error)
}

type emailLoginAPI struct {
	login          EmailLogin
	credentials    identity.Credentials
	requestSources RequestSource
	origin         ExternalOrigin
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
		writeUnverifiedRequestSource(response, "resolve email sign-in request source", err)
		return
	}
	challenge, err := api.login.RequestCode(request.Context(), identity.RequestEmailCodeCommand{
		ChallengeID: body.ChallengeID, Email: body.Email, Source: requestSource,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeEmailRequestError(response, err, "request email code")
		return
	}
	writeEmailChallenge(response, challenge)
}

func (api emailLoginAPI) requestReauthenticationCode(response http.ResponseWriter, request *http.Request) {
	api.requestMethodCode(response, request, false)
}

func (api emailLoginAPI) requestLinkCode(response http.ResponseWriter, request *http.Request) {
	api.requestMethodCode(response, request, true)
}

func (api emailLoginAPI) requestMethodCode(response http.ResponseWriter, request *http.Request, link bool) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeUnverifiedUserRequest(response)
		return
	}
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	sessionID, ok := (identityMethodsAPI{credentials: api.credentials}).browserSessionID(response, request)
	if !ok {
		return
	}
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
	if !link && body.Email != "" {
		writeAPIError(response, http.StatusBadRequest, "email is not accepted for reauthentication")
		return
	}
	requestSource, err := api.requestSources.Resolve(request)
	if err != nil {
		writeUnverifiedRequestSource(response, "resolve sign-in method request source", err)
		return
	}
	command := identity.RequestEmailMethodCodeCommand{
		ChallengeID: body.ChallengeID, Email: body.Email, Source: requestSource,
		IdempotencyKey: idempotencyKey, UserID: user.UserID, SessionID: sessionID,
	}
	var challenge identity.EmailChallenge
	if link {
		challenge, err = api.login.RequestLinkCode(request.Context(), command)
	} else {
		challenge, err = api.login.RequestReauthenticationCode(request.Context(), command)
	}
	if err != nil {
		writeEmailRequestError(response, err, "request sign-in method code")
		return
	}
	writeEmailChallenge(response, challenge)
}

func (api emailLoginAPI) verifyCode(response http.ResponseWriter, request *http.Request) {
	api.verify(response, request, identity.LoginPurpose)
}

func (api emailLoginAPI) verifyReauthenticationCode(response http.ResponseWriter, request *http.Request) {
	api.verify(response, request, identity.ReauthenticatePurpose)
}

func (api emailLoginAPI) verifyLinkCode(response http.ResponseWriter, request *http.Request) {
	api.verify(response, request, identity.LinkPurpose)
}

func (api emailLoginAPI) verify(response http.ResponseWriter, request *http.Request, purpose identity.ProofPurpose) {
	if purpose != identity.LoginPurpose && !api.origin.acceptsSensitivePOST(request) {
		writeUnverifiedUserRequest(response)
		return
	}
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
	command := identity.VerifyEmailCodeCommand{
		ChallengeID: challengeID, Code: body.Code, IdempotencyKey: idempotencyKey,
	}
	if purpose != identity.LoginPurpose {
		if request.Header.Get("Authorization") != "" {
			writeUserSignInRequired(response)
			return
		}
		sessionID, ok := (identityMethodsAPI{credentials: api.credentials}).browserSessionID(response, request)
		if !ok {
			return
		}
		command.InitiatingSessionID = sessionID
	}
	var session identity.BrowserSession
	var err error
	switch purpose {
	case identity.LoginPurpose:
		session, err = api.login.VerifyCode(request.Context(), command)
	case identity.ReauthenticatePurpose:
		session, err = api.login.VerifyReauthenticationCode(request.Context(), command)
	case identity.LinkPurpose:
		session, err = api.login.VerifyLinkCode(request.Context(), command)
	}
	if errors.Is(err, identity.ErrInvalidCode) {
		writeAPIError(response, http.StatusUnauthorized, "email code is invalid or expired")
		return
	}
	if err != nil {
		if purpose == identity.LoginPurpose {
			writeEmailRequestError(response, err, "verify email code")
		} else {
			writeIdentityMethodError(response, err, userMutationFailure, "verify sign-in method code")
		}
		return
	}
	credential, err := api.credentials.BrowserSessionCredential(session.SessionID)
	if err != nil {
		writeUserInternalError(response, userMutationFailure, "create Browser Session credential", err)
		return
	}
	setBrowserSessionCookie(response, credential, session.ExpiresAt)
	response.WriteHeader(http.StatusNoContent)
}

func writeEmailChallenge(response http.ResponseWriter, challenge identity.EmailChallenge) {
	writeJSON(response, http.StatusAccepted, struct {
		ChallengeID string    `json:"challenge_id"`
		ExpiresAt   time.Time `json:"expires_at"`
	}{ChallengeID: challenge.ChallengeID, ExpiresAt: challenge.ExpiresAt})
}

func writeEmailRequestError(response http.ResponseWriter, err error, operation string) {
	switch {
	case errors.Is(err, identity.ErrInvalidEmail):
		writeAPIError(response, http.StatusBadRequest, err.Error())
	case errors.Is(err, identity.ErrEmailRateLimited):
		scope := "unspecified"
		switch {
		case errors.Is(err, identity.ErrEmailAddressAdmissionLimited):
			scope = "address"
		case errors.Is(err, identity.ErrEmailSourceAdmissionLimited):
			scope = "source"
		case errors.Is(err, identity.ErrEmailResendDelayed):
			scope = "resend-delay"
		}
		slog.Warn("email challenge admission limited", "scope", scope)
		writeAPIError(response, http.StatusTooManyRequests, identity.ErrEmailRateLimited.Error())
	case errors.Is(err, identity.ErrIdempotencyConflict):
		writeAPIError(response, http.StatusConflict, "This action no longer matches the saved request. Reload before trying again.")
	case errors.Is(err, identity.ErrEmailSubmissionRejected):
		writeAPIError(response, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, identity.ErrUnauthenticated),
		errors.Is(err, identity.ErrRecentIdentityProofRequired),
		errors.Is(err, identity.ErrIdentityMethodNotLinked):
		writeIdentityMethodError(response, err, userMutationFailure, operation)
	default:
		writeUserInternalError(response, userMutationFailure, operation, err)
	}
}
