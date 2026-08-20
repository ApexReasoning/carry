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
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Command    string          `json:"command"`
	Success    bool            `json:"success"`
	Error      string          `json:"error"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	IsError    *bool           `json:"isError"`
	Message    json.RawMessage `json:"message"`
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
	promptAccepted       bool
	finalMessage         assistantMessage
	referenceEnabled     bool
	activeReferenceCalls map[string]struct{}
	seenReferenceCalls   map[string]struct{}
	referenceFailure     bool
}

func awaitText(ctx context.Context, stdout io.Reader, referenceEnabled bool) ([]byte, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxProtocolLineBytes)
	state := resultState{
		referenceEnabled:     referenceEnabled,
		activeReferenceCalls: make(map[string]struct{}),
		seenReferenceCalls:   make(map[string]struct{}),
	}
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
	case "tool_execution_start":
		if !state.promptAccepted || !state.referenceEnabled || state.finalMessage.StopReason == "stop" ||
			record.ToolName != "lookup_reference" || record.ToolCallID == "" {
			return nil, false, fmt.Errorf("%w: invalid Pi tool execution start", host.ErrAgentOutcomeLost)
		}
		if _, duplicate := state.seenReferenceCalls[record.ToolCallID]; duplicate {
			return nil, false, fmt.Errorf("%w: duplicate Pi tool execution start", host.ErrAgentOutcomeLost)
		}
		state.seenReferenceCalls[record.ToolCallID] = struct{}{}
		state.activeReferenceCalls[record.ToolCallID] = struct{}{}
	case "tool_execution_end":
		if !state.promptAccepted || !state.referenceEnabled ||
			record.ToolName != "lookup_reference" || record.ToolCallID == "" || record.IsError == nil {
			return nil, false, fmt.Errorf("%w: invalid Pi tool execution end", host.ErrAgentOutcomeLost)
		}
		if _, started := state.activeReferenceCalls[record.ToolCallID]; !started {
			return nil, false, fmt.Errorf("%w: unmatched Pi tool execution end", host.ErrAgentOutcomeLost)
		}
		delete(state.activeReferenceCalls, record.ToolCallID)
		if *record.IsError {
			state.referenceFailure = true
		}
	case "extension_error":
		return nil, false, fmt.Errorf("%w: Pi extension error", host.ErrAgentFailed)
	case "agent_settled":
		if len(state.activeReferenceCalls) != 0 {
			return nil, false, fmt.Errorf("%w: Pi settled with an incomplete lookup_reference call", host.ErrAgentOutcomeLost)
		}
		if state.referenceFailure {
			return nil, false, fmt.Errorf("%w: lookup_reference failed", host.ErrAgentFailed)
		}
		text, err := state.text()
		return text, true, err
	}
	return nil, false, nil
}

func (state resultState) text() ([]byte, error) {
	if state.referenceFailure {
		return nil, fmt.Errorf("%w: lookup_reference failed", host.ErrAgentFailed)
	}
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
