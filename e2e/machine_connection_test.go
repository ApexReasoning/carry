//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	writeFakeCodex(t, filepath.Join(binDirectory, "codex"))
	piOnlyDirectory := filepath.Join(temporary, "pi-only-bin")
	if err := os.Mkdir(piOnlyDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, filepath.Join(piOnlyDirectory, "pi"))
	hostEnvironment := append(environment, "PATH="+binDirectory)
	piOnlyEnvironment := append(environment, "PATH="+piOnlyDirectory)
	if started := runHostUntilStarted(t, root, carry, hostEnvironment); !strings.Contains(started, first.MachineID) {
		t.Fatalf("Host start = %q", started)
	}
	authorityPool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer authorityPool.Close()
	firstAgents := loadHostAgents(t, authorityPool, first.MachineID)
	if len(firstAgents) != 2 || firstAgents["Pi"].AgentID == "" || firstAgents["Codex"].AgentID == "" || !firstAgents["Pi"].Present || !firstAgents["Codex"].Present {
		t.Fatalf("first complete Agent inventory = %#v", firstAgents)
	}
	var firstRevision int64
	if err := authorityPool.QueryRow(t.Context(), `select agent_report_revision from machines where machine_id=$1`, first.MachineID).Scan(&firstRevision); err != nil || firstRevision < 2 {
		t.Fatalf("setup/start stale-zero recovery revision = %d, %v", firstRevision, err)
	}
	stopServer()
	stopServer, serverLog, _ = startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	waitForServer(t, serverURL, caPath, serverLog)
	if restarted := runHostUntilStarted(t, root, carry, piOnlyEnvironment); !strings.Contains(restarted, first.MachineID) {
		t.Fatalf("Host restart changed Machine = %q", restarted)
	}
	absentCodex := loadHostAgents(t, authorityPool, first.MachineID)
	if !absentCodex["Pi"].Present || absentCodex["Codex"].Present ||
		absentCodex["Pi"].AgentID != firstAgents["Pi"].AgentID || absentCodex["Codex"].AgentID != firstAgents["Codex"].AgentID {
		t.Fatalf("independent absent Agent inventory = %#v, first=%#v", absentCodex, firstAgents)
	}
	if restarted := runHostUntilStarted(t, root, carry, hostEnvironment); !strings.Contains(restarted, first.MachineID) {
		t.Fatalf("Host online restart changed Machine = %q", restarted)
	}
	restartedAgents := loadHostAgents(t, authorityPool, first.MachineID)
	if !restartedAgents["Pi"].Present || !restartedAgents["Codex"].Present ||
		restartedAgents["Pi"].AgentID != firstAgents["Pi"].AgentID || restartedAgents["Codex"].AgentID != firstAgents["Codex"].AgentID {
		t.Fatalf("stable restarted Agent inventory = %#v, first=%#v", restartedAgents, firstAgents)
	}

	disconnect := run(t, root, environment, carry, "host", "disconnect")
	if !strings.Contains(disconnect, "confirmed Machine") || !strings.Contains(disconnect, "does not prove a process stopped") {
		t.Fatalf("disconnect output = %q", disconnect)
	}
	if _, err := os.Stat(filepath.Join(configDirectory, "machine.json")); !os.IsNotExist(err) {
		t.Fatalf("credential remains after confirmed disconnect: %v", err)
	}
	var removedAgents int
	if err := authorityPool.QueryRow(t.Context(), `select count(*) from agents where machine_id=$1 and removed_at is not null`, first.MachineID).Scan(&removedAgents); err != nil || removedAgents != 2 {
		t.Fatalf("self revoke Removed Agents = %d, %v", removedAgents, err)
	}
	staleDirectory := filepath.Join(temporary, "stale")
	if err := os.Mkdir(staleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDirectory, "machine.json"), machineJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := runError(root, []string{"CARRY_CONFIG_DIR=" + staleDirectory, "PATH=" + binDirectory}, carry, "host", "start"); err == nil || !strings.Contains(output, "this Host is revoked") {
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
	pending := startCarryMachineConnectionWithEnvironment(
		t, root, carry, browserConfig, webURL, caPath, "browser-reviewed-machine",
		[]string{"PATH=" + binDirectory},
	)
	offlineRequestFile := filepath.Join(temporary, "browser-offline-request")
	offlineReadyFile := filepath.Join(temporary, "browser-offline-ready")
	restartRequestFile := filepath.Join(temporary, "browser-restart-request")
	onlineReadyFile := filepath.Join(temporary, "browser-online-ready")
	browserFinished := make(chan struct{})
	recoveryFinished := make(chan browserRecoveryEvidence, 1)
	go func() {
		recoveryFinished <- coordinateBrowserHostRecovery(
			t.Context(), authorityPool, root, carry, browserConfig, binDirectory, pending,
			offlineRequestFile, offlineReadyFile, restartRequestFile, onlineReadyFile,
			browserFinished,
		)
	}()
	playwrightOutput, playwrightErr := runError(root, []string{
		"CARRY_WEB_URL=" + webURL,
		"CARRY_EMAIL_CAPTURE_FILE=" + emailCaptureFile,
		"CARRY_LOGIN_EMAIL=" + memberEmail,
		"CARRY_MACHINE_CODE=" + pending.code,
		"CARRY_MACHINE_FINGERPRINT=" + pending.fingerprint,
		"CARRY_MACHINE_NAME=browser-reviewed-machine",
		"CARRY_MACHINE_SERVER_ORIGIN=" + webURL,
		"CARRY_MACHINE_SPACE_NAME=Carry Space",
		"CARRY_MACHINE_OFFLINE_REQUEST_FILE=" + offlineRequestFile,
		"CARRY_MACHINE_OFFLINE_READY_FILE=" + offlineReadyFile,
		"CARRY_MACHINE_RESTART_REQUEST_FILE=" + restartRequestFile,
		"CARRY_MACHINE_ONLINE_READY_FILE=" + onlineReadyFile,
	}, "pnpm", "--dir", "apps/web", "exec", "playwright", "test", "e2e/machine-connection.spec.ts")
	close(browserFinished)
	recovery := <-recoveryFinished
	if playwrightErr != nil {
		t.Fatalf("Machine Browser journey: %v\n%s\nCLI:\n%s", playwrightErr, playwrightOutput, pending.log.String())
	}
	if recovery.err != nil {
		t.Fatalf("coordinate Browser Host loss/recovery: %v\nHost:\n%s", recovery.err, recovery.hostLog)
	}
	if !agentsRecovered(recovery.before, recovery.after) {
		t.Fatalf("Browser Host recovery changed durable Agents: before=%#v after=%#v", recovery.before, recovery.after)
	}
	t.Logf("Machine Browser Playwright:\n%s", strings.TrimSpace(playwrightOutput))
	localOnly := run(t, root, []string{"CARRY_CONFIG_DIR=" + browserConfig}, carry, "host", "disconnect", "--local-only")
	if !strings.Contains(localOnly, "may still appear Active") {
		t.Fatalf("local-only output = %q", localOnly)
	}
}

