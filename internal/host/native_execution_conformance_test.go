package host_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ApexReasoning/carry/internal/agent/codex"
	"github.com/ApexReasoning/carry/internal/agent/pi"
	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
)

func TestNativeExecutorsShareOneUnderstandingContract(t *testing.T) {
	request := host.ExecutionRequest{
		Goal:                 "Compare onboarding options",
		CurrentUnderstanding: "Three options remain under review.",
		Messages:             []run.Message{{Text: "Support supplied handling times"}},
	}
	piBinary := writeConformanceExecutable(t, "pi", piConformanceScript)
	codexBinary := writeConformanceExecutable(t, "codex", codexConformanceScript)
	t.Setenv(
		"PATH",
		filepath.Dir(piBinary)+string(os.PathListSeparator)+
			filepath.Dir(codexBinary)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	for _, testCase := range []struct {
		name     string
		executor host.Executor
	}{
		{name: "Pi", executor: pi.New()},
		{name: "Codex", executor: codex.New()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			update, err := testCase.executor.Execute(context.Background(), request)
			if err != nil {
				t.Fatalf("execute native Agent: %v", err)
			}
			if update.Understanding != "Support evidence makes the three options comparable." ||
				update.NextStep != "Ask the owner to choose an option." {
				t.Fatalf("conforming update = %#v", update)
			}
		})
	}
}

func writeConformanceExecutable(t *testing.T, name string, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatalf("write native Agent fixture: %v", err)
	}
	return path
}

const piConformanceScript = `
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Support evidence makes the three options comparable.\",\"next_step\":\"Ask the owner to choose an option.\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`

const codexConformanceScript = `
IFS= read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fixture"}}'
IFS= read -r initialized
IFS= read -r thread_start
printf '{"id":2,"result":{"thread":{"id":"thread-conformance","ephemeral":true,"cwd":"%s","gitInfo":null},"runtimeWorkspaceRoots":["%s"],"instructionSources":[],"approvalPolicy":"never","sandbox":{"type":"readOnly","networkAccess":false}}}\n' "$PWD" "$PWD"
IFS= read -r turn_start
printf '%s\n' \
  '{"id":3,"result":{"turn":{"id":"turn-conformance","status":"inProgress","items":[]}}}' \
  '{"method":"item/completed","params":{"threadId":"thread-conformance","turnId":"turn-conformance","completedAtMs":1,"item":{"id":"message-conformance","type":"agentMessage","phase":"final_answer","text":"{\"understanding\":\"Support evidence makes the three options comparable.\",\"next_step\":\"Ask the owner to choose an option.\"}"}}}' \
  '{"method":"turn/completed","params":{"threadId":"thread-conformance","turn":{"id":"turn-conformance","status":"completed","items":[]}}}'
`
