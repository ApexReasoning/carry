package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	for _, expected := range []string{"lookup_reference", "https://references.example", `redirect: "error"`, "64 * 1024", "method: \"GET\""} {
		if !strings.Contains(string(source), expected) {
			t.Fatalf("Pi Reference extension does not contain %q", expected)
		}
	}
}

func TestPiReferenceFailureCannotProduceWorkUpdate(t *testing.T) {
	binary := writePiFixture(t, `
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"tool_execution_end","toolName":"lookup_reference","isError":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Unverified\",\"next_step\":\"Continue\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	_, err := New().Execute(context.Background(), host.ExecutionRequest{Goal: "Use the catalog"})
	if !errors.Is(err, host.ErrAgentFailed) {
		t.Fatalf("Pi reference failure = %v, want Agent failure", err)
	}
}

func TestAdapterRepliesToPrivateConversationAfterPiSettles(t *testing.T) {
	binary := writePiFixture(t, `
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"reply\":\"Here are the private options.\",\"delegation_goal\":null}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`)
	usePiFixture(t, binary)
	candidate, err := New().Reply(context.Background(), host.ConversationReplyRequest{
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember, Text: "What are my options?"}},
	})
	if err != nil {
		t.Fatalf("reply through Pi RPC: %v", err)
	}
	if candidate.Reply != "Here are the private options." || candidate.DelegationGoal != nil {
		t.Fatalf("Pi private candidate = %#v", candidate)
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
