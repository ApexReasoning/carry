package host

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
)

func TestMachineConnectionResponsesUseExactSnakeCaseWire(t *testing.T) {
	t.Parallel()
	beginResponse := &http.Response{
		StatusCode: http.StatusCreated,
		Body: io.NopCloser(strings.NewReader(`{
			"request_id":"11111111-1111-4111-8111-111111111111",
			"display_name":"Desk Mac","user_code":"BCDF-GHJ-KLM","poll_secret":"poll",
			"fingerprint":"SHA256:exact","verification_path":"/machine-connect",
			"expires_at":"2026-08-21T00:15:00Z","interval_seconds":5
		}`)),
	}
	var begun begunConnection
	if err := decodeConnectionResponse(beginResponse, &begun); err != nil || begun.RequestID == "" || begun.VerificationPath != "/machine-connect" {
		t.Fatalf("decode begin = %#v, %v", begun, err)
	}
	pollResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"machine_id":"22222222-2222-4222-8222-222222222222",
			"space_id":"33333333-3333-4333-8333-333333333333","display_name":"Desk Mac",
			"certificate_pem":"certificate","redeemed_at":"2026-08-21T00:05:00Z",
			"replay_until":"2026-08-21T00:20:00Z"
		}`)),
	}
	var connected connectedMachine
	if err := decodeConnectionResponse(pollResponse, &connected); err != nil || connected.MachineID == "" || connected.CertificatePEM != "certificate" {
		t.Fatalf("decode poll = %#v, %v", connected, err)
	}
}

func TestMachineConnectionPollRetriesOnlyTransientTransportFailures(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://carry.example")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name          string
		transport     http.RoundTripper
		wantTransient bool
	}{
		{
			name: "connection reset",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
			}),
			wantTransient: true,
		},
		{
			name: "expired TLS certificate",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, &tls.CertificateVerificationError{Err: x509.CertificateInvalidError{Reason: x509.Expired}}
			}),
		},
		{
			name: "fatal TLS alert",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tls.AlertError(42)
			}),
		},
		{
			name: "unknown protocol failure",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("unclassified transport protocol failure")
			}),
		},
		{
			name: "malformed successful JSON",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{not-json`), nil
			}),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := connectionClient{origin: origin, client: &http.Client{Transport: testCase.transport}}
			_, pollErr := client.poll(context.Background(), "carry_machine_connect_11111111-1111-4111-8111-111111111111.secret")
			var transient *transientConnectionError
			if got := errors.As(pollErr, &transient); got != testCase.wantTransient {
				t.Fatalf("poll error = %v, transient = %t", pollErr, got)
			}
		})
	}
}

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

func TestMachineHTTPClassifiesOnlyTemporaryFailuresForWorkerRetry(t *testing.T) {
	t.Parallel()

	request, err := newJSONRequest(context.Background(), http.MethodPost, "https://carry.example.com/v1/host/runs/claim", struct{}{})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for name, transport := range map[string]http.RoundTripper{
		"network operation": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
		}),
		"request timeout": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
		"server": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusServiceUnavailable, `{"error":"temporarily unavailable"}`), nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			err := sendJSON(&http.Client{Transport: transport}, request.Clone(context.Background()), nil)
			if !errors.Is(err, hostdomain.ErrControlPlaneUnavailable) {
				t.Fatalf("temporary error = %v", err)
			}
		})
	}
	forbidden := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusForbidden, `{"error":"Machine is revoked"}`), nil
	})
	if err := sendJSON(&http.Client{Transport: forbidden}, request.Clone(context.Background()), nil); err == nil ||
		errors.Is(err, hostdomain.ErrControlPlaneUnavailable) {
		t.Fatalf("authority error = %v", err)
	}
	for name, transport := range map[string]http.RoundTripper{
		"invalid certificate": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &tls.CertificateVerificationError{Err: x509.CertificateInvalidError{Reason: x509.Expired}}
		}),
		"peer TLS rejection": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, tls.AlertError(42)
		}),
		"wrong TLS protocol": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, tls.RecordHeaderError{Msg: "server gave HTTP response to HTTPS client"}
		}),
		"unknown protocol failure": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unclassified transport protocol failure")
		}),
	} {
		t.Run(name, func(t *testing.T) {
			err := sendJSON(&http.Client{Transport: transport}, request.Clone(context.Background()), nil)
			if err == nil || errors.Is(err, hostdomain.ErrControlPlaneUnavailable) {
				t.Fatalf("terminal TLS error = %v", err)
			}
		})
	}
}

