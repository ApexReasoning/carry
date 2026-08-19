package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) EnrollMachine(ctx context.Context, command host.EnrollMachineCommand) (host.MachineEnrollment, error) {
	if strings.TrimSpace(command.MachineID) == "" || strings.TrimSpace(command.SpaceID) == "" ||
		strings.TrimSpace(command.DisplayName) == "" || len(command.PublicKeyDER) == 0 ||
		len(command.CertificatePEM) == 0 || strings.TrimSpace(command.CertificateSerial) == "" ||
		strings.TrimSpace(command.EnrolledByUserID) == "" || strings.TrimSpace(command.IdempotencyKey) == "" {
		return host.MachineEnrollment{}, errors.New("complete Machine enrollment is required")
	}

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return host.MachineEnrollment{}, fmt.Errorf("begin Machine enrollment: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	queries := s.queries.WithTx(transaction)

	if err := requireMachineEnrollmentPermission(ctx, queries, command.SpaceID, command.EnrolledByUserID); err != nil {
		return host.MachineEnrollment{}, err
	}
	normalizedDisplayName := strings.TrimSpace(command.DisplayName)
	created, err := queries.CreateMachine(ctx, dbsqlc.CreateMachineParams{
		MachineID: command.MachineID, SpaceID: command.SpaceID,
		DisplayName: normalizedDisplayName, PublicKeyDer: command.PublicKeyDER,
		CertificatePem: command.CertificatePEM, CertificateSerial: command.CertificateSerial,
		EnrolledByUserID: command.EnrolledByUserID, EnrollmentIdempotencyKey: command.IdempotencyKey,
	})
	var enrollment host.MachineEnrollment
	if err == nil {
		enrollment = host.MachineEnrollment{
			MachineID: created.MachineID, SpaceID: created.SpaceID, CertificatePEM: created.CertificatePem,
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := loadEnrollmentByIdempotency(
			ctx, queries, command.SpaceID, command.EnrolledByUserID, command.IdempotencyKey,
		)
		if loadErr != nil {
			return host.MachineEnrollment{}, loadErr
		}
		if existing.displayName != normalizedDisplayName || !bytes.Equal(existing.publicKeyDER, command.PublicKeyDER) {
			return host.MachineEnrollment{}, host.ErrIdempotencyConflict
		}
		enrollment = existing.enrollment
	} else {
		return host.MachineEnrollment{}, fmt.Errorf("insert Machine enrollment: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return host.MachineEnrollment{}, fmt.Errorf("commit Machine enrollment: %w", err)
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
	enrollment   host.MachineEnrollment
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
		enrollment: host.MachineEnrollment{
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
		return host.ErrMachineNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Machine revocation: %w", err)
	}
	return nil
}

func (s *Store) ReplaceRuntimeObservations(ctx context.Context, machineID string, observations []host.RuntimeObservation) error {
	if err := host.ValidateRuntimeReport(observations); err != nil {
		return err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Runtime report: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	queries := s.queries.WithTx(transaction)

	// mTLS proves possession of an enrolled certificate; the current database
	// row remains authoritative for revocation on every Host write.
	machine, err := queries.LoadMachineStatus(ctx, machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return host.ErrMachineNotFound
	}
	if err != nil {
		return fmt.Errorf("load Machine for Runtime report: %w", err)
	}
	if machine.RevokedAt.Valid {
		return host.ErrMachineRevoked
	}
	if err := queries.DeleteRuntimeObservations(ctx, machineID); err != nil {
		return fmt.Errorf("replace Runtime observations: %w", err)
	}
	for _, observation := range observations {
		if err := queries.CreateRuntimeObservation(ctx, dbsqlc.CreateRuntimeObservationParams{
			MachineID: machineID, RuntimeKind: string(observation.Kind), Detection: string(observation.Detection),
			Executable: nullableString(observation.Executable), Version: nullableString(observation.Version),
			DiagnosticCode:   nullableString(observation.DiagnosticCode),
			DiagnosticDetail: nullableString(observation.DiagnosticDetail),
			ObservedAt:       pgtype.Timestamptz{Time: observation.ObservedAt.UTC(), Valid: true},
		}); err != nil {
			return fmt.Errorf("insert %s Runtime observation: %w", observation.Kind, err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Runtime report: %w", err)
	}
	return nil
}

func (s *Store) LoadMachineStatus(ctx context.Context, machineID string) (host.MachineStatus, error) {
	row, err := s.queries.LoadMachineStatus(ctx, machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return host.MachineStatus{}, host.ErrMachineNotFound
	}
	if err != nil {
		return host.MachineStatus{}, fmt.Errorf("load Machine: %w", err)
	}
	status := host.MachineStatus{
		MachineID: row.MachineID, SpaceID: row.SpaceID, DisplayName: row.DisplayName,
		EnrolledAt: row.EnrolledAt.Time,
	}
	if row.RevokedAt.Valid {
		revokedAt := row.RevokedAt.Time
		status.RevokedAt = &revokedAt
	}

	runtimeRows, err := s.queries.ListRuntimeObservations(ctx, machineID)
	if err != nil {
		return host.MachineStatus{}, fmt.Errorf("load Runtime observations: %w", err)
	}
	status.Runtimes = make([]host.RuntimeObservation, 0, len(runtimeRows))
	for _, runtimeRow := range runtimeRows {
		status.Runtimes = append(status.Runtimes, host.RuntimeObservation{
			Kind:       host.RuntimeKind(runtimeRow.RuntimeKind),
			Detection:  host.RuntimeDetection(runtimeRow.Detection),
			Executable: valueOrEmpty(runtimeRow.Executable), Version: valueOrEmpty(runtimeRow.Version),
			DiagnosticCode:   valueOrEmpty(runtimeRow.DiagnosticCode),
			DiagnosticDetail: valueOrEmpty(runtimeRow.DiagnosticDetail),
			ObservedAt:       runtimeRow.ObservedAt.Time,
		})
	}
	return status, nil
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
