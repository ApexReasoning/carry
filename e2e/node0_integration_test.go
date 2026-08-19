//go:build integration

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
		"--space", bootstrap.SpaceID, "--name", "node-zero-host")
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
	run(t, root, clientEnvironment, carry, "host", "revoke")
	if output, err := runError(root, hostEnvironment, carry, "host", "start"); err == nil || !strings.Contains(output, "403 Forbidden") {
		t.Fatalf("revoked Host output = %q, error = %v", output, err)
	}
}

var sharedBootstrap struct {
	once   sync.Once
	output string
	err    error
}

func bootstrapCarry(t *testing.T, root string, carryServer string, databaseURL string) string {
	t.Helper()
	sharedBootstrap.once.Do(func() {
		sharedBootstrap.output, sharedBootstrap.err = runError(
			root,
			[]string{"CARRY_DATABASE_URL=" + databaseURL},
			carryServer,
			"bootstrap",
			"--display-name", "Carry Member",
			"--space", "Carry Space",
		)
	})
	if sharedBootstrap.err != nil {
		t.Fatalf("bootstrap Carry: %v\n%s", sharedBootstrap.err, sharedBootstrap.output)
	}
	return sharedBootstrap.output
}

func startServer(
	t *testing.T,
	root string,
	carryServer string,
	address string,
	databaseURL string,
	pkiDirectory string,
) (func(), *lockedBuffer) {
	t.Helper()
	serverCtx, cancel := context.WithCancel(t.Context())
	serverLog := &lockedBuffer{}
	serverCommand := exec.CommandContext(serverCtx, carryServer, "serve", "--listen", address)
	serverCommand.Dir = root
	serverCommand.Env = append(os.Environ(),
		"CARRY_DATABASE_URL="+databaseURL,
		"CARRY_PKI_DIR="+pkiDirectory,
	)
	serverCommand.Stdout = serverLog
	serverCommand.Stderr = serverLog
	if err := serverCommand.Start(); err != nil {
		t.Fatalf("start carry-server: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := serverCommand.Wait(); err != nil && serverCtx.Err() == nil {
			t.Errorf("wait for carry-server: %v", err)
		}
	}
	return stop, serverLog
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

func build(t *testing.T, root string, output string, packagePath string) {
	t.Helper()
	command := exec.Command("go", "build", "-o", output, packagePath)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
}

func run(t *testing.T, root string, environment []string, commandPath string, arguments ...string) string {
	t.Helper()
	output, err := runError(root, environment, commandPath, arguments...)
	if err != nil {
		t.Fatalf("run %s %v: %v\n%s", commandPath, arguments, err, output)
	}
	return output
}

func runError(root string, environment []string, commandPath string, arguments ...string) (string, error) {
	command := exec.Command(commandPath, arguments...)
	command.Dir = root
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("run %s: %w", commandPath, err)
	}
	return string(output), nil
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate server address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release server address: %v", err)
	}
	return address
}

func waitForServer(t *testing.T, serverURL string, caCertificatePath string, serverLog *lockedBuffer) {
	t.Helper()
	caCertificate, err := os.ReadFile(caCertificatePath)
	if err != nil {
		t.Fatalf("read CA certificate: %v", err)
	}
	client, err := testHTTPClient(caCertificate)
	if err != nil {
		t.Fatalf("create health client: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(serverURL + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("carry-server did not become ready: %s", serverLog.String())
}

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

func testHTTPClient(caCertificate []byte) (*http.Client, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caCertificate) {
		return nil, fmt.Errorf("parse CA certificate")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}}}, nil
}
