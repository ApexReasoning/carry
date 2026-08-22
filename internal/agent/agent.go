// Package agent owns durable Agent identity vocabulary and pure projections.
package agent

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	MaxObservationsPerReport = 32
	MaxAdapterKeyBytes       = 63
	MaxOccurrenceKeyBytes    = 127
	MaxNameBaseBytes         = 96
	MaxNameBytes             = 128
	AvatarPresetCount        = 8
)

var (
	ErrInvalidAdapterKey    = errors.New("Agent adapter key is invalid")
	ErrInvalidOccurrenceKey = errors.New("Agent occurrence key is invalid")
	ErrInvalidVocabulary    = errors.New("Agent recognition vocabulary is invalid")
	ErrInvalidAgentName     = errors.New("Agent name is invalid")
	ErrInvalidAgentIdentity = errors.New("Agent identity is invalid")
)

// AdapterKey identifies one explicitly composed native Agent family.
type AdapterKey string

// OccurrenceKey identifies one stable occurrence within a concrete adapter.
type OccurrenceKey string

// Descriptor is the immutable Agent-owned recognition fact for one adapter family.
type Descriptor struct {
	Key                     AdapterKey
	NameBase                string
	MaxOccurrencesPerReport int
}

// Vocabulary is an immutable set of recognized native Agent families.
type Vocabulary struct {
	descriptors map[AdapterKey]Descriptor
}

// NewVocabulary validates and copies a bounded recognition vocabulary.
func NewVocabulary(descriptors ...Descriptor) (Vocabulary, error) {
	if len(descriptors) == 0 || len(descriptors) > MaxObservationsPerReport {
		return Vocabulary{}, ErrInvalidVocabulary
	}

	copied := make(map[AdapterKey]Descriptor, len(descriptors))
	normalizedBases := make(map[string]struct{}, len(descriptors))
	totalOccurrences := 0
	for _, descriptor := range descriptors {
		if !ValidAdapterKey(descriptor.Key) || descriptor.MaxOccurrencesPerReport <= 0 {
			return Vocabulary{}, ErrInvalidVocabulary
		}
		if descriptor.MaxOccurrencesPerReport > MaxObservationsPerReport-totalOccurrences {
			return Vocabulary{}, ErrInvalidVocabulary
		}
		if !validNameBase(descriptor.NameBase) {
			return Vocabulary{}, ErrInvalidVocabulary
		}
		nameKey, err := NormalizeName(descriptor.NameBase)
		if err != nil {
			return Vocabulary{}, ErrInvalidVocabulary
		}
		if _, exists := copied[descriptor.Key]; exists {
			return Vocabulary{}, ErrInvalidVocabulary
		}
		if _, exists := normalizedBases[nameKey]; exists {
			return Vocabulary{}, ErrInvalidVocabulary
		}
		copied[descriptor.Key] = descriptor
		normalizedBases[nameKey] = struct{}{}
		totalOccurrences += descriptor.MaxOccurrencesPerReport
	}
	return Vocabulary{descriptors: copied}, nil
}

// NativeVocabulary returns the V1 recognition vocabulary. It contains no process construction facts.
func NativeVocabulary() Vocabulary {
	return Vocabulary{descriptors: map[AdapterKey]Descriptor{
		"pi": {
			Key:                     "pi",
			NameBase:                "Pi",
			MaxOccurrencesPerReport: 1,
		},
		"codex": {
			Key:                     "codex",
			NameBase:                "Codex",
			MaxOccurrencesPerReport: 1,
		},
	}}
}

// Descriptor returns a copied recognition fact for key.
func (vocabulary Vocabulary) Descriptor(key AdapterKey) (Descriptor, bool) {
	descriptor, ok := vocabulary.descriptors[key]
	return descriptor, ok
}

// Empty reports whether the vocabulary contains no recognized family.
func (vocabulary Vocabulary) Empty() bool {
	return len(vocabulary.descriptors) == 0
}

