package host

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestRuntimeDetectorReportsPiAndMissingCodex(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	detector := runtimeDetector{
		lookPath: func(name string) (string, error) {
			switch name {
			case "pi":
				return "/opt/carry/bin/pi", nil
			case "codex":
				return "", exec.ErrNotFound
			default:
				t.Fatalf("unexpected runtime lookup %q", name)
				return "", errors.New("unreachable")
			}
		},
		probeVersion: func(_ context.Context, binaryPath string, arguments []string) (string, error) {
			if binaryPath != "/opt/carry/bin/pi" {
				t.Fatalf("unexpected executable %q", binaryPath)
			}
			if len(arguments) != 1 || arguments[0] != "--version" {
				t.Fatalf("unexpected version arguments %q", arguments)
			}
			return "0.84.2", nil
		},
		now: func() time.Time { return observedAt },
	}

	observations := detector.detect(context.Background())

	if len(observations) != 2 {
		t.Fatalf("observation count = %d, want 2", len(observations))
	}
	if got := observations[0]; got.Kind != RuntimePi || got.Detection != RuntimeDetected || got.Executable != "/opt/carry/bin/pi" || got.Version != "0.84.2" || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("Pi observation = %#v", got)
	}
	if got := observations[1]; got.Kind != RuntimeCodex || got.Detection != RuntimeNotFound || got.Executable != "" || got.Version != "" || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("Codex observation = %#v", got)
	}
}

func TestRuntimeDetectorKeepsProbeFailureDistinctFromMissing(t *testing.T) {
	t.Parallel()

	detector := runtimeDetector{
		lookPath: func(string) (string, error) { return "/opt/carry/bin/runtime", nil },
		probeVersion: func(_ context.Context, _ string, _ []string) (string, error) {
			return "", errProbeTimeout
		},
		now: time.Now,
	}

	observations := detector.detect(context.Background())

	for _, observation := range observations {
		if observation.Detection != RuntimeProbeFailed {
			t.Errorf("%s detection = %s, want %s", observation.Kind, observation.Detection, RuntimeProbeFailed)
		}
		if observation.DiagnosticCode != "probe_timeout" {
			t.Errorf("%s diagnostic code = %q, want probe_timeout", observation.Kind, observation.DiagnosticCode)
		}
	}
}
