package work

import (
	"os"
	"testing"
)

func TestPendingWorkIdentitySurvivesProcessReconstruction(t *testing.T) {
	directory := t.TempDir()
	path, first, err := pendingCreateIdentity(directory, "space-1", "Review renewals")
	if err != nil {
		t.Fatalf("create pending identity: %v", err)
	}
	secondPath, second, err := pendingCreateIdentity(directory, "space-1", "Review renewals")
	if err != nil {
		t.Fatalf("reload pending identity: %v", err)
	}
	if secondPath != path || second != first {
		t.Fatalf("reloaded identity = (%q, %q), want (%q, %q)", secondPath, second, path, first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat pending identity: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pending identity mode = %o, want 600", info.Mode().Perm())
	}

	_, changed, err := pendingCreateIdentity(directory, "space-1", "Review expansion")
	if err != nil {
		t.Fatalf("create changed identity: %v", err)
	}
	if changed == first {
		t.Fatal("different Work command reused an idempotency identity")
	}

	if err := clearPendingIdentity(path); err != nil {
		t.Fatalf("clear pending identity: %v", err)
	}
	_, afterSuccess, err := pendingCreateIdentity(directory, "space-1", "Review renewals")
	if err != nil {
		t.Fatalf("recreate identity after success: %v", err)
	}
	if afterSuccess == first {
		t.Fatal("completed Work command retained its old idempotency identity")
	}
}

func TestPendingMessageIdentityIncludesUnmodifiedTextAndTarget(t *testing.T) {
	directory := t.TempDir()
	_, first, err := pendingMessageIdentity(directory, "space-1", "work-1", "  keep spacing  ")
	if err != nil {
		t.Fatalf("create pending Message identity: %v", err)
	}
	_, same, err := pendingMessageIdentity(directory, "space-1", "work-1", "  keep spacing  ")
	if err != nil {
		t.Fatalf("reload pending Message identity: %v", err)
	}
	_, trimmed, err := pendingMessageIdentity(directory, "space-1", "work-1", "keep spacing")
	if err != nil {
		t.Fatalf("create trimmed Message identity: %v", err)
	}
	_, otherWork, err := pendingMessageIdentity(directory, "space-1", "work-2", "  keep spacing  ")
	if err != nil {
		t.Fatalf("create other Work Message identity: %v", err)
	}
	if same != first {
		t.Fatal("same Message command did not reuse its identity")
	}
	if trimmed == first || otherWork == first {
		t.Fatal("different Message command reused an idempotency identity")
	}
}
