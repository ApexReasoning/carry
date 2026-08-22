//go:build integration

package postgres

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
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
		"cli_login_requests",
		"cli_login_lookup_failures",
		"cli_credentials",
		"machines",
		"conversations",
		"conversation_messages",
		"conversation_reply_claims",
		"works",
		"work_messages",
		"runs",
		"run_attempts",
		"work_result_checks",
		"browser_sessions",
		"email_identities",
		"email_login_challenges",
		"email_login_attempts",
		"google_identities",
		"github_identities",
		"external_login_transactions",
		"space_invitations",
		"space_invitation_submissions",
		"agents",
		"agent_presence",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `select to_regclass('public.' || $1) is not null`, table).Scan(&exists); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}

	for _, removed := range []string{"user_tokens", "machine_runtime_observations", "coordinator_runs", "work_understanding_revisions"} {
		var exists bool
		if err := pool.QueryRow(ctx, `select to_regclass('public.' || $1) is not null`, removed).Scan(&exists); err != nil {
			t.Fatalf("find removed table %s: %v", removed, err)
		}
		if exists {
			t.Errorf("removed table %s still exists", removed)
		}
	}

	store := NewStore(pool)
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Migration Owner", SpaceName: "Migration Space",
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
	details, err := store.LoadWork(ctx, work.LoadCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID})
	if err != nil {
		t.Fatalf("reload lifecycle fixture: %v", err)
	}
	if details.Work.Lifecycle != work.LifecycleOpen {
		t.Fatalf("lifecycle = %q, want %q", details.Work.Lifecycle, work.LifecycleOpen)
	}
}

