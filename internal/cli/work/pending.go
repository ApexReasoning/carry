package work

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type pendingMutation struct {
	IdempotencyKey string `json:"idempotency_key"`
}

func pendingCreateIdentity(configDirectory string, spaceID string, goal string) (string, string, error) {
	return loadOrCreatePendingIdentity(configDirectory, []string{"create", spaceID, goal})
}

func pendingMessageIdentity(
	configDirectory string,
	spaceID string,
	workID string,
	text string,
) (string, string, error) {
	return loadOrCreatePendingIdentity(configDirectory, []string{"message", spaceID, workID, text})
}

func loadOrCreatePendingIdentity(configDirectory string, command []string) (string, string, error) {
	encodedCommand, err := json.Marshal(command)
	if err != nil {
		return "", "", fmt.Errorf("encode pending Work command identity: %w", err)
	}
	digest := sha256.Sum256(encodedCommand)
	directory := filepath.Join(configDirectory, "work-pending")
	path := filepath.Join(directory, hex.EncodeToString(digest[:])+".json")
	if identity, err := loadPendingIdentity(path); err == nil {
		return path, identity.IdempotencyKey, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", fmt.Errorf("create pending Work directory: %w", err)
	}

	identity := pendingMutation{IdempotencyKey: uuid.NewString()}
	encodedIdentity, err := json.Marshal(identity)
	if err != nil {
		return "", "", fmt.Errorf("encode pending Work identity: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".work-pending-*.json")
	if err != nil {
		return "", "", fmt.Errorf("create pending Work identity: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", "", fmt.Errorf("protect pending Work identity: %w", err)
	}
	if _, err := temporary.Write(encodedIdentity); err != nil {
		_ = temporary.Close()
		return "", "", fmt.Errorf("write pending Work identity: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", "", fmt.Errorf("sync pending Work identity: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", "", fmt.Errorf("close pending Work identity: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			winner, loadErr := loadPendingIdentity(path)
			if loadErr != nil {
				return "", "", loadErr
			}
			return path, winner.IdempotencyKey, nil
		}
		return "", "", fmt.Errorf("publish pending Work identity: %w", err)
	}
	if err := syncPendingDirectory(directory); err != nil {
		return "", "", err
	}
	return path, identity.IdempotencyKey, nil
}

func loadPendingIdentity(path string) (pendingMutation, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return pendingMutation{}, err
	}
	var identity pendingMutation
	if err := json.Unmarshal(encoded, &identity); err != nil {
		return pendingMutation{}, fmt.Errorf("decode pending Work identity: %w", err)
	}
	if uuid.Validate(identity.IdempotencyKey) != nil {
		return pendingMutation{}, errors.New("pending Work identity is invalid")
	}
	return identity, nil
}

func clearPendingIdentity(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pending Work identity: %w", err)
	}
	return syncPendingDirectory(filepath.Dir(path))
}

func syncPendingDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open pending Work directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync pending Work directory: %w", err)
	}
	return nil
}
