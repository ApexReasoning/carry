package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/host"
)

func TestAdapterExecutesIsolatedPiRPCAndParsesSettledDraft(t *testing.T) {
	argumentFile := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("PI_ARGS_FILE", argumentFile)
	binary := writePiFixture(t, `
printf '%s\n' "$@" > "$PI_ARGS_FILE"
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Finance approved the term.\",\"next_step\":\"Apply legal wording.\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	adapter := New()

	update, err := adapter.Execute(context.Background(), host.ExecutionRequest{
		Goal: "Prepare the renewal brief",
	})
	if err != nil {
		t.Fatalf("execute Pi RPC: %v", err)
	}
	if update.Understanding != "Finance approved the term." || update.NextStep != "Apply legal wording." {
		t.Fatalf("Pi update = %#v", update)
	}
	arguments, err := os.ReadFile(argumentFile)
	if err != nil {
		t.Fatalf("read Pi arguments: %v", err)
	}
	for _, required := range []string{
		"--mode\nrpc", "--no-session", "--no-builtin-tools", "--no-extensions",
		"--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files",
	} {
		if !strings.Contains(string(arguments), required) {
			t.Fatalf("Pi arguments do not contain %q: %s", required, arguments)
		}
	}
}

func TestConfiguredAdapterProvidesBoundedReferenceToolExtensionOnlyForWork(t *testing.T) {
	extensionFile := filepath.Join(t.TempDir(), "reference-extension.ts")
	t.Setenv("PI_EXTENSION_FILE", extensionFile)
	binary := writePiFixture(t, `
while [ "$#" -gt 0 ]; do
  if [ "${1:-}" = "-e" ]; then
    shift
    cat "$1" > "$PI_EXTENSION_FILE"
  fi
  shift
done
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Reference was consulted.\",\"next_step\":\"Continue the Work.\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	adapter := NewWithReferenceBaseURL("https://references.example")
	if _, err := adapter.Execute(context.Background(), host.ExecutionRequest{Goal: "Use the catalog"}); err != nil {
		t.Fatalf("execute configured Pi: %v", err)
	}
	source, err := os.ReadFile(extensionFile)
	if err != nil {
		t.Fatalf("read Pi Reference extension: %v", err)
	}
	for _, expected := range []string{
		"lookup_reference", "https://references.example", `redirect: "error"`,
		"64 * 1024", "5_000", "method: \"GET\"", "ExtensionAPI",
	} {
		if !strings.Contains(string(source), expected) {
			t.Fatalf("Pi Reference extension does not contain %q", expected)
		}
	}
}

func TestPiReferenceTransportEnforcesRuntimeBounds(t *testing.T) {
	t.Run("escaped success", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests.Add(1)
			if request.Method != http.MethodGet || request.URL.EscapedPath() != "/v1/references/a%2Fb%3Fc=d" {
				t.Errorf("request = %s %s", request.Method, request.URL.EscapedPath())
				return
			}
			if request.Header.Get("Authorization") != "" || request.Header.Get("Content-Type") != "" {
				t.Errorf("credential-bearing headers = %#v", request.Header)
				return
			}
			_, _ = response.Write([]byte("Reference text: 中文"))
		}))
		defer server.Close()
		output, err := runPiReferenceTransport(t, server.URL, time.Second, `console.log(await lookupReference("a/b?c=d"));`)
		if err != nil || output != "Reference text: 中文" || requests.Load() != 1 {
			t.Fatalf("Pi transport output=%q requests=%d error=%v", output, requests.Load(), err)
		}
	})

	for name, handler := range map[string]http.HandlerFunc{
		"redirect": func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, "/other", http.StatusFound)
		},
		"oversize": func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte(strings.Repeat("x", 64*1024+1)))
		},
		"invalid utf8": func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte{0xff, 0xfe})
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			output, err := runPiReferenceTransport(t, server.URL, time.Second, `
try { await lookupReference("key"); console.log("unexpected success"); }
catch (error) { console.log(error.message); }
`)
			if err != nil || !strings.HasPrefix(output, "lookup_reference ") {
				t.Fatalf("Pi transport output=%q error=%v", output, err)
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte("partial"))
			response.(http.Flusher).Flush()
			<-request.Context().Done()
		}))
		defer server.Close()
		output, err := runPiReferenceTransport(t, server.URL, 25*time.Millisecond, `
try { await lookupReference("key"); console.log("unexpected success"); }
catch (error) { console.log(error.message); }
`)
		if err != nil || output != "lookup_reference timed out" {
			t.Fatalf("Pi timeout output=%q error=%v", output, err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		defer server.Close()
		output, err := runPiReferenceTransport(t, server.URL, time.Second, `
const controller = new AbortController();
setTimeout(() => controller.abort(), 25);
try { await lookupReference("key", controller.signal); console.log("unexpected success"); }
catch (error) { console.log(error.message); }
`)
		if err != nil || output != "lookup_reference was cancelled" {
			t.Fatalf("Pi cancellation output=%q error=%v", output, err)
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		baseURL := server.URL
		server.Close()
		output, err := runPiReferenceTransport(t, baseURL, time.Second, `
try { await lookupReference("key"); console.log("unexpected success"); }
catch (error) { console.log(error.message); }
`)
		if err != nil || output != "lookup_reference unavailable" {
			t.Fatalf("Pi unavailable output=%q error=%v", output, err)
		}
	})

	t.Run("dot segments", func(t *testing.T) {
		output, err := runPiReferenceTransport(t, "https://references.example", time.Second, `
for (const key of [".", ".."]) {
  try { await lookupReference(key); console.log("unexpected success"); }
  catch (error) { console.log(error.message); }
}
`)
		if err != nil || output != "lookup_reference key is invalid\nlookup_reference key is invalid" {
			t.Fatalf("Pi dot-key output=%q error=%v", output, err)
		}
	})
}

