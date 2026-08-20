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

func TestMemberEnrollsAndRevokesIndependentMachine(t *testing.T) {
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
	resetProductJourneyFacts(t, databaseURL)
	var bootstrap struct {
		SpaceID   string `json:"space_id"`
		UserToken string `json:"user_token"`
	}
	if err := json.Unmarshal([]byte(bootstrapOutput), &bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}

	address := freeAddress(t)
	stopServer, serverLog := startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	defer func() { stopServer() }()

	serverURL := "https://" + address
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)
	configDirectory := filepath.Join(temporary, "config")
	clientEnvironment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	run(t, root, clientEnvironment, carry, "login",
		"--server", serverURL,
		"--ca-cert", filepath.Join(pkiDirectory, "ca.pem"),
		"--token", bootstrap.UserToken,
	)
	memberInfo, err := os.Stat(filepath.Join(configDirectory, "member.json"))
	if err != nil {
		t.Fatalf("stat member credential: %v", err)
	}
	if got := memberInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("member credential mode = %o, want 600", got)
	}
	enrollOutput := run(t, root, clientEnvironment, carry, "host", "enroll",
		"--space", bootstrap.SpaceID, "--name", "machine-enrollment-host")
	if !strings.Contains(enrollOutput, "Enrolled Machine") {
		t.Fatalf("enrollment output = %q", enrollOutput)
	}
	machineInfo, err := os.Stat(filepath.Join(configDirectory, "machine.json"))
	if err != nil {
		t.Fatalf("stat Machine credential: %v", err)
	}
	if got := machineInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("Machine credential mode = %o, want 600", got)
	}
	if _, err := os.Stat(filepath.Join(configDirectory, "machine-enrollment.json")); !os.IsNotExist(err) {
		t.Fatalf("pending Machine enrollment remains after success: %v", err)
	}

	var machineCredential struct {
		MachineID string `json:"machine_id"`
	}
	machineJSON, err := os.ReadFile(filepath.Join(configDirectory, "machine.json"))
	if err != nil {
		t.Fatalf("read Machine credential: %v", err)
	}
	if err := json.Unmarshal(machineJSON, &machineCredential); err != nil {
		t.Fatalf("decode Machine credential: %v", err)
	}
	binDirectory := filepath.Join(temporary, "bin")
	if err := os.Mkdir(binDirectory, 0o700); err != nil {
		t.Fatalf("create fake Agent bin directory: %v", err)
	}
	writeFakePi(t, filepath.Join(binDirectory, "pi"))
	hostEnvironment := append(clientEnvironment, "PATH="+binDirectory)

	memberPath := filepath.Join(configDirectory, "member.json")
	memberCredential, err := os.ReadFile(memberPath)
	if err != nil {
		t.Fatalf("read member credential: %v", err)
	}
	if err := os.Remove(memberPath); err != nil {
		t.Fatalf("remove member credential: %v", err)
	}
	started := runHostUntilStarted(t, root, carry, hostEnvironment)
	if !strings.Contains(started, machineCredential.MachineID) || !strings.Contains(started, "with Pi") {
		t.Fatalf("Host start output = %q", started)
	}

	stopServer()
	stopServer, serverLog = startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)
	restarted := runHostUntilStarted(t, root, carry, hostEnvironment)
	if !strings.Contains(restarted, machineCredential.MachineID) {
		t.Fatalf("Machine identity changed after server restart: %q", restarted)
	}

	if err := os.WriteFile(memberPath, memberCredential, 0o600); err != nil {
		t.Fatalf("restore member credential: %v", err)
	}
	revokeOutput := run(t, root, clientEnvironment, carry, "host", "revoke")
	if !strings.Contains(revokeOutput, "removed its local credential") {
		t.Fatalf("revoke output = %q", revokeOutput)
	}
	if _, err := os.Stat(filepath.Join(configDirectory, "machine.json")); !os.IsNotExist(err) {
		t.Fatalf("revoked Machine credential remains active: %v", err)
	}

	if err := os.WriteFile(filepath.Join(configDirectory, "machine-revoked.json"), machineJSON, 0o600); err != nil {
		t.Fatalf("restore confirmed-revoked Machine credential: %v", err)
	}
	if err := os.Remove(memberPath); err != nil {
		t.Fatalf("remove member credential before cleanup retry: %v", err)
	}
	cleanupOutput := run(t, root, clientEnvironment, carry, "host", "revoke")
	if !strings.Contains(cleanupOutput, "removed its local credential") {
		t.Fatalf("cleanup retry output = %q", cleanupOutput)
	}
	if _, err := os.Stat(filepath.Join(configDirectory, "machine-revoked.json")); !os.IsNotExist(err) {
		t.Fatalf("confirmed-revoked Machine credential remains after cleanup retry: %v", err)
	}
	if err := os.WriteFile(memberPath, memberCredential, 0o600); err != nil {
		t.Fatalf("restore member credential after cleanup retry: %v", err)
	}

	staleConfigDirectory := filepath.Join(temporary, "stale-revoked-config")
	if err := os.MkdirAll(staleConfigDirectory, 0o700); err != nil {
		t.Fatalf("create stale Machine config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleConfigDirectory, "machine.json"), machineJSON, 0o600); err != nil {
		t.Fatalf("restore stale Machine credential: %v", err)
	}
	staleEnvironment := []string{
		"CARRY_CONFIG_DIR=" + staleConfigDirectory,
		"PATH=" + binDirectory,
	}
	if output, err := runError(root, staleEnvironment, carry, "host", "start"); err == nil || !strings.Contains(output, "403 Forbidden") {
		t.Fatalf("revoked Host output = %q, error = %v", output, err)
	}

	reenrollOutput := run(t, root, clientEnvironment, carry, "host", "enroll",
		"--space", bootstrap.SpaceID, "--name", "replacement-enrollment-host")
	if !strings.Contains(reenrollOutput, "Enrolled Machine") {
		t.Fatalf("re-enrollment output = %q", reenrollOutput)
	}
	replacementJSON, err := os.ReadFile(filepath.Join(configDirectory, "machine.json"))
	if err != nil {
		t.Fatalf("read replacement Machine credential: %v", err)
	}
	var replacementCredential struct {
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(replacementJSON, &replacementCredential); err != nil {
		t.Fatalf("decode replacement Machine credential: %v", err)
	}
	if replacementCredential.MachineID == "" || replacementCredential.MachineID == machineCredential.MachineID {
		t.Fatalf("replacement Machine identity = %q, old = %q", replacementCredential.MachineID, machineCredential.MachineID)
	}
	if started := runHostUntilStarted(t, root, carry, hostEnvironment); !strings.Contains(started, replacementCredential.MachineID) {
		t.Fatalf("replacement Host start output = %q", started)
	}
}

func runHostUntilStarted(t *testing.T, root string, carry string, environment []string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	command := exec.CommandContext(ctx, carry, "host", "start")
	command.Dir = root
	command.Env = append(os.Environ(), environment...)
	log := &lockedBuffer{}
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start Carry Host: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if !strings.Contains(log.String(), "Started Carry Host") {
				continue
			}
			select {
			case err := <-done:
				cancel()
				t.Fatalf("Carry Host exited after start: %v\n%s", err, log.String())
			case <-time.After(200 * time.Millisecond):
				cancel()
				<-done
				return log.String()
			}
		case <-timeout.C:
			cancel()
			<-done
			t.Fatalf("Carry Host did not start before deadline\n%s", log.String())
			return ""
		}
	}
}
