package conversation

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ApexReasoning/carry/internal/work"
)

const (
	MaxTextBytes           = 16 * 1024
	MaxIdempotencyKeyBytes = 255
	MessagePageSize        = 50
	MaxContextMessages     = 32
	MaxContextTextBytes    = 256 * 1024
)

var (
	ErrInvalidText         = errors.New("conversation message must contain between 1 and 16384 UTF-8 bytes")
	ErrInvalidIdempotency  = errors.New("idempotency key must contain between 1 and 255 bytes")
	ErrInvalidCursor       = errors.New("conversation cursor is invalid")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different private input")
	ErrReplyPending        = errors.New("Carry must reply before another private message")
	ErrNoReplyAvailable    = errors.New("no private Conversation reply is ready")
	ErrStaleReplyClaim     = errors.New("private Conversation reply claim is no longer authorized")
	ErrReplyConflict       = errors.New("private Conversation reply was already committed with different output")
	ErrInvalidContext      = errors.New("private Conversation reply context is invalid")
)

type Author string

const (
	AuthorMember Author = "member"
	AuthorCarry  Author = "carry"
)

type SendCommand struct {
	SpaceID        string
	MemberUserID   string
	Text           string
	IdempotencyKey string
}

type ListCommand struct {
	SpaceID      string
	MemberUserID string
	Before       string
	After        string
}

type Message struct {
	MessageID     string
	Author        Author
	Text          string
	RequestID     string
	CreatedWorkID string
	Sequence      int64
	CreatedAt     time.Time
}

// ContextMessage is the only private Conversation fact exposed to an exact Machine claim.
type ContextMessage struct {
	Author Author
	Text   string
}

// ReplyClaim is one bounded, fenced authority to produce a reply for a source message.
type ReplyClaim struct {
	SourceMessageID string
	Fence           int64
	LeaseExpiresAt  time.Time
	Messages        []ContextMessage
}

type ReplyCandidate struct {
	Reply          string
	DelegationGoal *string
}

type RenewReplyCommand struct {
	MachineID       string
	SourceMessageID string
	Fence           int64
}

type CommitReplyCommand struct {
	MachineID       string
	SourceMessageID string
	Fence           int64
	Candidate       ReplyCandidate
}

type CommitReplyResult struct {
	ReplyMessageID string
	CreatedWorkID  string
}

func NormalizeText(value string) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" || len(text) > MaxTextBytes || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", ErrInvalidText
	}
	return text, nil
}

func NormalizeIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" || len(key) > MaxIdempotencyKeyBytes || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return "", ErrInvalidIdempotency
	}
	return key, nil
}

func ValidateCursors(before string, after string) error {
	if before != "" && after != "" {
		return ErrInvalidCursor
	}
	return nil
}

func MessageDigest(spaceID string, memberUserID string, text string) [sha256.Size]byte {
	return digestFields(spaceID, memberUserID, text)
}

// NormalizeReplyCandidate validates the complete Agent-owned output and returns
// a digest bound to the exact source. A distinct marker separates null from a
// present delegation goal.
func NormalizeReplyCandidate(
	sourceMessageID string,
	candidate ReplyCandidate,
) (ReplyCandidate, [sha256.Size]byte, error) {
	reply, err := NormalizeText(candidate.Reply)
	if err != nil {
		return ReplyCandidate{}, [sha256.Size]byte{}, err
	}
	normalized := ReplyCandidate{Reply: reply}
	if candidate.DelegationGoal == nil {
		return normalized, digestFields(sourceMessageID, reply, "delegation:null"), nil
	}
	goal, err := work.NormalizeGoal(*candidate.DelegationGoal)
	if err != nil {
		return ReplyCandidate{}, [sha256.Size]byte{}, err
	}
	normalized.DelegationGoal = &goal
	return normalized, digestFields(sourceMessageID, reply, "delegation:goal", goal), nil
}

// FixedContextSuffix selects the newest contiguous complete-message suffix
// within both fixed context limits. Input and output remain chronological.
func FixedContextSuffix(messages []ContextMessage) ([]ContextMessage, error) {
	if len(messages) == 0 {
		return nil, ErrInvalidContext
	}
	start := len(messages)
	textBytes := 0
	for start > 0 && len(messages)-start < MaxContextMessages {
		message := messages[start-1]
		if (message.Author != AuthorMember && message.Author != AuthorCarry) ||
			message.Text == "" || len(message.Text) > MaxTextBytes ||
			!utf8.ValidString(message.Text) || strings.ContainsRune(message.Text, 0) {
			return nil, ErrInvalidContext
		}
		if textBytes+len(message.Text) > MaxContextTextBytes {
			break
		}
		textBytes += len(message.Text)
		start--
	}
	if start == len(messages) {
		return nil, ErrInvalidContext
	}
	fixed := make([]ContextMessage, len(messages)-start)
	copy(fixed, messages[start:])
	return fixed, nil
}

func digestFields(fields ...string) [sha256.Size]byte {
	digest := sha256.New()
	for _, field := range fields {
		writeDigestField(digest, field)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func writeDigestField(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}
