//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostAdvancesWorkThroughNativeExecutionContract(t *testing.T) {
	databaseURL := os.Getenv("CARRY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("CARRY_TEST_DATABASE_URL is required")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	temporary := t.TempDir()
	carryServer := filepath.Join(temporary, "carry-server")
	carry := filepath.Join(temporary, "carry")
	build(t, root, carryServer, "./cmd/carry-server")
	build(t, root, carry, "./cmd/carry")

	pkiDirectory := filepath.Join(temporary, "pki")
	run(t, root, nil, carryServer, "pki", "init", "--dir", pkiDirectory, "--hosts", "localhost,127.0.0.1")
	bootstrapOutput := bootstrapCarry(t, root, carryServer, databaseURL)
	var bootstrap struct {
		SpaceID   string `json:"space_id"`
		UserToken string `json:"user_token"`
	}
	if err := json.Unmarshal([]byte(bootstrapOutput), &bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}

	address := freeAddress(t)
	stopServer, serverLog := startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	defer stopServer()
	serverURL := "https://" + address
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)

	configDirectory := filepath.Join(temporary, "config")
	clientEnvironment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	run(
		t, root, clientEnvironment, carry, "login",
		"--server", serverURL,
		"--ca-cert", filepath.Join(pkiDirectory, "ca.pem"),
		"--token", bootstrap.UserToken,
	)
	run(
		t, root, clientEnvironment, carry, "host", "enroll",
		"--space", bootstrap.SpaceID,
		"--name", "node-two-host",
	)
	createdOutput := run(
		t,
		root,
		clientEnvironment,
		carry,
		"work",
		"create",
		"--goal",
		"Prepare a customer renewal recommendation",
	)
	createdFields := strings.Fields(strings.SplitN(createdOutput, "\n", 2)[0])
	if len(createdFields) != 3 {
		t.Fatalf("create output = %q", createdOutput)
	}
	workID := createdFields[2]
	run(
		t,
		root,
		clientEnvironment,
		carry,
		"work",
		"message",
		workID,
		"--text",
		"Finance approved a twelve month term",
	)

	binDirectory := filepath.Join(temporary, "bin")
	if err := os.Mkdir(binDirectory, 0o700); err != nil {
		t.Fatalf("create fake Agent bin directory: %v", err)
	}
	promptPath := filepath.Join(temporary, "pi-prompt.json")
	writeFakePi(t, filepath.Join(binDirectory, "pi"))
	hostEnvironment := append(clientEnvironment,
		"PATH="+binDirectory,
		"CARRY_FAKE_PI_PROMPT="+promptPath,
	)
	hostCtx, cancelHost := context.WithCancel(t.Context())
	hostLog := &lockedBuffer{}
	hostCommand := exec.CommandContext(hostCtx, carry, "host", "start", "--interval", "1s")
	hostCommand.Dir = root
	hostCommand.Env = append(os.Environ(), hostEnvironment...)
	hostCommand.Stdout = hostLog
	hostCommand.Stderr = hostLog
	if err := hostCommand.Start(); err != nil {
		cancelHost()
		t.Fatalf("start Carry Host: %v", err)
	}
	defer func() {
		cancelHost()
		if err := hostCommand.Wait(); err != nil && hostCtx.Err() == nil {
			t.Errorf("wait for Carry Host: %v\n%s", err, hostLog.String())
		}
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		shown, showErr := runError(root, clientEnvironment, carry, "work", "show", workID)
		if showErr == nil && strings.Contains(shown, "Current understanding: Finance approved a twelve month term") &&
			strings.Contains(shown, "Next step: Prepare the renewal recommendation") {
			prompt, err := os.ReadFile(promptPath)
			if err != nil {
				t.Fatalf("read fake Pi prompt: %v", err)
			}
			if strings.Contains(string(prompt), "carry_agent_") || strings.Contains(string(prompt), "writer_token") {
				t.Fatalf("Agent prompt leaked authority: %s", prompt)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Work was not advanced before deadline\nHost log:\n%s\nServer log:\n%s", hostLog.String(), serverLog.String())
}

func writeFakePi(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' '0.84.2'
  exit 0
fi
IFS= read -r prompt
printf '%s' "$prompt" > "$CARRY_FAKE_PI_PROMPT"
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Finance approved a twelve month term.\",\"next_step\":\"Prepare the renewal recommendation.\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake Pi executable: %v", err)
	}
}
