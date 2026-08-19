package work

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"strings"
	"time"
)

const (
	MaxGoalBytes           = 2_000
	MaxMessageBytes        = 60 * 1_024
	MaxIdempotencyKeyBytes = 255
)

var (
	ErrInvalidGoal         = errors.New("work goal must contain between 1 and 2000 bytes")
	ErrInvalidMessage      = errors.New("work message must contain between 1 and 61440 bytes")
	ErrInvalidIdempotency  = errors.New("idempotency key must contain between 1 and 255 bytes")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different work input")
	ErrRetryNotNeeded      = errors.New("work does not need retry")
	ErrNotFound            = errors.New("work not found")
	ErrNotOpen             = errors.New("work is not open")
)

type Lifecycle string

const LifecycleOpen Lifecycle = "open"

type CreateCommand struct {
	SpaceID        string
	CreatorUserID  string
	Goal           string
	IdempotencyKey string
}

type AppendMessageCommand struct {
	WorkID         string
	SpaceID        string
	AuthorUserID   string
	Text           string
	IdempotencyKey string
}

type RetryCommand struct {
	WorkID         string
	SpaceID        string
	RequestedBy    string
	IdempotencyKey string
}

type Work struct {
	WorkID            string
	SpaceID           string
	Goal              string
	Lifecycle         Lifecycle
	OwnerUserID       string
	CreatorUserID     string
	Understanding     string
	NextStep          string
	HasUnappliedInput bool
	NeedsRetry        bool
	CreatedAt         time.Time
}

type Message struct {
	MessageID    string
	WorkID       string
	AuthorUserID string
	Text         string
	InputSeq     int64
	CreatedAt    time.Time
}

type Details struct {
	Work     Work
	Messages []Message
}

func NormalizeGoal(value string) (string, error) {
	goal := strings.TrimSpace(value)
	if len(goal) == 0 || len(goal) > MaxGoalBytes {
		return "", ErrInvalidGoal
	}
	return goal, nil
}

func ValidateMessage(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > MaxMessageBytes {
		return ErrInvalidMessage
	}
	return nil
}

func ValidateIdempotencyKey(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > MaxIdempotencyKeyBytes {
		return ErrInvalidIdempotency
	}
	return nil
}

func CreateDigest(spaceID string, creatorUserID string, goal string) [sha256.Size]byte {
	return digestFields(spaceID, creatorUserID, goal)
}

func MessageDigest(workID string, authorUserID string, text string) [sha256.Size]byte {
	return digestFields(workID, authorUserID, text)
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
