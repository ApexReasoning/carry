//go:build integration

package postgres

import (
	"context"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/google/uuid"
)

type testMachineCommand struct {
	MachineID, SpaceID, DisplayName, EnrolledByUserID string
	PublicKeyDER, CertificatePEM                      []byte
	CertificateSerial                                 string
}

func enrollMachineForTest(ctx context.Context, store *Store, command testMachineCommand) (machine.MachineRecord, error) {
	if command.MachineID == "" {
		command.MachineID = uuid.NewString()
	}
	if command.CertificateSerial == "" {
		command.CertificateSerial = uuid.NewString()
	}
	var enrolledAt time.Time
	err := store.pool.QueryRow(ctx, `
		insert into machines (
			machine_id, space_id, display_name, public_key_der, certificate_pem,
			certificate_serial, enrolled_by_user_id
		) values ($1,$2,$3,$4,$5,$6,$7)
		returning enrolled_at
	`, command.MachineID, command.SpaceID, command.DisplayName, command.PublicKeyDER,
		command.CertificatePEM, command.CertificateSerial, command.EnrolledByUserID).Scan(&enrolledAt)
	return machine.MachineRecord{
		MachineID: command.MachineID, SpaceID: command.SpaceID, DisplayName: command.DisplayName,
		Fingerprint: machine.PublicKeyFingerprint(command.PublicKeyDER), State: "Active",
		EnrolledByUserID: command.EnrolledByUserID, EnrolledAt: enrolledAt,
	}, err
}

func revokeMachineForTest(ctx context.Context, store *Store, machineID string) error {
	_, err := store.pool.Exec(ctx, `
		update machines
		set revoked_at=transaction_timestamp(), revocation_actor_kind='not_recorded'
		where machine_id=$1 and revoked_at is null
	`, machineID)
	return err
}
