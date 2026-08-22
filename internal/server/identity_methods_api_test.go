package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity"
)

func TestIdentityMethodUnexpectedFailuresDistinguishReadFromMutation(t *testing.T) {
	t.Parallel()

	credentials := testIdentityCredentials(t)
	sessionID := "10000000-0000-4000-8000-000000000001"
	credential, err := credentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	user := identity.AuthenticatedUser{UserID: "20000000-0000-4000-8000-000000000001"}
	api := identityMethodsAPI{
		methods:     failingIdentityMethods{},
		credentials: credentials,
		origin:      testExternalOrigin(t),
	}

	readRequest := httptest.NewRequest(http.MethodGet, "https://carry.example/v1/identity/methods", nil)
	readRequest = readRequest.WithContext(context.WithValue(readRequest.Context(), userContextKey{}, user))
	readRequest.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	readResponse := httptest.NewRecorder()
	api.list(readResponse, readRequest)
	assertUserFacingResponse(t, readResponse, http.StatusInternalServerError, "Carry could not load this right now. Reload to try again.")

	mutationRequest := httptest.NewRequest(http.MethodDelete, "https://carry.example/v1/identity/methods/email", nil)
	mutationRequest = mutationRequest.WithContext(context.WithValue(mutationRequest.Context(), userContextKey{}, user))
	mutationRequest.Header.Set("Origin", "https://carry.example")
	mutationRequest.Header.Set("Idempotency-Key", "remove-email")
	mutationRequest.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
	mutationResponse := httptest.NewRecorder()
	api.unlink(mutationResponse, mutationRequest)
	assertUserFacingResponse(t, mutationResponse, http.StatusInternalServerError, "Carry could not confirm whether this change finished. Check the current page before trying again.")
}

type failingIdentityMethods struct{}

func (failingIdentityMethods) List(context.Context, string, string) (identity.IdentityMethods, error) {
	return identity.IdentityMethods{}, errors.New("database unavailable")
}

func (failingIdentityMethods) Unlink(context.Context, identity.UnlinkMethodCommand) (identity.BrowserSession, error) {
	return identity.BrowserSession{}, errors.New("database unavailable")
}
