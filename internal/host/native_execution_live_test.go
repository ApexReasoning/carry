//go:build live

package host_test

import (
	"context"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/codex"
	"github.com/ApexReasoning/carry/internal/host/pi"
	"github.com/ApexReasoning/carry/internal/run"
)

func TestLiveNativeExecutorsAdvanceTheSameWorkContext(t *testing.T) {
	request := host.ExecutionRequest{
		Goal:                 "Compare three onboarding approaches",
		CurrentUnderstanding: "The approaches differ in setup time and support burden.",
		Messages: []run.Message{{
			Text: "The owner values a reversible first step.",
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
			update, err := testCase.executor.Execute(ctx, request)
			if err != nil {
				t.Fatalf("execute native Agent: %v", err)
			}
			if _, _, err := run.ValidateUnderstandingUpdate(update.Understanding, update.NextStep); err != nil {
				t.Fatalf("validate native Agent update: %v", err)
			}
		})
	}
}
