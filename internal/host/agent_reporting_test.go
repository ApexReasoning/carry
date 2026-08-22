package host

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
)

func TestAgentReporterRetriesUnknownExactBodyAndNoticesOncePerFailureClass(t *testing.T) {
	adapter := &stubAdapter{
		key: "pi",
		occurrences: []Occurrence{{
			Key:      "default",
			Present:  true,
			Executor: &stubExecutor{},
		}},
	}
	set, err := NewAdapterSet(adapter)
	if err != nil {
		t.Fatalf("construct adapter set: %v", err)
	}
	unknown := errors.New("response was lost after submission")
	client := &scriptedAgentReports{steps: []agentReportStep{
		{err: unknown},
		{err: machine.ErrAgentReportTemporarilyUnavailable},
		{err: machine.ErrAgentReportTemporarilyUnavailable},
		{result: machine.AgentReportResult{Revision: 1}},
	}}
	var notices []error
	reporter, err := NewAgentReporter(set, client, func(reportErr error) {
		notices = append(notices, reportErr)
	})
	if err != nil {
		t.Fatalf("construct Agent reporter: %v", err)
	}
	if reporter.retryInterval != AgentReportRetryInterval || reporter.reportInterval != machine.AgentReportInterval {
		t.Fatalf("production retry/cadence = %s/%s", reporter.retryInterval, reporter.reportInterval)
	}
	reporter.retryInterval = time.Millisecond

	snapshot, result, err := reporter.Report(context.Background())
	if err != nil {
		t.Fatalf("report with exact retries: %v", err)
	}
	if result.Revision != 1 || adapter.calls != 1 || len(snapshot.Observations()) != 1 {
		t.Fatalf("accepted report = %#v, observations = %#v, adapter calls = %d", result, snapshot.Observations(), adapter.calls)
	}
	if len(notices) != 2 || !errors.Is(notices[0], unknown) ||
		!errors.Is(notices[1], machine.ErrAgentReportTemporarilyUnavailable) {
		t.Fatalf("retry notices = %#v", notices)
	}
	if len(client.calls) != 4 {
		t.Fatalf("report transport calls = %d", len(client.calls))
	}
	first := client.calls[0]
	for _, call := range client.calls[1:] {
		if call.reportID != first.reportID || call.baseRevision != first.baseRevision ||
			!reflect.DeepEqual(call.observations, first.observations) {
			t.Fatalf("retry changed exact report: first %#v, retry %#v", first, call)
		}
	}
}

func TestAgentReporterReobservesWithNewIdentityAfterStaleRevision(t *testing.T) {
	adapter := &changingStubAdapter{key: "pi"}
	set, err := NewAdapterSet(adapter)
	if err != nil {
		t.Fatalf("construct adapter set: %v", err)
	}
	client := &scriptedAgentReports{steps: []agentReportStep{
		{err: machine.AgentReportStaleError{CurrentRevision: 7}},
		{result: machine.AgentReportResult{Revision: 8}},
	}}
	reporter, err := NewAgentReporter(set, client, func(error) {})
	if err != nil {
		t.Fatalf("construct Agent reporter: %v", err)
	}

	snapshot, result, err := reporter.Report(context.Background())
	if err != nil {
		t.Fatalf("re-observe stale Agent report: %v", err)
	}
	if adapter.calls != 2 || result.Revision != 8 || snapshot.Observations()[0].Present {
		t.Fatalf("stale recovery observations = %#v, calls = %d, result = %#v", snapshot.Observations(), adapter.calls, result)
	}
	if len(client.calls) != 2 || client.calls[0].baseRevision != 0 || client.calls[1].baseRevision != 7 {
		t.Fatalf("stale report bases = %#v", client.calls)
	}
	if client.calls[0].reportID == client.calls[1].reportID {
		t.Fatal("stale re-observation reused the damaged report identity")
	}
	if client.calls[0].observations[0].Present == client.calls[1].observations[0].Present {
		t.Fatal("stale recovery reused old observations instead of re-observing")
	}
}

