//go:build integration

package postgres

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConcurrentBootstrapCreatesOneSpace(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)

	store := NewStore(pool)
	commands := []BootstrapCommand{
		{DisplayName: "Ada", SpaceName: "Research", TokenExpiresAt: time.Now().Add(24 * time.Hour)},
		{DisplayName: "Grace", SpaceName: "Operations", TokenExpiresAt: time.Now().Add(24 * time.Hour)},
	}

	var wait sync.WaitGroup
	wait.Add(len(commands))
	results := make(chan error, len(commands))
	for _, command := range commands {
		go func() {
			defer wait.Done()
			_, err := store.Bootstrap(ctx, command)
			results <- err
		}()
	}
	wait.Wait()
	close(results)

	var succeeded int
	var rejected int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAlreadyBootstrapped):
			rejected++
		default:
			t.Fatalf("bootstrap returned unexpected error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("bootstrap outcomes = %d succeeded, %d rejected", succeeded, rejected)
	}

	var spaces int
	if err := pool.QueryRow(ctx, `select count(*) from spaces`).Scan(&spaces); err != nil {
		t.Fatalf("count spaces: %v", err)
	}
	if spaces != 1 {
		t.Fatalf("space count = %d, want 1", spaces)
	}
}

func TestBootstrapTokenAuthenticatesMember(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)

	result, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName:    "Katherine",
		SpaceName:      "Flight Research",
		TokenExpiresAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	member, err := store.AuthenticateUserToken(ctx, result.UserToken)
	if err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	if member.UserID != result.UserID {
		t.Fatalf("authenticated user = %s, want %s", member.UserID, result.UserID)
	}
	if _, err := store.AuthenticateUserToken(ctx, "carry_user_not-a-token"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("invalid token error = %v, want identity.ErrUnauthenticated", err)
	}
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
		truncate run_attempts, runs, work_messages, works, browser_sessions,
		machines, user_tokens, space_memberships, spaces, carry_users cascade
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
