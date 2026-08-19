package host

import (
	"errors"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/run"
)

func TestExecutionPromptContainsOnlyAuthorizedWorkContext(t *testing.T) {
	request := ExecutionRequest{
		Goal: "Prepare the renewal brief", CurrentUnderstanding: "Finance approved the term.",
		CurrentNextStep: "Apply legal wording.",
		Inputs:          []run.Input{{Sequence: 3, Kind: run.InputMessage, AuthorUserID: "member-1", Text: "Legal supplied wording"}},
	}
	prompt, err := request.Prompt()
	if err != nil {
		t.Fatalf("build Agent prompt: %v", err)
	}
	for _, expected := range []string{
		request.Goal,
		request.CurrentUnderstanding,
		request.CurrentNextStep,
		"Legal supplied wording",
		`"sequence":3`,
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt does not contain %q: %s", expected, prompt)
		}
	}
	for _, forbidden := range []string{"writer_token", "agent_credential", "carry_agent_"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt contains authority field %q: %s", forbidden, prompt)
		}
	}
}

func TestParseDraftRequiresOneExactObject(t *testing.T) {
	draft, err := ParseDraft([]byte(`{"understanding":"  Finance approved the term.  ","next_step":"  Apply legal wording.  "}`))
	if err != nil {
		t.Fatalf("parse current understanding draft: %v", err)
	}
	if draft.Understanding != "Finance approved the term." || draft.NextStep != "Apply legal wording." {
		t.Fatalf("normalized draft = %#v", draft)
	}
	invalid := []string{
		`{"understanding":"Known","next_step":"Continue","authority":"granted"}`,
		`{"understanding":"Known","next_step":""}`,
		`{"understanding":"Known","next_step":"Continue"} {}`,
		"not json",
	}
	for _, data := range invalid {
		if _, err := ParseDraft([]byte(data)); !errors.Is(err, ErrInvalidAgentDraft) {
			t.Fatalf("invalid draft %q error = %v", data, err)
		}
	}
}