func TestAgentReporterStopsOnTerminalRecoveryWithoutRetry(t *testing.T) {
	set, err := NewAdapterSet(&stubAdapter{key: "pi"})
	if err != nil {
		t.Fatalf("construct adapter set: %v", err)
	}
	for _, terminal := range []error{
		machine.ErrInvalidAgentReport,
		machine.ErrAgentReportConflict,
		machine.ErrMachineRevoked,
		machine.ErrMachineUnavailable,
		AgentReportRejectedError{StatusCode: 422},
	} {
		t.Run(terminal.Error(), func(t *testing.T) {
			client := &scriptedAgentReports{steps: []agentReportStep{{err: terminal}}}
			notices := 0
			reporter, err := NewAgentReporter(set, client, func(error) { notices++ })
			if err != nil {
				t.Fatalf("construct Agent reporter: %v", err)
			}
			_, _, reportErr := reporter.Report(context.Background())
			var wantRejected AgentReportRejectedError
			var gotRejected AgentReportRejectedError
			sameRecovery := errors.Is(reportErr, terminal) ||
				(errors.As(terminal, &wantRejected) && errors.As(reportErr, &gotRejected) && gotRejected.StatusCode == wantRejected.StatusCode)
			if !sameRecovery || len(client.calls) != 1 || notices != 0 {
				t.Fatalf("terminal report error = %v, calls = %d, notices = %d", reportErr, len(client.calls), notices)
			}
		})
	}
}

func TestAgentReporterServeWaitsAfterInitialReportAndEmitsOnlyRecoveryChanges(t *testing.T) {
	set, err := NewAdapterSet(&stubAdapter{key: "pi"})
	if err != nil {
		t.Fatalf("construct adapter set: %v", err)
	}
	client := &scriptedAgentReports{steps: []agentReportStep{
		{result: machine.AgentReportResult{Revision: 1}},
		{result: machine.AgentReportResult{Revision: 2}},
		{result: machine.AgentReportResult{
			Revision:               3,
			UnsupportedAdapterKeys: []agent.AdapterKey{"future"},
		}},
	}}
	reporter, err := NewAgentReporter(set, client, func(error) {})
	if err != nil {
		t.Fatalf("construct Agent reporter: %v", err)
	}
	if _, _, err := reporter.Report(context.Background()); err != nil {
		t.Fatalf("perform initial accepted report: %v", err)
	}
	reporter.reportInterval = 2 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var changed []machine.AgentReportResult
	done := make(chan error, 1)
	go func() {
		done <- reporter.Serve(ctx, func(result machine.AgentReportResult) {
			mu.Lock()
			changed = append(changed, result)
			mu.Unlock()
			if len(result.UnsupportedAdapterKeys) != 0 {
				cancel()
			}
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve periodic Agent reports: %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("periodic Agent reporting did not reach changed recovery facts")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(changed) != 1 || !reflect.DeepEqual(changed[0].UnsupportedAdapterKeys, []agent.AdapterKey{"future"}) {
		t.Fatalf("emitted report changes = %#v", changed)
	}
}

type changingStubAdapter struct {
	key   agent.AdapterKey
	calls int
}

func (adapter *changingStubAdapter) Key() agent.AdapterKey {
	return adapter.key
}

func (adapter *changingStubAdapter) Observe(context.Context) ([]Occurrence, error) {
	adapter.calls++
	return []Occurrence{{
		Key:      "default",
		Present:  adapter.calls == 1,
		Executor: executorWhen(adapter.calls == 1),
	}}, nil
}

func executorWhen(present bool) Executor {
	if !present {
		return nil
	}
	return &stubExecutor{}
}

type agentReportStep struct {
	result machine.AgentReportResult
	err    error
}

type agentReportCall struct {
	reportID     string
	baseRevision int64
	observations []machine.AgentObservation
}

type scriptedAgentReports struct {
	steps []agentReportStep
	calls []agentReportCall
}

func (reports *scriptedAgentReports) ReportAgentPresence(
	_ context.Context,
	reportID string,
	baseRevision int64,
	observations []machine.AgentObservation,
) (machine.AgentReportResult, error) {
	reports.calls = append(reports.calls, agentReportCall{
		reportID:     reportID,
		baseRevision: baseRevision,
		observations: append([]machine.AgentObservation(nil), observations...),
	})
	index := len(reports.calls) - 1
	if index >= len(reports.steps) {
		return machine.AgentReportResult{}, errors.New("unexpected Agent report call")
	}
	return reports.steps[index].result, reports.steps[index].err
}
