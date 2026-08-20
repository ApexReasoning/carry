package machine

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrMachineNotFound     = errors.New("machine not found")
	ErrMachineRevoked      = errors.New("machine is revoked")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different enrollment")
	ErrInvalidEnrollment   = errors.New("complete valid Machine enrollment is required")
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

func ValidateEnrollment(command EnrollMachineCommand) (string, error) {
	displayName := strings.TrimSpace(command.DisplayName)
	if uuid.Validate(command.MachineID) != nil || uuid.Validate(command.SpaceID) != nil ||
		uuid.Validate(command.EnrolledByUserID) != nil || displayName == "" ||
		!validText(displayName) || len(command.PublicKeyDER) == 0 || len(command.CertificatePEM) == 0 ||
		strings.TrimSpace(command.CertificateSerial) == "" || !validText(command.CertificateSerial) ||
		strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 ||
		!validText(command.IdempotencyKey) {
		return "", ErrInvalidEnrollment
	}
	return displayName, nil
}

func validText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
