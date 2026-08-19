package machinefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPendingEnrollmentIsProtectedAndRemoved(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	pending := PendingEnrollment{
		ServerURL: "https://carry.example.com", SpaceID: "space-1",
		IdempotencyKey: "request-1", PublicKeyDER: []byte("public"), PrivateKeyPEM: "private",
	}
	if err := SavePending(directory, pending); err != nil {
		t.Fatalf("save pending enrollment: %v", err)
	}
	path := filepath.Join(directory, "machine-enrollment.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat pending enrollment: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("pending enrollment mode = %o, want 600", got)
	}
	loaded, err := LoadPending(directory)
	if err != nil {
		t.Fatalf("load pending enrollment: %v", err)
	}
	if loaded.IdempotencyKey != pending.IdempotencyKey || loaded.PrivateKeyPEM != pending.PrivateKeyPEM {
		t.Fatalf("loaded pending enrollment = %#v", loaded)
	}
	if err := RemovePending(directory); err != nil {
		t.Fatalf("remove pending enrollment: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pending enrollment still exists: %v", err)
	}
}
