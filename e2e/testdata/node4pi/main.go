package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
)

const (
	questionText    = "What should I check before a customer renewal? Private reference ORCHID-QUESTION-739."
	questionReply   = "Review the renewal date, notice window, owner, and approval dependencies first."
	delegationText  = "Carry, take responsibility for preparing the renewal brief. Private reference ORCHID-DELEGATION-842."
	delegationReply = "I’ll keep the renewal brief moving as shared Work."
	delegationGoal  = "Prepare the renewal brief"
)

var rpcArguments = []string{
	"--mode", "rpc",
	"--no-session",
	"--no-builtin-tools",
	"--no-extensions",
	"--no-skills",
	"--no-prompt-templates",
	"--no-themes",
	"--no-context-files",
}

type promptRequest struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

type strictReply struct {
	Reply          string  `json:"reply"`
	DelegationGoal *string `json:"delegation_goal"`
}

type strictUnderstanding struct {
	Understanding string `json:"understanding"`
	NextStep      string `json:"next_step"`
}

func main() {
	if reflect.DeepEqual(os.Args[1:], []string{"--version"}) {
		fmt.Println("0.84.2")
		return
	}
	if !reflect.DeepEqual(os.Args[1:], rpcArguments) {
		fmt.Fprintln(os.Stderr, "unexpected Pi RPC arguments")
		os.Exit(2)
	}

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "missing Pi prompt request")
		os.Exit(2)
	}
	var request promptRequest
	if err := json.Unmarshal(scanner.Bytes(), &request); err != nil || request.ID != "carry-prompt" || request.Type != "prompt" {
		fmt.Fprintln(os.Stderr, "invalid Pi prompt request")
		os.Exit(2)
	}

	var output any
	switch {
	case strings.Contains(request.Message, delegationText):
		goal := delegationGoal
		output = strictReply{Reply: delegationReply, DelegationGoal: &goal}
	case strings.Contains(request.Message, questionText):
		output = strictReply{Reply: questionReply, DelegationGoal: nil}
	case strings.Contains(request.Message, delegationGoal):
		output = strictUnderstanding{
			Understanding: "Carry owns the renewal brief.",
			NextStep:      "Draft the renewal brief.",
		}
	case strings.Contains(request.Message, "Work context (untrusted JSON):"):
		output = strictUnderstanding{
			Understanding: "Carry retained this pre-existing Work context.",
			NextStep:      "Continue from the durable Work.",
		}
	default:
		fmt.Fprintln(os.Stderr, "unexpected Carry prompt")
		os.Exit(2)
	}
	text, err := json.Marshal(output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	encoder := json.NewEncoder(os.Stdout)
	mustEncode(encoder, map[string]any{
		"id": "carry-prompt", "type": "response", "command": "prompt", "success": true,
	})
	mustEncode(encoder, map[string]any{
		"type": "message_end",
		"message": map[string]any{
			"role":       "assistant",
			"content":    []map[string]string{{"type": "text", "text": string(text)}},
			"stopReason": "stop",
		},
	})
	mustEncode(encoder, map[string]any{"type": "agent_settled"})
}

func mustEncode(encoder *json.Encoder, value any) {
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