func TestPiReferenceFailureCannotProduceWorkUpdate(t *testing.T) {
	binary := writePiFixture(t, `
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"tool_execution_start","toolCallId":"call-reference","toolName":"lookup_reference","args":{"key":"renewal"}}' \
  '{"type":"tool_execution_end","toolCallId":"call-reference","toolName":"lookup_reference","isError":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Unverified\",\"next_step\":\"Continue\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	_, err := NewWithReferenceBaseURL("https://references.example").Execute(
		context.Background(),
		host.ExecutionRequest{Goal: "Use the catalog"},
	)
	if !errors.Is(err, host.ErrAgentFailed) {
		t.Fatalf("Pi reference failure = %v, want Agent failure", err)
	}
}

func TestPiRejectsMalformedReferenceSettlement(t *testing.T) {
	for name, records := range map[string]string{
		"missing isError": strings.Join([]string{
			`{"id":"carry-prompt","type":"response","command":"prompt","success":true}`,
			`{"type":"tool_execution_start","toolCallId":"call-1","toolName":"lookup_reference"}`,
			`{"type":"tool_execution_end","toolCallId":"call-1","toolName":"lookup_reference"}`,
		}, "\n"),
		"unmatched end": `{"type":"tool_execution_end","toolCallId":"call-1","toolName":"lookup_reference","isError":false}`,
		"incomplete call": strings.Join([]string{
			`{"type":"tool_execution_start","toolCallId":"call-1","toolName":"lookup_reference"}`,
			`{"type":"agent_settled"}`,
		}, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := awaitText(context.Background(), strings.NewReader(records+"\n"), true)
			if !errors.Is(err, host.ErrAgentOutcomeLost) {
				t.Fatalf("malformed settlement error = %v, want outcome lost", err)
			}
		})
	}
}

func TestAdapterRepliesToPrivateConversationAfterPiSettles(t *testing.T) {
	argumentFile := filepath.Join(t.TempDir(), "private-arguments")
	t.Setenv("PI_PRIVATE_ARGS_FILE", argumentFile)
	binary := writePiFixture(t, `
printf '%s\n' "$@" > "$PI_PRIVATE_ARGS_FILE"
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"reply\":\"Here are the private options.\",\"delegation_goal\":null}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	candidate, err := NewWithReferenceBaseURL("https://references.example").Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "What are my options?"}},
	})
	if err != nil {
		t.Fatalf("reply through Pi RPC: %v", err)
	}
	if candidate.Reply != "Here are the private options." || candidate.DelegationGoal != nil {
		t.Fatalf("Pi private candidate = %#v", candidate)
	}
	arguments, err := os.ReadFile(argumentFile)
	if err != nil {
		t.Fatalf("read Pi private arguments: %v", err)
	}
	for _, argument := range strings.Fields(string(arguments)) {
		if argument == "-e" {
			t.Fatalf("Pi private Reply loaded the Reference extension: %s", arguments)
		}
	}
}

