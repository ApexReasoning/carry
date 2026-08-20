//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
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
	details, err := restartedStore.LoadWork(ctx, work.LoadCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: workID})
	if err != nil {
		t.Fatalf("load Work after Store restart: %v", err)
	}
	if details.Work.Goal != command.Goal || len(details.Messages) != 0 {
		t.Fatalf("loaded Work = %#v", details)
	}
	listed, err := restartedStore.ListWorks(ctx, work.ListCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID})
	if err != nil {
		t.Fatalf("list Works: %v", err)
	}
	if len(listed.Works) != 1 || listed.Works[0].WorkID != workID {
		t.Fatalf("listed Works = %#v", listed)
	}
}

func TestWorkQueriesUseBoundedCursorPagesAndDisplayNames(t *testing.T) {
	t.Run("Work summaries", func(t *testing.T) {
		ctx := context.Background()
		pool := openMigratedTestPool(t, ctx)
		store := NewStore(pool)
		bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
			DisplayName: "Pagination Member", SpaceName: "Bounded Work Space",
			TokenExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		ids := make([]string, 0, work.ListPageSize+2)
		for index := range work.ListPageSize + 2 {
			created, createErr := store.CreateWork(ctx, work.CreateCommand{
				SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
				Goal:           fmt.Sprintf("Bounded responsibility %02d", index),
				IdempotencyKey: fmt.Sprintf("bounded-work-%02d", index),
			})
			if createErr != nil {
				t.Fatalf("create Work %d: %v", index, createErr)
			}
			ids = append(ids, created.WorkID)
		}
		if _, err := pool.Exec(ctx, `update works set created_at = '2026-08-20T00:00:00Z' where space_id = $1`, bootstrap.SpaceID); err != nil {
			t.Fatalf("align Work timestamps: %v", err)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(ids)))
		first, err := store.ListWorks(ctx, work.ListCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID})
		if err != nil {
			t.Fatalf("list newest Work page: %v", err)
		}
		if len(first.Works) != work.ListPageSize || !first.HasEarlier || first.Works[0].WorkID != ids[0] ||
			first.Works[0].OwnerDisplayName != "Pagination Member" {
			t.Fatalf("newest Work page = %#v", first)
		}
		second, err := store.ListWorks(ctx, work.ListCommand{
			UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID,
			Before: first.Works[len(first.Works)-1].WorkID,
		})
		if err != nil {
			t.Fatalf("list older Work page: %v", err)
		}
		if len(second.Works) != 2 || second.HasEarlier || second.Works[0].WorkID != ids[50] {
			t.Fatalf("older Work page = %#v", second)
		}
		if _, err := store.ListWorks(ctx, work.ListCommand{
			UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, Before: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		}); !errors.Is(err, work.ErrInvalidCursor) {
			t.Fatalf("invalid Work cursor error = %v", err)
		}
	})

	t.Run("Work messages", func(t *testing.T) {
		ctx := context.Background()
		pool := openMigratedTestPool(t, ctx)
		store := NewStore(pool)
		bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
			DisplayName: "Message Author", SpaceName: "Bounded Message Space",
			TokenExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		created, err := store.CreateWork(ctx, work.CreateCommand{
			SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
			Goal: "Keep a bounded message history", IdempotencyKey: "bounded-message-work",
		})
		if err != nil {
			t.Fatalf("create Work: %v", err)
		}
		text := strings.Repeat("m", work.MaxMessageBytes)
		messageIDs := make([]string, 0, 5)
		for index := range 5 {
			message, appendErr := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
				WorkID: created.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
				Text: text, IdempotencyKey: fmt.Sprintf("bounded-message-%d", index),
			})
			if appendErr != nil {
				t.Fatalf("append message %d: %v", index, appendErr)
			}
			messageIDs = append(messageIDs, message.MessageID)
		}
		first, err := store.LoadWork(ctx, work.LoadCommand{
			UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID,
		})
		if err != nil {
			t.Fatalf("load newest Work messages: %v", err)
		}
		if len(first.Messages) != 4 || !first.HasEarlierMessages ||
			first.Messages[0].AuthorDisplayName != "Message Author" || first.Messages[0].MessageID != messageIDs[1] {
			t.Fatalf("newest message page = %#v", first)
		}
		older, err := store.LoadWork(ctx, work.LoadCommand{
			UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID,
			BeforeMessage: first.Messages[0].MessageID,
		})
		if err != nil {
			t.Fatalf("load older Work messages: %v", err)
		}
		if len(older.Messages) != 1 || older.HasEarlierMessages || older.Messages[0].MessageID != messageIDs[0] {
			t.Fatalf("older message page = %#v", older)
		}
		other, err := store.CreateWork(ctx, work.CreateCommand{
			SpaceID: bootstrap.SpaceID, CreatorUserID: bootstrap.UserID,
			Goal: "Own a foreign message cursor", IdempotencyKey: "foreign-cursor-work",
		})
		if err != nil {
			t.Fatalf("create other Work: %v", err)
		}
		foreign, err := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
			WorkID: other.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
			Text: "Foreign cursor", IdempotencyKey: "foreign-cursor-message",
		})
		if err != nil {
			t.Fatalf("append foreign cursor message: %v", err)
		}
		if _, err := store.LoadWork(ctx, work.LoadCommand{
			UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID,
			BeforeMessage: foreign.MessageID,
		}); !errors.Is(err, work.ErrInvalidCursor) {
			t.Fatalf("foreign Work message cursor error = %v", err)
		}
	})
}

func TestConcurrentWorkMessagesReceiveContinuousInputSequence(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
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

	details, err := NewStore(pool).LoadWork(ctx, work.LoadCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID})
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
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
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

	messages, err := queries.ListNewestWorkMessages(ctx, created.WorkID)
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

	details, err := store.LoadWork(ctx, work.LoadCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID})
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
	bootstrap, err := bootstrapForTest(ctx, store, BootstrapCommand{
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
	if _, err := store.LoadWork(ctx, work.LoadCommand{UserID: bootstrap.UserID, SpaceID: bootstrap.SpaceID, WorkID: created.WorkID}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("load after membership revocation error = %v", err)
	}
	if _, err := store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: created.WorkID, SpaceID: bootstrap.SpaceID, AuthorUserID: bootstrap.UserID,
		Text: "This must not be recorded", IdempotencyKey: "revoked-message",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("append after membership revocation error = %v", err)
	}
}
