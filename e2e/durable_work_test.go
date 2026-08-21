//go:build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
	"github.com/ApexReasoning/carry/internal/cli/userapi"
)

func TestMemberCreatesMessagesAndReloadsDurableWork(t *testing.T) {
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
	testMemberOutput := prepareTestMember(t, databaseURL)
	resetProductJourneyFacts(t, databaseURL)
	var member struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal([]byte(testMemberOutput), &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	const cliLoginEmail = "cli-member@example.com"
	attachTestEmailIdentity(t, databaseURL, member.UserID, cliLoginEmail)

	address := freeAddress(t)
	apiURL := "https://" + address
	webAddress := freeAddress(t)
	webURL := "https://" + webAddress
	stopServer, serverLog, emailCaptureFile := startServerWithOrigin(t, root, carryServer, address, databaseURL, pkiDirectory, webURL)
	defer func() { stopServer() }()
	waitForServer(t, apiURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)
	run(t, root, nil, "pnpm", "--dir", "apps/web", "build")
	stopWeb, webLog := startWeb(t, root, webAddress, apiURL, pkiDirectory)
	defer stopWeb()
	waitForServer(t, webURL, filepath.Join(pkiDirectory, "ca.pem"), webLog)

	configDirectory := filepath.Join(temporary, "config")
	environment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	const cliLabel = "durable-work-cli"
	pendingLogin := startCarryCLILogin(t, root, carry, configDirectory, webURL, filepath.Join(pkiDirectory, "ca.pem"), cliLabel)
	browserOutput, browserErr := runError(root, []string{
		"CARRY_WEB_URL=" + webURL,
		"CARRY_EMAIL_CAPTURE_FILE=" + emailCaptureFile,
		"CARRY_LOGIN_EMAIL=" + cliLoginEmail,
		"CARRY_CLI_LOGIN_CODE=" + pendingLogin.code,
		"CARRY_CLI_SERVER_ORIGIN=" + webURL,
		"CARRY_CLI_LABEL=" + cliLabel,
	}, "pnpm", "--dir", "apps/web", "exec", "playwright", "test", "e2e/cli-login.spec.ts")
	if browserErr != nil || !strings.Contains(browserOutput, "1 passed") {
		t.Fatalf("run Browser-approved CLI journey: %v\n%s", browserErr, browserOutput)
	}
	finishCarryCLILogin(t, pendingLogin)
	createdOutput := run(
		t, root, environment, carry, "work", "create",
		"--goal", "Track recurring customer renewal questions",
	)
	createdFields := strings.Fields(strings.SplitN(createdOutput, "\n", 2)[0])
	if len(createdFields) != 3 || createdFields[0] != "Created" || createdFields[1] != "Work" {
		t.Fatalf("create output = %q", createdOutput)
	}
	workID := createdFields[2]

	listed := run(t, root, environment, carry, "work", "list")
	if !strings.Contains(listed, workID) || !strings.Contains(listed, "Track recurring customer renewal questions") {
		t.Fatalf("Work list = %q", listed)
	}
	initial := run(t, root, environment, carry, "work", "show", workID)
	if !strings.Contains(initial, "Messages: none") || !strings.Contains(initial, "Status: open") {
		t.Fatalf("initial Work = %q", initial)
	}
	messageOutput := run(
		t, root, environment, carry, "work", "message", workID,
		"--text", "Enterprise customers ask for a 60-day notice period",
	)
	if !strings.Contains(messageOutput, "Added message to Work "+workID) {
		t.Fatalf("message output = %q", messageOutput)
	}
	if logged := serverLog.String(); strings.Contains(logged, pendingLogin.code) || strings.Contains(logged, "carry_cli_") {
		t.Fatalf("server log contains CLI login secret: %s", logged)
	}

	stopServer()
	stopServer, serverLog, _ = startServerWithOrigin(t, root, carryServer, address, databaseURL, pkiDirectory, webURL)
	waitForServer(t, apiURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)
	restarted := run(t, root, environment, carry, "work", "show", workID)
	if !strings.Contains(restarted, "Track recurring customer renewal questions") ||
		!strings.Contains(restarted, "Enterprise customers ask for a 60-day notice period") {
		t.Fatalf("Work after server restart = %q", restarted)
	}
	oldCredential, err := credentialfile.Load(configDirectory)
	if err != nil {
		t.Fatalf("load CLI credential before revoke: %v", err)
	}
	logout := run(t, root, environment, carry, "logout")
	if !strings.Contains(logout, "Revoked CLI access") {
		t.Fatalf("logout output = %q", logout)
	}
	oldClient, err := userapi.FromCredential(oldCredential)
	if err != nil {
		t.Fatalf("recreate old CLI client: %v", err)
	}
	if _, err := oldClient.LoadMember(t.Context()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("revoked CLI credential still authenticated: %v", err)
	}
	if output, err := runError(root, environment, carry, "work", "show", workID); err == nil || !strings.Contains(output, "read CLI credential") {
		t.Fatalf("Work after local CLI cleanup = %q, %v", output, err)
	}
}

func TestBrowserCreatesDurableWorkWithoutStoringBearer(t *testing.T) {
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
	build(t, root, carryServer, "./cmd/carry-server")
	pkiDirectory := filepath.Join(temporary, "pki")
	run(t, root, nil, carryServer, "pki", "init", "--dir", pkiDirectory, "--hosts", "localhost,127.0.0.1")
	_ = prepareTestMember(t, databaseURL)
	resetProductJourneyFacts(t, databaseURL)

	serverAddress := freeAddress(t)
	webAddress := freeAddress(t)
	webURL := "https://" + webAddress
	stopServer, serverLog, emailCaptureFile := startServerWithOrigin(t, root, carryServer, serverAddress, databaseURL, pkiDirectory, webURL)
	defer func() { stopServer() }()
	serverURL := "https://" + serverAddress
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)

	run(t, root, nil, "pnpm", "--dir", "apps/web", "build")
	stopWeb, webLog := startWeb(t, root, webAddress, serverURL, pkiDirectory)
	defer func() { stopWeb() }()
	waitForServer(t, webURL, filepath.Join(pkiDirectory, "ca.pem"), webLog)

	loginEmail := fmt.Sprintf("new-user-%d@example.com", time.Now().UnixNano())
	output, err := runError(
		root,
		[]string{
			"CARRY_WEB_URL=" + webURL,
			"CARRY_EMAIL_CAPTURE_FILE=" + emailCaptureFile,
			"CARRY_LOGIN_EMAIL=" + loginEmail,
		},
		"pnpm", "--dir", "apps/web", "exec", "playwright", "test",
		"e2e/user-session.spec.ts", "e2e/first-durable-work.spec.ts",
	)
	if err != nil {
		t.Fatalf("run browser product journey: %v\n%s", err, output)
	}
	if !strings.Contains(output, "7 passed") {
		t.Fatalf("email identity and durable Work Playwright specs did not execute:\n%s", output)
	}
	serverOutput := serverLog.String()
	if localPart := strings.SplitN(loginEmail, "@", 2)[0]; strings.Contains(serverOutput, localPart) {
		t.Fatalf("carry-server log contains login email local part: %s", serverOutput)
	}
	capturedCode, readErr := os.ReadFile(emailCaptureFile)
	if readErr != nil {
		t.Fatalf("read latest submitted email code: %v", readErr)
	}
	if code := strings.TrimSpace(string(capturedCode)); code != "" && strings.Contains(serverOutput, code) {
		t.Fatalf("carry-server log contains email code: %s", serverOutput)
	}
}
