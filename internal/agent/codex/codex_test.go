package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/agent/reference"
	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/host"
)

func TestAdapterExecutesToollessCodexTurnAndParsesCompletedDraft(t *testing.T) {
	argumentFile := filepath.Join(t.TempDir(), "arguments")
	threadFile := filepath.Join(t.TempDir(), "thread-start")
	t.Setenv("CODEX_ARGS_FILE", argumentFile)
	t.Setenv("CODEX_TOOLLESS_THREAD_FILE", threadFile)
	binary := writeCodexFixture(t, `
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'codex-cli 0.148.0'
  exit 0
fi
printf '%s\n' "$@" > "$CODEX_ARGS_FILE"
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '%s\n' "$thread_start" > "$CODEX_TOOLLESS_THREAD_FILE"
printf '{"id":2,"result":{"thread":{"id":"thread-1","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}' \
  '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1,"item":{"id":"message-1","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Finance approved the term.\",\"next_step\":\"Apply legal wording.\"}"}}}' \
  '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
`)
	useCodexFixture(t, binary)
	adapter := New()
	if err := adapter.Diagnose(context.Background()); err != nil {
		t.Fatalf("diagnose Codex: %v", err)
	}

	update, err := adapter.Execute(context.Background(), host.ExecutionRequest{
		Goal: "Prepare the renewal brief",
	})
	if err != nil {
		t.Fatalf("execute Codex app-server: %v", err)
	}
	if update.Understanding != "Finance approved the term." || update.NextStep != "Apply legal wording." {
		t.Fatalf("Codex update = %#v", update)
	}
	arguments, err := os.ReadFile(argumentFile)
	if err != nil {
		t.Fatalf("read Codex arguments: %v", err)
	}
	for _, required := range []string{
		"--disable\nshell_tool", "--disable\nunified_exec", "--disable\nplugins",
		"mcp_servers={}", "app-server", "--stdio",
	} {
		if !strings.Contains(string(arguments), required) {
			t.Fatalf("Codex arguments do not contain %q: %s", required, arguments)
		}
	}
	threadData, err := os.ReadFile(threadFile)
	if err != nil {
		t.Fatalf("read toolless thread/start: %v", err)
	}
	var threadRequest struct {
		Params struct {
			BaseInstructions      string            `json:"baseInstructions"`
			DeveloperInstructions string            `json:"developerInstructions"`
			DynamicTools          []dynamicToolSpec `json:"dynamicTools"`
		} `json:"params"`
	}
	if json.Unmarshal(threadData, &threadRequest) != nil ||
		len(threadRequest.Params.DynamicTools) != 0 ||
		threadRequest.Params.BaseInstructions != baseInstructions ||
		threadRequest.Params.DeveloperInstructions != developerInstructions {
		t.Fatalf("unconfigured Codex thread was not toolless: %#v", threadRequest.Params)
	}
}

