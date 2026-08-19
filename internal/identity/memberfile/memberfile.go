package memberfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ApexReasoning/carry/internal/space"
)

// Credential is the local member principal used only by member-authorized CLI paths.
type Credential struct {
	ServerURL        string             `json:"server_url"`
	Token            string             `json:"token"`
	CACertificatePEM string             `json:"ca_certificate_pem"`
	UserID           string             `json:"user_id"`
	Spaces           []space.Membership `json:"spaces"`
}

// Save atomically publishes a mode-0600 member credential.
func Save(directory string, credential Credential) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create member credential directory: %w", err)
	}
	encoded, err := json.MarshalIndent(credential, "", "  ")
	if err != nil {
		return fmt.Errorf("encode member credential: %w", err)
	}
	path := filepath.Join(directory, "member.json")
	temporary, err := os.CreateTemp(directory, ".member-*.json")
	if err != nil {
		return fmt.Errorf("create member credential: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect member credential: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write member credential: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync member credential: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close member credential: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish member credential: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open member credential directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync member credential directory: %w", err)
	}
	return nil
}

// Load reads the local member credential.
func Load(directory string) (Credential, error) {
	path := filepath.Join(directory, "member.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Credential{}, fmt.Errorf("read member credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal(encoded, &credential); err != nil {
		return Credential{}, fmt.Errorf("decode member credential: %w", err)
	}
	return credential, nil
}
