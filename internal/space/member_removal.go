package space

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidMemberRemoval       = errors.New("member removal is invalid")
	ErrInvalidMemberCursor        = errors.New("member cursor is invalid")
	ErrMemberUnavailable          = errors.New("member is unavailable")
	ErrRemovalSuccessorRequired   = errors.New("an active successor is required")
	ErrRemovalSuccessorUnexpected = errors.New("a successor is not needed")
	ErrRemovalSuccessorInvalid    = errors.New("the successor is invalid")
	ErrLastMemberManager          = errors.New("Space must retain a member manager")
	ErrLastMachineEnroller        = errors.New("Space must retain a Machine enroller")
)

type RemoveMemberRequest struct {
	SpaceID, ActorUserID, TargetUserID, SuccessorUserID, IdempotencyKey string
}

type RemoveMemberCommand struct {
	SpaceID, ActorUserID, TargetUserID, SuccessorUserID, IdempotencyKey string
	RequestDigest                                                       [sha256.Size]byte
}

func NewRemoveMemberCommand(request RemoveMemberRequest) (RemoveMemberCommand, error) {
	idempotencyKey, validKey := normalizeCommandKey(request.IdempotencyKey)
	if uuid.Validate(request.SpaceID) != nil || uuid.Validate(request.ActorUserID) != nil ||
		uuid.Validate(request.TargetUserID) != nil ||
		(request.SuccessorUserID != "" && uuid.Validate(request.SuccessorUserID) != nil) ||
		!validKey {
		return RemoveMemberCommand{}, ErrInvalidMemberRemoval
	}
	encoded, err := json.Marshal(struct {
		SpaceID, ActorUserID, TargetUserID, SuccessorUserID, IdempotencyKey string
	}{request.SpaceID, request.ActorUserID, request.TargetUserID, request.SuccessorUserID, idempotencyKey})
	if err != nil {
		return RemoveMemberCommand{}, fmt.Errorf("digest member removal: %w", err)
	}
	return RemoveMemberCommand{
		SpaceID: request.SpaceID, ActorUserID: request.ActorUserID, TargetUserID: request.TargetUserID,
		SuccessorUserID: request.SuccessorUserID, IdempotencyKey: idempotencyKey,
		RequestDigest: sha256.Sum256(encoded),
	}, nil
}

const MemberPageSize = 50

type ListMembersCommand struct {
	SpaceID, ActorUserID, AfterUserID string
}

type SpaceMember struct {
	UserID, DisplayName                 string
	CanManageMembers, CanEnrollMachines bool
	OpenWorkCount                       int64
	JoinedAt                            time.Time
}

type MemberPage struct {
	Members    []SpaceMember
	NextCursor string
}
