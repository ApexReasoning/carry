package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
)

const reconciliationTimeout = 2 * time.Second

type turnInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type turnStartParams struct {
	ThreadID     string          `json:"threadId"`
	Input        []turnInput     `json:"input"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Sandbox      sandboxPolicy   `json:"sandboxPolicy"`
}

type threadItem struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Phase string `json:"phase"`
}

type turn struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Items  []threadItem `json:"items"`
}

type turnStartResult struct {
	Turn turn `json:"turn"`
}

func startStructuredTurnProtocol(
	client *appServerClient,
	ctx context.Context,
	threadID string,
	prompt string,
	outputSchema json.RawMessage,
) (string, error) {
	params := turnStartParams{
		ThreadID: threadID,
		Input: []turnInput{{Type: "text",
			Text: prompt}},
		OutputSchema: outputSchema,
		Sandbox: sandboxPolicy{Type: "readOnly",
			NetworkAccess: false},
	}
	if err := client.sendRequest(startTurnRequestID, "turn/start", params); err != nil {
		return "", fmt.Errorf("%w: %v", host.ErrAgentOutcomeLost, err)
	}
	response, err := client.readResponse(ctx, startTurnRequestID)
	if err != nil {
		return "", err
	}
	var started turnStartResult
	if json.Unmarshal(response, &started) != nil || started.Turn.ID == "" {
		return "", fmt.Errorf("%w: invalid Codex turn/start response", host.ErrAgentOutcomeLost)
	}
	return started.Turn.ID, nil
}

type turnObservation struct {
	threadID     string
	turnID       string
	finalText    string
	streamedText strings.Builder
}

func awaitTurnTextProtocol(client *appServerClient, ctx context.Context, threadID string, turnID string) ([]byte, error) {
	observation := turnObservation{threadID: threadID,
		turnID: turnID}
	var reconciliationDeadline time.Time
	reconciliationRequested := false
	for {
		message, err := client.readEnvelope(ctx, reconciliationDeadline)
		if err != nil {
			return nil, err
		}
		if responseID, ok := numericID(message.ID); ok && responseID == reconcileThreadRequestID {
			return observation.reconcile(message)
		}
		switch message.Method {
		case "item/agentMessage/delta":
			observation.recordDelta(message.Params)
		case "item/completed":
			observation.recordCompletedItem(message.Params)
		case "turn/completed":
			if completed, ok := observation.completedTurn(message.Params); ok {
				return observation.completedText(completed)
			}
		case "error":
			if observation.isTerminalError(message.Params) {
				return nil, fmt.Errorf("%w: Codex reported a terminal turn error", host.ErrAgentFailed)
			}
		case "thread/status/changed":
			if observation.isIdle(message.Params) && !reconciliationRequested {
				reconciliationRequested = true
				reconciliationDeadline = time.Now().Add(reconciliationTimeout)
				if err := client.requestReconciliation(threadID); err != nil {
					return nil, err
				}
			}
		}
	}
}

func (client *appServerClient) requestReconciliation(threadID string) error {
	return client.sendRequest(reconcileThreadRequestID, "thread/read", struct {
		ThreadID     string `json:"threadId"`
		IncludeTurns bool   `json:"includeTurns"`
	}{ThreadID: threadID,
		IncludeTurns: true})
}

func (observation *turnObservation) recordDelta(raw json.RawMessage) {
	var params struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Delta    string `json:"delta"`
	}
	if json.Unmarshal(raw, &params) != nil || params.ThreadID != observation.threadID || params.TurnID != observation.turnID {
		return
	}
	observation.streamedText.WriteString(params.Delta)
	observation.finalText = observation.streamedText.String()
}

func (observation *turnObservation) recordCompletedItem(raw json.RawMessage) {
	var params struct {
		ThreadID string     `json:"threadId"`
		TurnID   string     `json:"turnId"`
		Item     threadItem `json:"item"`
	}
	if json.Unmarshal(raw, &params) != nil || params.ThreadID != observation.threadID || params.TurnID != observation.turnID {
		return
	}
	if params.Item.Type == "agentMessage" && (params.Item.Phase == "" || params.Item.Phase == "final_answer") {
		observation.finalText = params.Item.Text
	}
}

func (observation turnObservation) completedTurn(raw json.RawMessage) (turn, bool) {
	var params struct {
		Turn turn `json:"turn"`
	}
	if json.Unmarshal(raw, &params) != nil || params.Turn.ID != observation.turnID {
		return turn{}, false
	}
	return params.Turn, true
}

func (observation *turnObservation) completedText(completed turn) ([]byte, error) {
	switch completed.Status {
	case "completed":
		if text := finalAgentText(completed.Items); text != "" {
			observation.finalText = text
		}
		return []byte(observation.finalText), nil
	case "failed", "interrupted":
		return nil, fmt.Errorf("%w: Codex turn status %q", host.ErrAgentFailed, completed.Status)
	default:
		return nil, fmt.Errorf("%w: unrecognized Codex turn status %q", host.ErrAgentOutcomeLost, completed.Status)
	}
}

func (observation turnObservation) isTerminalError(raw json.RawMessage) bool {
	var params struct {
		TurnID    string `json:"turnId"`
		WillRetry bool   `json:"willRetry"`
	}
	return json.Unmarshal(raw, &params) == nil && params.TurnID == observation.turnID && !params.WillRetry
}

func (observation turnObservation) isIdle(raw json.RawMessage) bool {
	var params struct {
		ThreadID string `json:"threadId"`
		Status   struct {
			Type string `json:"type"`
		} `json:"status"`
	}
	return json.Unmarshal(raw, &params) == nil && params.ThreadID == observation.threadID && params.Status.Type == "idle"
}

func (observation *turnObservation) reconcile(message envelope) ([]byte, error) {
	if len(message.Error) != 0 && string(message.Error) != "null" {
		return nil, fmt.Errorf(
			"%w: Codex thread/read failed: %s",
			host.ErrAgentOutcomeLost,
			protocolErrorMessage(message.Error),
		)
	}
	reconciled, ok := findTurn(message.Result, observation.turnID)
	if !ok {
		return nil, fmt.Errorf("%w: Codex turn completion is not provable", host.ErrAgentOutcomeLost)
	}
	return observation.completedText(reconciled)
}

func (client *appServerClient) readEnvelope(ctx context.Context, deadline time.Time) (envelope, error) {
	type result struct {
		message envelope
		err     error
	}
	completed := make(chan result, 1)
	go func() {
		if !client.scanner.Scan() {
			if err := client.scanner.Err(); err != nil {
				completed <- result{err: fmt.Errorf("%w: read Codex app-server: %v", host.ErrAgentOutcomeLost, err)}
				return
			}
			completed <- result{err: fmt.Errorf("%w: Codex app-server ended before a proven turn completion", host.ErrAgentOutcomeLost)}
			return
		}
		var message envelope
		if err := json.Unmarshal(client.scanner.Bytes(), &message); err != nil {
			completed <- result{err: fmt.Errorf("%w: decode Codex app-server record", host.ErrAgentOutcomeLost)}
			return
		}
		completed <- result{message: message}
	}()

	var deadlineReached <-chan time.Time
	var timer *time.Timer
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return envelope{}, fmt.Errorf("%w: Codex thread/read reconciliation timed out", host.ErrAgentOutcomeLost)
		}
		timer = time.NewTimer(remaining)
		deadlineReached = timer.C
		defer timer.Stop()
	}
	select {
	case read := <-completed:
		return read.message, read.err
	case <-ctx.Done():
		return envelope{}, host.ErrAgentOutcomeLost
	case <-deadlineReached:
		return envelope{}, fmt.Errorf("%w: Codex thread/read reconciliation timed out", host.ErrAgentOutcomeLost)
	}
}

func findTurn(result json.RawMessage, turnID string) (turn, bool) {
	var response struct {
		Thread struct {
			Turns []turn `json:"turns"`
		} `json:"thread"`
	}
	if json.Unmarshal(result, &response) != nil {
		return turn{}, false
	}
	for _, candidate := range response.Thread.Turns {
		if candidate.ID == turnID {
			return candidate, true
		}
	}
	return turn{}, false
}

func finalAgentText(items []threadItem) string {
	var text string
	for _, item := range items {
		if item.Type == "agentMessage" && (item.Phase == "" || item.Phase == "final_answer") {
			text = item.Text
		}
	}
	return text
}
