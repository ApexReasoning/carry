package host

import (
	"context"
	"errors"
	"testing"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
)

func TestSelectExecutorChoosesOneBeforeClaimingWork(t *testing.T) {
	pi := &diagnosticExecutor{}
	codex := &diagnosticExecutor{}
	selected, label, err := selectExecutor(context.Background(), pi, codex)
	if err != nil {
		t.Fatalf("select executor: %v", err)
	}
	if selected != pi || label != "Pi" || pi.diagnoses != 1 || codex.diagnoses != 0 {
		t.Fatalf("selection = %s, Pi diagnoses = %d, Codex diagnoses = %d", label, pi.diagnoses, codex.diagnoses)
	}
}

func TestSelectExecutorUsesCodexWhenPiIsUnavailable(t *testing.T) {
	pi := &diagnosticExecutor{diagnoseErr: hostdomain.ErrAgentUnavailable}
	codex := &diagnosticExecutor{}
	selected, label, err := selectExecutor(context.Background(), pi, codex)
	if err != nil {
		t.Fatalf("select executor: %v", err)
	}
	if selected != codex || label != "Codex" || pi.diagnoses != 1 || codex.diagnoses != 1 {
		t.Fatalf("selection = %s, Pi diagnoses = %d, Codex diagnoses = %d", label, pi.diagnoses, codex.diagnoses)
	}
}

func TestSelectExecutorReturnsBothDiagnostics(t *testing.T) {
	piFailure := errors.New("Pi installation is incomplete")
	codexFailure := errors.New("Codex installation is incomplete")
	_, _, err := selectExecutor(
		context.Background(),
		&diagnosticExecutor{diagnoseErr: piFailure},
		&diagnosticExecutor{diagnoseErr: codexFailure},
	)
	if !errors.Is(err, piFailure) || !errors.Is(err, codexFailure) {
		t.Fatalf("selection error = %v", err)
	}
}

type diagnosticExecutor struct {
	diagnoseErr error
	diagnoses   int
}

func (executor *diagnosticExecutor) Diagnose(context.Context) error {
	executor.diagnoses++
	return executor.diagnoseErr
}

func (*diagnosticExecutor) Execute(context.Context, hostdomain.ExecutionRequest) (hostdomain.UnderstandingUpdate, error) {
	return hostdomain.UnderstandingUpdate{}, errors.New("not implemented")
}

var _ hostdomain.Executor = (*diagnosticExecutor)(nil)
