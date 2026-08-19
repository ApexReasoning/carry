package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ApexReasoning/carry/internal/identity"
)

const browserSessionCookie = "__Host-carry_session"

// UserTokenAuthenticator validates a member token without owning browser-session state.
type UserTokenAuthenticator interface {
	AuthenticateUserToken(context.Context, string) (identity.AuthenticatedUser, error)
}

type memberAuthenticator struct {
	tokens   UserTokenAuthenticator
	sessions BrowserSessionStore
}

type memberContextKey struct{}

func (a memberAuthenticator) requireMember(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		user, ok := a.authenticate(response, request)
		if !ok {
			return
		}
		ctx := context.WithValue(request.Context(), memberContextKey{}, user)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (a memberAuthenticator) authenticate(
	response http.ResponseWriter,
	request *http.Request,
) (identity.AuthenticatedUser, bool) {
	token, hasToken := bearerToken(request)
	cookie, cookieErr := request.Cookie(browserSessionCookie)
	hasSession := cookieErr == nil && strings.TrimSpace(cookie.Value) != ""
	if hasToken && hasSession {
		writeAPIError(response, http.StatusUnauthorized, "member authentication is ambiguous")
		return identity.AuthenticatedUser{}, false
	}
	if hasToken {
		user, err := a.tokens.AuthenticateUserToken(request.Context(), token)
		return authenticatedUserResult(response, user, err)
	}
	if hasSession {
		user, err := a.sessions.AuthenticateBrowserSession(request.Context(), cookie.Value)
		return authenticatedUserResult(response, user, err)
	}
	writeAPIError(response, http.StatusUnauthorized, "member authentication is required")
	return identity.AuthenticatedUser{}, false
}

func currentMember(response http.ResponseWriter, request *http.Request) (identity.AuthenticatedUser, bool) {
	user, ok := request.Context().Value(memberContextKey{}).(identity.AuthenticatedUser)
	if !ok || user.UserID == "" {
		writeAPIError(response, http.StatusInternalServerError, "member authentication context is missing")
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
		writeAPIError(response, http.StatusUnauthorized, "member authentication is invalid")
		return identity.AuthenticatedUser{}, false
	}
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "authenticate member")
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
