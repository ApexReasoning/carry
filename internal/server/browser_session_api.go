package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
)

const browserSessionTTL = 8 * time.Hour

// BrowserSessionStore owns opaque browser-session creation, authentication,
// and revocation without exposing stored session digests to HTTP.
type BrowserSessionStore interface {
	CreateBrowserSession(context.Context, string, time.Time) (identity.BrowserSession, error)
	AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error)
	RevokeBrowserSession(context.Context, string) error
}

type browserSessionAPI struct {
	store BrowserSessionStore
}

func (api browserSessionAPI) create(response http.ResponseWriter, request *http.Request) {
	if _, err := request.Cookie(browserSessionCookie); err == nil {
		writeAPIError(response, http.StatusUnauthorized, "browser session already exists")
		return
	}
	token, ok := bearerToken(request)
	if !ok {
		writeAPIError(response, http.StatusUnauthorized, "member token is required")
		return
	}
	session, err := api.store.CreateBrowserSession(request.Context(), token, time.Now().Add(browserSessionTTL))
	if errors.Is(err, identity.ErrUnauthenticated) {
		writeAPIError(response, http.StatusUnauthorized, "member authentication is invalid")
		return
	}
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "create browser session")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	http.SetCookie(response, &http.Cookie{
		Name: browserSessionCookie, Value: session.Secret, Path: "/",
		Expires: session.ExpiresAt, MaxAge: maxAgeUntil(session.ExpiresAt),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	response.WriteHeader(http.StatusNoContent)
}

func (api browserSessionAPI) revokeCurrent(response http.ResponseWriter, request *http.Request) {
	if _, hasToken := bearerToken(request); hasToken {
		writeAPIError(response, http.StatusUnauthorized, "browser session authentication is required")
		return
	}
	// Expire the browser credential before database I/O so even a server-side
	// revocation failure stops this browser from presenting the old secret.
	response.Header().Set("Cache-Control", "no-store")
	http.SetCookie(response, &http.Cookie{
		Name: browserSessionCookie, Value: "", Path: "/",
		Expires: time.Unix(1, 0), MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil || cookie.Value == "" {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if err := api.store.RevokeBrowserSession(request.Context(), cookie.Value); err != nil {
		writeAPIError(response, http.StatusInternalServerError, "revoke browser session")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func maxAgeUntil(expiry time.Time) int {
	seconds := int(time.Until(expiry).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}
