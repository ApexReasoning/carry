package identity

import "testing"

func TestFallbackDisplayNameUsesCanonicalUserIDPrefix(t *testing.T) {
	t.Parallel()

	got, err := FallbackDisplayName("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	if err != nil {
		t.Fatalf("fallback display name: %v", err)
	}
	if got != "Member aaaaaaaa" {
		t.Fatalf("fallback display name = %q", got)
	}
}

func TestFallbackDisplayNameRejectsInvalidUserID(t *testing.T) {
	t.Parallel()

	if _, err := FallbackDisplayName("not-a-user-id"); err == nil {
		t.Fatal("invalid User ID was accepted")
	}
}
