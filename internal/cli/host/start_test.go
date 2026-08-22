package host

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
)

func TestAgentReportWarningsNameExactOperatorRecoveries(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	writeAgentReportWarnings(&output, machine.AgentReportResult{
		UnsupportedAdapterKeys:   []agent.AdapterKey{"future"},
		SetupRequiredAdapterKeys: []agent.AdapterKey{"codex"},
	})
	got := output.String()
	if !strings.Contains(got, `Adapter "future" is not supported by this carry-server. Update carry-server.`) ||
		!strings.Contains(got, `Carry cannot add "codex" on this Host because its approving member is no longer active. Revoke this Host and run carry setup again.`) {
		t.Fatalf("warnings = %q", got)
	}
}

func TestAgentReportTerminalRecoveryRequiresSetup(t *testing.T) {
	t.Parallel()
	if got := agentReportTerminalError(machine.ErrMachineUnavailable).Error(); !strings.Contains(got, "run carry setup again") {
		t.Fatalf("unavailable recovery = %q", got)
	}
	if got := agentReportTerminalError(machine.ErrMachineRevoked).Error(); !strings.Contains(got, "run carry setup") {
		t.Fatalf("revoked recovery = %q", got)
	}
	unexpected := errors.New("wire broke")
	if !errors.Is(agentReportTerminalError(unexpected), unexpected) {
		t.Fatal("unexpected report failure lost its cause")
	}
}
