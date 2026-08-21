package userapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestParseServerURLRequiresHTTPSOrigin(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"https://carry.example.com", "https://127.0.0.1:8443/"} {
		if _, err := ParseServerURL(valid); err != nil {
			t.Errorf("valid URL %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"http://carry.example.com", "carry.example.com", "https://user@carry.example.com",
		"https://carry.example.com/v1", "https://carry.example.com?token=secret",
		"https://carry.example.com#fragment",
	} {
		if _, err := ParseServerURL(invalid); err == nil {
			t.Errorf("invalid URL %q accepted", invalid)
		}
	}
}

func TestWorkMutationRetriesResponseLossWithSameIdentityAndBytes(t *testing.T) {
	t.Parallel()
	var requests []*http.Request
	var bodies [][]byte
	attempt := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempt++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requests = append(requests, request)
		bodies = append(bodies, body)
		if attempt == 1 {
			return nil, errors.New("response lost")
		}
		return jsonResponse(request, `{
			"work_id":"11111111-1111-4111-8111-111111111111",
			"space_id":"22222222-2222-4222-8222-222222222222",
			"goal":"Review renewals","lifecycle":"open",
			"owner_user_id":"33333333-3333-4333-8333-333333333333",
			"owner_display_name":"Mina","creator_user_id":"33333333-3333-4333-8333-333333333333",
			"creator_display_name":"Mina","understanding":"","next_step":"",
			"has_unapplied_input":true,"needs_retry":false,"created_at":"2026-08-18T16:00:00Z"
		}`), nil
	})
	origin, _ := url.Parse("https://carry.example")
	client := Client{origin: origin, credential: "member-secret", client: &http.Client{Transport: transport}}

	created, err := client.CreateWork(context.Background(), "22222222-2222-4222-8222-222222222222", "Review renewals", "stable-create-key")
	if err != nil {
		t.Fatalf("create Work: %v", err)
	}
	if created.WorkID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("created Work = %#v", created)
	}
	if len(requests) != 2 {
		t.Fatalf("request attempts = %d, want 2", len(requests))
	}
	for index, request := range requests {
		if request.Header.Get("Idempotency-Key") != "stable-create-key" ||
			request.Header.Get("Authorization") != "Bearer member-secret" {
			t.Fatalf("attempt %d headers = %#v", index, request.Header)
		}
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatalf("retried body changed: %q then %q", bodies[0], bodies[1])
	}
}

func TestWorkMutationReportsUnknownAfterResponsesRemainLost(t *testing.T) {
	t.Parallel()
	attempts := 0
	origin, _ := url.Parse("https://carry.example")
	client := Client{origin: origin, credential: "member-secret", client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("response lost")
	})}}
	_, err := client.CreateWork(context.Background(), "22222222-2222-4222-8222-222222222222", "Review renewals", "stable-create-key")
	if _, ok := errors.AsType[*OutcomeUnknownError](err); !ok {
		t.Fatalf("create error = %v, want unknown outcome", err)
	}
	if attempts != 2 {
		t.Fatalf("request attempts = %d, want 2", attempts)
	}
}

func TestMachineEnrollmentDoesNotHideResponseLoss(t *testing.T) {
	t.Parallel()
	attempts := 0
	origin, _ := url.Parse("https://carry.example")
	client := Client{origin: origin, credential: "member-secret", client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("response lost")
	})}}
	_, err := client.EnrollMachine(context.Background(), "22222222-2222-4222-8222-222222222222", "Desk Host", "stable-enroll-key", []byte("public-key"))
	if err == nil || attempts != 1 {
		t.Fatalf("enrollment error = %v, attempts = %d", err, attempts)
	}
}

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body)), Request: request}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
