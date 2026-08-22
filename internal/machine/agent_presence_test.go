package machine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/google/uuid"
)

func TestAgentPresenceCanonicalizesCompleteReportAndPartitionsUnsupportedFamilies(t *testing.T) {
	vocabulary, err := agent.NewVocabulary(
		agent.Descriptor{
			Key:                     "pi",
			NameBase:                "Pi",
			MaxOccurrencesPerReport: 1,
		},
		agent.Descriptor{
			Key:                     "deepseek-harness",
			NameBase:                "DeepSeek",
			MaxOccurrencesPerReport: 2,
		},
	)
	if err != nil {
		t.Fatalf("construct vocabulary: %v", err)
	}
	persistence := &recordingAgentPresencePersistence{result: AgentReportResult{Revision: 8}}
	presence, err := NewAgentPresence(persistence, vocabulary)
	if err != nil {
		t.Fatalf("construct Agent presence: %v", err)
	}
	machineID := uuid.NewString()
	reportID := uuid.NewString()
	request := AgentReportRequest{
		MachineID:         machineID,
		CertificateSerial: "42",
		ReportID:          reportID,
		BaseRevision:      7,
		Observations: []AgentObservation{
			{
				AdapterKey:    "future",
				OccurrenceKey: "default",
				Present:       true,
			},
			{
				AdapterKey:    "deepseek-harness",
				OccurrenceKey: "second",
				Present:       false,
			},
			{
				AdapterKey:    "pi",
				OccurrenceKey: "default",
				Present:       true,
			},
			{
				AdapterKey:    "deepseek-harness",
				OccurrenceKey: "first",
				Present:       true,
			},
			{
				AdapterKey:    "another-future",
				OccurrenceKey: "default",
				Present:       false,
			},
		},
	}
	original := append([]AgentObservation(nil), request.Observations...)

	result, err := presence.Report(context.Background(), request)
	if err != nil {
		t.Fatalf("report complete Agent presence: %v", err)
	}
	if result.Revision != 8 || persistence.calls != 1 {
		t.Fatalf("report result = %#v, persistence calls = %d", result, persistence.calls)
	}
	if !reflect.DeepEqual(request.Observations, original) {
		t.Fatalf("Report mutated caller observations: %#v", request.Observations)
	}
	command := persistence.commands[0]
	if !reflect.DeepEqual(command.UnsupportedAdapterKeys, []agent.AdapterKey{"another-future", "future"}) {
		t.Fatalf("unsupported families = %#v", command.UnsupportedAdapterKeys)
	}
	wantRecognized := []RecognizedObservation{
		{
			AdapterKey:    "deepseek-harness",
			OccurrenceKey: "first",
			Present:       true,
			NameBase:      "DeepSeek",
		},
		{
			AdapterKey:    "deepseek-harness",
			OccurrenceKey: "second",
			Present:       false,
			NameBase:      "DeepSeek",
		},
		{
			AdapterKey:    "pi",
			OccurrenceKey: "default",
			Present:       true,
			NameBase:      "Pi",
		},
	}
	if !reflect.DeepEqual(command.Recognized, wantRecognized) {
		t.Fatalf("recognized observations = %#v", command.Recognized)
	}

	reordered := request
	reordered.Observations = []AgentObservation{
		request.Observations[4],
		request.Observations[2],
		request.Observations[0],
		request.Observations[3],
		request.Observations[1],
	}
	if _, err := presence.Report(context.Background(), reordered); err != nil {
		t.Fatalf("report reordered semantic set: %v", err)
	}
	if persistence.commands[1].RequestDigest != command.RequestDigest {
		t.Fatal("semantic report digest changed with observation order")
	}
}

