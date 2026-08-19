//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	bootstrapOutput := bootstrapCarry(t, root, carryServer, databaseURL)
	var bootstrap struct {
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
	environment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	run(
		t, root, environment, carry, "login",
		"--server", serverURL,
		"--ca-cert", filepath.Join(pkiDirectory, "ca.pem"),
		"--token", bootstrap.UserToken,
	)
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

	stopServer()
	stopServer, serverLog = startServer(t, root, carryServer, address, databaseURL, pkiDirectory)
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)
	restarted := run(t, root, environment, carry, "work", "show", workID)
	if !strings.Contains(restarted, "Track recurring customer renewal questions") ||
		!strings.Contains(restarted, "Enterprise customers ask for a 60-day notice period") {
		t.Fatalf("Work after server restart = %q", restarted)
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
	bootstrapOutput := bootstrapCarry(t, root, carryServer, databaseURL)
	var bootstrap struct {
		UserToken string `json:"user_token"`
	}
	if err := json.Unmarshal([]byte(bootstrapOutput), &bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}

	serverAddress := freeAddress(t)
	stopServer, serverLog := startServer(t, root, carryServer, serverAddress, databaseURL, pkiDirectory)
	defer func() { stopServer() }()
	serverURL := "https://" + serverAddress
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)

	run(t, root, nil, "pnpm", "--dir", "apps/web", "build")
	webAddress := freeAddress(t)
	stopWeb, webLog := startWeb(t, root, webAddress, serverURL, pkiDirectory)
	defer func() { stopWeb() }()
	webURL := "https://" + webAddress
	waitForServer(t, webURL, filepath.Join(pkiDirectory, "ca.pem"), webLog)

	output, err := runError(
		root,
		[]string{
			"CARRY_WEB_URL=" + webURL,
			"CARRY_MEMBER_TOKEN=" + bootstrap.UserToken,
		},
		"pnpm", "--dir", "apps/web", "test:product",
	)
	if err != nil {
		t.Fatalf("run browser product journey: %v\n%s", err, output)
	}
}

func startWeb(
	t *testing.T,
	root string,
	address string,
	apiURL string,
	pkiDirectory string,
) (func(), *lockedBuffer) {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("parse Web address: %v", err)
	}
	webCtx, cancel := context.WithCancel(t.Context())
	webLog := &lockedBuffer{}
	vite := filepath.Join(root, "apps", "web", "node_modules", ".bin", "vite")
	command := exec.CommandContext(
		webCtx, vite, "preview", "--host", host, "--port", port,
	)
	command.Dir = filepath.Join(root, "apps", "web")
	command.Env = append(
		os.Environ(),
		"CARRY_API_URL="+apiURL,
		"CARRY_WEB_TLS_CERT="+filepath.Join(pkiDirectory, "server.pem"),
		"CARRY_WEB_TLS_KEY="+filepath.Join(pkiDirectory, "server-key.pem"),
	)
	command.Stdout = webLog
	command.Stderr = webLog
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start Web preview: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := command.Wait(); err != nil && webCtx.Err() == nil {
			t.Errorf("wait for Web preview: %v", err)
		}
	}
	return stop, webLog
}
