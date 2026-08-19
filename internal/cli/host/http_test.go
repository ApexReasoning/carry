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

func TestMachineReportCannotSendMemberAuthorization(t *testing.T) {
	t.Parallel()

	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if authorization := request.Header.Get("Authorization"); authorization != "" {
				t.Fatalf("Machine request carried member authorization %q", authorization)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	observations := []hostdomain.RuntimeObservation{
		{Kind: hostdomain.RuntimePi, Detection: hostdomain.RuntimeDetected},
		{Kind: hostdomain.RuntimeCodex, Detection: hostdomain.RuntimeNotFound},
	}
	if err := connection.reportRuntimes(context.Background(), observations); err != nil {
		t.Fatalf("report Runtimes: %v", err)
	}
}

func TestMachineClaimMapsNoContentToNoPendingRun(t *testing.T) {
	t.Parallel()

	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/host/runs/claim" || request.Header.Get("Authorization") != "" {
				t.Fatalf("Machine claim request = %s, Authorization %q", request.URL, request.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     "204 No Content",
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	}

	if _, err := connection.Claim(context.Background()); !errors.Is(err, run.ErrNoPendingRun) {
		t.Fatalf("empty claim error = %v", err)
	}
}

func TestAttemptContextUsesAgentBearerWithoutMachineClient(t *testing.T) {
	t.Parallel()

	claim := run.Claim{
		Coordinator: run.Coordinator{RunID: "run-1"}, AttemptID: "attempt-1",
		Fence: 2, AgentCredential: "carry_agent_secret",
	}
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("Agent request used Machine mTLS client")
			return nil, nil
		})},
		agentClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Authorization") != "Bearer carry_agent_secret" {
				t.Fatalf("Agent Authorization = %q", request.Header.Get("Authorization"))
			}
			if request.URL.Path != "/v1/agent/runs/run-1/attempts/attempt-1/context" || request.URL.Query().Get("fence") != "2" {
				t.Fatalf("Agent context URL = %s", request.URL)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body: io.NopCloser(strings.NewReader(
					`{"run_id":"run-1","attempt_id":"attempt-1","work_id":"work-1","space_id":"space-1","goal":"Prepare renewal","current_understanding":"","current_next_step":"","input_start_seq":1,"input_end_seq":1,"base_revision":0,"fence":2,"inputs":[]}`,
				)),
				Header: make(http.Header),
			}, nil
		})},
	}

	attemptContext, err := connection.LoadContext(context.Background(), claim)
	if err != nil {
		t.Fatalf("load Agent context: %v", err)
	}
	if attemptContext.RunID != claim.RunID || attemptContext.Fence != claim.Fence {
		t.Fatalf("Attempt context = %#v", attemptContext)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
