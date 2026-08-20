//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
)

func TestReviewRequiredCommitOnlyRequestsReviewForCurrentInput(t *testing.T) {
	t.Run("current input", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim Run: %v", err)
		}
		if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
			MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
			Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
			InputEndSeq: claim.InputEndSeq, Understanding: "The renewal recommendation is ready.",
			NextStep: "The responsible member should review the recommendation.", ReviewRequired: true,
		}); err != nil {
			t.Fatalf("commit reviewable understanding: %v", err)
		}
		details, err := fixture.store.LoadWork(ctx, work.LoadCommand{
			UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID,
			WorkID: fixture.work.WorkID,
		})
		if err != nil {
			t.Fatalf("load reviewable Work: %v", err)
		}
		if !details.Work.NeedsReview || details.Work.ReviewID == "" {
			t.Fatalf("reviewable Work = %#v", details.Work)
		}
	})

	t.Run("newer input", func(t *testing.T) {
		ctx := context.Background()
		fixture := newRunFixture(t, ctx)
		claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
		if err != nil {
			t.Fatalf("claim Run: %v", err)
		}
		if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
			WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
			AuthorUserID: fixture.bootstrap.UserID, Text: "Include the late finance correction.",
			IdempotencyKey: "late-finance-correction",
		}); err != nil {
			t.Fatalf("append newer input: %v", err)
		}
		if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
			MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
			Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
			InputEndSeq: claim.InputEndSeq, Understanding: "The earlier recommendation is ready.",
			NextStep: "Apply the late correction.", ReviewRequired: true,
		}); err != nil {
			t.Fatalf("commit bounded understanding: %v", err)
		}
		details, err := fixture.store.LoadWork(ctx, work.LoadCommand{
			UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID,
			WorkID: fixture.work.WorkID,
		})
		if err != nil {
			t.Fatalf("load Work with newer input: %v", err)
		}
		if details.Work.NeedsReview || details.Work.ReviewID != "" {
			t.Fatalf("older bounded result requested review = %#v", details.Work)
		}
	})
}

