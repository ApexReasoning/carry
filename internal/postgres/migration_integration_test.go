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
		"machine_runtime_observations",
		"works",
		"work_messages",
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