func TestAdapterDoesNotExposePrivateConversationThroughPiStderr(t *testing.T) {
	binary := writePiFixture(t, `
printf '%s\n' 'PRIVATE CONVERSATION MUST NOT LEAK' >&2
IFS= read -r prompt
printf '%s\n' '{"id":"carry-prompt","type":"response","command":"prompt","success":false,"error":"generation failed"}'
`)
	usePiFixture(t, binary)
	_, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "Private question"}},
	})
	if err == nil || strings.Contains(err.Error(), "PRIVATE CONVERSATION MUST NOT LEAK") {
		t.Fatalf("Pi private failure exposed stderr: %v", err)
	}
}

func TestAdapterSanitizesPrivatePiProtocolError(t *testing.T) {
	const privateSentinel = "PRIVATE PI PROTOCOL ERROR MUST NOT LEAK"
	binary := writePiFixture(t, `
IFS= read -r prompt
printf '%s\n' '{"id":"carry-prompt","type":"response","command":"prompt","success":false,"error":"PRIVATE PI PROTOCOL ERROR MUST NOT LEAK"}'
`)
	usePiFixture(t, binary)
	_, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "Private question"}},
	})
	if !errors.Is(err, host.ErrAgentFailed) {
		t.Fatalf("Pi private protocol error category = %v", err)
	}
	if strings.Contains(err.Error(), privateSentinel) {
		t.Fatalf("Pi private failure exposed protocol error: %v", err)
	}
}

func TestAdapterKeepsPiOutcomeUnknownWithoutAgentSettled(t *testing.T) {
	binary := writePiFixture(t, `
IFS= read -r prompt
printf '%s\n' '{"id":"carry-prompt","type":"response","command":"prompt","success":true}'
`)
	usePiFixture(t, binary)
	_, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Prepare the renewal brief"})
	if !errors.Is(err, host.ErrAgentOutcomeLost) {
		t.Fatalf("Pi missing settled error = %v", err)
	}
}

func TestAdapterRejectsPiErrorStopReason(t *testing.T) {
	binary := writePiFixture(t, `
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"failed"}],"stopReason":"error"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	_, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Prepare the renewal brief"})
	if !errors.Is(err, host.ErrAgentFailed) {
		t.Fatalf("Pi error stop reason = %v", err)
	}
}

func runPiReferenceTransport(t *testing.T, baseURL string, timeout time.Duration, script string) (string, error) {
	t.Helper()
	encodedBaseURL, err := json.Marshal(baseURL)
	if err != nil {
		return "", err
	}
	program := fmt.Sprintf(
		"const BASE_URL = %s;\nconst MAX_RESPONSE_BYTES = 64 * 1024;\nconst REQUEST_TIMEOUT_MS = %d;\n",
		encodedBaseURL,
		max(timeout.Milliseconds(), 1),
	) + referenceTransportSource + "\n" + script + "\n"
	path := filepath.Join(t.TempDir(), "reference-transport.ts")
	if err := os.WriteFile(path, []byte(program), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "node", "--no-warnings", path).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf("execute Pi reference transport: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func writePiFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatalf("write Pi fixture: %v", err)
	}
	return path
}

func usePiFixture(t *testing.T, binary string) {
	t.Helper()
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
}
