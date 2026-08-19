package host

import (
	"errors"
	"time"
)

var (
	ErrMachineNotFound      = errors.New("machine not found")
	ErrMachineRevoked       = errors.New("machine is revoked")
	ErrIdempotencyConflict  = errors.New("idempotency key refers to different enrollment")
	ErrInvalidRuntimeReport = errors.New("runtime report must contain every supported Runtime exactly once")
)

type EnrollMachineCommand struct {
	MachineID         string
	SpaceID           string
	DisplayName       string
	PublicKeyDER      []byte
	CertificatePEM    []byte
	CertificateSerial string
	EnrolledByUserID  string
	IdempotencyKey    string
}

type MachineEnrollment struct {
	MachineID      string
	SpaceID        string
	CertificatePEM []byte
}

type MachineStatus struct {
	MachineID   string
	SpaceID     string
	DisplayName string
	EnrolledAt  time.Time
	RevokedAt   *time.Time
	Runtimes    []RuntimeObservation
}

func ValidateRuntimeReport(observations []RuntimeObservation) error {
	seen := make(map[RuntimeKind]bool, len(supportedRuntimes))
	for _, observation := range observations {
		if !isSupportedRuntime(observation.Kind) || seen[observation.Kind] {
			return ErrInvalidRuntimeReport
		}
		seen[observation.Kind] = true
	}
	if len(seen) != len(supportedRuntimes) {
		return ErrInvalidRuntimeReport
	}
	return nil
}

func isSupportedRuntime(kind RuntimeKind) bool {
	for _, definition := range supportedRuntimes {
		if definition.kind == kind {
			return true
		}
	}
	return false
}
