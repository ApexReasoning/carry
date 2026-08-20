package space

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrInvalidSpaceCreation = errors.New("first Space creation is invalid")
	ErrAlreadyHasSpace      = errors.New("User already belongs to a Space")
	ErrIdempotencyConflict  = errors.New("Space idempotency key was reused for a different request")
)

// FirstSpacePersistence is the complete atomic mutation consumed by Space.
type FirstSpacePersistence interface {
	CreateFirstSpace(context.Context, CreateFirstCommand) (CreatedSpace, error)
}

type FirstSpace struct {
	persistence FirstSpacePersistence
}

func NewFirstSpace(persistence FirstSpacePersistence) (*FirstSpace, error) {
	if persistence == nil {
		return nil, errors.New("first Space persistence is required")
	}
	return &FirstSpace{persistence: persistence}, nil
}

type CreateFirstRequest struct {
	UserID         string
	DisplayName    string
	SpaceName      string
	IdempotencyKey string
}

func (creator *FirstSpace) Create(ctx context.Context, request CreateFirstRequest) (CreatedSpace, error) {
	displayName := strings.TrimSpace(request.DisplayName)
	spaceName := strings.TrimSpace(request.SpaceName)
	digest, err := firstSpaceRequestDigest(displayName, spaceName)
	if err != nil {
		return CreatedSpace{}, err
	}
	return creator.persistence.CreateFirstSpace(ctx, CreateFirstCommand{
		SpaceID: uuid.NewString(), UserID: request.UserID, DisplayName: displayName, SpaceName: spaceName,
		IdempotencyKey: request.IdempotencyKey, RequestDigest: digest,
	})
}

type CreateFirstCommand struct {
	SpaceID        string
	UserID         string
	DisplayName    string
	SpaceName      string
	IdempotencyKey string
	RequestDigest  [sha256.Size]byte
}

type CreatedSpace struct {
	SpaceID           string
	Name              string
	CanManageMembers  bool
	CanEnrollMachines bool
}

func firstSpaceRequestDigest(displayName string, spaceName string) ([sha256.Size]byte, error) {
	if displayName == "" || spaceName == "" {
		return [sha256.Size]byte{}, ErrInvalidSpaceCreation
	}
	encoded, err := json.Marshal(struct {
		DisplayName string `json:"display_name"`
		SpaceName   string `json:"space_name"`
	}{DisplayName: displayName, SpaceName: spaceName})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
