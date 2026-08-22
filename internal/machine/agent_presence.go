package machine

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/google/uuid"
)

const (
	AgentReportInterval       = 10 * time.Second
	AgentPresenceFreshness    = 45 * time.Second
	maxCertificateSerialBytes = 256
)

var (
	ErrInvalidAgentReport                = errors.New("Machine Agent report is invalid")
	ErrAgentReportConflict               = errors.New("Machine Agent report identity was reused with different observations")
	ErrAgentReportTemporarilyUnavailable = errors.New("Machine Agent report is temporarily unavailable")
)

// AgentReportStaleError tells a Host which accepted revision it must re-observe from.
type AgentReportStaleError struct {
	CurrentRevision int64
}

func (stale AgentReportStaleError) Error() string {
	return "Machine Agent report is based on a stale revision"
}

// AgentObservation is one discovered adapter occurrence in a complete Machine snapshot.
type AgentObservation struct {
	AdapterKey    agent.AdapterKey
	OccurrenceKey agent.OccurrenceKey
	Present       bool
}

// AgentReportRequest carries only the report identity and Machine-owned observations.
type AgentReportRequest struct {
	MachineID         string
	CertificateSerial string
	ReportID          string
	BaseRevision      int64
	Observations      []AgentObservation
}

// AgentReportResult is the accepted PostgreSQL revision and bounded operator recovery facts.
type AgentReportResult struct {
	Revision                 int64
	UnsupportedAdapterKeys   []agent.AdapterKey
	SetupRequiredAdapterKeys []agent.AdapterKey
}

// RecognizedObservation combines a Machine observation with the immutable Agent descriptor fact needed for allocation.
type RecognizedObservation struct {
	AdapterKey    agent.AdapterKey
	OccurrenceKey agent.OccurrenceKey
	Present       bool
	NameBase      string
}

// AgentReportCommand is the canonical semantic report passed to PostgreSQL authority.
type AgentReportCommand struct {
	MachineID              string
	CertificateSerial      string
	ReportID               string
	BaseRevision           int64
	RequestDigest          [sha256.Size]byte
	Recognized             []RecognizedObservation
	UnsupportedAdapterKeys []agent.AdapterKey
}

// AgentPresencePersistence is the Machine consumer's narrow reconciliation capability.
type AgentPresencePersistence interface {
	ReconcileAgentPresence(context.Context, AgentReportCommand) (AgentReportResult, error)
}

// AgentPresence validates a complete report against the immutable recognition vocabulary.
type AgentPresence struct {
	persistence AgentPresencePersistence
	vocabulary  agent.Vocabulary
}

// NewAgentPresence constructs the Machine report behavior.
func NewAgentPresence(persistence AgentPresencePersistence, vocabulary agent.Vocabulary) (*AgentPresence, error) {
	if persistence == nil {
		return nil, errors.New("Agent presence persistence is required")
	}
	if vocabulary.Empty() {
		return nil, errors.New("Agent recognition vocabulary is required")
	}
	return &AgentPresence{
		persistence: persistence,
		vocabulary:  vocabulary,
	}, nil
}

// Report canonicalizes and validates one complete Machine observation before persistence.
func (presence *AgentPresence) Report(ctx context.Context, request AgentReportRequest) (AgentReportResult, error) {
	machineID, machineErr := canonicalUUID(request.MachineID)
	reportID, reportErr := canonicalUUID(request.ReportID)
	if machineErr != nil || reportErr != nil ||
		request.CertificateSerial == "" || strings.TrimSpace(request.CertificateSerial) != request.CertificateSerial ||
		len(request.CertificateSerial) > maxCertificateSerialBytes || request.BaseRevision < 0 ||
		len(request.Observations) > agent.MaxObservationsPerReport {
		return AgentReportResult{}, ErrInvalidAgentReport
	}

	observations := append([]AgentObservation(nil), request.Observations...)
	sort.Slice(observations, func(left, right int) bool {
		if observations[left].AdapterKey != observations[right].AdapterKey {
			return observations[left].AdapterKey < observations[right].AdapterKey
		}
		return observations[left].OccurrenceKey < observations[right].OccurrenceKey
	})

	recognizedCounts := make(map[agent.AdapterKey]int)
	recognized := make([]RecognizedObservation, 0, len(observations))
	unsupportedSet := make(map[agent.AdapterKey]struct{})
	for index, observation := range observations {
		if !agent.ValidAdapterKey(observation.AdapterKey) || !agent.ValidOccurrenceKey(observation.OccurrenceKey) {
			return AgentReportResult{}, ErrInvalidAgentReport
		}
		if index > 0 && observation.AdapterKey == observations[index-1].AdapterKey &&
			observation.OccurrenceKey == observations[index-1].OccurrenceKey {
			return AgentReportResult{}, ErrInvalidAgentReport
		}
		descriptor, known := presence.vocabulary.Descriptor(observation.AdapterKey)
		if !known {
			unsupportedSet[observation.AdapterKey] = struct{}{}
			continue
		}
		recognizedCounts[observation.AdapterKey]++
		if recognizedCounts[observation.AdapterKey] > descriptor.MaxOccurrencesPerReport {
			return AgentReportResult{}, ErrInvalidAgentReport
		}
		recognized = append(recognized, RecognizedObservation{
			AdapterKey:    observation.AdapterKey,
			OccurrenceKey: observation.OccurrenceKey,
			Present:       observation.Present,
			NameBase:      descriptor.NameBase,
		})
	}

	unsupported := make([]agent.AdapterKey, 0, len(unsupportedSet))
	for key := range unsupportedSet {
		unsupported = append(unsupported, key)
	}
	sort.Slice(unsupported, func(left, right int) bool { return unsupported[left] < unsupported[right] })

	command := AgentReportCommand{
		MachineID:              machineID,
		CertificateSerial:      request.CertificateSerial,
		ReportID:               reportID,
		BaseRevision:           request.BaseRevision,
		RequestDigest:          agentReportDigest(reportID, request.BaseRevision, observations),
		Recognized:             recognized,
		UnsupportedAdapterKeys: unsupported,
	}
	return presence.persistence.ReconcileAgentPresence(ctx, command)
}

func canonicalUUID(value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func agentReportDigest(reportID string, baseRevision int64, observations []AgentObservation) [sha256.Size]byte {
	var encoded []byte
	encoded = appendDigestPart(encoded, []byte("carry/machine-agent-report/v1"))
	encoded = appendDigestPart(encoded, []byte(reportID))
	var revision [8]byte
	binary.BigEndian.PutUint64(revision[:], uint64(baseRevision))
	encoded = appendDigestPart(encoded, revision[:])
	for _, observation := range observations {
		encoded = appendDigestPart(encoded, []byte(observation.AdapterKey))
		encoded = appendDigestPart(encoded, []byte(observation.OccurrenceKey))
		present := byte(0)
		if observation.Present {
			present = 1
		}
		encoded = appendDigestPart(encoded, []byte{present})
	}
	return sha256.Sum256(encoded)
}

func appendDigestPart(encoded []byte, part []byte) []byte {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(part)))
	encoded = append(encoded, length[:]...)
	return append(encoded, part...)
}
