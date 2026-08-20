package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/jackc/pgx/v5"
)

func (s *Store) EnrollMachine(ctx context.Context, command machine.EnrollMachineCommand) (machine.MachineEnrollment, error) {
	normalizedDisplayName, err := machine.ValidateEnrollment(command)
	if err != nil {
		return machine.MachineEnrollment{}, err
	}

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.MachineEnrollment{}, fmt.Errorf("begin Machine enrollment: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	queries := s.queries.WithTx(transaction)

	if err := requireMachineEnrollmentPermission(ctx, queries, command.SpaceID, command.EnrolledByUserID); err != nil {
		return machine.MachineEnrollment{}, err
	}
	created, err := queries.CreateMachine(ctx, dbsqlc.CreateMachineParams{
		MachineID: command.MachineID, SpaceID: command.SpaceID,
		DisplayName: normalizedDisplayName, PublicKeyDer: command.PublicKeyDER,
		CertificatePem: command.CertificatePEM, CertificateSerial: command.CertificateSerial,
		EnrolledByUserID: command.EnrolledByUserID, EnrollmentIdempotencyKey: command.IdempotencyKey,
	})
	var enrollment machine.MachineEnrollment
	if err == nil {
		enrollment = machine.MachineEnrollment{
			MachineID: created.MachineID, SpaceID: created.SpaceID, CertificatePEM: created.CertificatePem,
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := loadEnrollmentByIdempotency(
			ctx, queries, command.SpaceID, command.EnrolledByUserID, command.IdempotencyKey,
		)
		if loadErr != nil {
			return machine.MachineEnrollment{}, loadErr
		}
		if existing.displayName != normalizedDisplayName || !bytes.Equal(existing.publicKeyDER, command.PublicKeyDER) {
			return machine.MachineEnrollment{}, machine.ErrIdempotencyConflict
		}
		enrollment = existing.enrollment
	} else {
		return machine.MachineEnrollment{}, fmt.Errorf("insert Machine enrollment: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return machine.MachineEnrollment{}, fmt.Errorf("commit Machine enrollment: %w", err)
	}
	return enrollment, nil
}

func requireMachineEnrollmentPermission(
	ctx context.Context,
	queries *dbsqlc.Queries,
	spaceID string,
	userID string,
) error {
	allowed, err := queries.GetMachineEnrollmentPermission(ctx, dbsqlc.GetMachineEnrollmentPermissionParams{
		SpaceID: spaceID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) || !allowed {
		return space.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("load Machine enrollment permission: %w", err)
	}
	return nil
}

type storedMachineEnrollment struct {
	enrollment   machine.MachineEnrollment
	displayName  string
	publicKeyDER []byte
}

func loadEnrollmentByIdempotency(
	ctx context.Context,
	queries *dbsqlc.Queries,
	spaceID string,
	userID string,
	idempotencyKey string,
) (storedMachineEnrollment, error) {
	row, err := queries.FindEnrollmentByIdempotency(ctx, dbsqlc.FindEnrollmentByIdempotencyParams{
		SpaceID: spaceID, EnrolledByUserID: userID, EnrollmentIdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return storedMachineEnrollment{}, fmt.Errorf("load idempotent Machine enrollment: %w", err)
	}
	return storedMachineEnrollment{
		enrollment: machine.MachineEnrollment{
			MachineID: row.MachineID, SpaceID: row.SpaceID, CertificatePEM: row.CertificatePem,
		},
		displayName:  row.DisplayName,
		publicKeyDER: row.PublicKeyDer,
	}, nil
}

func (s *Store) RevokeMachine(ctx context.Context, userID string, spaceID string, machineID string) error {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Machine revocation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	queries := s.queries.WithTx(transaction)
	if err := requireMachineEnrollmentPermission(ctx, queries, spaceID, userID); err != nil {
		return err
	}

	rowsAffected, err := queries.RevokeMachine(ctx, dbsqlc.RevokeMachineParams{
		MachineID: machineID, SpaceID: spaceID,
	})
	if err != nil {
		return fmt.Errorf("revoke Machine: %w", err)
	}
	if rowsAffected == 0 {
		return machine.ErrMachineNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Machine revocation: %w", err)
	}
	return nil
}
