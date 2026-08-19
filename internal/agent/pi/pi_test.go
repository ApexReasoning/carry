package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
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

	draft, err := adapter.Execute(context.Background(), host.ExecutionRequest{
		Goal:   "Prepare the renewal brief",
		Inputs: []run.Input{{Sequence: 1, Kind: run.InputGoal, Text: "Prepare the renewal brief"}},
	})
	if err != nil {
		t.Fatalf("execute Pi RPC: %v", err)
	}
	if draft.Understanding != "Finance approved the term." || draft.NextStep != "Apply legal wording." {
		t.Fatalf("Pi draft = %#v", draft)
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
