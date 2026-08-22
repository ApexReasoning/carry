package host

import (
	"context"
	"errors"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/google/uuid"
)

const AgentReportRetryInterval = 5 * time.Second

// AgentReportRejectedError is a known HTTP rejection that must not be replayed.
type AgentReportRejectedError struct {
	StatusCode int
}

func (err AgentReportRejectedError) Error() string {
	return "Agent report was rejected"
}

// AgentReports is the Host consumer's narrow Machine-presence transport.
type AgentReports interface {
	ReportAgentPresence(context.Context, string, int64, []machine.AgentObservation) (machine.AgentReportResult, error)
}

// AgentReporter owns complete local observation, exact retry identity, and report cadence.
type AgentReporter struct {
	adapters        AdapterSet
	reports         AgentReports
	retryNotice     func(error)
	currentRevision int64
	retryInterval   time.Duration
	reportInterval  time.Duration
	lastResult      machine.AgentReportResult
	hasResult       bool
}

// NewAgentReporter constructs a serial reporting loop. It starts no background work.
func NewAgentReporter(adapters AdapterSet, reports AgentReports, retryNotice func(error)) (*AgentReporter, error) {
	if len(adapters.adapters) == 0 || reports == nil || retryNotice == nil {
		return nil, errors.New("Agent reporter dependencies are required")
	}
	return &AgentReporter{
		adapters:       adapters,
		reports:        reports,
		retryNotice:    retryNotice,
		retryInterval:  AgentReportRetryInterval,
		reportInterval: machine.AgentReportInterval,
	}, nil
}

// Report observes one complete snapshot and retries only when the exact report body is safe to replay.
func (reporter *AgentReporter) Report(ctx context.Context) (Snapshot, machine.AgentReportResult, error) {
	for {
		snapshot, err := reporter.adapters.Observe(ctx)
		if err != nil {
			return Snapshot{}, machine.AgentReportResult{}, err
		}
		reportID := uuid.NewString()
		baseRevision := reporter.currentRevision
		observations := snapshot.Observations()
		blockedClass := agentReportFailureNone
		for {
			result, reportErr := reporter.reports.ReportAgentPresence(
				ctx,
				reportID,
				baseRevision,
				append([]machine.AgentObservation(nil), observations...),
			)
			if reportErr == nil {
				reporter.currentRevision = result.Revision
				reporter.lastResult = copyAgentReportResult(result)
				reporter.hasResult = true
				return snapshot, result, nil
			}
			if ctx.Err() != nil {
				return Snapshot{}, machine.AgentReportResult{}, ctx.Err()
			}
			var stale machine.AgentReportStaleError
			if errors.As(reportErr, &stale) {
				reporter.currentRevision = stale.CurrentRevision
				break
			}
			var rejected AgentReportRejectedError
			if errors.Is(reportErr, machine.ErrInvalidAgentReport) ||
				errors.Is(reportErr, machine.ErrAgentReportConflict) ||
				errors.Is(reportErr, machine.ErrMachineRevoked) ||
				errors.Is(reportErr, machine.ErrMachineUnavailable) ||
				errors.As(reportErr, &rejected) {
				return Snapshot{}, machine.AgentReportResult{}, reportErr
			}

			failureClass := agentReportFailureUnknown
			if errors.Is(reportErr, machine.ErrAgentReportTemporarilyUnavailable) {
				failureClass = agentReportFailureKnownNoWrite
			}
			if blockedClass != failureClass {
				reporter.retryNotice(reportErr)
				blockedClass = failureClass
			}
			if !waitForAgentReport(ctx, reporter.retryInterval) {
				return Snapshot{}, machine.AgentReportResult{}, ctx.Err()
			}
		}
	}
}

// Serve reports on the fixed cadence and emits only changed operator recovery facts.
func (reporter *AgentReporter) Serve(ctx context.Context, resultChanged func(machine.AgentReportResult)) error {
	if resultChanged == nil {
		return errors.New("Agent report result callback is required")
	}
	for {
		if reporter.hasResult && !waitForAgentReport(ctx, reporter.reportInterval) {
			return nil
		}
		previous := copyAgentReportResult(reporter.lastResult)
		hadPrevious := reporter.hasResult
		_, result, err := reporter.Report(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !hadPrevious || !sameAgentReportRecovery(previous, result) {
			resultChanged(copyAgentReportResult(result))
		}
	}
}

type agentReportFailureClass uint8

const (
	agentReportFailureNone agentReportFailureClass = iota
	agentReportFailureKnownNoWrite
	agentReportFailureUnknown
)

func waitForAgentReport(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func copyAgentReportResult(result machine.AgentReportResult) machine.AgentReportResult {
	result.UnsupportedAdapterKeys = append(result.UnsupportedAdapterKeys[:0:0], result.UnsupportedAdapterKeys...)
	result.SetupRequiredAdapterKeys = append(result.SetupRequiredAdapterKeys[:0:0], result.SetupRequiredAdapterKeys...)
	return result
}

func sameAgentReportRecovery(left, right machine.AgentReportResult) bool {
	if len(left.UnsupportedAdapterKeys) != len(right.UnsupportedAdapterKeys) ||
		len(left.SetupRequiredAdapterKeys) != len(right.SetupRequiredAdapterKeys) {
		return false
	}
	for index := range left.UnsupportedAdapterKeys {
		if left.UnsupportedAdapterKeys[index] != right.UnsupportedAdapterKeys[index] {
			return false
		}
	}
	for index := range left.SetupRequiredAdapterKeys {
		if left.SetupRequiredAdapterKeys[index] != right.SetupRequiredAdapterKeys[index] {
			return false
		}
	}
	return true
}
