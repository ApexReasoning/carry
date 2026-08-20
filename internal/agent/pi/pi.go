package pi

import (
	"context"
	"encoding/json"
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

// Adapter owns the documented Pi RPC subprocess protocol.
type Adapter struct {
	referenceBaseURL string
}

// New constructs the Pi adapter without an optional Reference Catalog.
func New() *Adapter {
	return NewWithReferenceBaseURL("")
}

// NewWithReferenceBaseURL constructs a Pi adapter with one fixed Reference Catalog URL.
func NewWithReferenceBaseURL(baseURL string) *Adapter {
	return &Adapter{referenceBaseURL: baseURL}
}

func (adapter *Adapter) Diagnose(ctx context.Context) error {
	if adapter.referenceBaseURL != "" {
		if _, err := reference.New(adapter.referenceBaseURL); err != nil {
			return fmt.Errorf("%w: Reference Catalog: %v", host.ErrAgentUnavailable, err)
		}
	}
	if _, err := exec.LookPath("pi"); err != nil {
		return fmt.Errorf("%w: Pi executable: %v", host.ErrAgentUnavailable, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var output boundedBuffer
	command := exec.CommandContext(probeCtx, "pi", "--version")
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: probe Pi 0.84.2: %v", host.ErrAgentUnavailable, err)
	}
	if output.String() != "0.84.2" {
		return fmt.Errorf("%w: Pi version %q, want 0.84.2", host.ErrAgentUnavailable, output.String())
	}
	return nil
}

func (adapter *Adapter) Execute(ctx context.Context, request host.ExecutionRequest) (host.UnderstandingUpdate, error) {
	prompt, err := request.Prompt()
	if err != nil {
		return host.UnderstandingUpdate{}, err
	}
	text, err := adapter.generate(ctx, prompt, true, adapter.referenceBaseURL != "")
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
	text, err := adapter.generate(ctx, prompt, false, false)
	if err != nil {
		return conversation.ReplyCandidate{}, host.SanitizePrivateAgentError(err)
	}
	return host.ParseConversationReply(text)
}

func (adapter *Adapter) generate(ctx context.Context, prompt string, includeStderr bool, enableReference bool) ([]byte, error) {
	attemptDirectory, err := os.MkdirTemp("", "carry-pi-attempt-")
	if err != nil {
		return nil, fmt.Errorf("create Pi Attempt directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(attemptDirectory) }()

	arguments := piRPCArguments()
	if enableReference {
		extensionPath, extensionErr := writeReferenceExtension(attemptDirectory, adapter.referenceBaseURL)
		if extensionErr != nil {
			return nil, extensionErr
		}
		arguments = append(arguments, "-e", extensionPath)
	}
	command := exec.CommandContext(ctx, "pi", arguments...)
	command.WaitDelay = time.Second
	command.Dir = attemptDirectory
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stdout: %w", err)
	}
	var stderr boundedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("%w: start Pi RPC: %v", host.ErrAgentUnavailable, err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()

	if err := json.NewEncoder(stdin).Encode(promptRequest{ID: promptRequestID, Type: "prompt", Message: prompt}); err != nil {
		_ = stdin.Close()
		_ = command.Process.Kill()
		<-processDone
		return nil, fmt.Errorf("write Pi prompt: %w", err)
	}

	text, resultErr := awaitText(ctx, stdout, enableReference)
	_ = stdin.Close()
	if resultErr == nil {
		if err := waitForExit(command, processDone); err != nil {
			return nil, fmt.Errorf("close Pi RPC: %w", err)
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

func piRPCArguments() []string {
	return []string{
		"--mode", "rpc",
		"--no-session",
		"--no-builtin-tools",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
	}
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
