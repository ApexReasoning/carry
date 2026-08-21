//go:build integration

package postgres

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMachineEnrollmentRequiresPermissionAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Dorothy",
		SpaceName:   "Compiler Research",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	command := machine.EnrollMachineCommand{
		MachineID: uuid.NewString(), SpaceID: bootstrap.SpaceID, DisplayName: "lab-mac",
		PublicKeyDER: []byte("public-key"), CertificatePEM: []byte("certificate"),
		CertificateSerial: uuid.NewString(), EnrolledByUserID: bootstrap.UserID,
		IdempotencyKey: "enroll-lab-mac",
	}
	first, err := store.EnrollMachine(ctx, command)
	if err != nil {
		t.Fatalf("enroll Machine: %v", err)
	}
	second, err := store.EnrollMachine(ctx, command)
	if err != nil {
		t.Fatalf("repeat enrollment: %v", err)
	}
	if second.MachineID != first.MachineID || !bytes.Equal(second.CertificatePEM, first.CertificatePEM) {
		t.Fatalf("repeat enrollment = %#v, want %#v", second, first)
	}

	conflict := command
	conflict.MachineID = uuid.NewString()
	conflict.PublicKeyDER = []byte("different-public-key")
	conflict.CertificatePEM = []byte("different-certificate")
	conflict.CertificateSerial = uuid.NewString()
	if _, err := store.EnrollMachine(ctx, conflict); !errors.Is(err, machine.ErrIdempotencyConflict) {
		t.Fatalf("conflicting enrollment error = %v", err)
	}
	displayNameConflict := command
	displayNameConflict.MachineID = uuid.NewString()
	displayNameConflict.DisplayName = "another-lab-mac"
	displayNameConflict.CertificatePEM = []byte("another-certificate")
	displayNameConflict.CertificateSerial = uuid.NewString()
	if _, err := store.EnrollMachine(ctx, displayNameConflict); !errors.Is(err, machine.ErrIdempotencyConflict) {
		t.Fatalf("changed display name enrollment error = %v", err)
	}

	unauthorizedUserID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, 'Observer')`, unauthorizedUserID); err != nil {
		t.Fatalf("create unauthorized user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_enroll_machines)
		values ($1, $2, false)
	`, bootstrap.SpaceID, unauthorizedUserID); err != nil {
		t.Fatalf("create unauthorized membership: %v", err)
	}
	unauthorized := command
	unauthorized.MachineID = uuid.NewString()
	unauthorized.EnrolledByUserID = unauthorizedUserID
	unauthorized.IdempotencyKey = "unauthorized"
	unauthorized.CertificateSerial = uuid.NewString()
	if _, err := store.EnrollMachine(ctx, unauthorized); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("unauthorized enrollment error = %v", err)
	}
}

func TestConcurrentMachineEnrollmentReturnsTheDurableWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Margaret", SpaceName: "Materials Lab",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	const callers = 8
	results := make(chan machine.MachineEnrollment, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			enrollment, enrollErr := store.EnrollMachine(ctx, machine.EnrollMachineCommand{
				MachineID: uuid.NewString(), SpaceID: bootstrap.SpaceID, DisplayName: "materials-host",
				PublicKeyDER: []byte("same-public-key"), CertificatePEM: []byte(uuid.NewString()),
				CertificateSerial: uuid.NewString(), EnrolledByUserID: bootstrap.UserID,
				IdempotencyKey: "concurrent-materials-host",
			})
			if enrollErr != nil {
				errorsFound <- enrollErr
				return
			}
			results <- enrollment
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for enrollErr := range errorsFound {
		t.Fatalf("concurrent enrollment: %v", enrollErr)
	}

	var winner machine.MachineEnrollment
	for enrollment := range results {
		if winner.MachineID == "" {
			winner = enrollment
		}
		if enrollment.MachineID != winner.MachineID ||
			!bytes.Equal(enrollment.CertificatePEM, winner.CertificatePEM) {
			t.Fatalf("concurrent enrollment = %#v, want winner %#v", enrollment, winner)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from machines where space_id = $1`, bootstrap.SpaceID).Scan(&count); err != nil {
		t.Fatalf("count Machines: %v", err)
	}
	if count != 1 {
		t.Fatalf("Machine count = %d, want 1", count)
	}
}

func TestMachineEnrollmentPermissionLockSerializesRevocation(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Katherine", SpaceName: "Flight Controls",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	enrollmentTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin enrollment: %v", err)
	}
	defer func() { _ = enrollmentTransaction.Rollback(context.Background()) }()
	queries := store.queries.WithTx(enrollmentTransaction)
	allowed, err := queries.GetMachineEnrollmentPermission(ctx, dbsqlc.GetMachineEnrollmentPermissionParams{
		SpaceID: bootstrap.SpaceID,
		UserID:  bootstrap.UserID,
	})
	if err != nil || !allowed {
		t.Fatalf("lock enrollment permission: allowed=%t error=%v", allowed, err)
	}

	revocationTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin competing revocation: %v", err)
	}
	if _, err := revocationTransaction.Exec(ctx, `set local lock_timeout = '100ms'`); err != nil {
		_ = revocationTransaction.Rollback(ctx)
		t.Fatalf("set revocation lock timeout: %v", err)
	}
	_, revocationErr := revocationTransaction.Exec(ctx, `
		update space_memberships
		set revoked_at = transaction_timestamp(), version = version + 1
		where space_id = $1 and user_id = $2
	`, bootstrap.SpaceID, bootstrap.UserID)
	var postgresError *pgconn.PgError
	if !errors.As(revocationErr, &postgresError) || postgresError.Code != "55P03" {
		_ = revocationTransaction.Rollback(ctx)
		t.Fatalf("competing revocation error = %v, want lock_not_available (55P03)", revocationErr)
	}
	if err := revocationTransaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback blocked revocation: %v", err)
	}

	machineID := uuid.NewString()
	if _, err := queries.CreateMachine(ctx, dbsqlc.CreateMachineParams{
		MachineID: machineID, SpaceID: bootstrap.SpaceID, DisplayName: "flight-controls-host",
		PublicKeyDer: []byte("public-key"), CertificatePem: []byte("certificate"),
		CertificateSerial: uuid.NewString(), EnrolledByUserID: bootstrap.UserID,
		EnrollmentIdempotencyKey: "flight-controls-host",
	}); err != nil {
		t.Fatalf("insert Machine while permission locked: %v", err)
	}
	if err := enrollmentTransaction.Commit(ctx); err != nil {
		t.Fatalf("commit enrollment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update space_memberships
		set revoked_at = transaction_timestamp(), version = version + 1
		where space_id = $1 and user_id = $2
	`, bootstrap.SpaceID, bootstrap.UserID); err != nil {
		t.Fatalf("revoke membership after enrollment committed: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `select count(*) from machines where machine_id = $1`, machineID).Scan(&count); err != nil {
		t.Fatalf("count enrolled Machine: %v", err)
	}
	if count != 1 {
		t.Fatalf("enrolled Machine count = %d, want 1", count)
	}
}
