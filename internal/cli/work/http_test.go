package work

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity/memberfile"
	"github.com/ApexReasoning/carry/internal/space"
)

func TestParseServerURLRequiresHTTPSRoot(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"https://carry.example.com", "https://127.0.0.1:8443/"} {
		if _, err := parseServerURL(valid); err != nil {
			t.Errorf("valid URL %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"http://carry.example.com",
		"carry.example.com",
		"https://user@carry.example.com",
		"https://carry.example.com/v1",
		"https://carry.example.com?token=secret",
		"https://carry.example.com#fragment",
	} {
		if _, err := parseServerURL(invalid); err == nil {
			t.Errorf("invalid URL %q accepted", invalid)
		}
	}
}

func TestMutationRetriesResponseLossWithSameIdentityAndBytes(t *testing.T) {
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
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(bytes.NewBufferString(
				`{"work_id":"11111111-1111-4111-8111-111111111111","space_id":"22222222-2222-4222-8222-222222222222","goal":"Review renewals","lifecycle":"open","owner_user_id":"member-1","creator_user_id":"member-1","input_head_seq":1,"created_at":"2026-08-18T16:00:00Z"}`,
			)),
			Request: request,
		}, nil
	})
	origin, _ := url.Parse("https://carry.example")
	client := memberHTTP{
		origin: origin,
		token:  "member-secret",
		client: &http.Client{Transport: transport},
	}

	created, err := client.create(
		context.Background(),
		"22222222-2222-4222-8222-222222222222",
		"Review renewals",
		"stable-create-key",
	)
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
		if request.Header.Get("Idempotency-Key") != "stable-create-key" {
			t.Fatalf("attempt %d idempotency key = %q", index, request.Header.Get("Idempotency-Key"))
		}
		if request.Header.Get("Authorization") != "Bearer member-secret" {
			t.Fatalf("attempt %d authorization = %q", index, request.Header.Get("Authorization"))
		}
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatalf("retried body changed: %q then %q", bodies[0], bodies[1])
	}
}

func TestMutationReportsUnknownAfterResponsesRemainLost(t *testing.T) {
	t.Parallel()

	attempts := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("response lost")
	})
	origin, _ := url.Parse("https://carry.example")
	client := memberHTTP{
		origin: origin,
		token:  "member-secret",
		client: &http.Client{Transport: transport},
	}

	_, err := client.create(
		context.Background(),
		"22222222-2222-4222-8222-222222222222",
		"Review renewals",
		"stable-create-key",
	)
	var unknown *outcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("create error = %v, want unknown outcome", err)
	}
	if attempts != 2 {
		t.Fatalf("request attempts = %d, want 2", attempts)
	}
}

func TestSelectSpaceUsesOnlyCredentialMemberships(t *testing.T) {
	t.Parallel()

	credential := memberfile.Credential{Spaces: []space.Membership{
		{SpaceID: "11111111-1111-4111-8111-111111111111", Name: "Research"},
		{SpaceID: "22222222-2222-4222-8222-222222222222", Name: "Operations"},
	}}
	selected, err := selectSpace(credential, "22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("select Space: %v", err)
	}
	if selected != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("selected Space = %q", selected)
	}
	if _, err := selectSpace(credential, "33333333-3333-4333-8333-333333333333"); err == nil {
		t.Fatal("selected Space outside member credential")
	}
	if _, err := selectSpace(credential, ""); err == nil {
		t.Fatal("multiple Spaces did not require explicit selection")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
