package machine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrMachineNotFound     = errors.New("machine not found")
	ErrMachineRevoked      = errors.New("machine is revoked")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different enrollment")
	ErrInvalidEnrollment   = errors.New("complete valid Machine enrollment is required")
)

// EnrollmentPersistence is the atomic Machine enrollment mutation consumed by Machine.
type EnrollmentPersistence interface {
	EnrollMachine(context.Context, EnrollMachineCommand) (MachineEnrollment, error)
}

type Enrollment struct {
	persistence EnrollmentPersistence
	authority   *CertificateAuthority
}

func NewEnrollment(persistence EnrollmentPersistence, authority *CertificateAuthority) (*Enrollment, error) {
	if persistence == nil || authority == nil {
		return nil, errors.New("Machine enrollment dependencies are required")
	}
	return &Enrollment{persistence: persistence, authority: authority}, nil
}

type EnrollmentRequest struct {
	SpaceID          string
	DisplayName      string
	PublicKeyDER     []byte
	EnrolledByUserID string
	IdempotencyKey   string
}

func (enrollment *Enrollment) Enroll(ctx context.Context, request EnrollmentRequest) (MachineEnrollment, error) {
	machineID := uuid.NewString()
	issued, err := enrollment.authority.IssueMachineCertificate(machineID, request.PublicKeyDER, time.Now().UTC())
	if err != nil {
		return MachineEnrollment{}, ErrInvalidEnrollment
	}
	return enrollment.persistence.EnrollMachine(ctx, EnrollMachineCommand{
		MachineID: machineID, SpaceID: request.SpaceID, DisplayName: request.DisplayName,
		PublicKeyDER: request.PublicKeyDER, CertificatePEM: issued.CertificatePEM,
		CertificateSerial: issued.Serial, EnrolledByUserID: request.EnrolledByUserID,
		IdempotencyKey: request.IdempotencyKey,
	})
}

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
