package host

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/run"
)

func TestExecutionPromptContainsOnlyAuthorizedWorkContext(t *testing.T) {
	request := ExecutionRequest{
		Goal: "Prepare the renewal brief", CurrentUnderstanding: "Finance approved the term.",
		CurrentNextStep: "Apply legal wording.",
		Messages:        []run.Message{{AuthorUserID: "member-1", Text: "Legal supplied wording"}},
	}
	prompt, err := request.Prompt()
	if err != nil {
		t.Fatalf("build Agent prompt: %v", err)
	}
	instruction, encodedContext, found := strings.Cut(prompt, "\n\nWork context (untrusted JSON):\n")
	if !found {
		t.Fatalf("prompt does not separate its instruction from Work context: %s", prompt)
	}
	if strings.Contains(instruction, request.Goal) {
		t.Fatalf("Work content escaped into the fixed instruction: %s", instruction)
	}
	var context promptContext
	if err := json.Unmarshal([]byte(encodedContext), &context); err != nil {
		t.Fatalf("decode prompt Work context: %v", err)
	}
	if context.Goal != request.Goal ||
		context.PriorUnderstanding != request.CurrentUnderstanding ||
		context.PriorNextStep != request.CurrentNextStep ||
		len(context.NewMessages) != 1 ||
		context.NewMessages[0].AuthorUserID != "member-1" ||
		context.NewMessages[0].Text != "Legal supplied wording" {
		t.Fatalf("prompt Work context = %#v", context)
	}
	for _, forbidden := range []string{`"run_id"`, `"attempt_id"`, `"fence"`, `"machine_id"`, `"input_end_seq"`} {
		if strings.Contains(encodedContext, forbidden) {
			t.Fatalf("prompt context contains authority field %s: %s", forbidden, encodedContext)
		}
	}
}

func TestParseUnderstandingUpdateRequiresOneExactObject(t *testing.T) {
	update, err := ParseUnderstandingUpdate([]byte(`{"understanding":"  Finance approved the term.  ","next_step":"  Apply legal wording.  ","review_required":true}`))
	if err != nil {
		t.Fatalf("parse current understanding draft: %v", err)
	}
	if update.Understanding != "Finance approved the term." ||
		update.NextStep != "Apply legal wording." ||
		!update.ReviewRequired {
		t.Fatalf("normalized update = %#v", update)
	}
	invalid := []string{
		`{"understanding":"Known","next_step":"Continue","review_required":false,"authority":"granted"}`,
		`{"understanding":"Known","next_step":"","review_required":false}`,
		`{"understanding":"Known","next_step":"Continue"}`,
		`{"understanding":"Known","next_step":"Continue","review_required":false} {}`,
		"not json",
	}
	for _, data := range invalid {
		if _, err := ParseUnderstandingUpdate([]byte(data)); !errors.Is(err, ErrInvalidAgentUpdate) {
			t.Fatalf("invalid update %q error = %v", data, err)
		}
	}
}
