package machinefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmedRevocationRetiresThenRemovesMachineCredential(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	credential := Credential{MachineID: "machine-1", SpaceID: "space-1", PrivateKeyPEM: "private"}
	if err := Save(directory, credential); err != nil {
		t.Fatalf("save Machine credential: %v", err)
	}
	loaded, confirmed, err := LoadForRevocation(directory)
	if err != nil || confirmed || loaded.MachineID != credential.MachineID {
		t.Fatalf("active revocation credential = %#v, confirmed = %t, error = %v", loaded, confirmed, err)
	}
	if err := MarkRevoked(directory); err != nil {
		t.Fatalf("mark Machine revoked: %v", err)
	}
	if _, err := Load(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active Machine credential remains: %v", err)
	}
	loaded, confirmed, err = LoadForRevocation(directory)
	if err != nil || !confirmed || loaded.MachineID != credential.MachineID {
		t.Fatalf("retired revocation credential = %#v, confirmed = %t, error = %v", loaded, confirmed, err)
	}
	if err := RemoveRevoked(directory); err != nil {
		t.Fatalf("remove revoked Machine credential: %v", err)
	}
	if _, _, err := LoadForRevocation(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("revoked Machine credential remains: %v", err)
	}
}

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