type browserRecoveryEvidence struct {
	before  map[string]hostAgentFact
	after   map[string]hostAgentFact
	hostLog string
	err     error
}

type hostAgentFact struct {
	AgentID    string
	Present    bool
	LastActive time.Time
}

func coordinateBrowserHostRecovery(
	ctx context.Context,
	pool *pgxpool.Pool,
	root string,
	carry string,
	configDirectory string,
	binDirectory string,
	pending pendingMachineConnection,
	offlineRequestFile string,
	offlineReadyFile string,
	restartRequestFile string,
	onlineReadyFile string,
	browserFinished <-chan struct{},
) browserRecoveryEvidence {
	var evidence browserRecoveryEvidence
	if err := waitForBrowserSignal(ctx, offlineRequestFile, browserFinished); err != nil {
		evidence.err = err
		return evidence
	}
	var machineID string
	if err := pool.QueryRow(ctx, `
		select machine_id
		from machines
		where display_name='browser-reviewed-machine' and revoked_at is null
		order by enrolled_at desc
		limit 1
	`).Scan(&machineID); err != nil {
		evidence.err = fmt.Errorf("load Browser-reviewed Machine: %w", err)
		return evidence
	}
	before, err := queryHostAgents(ctx, pool, machineID)
	if err != nil || len(before) != 2 {
		evidence.err = fmt.Errorf("load online Browser Agent identities: %w", err)
		return evidence
	}
	evidence.before = before
	if err := pending.command.Process.Kill(); err != nil {
		evidence.err = fmt.Errorf("stop Browser setup Host: %w", err)
		return evidence
	}
	select {
	case <-pending.done:
	case <-ctx.Done():
		evidence.err = ctx.Err()
		return evidence
	}
	if _, err := pool.Exec(ctx, `
		update machines
		set agent_reported_at=transaction_timestamp()-interval '46 seconds'
		where machine_id=$1
	`, machineID); err != nil {
		evidence.err = fmt.Errorf("age Browser Agent report: %w", err)
		return evidence
	}
	if err := os.WriteFile(offlineReadyFile, []byte("offline"), 0o600); err != nil {
		evidence.err = fmt.Errorf("publish Browser Offline fixture: %w", err)
		return evidence
	}
	if err := waitForBrowserSignal(ctx, restartRequestFile, browserFinished); err != nil {
		evidence.err = err
		return evidence
	}

	restartContext, cancelRestart := context.WithCancel(ctx)
	restart := exec.CommandContext(restartContext, carry, "host", "start")
	restart.Dir = root
	restart.Env = append(os.Environ(),
		"CARRY_CONFIG_DIR="+configDirectory,
		"PATH="+binDirectory,
	)
	log := &lockedBuffer{}
	restart.Stdout = log
	restart.Stderr = log
	if err := restart.Start(); err != nil {
		cancelRestart()
		evidence.err = fmt.Errorf("restart Browser Host: %w", err)
		return evidence
	}
	restartDone := make(chan error, 1)
	go func() { restartDone <- restart.Wait() }()
	defer func() {
		cancelRestart()
		<-restartDone
		evidence.hostLog = log.String()
	}()

	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !strings.Contains(log.String(), "Started Carry Host") {
				continue
			}
			var online int
			if err := pool.QueryRow(ctx, `
				select count(*)
				from agents as stored_agent
				join agent_presence as presence using(agent_id)
				join machines as stored_machine using(machine_id)
				where stored_agent.machine_id=$1
					and stored_agent.removed_at is null
					and presence.present
					and stored_machine.agent_reported_at > transaction_timestamp()-interval '45 seconds'
			`, machineID).Scan(&online); err != nil {
				evidence.err = fmt.Errorf("observe restarted Browser Host: %w", err)
				return evidence
			}
			if online != 2 {
				continue
			}
			after, err := queryHostAgents(ctx, pool, machineID)
			if err != nil {
				evidence.err = fmt.Errorf("load restarted Browser Agent identities: %w", err)
				return evidence
			}
			evidence.after = after
			if err := os.WriteFile(onlineReadyFile, []byte("online"), 0o600); err != nil {
				evidence.err = fmt.Errorf("publish Browser Online fixture: %w", err)
				return evidence
			}
			<-browserFinished
			return evidence
		case err := <-restartDone:
			restartDone <- err
			evidence.err = fmt.Errorf("Browser Host exited before recovery: %w", err)
			return evidence
		case <-deadline.C:
			evidence.err = errors.New("Browser Host did not recover before deadline")
			return evidence
		case <-browserFinished:
			evidence.err = errors.New("Browser journey ended before Host recovery")
			return evidence
		case <-ctx.Done():
			evidence.err = ctx.Err()
			return evidence
		}
	}
}

