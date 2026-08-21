package credentialfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCredentialFileIsPrivateStrictAndAtomic(t *testing.T) {
	directory := t.TempDir()
	credential := testCredential()
	if err := Save(directory, credential); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	info, err := os.Stat(filepath.Join(directory, "cli.json"))
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}
	loaded, err := Load(directory)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if loaded.Credential != credential.Credential || loaded.DefaultSpaceID != credential.DefaultSpaceID {
		t.Fatalf("loaded credential = %#v", loaded)
	}
	if err := os.WriteFile(filepath.Join(directory, "cli.json"), []byte(`{"server_url":"https://carry.example","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directory); err == nil {
		t.Fatal("unknown credential field accepted")
	}
}

func TestCredentialFileRejectsSymlinkAndPermissiveMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode and symlink evidence")
	}
	directory := t.TempDir()
	targetDirectory := t.TempDir()
	if err := Save(targetDirectory, testCredential()); err != nil {
		t.Fatalf("save target: %v", err)
	}
	if err := os.Symlink(filepath.Join(targetDirectory, "cli.json"), filepath.Join(directory, "cli.json")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := Load(directory); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("symlink error = %v", err)
	}
	if err := os.Remove(filepath.Join(directory, "cli.json")); err != nil {
		t.Fatal(err)
	}
	if err := Save(directory, testCredential()); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := os.Chmod(filepath.Join(directory, "cli.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directory); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("permissive mode error = %v", err)
	}
}

func TestPendingLoginIsPrivateStrictAndRemovable(t *testing.T) {
	directory := t.TempDir()
	pending := PendingLogin{
		ServerURL: "https://carry.example", RequestID: "44444444-4444-4444-8444-444444444444",
		UserCode:         "BCDF-GHJ-KLM",
		PollSecret:       "carry_cli_poll_44444444-4444-4444-8444-444444444444.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		VerificationPath: "/cli-login", Label: "Desk CLI", ExpiresAt: time.Now().Add(time.Hour), IntervalSeconds: 5,
	}
	if err := SavePending(directory, pending); err != nil {
		t.Fatalf("save pending login: %v", err)
	}
	loaded, err := LoadPending(directory)
	if err != nil || loaded.PollSecret != pending.PollSecret {
		t.Fatalf("load pending login = %#v, %v", loaded, err)
	}
	if err := RemovePending(directory); err != nil {
		t.Fatalf("remove pending login: %v", err)
	}
	if _, err := LoadPending(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending login remains: %v", err)
	}
}

func testCredential() Credential {
	return Credential{
		ServerURL: "https://carry.example", Credential: "carry_cli_11111111-1111-4111-8111-111111111111.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		CredentialID: "11111111-1111-4111-8111-111111111111", UserID: "22222222-2222-4222-8222-222222222222",
		DefaultSpaceID: "33333333-3333-4333-8333-333333333333", Label: "Desk CLI", ExpiresAt: time.Now().Add(time.Hour),
	}
}
