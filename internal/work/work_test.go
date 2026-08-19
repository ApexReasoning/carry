package work

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeGoalKeepsOneMeaningfulSentence(t *testing.T) {
	t.Parallel()

	goal, err := NormalizeGoal("  Compare three pricing changes  ")
	if err != nil {
		t.Fatalf("normalize goal: %v", err)
	}
	if goal != "Compare three pricing changes" {
		t.Fatalf("goal = %q", goal)
	}
	for _, invalid := range []string{"", "   ", strings.Repeat("x", MaxGoalBytes+1)} {
		if _, err := NormalizeGoal(invalid); !errors.Is(err, ErrInvalidGoal) {
			t.Errorf("invalid goal error = %v", err)
		}
	}
}

func TestValidateMessagePreservesAuthoredText(t *testing.T) {
	t.Parallel()

	const text = "  Keep the leading context\n"
	if err := ValidateMessage(text); err != nil {
		t.Fatalf("validate message: %v", err)
	}
	for _, invalid := range []string{"", "\n\t", strings.Repeat("x", MaxMessageBytes+1)} {
		if err := ValidateMessage(invalid); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("invalid message error = %v", err)
		}
	}
}

func TestValidateIdempotencyKey(t *testing.T) {
	t.Parallel()

	if err := ValidateIdempotencyKey("request-1"); err != nil {
		t.Fatalf("validate idempotency key: %v", err)
	}
	for _, invalid := range []string{"", "   ", strings.Repeat("x", MaxIdempotencyKeyBytes+1)} {
		if err := ValidateIdempotencyKey(invalid); !errors.Is(err, ErrInvalidIdempotency) {
			t.Errorf("invalid idempotency key error = %v", err)
		}
	}
}

func TestRequestDigestsBindMeaningBearingFields(t *testing.T) {
	t.Parallel()

	create := CreateDigest("space-1", "member-1", "Track supplier lead times")
	if create == CreateDigest("space-2", "member-1", "Track supplier lead times") ||
		create == CreateDigest("space-1", "member-2", "Track supplier lead times") ||
		create == CreateDigest("space-1", "member-1", "Track a different supplier") {
		t.Fatal("create digest did not bind all meaning-bearing fields")
	}
	message := MessageDigest("work-1", "member-1", "Add the APAC supplier")
	if message == MessageDigest("work-2", "member-1", "Add the APAC supplier") ||
		message == MessageDigest("work-1", "member-2", "Add the APAC supplier") ||
		message == MessageDigest("work-1", "member-1", "Add the EU supplier") {
		t.Fatal("message digest did not bind all meaning-bearing fields")
	}
}
