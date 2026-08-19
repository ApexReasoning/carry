package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// RuntimeKind identifies a concrete native Agent runtime supported by Carry.
type RuntimeKind string

const (
	RuntimePi    RuntimeKind = "pi"
	RuntimeCodex RuntimeKind = "codex"
)

type RuntimeDetection string

const (
	RuntimeDetected    RuntimeDetection = "detected"
	RuntimeNotFound    RuntimeDetection = "not_found"
	RuntimeProbeFailed RuntimeDetection = "probe_failed"
)

// RuntimeObservation records one bounded physical installation probe.
type RuntimeObservation struct {
	Kind             RuntimeKind
	Detection        RuntimeDetection
	Executable       string
	Version          string
	DiagnosticCode   string
	DiagnosticDetail string
	ObservedAt       time.Time
}

type runtimeDefinition struct {
	kind             RuntimeKind
	binaryName       string
	versionArguments []string
}

// This closed list describes Node 0 installation probes, not Agent execution
// adapters or a provider registry. Detection proves only that --version works.
var supportedRuntimes = [...]runtimeDefinition{
	{kind: RuntimePi, binaryName: "pi", versionArguments: []string{"--version"}},
	{kind: RuntimeCodex, binaryName: "codex", versionArguments: []string{"--version"}},
}

var errProbeTimeout = errors.New("runtime version probe timed out")

// DetectRuntimes probes only the closed Node 0 runtime definitions.
func DetectRuntimes(ctx context.Context) []RuntimeObservation {
	detector := runtimeDetector{
		lookPath:     exec.LookPath,
		probeVersion: probeRuntimeVersion,
		now:          time.Now,
	}
	return detector.detect(ctx)
}

type runtimeDetector struct {
	lookPath     func(string) (string, error)
	probeVersion func(context.Context, string, []string) (string, error)
	now          func() time.Time
}

func (d runtimeDetector) detect(ctx context.Context) []RuntimeObservation {
	observedAt := d.now().UTC()
	observations := make([]RuntimeObservation, 0, len(supportedRuntimes))
	for _, definition := range supportedRuntimes {
		observations = append(observations, d.detectOne(ctx, definition, observedAt))
	}
	return observations
}

func (d runtimeDetector) detectOne(ctx context.Context, definition runtimeDefinition, observedAt time.Time) RuntimeObservation {
	observation := RuntimeObservation{Kind: definition.kind, ObservedAt: observedAt}
	binaryPath, err := d.lookPath(definition.binaryName)
	if errors.Is(err, exec.ErrNotFound) {
		observation.Detection = RuntimeNotFound
		return observation
	}
	if err != nil {
		observation.Detection = RuntimeProbeFailed
		observation.DiagnosticCode = "lookup_failed"
		observation.DiagnosticDetail = boundedDetail(err.Error())
		return observation
	}

	version, err := d.probeVersion(ctx, binaryPath, definition.versionArguments)
	if err != nil {
		observation.Detection = RuntimeProbeFailed
		observation.DiagnosticCode = probeDiagnosticCode(err)
		observation.DiagnosticDetail = boundedDetail(err.Error())
		return observation
	}
	if strings.TrimSpace(version) == "" {
		observation.Detection = RuntimeProbeFailed
		observation.DiagnosticCode = "invalid_output"
		observation.DiagnosticDetail = "version command returned empty output"
		return observation
	}

	observation.Detection = RuntimeDetected
	observation.Executable = binaryPath
	observation.Version = strings.TrimSpace(version)
	return observation
}

func probeRuntimeVersion(ctx context.Context, binaryPath string, arguments []string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var output limitedBuffer
	output.remaining = 4096
	process := exec.CommandContext(probeCtx, binaryPath, arguments...)
	process.Stdin = strings.NewReader("")
	process.Stdout = &output
	process.Stderr = &output
	if err := process.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return "", errProbeTimeout
		}
		return "", fmt.Errorf("run version command: %w", err)
	}
	if output.truncated {
		return "", errors.New("version command output exceeded 4096 bytes")
	}
	return output.String(), nil
}

func probeDiagnosticCode(err error) string {
	if errors.Is(err, errProbeTimeout) {
		return "probe_timeout"
	}
	return "probe_failed"
}

func boundedDetail(detail string) string {
	const limit = 256
	detail = strings.TrimSpace(detail)
	if len(detail) <= limit {
		return detail
	}
	return detail[:limit]
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	if len(data) > b.remaining {
		data = data[:b.remaining]
		b.truncated = true
	}
	if len(data) != 0 {
		_, _ = b.buffer.Write(data)
		b.remaining -= len(data)
	}
	return originalLength, nil
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