func TestConfiguredAdapterUsesBoundedReferenceDynamicTool(t *testing.T) {
	referenceServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.EscapedPath() != "/v1/references/renewal" {
			t.Fatalf("reference request = %s %s", request.Method, request.URL.EscapedPath())
		}
		_, _ = response.Write([]byte("Reference says renewals require owner review."))
	}))
	defer referenceServer.Close()
	toolResponseFile := filepath.Join(t.TempDir(), "tool-response")
	t.Setenv("CODEX_TOOL_RESPONSE_FILE", toolResponseFile)
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' "$initialize" > "$CODEX_INITIALIZE_FILE"
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '%s\n' "$thread_start" > "$CODEX_THREAD_START_FILE"
printf '{"id":2,"result":{"thread":{"id":"thread-reference","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-reference","status":"inProgress","items":[]}}}'
printf '%s\n' '{"id":10,"method":"item/tool/call","params":{"threadId":"thread-reference","turnId":"turn-reference","callId":"call-reference","namespace":null,"tool":"lookup_reference","arguments":{"key":"renewal"}}}'
IFS= read -r tool_response
printf '%s\n' "$tool_response" > "$CODEX_TOOL_RESPONSE_FILE"
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-reference","turnId":"turn-reference","item":{"id":"message-reference","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Reference was consulted.\",\"next_step\":\"Continue the Work.\"}"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-reference","turn":{"id":"turn-reference","status":"completed","items":[]}}}'
`)
	t.Setenv("CODEX_INITIALIZE_FILE", filepath.Join(t.TempDir(), "initialize"))
	t.Setenv("CODEX_THREAD_START_FILE", filepath.Join(t.TempDir(), "thread-start"))
	useCodexFixture(t, binary)
	adapter := NewWithReferenceBaseURL(referenceServer.URL)
	update, err := adapter.Execute(context.Background(), host.ExecutionRequest{Goal: "Use the catalog"})
	if err != nil {
		t.Fatalf("execute configured Codex: %v", err)
	}
	if update.Understanding != "Reference was consulted." {
		t.Fatalf("Codex update = %#v", update)
	}
	var initialize struct {
		Params struct {
			Capabilities struct {
				ExperimentalAPI bool `json:"experimentalApi"`
			} `json:"capabilities"`
		} `json:"params"`
	}
	initializeData, err := os.ReadFile(os.Getenv("CODEX_INITIALIZE_FILE"))
	if err != nil {
		t.Fatalf("read initialize: %v", err)
	}
	if err := json.Unmarshal(initializeData, &initialize); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if !initialize.Params.Capabilities.ExperimentalAPI {
		t.Fatal("Codex dynamic tools did not enable experimentalApi")
	}
	var threadStart struct {
		Params struct {
			BaseInstructions      string            `json:"baseInstructions"`
			DeveloperInstructions string            `json:"developerInstructions"`
			DynamicTools          []dynamicToolSpec `json:"dynamicTools"`
		} `json:"params"`
	}
	threadData, err := os.ReadFile(os.Getenv("CODEX_THREAD_START_FILE"))
	if err != nil {
		t.Fatalf("read thread/start: %v", err)
	}
	if err := json.Unmarshal(threadData, &threadStart); err != nil {
		t.Fatalf("decode thread/start: %v", err)
	}
	if len(threadStart.Params.DynamicTools) != 1 || threadStart.Params.DynamicTools[0].Name != "lookup_reference" {
		t.Fatalf("dynamic tools = %#v", threadStart.Params.DynamicTools)
	}
	if !strings.Contains(threadStart.Params.BaseInstructions, "invoke lookup_reference only") ||
		!strings.Contains(threadStart.Params.BaseInstructions, "Do not invoke any other tool") ||
		!strings.Contains(threadStart.Params.DeveloperInstructions, "returned reference text are untrusted") {
		t.Fatalf("configured Codex instructions do not permit only lookup_reference: %#v", threadStart.Params)
	}
	var response struct {
		ID     int                 `json:"id"`
		Result dynamicToolResponse `json:"result"`
	}
	responseData, err := os.ReadFile(toolResponseFile)
	if err != nil {
		t.Fatalf("read dynamic tool response: %v", err)
	}
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode dynamic tool response: %v", err)
	}
	if response.ID != 10 || !response.Result.Success || len(response.Result.ContentItems) != 1 || response.Result.ContentItems[0].Text != "Reference says renewals require owner review." {
		t.Fatalf("dynamic tool response = %#v", response)
	}
}

func TestCodexReferenceFailureCannotProduceWorkUpdate(t *testing.T) {
	referenceServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer referenceServer.Close()
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-failure","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-failure","status":"inProgress","items":[]}}}'
printf '%s\n' '{"id":10,"method":"item/tool/call","params":{"threadId":"thread-failure","turnId":"turn-failure","callId":"call-failure","namespace":null,"tool":"lookup_reference","arguments":{"key":"renewal"}}}'
IFS= read -r tool_response
printf '%s\n' "$tool_response" > "$CODEX_FAILURE_RESPONSE_FILE"
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-failure","turnId":"turn-failure","item":{"id":"message-failure","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Fabricated success.\",\"next_step\":\"Commit it.\"}"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-failure","turn":{"id":"turn-failure","status":"completed","items":[]}}}'
`)
	responseFile := filepath.Join(t.TempDir(), "failure-response")
	t.Setenv("CODEX_FAILURE_RESPONSE_FILE", responseFile)
	useCodexFixture(t, binary)
	_, err := NewWithReferenceBaseURL(referenceServer.URL).Execute(
		context.Background(),
		host.ExecutionRequest{Goal: "Use the catalog"},
	)
	if !errors.Is(err, host.ErrAgentFailed) {
		t.Fatalf("Codex reference failure = %v, want Agent failure", err)
	}
	responseData, readErr := os.ReadFile(responseFile)
	if readErr != nil {
		t.Fatalf("read failed tool response: %v", readErr)
	}
	var response struct {
		Result dynamicToolResponse `json:"result"`
	}
	if json.Unmarshal(responseData, &response) != nil || response.Result.Success {
		t.Fatalf("failed Codex tool response = %s", responseData)
	}
}