func waitForBrowserSignal(ctx context.Context, path string, browserFinished <-chan struct{}) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				return nil
			} else if !os.IsNotExist(err) {
				return err
			}
		case <-browserFinished:
			return errors.New("Browser journey ended before fixture signal")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func agentsRecovered(before, after map[string]hostAgentFact) bool {
	if len(before) != len(after) {
		return false
	}
	for name, original := range before {
		recovered, exists := after[name]
		if !exists || recovered.AgentID != original.AgentID || !recovered.Present ||
			recovered.LastActive.Before(original.LastActive) {
			return false
		}
	}
	return true
}

func loadHostAgents(t *testing.T, pool *pgxpool.Pool, machineID string) map[string]hostAgentFact {
	t.Helper()
	facts, err := queryHostAgents(t.Context(), pool, machineID)
	if err != nil {
		t.Fatalf("load Host Agents: %v", err)
	}
	return facts
}

func queryHostAgents(ctx context.Context, pool *pgxpool.Pool, machineID string) (map[string]hostAgentFact, error) {
	rows, err := pool.Query(ctx, `
		select agent.name, agent.agent_id::text, presence.present, presence.last_present_at
		from agents as agent
		join agent_presence as presence on presence.agent_id=agent.agent_id
		where agent.machine_id=$1
		order by agent.name`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := make(map[string]hostAgentFact)
	for rows.Next() {
		var name string
		var fact hostAgentFact
		var lastActive *time.Time
		if err := rows.Scan(&name, &fact.AgentID, &fact.Present, &lastActive); err != nil {
			return nil, err
		}
		if lastActive != nil {
			fact.LastActive = *lastActive
		}
		facts[name] = fact
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return facts, nil
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
