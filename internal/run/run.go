package run

import (
	"errors"
	"strings"
	"time"
)

const (
	MaxUnderstandingBytes = 60 * 1024
	MaxNextStepBytes      = 8 * 1024
)

var (
	ErrNoRunAvailable = errors.New("no Work is ready to run")
	ErrStaleAttempt   = errors.New("run attempt is no longer authorized")
	ErrInvalidUpdate  = errors.New("understanding and next step must be non-empty and within size limits")
	ErrInvalidOutcome = errors.New("attempt outcome must be failed or unknown")
)

type State string

const (
	StateActive    State = "active"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateUnknown   State = "unknown"
)

// Message is member-authored Work input included in one fixed Run range.
type Message struct {
	AuthorUserID string
	Text         string
}

// Claim is the complete immutable descriptor granted to one Machine Attempt.
type Claim struct {
	RunID                    string
	AttemptID                string
	WorkID                   string
	Fence                    int64
	LeaseExpiresAt           time.Time
	Goal                     string
	CurrentUnderstanding     string
	CurrentNextStep          string
	BaseUnderstandingVersion int64
	InputEndSeq              int64
	Messages                 []Message
}

type CommitCommand struct {
	MachineID                string
	RunID                    string
	AttemptID                string
	Fence                    int64
	BaseUnderstandingVersion int64
	InputEndSeq              int64
	Understanding            string
	NextStep                 string
}

type FinishCommand struct {
	MachineID string
	RunID     string
	AttemptID string
	Fence     int64
	Outcome   State
}

func ValidateUnderstandingUpdate(understanding string, nextStep string) (string, string, error) {
	understanding = strings.TrimSpace(understanding)
	nextStep = strings.TrimSpace(nextStep)
	if understanding == "" || nextStep == "" || len(understanding) > MaxUnderstandingBytes || len(nextStep) > MaxNextStepBytes {
		return "", "", ErrInvalidUpdate
	}
	return understanding, nextStep, nil
}

func ValidateUnresolvedOutcome(outcome State) error {
	if outcome != StateFailed && outcome != StateUnknown {
		return ErrInvalidOutcome
	}
	return nil
}