// ValidAdapterKey reports whether key has the stable wire form owned by Agent.
func ValidAdapterKey(key AdapterKey) bool {
	value := string(key)
	if len(value) == 0 || len(value) > MaxAdapterKeyBytes {
		return false
	}
	for index, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && character == '-' {
			continue
		}
		return false
	}
	return true
}

// ValidOccurrenceKey reports whether key has the stable adapter-local wire form owned by Agent.
func ValidOccurrenceKey(key OccurrenceKey) bool {
	value := string(key)
	if len(value) == 0 || len(value) > MaxOccurrenceKeyBytes {
		return false
	}
	for index, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '.' || character == '_' || character == '-') {
			continue
		}
		return false
	}
	return true
}

// NormalizeName returns the immutable NFKC and case-folded uniqueness key for a display name.
func NormalizeName(name string) (string, error) {
	if !validName(name, MaxNameBytes) {
		return "", ErrInvalidAgentName
	}
	normalized := cases.Fold().String(norm.NFKC.String(name))
	if normalized == "" {
		return "", ErrInvalidAgentName
	}
	return normalized, nil
}

// NameForOrdinal returns the independently allocated family name and uniqueness key.
func NameForOrdinal(nameBase string, ordinal int) (string, string, error) {
	if !validNameBase(nameBase) || ordinal <= 0 {
		return "", "", ErrInvalidAgentName
	}
	name := nameBase
	if ordinal > 1 {
		name += " " + strconv.Itoa(ordinal)
	}
	nameKey, err := NormalizeName(name)
	if err != nil {
		return "", "", err
	}
	return name, nameKey, nil
}

// Lifecycle is the durable Agent identity state.
type Lifecycle string

const (
	LifecycleActive  Lifecycle = "active"
	LifecycleRemoved Lifecycle = "removed"
)

// Agent is the durable identity fact consumed by Agent projections.
type Agent struct {
	AgentID     string
	MachineID   string
	OwnerUserID string
	Name        string
	Lifecycle   Lifecycle
}

// Presence is the Machine-derived presence projected beside an Agent identity.
type Presence struct {
	Online       bool
	LastActiveAt *time.Time
}

// InventoryRecord is the Agent-owned identity projection returned beside a Machine page.
type InventoryRecord struct {
	AgentID      string
	MachineID    string
	Name         string
	AvatarIndex  int
	OwnerUserID  string
	OwnerName    string
	Lifecycle    Lifecycle
	Online       bool
	LastActiveAt *time.Time
}

// ProjectInventory combines an Agent identity with already-derived presence without exposing adapter facts.
func ProjectInventory(identity Agent, ownerName string, presence Presence) (InventoryRecord, error) {
	avatarIndex, err := AvatarIndex(identity.AgentID)
	if err != nil {
		return InventoryRecord{}, err
	}
	online := presence.Online
	if identity.Lifecycle == LifecycleRemoved {
		online = false
	}
	return InventoryRecord{
		AgentID:      identity.AgentID,
		MachineID:    identity.MachineID,
		Name:         identity.Name,
		AvatarIndex:  avatarIndex,
		OwnerUserID:  identity.OwnerUserID,
		OwnerName:    ownerName,
		Lifecycle:    identity.Lifecycle,
		Online:       online,
		LastActiveAt: presence.LastActiveAt,
	}, nil
}

// AvatarIndex returns the deterministic preset palette index for an immutable Agent ID.
func AvatarIndex(agentID string) (int, error) {
	parsed, err := uuid.Parse(agentID)
	if err != nil {
		return 0, ErrInvalidAgentIdentity
	}
	digest := sha256.Sum256([]byte(parsed.String()))
	return int(digest[len(digest)-1] % AvatarPresetCount), nil
}

func validNameBase(name string) bool {
	return validName(name, MaxNameBaseBytes)
}

func validName(name string, maxBytes int) bool {
	if !utf8.ValidString(name) || name == "" || len(name) > maxBytes || strings.TrimSpace(name) != name {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
