package space

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrIdempotencyConflict = errors.New("Space idempotency key was reused for a different request")

// SpaceCreationPersistence owns the atomic Space and creator Membership mutation.
type SpaceCreationPersistence interface {
	CreateSpace(context.Context, CreateSpaceCommand) (CreatedSpace, error)
}

// Creator derives one exact Space creation command from authenticated User input.
type Creator struct {
	persistence SpaceCreationPersistence
}

// NewCreator constructs the Space creation behavior.
func NewCreator(persistence SpaceCreationPersistence) (*Creator, error) {
	if persistence == nil {
		return nil, errors.New("Space creation persistence is required")
	}
	return &Creator{persistence: persistence}, nil
}

// CreateSpaceRequest contains only the authenticated User's creation intent.
type CreateSpaceRequest struct {
	UserID         string
	Name           string
	Suffix         int
	IdempotencyKey string
}

// Create creates or exactly replays one Space.
func (creator *Creator) Create(ctx context.Context, request CreateSpaceRequest) (CreatedSpace, error) {
	if uuid.Validate(request.UserID) != nil {
		return CreatedSpace{}, ErrForbidden
	}
	key, validKey := normalizeCommandKey(request.IdempotencyKey)
	if !validKey {
		return CreatedSpace{}, ErrIdempotencyConflict
	}
	name, slug, err := NormalizeSpaceName(request.Name, request.Suffix)
	if err != nil {
		return CreatedSpace{}, err
	}
	digest, err := spaceCreationDigest(name, request.Suffix)
	if err != nil {
		return CreatedSpace{}, err
	}
	return creator.persistence.CreateSpace(ctx, CreateSpaceCommand{
		SpaceID:        uuid.NewString(),
		UserID:         request.UserID,
		Name:           name,
		Slug:           slug,
		Suffix:         request.Suffix,
		IdempotencyKey: key,
		RequestDigest:  digest,
	})
}

// CreateSpaceCommand carries Space-owned canonical facts to persistence.
type CreateSpaceCommand struct {
	SpaceID        string
	UserID         string
	Name           string
	Slug           string
	Suffix         int
	IdempotencyKey string
	RequestDigest  [sha256.Size]byte
}

// CreatedSpace is the creator Membership returned after commit or exact replay.
type CreatedSpace struct {
	SpaceID           string
	Name              string
	Slug              string
	CanManageMembers  bool
	CanEnrollMachines bool
}

// SlugConflictError reports one losing slug and its next unreserved attempt.
type SlugConflictError struct {
	Slug            string
	SuggestedSlug   string
	SuggestedSuffix int
}

func (conflict *SlugConflictError) Error() string {
	return "Space URL is already in use"
}

// NewSlugConflictError derives the next truthful, unreserved conflict suggestion.
func NewSlugConflictError(command CreateSpaceCommand) *SlugConflictError {
	conflict := &SlugConflictError{Slug: command.Slug}
	next := command.Suffix + 1
	if next == 1 {
		next = 2
	}
	if next > MaxSpaceSlugSuffix {
		return conflict
	}
	_, suggestion, err := NormalizeSpaceName(command.Name, next)
	if err != nil {
		return conflict
	}
	conflict.SuggestedSlug = suggestion
	conflict.SuggestedSuffix = next
	return conflict
}

func spaceCreationDigest(name string, suffix int) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Name   string `json:"name"`
		Suffix int    `json:"suffix,omitempty"` // Owner semantics make zero and absent equivalent: both mean no suffix.
	}{
		Name:   name,
		Suffix: suffix,
	})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("marshal Space creation digest: %w", err)
	}
	return sha256.Sum256(encoded), nil
}
