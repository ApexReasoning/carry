package credentialfile

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrUnsafeCredential = errors.New("CLI credential file is not a private regular file")

type Credential struct {
	ServerURL            string    `json:"server_url"`
	CACertificatePEM     string    `json:"ca_certificate_pem,omitempty"`
	Credential           string    `json:"credential"`
	CredentialID         string    `json:"credential_id"`
	UserID               string    `json:"user_id"`
	DefaultSpaceID       string    `json:"default_space_id"`
	Label                string    `json:"label"`
	ExpiresAt            time.Time `json:"expires_at"`
	LogoutIdempotencyKey string    `json:"logout_idempotency_key,omitempty"`
}

func Save(directory string, credential Credential) error {
	if err := validate(credential); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(credential, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI credential: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".cli-*.json")
	if err != nil {
		return fmt.Errorf("create CLI credential: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect CLI credential: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write CLI credential: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync CLI credential: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close CLI credential: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, "cli.json")); err != nil {
		return fmt.Errorf("publish CLI credential: %w", err)
	}
	return syncDirectory(directory)
}

func Load(directory string) (Credential, error) {
	path := filepath.Join(directory, "cli.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Credential{}, fmt.Errorf("read CLI credential: %w", err)
	}
	if err != nil {
		return Credential{}, fmt.Errorf("inspect CLI credential: %w", err)
	}
	if err := inspectPrivateDirectory(directory); err != nil {
		return Credential{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return Credential{}, ErrUnsafeCredential
	}
	file, err := os.Open(path)
	if err != nil {
		return Credential{}, fmt.Errorf("open CLI credential: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var credential Credential
	if err := decoder.Decode(&credential); err != nil {
		return Credential{}, fmt.Errorf("decode CLI credential: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Credential{}, errors.New("decode CLI credential: expected one JSON value")
	}
	if err := validate(credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func Remove(directory string) error {
	path := filepath.Join(directory, "cli.json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove CLI credential: %w", err)
	}
	return syncDirectory(directory)
}

func validate(credential Credential) error {
	if !strings.HasPrefix(credential.ServerURL, "https://") || strings.Contains(strings.TrimPrefix(credential.ServerURL, "https://"), "/") ||
		!validFinalCredential(credential.Credential, credential.CredentialID) || uuid.Validate(credential.CredentialID) != nil ||
		uuid.Validate(credential.UserID) != nil || uuid.Validate(credential.DefaultSpaceID) != nil ||
		strings.TrimSpace(credential.Label) == "" || len([]byte(credential.Label)) > 128 || credential.ExpiresAt.IsZero() ||
		(credential.LogoutIdempotencyKey != "" && uuid.Validate(credential.LogoutIdempotencyKey) != nil) {
		return errors.New("CLI credential content is invalid")
	}
	return nil
}

func validFinalCredential(value string, credentialID string) bool {
	if !strings.HasPrefix(value, "carry_cli_") || strings.HasPrefix(value, "carry_cli_poll_") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "carry_cli_"), ".")
	if len(parts) != 2 || parts[0] != credentialID || uuid.Validate(parts[0]) != nil {
		return false
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	return err == nil && len(mac) == 32
}

func inspectPrivateDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect CLI credential directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return ErrUnsafeCredential
	}
	return nil
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create CLI credential directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect CLI credential directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("CLI credential directory is not private")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("protect CLI credential directory: %w", err)
		}
	}
	return nil
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open CLI credential directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync CLI credential directory: %w", err)
	}
	return nil
}
