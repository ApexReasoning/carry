package host

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
)

func TestParseServerURLRequiresHTTPSRoot(t *testing.T) {
	t.Parallel()

	if got, err := parseServerURL("https://carry.example.com/"); err != nil || got != "https://carry.example.com" {
		t.Fatalf("valid URL = %q, %v", got, err)
	}
	for _, invalid := range []string{
		"http://carry.example.com",
		"https://carry.example.com/v1",
		"https://user@carry.example.com",
	} {
		if _, err := parseServerURL(invalid); err == nil {
			t.Errorf("invalid URL %q accepted", invalid)
		}
	}
}

func TestMachineClaimUsesOnlyMTLSClientAndReturnsCompleteContext(t *testing.T) {
	t.Parallel()

	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/host/runs/claim" || request.Header.Get("Authorization") != "" {
				t.Fatalf("Machine claim request = %s, Authorization %q", request.URL, request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{
				"run_id":"run-1","attempt_id":"attempt-1","work_id":"work-1","space_id":"space-1",
				"fence":2,"lease_expires_at":"2030-01-01T00:00:00Z","goal":"Prepare renewal",
				"current_understanding":"Finance approved","current_next_step":"Apply wording",
				"base_understanding_version":3,"input_end_seq":5,
				"messages":[{"author_user_id":"member-1","text":"Legal supplied wording"}]
			}`), nil
		})},
	}

	claim, err := connection.Claim(context.Background())
	if err != nil {
		t.Fatalf("claim Run: %v", err)
	}
	if claim.RunID != "run-1" || claim.BaseUnderstandingVersion != 3 ||
		len(claim.Messages) != 1 || claim.Messages[0].Text != "Legal supplied wording" {
		t.Fatalf("claim = %#v", claim)
	}
}

func TestMachineClaimMapsNoContentToNoRunAvailable(t *testing.T) {
	t.Parallel()

	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNoContent, ""), nil
		})},
	}
	if _, err := connection.Claim(context.Background()); !errors.Is(err, run.ErrNoRunAvailable) {
		t.Fatalf("empty claim error = %v", err)
	}
}

func TestMachineCommitUsesHostRouteWithoutBearer(t *testing.T) {
	t.Parallel()

	claim := run.Claim{
		RunID: "run-1", AttemptID: "attempt-1", Fence: 2,
		BaseUnderstandingVersion: 3, InputEndSeq: 5,
	}
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/host/runs/run-1/attempts/attempt-1/understanding" ||
				request.Header.Get("Authorization") != "" {
				t.Fatalf("Machine commit request = %s, Authorization %q", request.URL, request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusNoContent, ""), nil
		})},
	}
	if err := connection.Commit(context.Background(), claim, hostdomain.UnderstandingUpdate{
		Understanding: "Finance and legal approved", NextStep: "Ask the owner",
	}); err != nil {
		t.Fatalf("commit understanding: %v", err)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status),
		Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
