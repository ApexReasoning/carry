package host

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
	"github.com/ApexReasoning/carry/internal/space"
)

func TestPendingEnrollmentReusesIdentityAfterResponseLoss(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	member := credentialfile.Credential{
		ServerURL: "https://carry.example.com", CACertificatePEM: "test-ca", UserID: "user-1",
	}
	memberships := []space.Membership{{SpaceID: "space-1", CanEnrollMachines: true}}
	flags := enrollFlags{displayName: "test-host"}
	first, err := loadOrCreatePendingEnrollment(directory, member, memberships, flags)
	if err != nil {
		t.Fatalf("create pending enrollment: %v", err)
	}
	second, err := loadOrCreatePendingEnrollment(directory, member, memberships, flags)
	if err != nil {
		t.Fatalf("resume pending enrollment: %v", err)
	}
	if first.IdempotencyKey != second.IdempotencyKey ||
		!bytes.Equal(first.PublicKeyDER, second.PublicKeyDER) ||
		first.PrivateKeyPEM != second.PrivateKeyPEM {
		t.Fatal("resumed enrollment did not reuse its identity")
	}
	info, err := os.Stat(filepath.Join(directory, "machine-enrollment.json"))
	if err != nil {
		t.Fatalf("stat pending enrollment: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("pending enrollment mode = %o, want 600", got)
	}
}

func TestPendingEnrollmentRejectsChangedCommand(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	member := credentialfile.Credential{
		ServerURL: "https://carry.example.com", CACertificatePEM: "test-ca", UserID: "user-1",
	}
	memberships := []space.Membership{{SpaceID: "space-1", CanEnrollMachines: true}}
	if _, err := loadOrCreatePendingEnrollment(directory, member, memberships, enrollFlags{displayName: "first"}); err != nil {
		t.Fatalf("create pending enrollment: %v", err)
	}
	if _, err := loadOrCreatePendingEnrollment(directory, member, memberships, enrollFlags{displayName: "second"}); err == nil {
		t.Fatal("changed display name accepted for pending enrollment")
	}
	otherMember := member
	otherMember.UserID = "user-2"
	if _, err := loadOrCreatePendingEnrollment(directory, otherMember, memberships, enrollFlags{}); err == nil {
		t.Fatal("different member accepted for pending enrollment")
	}
}

func TestEnrollmentSpaceRequiresChoiceWhenSeveralAreAllowed(t *testing.T) {
	t.Parallel()

	memberships := []space.Membership{
		{SpaceID: "space-1", CanEnrollMachines: true},
		{SpaceID: "space-2", CanEnrollMachines: true},
	}
	if _, err := enrollmentSpace(memberships, ""); err == nil {
		t.Fatal("multiple enrollment Spaces accepted without --space")
	}
	selected, err := enrollmentSpace(memberships, "space-2")
	if err != nil {
		t.Fatalf("explicit Space rejected: %v", err)
	}
	if selected != "space-2" {
		t.Fatalf("selected Space = %q", selected)
	}
}
