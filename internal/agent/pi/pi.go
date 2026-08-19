package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
)

const maxProtocolLineBytes = 1 << 20

// Adapter owns the documented Pi RPC subprocess protocol.
type Adapter struct{}

// New constructs the single supported Pi adapter.
func New() *Adapter {
	return &Adapter{}
}

func (adapter *Adapter) Diagnose(ctx context.Context) error {
	if _, err := exec.LookPath("pi"); err != nil {
		return fmt.Errorf("%w: Pi executable: %v", host.ErrAgentUnavailable, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
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

func (adapter *Adapter) Execute(ctx context.Context, request host.ExecutionRequest) (host.Draft, error) {
	prompt, err := request.Prompt()
	if err != nil {
		return host.Draft{}, err
	}
	temporaryDirectory, err := os.MkdirTemp("", "carry-pi-attempt-")
	if err != nil {
		return host.Draft{}, fmt.Errorf("create Pi Attempt directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryDirectory) }()

	command := exec.CommandContext(
		ctx,
		"pi",
		"--mode", "rpc",
		"--no-session",
		"--no-builtin-tools",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
	)
	command.WaitDelay = time.Second
	command.Dir = temporaryDirectory
	stdin, err := command.StdinPipe()
	if err != nil {
		return host.Draft{}, fmt.Errorf("open Pi stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return host.Draft{}, fmt.Errorf("open Pi stdout: %w", err)
	}
	var stderr boundedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return host.Draft{}, fmt.Errorf("%w: start Pi RPC: %v", host.ErrAgentUnavailable, err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()
	if err := json.NewEncoder(stdin).Encode(struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}{ID: "carry-prompt", Type: "prompt", Message: prompt}); err != nil {
		_ = stdin.Close()
		_ = command.Process.Kill()
		<-processDone
		return host.Draft{}, fmt.Errorf("write Pi prompt: %w", err)
	}

	draft, resultErr := readPiResult(ctx, stdout)
	_ = stdin.Close()
	if resultErr == nil {
		if err := waitForExit(command, processDone); err != nil {
			return host.Draft{}, fmt.Errorf("close Pi RPC: %w", err)
		}
		return draft, nil
	}
	_ = command.Process.Kill()
	<-processDone
	if ctx.Err() != nil {
		return host.Draft{}, host.ErrAgentOutcomeLost
	}
	if stderr.String() != "" {
		return host.Draft{}, fmt.Errorf("%w: %s", resultErr, stderr.String())
	}
	return host.Draft{}, resultErr
}

type piEnvelope struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Command  string          `json:"command"`
	Success  bool            `json:"success"`
	Error    string          `json:"error"`
	Message  json.RawMessage `json:"message"`
	Messages json.RawMessage `json:"messages"`
}

type piAssistantMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stopReason"`
}

func readPiResult(ctx context.Context, stdout io.Reader) (host.Draft, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxProtocolLineBytes)
	promptAccepted := false
	var finalMessage piAssistantMessage
	for scanner.Scan() {
		var envelope piEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return host.Draft{}, fmt.Errorf("%w: decode Pi RPC record", host.ErrAgentOutcomeLost)
		}
		switch envelope.Type {
		case "response":
			if envelope.ID == "carry-prompt" && envelope.Command == "prompt" {
				if !envelope.Success {
					return host.Draft{}, fmt.Errorf("%w: Pi rejected prompt: %s", host.ErrAgentFailed, envelope.Error)
				}
				promptAccepted = true
			}
		case "message_end":
			var message piAssistantMessage
			if err := json.Unmarshal(envelope.Message, &message); err != nil {
				return host.Draft{}, fmt.Errorf("%w: decode Pi assistant message", host.ErrAgentOutcomeLost)
			}
			if message.Role == "assistant" {
				finalMessage = message
			}
		case "extension_error":
			return host.Draft{}, fmt.Errorf("%w: Pi extension error", host.ErrAgentFailed)
		case "agent_settled":
			if !promptAccepted || finalMessage.Role != "assistant" {
				return host.Draft{}, fmt.Errorf("%w: Pi settled without an accepted prompt and final message", host.ErrAgentFailed)
			}
			if finalMessage.StopReason != "stop" {
				return host.Draft{}, fmt.Errorf("%w: Pi stop reason %q", host.ErrAgentFailed, finalMessage.StopReason)
			}
			var text strings.Builder
			for _, content := range finalMessage.Content {
				if content.Type == "text" {
					text.WriteString(content.Text)
				}
			}
			draft, err := host.ParseDraft([]byte(text.String()))
			if err != nil {
				return host.Draft{}, err
			}
			return draft, nil
		}
		select {
		case <-ctx.Done():
			return host.Draft{}, host.ErrAgentOutcomeLost
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		return host.Draft{}, fmt.Errorf("%w: read Pi RPC: %v", host.ErrAgentOutcomeLost, err)
	}
	return host.Draft{}, fmt.Errorf("%w: Pi RPC ended before agent_settled", host.ErrAgentOutcomeLost)
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
