//go:build integration

package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testMemberCommand and testMember are integration-fixture shapes only; production bootstrap was deleted in Node 11.
type testMemberCommand struct {
	DisplayName, SpaceName string
}

type testMember struct {
	UserID, SpaceID string
}

func createMemberForTest(ctx context.Context, store *Store, requested testMemberCommand) (testMember, error) {
	userID, spaceID := uuid.NewString(), uuid.NewString()
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return testMember{}, fmt.Errorf("begin test identity fixture: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, $2)`, userID, requested.DisplayName); err != nil {
		return testMember{}, fmt.Errorf("create test User: %w", err)
	}
	var hasSlug bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1 from information_schema.columns
			where table_schema = 'public' and table_name = 'spaces' and column_name = 'slug'
		)
	`).Scan(&hasSlug); err != nil {
		return testMember{}, fmt.Errorf("inspect test Space schema: %w", err)
	}
	if hasSlug {
		if _, err := tx.Exec(ctx, `insert into spaces (space_id, name, slug) values ($1::uuid, $2, replace(($1::uuid)::text, '-', ''))`, spaceID, requested.SpaceName); err != nil {
			return testMember{}, fmt.Errorf("create test Space: %w", err)
		}
	} else if _, err := tx.Exec(ctx, `insert into spaces (space_id, name) values ($1, $2)`, spaceID, requested.SpaceName); err != nil {
		return testMember{}, fmt.Errorf("create legacy test Space: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into space_memberships (space_id, user_id, can_enroll_machines, can_manage_members) values ($1, $2, true, true)`, spaceID, userID); err != nil {
		return testMember{}, fmt.Errorf("create test Membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return testMember{}, fmt.Errorf("commit test identity fixture: %w", err)
	}
	return testMember{UserID: userID, SpaceID: spaceID}, nil
}

func openMigratedTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("CARRY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("CARRY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	requireIsolatedTestDatabase(t, ctx, pool, databaseURL)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		truncate cli_login_lookup_failures, cli_credentials, cli_login_requests,
		run_attempts, runs, conversation_reply_claims, conversation_messages, conversations,
		work_result_checks, work_messages, works, browser_sessions, machines,
		space_invitation_submissions, space_invitations, space_memberships, spaces,
		external_login_transactions, identity_method_unlinks, google_identities, github_identities,
		email_login_attempts, email_login_challenges, email_identities, carry_users cascade
	`); err != nil {
		t.Fatalf("reset PostgreSQL facts: %v", err)
	}
	return pool
}

func requireIsolatedTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseURL string) {
	t.Helper()
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL test URL: %v", err)
	}
	hostname := parsedURL.Hostname()
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		t.Fatalf("refusing non-local PostgreSQL test host %q", hostname)
	}
	var databaseName string
	if err := pool.QueryRow(ctx, `select current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("inspect PostgreSQL test target: %v", err)
	}
	if !strings.HasPrefix(databaseName, "carry_test_") || !strings.HasSuffix(databaseName, "_postgres") {
		t.Fatalf("refusing PostgreSQL test database %q", databaseName)
	}
}