func TestCodexRejectsEarlyReferenceToolCall(t *testing.T) {
	var catalogRequests atomic.Int32
	referenceServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		catalogRequests.Add(1)
		_, _ = response.Write([]byte("unexpected"))
	}))
	defer referenceServer.Close()
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-early","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":10,"method":"item/tool/call","params":{"threadId":"thread-early","turnId":"turn-early","callId":"call-early","namespace":null,"tool":"lookup_reference","arguments":{"key":"renewal"}}}' \
  '{"id":3,"result":{"turn":{"id":"turn-early","status":"inProgress","items":[]}}}' \
  '{"method":"item/completed","params":{"threadId":"thread-early","turnId":"turn-early","item":{"id":"message-early","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Fabricated success.\",\"next_step\":\"Commit it.\"}"}}}' \
  '{"method":"turn/completed","params":{"threadId":"thread-early","turn":{"id":"turn-early","status":"completed","items":[]}}}'
`)
	useCodexFixture(t, binary)
	_, err := NewWithReferenceBaseURL(referenceServer.URL).Execute(
		context.Background(),
		host.ExecutionRequest{Goal: "Use the catalog"},
	)
	if !errors.Is(err, host.ErrAgentOutcomeLost) {
		t.Fatalf("early Codex tool call = %v, want outcome lost", err)
	}
	if got := catalogRequests.Load(); got != 0 {
		t.Fatalf("early Codex tool call reached catalog %d times", got)
	}
}

func TestCodexRejectsMalformedReferenceToolCall(t *testing.T) {
	var output bytes.Buffer
	client := newAppServerClient(&output, strings.NewReader(""), func(context.Context, string) (string, error) {
		t.Fatal("malformed call reached Reference Catalog")
		return "", nil
	})
	message := envelope{
		ID:     json.RawMessage("10"),
		Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","callId":"call","namespace":null,"tool":"lookup_reference","arguments":{"key":"renewal","url":"https://attacker.example"}}`),
	}
	if err := client.answerReferenceTool(context.Background(), message, "thread", "turn"); err != nil {
		t.Fatalf("answer malformed tool call: %v", err)
	}
	if !client.referenceFailure {
		t.Fatal("malformed Codex tool call was not retained as execution failure")
	}
	var response struct {
		Result dynamicToolResponse `json:"result"`
	}
	if json.Unmarshal(output.Bytes(), &response) != nil || response.Result.Success {
		t.Fatalf("malformed Codex tool response = %s", output.Bytes())
	}
}

func TestCodexRejectsDuplicateReferenceCallID(t *testing.T) {
	var output bytes.Buffer
	catalogRequests := 0
	client := newAppServerClient(&output, strings.NewReader(""), func(context.Context, string) (string, error) {
		catalogRequests++
		return "reference", nil
	})
	message := envelope{
		ID:     json.RawMessage("10"),
		Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","callId":"call","namespace":null,"tool":"lookup_reference","arguments":{"key":"renewal"}}`),
	}
	if err := client.answerReferenceTool(context.Background(), message, "thread", "turn"); err != nil {
		t.Fatalf("answer first tool call: %v", err)
	}
	message.ID = json.RawMessage("11")
	if err := client.answerReferenceTool(context.Background(), message, "thread", "turn"); err != nil {
		t.Fatalf("answer duplicate tool call: %v", err)
	}
	if catalogRequests != 1 || !client.referenceFailure {
		t.Fatalf("duplicate call requests=%d failure=%t", catalogRequests, client.referenceFailure)
	}
}

func TestCodexRejectsMalformedReferenceUnicode(t *testing.T) {
	invalidUTF8 := append([]byte(`{"key":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	for name, arguments := range map[string][]byte{
		"invalid utf8":        invalidUTF8,
		"lone high surrogate": []byte(`{"key":"\uD800"}`),
		"lone low surrogate":  []byte(`{"key":"\uDC00"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeReferenceArguments(arguments); !errors.Is(err, reference.ErrInvalidKey) {
				t.Fatalf("decode malformed Unicode = %v, want invalid key", err)
			}
		})
	}
	key, err := decodeReferenceArguments([]byte(`{"key":"\uD83D\uDE00"}`))
	if err != nil || key != "😀" {
		t.Fatalf("decode valid surrogate pair = %q, %v", key, err)
	}
}

