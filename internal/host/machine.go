package host

import (
	"errors"
)

var (
	ErrMachineNotFound     = errors.New("machine not found")
	ErrMachineRevoked      = errors.New("machine is revoked")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different enrollment")
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
