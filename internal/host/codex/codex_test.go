package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/host"
)

func TestAdapterObservesOnlyDiscoveredCodexDefault(t *testing.T) {
	adapter := New()
	if adapter.Key() != "codex" {
		t.Fatalf("Codex adapter key = %q", adapter.Key())
	}

	t.Setenv("PATH", t.TempDir())
	occurrences, err := adapter.Observe(context.Background())
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("uninstalled Codex observation = %#v, error = %v", occurrences, err)
	}

	unhealthy := writeCodexFixture(t, `
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'codex-cli 0.147.0'
  exit 0
fi
`)
	t.Setenv("PATH", filepath.Dir(unhealthy))
	occurrences, err = adapter.Observe(context.Background())
	if err != nil || len(occurrences) != 1 || occurrences[0].Key != "default" ||
		occurrences[0].Present || occurrences[0].Executor != nil {
		t.Fatalf("unhealthy Codex observation = %#v, error = %v", occurrences, err)
	}

	healthy := writeCodexFixture(t, `
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'codex-cli 0.148.0'
  exit 0
fi
`)
	t.Setenv("PATH", filepath.Dir(healthy))
	occurrences, err = adapter.Observe(context.Background())
	if err != nil || len(occurrences) != 1 || !occurrences[0].Present || occurrences[0].Executor != adapter {
		t.Fatalf("healthy Codex observation = %#v, error = %v", occurrences, err)
	}
}

func TestTurnStartWriteFailureKeepsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	client := newAppServerClient(failingWriter{}, strings.NewReader(""))
	_, err := startStructuredTurnProtocol(
		client,
		context.Background(),
		"thread-1",
		"Advance the Work",
		host.UnderstandingOutputSchema,
	)
	if !errors.Is(err, host.ErrAgentOutcomeLost) {
		t.Fatalf("turn/start write error = %v, want outcome lost", err)
	}
}

func TestAdapterExecutesToollessCodexTurnAndParsesCompletedDraft(t *testing.T) {
	argumentFile := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("CODEX_ARGS_FILE", argumentFile)
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
printf '{"id":2,"result":{"thread":{"id":"thread-1","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}' \
  '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1,"item":{"id":"message-1","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Finance approved the term.\",\"next_step\":\"Apply legal wording.\",\"review_required\":false}"}}}' \
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
}

func TestAdapterSelectsPrivateReplySchemaForCodexTurn(t *testing.T) {
	turnFile := filepath.Join(t.TempDir(), "turn-start.json")
	t.Setenv("CODEX_TURN_FILE", turnFile)
	binary := writeCodexFixture(t, `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-private","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' "$turn_start" > "$CODEX_TURN_FILE"
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-private","status":"inProgress","items":[]}}}' \
  '{"method":"item/completed","params":{"threadId":"thread-private","turnId":"turn-private","item":{"id":"message-private","type":"agentMessage","phase":"final_answer","text":"{\"reply\":\"Here are the private options.\",\"delegation_goal\":null}"}}}' \
  '{"method":"turn/completed","params":{"threadId":"thread-private","turn":{"id":"turn-private","status":"completed","items":[]}}}'
`)
	useCodexFixture(t, binary)
	candidate, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember,
			Text: "What are my options?"}},
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
}

func TestAdapterDoesNotExposePrivateConversationThroughCodexStderr(t *testing.T) {
	binary := writeCodexFixture(t, `
printf '%s\n' 'PRIVATE CONVERSATION MUST NOT LEAK' >&2
	exit 1
`)
	useCodexFixture(t, binary)
	_, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember,
			Text: "Private question"}},
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
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember,
			Text: "Private question"}},
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
  '{"method":"item/agentMessage/delta","params":{"threadId":"thread-2","turnId":"turn-2","itemId":"message-2","delta":"{\"understanding\":\"The options are now comparable.\",\"next_step\":\"Ask the owner to choose.\",\"review_required\":true}"}}' \
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
		{status: "inProgress",
			wantError: host.ErrAgentOutcomeLost},
		{status: "futureStatus",
			wantError: host.ErrAgentOutcomeLost},
		{status: "failed",
			wantError: host.ErrAgentFailed},
		{status: "interrupted",
			wantError: host.ErrAgentFailed},
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
  '{"method":"item/agentMessage/delta","params":{"threadId":"thread-3","turnId":"turn-3","itemId":"message-3","delta":"{\"understanding\":\"Unconfirmed\",\"next_step\":\"Wait\",\"review_required\":false}"}}' \
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
printf '%s\n' '{"id":4,"result":{"thread":{"turns":[{"id":"turn-idle","status":"completed","items":[{"id":"message-idle","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"The idle turn is complete.\",\"next_step\":\"Continue from durable Work.\",\"review_required\":false}"}]}]}}}'
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

func TestThreadIsolationReportsEachMissingGuarantee(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	valid := func() threadStartResult {
		var started threadStartResult
		started.Thread.ID = "thread-isolated"
		started.Thread.Ephemeral = true
		started.Thread.CWD = cwd
		started.RuntimeWorkspaceRoots = []string{cwd}
		started.ApprovalPolicy = "never"
		started.Sandbox.Type = "readOnly"
		return started
	}
	for _, test := range []struct {
		name   string
		mutate func(*threadStartResult)
		want   string
	}{
		{name: "identity",
			mutate: func(result *threadStartResult) { result.Thread.ID = "" },
			want:   "identity"},
		{name: "ephemeral",
			mutate: func(result *threadStartResult) { result.Thread.Ephemeral = false },
			want:   "ephemeral"},
		{name: "working directory",
			mutate: func(result *threadStartResult) { result.Thread.CWD = t.TempDir() },
			want:   "working directory"},
		{name: "Git authority",
			mutate: func(result *threadStartResult) { result.Thread.GitInfo = struct{}{} },
			want:   "Git repository authority"},
		{name: "workspace count",
			mutate: func(result *threadStartResult) { result.RuntimeWorkspaceRoots = nil },
			want:   "one isolated workspace root"},
		{name: "workspace identity",
			mutate: func(result *threadStartResult) { result.RuntimeWorkspaceRoots[0] = t.TempDir() },
			want:   "workspace root differs"},
		{name: "instructions",
			mutate: func(result *threadStartResult) { result.InstructionSources = []json.RawMessage{json.RawMessage(`{}`)} },
			want:   "instruction sources"},
		{name: "approval",
			mutate: func(result *threadStartResult) { result.ApprovalPolicy = "on-request" },
			want:   "provider-side approval"},
		{name: "sandbox",
			mutate: func(result *threadStartResult) { result.Sandbox.Type = "workspaceWrite" },
			want:   "not read-only"},
		{name: "network",
			mutate: func(result *threadStartResult) { result.Sandbox.NetworkAccess = true },
			want:   "network access"},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := valid()
			test.mutate(&started)
			err := started.validateIsolation(cwd)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate isolation = %v, want %q", err, test.want)
			}
		})
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

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed after possible delivery")
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
