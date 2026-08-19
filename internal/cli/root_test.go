package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
)

func TestRunBuildsFreshCommandTree(t *testing.T) {
	t.Parallel()

	detect := func(context.Context) []host.RuntimeObservation {
		return []host.RuntimeObservation{
			{Kind: host.RuntimePi, Detection: host.RuntimeDetected, Version: "0.84.2", ObservedAt: time.Now()},
			{Kind: host.RuntimeCodex, Detection: host.RuntimeNotFound, ObservedAt: time.Now()},
		}
	}
	for range 2 {
		var output bytes.Buffer
		var errorOutput bytes.Buffer
		exitCode := Run(
			context.Background(),
			[]string{"host", "status"},
			"test-version",
			t.TempDir(),
			Streams{Input: strings.NewReader(""), Output: &output, ErrorOutput: &errorOutput},
			detect,
			nil,
			nil,
		)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, stderr = %q", exitCode, errorOutput.String())
		}
		const expected = "Machine: not enrolled\nRuntime pi: detected (0.84.2)\nRuntime codex: not found\n"
		if got := output.String(); got != expected {
			t.Fatalf("output = %q, want %q", got, expected)
		}
	}
}

func TestRunPrintsHelpAndVersion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		arguments []string
		contains  string
	}{
		{name: "help", arguments: nil, contains: "Carry keeps team Work moving"},
		{name: "version command", arguments: []string{"version"}, contains: "test-version\n"},
		{name: "version flag", arguments: []string{"--version"}, contains: "test-version\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var errorOutput bytes.Buffer
			exitCode := Run(
				context.Background(), test.arguments, "test-version", t.TempDir(),
				Streams{Input: strings.NewReader(""), Output: &output, ErrorOutput: &errorOutput},
				func(context.Context) []host.RuntimeObservation { return nil },
				nil,
				nil,
			)
			if exitCode != 0 {
				t.Fatalf("exit code = %d, stderr = %q", exitCode, errorOutput.String())
			}
			if !strings.Contains(output.String(), test.contains) {
				t.Fatalf("output %q does not contain %q", output.String(), test.contains)
			}
		})
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := Run(
		context.Background(), []string{"unknown"}, "test-version", t.TempDir(),
		Streams{Input: strings.NewReader(""), Output: &output, ErrorOutput: &errorOutput},
		func(context.Context) []host.RuntimeObservation { return nil },
		nil,
		nil,
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(errorOutput.String(), `unknown command "unknown"`) {
		t.Fatalf("stderr = %q", errorOutput.String())
	}
}