func TestMigration19PreservesMachinesAndAddsGenericAgentFacts(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `create table carry_schema_migrations(version text primary key, applied_at timestamptz not null default transaction_timestamp())`); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0019_durable_agent_identity.sql" {
			continue
		}
		migration, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			connection.Release()
			t.Fatal(readErr)
		}
		if err := applyMigration(ctx, connection, entry.Name(), string(migration)); err != nil {
			connection.Release()
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
	connection.Release()
	const userID = "10000000-0000-4000-8000-000000000019"
	const spaceID = "20000000-0000-4000-8000-000000000019"
	const machineID = "30000000-0000-4000-8000-000000000019"
	if _, err := pool.Exec(ctx, `insert into carry_users(user_id,display_name) values($1,'Agent Upgrade Owner')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into spaces(space_id,name,slug) values($1,'Agent Upgrade Space','agent-upgrade-space')`, spaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into space_memberships(space_id,user_id,can_manage_members,can_enroll_machines) values($1,$2,true,true)`, spaceID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into machines(machine_id,space_id,display_name,public_key_der,certificate_pem,certificate_serial,enrolled_by_user_id)
		values($1,$2,'Preserved Host',decode('01','hex'),decode('02','hex'),'upgrade-agent-serial',$3)
	`, machineID, spaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var revision int64
	var reportedAt, reportID, digest *string
	var unsupported, setup []string
	if err := pool.QueryRow(ctx, `
		select agent_report_revision,agent_reported_at::text,last_agent_report_id::text,
			encode(last_agent_report_digest,'hex'),last_agent_report_unsupported_keys,last_agent_report_setup_required_keys
		from machines where machine_id=$1
	`, machineID).Scan(&revision, &reportedAt, &reportID, &digest, &unsupported, &setup); err != nil {
		t.Fatal(err)
	}
	if revision != 0 || reportedAt != nil || reportID != nil || digest != nil || len(unsupported) != 0 || len(setup) != 0 {
		t.Fatalf("upgraded Machine report shape = %d %v %v %v %#v %#v", revision, reportedAt, reportID, digest, unsupported, setup)
	}
	for _, table := range []string{"agents", "agent_presence"} {
		var exists bool
		if err := pool.QueryRow(ctx, `select to_regclass('public.' || $1) is not null`, table).Scan(&exists); err != nil || !exists {
			t.Fatalf("migration 19 table %s exists=%t err=%v", table, exists, err)
		}
	}
}

func TestConversationReplySchemaRejectsInvalidSourceAndReplyShapes(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := createMemberForTest(ctx, store, testMemberCommand{
		DisplayName: "Reply Constraint Owner", SpaceName: "Reply Constraint Space",
	})
	if err != nil {
		t.Fatalf("bootstrap constraint fixture: %v", err)
	}
	source, err := store.SendConversationMessage(ctx, conversation.SendCommand{
		SpaceID: bootstrap.SpaceID, MemberUserID: bootstrap.UserID,
		Text: "Private source", IdempotencyKey: "reply-constraint-source",
	})
	if err != nil {
		t.Fatalf("send constraint source: %v", err)
	}
	var conversationID string
	if err := pool.QueryRow(ctx, `select conversation_id from conversations where space_id = $1 and member_user_id = $2`, bootstrap.SpaceID, bootstrap.UserID).Scan(&conversationID); err != nil {
		t.Fatalf("load constraint Conversation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into conversation_messages (message_id, conversation_id, message_seq, author, text)
		values ($1, $2, 2, 'carry', 'Missing source relation')
	`, uuid.NewString(), conversationID); err == nil {
		t.Fatal("Carry reply without a source member message was accepted")
	}
	if _, err := pool.Exec(ctx, `
		insert into conversation_messages (
			message_id, conversation_id, message_seq, author, author_user_id,
			text, member_request_id, request_digest, reply_to_member_message_id
		) values ($1, $2, 2, 'member', $3, 'Invalid member reply', 'invalid-member-reply', decode(repeat('01', 32), 'hex'), $4)
	`, uuid.NewString(), conversationID, bootstrap.UserID, source.MessageID); err == nil {
		t.Fatal("member message with a reply source was accepted")
	}

	machineID := insertReplyMachine(t, ctx, pool, bootstrap.SpaceID, bootstrap.UserID)
	if _, err := pool.Exec(ctx, `
		update conversation_reply_claims
		set current_machine_id = $1,
			current_fence = 1,
			lease_expires_at = clock_timestamp() + interval '5 minutes',
			context_start_seq = 1,
			context_end_seq = 1,
			output_digest = decode(repeat('01', 32), 'hex'),
			committed_reply_message_id = $2,
			committed_reply_author = 'carry'
		where source_message_id = $2
	`, machineID, source.MessageID); err == nil {
		t.Fatal("private claim accepted a member message as its committed Carry reply")
	}
	if _, err := pool.Exec(ctx, `delete from conversation_reply_claims where source_message_id = $1`, source.MessageID); err != nil {
		t.Fatalf("remove source claim: %v", err)
	}
	carryMessageID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		insert into conversation_messages (
			message_id, conversation_id, message_seq, author, text, reply_to_member_message_id
		) values ($1, $2, 2, 'carry', 'Valid Carry reply shape', $3)
	`, carryMessageID, conversationID, source.MessageID); err != nil {
		t.Fatalf("insert valid Carry reply shape: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into conversation_reply_claims (source_message_id, conversation_id)
		values ($1, $2)
	`, carryMessageID, conversationID); err == nil {
		t.Fatal("private reply claim accepted a Carry message as its source")
	}
}

func TestMigratePreservesPreNode12MachinesAndMarksHistoricalRevokerUnknown(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatalf("reset schema for Machine upgrade: %v", err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `
		create table carry_schema_migrations (
			version text primary key,
			applied_at timestamptz not null default transaction_timestamp()
		)
	`); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, filename := range []string{
		"0001_node0_foundation.sql", "0002_first_durable_work.sql", "0003_work_open_lifecycle_only.sql",
		"0004_native_execution_authority.sql", "0005_simplify_execution.sql", "0006_explicit_run_retry.sql",
		"0007_private_conversation.sql", "0008_work_result_check.sql", "0009_email_identity_first_space.sql",
		"0010_external_identity_login.sql", "0011_identity_method_management.sql", "0012_member_admission.sql",
		"0013_member_removal.sql", "0014_browser_approved_cli.sql",
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

	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Upgrade Owner", SpaceName: "Upgrade Space"})
	if err != nil {
		t.Fatal(err)
	}
	activeID, revokedID := uuid.NewString(), uuid.NewString()
	enrolledAt := time.Date(2026, time.August, 1, 2, 3, 4, 0, time.UTC)
	revokedAt := enrolledAt.Add(48 * time.Hour)
	activeKey, revokedKey := []byte("pre-node12-active-spki"), []byte("pre-node12-revoked-spki")
	if _, err := pool.Exec(ctx, `
		insert into machines (
			machine_id, space_id, display_name, public_key_der, certificate_pem,
			certificate_serial, enrolled_by_user_id, enrollment_idempotency_key, enrolled_at, revoked_at
		) values
			($1,$2,'Old Active',$3,'old-active-certificate','1001',$4,'old-active-enrollment',$5,null),
			($6,$2,'Old Revoked',$7,'old-revoked-certificate','1002',$4,'old-revoked-enrollment',$5,$8)
	`, activeID, member.SpaceID, activeKey, member.UserID, enrolledAt, revokedID, revokedKey, revokedAt); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("apply Node 12 Machine migration: %v", err)
	}

	var gotActiveKey, gotRevokedKey, activeCertificate, revokedCertificate []byte
	var activeSpace, activeName, activeSerial, activeEnroller string
	var revokedSpace, revokedName, revokedSerial, revokedEnroller string
	var activeEnrolledAt, revokedEnrolledAt, gotRevokedAt time.Time
	var activeRevokedAt *time.Time
	var historicalActor *string
	if err := pool.QueryRow(ctx, `
		select space_id, display_name, public_key_der, certificate_pem, certificate_serial,
			enrolled_by_user_id, enrolled_at, revoked_at
		from machines where machine_id=$1
	`, activeID).Scan(&activeSpace, &activeName, &gotActiveKey, &activeCertificate, &activeSerial, &activeEnroller, &activeEnrolledAt, &activeRevokedAt); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		select space_id, display_name, public_key_der, certificate_pem, certificate_serial,
			enrolled_by_user_id, enrolled_at, revoked_at, revocation_actor_kind
		from machines where machine_id=$1
	`, revokedID).Scan(&revokedSpace, &revokedName, &gotRevokedKey, &revokedCertificate, &revokedSerial, &revokedEnroller, &revokedEnrolledAt, &gotRevokedAt, &historicalActor); err != nil {
		t.Fatal(err)
	}
	if activeSpace != member.SpaceID || activeName != "Old Active" || activeSerial != "1001" || activeEnroller != member.UserID ||
		revokedSpace != member.SpaceID || revokedName != "Old Revoked" || revokedSerial != "1002" || revokedEnroller != member.UserID ||
		!bytes.Equal(gotActiveKey, activeKey) || !bytes.Equal(gotRevokedKey, revokedKey) ||
		string(activeCertificate) != "old-active-certificate" || string(revokedCertificate) != "old-revoked-certificate" ||
		!activeEnrolledAt.Equal(enrolledAt) || !revokedEnrolledAt.Equal(enrolledAt) || activeRevokedAt != nil ||
		!gotRevokedAt.Equal(revokedAt) || historicalActor == nil || *historicalActor != "not_recorded" {
		t.Fatalf("upgraded Machine facts changed: active=%s/%s/%q/%q/%s/%s/%s/%v revoked=%s/%s/%q/%q/%s/%s/%s/%s/%v",
			activeID, activeSpace, gotActiveKey, activeCertificate, activeSerial, activeEnroller, activeEnrolledAt, activeRevokedAt,
			revokedID, revokedSpace, gotRevokedKey, revokedCertificate, revokedSerial, revokedEnroller, revokedEnrolledAt, gotRevokedAt, historicalActor)
	}
	var oldColumnExists bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from information_schema.columns where table_schema='public' and table_name='machines' and column_name='enrollment_idempotency_key')`).Scan(&oldColumnExists); err != nil {
		t.Fatal(err)
	}
	if oldColumnExists {
		t.Fatal("obsolete enrollment idempotency column survived Node 12 migration")
	}

	sessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id,user_id,identity_proof_method,expires_at) values ($1,$2,'email',transaction_timestamp()+interval '1 hour')`, sessionID, member.UserID); err != nil {
		t.Fatal(err)
	}
	connections := testMachineConnections(t, store)
	page, _, err := connections.List(ctx, sessionID, member.SpaceID, "")
	if err != nil || len(page.Machines) != 2 {
		t.Fatalf("upgraded Machine inventory = %#v, %v", page, err)
	}
	var historicalFound bool
	for _, record := range page.Machines {
		if record.MachineID == revokedID {
			historicalFound = record.State == "Revoked" && record.RevocationActor == "not_recorded"
		}
	}
	if !historicalFound {
		t.Fatalf("historical revoked Machine was not projected as Not recorded: %#v", page.Machines)
	}
	if _, err := connections.RevokeFromHost(ctx, activeID, "1001", uuid.NewString()); err != nil {
		t.Fatalf("preserved active Machine could not use exact certificate identity: %v", err)
	}
	if _, err := connections.Poll(ctx, "not-a-poll-secret"); !errors.Is(err, machine.ErrMachineUnavailable) {
		t.Fatalf("invalid poll error = %v", err)
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
	if _, err := pool.Exec(ctx, `
		insert into spaces (
			space_id, name, slug, created_by_user_id, create_idempotency_key, create_request_digest
		) values (
			'20000000-0000-0000-0000-000000000099', 'Missing Digest',
			'20000000000000000000000000000099',
			'10000000-0000-0000-0000-000000000001', 'missing-digest', null
		)
	`); err == nil {
		t.Fatal("upgraded schema accepted explicit Space creation without a request digest")
	}
}

func TestMigration18BackfillsExternalLoginAdmissionSource(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `create table carry_schema_migrations (version text primary key, applied_at timestamptz not null default transaction_timestamp())`); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0018_external_login_admission.sql" {
			continue
		}
		migration, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			connection.Release()
			t.Fatal(readErr)
		}
		if err := applyMigration(ctx, connection, entry.Name(), string(migration)); err != nil {
			connection.Release()
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
	connection.Release()

	transactionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into external_login_transactions(transaction_id,provider,purpose,expires_at) values($1,'google','login',transaction_timestamp()+interval '10 minutes')`, transactionID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var sourceBytes int
	if err := pool.QueryRow(ctx, `select octet_length(source_digest) from external_login_transactions where transaction_id=$1`, transactionID).Scan(&sourceBytes); err != nil || sourceBytes != 32 {
		t.Fatalf("backfilled source bytes = %d, %v", sourceBytes, err)
	}
	var indexes int
	if err := pool.QueryRow(ctx, `select count(*) from pg_indexes where schemaname='public' and indexname in ('external_login_expiry_idx','external_login_live_source_idx')`).Scan(&indexes); err != nil || indexes != 2 {
		t.Fatalf("external login admission indexes = %d, %v", indexes, err)
	}
}

func TestMigration17AddsLoginOnlyInvitationContinuationWithoutForeignKey(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `create table carry_schema_migrations (version text primary key, applied_at timestamptz not null default transaction_timestamp())`); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0017_invitation_login_continuation.sql" {
			continue
		}
		migration, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			connection.Release()
			t.Fatal(readErr)
		}
		if err := applyMigration(ctx, connection, entry.Name(), string(migration)); err != nil {
			connection.Release()
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
	connection.Release()

	transactionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into external_login_transactions(transaction_id,provider,expires_at) values($1,'google',transaction_timestamp()+interval '10 minutes')`, transactionID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var continuation *string
	if err := pool.QueryRow(ctx, `select invitation_id from external_login_transactions where transaction_id=$1`, transactionID).Scan(&continuation); err != nil || continuation != nil {
		t.Fatalf("backfilled continuation = %v, %v", continuation, err)
	}
	arbitraryInvitationID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into external_login_transactions(transaction_id,provider,purpose,invitation_id,source_digest,expires_at) values($1,'github','login',$2,decode(repeat('00',32),'hex'),transaction_timestamp()+interval '10 minutes')`, uuid.NewString(), arbitraryInvitationID); err != nil {
		t.Fatalf("non-FK continuation was rejected: %v", err)
	}
	targetUserID := uuid.NewString()
	initiatingSessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into carry_users(user_id,display_name) values($1,'Migration 17 User')`, targetUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into browser_sessions(session_id,user_id,expires_at,identity_proof_method) values($1,$2,transaction_timestamp()+interval '1 hour','github')`, initiatingSessionID, targetUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into external_login_transactions(transaction_id,provider,purpose,target_user_id,initiating_session_id,invitation_id,source_digest,expires_at) values($1,'github','link',$2,$3,$4,decode(repeat('00',32),'hex'),transaction_timestamp()+interval '10 minutes')`, uuid.NewString(), targetUserID, initiatingSessionID, arbitraryInvitationID); err == nil {
		t.Fatal("valid link-purpose transaction accepted an invitation continuation")
	}
	var foreignKeys int
	if err := pool.QueryRow(ctx, `select count(*) from pg_constraint where conrelid='external_login_transactions'::regclass and contype='f' and pg_get_constraintdef(oid) like '%invitation_id%'`).Scan(&foreignKeys); err != nil || foreignKeys != 0 {
		t.Fatalf("invitation continuation foreign keys = %d, %v", foreignKeys, err)
	}
}

