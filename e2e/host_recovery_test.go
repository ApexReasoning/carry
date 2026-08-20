//go:build integration

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
)

func TestInterruptedHostWorkContinuesWithNewAttempt(t *testing.T) {
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
	defer stopServer()
	serverURL := "https://" + address
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)

	configDirectory := filepath.Join(temporary, "config")
	clientEnvironment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	run(t, root, clientEnvironment, carry, "login",
		"--server", serverURL,
		"--ca-cert", filepath.Join(pkiDirectory, "ca.pem"),
		"--token", bootstrap.UserToken,
	)
	run(t, root, clientEnvironment, carry, "host", "enroll",
		"--space", bootstrap.SpaceID, "--name", "recovery-host",
	)
	created := run(t, root, clientEnvironment, carry, "work", "create",
		"--goal", "Recover this Work after its Host disappears",
	)
	createdFields := strings.Fields(strings.SplitN(created, "\n", 2)[0])
	if len(createdFields) != 3 {
		t.Fatalf("create output = %q", created)
	}
	workID := createdFields[2]

	binDirectory := filepath.Join(temporary, "bin")
	if err := os.Mkdir(binDirectory, 0o700); err != nil {
		t.Fatalf("create fake Agent bin directory: %v", err)
	}
	writeRecoveryPi(t, filepath.Join(binDirectory, "pi"))
	firstStarted := filepath.Join(temporary, "first-attempt-started")
	firstSelected := filepath.Join(temporary, "first-attempt-selected")
	firstPrompt := filepath.Join(temporary, "first-attempt-prompt.json")
	secondPrompt := filepath.Join(temporary, "second-attempt-prompt.json")
	terminalSelected := filepath.Join(temporary, "terminal-attempt-selected")
	agentEnvironment := []string{
		"PATH=" + binDirectory,
		"CARRY_FAKE_PI_FIRST_STARTED=" + firstStarted,
		"CARRY_FAKE_PI_FIRST_SELECTED=" + firstSelected,
		"CARRY_FAKE_PI_FIRST_PROMPT=" + firstPrompt,
		"CARRY_FAKE_PI_SECOND_PROMPT=" + secondPrompt,
		"CARRY_FAKE_PI_TERMINAL_SELECTED=" + terminalSelected,
	}
	firstHostEnvironment := append(append([]string{}, clientEnvironment...), agentEnvironment...)

	firstHost := exec.Command(carry, "host", "start")
	firstHost.Dir = root
	firstHost.Env = append(os.Environ(), firstHostEnvironment...)
	firstLog := &lockedBuffer{}
	firstHost.Stdout = firstLog
	firstHost.Stderr = firstLog
	if err := firstHost.Start(); err != nil {
		t.Fatalf("start first Host: %v", err)
	}
	waitForFile(t, firstStarted, 10*time.Second, firstLog)
	if err := firstHost.Process.Kill(); err != nil {
		t.Fatalf("interrupt first Host: %v", err)
	}
	_ = firstHost.Wait()

	pool, err := carrypostgres.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open recovery database: %v", err)
	}
	defer pool.Close()
	var oldRunID string
	var oldAttemptID string
	var oldMachineID string
	var oldFence int64
	if err := pool.QueryRow(context.Background(), `
		select run.run_id, attempt.attempt_id, attempt.machine_id, attempt.fence
		from runs as run
		join run_attempts as attempt on attempt.run_id = run.run_id
		where run.work_id = $1
		order by attempt.fence
		limit 1
	`, workID).Scan(&oldRunID, &oldAttemptID, &oldMachineID, &oldFence); err != nil {
		t.Fatalf("load interrupted Attempt: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		update run_attempts
		set
			claimed_at = clock_timestamp() - interval '2 seconds',
			lease_expires_at = clock_timestamp() - interval '1 second'
		where attempt_id = $1
	`, oldAttemptID); err != nil {
		t.Fatalf("expire interrupted Attempt lease: %v", err)
	}

	replacementConfigDirectory := filepath.Join(temporary, "replacement-config")
	replacementClientEnvironment := []string{"CARRY_CONFIG_DIR=" + replacementConfigDirectory}
	run(t, root, replacementClientEnvironment, carry, "login",
		"--server", serverURL,
		"--ca-cert", filepath.Join(pkiDirectory, "ca.pem"),
		"--token", bootstrap.UserToken,
	)
	run(t, root, replacementClientEnvironment, carry, "host", "enroll",
		"--space", bootstrap.SpaceID, "--name", "replacement-recovery-host",
	)
	replacementHostEnvironment := append(append([]string{}, replacementClientEnvironment...), agentEnvironment...)
	secondContext, cancelSecond := context.WithCancel(context.Background())
	secondHost := exec.CommandContext(secondContext, carry, "host", "start")
	secondHost.Dir = root
	secondHost.Env = append(os.Environ(), replacementHostEnvironment...)
	secondLog := &lockedBuffer{}
	secondHost.Stdout = secondLog
	secondHost.Stderr = secondLog
	if err := secondHost.Start(); err != nil {
		cancelSecond()
		t.Fatalf("start replacement Host: %v", err)
	}
	waitForWorkUnderstanding(
		t, root, carry, clientEnvironment, workID,
		"Recovered after Host interruption.", secondLog,
	)
	var recoveredMachineID string
	if err := pool.QueryRow(context.Background(), `
		select machine_id
		from run_attempts
		where run_id = $1 and fence > $2
		order by fence desc
		limit 1
	`, oldRunID, oldFence).Scan(&recoveredMachineID); err != nil {
		t.Fatalf("load recovery Machine: %v", err)
	}
	if recoveredMachineID == oldMachineID {
		t.Fatalf("recovery reused Machine %s instead of crossing Machines", recoveredMachineID)
	}
	firstPromptBytes, err := os.ReadFile(firstPrompt)
	if err != nil {
		t.Fatalf("read first recovery prompt: %v", err)
	}
	secondPromptBytes, err := os.ReadFile(secondPrompt)
	if err != nil {
		t.Fatalf("read second recovery prompt: %v", err)
	}
	if !bytes.Equal(firstPromptBytes, secondPromptBytes) {
		t.Fatalf("recovery prompt changed:\nfirst: %s\nsecond: %s", firstPromptBytes, secondPromptBytes)
	}
	for _, promptPath := range []string{firstPrompt, secondPrompt} {
		info, statErr := os.Stat(promptPath)
		if statErr != nil {
			t.Fatalf("stat recovery prompt %s: %v", promptPath, statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("recovery prompt %s mode = %o, want 600", promptPath, info.Mode().Perm())
		}
	}

	terminalCreated := run(t, root, clientEnvironment, carry, "work", "create",
		"--goal", "Require explicit retry after a terminal native failure",
	)
	terminalFields := strings.Fields(strings.SplitN(terminalCreated, "\n", 2)[0])
	if len(terminalFields) != 3 {
		t.Fatalf("terminal Work create output = %q", terminalCreated)
	}
	terminalWorkID := terminalFields[2]
	waitForWorkUnderstanding(
		t, root, carry, clientEnvironment, terminalWorkID,
		"It will not try again until a member runs", secondLog,
	)
	var attemptsBefore int
	if err := pool.QueryRow(context.Background(), `
		select count(*)
		from run_attempts as attempt
		join runs as run on run.run_id = attempt.run_id
		where run.work_id = $1
	`, terminalWorkID).Scan(&attemptsBefore); err != nil {
		t.Fatalf("count terminal Attempts: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	var attemptsWithoutRetry int
	if err := pool.QueryRow(context.Background(), `
		select count(*)
		from run_attempts as attempt
		join runs as run on run.run_id = attempt.run_id
		where run.work_id = $1
	`, terminalWorkID).Scan(&attemptsWithoutRetry); err != nil {
		t.Fatalf("recount terminal Attempts: %v", err)
	}
	if attemptsBefore != 1 || attemptsWithoutRetry != 1 {
		t.Fatalf("terminal Work Attempts before explicit retry = %d then %d", attemptsBefore, attemptsWithoutRetry)
	}
	retryOutput := run(t, root, clientEnvironment, carry, "work", "retry", terminalWorkID)
	if !strings.Contains(retryOutput, "Carry will try Work") {
		t.Fatalf("retry output = %q", retryOutput)
	}
	waitForWorkUnderstanding(
		t, root, carry, clientEnvironment, terminalWorkID,
		"Retried only after the member choice.", secondLog,
	)
	var terminalStates string
	if err := pool.QueryRow(context.Background(), `
		select string_agg(state, ',' order by created_at)
		from runs
		where work_id = $1
	`, terminalWorkID).Scan(&terminalStates); err != nil {
		t.Fatalf("load terminal retry states: %v", err)
	}
	if terminalStates != "unknown,succeeded" {
		t.Fatalf("terminal retry states = %q", terminalStates)
	}

	cancelSecond()
	_ = secondHost.Wait()

	rows, err := pool.Query(context.Background(), `
		select state, fence
		from run_attempts
		where run_id = $1
		order by fence
	`, oldRunID)
	if err != nil {
		t.Fatalf("list recovery Attempts: %v", err)
	}
	defer rows.Close()
	var attempts []string
	for rows.Next() {
		var state string
		var fence int64
		if err := rows.Scan(&state, &fence); err != nil {
			t.Fatalf("scan recovery Attempt: %v", err)
		}
		attempts = append(attempts, fmt.Sprintf("%d:%s", fence, state))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate recovery Attempts: %v", err)
	}
	if strings.Join(attempts, ",") != "1:expired,2:succeeded" {
		t.Fatalf("recovery Attempts = %v", attempts)
	}

	credential, err := machinefile.Load(configDirectory)
	if err != nil {
		t.Fatalf("load Machine credential: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"fence": oldFence, "base_understanding_version": 0, "input_end_seq": 1,
		"understanding": "A stale Host overwrote recovery.", "next_step": "This must be rejected.",
	})
	if err != nil {
		t.Fatalf("encode stale commit: %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		serverURL+"/v1/host/runs/"+oldRunID+"/attempts/"+oldAttemptID+"/understanding",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build stale commit request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := machineHTTPClient(t, credential).Do(request)
	if err != nil {
		t.Fatalf("send stale commit: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale commit status = %s, want 409 Conflict", response.Status)
	}
}

func writeRecoveryPi(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
umask 077
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' '0.84.2'
  exit 0
fi
IFS= read -r prompt
case "$prompt" in
  *"Require explicit retry after a terminal native failure"*)
    if [ ! -e "$CARRY_FAKE_PI_TERMINAL_SELECTED" ]; then
      : > "$CARRY_FAKE_PI_TERMINAL_SELECTED"
      exit 1
    fi
    printf '%s\n' \
      '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
      '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Retried only after the member choice.\",\"next_step\":\"Continue the explicitly authorized fresh Run.\"}"}],"stopReason":"stop"}}' \
      '{"type":"agent_settled"}'
    exit 0
    ;;
esac
if [ ! -e "$CARRY_FAKE_PI_FIRST_SELECTED" ]; then
  printf '%s' "$prompt" > "$CARRY_FAKE_PI_FIRST_PROMPT"
  : > "$CARRY_FAKE_PI_FIRST_SELECTED"
  : > "$CARRY_FAKE_PI_FIRST_STARTED"
  IFS= read -r wait_for_parent || true
  exit 0
fi
printf '%s' "$prompt" > "$CARRY_FAKE_PI_SECOND_PROMPT"
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{\"understanding\":\"Recovered after Host interruption.\",\"next_step\":\"Continue from the durable Work context.\"}"}],"stopReason":"stop"}}' \
  '{"type":"agent_settled"}'
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write recovery Pi executable: %v", err)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration, log *lockedBuffer) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				return
			}
		case <-deadline.C:
			t.Fatalf("file %s did not appear\n%s", path, log.String())
		}
	}
}

func waitForWorkUnderstanding(
	t *testing.T,
	root string,
	carry string,
	environment []string,
	workID string,
	understanding string,
	hostLog *lockedBuffer,
) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			output, err := runError(root, environment, carry, "work", "show", workID)
			if err == nil && strings.Contains(output, understanding) {
				return
			}
		case <-deadline.C:
			t.Fatalf("replacement Host did not update Work\n%s", hostLog.String())
		}
	}
}

func machineHTTPClient(t *testing.T, credential machinefile.Credential) *http.Client {
	t.Helper()
	certificate, err := tls.X509KeyPair(
		[]byte(credential.CertificatePEM), []byte(credential.PrivateKeyPEM),
	)
	if err != nil {
		t.Fatalf("load Machine key pair: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(credential.CACertificatePEM)) {
		t.Fatal("load Machine CA certificate")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate},
	}}}
}