func TestAdapterSelectsPrivateReplySchemaForCodexTurn(t *testing.T) {
	turnFile := filepath.Join(t.TempDir(), "turn-start.json")
	threadFile := filepath.Join(t.TempDir(), "thread-start.json")
	t.Setenv("CODEX_TURN_FILE", turnFile)
	t.Setenv("CODEX_PRIVATE_THREAD_FILE", threadFile)
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '%s\n' "$thread_start" > "$CODEX_PRIVATE_THREAD_FILE"
printf '{"id":2,"result":{"thread":{"id":"thread-private","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' "$turn_start" > "$CODEX_TURN_FILE"
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-private","status":"inProgress","items":[]}}}' \
  '{"method":"item/completed","params":{"threadId":"thread-private","turnId":"turn-private","item":{"id":"message-private","type":"agentMessage","phase":"final_answer","text":"{\"reply\":\"Here are the private options.\",\"delegation_goal\":null}"}}}' \
  '{"method":"turn/completed","params":{"threadId":"thread-private","turn":{"id":"turn-private","status":"completed","items":[]}}}'
`)
	useCodexFixture(t, binary)
	candidate, err := NewWithReferenceBaseURL("https://references.example").Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "What are my options?"}},
	})
	if err != nil {
		t.Fatalf("reply through Codex app-server: %v", err)
	}
	if candidate.Reply != "Here are the private options." || candidate.DelegationGoal != nil {
		t.Fatalf("Codex private candidate = %#v", candidate)
	}
	turnData, err := os.ReadFile(turnFile)
	if err != nil {
		t.Fatalf("read Codex private turn/start: %v", err)
	}
	var turnRequest struct {
		Params struct {
			OutputSchema struct {
				AdditionalProperties bool     `json:"additionalProperties"`
				Required             []string `json:"required"`
			} `json:"outputSchema"`
		} `json:"params"`
	}
	if err := json.Unmarshal(turnData, &turnRequest); err != nil {
		t.Fatalf("decode Codex private turn/start: %v", err)
	}
	if turnRequest.Params.OutputSchema.AdditionalProperties ||
		!sameStrings(turnRequest.Params.OutputSchema.Required, []string{"reply", "delegation_goal"}) {
		t.Fatalf("Codex private output schema = %#v", turnRequest.Params.OutputSchema)
	}
	threadData, err := os.ReadFile(threadFile)
	if err != nil {
		t.Fatalf("read Codex private thread/start: %v", err)
	}
	var threadRequest struct {
		Params struct {
			BaseInstructions      string            `json:"baseInstructions"`
			DeveloperInstructions string            `json:"developerInstructions"`
			DynamicTools          []dynamicToolSpec `json:"dynamicTools"`
		} `json:"params"`
	}
	if json.Unmarshal(threadData, &threadRequest) != nil ||
		len(threadRequest.Params.DynamicTools) != 0 ||
		threadRequest.Params.BaseInstructions != baseInstructions ||
		threadRequest.Params.DeveloperInstructions != developerInstructions {
		t.Fatalf("Codex private thread exposed Reference capability: %#v", threadRequest.Params)
	}
}

func TestAdapterDoesNotExposePrivateConversationThroughCodexStderr(t *testing.T) {
	binary := writeCodexFixture(t, `
printf '%s\n' 'PRIVATE CONVERSATION MUST NOT LEAK' >&2
	exit 1
`)
	useCodexFixture(t, binary)
	_, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "Private question"}},
	})
	if err == nil || strings.Contains(err.Error(), "PRIVATE CONVERSATION MUST NOT LEAK") {
		t.Fatalf("Codex private failure exposed stderr: %v", err)
	}
}

func TestAdapterSanitizesPrivateCodexProtocolError(t *testing.T) {
	const privateSentinel = "PRIVATE CODEX PROTOCOL ERROR MUST NOT LEAK"
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-private-error","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' '{"id":3,"error":{"code":-1,"message":"PRIVATE CODEX PROTOCOL ERROR MUST NOT LEAK"}}'
`)
	useCodexFixture(t, binary)
	_, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "Private question"}},
	})
	if !errors.Is(err, host.ErrAgentFailed) {
		t.Fatalf("Codex private protocol error category = %v", err)
	}
	if strings.Contains(err.Error(), privateSentinel) {
		t.Fatalf("Codex private failure exposed protocol error: %v", err)
	}
}

