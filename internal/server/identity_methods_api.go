package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/go-chi/chi/v5"
)

// IdentityMethods is the Browser-only Identity Settings behavior consumed by HTTP.
type IdentityMethods interface {
	List(context.Context, string, string) (identity.IdentityMethods, error)
	Unlink(context.Context, identity.UnlinkMethodCommand) (identity.BrowserSession, error)
}

type identityMethodsAPI struct {
	methods     IdentityMethods
	credentials identity.Credentials
	origin      ExternalOrigin
}

func (api identityMethodsAPI) list(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	methods, err := api.methods.List(request.Context(), user.UserID, sessionID)
	if err != nil {
		writeIdentityMethodError(response, err, userReadFailure, "list sign-in methods")
		return
	}
	labels := make([]string, len(methods.Methods))
	for index, method := range methods.Methods {
		labels[index] = string(method)
	}
	writeJSON(response, http.StatusOK, struct {
		Methods                  []string `json:"methods"`
		ReauthenticationRequired bool     `json:"reauthentication_required"`
	}{Methods: labels, ReauthenticationRequired: methods.ReauthenticationRequired})
}

func (api identityMethodsAPI) unlink(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeUnverifiedUserRequest(response)
		return
	}
	if strings.TrimSpace(request.Header.Get("Authorization")) != "" {
		writeUserSignInRequired(response)
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	method := identity.Method(chi.URLParam(request, "method"))
	session, err := api.methods.Unlink(request.Context(), identity.UnlinkMethodCommand{
		SessionID: sessionID, Method: method, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeIdentityMethodError(response, err, userMutationFailure, "remove sign-in method")
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

func (api identityMethodsAPI) browserSessionID(response http.ResponseWriter, request *http.Request) (string, bool) {
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeUserSignInRequired(response)
		return "", false
	}
	sessionID, ok := api.credentials.ParseBrowserSessionCredential(cookie.Value)
	if !ok {
		writeUserSignInRequired(response)
		return "", false
	}
	return sessionID, true
}

func writeIdentityMethodError(response http.ResponseWriter, err error, recovery userFailureRecovery, operation string) {
	switch {
	case errors.Is(err, identity.ErrUnauthenticated):
		writeUserSignInRequired(response)
	case errors.Is(err, identity.ErrRecentIdentityProofRequired):
		writeAPIError(response, http.StatusPreconditionRequired, err.Error())
	case errors.Is(err, identity.ErrIdempotencyConflict):
		writeAPIError(response, http.StatusConflict, "This action no longer matches the saved request. Reload before trying again.")
	case errors.Is(err, identity.ErrIdentityMethodOccupied),
		errors.Is(err, identity.ErrIdentityMethodAlreadyLinked),
		errors.Is(err, identity.ErrIdentityMethodNotLinked),
		errors.Is(err, identity.ErrLastIdentityMethod):
		writeAPIError(response, http.StatusConflict, err.Error())
	default:
		writeUserInternalError(response, recovery, operation, err)
	}
}
