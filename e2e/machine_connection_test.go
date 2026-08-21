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

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMemberConnectsRunsDisconnectsAndBrowserRevokesMachine(t *testing.T) {
	databaseURL := os.Getenv("CARRY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("CARRY_TEST_DATABASE_URL is required")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	carryServer := filepath.Join(temporary, "carry-server")
	carry := filepath.Join(temporary, "carry")
	build(t, root, carryServer, "./cmd/carry-server")
	build(t, root, carry, "./cmd/carry")
	pkiDirectory := filepath.Join(temporary, "pki")
	caPath := filepath.Join(pkiDirectory, "ca.pem")
	run(t, root, nil, carryServer, "pki", "init", "--dir", pkiDirectory, "--hosts", "localhost,127.0.0.1")

	testMemberOutput := prepareTestMember(t, databaseURL)
	resetProductJourneyFacts(t, databaseURL)
	var member struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal([]byte(testMemberOutput), &member); err != nil {
		t.Fatal(err)
	}
	const memberEmail = "machine-member@example.com"
	attachTestEmailIdentity(t, databaseURL, member.UserID, memberEmail)

	address := freeAddress(t)
	serverURL := "https://" + address
	stopServer, serverLog, _ := startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	defer func() { stopServer() }()
	waitForServer(t, serverURL, caPath, serverLog)

	configDirectory := filepath.Join(temporary, "config")
	environment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	connectOutput := connectCarryMachine(t, root, carry, databaseURL, serverURL, caPath, configDirectory, member.UserID, member.SpaceID, "direct-machine")
	if !strings.Contains(connectOutput, "connected to Space") {
		t.Fatalf("connect output = %q", connectOutput)
	}
	machineJSON, err := os.ReadFile(filepath.Join(configDirectory, "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(machineJSON, &first); err != nil || first.MachineID == "" {
		t.Fatalf("first Machine credential = %#v, %v", first, err)
	}
	if info, err := os.Stat(filepath.Join(configDirectory, "machine.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Machine credential permissions = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(configDirectory, "machine-connection.json")); !os.IsNotExist(err) {
		t.Fatalf("pending proof remains after install: %v", err)
	}

	binDirectory := filepath.Join(temporary, "bin")
	if err := os.Mkdir(binDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, filepath.Join(binDirectory, "pi"))
	hostEnvironment := append(environment, "PATH="+binDirectory)
	if started := runHostUntilStarted(t, root, carry, hostEnvironment); !strings.Contains(started, first.MachineID) {
		t.Fatalf("Host start = %q", started)
	}
	stopServer()
	stopServer, serverLog, _ = startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	waitForServer(t, serverURL, caPath, serverLog)
	if restarted := runHostUntilStarted(t, root, carry, hostEnvironment); !strings.Contains(restarted, first.MachineID) {
		t.Fatalf("Host restart changed Machine = %q", restarted)
	}

	disconnect := run(t, root, environment, carry, "host", "disconnect")
	if !strings.Contains(disconnect, "confirmed Machine") || !strings.Contains(disconnect, "does not prove a process stopped") {
		t.Fatalf("disconnect output = %q", disconnect)
	}
	if _, err := os.Stat(filepath.Join(configDirectory, "machine.json")); !os.IsNotExist(err) {
		t.Fatalf("credential remains after confirmed disconnect: %v", err)
	}
	staleDirectory := filepath.Join(temporary, "stale")
	if err := os.Mkdir(staleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDirectory, "machine.json"), machineJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := runError(root, []string{"CARRY_CONFIG_DIR=" + staleDirectory, "PATH=" + binDirectory}, carry, "host", "start"); err == nil || !strings.Contains(output, "403 Forbidden") {
		t.Fatalf("revoked Machine authority = %q, %v", output, err)
	}

	connectCarryMachine(t, root, carry, databaseURL, serverURL, caPath, configDirectory, member.UserID, member.SpaceID, "replacement-machine")
	replacementJSON, err := os.ReadFile(filepath.Join(configDirectory, "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var replacement struct {
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(replacementJSON, &replacement); err != nil || replacement.MachineID == "" || replacement.MachineID == first.MachineID {
		t.Fatalf("fresh replacement = %#v, old=%q, %v", replacement, first.MachineID, err)
	}
	if started := runHostUntilStarted(t, root, carry, hostEnvironment); !strings.Contains(started, replacement.MachineID) {
		t.Fatalf("replacement Host start = %q", started)
	}
	localOnlyReplacement := run(t, root, environment, carry, "host", "disconnect", "--local-only")
	if !strings.Contains(localOnlyReplacement, "may still appear Active") {
		t.Fatalf("local-only replacement output = %q", localOnlyReplacement)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var replacementStillActive bool
	if err := pool.QueryRow(context.Background(), `select revoked_at is null from machines where machine_id=$1`, replacement.MachineID).Scan(&replacementStillActive); err != nil || !replacementStillActive {
		t.Fatalf("local-only changed remote Machine authority: active=%t err=%v", replacementStillActive, err)
	}

	// Run the real Browser review/inventory/revoke journey at the Web origin.
	stopServer()
	webAddress := freeAddress(t)
	webURL := "https://" + webAddress
	stopServer, serverLog, emailCaptureFile := startServerWithOrigin(t, root, carryServer, address, databaseURL, pkiDirectory, webURL)
	waitForServer(t, serverURL, caPath, serverLog)
	run(t, root, nil, "pnpm", "--dir", "apps/web", "build")
	stopWeb, webLog := startWeb(t, root, webAddress, serverURL, pkiDirectory)
	defer stopWeb()
	waitForServer(t, webURL, caPath, webLog)
	browserConfig := filepath.Join(temporary, "browser-config")
	pending := startCarryMachineConnection(t, root, carry, browserConfig, webURL, caPath, "browser-reviewed-machine")
	playwrightOutput, playwrightErr := runError(root, []string{
		"CARRY_WEB_URL=" + webURL,
		"CARRY_EMAIL_CAPTURE_FILE=" + emailCaptureFile,
		"CARRY_LOGIN_EMAIL=" + memberEmail,
		"CARRY_MACHINE_CODE=" + pending.code,
		"CARRY_MACHINE_FINGERPRINT=" + pending.fingerprint,
		"CARRY_MACHINE_NAME=browser-reviewed-machine",
		"CARRY_MACHINE_SERVER_ORIGIN=" + webURL,
		"CARRY_MACHINE_SPACE_NAME=Carry Space",
	}, "pnpm", "--dir", "apps/web", "exec", "playwright", "test", "e2e/machine-connection.spec.ts")
	if playwrightErr != nil || !strings.Contains(playwrightOutput, "1 passed") {
		t.Fatalf("Machine Browser journey: %v\n%s\nCLI:\n%s", playwrightErr, playwrightOutput, pending.log.String())
	}
	finishCarryMachineConnection(t, pending)
	localOnly := run(t, root, []string{"CARRY_CONFIG_DIR=" + browserConfig}, carry, "host", "disconnect", "--local-only")
	if !strings.Contains(localOnly, "may still appear Active") {
		t.Fatalf("local-only output = %q", localOnly)
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