func TestAdapterReconcilesMissingCodexTurnCompletedWithThreadRead(t *testing.T) {
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-2","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-2","status":"inProgress","items":[]}}}' \
  '{"method":"item/agentMessage/delta","params":{"threadId":"thread-2","turnId":"turn-2","itemId":"message-2","delta":"{\"understanding\":\"The options are now comparable.\",\"next_step\":\"Ask the owner to choose.\"}"}}' \
  '{"method":"thread/status/changed","params":{"threadId":"thread-2","status":{"type":"idle"}}}'
IFS= read -r thread_read
printf '%s\n' '{"id":4,"result":{"thread":{"turns":[{"id":"turn-2","status":"completed","items":[]}]}}}'
`)
	useCodexFixture(t, binary)
	draft, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Compare onboarding options"})
	if err != nil {
		t.Fatalf("reconcile Codex turn: %v", err)
	}
	if draft.Understanding != "The options are now comparable." || draft.NextStep != "Ask the owner to choose." {
		t.Fatalf("reconciled draft = %#v", draft)
	}
}

func TestAdapterClassifiesReconciledCodexTurnStatuses(t *testing.T) {
	tests := []struct {
		status    string
		wantError error
	}{
		{status: "inProgress", wantError: host.ErrAgentOutcomeLost},
		{status: "futureStatus", wantError: host.ErrAgentOutcomeLost},
		{status: "failed", wantError: host.ErrAgentFailed},
		{status: "interrupted", wantError: host.ErrAgentFailed},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			fixture := strings.ReplaceAll(`
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-3","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-3","status":"inProgress","items":[]}}}' \
  '{"method":"item/agentMessage/delta","params":{"threadId":"thread-3","turnId":"turn-3","itemId":"message-3","delta":"{\"understanding\":\"Unconfirmed\",\"next_step\":\"Wait\"}"}}' \
  '{"method":"thread/status/changed","params":{"threadId":"thread-3","status":{"type":"idle"}}}'
IFS= read -r thread_read
printf '%s\n' '{"id":4,"result":{"thread":{"turns":[{"id":"turn-3","status":"RECONCILED_STATUS","items":[]}]}}}'
`, "RECONCILED_STATUS", test.status)
			binary := writeCodexFixture(t, fixture)
			useCodexFixture(t, binary)
			_, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Compare onboarding options"})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("reconciled Codex turn status %q error = %v, want %v", test.status, err, test.wantError)
			}
		})
	}
}

func TestAdapterReconcilesIdleBeforeAnyCodexText(t *testing.T) {
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-idle","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-idle","status":"inProgress","items":[]}}}' \
  '{"method":"thread/status/changed","params":{"threadId":"thread-idle","status":{"type":"idle"}}}'
IFS= read -r thread_read
printf '%s\n' '{"id":4,"result":{"thread":{"turns":[{"id":"turn-idle","status":"completed","items":[{"id":"message-idle","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"The idle turn is complete.\",\"next_step\":\"Continue from durable Work.\"}"}]}]}}}'
`)
	useCodexFixture(t, binary)
	draft, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Confirm idle reconciliation"})
	if err != nil {
		t.Fatalf("reconcile idle Codex turn: %v", err)
	}
	if draft.Understanding != "The idle turn is complete." {
		t.Fatalf("idle reconciled draft = %#v", draft)
	}
}

func TestAdapterBoundsUnansweredCodexThreadRead(t *testing.T) {
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-timeout","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-timeout","status":"inProgress","items":[]}}}' \
  '{"method":"thread/status/changed","params":{"threadId":"thread-timeout","status":{"type":"idle"}}}'
IFS= read -r thread_read
sleep 10
`)
	useCodexFixture(t, binary)
	startedAt := time.Now()
	_, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Bound reconciliation"})
	if !errors.Is(err, host.ErrAgentOutcomeLost) {
		t.Fatalf("unanswered Codex thread/read error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
		t.Fatalf("unanswered Codex thread/read took %s", elapsed)
	}
}

func writeCodexFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatalf("write Codex fixture: %v", err)
	}
	return path
}

func useCodexFixture(t *testing.T, binary string) {
	t.Helper()
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
