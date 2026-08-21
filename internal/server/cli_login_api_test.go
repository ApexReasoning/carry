package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
)

func TestCLILoginBeginUsesConfiguredOriginAndNeverForwardedHost(t *testing.T) {
	stub := &recordingCLILogins{begun: identity.BegunCLILogin{
		RequestID: "11111111-1111-4111-8111-111111111111", UserCode: "BCDF-GHJ-KLM",
		PollSecret: "carry_cli_poll_secret", VerificationPath: "/cli-login",
		ExpiresAt: time.Now().Add(time.Minute), PollInterval: 5 * time.Second,
	}}
	api := cliLoginAPI{logins: stub, origin: testExternalOrigin(t), requestSources: NewRequestSource(nil)}
	body := `{"request_id":"11111111-1111-4111-8111-111111111111","label":"Desk CLI"}`
	hostile := httptest.NewRequest(http.MethodPost, "https://evil.example/v1/cli-logins", strings.NewReader(body))
	hostile.Header.Set("Idempotency-Key", "begin")
	hostile.Header.Set("Forwarded", "host=carry.example;proto=https")
	hostile.Header.Set("X-Forwarded-Host", "carry.example")
	hostile.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	api.begin(response, hostile)
	if response.Code != http.StatusBadRequest || stub.beginCalls != 0 {
		t.Fatalf("hostile origin status = %d, begin calls = %d", response.Code, stub.beginCalls)
	}

	valid := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/cli-logins", strings.NewReader(body))
	valid.Header.Set("Idempotency-Key", "begin")
	valid.RemoteAddr = "127.0.0.1:1234"
	response = httptest.NewRecorder()
	api.begin(response, valid)
	if response.Code != http.StatusCreated || stub.beginCalls != 1 {
		t.Fatalf("valid begin status = %d, calls = %d, body = %s", response.Code, stub.beginCalls, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "carry.example/v1") {
		t.Fatalf("begin headers/body = %#v %s", response.Header(), response.Body.String())
	}
}

func TestCLILoginBrowserDecisionRequiresSameOriginSessionAndReturnsNoCredential(t *testing.T) {
	credentials := testIdentityCredentials(t)
	stub := &recordingCLILogins{}
	api := cliLoginAPI{logins: stub, credentials: credentials, origin: testExternalOrigin(t), requestSources: NewRequestSource(nil)}
	body := `{"request_id":"11111111-1111-4111-8111-111111111111","user_code":"BCDF-GHJ-KLM","space_id":"22222222-2222-4222-8222-222222222222"}`
	request := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/cli-logins/approve", strings.NewReader(body))
	request.Header.Set("Origin", "https://carry.example")
	request.Header.Set("Idempotency-Key", "approve")
	session, err := credentials.BrowserSessionCredential(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: session})
	response := httptest.NewRecorder()
	api.approve(response, request)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 || stub.approved.BrowserSessionID != testSessionID {
		t.Fatalf("approval = %d %q %#v", response.Code, response.Body.String(), stub.approved)
	}

	crossOrigin := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/cli-logins/approve", strings.NewReader(body))
	crossOrigin.Header.Set("Origin", "https://phishing.example")
	crossOrigin.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: session})
	response = httptest.NewRecorder()
	api.approve(response, crossOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin approval status = %d", response.Code)
	}
}

type recordingCLILogins struct {
	begun      identity.BegunCLILogin
	beginCalls int
	approved   identity.ApproveCLILoginRequest
}

func (stub *recordingCLILogins) Begin(_ context.Context, request identity.BeginCLILoginRequest) (identity.BegunCLILogin, error) {
	stub.beginCalls++
	return stub.begun, nil
}
func (*recordingCLILogins) Lookup(context.Context, identity.LookupCLILoginRequest) (identity.CLILoginPreview, error) {
	return identity.CLILoginPreview{}, nil
}
func (stub *recordingCLILogins) Approve(_ context.Context, request identity.ApproveCLILoginRequest) error {
	stub.approved = request
	return nil
}
func (*recordingCLILogins) Deny(context.Context, identity.DenyCLILoginRequest) error { return nil }
func (*recordingCLILogins) Poll(context.Context, string) (identity.CLICredentialResult, error) {
	return identity.CLICredentialResult{}, identity.ErrCLILoginPending
}
func (*recordingCLILogins) Cancel(context.Context, string) error { return nil }
func (*recordingCLILogins) ListCredentials(context.Context, string) ([]identity.CLICredential, error) {
	return nil, nil
}
func (*recordingCLILogins) RevokeFromBrowser(context.Context, string, string, string) error {
	return nil
}
func (*recordingCLILogins) RevokeCurrent(context.Context, string, string) error { return nil }