func TestAgentPresenceRejectsMalformedDuplicateOversizeAndOverCardinalityReports(t *testing.T) {
	vocabulary, err := agent.NewVocabulary(agent.Descriptor{
		Key:                     "pi",
		NameBase:                "Pi",
		MaxOccurrencesPerReport: 1,
	})
	if err != nil {
		t.Fatalf("construct vocabulary: %v", err)
	}
	persistence := &recordingAgentPresencePersistence{}
	presence, err := NewAgentPresence(persistence, vocabulary)
	if err != nil {
		t.Fatalf("construct Agent presence: %v", err)
	}
	valid := AgentReportRequest{
		MachineID:         uuid.NewString(),
		CertificateSerial: "42",
		ReportID:          uuid.NewString(),
		Observations: []AgentObservation{{
			AdapterKey:    "pi",
			OccurrenceKey: "default",
			Present:       true,
		}},
	}
	tests := []struct {
		name   string
		mutate func(*AgentReportRequest)
	}{
		{
			name: "machine identity",
			mutate: func(request *AgentReportRequest) {
				request.MachineID = "not-a-uuid"
			},
		},
		{
			name: "report identity",
			mutate: func(request *AgentReportRequest) {
				request.ReportID = "not-a-uuid"
			},
		},
		{
			name: "certificate",
			mutate: func(request *AgentReportRequest) {
				request.CertificateSerial = " "
			},
		},
		{
			name: "base revision",
			mutate: func(request *AgentReportRequest) {
				request.BaseRevision = -1
			},
		},
		{
			name: "adapter key",
			mutate: func(request *AgentReportRequest) {
				request.Observations[0].AdapterKey = "Pi"
			},
		},
		{
			name: "occurrence key",
			mutate: func(request *AgentReportRequest) {
				request.Observations[0].OccurrenceKey = ".default"
			},
		},
		{
			name: "duplicate pair",
			mutate: func(request *AgentReportRequest) {
				request.Observations = append(request.Observations, request.Observations[0])
			},
		},
		{
			name: "descriptor cardinality",
			mutate: func(request *AgentReportRequest) {
				request.Observations = append(request.Observations, AgentObservation{
					AdapterKey:    "pi",
					OccurrenceKey: "second",
				})
			},
		},
		{
			name: "complete report bound",
			mutate: func(request *AgentReportRequest) {
				request.Observations = make([]AgentObservation, agent.MaxObservationsPerReport+1)
				for index := range request.Observations {
					request.Observations[index] = AgentObservation{
						AdapterKey:    "future",
						OccurrenceKey: agent.OccurrenceKey("occurrence-" + strings.Repeat("x", index)),
					}
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			request.Observations = append([]AgentObservation(nil), valid.Observations...)
			test.mutate(&request)
			if _, err := presence.Report(context.Background(), request); !errors.Is(err, ErrInvalidAgentReport) {
				t.Fatalf("Report error = %v, want invalid report", err)
			}
		})
	}
	if persistence.calls != 0 {
		t.Fatalf("invalid reports reached persistence %d times", persistence.calls)
	}
}

func TestAgentPresencePreservesRecoveryErrors(t *testing.T) {
	vocabulary := agent.NativeVocabulary()
	request := AgentReportRequest{
		MachineID:         uuid.NewString(),
		CertificateSerial: "42",
		ReportID:          uuid.NewString(),
	}
	stale := AgentReportStaleError{CurrentRevision: 9}
	for _, expected := range []error{
		stale,
		ErrAgentReportConflict,
		ErrMachineRevoked,
		ErrMachineUnavailable,
		ErrAgentReportTemporarilyUnavailable,
	} {
		t.Run(expected.Error(), func(t *testing.T) {
			persistence := &recordingAgentPresencePersistence{err: expected}
			presence, err := NewAgentPresence(persistence, vocabulary)
			if err != nil {
				t.Fatalf("construct Agent presence: %v", err)
			}
			_, reportErr := presence.Report(context.Background(), request)
			var staleError AgentReportStaleError
			if errors.As(expected, &staleError) {
				if !errors.As(reportErr, &staleError) || staleError.CurrentRevision != 9 {
					t.Fatalf("stale report error = %v", reportErr)
				}
				return
			}
			if !errors.Is(reportErr, expected) {
				t.Fatalf("report error = %v, want %v", reportErr, expected)
			}
		})
	}
}

type recordingAgentPresencePersistence struct {
	commands []AgentReportCommand
	result   AgentReportResult
	err      error
	calls    int
}

func (persistence *recordingAgentPresencePersistence) ReconcileAgentPresence(
	_ context.Context,
	command AgentReportCommand,
) (AgentReportResult, error) {
	persistence.calls++
	persistence.commands = append(persistence.commands, command)
	return persistence.result, persistence.err
}
