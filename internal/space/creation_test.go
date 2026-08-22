package space

import (
	"context"
	"errors"
	"testing"
)

func TestCreatorOwnsCanonicalSpaceFactsAndRequestDigest(t *testing.T) {
	t.Parallel()

	persistence := &recordingSpaceCreationPersistence{}
	creator, err := NewCreator(persistence)
	if err != nil {
		t.Fatal(err)
	}
	created, err := creator.Create(context.Background(), CreateSpaceRequest{
		UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Name:           "  Research Team  ",
		IdempotencyKey: "  create-research  ",
	})
	if err != nil {
		t.Fatalf("create Space: %v", err)
	}
	command := persistence.command
	if command.SpaceID == "" || command.UserID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("identity = %#v", command)
	}
	if command.Name != "Research Team" || command.Slug != "research-team" || command.Suffix != 0 {
		t.Fatalf("canonical facts = %#v", command)
	}
	if command.IdempotencyKey != "create-research" || command.RequestDigest == ([32]byte{}) {
		t.Fatalf("request identity = %#v", command)
	}
	if created.SpaceID != command.SpaceID || created.Name != command.Name || created.Slug != command.Slug {
		t.Fatalf("created = %#v", created)
	}
}

func TestCreatorBindsSuffixIntoRequestDigest(t *testing.T) {
	t.Parallel()

	persistence := &recordingSpaceCreationPersistence{}
	creator, err := NewCreator(persistence)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateSpaceRequest{
		UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Name:           "Research",
		Suffix:         2,
		IdempotencyKey: "create-research-2",
	}
	if _, err := creator.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	first := persistence.command.RequestDigest
	request.Suffix = 3
	if _, err := creator.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if persistence.command.RequestDigest == first {
		t.Fatal("suffix did not change request digest")
	}
}

func TestCreatorRejectsInvalidAuthorityOrRequestIdentity(t *testing.T) {
	t.Parallel()

	creator, err := NewCreator(&recordingSpaceCreationPersistence{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creator.Create(context.Background(), CreateSpaceRequest{
		Name:           "Research",
		IdempotencyKey: "key",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing User error = %v", err)
	}
	if _, err := creator.Create(context.Background(), CreateSpaceRequest{
		UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Name:   "Research",
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestSlugConflictAdvancesWithoutPromisingAvailability(t *testing.T) {
	t.Parallel()

	conflict := NewSlugConflictError(CreateSpaceCommand{
		Name: "Acme",
		Slug: "acme",
	})
	if conflict.SuggestedSlug != "acme-2" || conflict.SuggestedSuffix != 2 {
		t.Fatalf("first conflict = %#v", conflict)
	}
	conflict = NewSlugConflictError(CreateSpaceCommand{
		Name:   "Acme",
		Slug:   "acme-2",
		Suffix: 2,
	})
	if conflict.SuggestedSlug != "acme-3" || conflict.SuggestedSuffix != 3 {
		t.Fatalf("second conflict = %#v", conflict)
	}
	conflict = NewSlugConflictError(CreateSpaceCommand{
		Name:   "Acme",
		Slug:   "acme-9999",
		Suffix: MaxSpaceSlugSuffix,
	})
	if conflict.SuggestedSlug != "" || conflict.SuggestedSuffix != 0 {
		t.Fatalf("exhausted conflict = %#v", conflict)
	}
}

type recordingSpaceCreationPersistence struct {
	command CreateSpaceCommand
}

func (persistence *recordingSpaceCreationPersistence) CreateSpace(_ context.Context, command CreateSpaceCommand) (CreatedSpace, error) {
	persistence.command = command
	return CreatedSpace{
		SpaceID:           command.SpaceID,
		Name:              command.Name,
		Slug:              command.Slug,
		CanManageMembers:  true,
		CanEnrollMachines: true,
	}, nil
}
