package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunBuildsFreshCommandTree(t *testing.T) {
	t.Parallel()

	for range 2 {
		var output bytes.Buffer
		var errorOutput bytes.Buffer
		exitCode := Run(
			context.Background(), nil, "test-version", t.TempDir(),
			Streams{Input: strings.NewReader(""), Output: &output, ErrorOutput: &errorOutput},
			nil, nil,
		)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, stderr = %q", exitCode, errorOutput.String())
		}
		if !strings.Contains(output.String(), "Carry keeps team Work moving") ||
			!strings.Contains(output.String(), "host") {
			t.Fatalf("root help = %q", output.String())
		}
	}
}

func TestRunPrintsHelpAndVersionFlag(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		arguments []string
		contains  string
	}{
		{name: "help", arguments: nil, contains: "Carry keeps team Work moving"},
		{name: "version flag", arguments: []string{"--version"}, contains: "test-version\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var errorOutput bytes.Buffer
			exitCode := Run(
				context.Background(), test.arguments, "test-version", t.TempDir(),
				Streams{Input: strings.NewReader(""), Output: &output, ErrorOutput: &errorOutput},
				nil, nil,
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

func TestRunRejectsUnknownAndRemovedVersionCommands(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"unknown", "version"} {
		var output bytes.Buffer
		var errorOutput bytes.Buffer
		exitCode := Run(
			context.Background(), []string{command}, "test-version", t.TempDir(),
			Streams{Input: strings.NewReader(""), Output: &output, ErrorOutput: &errorOutput},
			nil, nil,
		)
		if exitCode != 1 {
			t.Fatalf("%s exit code = %d, want 1", command, exitCode)
		}
		if !strings.Contains(errorOutput.String(), `unknown command "`+command+`"`) {
			t.Fatalf("%s stderr = %q", command, errorOutput.String())
		}
	}
}
