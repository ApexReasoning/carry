package work

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxGoalBytes            = 2_000
	MaxMessageBytes         = 60 * 1_024
	MaxIdempotencyKeyBytes  = 255
	ListPageSize            = 50
	MessagePageSize         = 50
	MaxMessagePageTextBytes = 256 * 1_024
)

var (
	ErrInvalidGoal         = errors.New("work goal must contain between 1 and 2000 bytes")
	ErrInvalidMessage      = errors.New("work message must contain between 1 and 61440 bytes")
	ErrInvalidIdempotency  = errors.New("idempotency key must contain between 1 and 255 bytes")
	ErrInvalidCursor       = errors.New("work cursor is invalid")
	ErrIdempotencyConflict = errors.New("idempotency key refers to different work input")
	ErrRetryNotNeeded      = errors.New("work does not need retry")
	ErrReviewNotCurrent    = errors.New("work review is not current")
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

type AcceptReviewCommand struct {
	WorkID         string
	SpaceID        string
	ReviewID       string
	AcceptedBy     string
	IdempotencyKey string
}

type ListCommand struct {
	UserID   string
	SpaceID  string
	Before   string
	NeedsYou bool
}

type LoadCommand struct {
	UserID        string
	SpaceID       string
	WorkID        string
	BeforeMessage string
}

type Work struct {
	WorkID             string
	SpaceID            string
	Goal               string
	Lifecycle          Lifecycle
	OwnerUserID        string
	OwnerDisplayName   string
	CreatorUserID      string
	CreatorDisplayName string
	Understanding      string
	NextStep           string
	HasUnappliedInput  bool
	NeedsRetry         bool
	NeedsReview        bool
	ReviewID           string
	CreatedAt          time.Time
}

type Summary struct {
	WorkID             string
	SpaceID            string
	Goal               string
	Lifecycle          Lifecycle
	OwnerUserID        string
	OwnerDisplayName   string
	CreatorUserID      string
	CreatorDisplayName string
	HasUnappliedInput  bool
	NeedsRetry         bool
	NeedsReview        bool
	CreatedAt          time.Time
}

type Page struct {
	Works      []Summary
	HasEarlier bool
}

type Message struct {
	MessageID         string
	WorkID            string
	AuthorUserID      string
	AuthorDisplayName string
	Text              string
	InputSeq          int64
	CreatedAt         time.Time
}

type Details struct {
	Work               Work
	Messages           []Message
	HasEarlierMessages bool
}

func NormalizeGoal(value string) (string, error) {
	goal := strings.TrimSpace(value)
	if len(goal) == 0 || len(goal) > MaxGoalBytes || !validText(goal) {
		return "", ErrInvalidGoal
	}
	return goal, nil
}

func ValidateMessage(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > MaxMessageBytes || !validText(value) {
		return ErrInvalidMessage
	}
	return nil
}

func ValidateIdempotencyKey(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > MaxIdempotencyKeyBytes || !validText(value) {
		return ErrInvalidIdempotency
	}
	return nil
}

func validText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func CreateDigest(spaceID string, creatorUserID string, goal string) [sha256.Size]byte {
	return digestFields(spaceID, creatorUserID, goal)
}

func MessageDigest(workID string, authorUserID string, text string) [sha256.Size]byte {
	return digestFields(workID, authorUserID, text)
}

func ReviewContentDigest(understanding string, nextStep string) [sha256.Size]byte {
	return digestFields(understanding, nextStep)
}

func ReviewAcceptanceDigest(workID string, reviewID string) [sha256.Size]byte {
	return digestFields(workID, reviewID)
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
