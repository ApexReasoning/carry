package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigDirectory returns the local directory shared by member and Machine
// credential files; the files themselves remain separate principals.
func ConfigDirectory() string {
	if configured := strings.TrimSpace(os.Getenv("CARRY_CONFIG_DIR")); configured != "" {
		return configured
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", ".carry")
	}
	return filepath.Join(directory, "carry")
}
