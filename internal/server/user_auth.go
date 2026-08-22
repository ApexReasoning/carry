package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ApexReasoning/carry/internal/identity"
)

const browserSessionCookie = "__Host-carry_session"

// CLICredentialAuthenticator validates the final Browser-approved CLI identity.
type CLICredentialAuthenticator interface {
	AuthenticateCLICredential(context.Context, string) (identity.AuthenticatedUser, error)
}

type userAuthenticator struct {
	cli         CLICredentialAuthenticator
	sessions    BrowserSessions
	credentials identity.Credentials
	origin      ExternalOrigin
}

type userContextKey struct{}

func rejectMachinePrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.TLS != nil && len(request.TLS.PeerCertificates) != 0 {
			writeAPIError(response, http.StatusUnauthorized, "User route does not accept Machine authentication")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (a userAuthenticator) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		user, ok := a.authenticate(response, request)
		if !ok {
			return
		}
		ctx := context.WithValue(request.Context(), userContextKey{}, user)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (a userAuthenticator) requireBrowserUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, hasToken := bearerToken(request); hasToken {
			writeUserSignInRequired(response)
			return
		}
		cookie, err := request.Cookie(browserSessionCookie)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeUserSignInRequired(response)
			return
		}
		sessionID, ok := a.credentials.ParseBrowserSessionCredential(cookie.Value)
		if !ok {
			writeUserSignInRequired(response)
			return
		}
		user, err := a.sessions.AuthenticateBrowserSession(request.Context(), sessionID)
		user, ok = authenticatedUserResult(response, user, err)
		if !ok {
			return
		}
		ctx := context.WithValue(request.Context(), userContextKey{}, user)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (a userAuthenticator) authenticate(
	response http.ResponseWriter,
	request *http.Request,
) (identity.AuthenticatedUser, bool) {
	token, hasToken := bearerToken(request)
	cookie, cookieErr := request.Cookie(browserSessionCookie)
	hasSession := cookieErr == nil && strings.TrimSpace(cookie.Value) != ""
	if hasToken && hasSession {
		writeAPIError(response, http.StatusUnauthorized, "Use either this browser or one CLI sign-in, not both.")
		return identity.AuthenticatedUser{}, false
	}
	if hasToken {
		credentialID, valid := a.credentials.ParseCLICredential(token, a.origin.value)
		if !valid {
			writeUserSignInRequired(response)
			return identity.AuthenticatedUser{}, false
		}
		user, err := a.cli.AuthenticateCLICredential(request.Context(), credentialID)
		return authenticatedUserResult(response, user, err)
	}
	if hasSession {
		sessionID, ok := a.credentials.ParseBrowserSessionCredential(cookie.Value)
		if !ok {
			writeUserSignInRequired(response)
			return identity.AuthenticatedUser{}, false
		}
		user, err := a.sessions.AuthenticateBrowserSession(request.Context(), sessionID)
		return authenticatedUserResult(response, user, err)
	}
	writeUserSignInRequired(response)
	return identity.AuthenticatedUser{}, false
}

func currentUser(response http.ResponseWriter, request *http.Request) (identity.AuthenticatedUser, bool) {
	user, ok := request.Context().Value(userContextKey{}).(identity.AuthenticatedUser)
	if !ok || user.UserID == "" {
		writeUserInternalError(response, userReadFailure, "load authenticated User", errors.New("authenticated User is missing from request context"))
		return identity.AuthenticatedUser{}, false
	}
	return user, true
}

func authenticatedUserResult(
	response http.ResponseWriter,
	user identity.AuthenticatedUser,
	err error,
) (identity.AuthenticatedUser, bool) {
	if errors.Is(err, identity.ErrUnauthenticated) {
		writeUserSignInRequired(response)
		return identity.AuthenticatedUser{}, false
	}
	if err != nil {
		writeUserInternalError(response, userReadFailure, "authenticate User", err)
		return identity.AuthenticatedUser{}, false
	}
	return user, true
}

func bearerToken(request *http.Request) (string, bool) {
	const prefix = "Bearer "
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	return token, token != ""
}
