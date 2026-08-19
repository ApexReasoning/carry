package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
)

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
  '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1,"item":{"id":"message-1","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Finance approved the term.\",\"next_step\":\"Apply legal wording.\"}"}}}' \
  '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
`)
	useCodexFixture(t, binary)
	adapter := New()
	if err := adapter.Diagnose(context.Background()); err != nil {
		t.Fatalf("diagnose Codex: %v", err)
	}

	draft, err := adapter.Execute(context.Background(), host.ExecutionRequest{
		Goal:   "Prepare the renewal brief",
		Inputs: []run.Input{{Sequence: 1, Kind: run.InputGoal, Text: "Prepare the renewal brief"}},
	})
	if err != nil {
		t.Fatalf("execute Codex app-server: %v", err)
	}
	if draft.Understanding != "Finance approved the term." || draft.NextStep != "Apply legal wording." {
		t.Fatalf("Codex draft = %#v", draft)
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
