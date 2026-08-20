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
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var sharedBootstrap struct {
	once   sync.Once
	output string
	err    error
}

func bootstrapCarry(t *testing.T, root string, carryServer string, databaseURL string) string {
	t.Helper()
	sharedBootstrap.once.Do(func() {
		credentialFile := filepath.Join(filepath.Dir(carryServer), "bootstrap-credential.json")
		arguments := []string{
			"bootstrap",
			"--display-name", "Carry Member",
			"--space", "Carry Space",
			"--credential-file", credentialFile,
		}
		// Discard the first stdout result after the database commit. The durable
		// credential must make the exact retry recoverable.
		_, sharedBootstrap.err = runError(
			root, []string{"CARRY_DATABASE_URL=" + databaseURL}, carryServer, arguments...,
		)
		if sharedBootstrap.err != nil {
			return
		}
		credential, err := os.ReadFile(credentialFile)
		if err != nil {
			sharedBootstrap.err = fmt.Errorf("read durable bootstrap credential: %w", err)
			return
		}
		var durable struct {
			UserID    string `json:"user_id"`
			SpaceID   string `json:"space_id"`
			UserToken string `json:"user_token"`
		}
		if err := json.Unmarshal(credential, &durable); err != nil {
			sharedBootstrap.err = fmt.Errorf("decode durable bootstrap credential: %w", err)
			return
		}
		replayed, replayErr := runError(
			root, []string{"CARRY_DATABASE_URL=" + databaseURL}, carryServer, arguments...,
		)
		if replayErr != nil {
			sharedBootstrap.err = replayErr
			return
		}
		var replayedResult struct {
			UserID    string `json:"user_id"`
			SpaceID   string `json:"space_id"`
			UserToken string `json:"user_token"`
		}
		if err := json.Unmarshal([]byte(replayed), &replayedResult); err != nil {
			sharedBootstrap.err = fmt.Errorf("decode replayed bootstrap result: %w", err)
			return
		}
		if replayedResult != durable {
			sharedBootstrap.err = fmt.Errorf("replayed bootstrap result differs from durable credential")
			return
		}
		sharedBootstrap.output = string(credential)
		info, err := os.Stat(credentialFile)
		if err != nil {
			sharedBootstrap.err = fmt.Errorf("stat bootstrap credential: %w", err)
			return
		}
		if info.Mode().Perm() != 0o600 {
			sharedBootstrap.err = fmt.Errorf("bootstrap credential mode = %o, want 600", info.Mode().Perm())
		}
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

func resetProductJourneyFacts(t *testing.T, databaseURL string) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open product journey database: %v", err)
	}
	defer pool.Close()
	transaction, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin product journey reset: %v", err)
	}
	defer func() { _ = transaction.Rollback(t.Context()) }()
	for _, table := range []string{
		"conversation_reply_claims",
		"conversation_messages",
		"conversations",
		"run_attempts",
		"runs",
		"work_result_checks",
		"work_messages",
		"works",
		"machines",
		"browser_sessions",
	} {
		if _, err := transaction.Exec(t.Context(), "delete from "+table); err != nil {
			t.Fatalf("reset product journey table %s: %v", table, err)
		}
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatalf("commit product journey reset: %v", err)
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
review_required="${CARRY_FAKE_PI_REVIEW_REQUIRED:-false}"
printf '%s\n' \
  '{"id":"carry-prompt","type":"response","command":"prompt","success":true}' \
  "{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"{\\\"understanding\\\":\\\"Finance approved a twelve month term.\\\",\\\"next_step\\\":\\\"Prepare the renewal recommendation.\\\",\\\"review_required\\\":$review_required}\"}],\"stopReason\":\"stop\"}}" \
  '{"type":"agent_settled"}'
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake Pi executable: %v", err)
	}
}
