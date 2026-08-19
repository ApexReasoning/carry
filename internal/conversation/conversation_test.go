package conversation

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTextUsesUTF8ByteLimit(t *testing.T) {
	valid := strings.Repeat("界", MaxTextBytes/3)
	if normalized, err := NormalizeText("  " + valid + "  "); err != nil || normalized != valid {
		t.Fatalf("normalize valid text = %q, %v", normalized, err)
	}
	if _, err := NormalizeText(valid + "界"); !errors.Is(err, ErrInvalidText) {
		t.Fatalf("oversized text error = %v", err)
	}
	for _, invalid := range []string{"   ", "message\x00body", string([]byte{0xff})} {
		if _, err := NormalizeText(invalid); !errors.Is(err, ErrInvalidText) {
			t.Fatalf("invalid text %q error = %v", invalid, err)
		}
	}
}

func TestNormalizeIdempotencyKeyAndCursors(t *testing.T) {
	if key, err := NormalizeIdempotencyKey("  private-message-1  "); err != nil || key != "private-message-1" {
		t.Fatalf("normalize key = %q, %v", key, err)
	}
	if _, err := NormalizeIdempotencyKey(strings.Repeat("x", MaxIdempotencyKeyBytes+1)); !errors.Is(err, ErrInvalidIdempotency) {
		t.Fatalf("oversized key error = %v", err)
	}
	if err := ValidateCursors("before", "after"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("two cursor error = %v", err)
	}
	if err := ValidateCursors("before", ""); err != nil {
		t.Fatalf("single cursor error = %v", err)
	}
}

func TestMessageDigestSeparatesActorAndSpace(t *testing.T) {
	first := MessageDigest("space-a", "member-a", "private question")
	if first == MessageDigest("space-b", "member-a", "private question") ||
		first == MessageDigest("space-a", "member-b", "private question") {
		t.Fatal("message digest did not bind Space and member")
	}
}

func TestReplyCandidateDigestBindsSourceAndNullableDelegation(t *testing.T) {
	withoutGoal, withoutDigest, err := NormalizeReplyCandidate("source-a", ReplyCandidate{
		Reply: "  I can help clarify that.  ",
	})
	if err != nil || withoutGoal.Reply != "I can help clarify that." || withoutGoal.DelegationGoal != nil {
		t.Fatalf("normalize ordinary reply = %#v, %v", withoutGoal, err)
	}
	emptyGoal := ""
	if _, _, err := NormalizeReplyCandidate("source-a", ReplyCandidate{
		Reply: "I can help clarify that.", DelegationGoal: &emptyGoal,
	}); err == nil {
		t.Fatal("present empty delegation goal was accepted as null")
	}
	goal := "  Prepare the renewal packet  "
	withGoal, withDigest, err := NormalizeReplyCandidate("source-a", ReplyCandidate{
		Reply: "I will take that on.", DelegationGoal: &goal,
	})
	if err != nil || withGoal.DelegationGoal == nil || *withGoal.DelegationGoal != "Prepare the renewal packet" {
		t.Fatalf("normalize delegated reply = %#v, %v", withGoal, err)
	}
	if withoutDigest == withDigest {
		t.Fatal("null and present delegation goals share an output digest")
	}
	_, otherSourceDigest, err := NormalizeReplyCandidate("source-b", ReplyCandidate{Reply: withoutGoal.Reply})
	if err != nil || otherSourceDigest == withoutDigest {
		t.Fatal("reply output digest did not bind source message")
	}
}

func TestFixedContextSuffixKeepsCompleteNewestMessagesWithinBothBounds(t *testing.T) {
	messages := make([]ContextMessage, 40)
	for index := range messages {
		messages[index] = ContextMessage{Author: AuthorMember, Text: strings.Repeat("x", 9*1024)}
	}
	fixed, err := FixedContextSuffix(messages)
	if err != nil {
		t.Fatalf("fix context: %v", err)
	}
	if len(fixed) != 28 {
		t.Fatalf("fixed message count = %d, want 28", len(fixed))
	}
	textBytes := 0
	for _, message := range fixed {
		textBytes += len(message.Text)
	}
	if textBytes > MaxContextTextBytes {
		t.Fatalf("fixed context bytes = %d", textBytes)
	}

	small := make([]ContextMessage, 40)
	for index := range small {
		small[index] = ContextMessage{Author: AuthorCarry, Text: "ok"}
	}
	fixed, err = FixedContextSuffix(small)
	if err != nil || len(fixed) != MaxContextMessages {
		t.Fatalf("count-bounded context = %d, %v", len(fixed), err)
	}
}