func TestMigration16BackfillsImmutableSpaceSlugAndUserLabel(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatalf("reset schema for Space upgrade: %v", err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `
		create table carry_schema_migrations (
			version text primary key,
			applied_at timestamptz not null default transaction_timestamp()
		)
	`); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		connection.Release()
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0016_space_choice_slug.sql" {
			continue
		}
		migration, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			connection.Release()
			t.Fatal(readErr)
		}
		if err := applyMigration(ctx, connection, entry.Name(), string(migration)); err != nil {
			connection.Release()
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
	connection.Release()

	const userID = "10000000-0000-4000-8000-000000000016"
	const spaceID = "20000000-0000-4000-8000-000000000016"
	if _, err := pool.Exec(ctx, `insert into carry_users(user_id,display_name) values($1,null)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into spaces(space_id,name) values($1,'Legacy Space')`, spaceID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate legacy Space: %v", err)
	}
	var slug, displayName string
	if err := pool.QueryRow(ctx, `
		select space.slug, carry_user.display_name
		from spaces as space
		cross join carry_users as carry_user
		where space.space_id=$1 and carry_user.user_id=$2
	`, spaceID, userID).Scan(&slug, &displayName); err != nil {
		t.Fatal(err)
	}
	expectedLabel, err := identity.FallbackDisplayName(userID)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "20000000000040008000000000000016" || displayName != expectedLabel {
		t.Fatalf("backfill = slug %q label %q, want %q / %q", slug, displayName, "20000000000040008000000000000016", expectedLabel)
	}
}
