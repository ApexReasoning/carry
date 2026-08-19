//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
)

func TestConcurrentWorkCreationReturnsOneDurableWork(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Mae", SpaceName: "Supply Operations", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	command := work.CreateCommand{
		SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
		Goal: "Track supplier lead times", IdempotencyKey: "create-supplier-work",
	}

	const callers = 8
	results := make(chan work.Work, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			created, err := store.CreateWork(ctx, command)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- created
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("create Work: %v", err)
	}

	var workID string
	for created := range results {
		if workID == "" {
			workID = created.WorkID
		}
		if created.WorkID != workID {
			t.Fatalf("idempotent Work ID = %s, want %s", created.WorkID, workID)
		}
		if created.OwnerUserID != bootstrap.UserID || created.CreatorUserID != bootstrap.UserID {
			t.Fatalf("Work owner/creator = %#v", created)
		}
		if !created.HasUnappliedInput {
			t.Fatal("new Work did not report unapplied input")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from works`).Scan(&count); err != nil {
		t.Fatalf("count Works: %v", err)
	}
	if count != 1 {
		t.Fatalf("Work count = %d, want 1", count)
	}

	conflict := command
	conflict.Goal = "Track carrier capacity"
	if _, err := store.CreateWork(ctx, conflict); !errors.Is(err, work.ErrIdempotencyConflict) {
		t.Fatalf("conflicting create error = %v", err)
	}

	restartedStore := NewStore(pool)
	details, err := restartedStore.LoadWork(ctx, bootstrap.UserID, bootstrap.SpaceID, workID)
	if err != nil {
		t.Fatalf("load Work after Store restart: %v", err)
	}
	if details.Work.Goal != command.Goal || len(details.Messages) != 0 {
		t.Fatalf("loaded Work = %#v", details)
	}
	listed, err := restartedStore.ListWorks(ctx, bootstrap.UserID, bootstrap.SpaceID)
	if err != nil {
		t.Fatalf("list Works: %v", err)
	}
	if len(listed) != 1 || listed[0].WorkID != workID {
		t.Fatalf("listed Works = %#v", listed)
	}
}

func TestConcurrentWorkMessagesReceiveContinuousInputSequence(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Lillian", SpaceName: "Customer Research", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	created, err := store.CreateWork(ctx, work.CreateCommand{
		SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
		Goal: "Summarize recurring customer requests", IdempotencyKey: "create-customer-work",
	})
	if err != nil {
		t.Fatalf("create Work: %v", err)
	}

	const messageCount = 20
	messages := make(chan work.Message, messageCount)
	errorsFound := make(chan error, messageCount)
	var wait sync.WaitGroup
	wait.Add(messageCount)
	for index := range messageCount {
		go func() {
			defer wait.Done()
			message, err := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
				WorkID: created.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
				Text:           fmt.Sprintf("Customer observation %02d", index),
				IdempotencyKey: fmt.Sprintf("message-%02d", index),
			})
			if err != nil {
				errorsFound <- err
				return
			}
			messages <- message
		}()
	}
	wait.Wait()
	close(messages)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("append Work Message: %v", err)
	}

	sequences := make([]int, 0, messageCount)
	for message := range messages {
		sequences = append(sequences, int(message.InputSeq))
	}
	sort.Ints(sequences)
	for index, sequence := range sequences {
		if want := index + 2; sequence != want {
			t.Fatalf("input sequence[%d] = %d, want %d; all = %v", index, sequence, want, sequences)
		}
	}

	retryCommand := work.AppendMessageCommand{
		WorkID: created.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
		Text: "Customer observation 00", IdempotencyKey: "message-00",
	}
	firstRetry, err := store.AppendWorkMessage(ctx, retryCommand)
	if err != nil {
		t.Fatalf("retry Work Message: %v", err)
	}
	secondRetry, err := store.AppendWorkMessage(ctx, retryCommand)
	if err != nil {
		t.Fatalf("repeat Work Message retry: %v", err)
	}
	if firstRetry.MessageID != secondRetry.MessageID || firstRetry.InputSeq != secondRetry.InputSeq {
		t.Fatalf("message retry = %#v, then %#v", firstRetry, secondRetry)
	}
	conflict := retryCommand
	conflict.Text = "Different customer observation"
	if _, err := store.AppendWorkMessage(ctx, conflict); !errors.Is(err, work.ErrIdempotencyConflict) {
		t.Fatalf("conflicting Message error = %v", err)
	}

	details, err := NewStore(pool).LoadWork(ctx, bootstrap.UserID, bootstrap.SpaceID, created.WorkID)
	if err != nil {
		t.Fatalf("load Work after appends: %v", err)
	}
	if !details.Work.HasUnappliedInput || len(details.Messages) != messageCount {
		t.Fatalf("Work after messages = %#v", details)
	}
	for index, message := range details.Messages {
		if want := int64(index + 2); message.InputSeq != want {
			t.Fatalf("loaded Message sequence[%d] = %d, want %d", index, message.InputSeq, want)
		}
	}
}

func TestLoadWorkHoldsAConsistentHeadAndMessageSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "June", SpaceName: "Renewal Planning", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	created, err := store.CreateWork(ctx, work.CreateCommand{
		SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
		Goal: "Prepare the renewal recommendation", IdempotencyKey: "create-renewal-work",
	})
	if err != nil {
		t.Fatalf("create Work: %v", err)
	}

	readTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin coherent read: %v", err)
	}
	defer func() { _ = readTransaction.Rollback(context.Background()) }()
	queries := store.queries.WithTx(readTransaction)
	loaded, err := queries.LoadWork(ctx, dbsqlc.LoadWorkParams{
		SpaceID: bootstrap.SpaceID,
		WorkID:  created.WorkID,
	})
	if err != nil {
		t.Fatalf("load locked Work: %v", err)
	}

	appendDone := make(chan error, 1)
	go func() {
		_, appendErr := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
			WorkID: created.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
			Text: "Renewal requires finance approval", IdempotencyKey: "renewal-message",
		})
		appendDone <- appendErr
	}()
	select {
	case appendErr := <-appendDone:
		t.Fatalf("append completed while coherent read held the Work lock: %v", appendErr)
	case <-time.After(100 * time.Millisecond):
	}

	messages, err := queries.ListWorkMessages(ctx, created.WorkID)
	if err != nil {
		t.Fatalf("list messages in coherent read: %v", err)
	}
	if loaded.InputHeadSeq != 1 || len(messages) != 0 {
		t.Fatalf("coherent read = head %d, messages %d; want head 1 and no messages", loaded.InputHeadSeq, len(messages))
	}
	if err := readTransaction.Commit(ctx); err != nil {
		t.Fatalf("commit coherent read: %v", err)
	}
	select {
	case appendErr := <-appendDone:
		if appendErr != nil {
			t.Fatalf("append after coherent read: %v", appendErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("append remained blocked after coherent read committed")
	}

	details, err := store.LoadWork(ctx, bootstrap.UserID, bootstrap.SpaceID, created.WorkID)
	if err != nil {
		t.Fatalf("load Work after append: %v", err)
	}
	if !details.Work.HasUnappliedInput || len(details.Messages) != 1 || details.Messages[0].InputSeq != 2 {
		t.Fatalf("Work after append = %#v", details)
	}
}

func TestWorkAccessRequiresCurrentSpaceMembership(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Annie", SpaceName: "Field Research", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	created, err := store.CreateWork(ctx, work.CreateCommand{
		SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
		Goal: "Prepare the field interview", IdempotencyKey: "create-field-work",
	})
	if err != nil {
		t.Fatalf("create Work: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update space_memberships
		set revoked_at = transaction_timestamp(), version = version + 1
		where space_id = $1 and user_id = $2
	`, bootstrap.SpaceID, bootstrap.UserID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if _, err := store.LoadWork(ctx, bootstrap.UserID, bootstrap.SpaceID, created.WorkID); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("load after membership revocation error = %v", err)
	}
	if _, err := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: created.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
		Text: "This must not be recorded", IdempotencyKey: "revoked-message",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("append after membership revocation error = %v", err)
	}
}
