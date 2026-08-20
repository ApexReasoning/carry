package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/agent/reference"
	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/host"
)

var disabledFeatures = [...]string{
	"apps",
	"browser_use",
	"browser_use_external",
	"browser_use_full_cdp_access",
	"code_mode_host",
	"computer_use",
	"goals",
	"hooks",
	"image_generation",
	"in_app_browser",
	"multi_agent",
	"plugins",
	"remote_plugin",
	"shell_tool",
	"skill_search",
	"standalone_web_search",
	"tool_suggest",
	"unified_exec",
	"view_image",
	"workspace_dependencies",
}

// Adapter owns the Codex app-server protocol used by Carry.
type Adapter struct {
	referenceBaseURL string
	reference        *reference.Client
}

// New constructs the Codex adapter without an optional Reference Catalog.
func New() *Adapter {
	return NewWithReferenceBaseURL("")
}

// NewWithReferenceBaseURL constructs a Codex adapter with one fixed Reference Catalog URL.
func NewWithReferenceBaseURL(baseURL string) *Adapter {
	adapter := &Adapter{referenceBaseURL: baseURL}
	if baseURL != "" {
		adapter.reference, _ = reference.New(baseURL)
	}
	return adapter
}

func (adapter *Adapter) Diagnose(ctx context.Context) error {
	if adapter.reference == nil && adapter.referenceBaseURL != "" {
		return fmt.Errorf("%w: Reference Catalog is invalid", host.ErrAgentUnavailable)
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("%w: Codex executable: %v", host.ErrAgentUnavailable, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var output boundedBuffer
	command := exec.CommandContext(probeCtx, "codex", "--version")
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: probe Codex 0.148.0: %v", host.ErrAgentUnavailable, err)
	}
	if output.String() != "codex-cli 0.148.0" {
		return fmt.Errorf("%w: Codex version %q, want codex-cli 0.148.0", host.ErrAgentUnavailable, output.String())
	}
	return nil
}

func (adapter *Adapter) Execute(ctx context.Context, request host.ExecutionRequest) (host.UnderstandingUpdate, error) {
	prompt, err := request.Prompt()
	if err != nil {
		return host.UnderstandingUpdate{}, err
	}
	text, err := adapter.generate(ctx, prompt, host.UnderstandingOutputSchema, true, adapter.reference != nil)
	if err != nil {
		return host.UnderstandingUpdate{}, err
	}
	return host.ParseUnderstandingUpdate(text)
}

func (adapter *Adapter) Reply(ctx context.Context, request host.ConversationReplyRequest) (conversation.ReplyCandidate, error) {
	prompt, err := request.Prompt()
	if err != nil {
		return conversation.ReplyCandidate{}, err
	}
	text, err := adapter.generate(ctx, prompt, host.ConversationReplyOutputSchema, false, false)
	if err != nil {
		return conversation.ReplyCandidate{}, host.SanitizePrivateAgentError(err)
	}
	return host.ParseConversationReply(text)
}

func (adapter *Adapter) generate(ctx context.Context, prompt string, outputSchema []byte, includeStderr bool, enableReference bool) ([]byte, error) {
	attemptDirectory, err := os.MkdirTemp("", "carry-codex-attempt-")
	if err != nil {
		return nil, fmt.Errorf("create Codex Attempt directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(attemptDirectory) }()

	command := exec.CommandContext(ctx, "codex", appServerArguments()...)
	command.WaitDelay = time.Second
	command.Dir = attemptDirectory
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex stdout: %w", err)
	}
	var stderr boundedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("%w: start Codex app-server: %v", host.ErrAgentUnavailable, err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()

	var lookup func(context.Context, string) (string, error)
	if enableReference && adapter.reference != nil {
		lookup = adapter.reference.Lookup
	}
	client := newAppServerClient(stdin, stdout, lookup)
	text, resultErr := client.execute(ctx, attemptDirectory, prompt, outputSchema)
	_ = stdin.Close()
	if resultErr == nil {
		if err := waitForExit(command, processDone); err != nil {
			return nil, fmt.Errorf("close Codex app-server: %w", err)
		}
		return text, nil
	}
	_ = command.Process.Kill()
	<-processDone
	if ctx.Err() != nil {
		return nil, host.ErrAgentOutcomeLost
	}
	if includeStderr && stderr.String() != "" {
		return nil, fmt.Errorf("%w: %s", resultErr, stderr.String())
	}
	return nil, resultErr
}

func appServerArguments() []string {
	arguments := make([]string, 0, len(disabledFeatures)*2+5)
	for _, feature := range disabledFeatures {
		arguments = append(arguments, "--disable", feature)
	}
	return append(arguments, "-c", "mcp_servers={}", "app-server", "--stdio")
}

func waitForExit(command *exec.Cmd, processDone <-chan error) error {
	select {
	case <-processDone:
		return nil
	case <-time.After(2 * time.Second):
		if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-processDone
		return nil
	}
}

type boundedBuffer struct {
	data []byte
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	const limit = 4096
	originalLength := len(data)
	remaining := limit - len(buffer.data)
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		buffer.data = append(buffer.data, data...)
	}
	return originalLength, nil
}

func (buffer *boundedBuffer) String() string {
	return strings.TrimSpace(string(buffer.data))
}

var _ host.Executor = (*Adapter)(nil)
