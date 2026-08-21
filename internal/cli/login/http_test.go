package login

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestLoginClientKeepsExactOriginAndTypedPollingOutcomes(t *testing.T) {
	requestID := "11111111-1111-4111-8111-111111111111"
	pollSecret := "carry_cli_poll_11111111-1111-4111-8111-111111111111.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli-logins":
			if request.Header.Get("Idempotency-Key") != "begin-key" {
				t.Errorf("begin key = %q", request.Header.Get("Idempotency-Key"))
			}
			writeLoginJSON(response, http.StatusCreated, begunLogin{
				RequestID: requestID, UserCode: "BCDF-GHJ-KLM", PollSecret: pollSecret,
				VerificationPath: "/cli-login", ExpiresAt: time.Now().Add(time.Minute), IntervalSeconds: 5,
			})
		case "/v1/cli-logins/poll":
			if request.Header.Get("X-Carry-CLI-Poll") != pollSecret {
				t.Errorf("poll proof = %q", request.Header.Get("X-Carry-CLI-Poll"))
			}
			response.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := testLoginClient(t, server)
	begun, err := client.begin(context.Background(), requestID, "begin-key", "Desk CLI", "")
	if err != nil || begun.UserCode != "BCDF-GHJ-KLM" {
		t.Fatalf("begin = %#v, %v", begun, err)
	}
	if _, err := client.poll(context.Background(), pollSecret); err == nil {
		t.Fatal("pending poll was treated as redemption")
	} else {
		var responseErr *loginHTTPError
		if !errors.As(err, &responseErr) || responseErr.status != http.StatusAccepted {
			t.Fatalf("pending poll error = %v", err)
		}
	}
}

func TestLoginClientDoesNotFollowRedirects(t *testing.T) {
	targetCalled := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalled = true }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := testLoginClient(t, server)
	_, err := client.begin(context.Background(), "11111111-1111-4111-8111-111111111111", "key", "Desk", "")
	var responseErr *loginHTTPError
	if !errors.As(err, &responseErr) || responseErr.status != http.StatusTemporaryRedirect {
		t.Fatalf("redirect error = %v", err)
	}
	if targetCalled {
		t.Fatal("CLI login followed a redirect to another origin")
	}
}

func testLoginClient(t *testing.T, server *httptest.Server) *loginClient {
	t.Helper()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newLoginClient(origin, string(certificate))
	if err != nil {
		t.Fatalf("create login client: %v", err)
	}
	return client
}

func writeLoginJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
