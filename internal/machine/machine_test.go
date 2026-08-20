package machine

import (
	"errors"
	"testing"
)

func TestValidateEnrollmentRejectsInvalidTextAndIdentity(t *testing.T) {
	t.Parallel()
	valid := EnrollMachineCommand{
		MachineID: "38a0e783-2f61-4de4-a264-91fe1c099893",
		SpaceID:   "a30f0a9a-8cb2-4ae4-9a7e-ae85e207788a", DisplayName: " Desk Host ",
		PublicKeyDER: []byte("public"), CertificatePEM: []byte("certificate"), CertificateSerial: "17",
		EnrolledByUserID: "76fa247e-e9ef-4036-ac5d-87463cabb2ff", IdempotencyKey: "enroll-desk-host",
	}
	name, err := ValidateEnrollment(valid)
	if err != nil || name != "Desk Host" {
		t.Fatalf("valid enrollment = %q, %v", name, err)
	}
	for name, mutate := range map[string]func(*EnrollMachineCommand){
		"invalid Machine":      func(command *EnrollMachineCommand) { command.MachineID = "not-a-uuid" },
		"nul display":          func(command *EnrollMachineCommand) { command.DisplayName = "Desk\x00Host" },
		"invalid utf8":         func(command *EnrollMachineCommand) { command.DisplayName = string([]byte{0xff}) },
		"oversize idempotency": func(command *EnrollMachineCommand) { command.IdempotencyKey = string(make([]byte, 256)) },
	} {
		t.Run(name, func(t *testing.T) {
			command := valid
			mutate(&command)
			if _, err := ValidateEnrollment(command); !errors.Is(err, ErrInvalidEnrollment) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}
