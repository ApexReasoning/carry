package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
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

func TestMachineConversationClaimUsesExactMTLSRouteAndBoundedContext(t *testing.T) {
	t.Parallel()
	const sourceID = "11111111-1111-1111-1111-111111111111"
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.Path != "/v1/host/conversation-replies/claim" ||
				request.Header.Get("Authorization") != "" {
				t.Fatalf("private claim request = %s %s, Authorization %q", request.Method, request.URL, request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{
				"source_message_id":"`+sourceID+`","fence":3,
				"lease_expires_at":"2030-01-01T00:00:00Z",
				"messages":[{"author":"carry","text":"Earlier private answer"},{"author":"member","text":"Private question"}]
			}`), nil
		})},
	}
	claim, err := connection.ClaimConversation(context.Background())
	if err != nil {
		t.Fatalf("claim private Conversation reply: %v", err)
	}
	if claim.SourceMessageID != sourceID || claim.Fence != 3 || len(claim.Messages) != 2 ||
		claim.Messages[0].Author != conversation.AuthorCarry || claim.Messages[1].Text != "Private question" {
		t.Fatalf("private claim = %#v", claim)
	}
}

func TestMachineConversationClaimMapsNoContentAndRejectsMalformedResponse(t *testing.T) {
	t.Parallel()
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNoContent, ""), nil
		})},
	}
	if _, err := connection.ClaimConversation(context.Background()); !errors.Is(err, conversation.ErrNoReplyAvailable) {
		t.Fatalf("empty private claim error = %v", err)
	}

	for _, body := range []string{
		`{"source_message_id":"11111111-1111-1111-1111-111111111111","fence":1,"lease_expires_at":"2030-01-01T00:00:00Z","messages":[{"author":"member","text":"hello"}],"unexpected":true}`,
		`{"source_message_id":"11111111-1111-1111-1111-111111111111","fence":1,"lease_expires_at":"2030-01-01T00:00:00Z","messages":[{"author":"other","text":"hello"}]}`,
		`{"source_message_id":"11111111-1111-1111-1111-111111111111","fence":1,"lease_expires_at":"2030-01-01T00:00:00Z","messages":[{"author":"member","text":"hello"}]} {}`,
	} {
		body := body
		connection.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, body), nil
		})}
		if _, err := connection.ClaimConversation(context.Background()); err == nil {
			t.Fatalf("malformed private claim was accepted: %s", body)
		}
	}

	oversized := strings.Repeat(" ", conversation.MaxContextTextBytes*6+64*1024+1)
	connection.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, oversized), nil
	})}
	if _, err := connection.ClaimConversation(context.Background()); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized private claim error = %v", err)
	}
}

func TestMachineConversationRenewAndCommitUseExactWireWithoutBearer(t *testing.T) {
	t.Parallel()
	claim := conversation.ReplyClaim{SourceMessageID: "11111111-1111-1111-1111-111111111111", Fence: 4}
	goal := "Prepare the renewal packet"
	requests := 0
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.Header.Get("Authorization") != "" {
				t.Fatalf("private mutation used member bearer %q", request.Header.Get("Authorization"))
			}
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode private mutation request: %v", err)
			}
			switch requests {
			case 1:
				if request.URL.Path != "/v1/host/conversation-replies/"+claim.SourceMessageID+"/renew" || len(body) != 1 || body["fence"] != float64(4) {
					t.Fatalf("private renewal = %s %#v", request.URL.Path, body)
				}
				return jsonResponse(http.StatusOK, `{"lease_expires_at":"2030-01-01T00:01:00Z"}`), nil
			case 2:
				if request.URL.Path != "/v1/host/conversation-replies/"+claim.SourceMessageID+"/commit" || len(body) != 3 ||
					body["fence"] != float64(4) || body["reply"] != "I will prepare it." || body["delegation_goal"] != goal {
					t.Fatalf("private commit = %s %#v", request.URL.Path, body)
				}
				return jsonResponse(http.StatusOK, `{"reply_message_id":"22222222-2222-2222-2222-222222222222","created_work_id":"33333333-3333-3333-3333-333333333333"}`), nil
			default:
				t.Fatalf("unexpected private mutation request %d", requests)
				return nil, nil
			}
		})},
	}
	lease, err := connection.RenewConversation(context.Background(), claim)
	if err != nil || !lease.Equal(time.Date(2030, 1, 1, 0, 1, 0, 0, time.UTC)) {
		t.Fatalf("renew private claim = %s, %v", lease, err)
	}
	result, err := connection.CommitConversation(context.Background(), claim, conversation.ReplyCandidate{
		Reply: "I will prepare it.", DelegationGoal: &goal,
	})
	if err != nil {
		t.Fatalf("commit private reply: %v", err)
	}
	if result.ReplyMessageID != "22222222-2222-2222-2222-222222222222" || result.CreatedWorkID != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("private commit result = %#v", result)
	}
}

func TestMachineConversationCommitSendsExplicitNullGoal(t *testing.T) {
	t.Parallel()
	claim := conversation.ReplyClaim{SourceMessageID: "11111111-1111-1111-1111-111111111111", Fence: 4}
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode null delegation commit: %v", err)
			}
			goal, exists := body["delegation_goal"]
			if !exists || goal != nil || len(body) != 3 {
				t.Fatalf("null delegation commit body = %#v", body)
			}
			return jsonResponse(http.StatusOK, `{"reply_message_id":"22222222-2222-2222-2222-222222222222"}`), nil
		})},
	}
	if _, err := connection.CommitConversation(context.Background(), claim, conversation.ReplyCandidate{Reply: "Here are the options."}); err != nil {
		t.Fatalf("commit null private delegation: %v", err)
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
