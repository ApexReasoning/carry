package space

import (
	"context"
	"testing"
)

func TestFirstSpaceOwnsIdentityNormalizationAndDigest(t *testing.T) {
	t.Parallel()
	persistence := &recordingFirstSpacePersistence{}
	creator, err := NewFirstSpace(persistence)
	if err != nil {
		t.Fatalf("compose first Space: %v", err)
	}
	request := CreateFirstRequest{
		UserID: "11111111-1111-4111-8111-111111111111", DisplayName: "  Ada  ",
		SpaceName: "  Research  ", IdempotencyKey: "create-first",
	}
	if _, err := creator.Create(context.Background(), request); err != nil {
		t.Fatalf("create first Space: %v", err)
	}
	first := persistence.command
	if first.SpaceID == "" || first.DisplayName != "Ada" || first.SpaceName != "Research" {
		t.Fatalf("first Space command = %#v", first)
	}
	if _, err := creator.Create(context.Background(), request); err != nil {
		t.Fatalf("replay first Space: %v", err)
	}
	if persistence.command.SpaceID == first.SpaceID || persistence.command.RequestDigest != first.RequestDigest {
		t.Fatalf("replay identity/digest first = %#v replay = %#v", first, persistence.command)
	}
}

type recordingFirstSpacePersistence struct {
	command CreateFirstCommand
}

func (persistence *recordingFirstSpacePersistence) CreateFirstSpace(_ context.Context, command CreateFirstCommand) (CreatedSpace, error) {
	persistence.command = command
	return CreatedSpace{SpaceID: command.SpaceID, Name: command.SpaceName}, nil
}
