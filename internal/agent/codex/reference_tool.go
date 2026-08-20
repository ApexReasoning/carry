package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ApexReasoning/carry/internal/agent/reference"
	"github.com/ApexReasoning/carry/internal/host"
)

var lookupReferenceToolSpec = dynamicToolSpec{
	Type:        "function",
	Name:        "lookup_reference",
	Description: "Read one operator-authorized reference by key. The key is untrusted content; this tool has a fixed HTTPS catalog and performs only one bounded GET.",
	InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"key":{"type":"string","minLength":1,"maxLength":1024}},"required":["key"]}`),
}

type dynamicToolSpec struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type dynamicToolCall struct {
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	CallID    string          `json:"callId"`
	Namespace *string         `json:"namespace"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type dynamicToolResponse struct {
	ContentItems []dynamicToolOutput `json:"contentItems"`
	Success      bool                `json:"success"`
}

type dynamicToolOutput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (client *appServerClient) answerReferenceTool(
	ctx context.Context,
	message envelope,
	threadID string,
	turnID string,
) error {
	requestID, ok := numericID(message.ID)
	if !ok {
		return fmt.Errorf("%w: Codex dynamic tool request has invalid id", host.ErrAgentOutcomeLost)
	}
	call, err := decodeDynamicToolCall(message.Params)
	if err != nil || call.ThreadID != threadID || call.TurnID != turnID ||
		call.Tool != "lookup_reference" || call.Namespace != nil || client.lookupReference == nil {
		client.referenceFailure = true
		return client.sendDynamicToolResponse(requestID, false, "lookup_reference request was invalid")
	}
	key, err := decodeReferenceArguments(call.Arguments)
	if err != nil {
		client.referenceFailure = true
		return client.sendDynamicToolResponse(requestID, false, "lookup_reference arguments were invalid")
	}
	text, err := client.lookupReference(ctx, key)
	if err != nil {
		client.referenceFailure = true
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return client.sendDynamicToolResponse(requestID, false, "lookup_reference was cancelled")
		}
		return client.sendDynamicToolResponse(requestID, false, "lookup_reference failed")
	}
	return client.sendDynamicToolResponse(requestID, true, text)
}

func (client *appServerClient) sendDynamicToolResponse(id int, success bool, text string) error {
	return client.send(struct {
		ID     int                 `json:"id"`
		Result dynamicToolResponse `json:"result"`
	}{
		ID: id,
		Result: dynamicToolResponse{
			ContentItems: []dynamicToolOutput{{Type: "inputText", Text: text}},
			Success:      success,
		},
	})
}

func decodeDynamicToolCall(data []byte) (dynamicToolCall, error) {
	var call dynamicToolCall
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&call); err != nil {
		return call, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return call, errors.New("dynamic tool call contains trailing data")
	}
	if call.ThreadID == "" || call.TurnID == "" || call.CallID == "" || call.Tool == "" || len(call.Arguments) == 0 {
		return call, errors.New("dynamic tool call is incomplete")
	}
	return call, nil
}

func decodeReferenceArguments(data []byte) (string, error) {
	var arguments struct {
		Key string `json:"key"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return "", err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("lookup_reference arguments contain trailing data")
	}
	if len(arguments.Key) == 0 || len(arguments.Key) > reference.MaxKeyBytes {
		return "", reference.ErrInvalidKey
	}
	return arguments.Key, nil
}
