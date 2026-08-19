package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ApexReasoning/carry/internal/host"
)

const (
	maxProtocolLineBytes = 1 << 20
	promptRequestID      = "carry-prompt"
)

type promptRequest struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

type envelope struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Message json.RawMessage `json:"message"`
}

type assistantMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stopReason"`
}

type resultState struct {
	promptAccepted bool
	finalMessage   assistantMessage
}

func awaitText(ctx context.Context, stdout io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxProtocolLineBytes)
	var state resultState
	for scanner.Scan() {
		var record envelope
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("%w: decode Pi RPC record", host.ErrAgentOutcomeLost)
		}
		text, settled, err := state.accept(record)
		if err != nil {
			return nil, fmt.Errorf("read Pi result: %w", err)
		}
		if settled {
			return text, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf(
				"%w: Pi execution context ended: %v", host.ErrAgentOutcomeLost, ctx.Err(),
			)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: read Pi RPC: %v", host.ErrAgentOutcomeLost, err)
	}
	return nil, fmt.Errorf("%w: Pi RPC ended before agent_settled", host.ErrAgentOutcomeLost)
}

func (state *resultState) accept(record envelope) ([]byte, bool, error) {
	switch record.Type {
	case "response":
		if record.ID != promptRequestID || record.Command != "prompt" {
			return nil, false, nil
		}
		if !record.Success {
			return nil, false, fmt.Errorf("%w: Pi rejected prompt: %s", host.ErrAgentFailed, record.Error)
		}
		state.promptAccepted = true
	case "message_end":
		var message assistantMessage
		if err := json.Unmarshal(record.Message, &message); err != nil {
			return nil, false, fmt.Errorf("%w: decode Pi assistant message", host.ErrAgentOutcomeLost)
		}
		if message.Role == "assistant" {
			state.finalMessage = message
		}
	case "extension_error":
		return nil, false, fmt.Errorf("%w: Pi extension error", host.ErrAgentFailed)
	case "agent_settled":
		text, err := state.text()
		return text, true, err
	}
	return nil, false, nil
}

func (state resultState) text() ([]byte, error) {
	if !state.promptAccepted || state.finalMessage.Role != "assistant" {
		return nil, fmt.Errorf("%w: Pi settled without an accepted prompt and final message", host.ErrAgentFailed)
	}
	if state.finalMessage.StopReason != "stop" {
		return nil, fmt.Errorf("%w: Pi stop reason %q", host.ErrAgentFailed, state.finalMessage.StopReason)
	}
	var text strings.Builder
	for _, content := range state.finalMessage.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
		}
	}
	return []byte(text.String()), nil
}
