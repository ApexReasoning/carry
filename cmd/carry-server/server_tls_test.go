package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadServerTLSRejectsUnsafePrivateKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix private-key permissions and symlinks are not portable to Windows")
	}

	for _, test := range []struct {
		name    string
		keyName string
		unsafe  func(t *testing.T, path string)
	}{
		{
			name:    "permissive CA key",
			keyName: "ca-key.pem",
			unsafe: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatalf("make CA key permissive: %v", err)
				}
			},
		},
		{
			name:    "permissive server key",
			keyName: "server-key.pem",
			unsafe: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatalf("make server key permissive: %v", err)
				}
			},
		},
		{
			name:    "symlinked CA key",
			keyName: "ca-key.pem",
			unsafe:  replaceWithSymlink,
		},
		{
			name:    "symlinked server key",
			keyName: "server-key.pem",
			unsafe:  replaceWithSymlink,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "pki")
			if err := initializePKI(pkiInitConfig{directory: directory, hosts: "localhost,127.0.0.1"}); err != nil {
				t.Fatalf("initialize PKI: %v", err)
			}
			test.unsafe(t, filepath.Join(directory, test.keyName))
			_, _, err := loadServerTLS(
				filepath.Join(directory, "ca.pem"),
				filepath.Join(directory, "ca-key.pem"),
				filepath.Join(directory, "server.pem"),
				filepath.Join(directory, "server-key.pem"),
			)
			if err == nil {
				t.Fatal("unsafe private key was accepted")
			}
		})
	}
}

func TestPKIWritesStayInVerifiedDirectoryAfterPathReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory replacement semantics differ on Windows")
	}
	parent := t.TempDir()
	path := filepath.Join(parent, "pki")
	root, err := openPrivatePKIDirectory(path)
	if err != nil {
		t.Fatalf("open private PKI directory: %v", err)
	}
	defer root.Close()
	heldPath := filepath.Join(parent, "verified-pki")
	if err := os.Rename(path, heldPath); err != nil {
		t.Fatalf("move verified PKI directory: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("replace PKI path: %v", err)
	}

	if err := writeExclusive(root, "ca-key.pem", []byte("private"), 0o600); err != nil {
		t.Fatalf("write through verified root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "ca-key.pem")); !os.IsNotExist(err) {
		t.Fatalf("replacement directory received private key: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(heldPath, "ca-key.pem"))
	if err != nil || string(written) != "private" {
		t.Fatalf("verified directory key = %q, %v", written, err)
	}
}

func replaceWithSymlink(t *testing.T, path string) {
	t.Helper()
	target := path + ".target"
	if err := os.Rename(path, target); err != nil {
		t.Fatalf("move private key behind symlink: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink private key: %v", err)
	}
}