func TestWorkReviewAcceptanceIsExactIdempotentAndLeavesWorkOpen(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	reviewID := commitReviewableWork(t, ctx, fixture)
	command := work.AcceptReviewCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		ReviewID: reviewID, AcceptedBy: fixture.bootstrap.UserID,
		IdempotencyKey: "accept-renewal-result",
	}
	if err := fixture.store.AcceptWorkReview(ctx, command); err != nil {
		t.Fatalf("accept Work review: %v", err)
	}
	if err := fixture.store.AcceptWorkReview(ctx, command); err != nil {
		t.Fatalf("replay Work review acceptance: %v", err)
	}
	details, err := fixture.store.LoadWork(ctx, work.LoadCommand{
		UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID,
		WorkID: fixture.work.WorkID,
	})
	if err != nil {
		t.Fatalf("load accepted Work: %v", err)
	}
	if details.Work.NeedsReview || details.Work.ReviewID != "" || details.Work.Lifecycle != work.LifecycleOpen {
		t.Fatalf("accepted Work = %#v", details.Work)
	}

	conflict := command
	conflict.ReviewID = uuid.NewString()
	if err := fixture.store.AcceptWorkReview(ctx, conflict); !errors.Is(err, work.ErrIdempotencyConflict) {
		t.Fatalf("conflicting acceptance replay error = %v", err)
	}

	otherSpaceID := uuid.NewString()
	if _, err := fixture.store.pool.Exec(ctx, `
		insert into spaces (space_id, name) values ($1, 'Other Space')
	`, otherSpaceID); err != nil {
		t.Fatalf("create other Space: %v", err)
	}
	if _, err := fixture.store.pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_enroll_machines)
		values ($1, $2, false)
	`, otherSpaceID, fixture.bootstrap.UserID); err != nil {
		t.Fatalf("create other Space membership: %v", err)
	}
	if _, err := fixture.store.pool.Exec(ctx, `
		update space_memberships
		set revoked_at = transaction_timestamp(), version = version + 1
		where space_id = $1 and user_id = $2
	`, fixture.bootstrap.SpaceID, fixture.bootstrap.UserID); err != nil {
		t.Fatalf("revoke accepting member: %v", err)
	}
	wrongSpace := command
	wrongSpace.SpaceID = otherSpaceID
	if err := fixture.store.AcceptWorkReview(ctx, wrongSpace); !errors.Is(err, work.ErrNotFound) {
		t.Fatalf("cross-Space acceptance replay error = %v", err)
	}
	if err := fixture.store.AcceptWorkReview(ctx, command); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("former member acceptance replay error = %v", err)
	}
}

func TestWorkReviewAcceptanceRejectsStaleAndUnauthorizedMembers(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	reviewID := commitReviewableWork(t, ctx, fixture)

	crossSpaceUserID := uuid.NewString()
	if _, err := fixture.store.pool.Exec(ctx, `
		insert into carry_users (user_id, display_name) values ($1, 'Cross Space Member')
	`, crossSpaceUserID); err != nil {
		t.Fatalf("create cross-Space member: %v", err)
	}
	if err := fixture.store.AcceptWorkReview(ctx, work.AcceptReviewCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		ReviewID: reviewID, AcceptedBy: crossSpaceUserID, IdempotencyKey: "cross-space-accept",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("cross-Space acceptance error = %v", err)
	}

	otherUserID := createReviewTestMember(t, ctx, fixture)
	if err := fixture.store.AcceptWorkReview(ctx, work.AcceptReviewCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		ReviewID: reviewID, AcceptedBy: otherUserID, IdempotencyKey: "other-member-accept",
	}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("non-owner acceptance error = %v", err)
	}

	if _, err := fixture.store.AppendWorkMessage(ctx, work.AppendMessageCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		AuthorUserID: otherUserID, Text: "The result omitted a required limitation.",
		IdempotencyKey: "result-limitation",
	}); err != nil {
		t.Fatalf("append review-staling message: %v", err)
	}
	if err := fixture.store.AcceptWorkReview(ctx, work.AcceptReviewCommand{
		WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
		ReviewID: reviewID, AcceptedBy: fixture.bootstrap.UserID,
		IdempotencyKey: "accept-stale-result",
	}); !errors.Is(err, work.ErrReviewNotCurrent) {
		t.Fatalf("stale acceptance error = %v", err)
	}
}

func TestConcurrentWorkReviewAcceptanceHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	reviewID := commitReviewableWork(t, ctx, fixture)

	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, key := range []string{"accept-result-a", "accept-result-b"} {
		wait.Add(1)
		go func(idempotencyKey string) {
			defer wait.Done()
			results <- fixture.store.AcceptWorkReview(ctx, work.AcceptReviewCommand{
				WorkID: fixture.work.WorkID, SpaceID: fixture.bootstrap.SpaceID,
				ReviewID: reviewID, AcceptedBy: fixture.bootstrap.UserID,
				IdempotencyKey: idempotencyKey,
			})
		}(key)
	}
	wait.Wait()
	close(results)

	var accepted, rejected int
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, work.ErrReviewNotCurrent) {
			rejected++
		} else {
			t.Fatalf("unexpected concurrent acceptance error: %v", err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("acceptance outcomes = %d accepted, %d rejected", accepted, rejected)
	}
}

func TestNeedsYouListsOnlyCurrentOwnerReviewOrRetry(t *testing.T) {
	ctx := context.Background()
	fixture := newRunFixture(t, ctx)
	commitReviewableWork(t, ctx, fixture)

	retryWork, err := fixture.store.CreateWork(ctx, work.CreateCommand{
		SpaceID: fixture.bootstrap.SpaceID, CreatorUserID: fixture.bootstrap.UserID,
		Goal: "Recover a terminal supplier check", IdempotencyKey: "retry-needs-you",
	})
	if err != nil {
		t.Fatalf("create retry Work: %v", err)
	}
	retryClaim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim retry Work: %v", err)
	}
	if retryClaim.WorkID != retryWork.WorkID {
		t.Fatalf("retry claim Work = %q, want %q", retryClaim.WorkID, retryWork.WorkID)
	}
	if err := fixture.store.FinishUnresolvedAttempt(ctx, run.FinishCommand{
		MachineID: fixture.machineID, RunID: retryClaim.RunID,
		AttemptID: retryClaim.AttemptID, Fence: retryClaim.Fence, Outcome: run.StateFailed,
	}); err != nil {
		t.Fatalf("finish retry Work: %v", err)
	}

	progressWork, err := fixture.store.CreateWork(ctx, work.CreateCommand{
		SpaceID: fixture.bootstrap.SpaceID, CreatorUserID: fixture.bootstrap.UserID,
		Goal: "Continue ordinary supplier research", IdempotencyKey: "ordinary-progress",
	})
	if err != nil {
		t.Fatalf("create progress Work: %v", err)
	}
	progressClaim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim progress Work: %v", err)
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
		MachineID: fixture.machineID, RunID: progressClaim.RunID, AttemptID: progressClaim.AttemptID,
		Fence: progressClaim.Fence, BaseUnderstandingVersion: progressClaim.BaseUnderstandingVersion,
		InputEndSeq: progressClaim.InputEndSeq, Understanding: "Supplier research is progressing.",
		NextStep: "Continue collecting ordinary evidence.", ReviewRequired: false,
	}); err != nil {
		t.Fatalf("commit ordinary progress: %v", err)
	}

	otherUserID := createReviewTestMember(t, ctx, fixture)

	page, err := fixture.store.ListWorks(ctx, work.ListCommand{
		UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID, NeedsYou: true,
	})
	if err != nil {
		t.Fatalf("list owner Needs You: %v", err)
	}
	if len(page.Works) != 2 {
		t.Fatalf("owner Needs You = %#v", page)
	}
	byID := make(map[string]work.Summary, len(page.Works))
	for _, summary := range page.Works {
		byID[summary.WorkID] = summary
	}
	if !byID[fixture.work.WorkID].NeedsReview || !byID[retryWork.WorkID].NeedsRetry {
		t.Fatalf("owner Needs You facts = %#v", page)
	}
	if _, found := byID[progressWork.WorkID]; found {
		t.Fatalf("ordinary progress entered Needs You: %#v", page)
	}

	otherPage, err := fixture.store.ListWorks(ctx, work.ListCommand{
		UserID: otherUserID, SpaceID: fixture.bootstrap.SpaceID, NeedsYou: true,
	})
	if err != nil {
		t.Fatalf("list other member Needs You: %v", err)
	}
	if len(otherPage.Works) != 0 {
		t.Fatalf("other member Needs You = %#v", otherPage)
	}
}

func createReviewTestMember(t *testing.T, ctx context.Context, fixture runFixture) string {
	t.Helper()
	userID := uuid.NewString()
	if _, err := fixture.store.pool.Exec(ctx, `
		insert into carry_users (user_id, display_name) values ($1, 'Other Member')
	`, userID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if _, err := fixture.store.pool.Exec(ctx, `
		insert into space_memberships (space_id, user_id, can_enroll_machines)
		values ($1, $2, false)
	`, fixture.bootstrap.SpaceID, userID); err != nil {
		t.Fatalf("create other membership: %v", err)
	}
	return userID
}

func commitReviewableWork(t *testing.T, ctx context.Context, fixture runFixture) string {
	t.Helper()
	claim, err := fixture.store.ClaimRun(ctx, fixture.machineID)
	if err != nil {
		t.Fatalf("claim reviewable Run: %v", err)
	}
	if err := fixture.store.CommitWorkUnderstanding(ctx, run.CommitCommand{
		MachineID: fixture.machineID, RunID: claim.RunID, AttemptID: claim.AttemptID,
		Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
		InputEndSeq: claim.InputEndSeq, Understanding: "The renewal recommendation is ready.",
		NextStep: "The responsible member should review this stage result.", ReviewRequired: true,
	}); err != nil {
		t.Fatalf("commit reviewable Work: %v", err)
	}
	details, err := fixture.store.LoadWork(ctx, work.LoadCommand{
		UserID: fixture.bootstrap.UserID, SpaceID: fixture.bootstrap.SpaceID,
		WorkID: fixture.work.WorkID,
	})
	if err != nil {
		t.Fatalf("load reviewable Work: %v", err)
	}
	if !details.Work.NeedsReview || details.Work.ReviewID == "" {
		t.Fatalf("reviewable Work = %#v", details.Work)
	}
	return details.Work.ReviewID
}
