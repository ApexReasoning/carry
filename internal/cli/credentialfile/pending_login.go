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

// PendingLogin retains only the short-lived poll proof needed to recover an
// already-redeemed credential after response loss or local publication failure.
type PendingLogin struct {
	ServerURL                       string    `json:"server_url"`
	CACertificatePEM                string    `json:"ca_certificate_pem,omitempty"`
	RequestID                       string    `json:"request_id"`
	UserCode                        string    `json:"user_code"`
	PollSecret                      string    `json:"poll_secret"`
	VerificationPath                string    `json:"verification_path"`
	Label                           string    `json:"label"`
	ProposedReplacementCredentialID string    `json:"proposed_replacement_credential_id,omitempty"`
	ExpiresAt                       time.Time `json:"expires_at"`
	IntervalSeconds                 int       `json:"interval_seconds"`
}

func SavePending(directory string, pending PendingLogin) error {
	if err := validatePending(pending); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pending CLI login: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".cli-login-*.json")
	if err != nil {
		return fmt.Errorf("create pending CLI login: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect pending CLI login: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write pending CLI login: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync pending CLI login: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close pending CLI login: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, "cli-login.json")); err != nil {
		return fmt.Errorf("publish pending CLI login: %w", err)
	}
	return syncDirectory(directory)
}

func LoadPending(directory string) (PendingLogin, error) {
	path := filepath.Join(directory, "cli-login.json")
	info, err := os.Lstat(path)
	if err != nil {
		return PendingLogin{}, fmt.Errorf("read pending CLI login: %w", err)
	}
	if err := inspectPrivateDirectory(directory); err != nil {
		return PendingLogin{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return PendingLogin{}, ErrUnsafeCredential
	}
	file, err := os.Open(path)
	if err != nil {
		return PendingLogin{}, fmt.Errorf("open pending CLI login: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var pending PendingLogin
	if err := decoder.Decode(&pending); err != nil {
		return PendingLogin{}, fmt.Errorf("decode pending CLI login: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return PendingLogin{}, errors.New("decode pending CLI login: expected one JSON value")
	}
	if err := validatePending(pending); err != nil {
		return PendingLogin{}, err
	}
	return pending, nil
}

func RemovePending(directory string) error {
	if err := os.Remove(filepath.Join(directory, "cli-login.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pending CLI login: %w", err)
	}
	return syncDirectory(directory)
}

func validatePending(pending PendingLogin) error {
	code := strings.NewReplacer("-", "", " ", "").Replace(strings.ToUpper(strings.TrimSpace(pending.UserCode)))
	validCode := len(code) == 10
	for _, character := range code {
		validCode = validCode && strings.ContainsRune("BCDFGHJKLMNPQRSTVWXZ", character)
	}
	if !validCode || !strings.HasPrefix(pending.ServerURL, "https://") || strings.Contains(strings.TrimPrefix(pending.ServerURL, "https://"), "/") ||
		uuid.Validate(pending.RequestID) != nil || pending.VerificationPath != "/cli-login" ||
		strings.TrimSpace(pending.Label) == "" || len([]byte(pending.Label)) > 128 || pending.ExpiresAt.IsZero() ||
		pending.IntervalSeconds < 5 || pending.IntervalSeconds > 30 ||
		(pending.ProposedReplacementCredentialID != "" && uuid.Validate(pending.ProposedReplacementCredentialID) != nil) {
		return errors.New("pending CLI login content is invalid")
	}
	parts := strings.Split(strings.TrimPrefix(pending.PollSecret, "carry_cli_poll_"), ".")
	if !strings.HasPrefix(pending.PollSecret, "carry_cli_poll_") || len(parts) != 2 || parts[0] != pending.RequestID {
		return errors.New("pending CLI login poll proof is invalid")
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(mac) != 32 {
		return errors.New("pending CLI login poll proof is invalid")
	}
	return nil
}
