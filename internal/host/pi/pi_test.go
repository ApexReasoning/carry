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

func TestAdapterObservesOnlyDiscoveredPiDefault(t *testing.T) {
	adapter := New()
	if adapter.Key() != "pi" {
		t.Fatalf("Pi adapter key = %q", adapter.Key())
	}

	t.Setenv("PATH", t.TempDir())
	occurrences, err := adapter.Observe(context.Background())
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("uninstalled Pi observation = %#v, error = %v", occurrences, err)
	}

	unhealthy := writePiFixture(t, `
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' '0.83.0'
  exit 0
fi
`)
	t.Setenv("PATH", filepath.Dir(unhealthy))
	occurrences, err = adapter.Observe(context.Background())
	if err != nil || len(occurrences) != 1 || occurrences[0].Key != "default" ||
		occurrences[0].Present || occurrences[0].Executor != nil {
		t.Fatalf("unhealthy Pi observation = %#v, error = %v", occurrences, err)
	}

	healthy := writePiFixture(t, `
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' '0.84.2'
  exit 0
fi
`)
	t.Setenv("PATH", filepath.Dir(healthy))
	occurrences, err = adapter.Observe(context.Background())
	if err != nil || len(occurrences) != 1 || !occurrences[0].Present || occurrences[0].Executor != adapter {
		t.Fatalf("healthy Pi observation = %#v, error = %v", occurrences, err)
	}
}

func TestPromptWriteFailureKeepsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	err := writePrompt(failingWriter{}, "Advance the Work")
	if !errors.Is(err, host.ErrAgentOutcomeLost) {
		t.Fatalf("prompt write error = %v, want outcome lost", err)
	}
}

func TestAdapterExecutesIsolatedPiRPCAndParsesSettledDraft(t *testing.T) {
	argumentFile := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("PI_ARGS_FILE", argumentFile)
	binary := writePiFixture(t, `
printf '%s\n' "$@" > "$PI_ARGS_FILE"
IFS= read -r prompt
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Finance approved the term.\",\"next_step\":\"Apply legal wording.\",\"review_required\":false}"}],"stopReason":"stop"}}' \
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
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember,
			Text: "What are my options?"}},
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
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember,
			Text: "Private question"}},
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
		Messages: []conversation.ContextMessage{{Author: conversation.AuthorMember,
			Text: "Private question"}},
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

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed after possible delivery")
}

func usePiFixture(t *testing.T, binary string) {
	t.Helper()
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
}
