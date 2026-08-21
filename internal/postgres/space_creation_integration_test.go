//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSpaceCreationReplaysExactlyAndCreatesInitialMembership(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	userID := insertSpaceCreator(t, ctx, pool)
	creator, err := space.NewCreator(store)
	if err != nil {
		t.Fatal(err)
	}
	request := space.CreateSpaceRequest{
		UserID:         userID,
		Name:           "Research Team",
		IdempotencyKey: "create-research",
	}
	created, err := creator.Create(ctx, request)
	if err != nil {
		t.Fatalf("create Space: %v", err)
	}
	replayed, err := creator.Create(ctx, request)
	if err != nil {
		t.Fatalf("replay Space: %v", err)
	}
	if replayed != created {
		t.Fatalf("replayed = %#v, want %#v", replayed, created)
	}
	request.Suffix = 2
	if _, err := creator.Create(ctx, request); !errors.Is(err, space.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	var spaces, memberships int
	if err := pool.QueryRow(ctx, `select count(*) from spaces where space_id=$1`, created.SpaceID).Scan(&spaces); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships where space_id=$1 and user_id=$2 and can_manage_members and can_enroll_machines`, created.SpaceID, userID).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if spaces != 1 || memberships != 1 || created.Slug != "research-team" {
		t.Fatalf("facts = spaces %d memberships %d created %#v", spaces, memberships, created)
	}
}

func TestConcurrentSpaceSlugHasOneWinnerAndExplicitSuffixProgression(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	creator, err := space.NewCreator(store)
	if err != nil {
		t.Fatal(err)
	}
	users := []string{
		insertSpaceCreator(t, ctx, pool),
		insertSpaceCreator(t, ctx, pool),
	}
	start := make(chan struct{})
	results := make(chan struct {
		user    string
		created space.CreatedSpace
		err     error
	}, len(users))
	var wait sync.WaitGroup
	for index, userID := range users {
		wait.Add(1)
		go func(index int, userID string) {
			defer wait.Done()
			<-start
			created, createErr := creator.Create(ctx, space.CreateSpaceRequest{
				UserID:         userID,
				Name:           "Acme",
				IdempotencyKey: "create-acme-" + string(rune('a'+index)),
			})
			results <- struct {
				user    string
				created space.CreatedSpace
				err     error
			}{
				user:    userID,
				created: created,
				err:     createErr,
			}
		}(index, userID)
	}
	close(start)
	wait.Wait()
	close(results)
	var winner, loser string
	for result := range results {
		if result.err == nil {
			winner = result.user
			continue
		}
		var conflict *space.SlugConflictError
		if !errors.As(result.err, &conflict) {
			t.Fatalf("loser error = %v", result.err)
		}
		if conflict.Slug != "acme" || conflict.SuggestedSlug != "acme-2" || conflict.SuggestedSuffix != 2 {
			t.Fatalf("first conflict = %#v", conflict)
		}
		loser = result.user
	}
	if winner == "" || loser == "" || winner == loser {
		t.Fatalf("winner/loser = %q/%q", winner, loser)
	}
	if _, err := creator.Create(ctx, space.CreateSpaceRequest{
		UserID:         loser,
		Name:           "Acme",
		Suffix:         2,
		IdempotencyKey: "create-acme-2",
	}); err != nil {
		t.Fatalf("accept suffix 2: %v", err)
	}
	third := insertSpaceCreator(t, ctx, pool)
	_, err = creator.Create(ctx, space.CreateSpaceRequest{
		UserID:         third,
		Name:           "Acme",
		Suffix:         2,
		IdempotencyKey: "conflict-acme-2",
	})
	var conflict *space.SlugConflictError
	if !errors.As(err, &conflict) || conflict.SuggestedSlug != "acme-3" || conflict.SuggestedSuffix != 3 {
		t.Fatalf("second conflict = %#v, %v", conflict, err)
	}
	if _, err := creator.Create(ctx, space.CreateSpaceRequest{
		UserID:         third,
		Name:           "Acme",
		Suffix:         3,
		IdempotencyKey: "create-acme-3",
	}); err != nil {
		t.Fatalf("accept suffix 3: %v", err)
	}
	fourth := insertSpaceCreator(t, ctx, pool)
	_, err = creator.Create(ctx, space.CreateSpaceRequest{
		UserID:         fourth,
		Name:           "Acme",
		Suffix:         3,
		IdempotencyKey: "conflict-acme-3",
	})
	conflict = nil
	if !errors.As(err, &conflict) || conflict.SuggestedSlug != "acme-4" || conflict.SuggestedSuffix != 4 {
		t.Fatalf("third conflict = %#v, %v", conflict, err)
	}
}

func TestSpaceSlugUniqueIndexWaitsAndRechecks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := openMigratedTestPool(t, ctx)

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	if _, err := first.Exec(ctx, `insert into spaces(space_id,name,slug) values ($1,'First','wait-check')`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, insertErr := pool.Exec(ctx, `insert into spaces(space_id,name,slug) values ($1,'Second','wait-check')`, uuid.NewString())
		result <- insertErr
	}()
	select {
	case err := <-result:
		t.Fatalf("conflicting insert did not wait: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("losing insert succeeded after winner commit")
	}

	if _, err := pool.Exec(ctx, `delete from spaces where slug='wait-check'`); err != nil {
		t.Fatal(err)
	}
	rollbackWinner, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rollbackWinner.Exec(ctx, `insert into spaces(space_id,name,slug) values ($1,'First','rollback-check')`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	result = make(chan error, 1)
	go func() {
		_, insertErr := pool.Exec(ctx, `insert into spaces(space_id,name,slug) values ($1,'Second','rollback-check')`, uuid.NewString())
		result <- insertErr
	}()
	select {
	case err := <-result:
		t.Fatalf("rollback scenario did not wait: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := rollbackWinner.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("second insert did not become winner after rollback: %v", err)
	}
}

func insertSpaceCreator(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	userID := uuid.NewString()
	displayName, err := identity.FallbackDisplayName(userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into carry_users(user_id,display_name) values($1,$2)`, userID, displayName); err != nil {
		t.Fatal(err)
	}
	return userID
}
