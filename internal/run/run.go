package run

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	MaxUnderstandingBytes = 60 * 1024
	MaxNextStepBytes      = 8 * 1024
	agentCredentialPrefix = "carry_agent_"
)

var (
	ErrNoCoordinatorNeeded = errors.New("no work input needs coordination")
	ErrNoPendingRun        = errors.New("no coordinator run is pending")
	ErrStaleAttempt        = errors.New("run attempt is no longer authorized")
	ErrInvalidDraft        = errors.New("understanding and next step must be non-empty and within size limits")
	ErrInvalidOutcome      = errors.New("attempt outcome must be failed or unknown")
)

type State string

const (
	StatePending   State = "pending"
	StateActive    State = "active"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateUnknown   State = "unknown"
)

type InputKind string

const (
	InputGoal    InputKind = "goal"
	InputMessage InputKind = "message"
)

type Coordinator struct {
	RunID         string
	WorkID        string
	SpaceID       string
	InputStartSeq int64
	InputEndSeq   int64
	BaseRevision  int64
	State         State
	CreatedAt     time.Time
}

type Claim struct {
	Coordinator
	AttemptID       string
	Fence           int64
	WriterToken     string
	AgentCredential string
	LeaseExpiresAt  time.Time
}

type Input struct {
	Sequence     int64     `json:"sequence"`
	Kind         InputKind `json:"kind"`
	AuthorUserID string    `json:"author_user_id,omitempty"`
	Text         string    `json:"text"`
}

type Context struct {
	RunID                string
	AttemptID            string
	WorkID               string
	SpaceID              string
	Goal                 string
	CurrentUnderstanding string
	CurrentNextStep      string
	InputStartSeq        int64
	InputEndSeq          int64
	BaseRevision         int64
	Fence                int64
	Inputs               []Input
}

type CommitCommand struct {
	RunID           string
	AttemptID       string
	Fence           int64
	WriterToken     string
	AgentCredential string
	BaseRevision    int64
	InputEndSeq     int64
	Understanding   string
	NextStep        string
}

type FinishCommand struct {
	RunID           string
	AttemptID       string
	Fence           int64
	WriterToken     string
	AgentCredential string
	Outcome         State
}

type AgentCredential struct {
	Secret string
	Digest [sha256.Size]byte
}

func NewAgentCredential() (AgentCredential, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return AgentCredential{}, fmt.Errorf("generate Agent credential: %w", err)
	}
	secret := agentCredentialPrefix + base64.RawURLEncoding.EncodeToString(secretBytes)
	return AgentCredential{Secret: secret, Digest: DigestAgentCredential(secret)}, nil
}

func DigestAgentCredential(secret string) [sha256.Size]byte {
	return sha256.Sum256([]byte(secret))
}

func ValidateDraft(understanding string, nextStep string) (string, string, error) {
	understanding = strings.TrimSpace(understanding)
	nextStep = strings.TrimSpace(nextStep)
	if understanding == "" || nextStep == "" || len(understanding) > MaxUnderstandingBytes || len(nextStep) > MaxNextStepBytes {
		return "", "", ErrInvalidDraft
	}
	return understanding, nextStep, nil
}

func ValidateUnresolvedOutcome(outcome State) error {
	if outcome != StateFailed && outcome != StateUnknown {
		return ErrInvalidOutcome
	}
	return nil
}
