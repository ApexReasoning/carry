//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/work"
)

func TestMigrateCreatesCurrentFactsAndRejectsUnearnedWorkLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}

	for _, table := range []string{
		"carry_users",
		"spaces",
		"space_memberships",
		"user_tokens",
		"machines",
		"works",
		"work_messages",
		"runs",
		"run_attempts",
		"browser_sessions",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `select to_regclass('public.' || $1) is not null`, table).Scan(&exists); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}

	for _, removed := range []string{"machine_runtime_observations", "coordinator_runs", "work_understanding_revisions"} {
		var exists bool
		if err := pool.QueryRow(ctx, `select to_regclass('public.' || $1) is not null`, removed).Scan(&exists); err != nil {
			t.Fatalf("find removed table %s: %v", removed, err)
		}
		if exists {
			t.Errorf("removed table %s still exists", removed)
		}
	}

	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Migration Owner", SpaceName: "Migration Space",
		TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap lifecycle fixture: %v", err)
	}
	created, err := store.CreateWork(ctx, work.CreateCommand{
		SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
		Goal: "Keep Work lifecycle limited to earned states", IdempotencyKey: "migration-lifecycle",
	})
	if err != nil {
		t.Fatalf("create lifecycle fixture: %v", err)
	}

	if _, err := pool.Exec(ctx, `update works set lifecycle = 'paused' where work_id = $1`, created.WorkID); err == nil {
		t.Fatal("unimplemented paused lifecycle was accepted")
	}
	details, err := store.LoadWork(ctx, bootstrap.UserID, bootstrap.SpaceID, created.WorkID)
	if err != nil {
		t.Fatalf("reload lifecycle fixture: %v", err)
	}
	if details.Work.Lifecycle != work.LifecycleOpen {
		t.Fatalf("lifecycle = %q, want %q", details.Work.Lifecycle, work.LifecycleOpen)
	}
}

func TestMigrateUpgradesTerminalNode2Runs(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatalf("reset schema for upgrade: %v", err)
	}

	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		create table carry_schema_migrations (
			version text primary key,
			applied_at timestamptz not null default transaction_timestamp()
		)
	`); err != nil {
		connection.Release()
		t.Fatalf("create migration ledger: %v", err)
	}
	for _, filename := range []string{
		"0001_node0_foundation.sql",
		"0002_first_durable_work.sql",
		"0003_work_open_lifecycle_only.sql",
		"0004_native_execution_authority.sql",
	} {
		migration, readErr := migrationFiles.ReadFile("migrations/" + filename)
		if readErr != nil {
			connection.Release()
			t.Fatalf("read migration %s: %v", filename, readErr)
		}
		if applyErr := applyMigration(ctx, connection, filename, string(migration)); applyErr != nil {
			connection.Release()
			t.Fatalf("apply migration %s: %v", filename, applyErr)
		}
	}
	connection.Release()

	if _, err := pool.Exec(ctx, `
		insert into carry_users (user_id, display_name)
		values ('10000000-0000-0000-0000-000000000001', 'Upgrade Owner');
		insert into spaces (space_id, name)
		values ('20000000-0000-0000-0000-000000000001', 'Upgrade Space');
		insert into space_memberships (space_id, user_id, can_enroll_machines)
		values (
			'20000000-0000-0000-0000-000000000001',
			'10000000-0000-0000-0000-000000000001',
			true
		);
		insert into works (
			work_id, space_id, goal, owner_user_id, creator_user_id,
			create_idempotency_key, create_request_digest
		) values
			(
				'30000000-0000-0000-0000-000000000001',
				'20000000-0000-0000-0000-000000000001',
				'Preserve failed Run',
				'10000000-0000-0000-0000-000000000001',
				'10000000-0000-0000-0000-000000000001',
				'upgrade-failed', decode(repeat('01', 32), 'hex')
			),
			(
				'30000000-0000-0000-0000-000000000002',
				'20000000-0000-0000-0000-000000000001',
				'Preserve unknown Run',
				'10000000-0000-0000-0000-000000000001',
				'10000000-0000-0000-0000-000000000001',
				'upgrade-unknown', decode(repeat('02', 32), 'hex')
			);
		insert into coordinator_runs (
			run_id, work_id, input_start_seq, input_end_seq,
			base_revision, writer_token, state, current_fence
		) values
			(
				'40000000-0000-0000-0000-000000000001',
				'30000000-0000-0000-0000-000000000001',
				1, 1, 0, '50000000-0000-0000-0000-000000000001', 'failed', 1
			),
			(
				'40000000-0000-0000-0000-000000000002',
				'30000000-0000-0000-0000-000000000002',
				1, 1, 0, '50000000-0000-0000-0000-000000000002', 'unknown', 1
			);
	`); err != nil {
		t.Fatalf("seed Node 2 terminal Runs: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade Node 2 schema: %v", err)
	}
	var migrated int
	if err := pool.QueryRow(ctx, `
		select count(*)
		from runs
		where state in ('failed', 'unknown') and completed_at is not null
	`).Scan(&migrated); err != nil {
		t.Fatalf("count upgraded terminal Runs: %v", err)
	}
	if migrated != 2 {
		t.Fatalf("upgraded terminal Runs = %d, want 2", migrated)
	}
}
