//go:build live

package host_test

import (
	"context"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/agent/codex"
	"github.com/ApexReasoning/carry/internal/agent/pi"
	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
)

func TestLiveNativeExecutorsAdvanceTheSameWorkContext(t *testing.T) {
	request := host.ExecutionRequest{
		Goal:                 "Compare three onboarding approaches",
		CurrentUnderstanding: "The approaches differ in setup time and support burden.",
		Inputs: []run.Input{{
			Sequence: 2,
			Kind:     run.InputMessage,
			Text:     "The owner values a reversible first step.",
		}},
	}
	for _, testCase := range []struct {
		name     string
		executor host.Executor
	}{
		{name: "Pi 0.84.2", executor: pi.New()},
		{name: "Codex 0.148.0", executor: codex.New()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := testCase.executor.Diagnose(ctx); err != nil {
				t.Fatalf("diagnose native Agent: %v", err)
			}
			draft, err := testCase.executor.Execute(ctx, request)
			if err != nil {
				t.Fatalf("execute native Agent: %v", err)
			}
			if _, _, err := run.ValidateDraft(draft.Understanding, draft.NextStep); err != nil {
				t.Fatalf("validate native Agent draft: %v", err)
			}
		})
	}
}
