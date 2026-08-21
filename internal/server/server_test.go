package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReportsReady(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	healthTestAPI(t, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Body.String(); got != "{\"status\":\"ready\"}\n" {
		t.Fatalf("health body = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("public health Cache-Control = %q, want unset", got)
	}
}

func TestHealthReportsUnavailableWhenDatabaseFails(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	healthTestAPI(t, failingReadiness{}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Body.String(); got != "{\"status\":\"unavailable\"}\n" {
		t.Fatalf("health body = %q", got)
	}
}

func healthTestAPI(t *testing.T, readiness Readiness) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	member := testUserRoutes(t, authority)
	runStore := &recordingMachineRuns{}
	machine, err := NewMachineRoutes(runStore, unavailableMachineConversations{}, unavailableMachineConnections{})
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPIWithReadiness(t, readiness, member, machine)
}

type failingReadiness struct{}

func (failingReadiness) Ping(context.Context) error {
	return errors.New("database unavailable")
}

func TestUnmatchedV1ResponseIsNotStored(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/not-a-route", nil)
	response := httptest.NewRecorder()
	healthTestAPI(t, nil).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("unmatched v1 status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unmatched v1 Cache-Control = %q, want no-store", got)
	}
}

func TestHealthRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()

	healthTestAPI(t, nil).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
