package server

import (
	"context"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
)

// BrowserSessions exposes current Browser Session authentication and revocation use cases.
type BrowserSessions interface {
	AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error)
	RevokeBrowserSession(context.Context, string) error
}

type browserSessionAPI struct {
	sessions    BrowserSessions
	credentials identity.Credentials
}

func (api browserSessionAPI) revokeCurrent(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil {
		writeAPIError(response, http.StatusUnauthorized, "browser session is required")
		return
	}
	sessionID, ok := api.credentials.ParseBrowserSessionCredential(cookie.Value)
	if !ok {
		writeAPIError(response, http.StatusUnauthorized, "browser session is invalid")
		return
	}
	if err := api.sessions.RevokeBrowserSession(request.Context(), sessionID); err != nil {
		writeAPIError(response, http.StatusInternalServerError, "revoke browser session")
		return
	}
	expireBrowserSessionCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func setBrowserSessionCookie(response http.ResponseWriter, credential string, expiresAt time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name: browserSessionCookie, Value: credential, Path: "/",
		Expires: expiresAt, MaxAge: maxAgeUntil(expiresAt),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func expireBrowserSessionCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name: browserSessionCookie, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func maxAgeUntil(expiry time.Time) int {
	seconds := int(time.Until(expiry).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}
