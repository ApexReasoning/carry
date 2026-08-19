package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
)

type bootstrapCredentialWire struct {
	DisplayName    string    `json:"display_name"`
	SpaceName      string    `json:"space_name"`
	TokenExpiresAt time.Time `json:"token_expires_at"`
	UserID         string    `json:"user_id"`
	SpaceID        string    `json:"space_id"`
	TokenID        string    `json:"token_id"`
	UserToken      string    `json:"user_token"`
}

func loadOrCreateBootstrapCredential(path string, displayName string, spaceName string, expiresAt time.Time) (carrypostgres.BootstrapCommand, error) {
	displayName = strings.TrimSpace(displayName)
	spaceName = strings.TrimSpace(spaceName)
	if command, err := loadBootstrapCredential(path); err == nil {
		return bootstrapCredentialForNames(command, displayName, spaceName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return carrypostgres.BootstrapCommand{}, err
	}
	command, err := carrypostgres.PrepareBootstrap(displayName, spaceName, expiresAt)
	if err != nil {
		return carrypostgres.BootstrapCommand{}, err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("create bootstrap credential directory: %w", err)
	}
	encoded, err := json.MarshalIndent(bootstrapCredentialFromCommand(command), "", "  ")
	if err != nil {
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("encode bootstrap credential: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".bootstrap-credential-*.json")
	if err != nil {
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("create bootstrap credential: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("protect bootstrap credential: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("write bootstrap credential: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("sync bootstrap credential: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("close bootstrap credential: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			winner, loadErr := loadBootstrapCredential(path)
			if loadErr != nil {
				return carrypostgres.BootstrapCommand{}, loadErr
			}
			return bootstrapCredentialForNames(winner, displayName, spaceName)
		}
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("publish bootstrap credential: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return carrypostgres.BootstrapCommand{}, err
	}
	return command, nil
}

func bootstrapCredentialForNames(
	command carrypostgres.BootstrapCommand,
	displayName string,
	spaceName string,
) (carrypostgres.BootstrapCommand, error) {
	if command.DisplayName != displayName || command.SpaceName != spaceName {
		return carrypostgres.BootstrapCommand{}, errors.New("bootstrap credential belongs to different initial names")
	}
	return command, nil
}

func loadBootstrapCredential(path string) (carrypostgres.BootstrapCommand, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return carrypostgres.BootstrapCommand{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return carrypostgres.BootstrapCommand{}, errors.New("bootstrap credential must be a regular mode-0600 file")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return carrypostgres.BootstrapCommand{}, err
	}
	var wire bootstrapCredentialWire
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return carrypostgres.BootstrapCommand{}, fmt.Errorf("decode bootstrap credential: %w", err)
	}
	return carrypostgres.BootstrapCommand{
		DisplayName: wire.DisplayName, SpaceName: wire.SpaceName, TokenExpiresAt: wire.TokenExpiresAt,
		UserID: wire.UserID, SpaceID: wire.SpaceID, TokenID: wire.TokenID, UserToken: wire.UserToken,
	}, nil
}

func bootstrapCredentialFromCommand(command carrypostgres.BootstrapCommand) bootstrapCredentialWire {
	return bootstrapCredentialWire{
		DisplayName: command.DisplayName, SpaceName: command.SpaceName, TokenExpiresAt: command.TokenExpiresAt,
		UserID: command.UserID, SpaceID: command.SpaceID, TokenID: command.TokenID, UserToken: command.UserToken,
	}
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open bootstrap credential directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync bootstrap credential directory: %w", err)
	}
	return nil
}