func TestMachineClaimUsesOnlyMTLSClientAndReturnsCompleteContext(t *testing.T) {
	t.Parallel()
	const (
		runID     = "8b673a9d-71ce-4d4f-babc-9124a020bf11"
		attemptID = "d10d356f-257a-46bd-b68d-b340e3e7bbc9"
		workID    = "53c97fa6-4d78-4fd0-82b4-6cd0419c876d"
		memberID  = "29b83126-8337-431c-a568-8bfda5670501"
	)

	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/host/runs/claim" || request.Header.Get("Authorization") != "" {
				t.Fatalf("Machine claim request = %s, Authorization %q", request.URL, request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{
				"run_id":"`+runID+`","attempt_id":"`+attemptID+`","work_id":"`+workID+`",
				"fence":2,"lease_expires_at":"2030-01-01T00:00:00Z","goal":"Prepare renewal",
				"current_understanding":"Finance approved","current_next_step":"Apply wording",
				"base_understanding_version":3,"input_end_seq":5,
				"messages":[{"author_user_id":"`+memberID+`","text":"Legal supplied wording"}]
			}`), nil
		})},
	}

	claim, err := connection.Claim(context.Background())
	if err != nil {
		t.Fatalf("claim Run: %v", err)
	}
	if claim.RunID != runID || claim.BaseUnderstandingVersion != 3 ||
		len(claim.Messages) != 1 || claim.Messages[0].Text != "Legal supplied wording" {
		t.Fatalf("claim = %#v", claim)
	}
}

func TestMachineClaimAcceptsWorstCaseBoundedEscapedContext(t *testing.T) {
	t.Parallel()
	messages := make([]runMessageWire, 0, run.MaxInputMessages)
	for range run.MaxInputMessages {
		messages = append(messages, runMessageWire{
			AuthorUserID: "29b83126-8337-431c-a568-8bfda5670501",
			Text:         strings.Repeat("\x01", run.MaxInputTextBytes/run.MaxInputMessages),
		})
	}
	encoded, err := json.Marshal(runClaimWire{
		RunID: "8b673a9d-71ce-4d4f-babc-9124a020bf11", AttemptID: "d10d356f-257a-46bd-b68d-b340e3e7bbc9",
		WorkID: "53c97fa6-4d78-4fd0-82b4-6cd0419c876d", Fence: 2,
		LeaseExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Goal:           strings.Repeat("\x01", 2000), CurrentUnderstanding: strings.Repeat("\x01", run.MaxUnderstandingBytes),
		CurrentNextStep: strings.Repeat("\x01", run.MaxNextStepBytes), BaseUnderstandingVersion: 3,
		InputEndSeq: 33, Messages: messages,
	})
	if err != nil {
		t.Fatalf("encode worst-case Run claim: %v", err)
	}
	if len(encoded) >= maxRunClaimWireBytes {
		t.Fatalf("worst-case Run claim bytes = %d, limit = %d", len(encoded), maxRunClaimWireBytes)
	}
	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, string(encoded)), nil
		})},
	}
	claim, err := connection.Claim(context.Background())
	if err != nil {
		t.Fatalf("decode worst-case Run claim: %v", err)
	}
	if len(claim.Messages) != run.MaxInputMessages {
		t.Fatalf("decoded message count = %d", len(claim.Messages))
	}
}

func TestMachineClaimRejectsUnboundedOrInexactJSON(t *testing.T) {
	t.Parallel()
	valid := `{
		"run_id":"8b673a9d-71ce-4d4f-babc-9124a020bf11",
		"attempt_id":"d10d356f-257a-46bd-b68d-b340e3e7bbc9",
		"work_id":"53c97fa6-4d78-4fd0-82b4-6cd0419c876d","fence":1,
		"lease_expires_at":"2030-01-01T00:00:00Z","goal":"Prepare renewal",
		"current_understanding":"","current_next_step":"","base_understanding_version":0,
		"input_end_seq":1,"messages":[]
	}`
	cases := map[string]string{
		"unknown field":   strings.Replace(valid, `"messages":[]`, `"messages":[],"space_id":"forbidden"`, 1),
		"trailing JSON":   valid + `{}`,
		"over byte limit": strings.Repeat(" ", maxRunClaimWireBytes+1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			connection := machineHTTP{
				serverURL: "https://carry.example.com",
				client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return jsonResponse(http.StatusOK, body), nil
				})},
			}
			if _, err := connection.Claim(context.Background()); err == nil {
				t.Fatal("invalid Run claim was accepted")
			}
		})
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
