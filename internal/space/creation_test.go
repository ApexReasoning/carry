package space

import (
	"errors"
	"testing"
)

func TestNewCreateSpaceCommandOwnsCanonicalFactsAndRequestDigest(t *testing.T) {
	t.Parallel()

	command, err := NewCreateSpaceCommand(CreateSpaceRequest{
		UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Name:           "  Research Team  ",
		IdempotencyKey: "  create-research  ",
	})
	if err != nil {
		t.Fatalf("derive Space command: %v", err)
	}
	if command.SpaceID == "" || command.UserID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("identity = %#v", command)
	}
	if command.Name != "Research Team" || command.Slug != "research-team" || command.Suffix != 0 {
		t.Fatalf("canonical facts = %#v", command)
	}
	if command.IdempotencyKey != "create-research" || command.RequestDigest == ([32]byte{}) {
		t.Fatalf("request identity = %#v", command)
	}
}

func TestNewCreateSpaceCommandBindsSuffixIntoRequestDigest(t *testing.T) {
	t.Parallel()

	request := CreateSpaceRequest{
		UserID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Name:           "Research",
		Suffix:         2,
		IdempotencyKey: "create-research-2",
	}
	first, err := NewCreateSpaceCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Suffix = 3
	second, err := NewCreateSpaceCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	if second.RequestDigest == first.RequestDigest {
		t.Fatal("suffix did not change request digest")
	}
	if second.SpaceID == first.SpaceID {
		t.Fatal("proposed Space IDs must be distinct before PostgreSQL chooses a winner")
	}
}

func TestNewCreateSpaceCommandRejectsInvalidAuthorityOrRequestIdentity(t *testing.T) {
	t.Parallel()

	if _, err := NewCreateSpaceCommand(CreateSpaceRequest{
		Name:           "Research",
		IdempotencyKey: "key",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing User error = %v", err)
	}
	if _, err := NewCreateSpaceCommand(CreateSpaceRequest{
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
