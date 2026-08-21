//go:build integration

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var sharedTestMember struct {
	once   sync.Once
	output string
	err    error
}

// prepareTestMember is test-only database setup. Production has no operator
// bootstrap or reusable member bearer after Node 11.
func prepareTestMember(t *testing.T, databaseURL string) string {
	t.Helper()
	sharedTestMember.once.Do(func() {
		ctx := context.Background()
		pool, err := carrypostgres.Open(ctx, databaseURL)
		if err != nil {
			sharedTestMember.err = err
			return
		}
		defer pool.Close()
		if err := carrypostgres.Migrate(ctx, pool); err != nil {
			sharedTestMember.err = err
			return
		}
		userID, spaceID := uuid.NewString(), uuid.NewString()
		tx, err := pool.Begin(ctx)
		if err != nil {
			sharedTestMember.err = err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, 'Carry Member')`, userID); err != nil {
			sharedTestMember.err = err
			return
		}
		if _, err := tx.Exec(ctx, `insert into spaces (space_id, name, slug) values ($1::uuid, 'Carry Space', replace(($1::uuid)::text, '-', ''))`, spaceID); err != nil {
			sharedTestMember.err = err
			return
		}
		if _, err := tx.Exec(ctx, `insert into space_memberships (space_id, user_id, can_enroll_machines, can_manage_members) values ($1, $2, true, true)`, spaceID, userID); err != nil {
			sharedTestMember.err = err
			return
		}
		if err := tx.Commit(ctx); err != nil {
			sharedTestMember.err = err
			return
		}
		encoded, err := json.Marshal(struct {
			UserID  string `json:"user_id"`
			SpaceID string `json:"space_id"`
		}{UserID: userID, SpaceID: spaceID})
		if err != nil {
			sharedTestMember.err = err
			return
		}
		sharedTestMember.output = string(encoded)
	})
	if sharedTestMember.err != nil {
		t.Fatalf("prepare Carry test member: %v", sharedTestMember.err)
	}
	return sharedTestMember.output
}

type pendingCLILogin struct {
	command *exec.Cmd
	log     *lockedBuffer
	code    string
	done    chan error
}

func startCarryCLILogin(
	t *testing.T,
	root string,
	carry string,
	configDirectory string,
	serverURL string,
	caCertificatePath string,
	label string,
) pendingCLILogin {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, carry, "login", "--server", serverURL, "--ca-cert", caCertificatePath, "--name", label)
	command.Dir = root
	command.Env = append(os.Environ(), "CARRY_CONFIG_DIR="+configDirectory)
	log := &lockedBuffer{}
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatalf("start Browser-approved carry login: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	pattern := regexp.MustCompile(`Code: ([BCDFGHJKLMNPQRSTVWXZ-]+)`)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if match := pattern.FindStringSubmatch(log.String()); len(match) == 2 {
			return pendingCLILogin{command: command, log: log, code: match[1], done: done}
		}
		select {
		case err := <-done:
			t.Fatalf("carry login stopped before showing a code: %v\n%s", err, log.String())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("carry login did not show a code\n%s", log.String())
	return pendingCLILogin{}
}

func approveCarryCLILoginHTTP(
	t *testing.T,
	databaseURL string,
	serverURL string,
	caCertificatePath string,
	userID string,
	spaceID string,
	code string,
) {
	t.Helper()
	sessionID := uuid.NewString()
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open CLI approval fixture database: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(t.Context(), `insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1, $2, 'email', transaction_timestamp() + interval '1 hour')`, sessionID, userID); err != nil {
		t.Fatalf("create CLI approval Browser Session: %v", err)
	}
	caPEM, err := os.ReadFile(caCertificatePath)
	if err != nil {
		t.Fatalf("read CLI approval CA: %v", err)
	}
	client, err := testHTTPClient(caPEM)
	if err != nil {
		t.Fatalf("create CLI approval client: %v", err)
	}
	identityCredentials, err := identity.NewCredentials(bytes.Repeat([]byte{3}, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("create CLI approval Identity credentials: %v", err)
	}
	cookie, err := identityCredentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatalf("create CLI approval Browser cookie: %v", err)
	}
	post := func(path string, body any, key string, destination any) {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, serverURL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", serverURL)
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		request.AddCookie(&http.Cookie{Name: "__Host-carry_session", Value: cookie})
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("send CLI Browser approval: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			failure, _ := io.ReadAll(response.Body)
			t.Fatalf("CLI Browser approval %s = %d: %s", path, response.StatusCode, failure)
		}
		if destination != nil && json.NewDecoder(response.Body).Decode(destination) != nil {
			t.Fatalf("decode CLI Browser approval %s", path)
		}
	}
	var preview struct {
		RequestID string `json:"request_id"`
		UserCode  string `json:"user_code"`
	}
	post("/v1/cli-logins/lookup", map[string]string{"user_code": code}, "", &preview)
	if preview.UserCode != code {
		t.Fatalf("Browser code = %q, terminal code = %q", preview.UserCode, code)
	}
	post("/v1/cli-logins/approve", map[string]string{
		"request_id": preview.RequestID, "user_code": code, "space_id": spaceID,
	}, uuid.NewString(), nil)
}

func finishCarryCLILogin(t *testing.T, pending pendingCLILogin) string {
	t.Helper()
	select {
	case err := <-pending.done:
		if err != nil {
			t.Fatalf("complete Browser-approved carry login: %v\n%s", err, pending.log.String())
		}
	case <-time.After(20 * time.Second):
		_ = pending.command.Process.Kill()
		t.Fatalf("Browser-approved carry login timed out\n%s", pending.log.String())
	}
	output := pending.log.String()
	if !strings.Contains(output, "Logged in to ") || !strings.Contains(output, "Default Space:") {
		t.Fatalf("CLI login completion output = %q", output)
	}
	return output
}

func loginCarryCLI(t *testing.T, root, carry, databaseURL, serverURL, caCertificatePath, configDirectory, userID, spaceID, label string) string {
	t.Helper()
	pending := startCarryCLILogin(t, root, carry, configDirectory, serverURL, caCertificatePath, label)
	approveCarryCLILoginHTTP(t, databaseURL, serverURL, caCertificatePath, userID, spaceID, pending.code)
	return finishCarryCLILogin(t, pending)
}

type pendingMachineConnection struct {
	command     *exec.Cmd
	log         *lockedBuffer
	code        string
	fingerprint string
	done        <-chan error
}

func startCarryMachineConnection(t *testing.T, root, carry, configDirectory, serverURL, caCertificatePath, name string) pendingMachineConnection {
	t.Helper()
	log := &lockedBuffer{}
	command := exec.CommandContext(t.Context(), carry, "host", "connect", "--server", serverURL, "--ca-cert", caCertificatePath, "--name", name)
	command.Dir = root
	command.Env = append(os.Environ(), "CARRY_CONFIG_DIR="+configDirectory)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatalf("start Machine connection: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	codePattern := regexp.MustCompile(`Code: ([BCDFGHJKLMNPQRSTVWXZ-]+)`)
	fingerprintPattern := regexp.MustCompile(`Public key: (SHA256:[A-Za-z0-9+/]+)`)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		codeMatch := codePattern.FindStringSubmatch(log.String())
		fingerprintMatch := fingerprintPattern.FindStringSubmatch(log.String())
		if len(codeMatch) == 2 && len(fingerprintMatch) == 2 {
			return pendingMachineConnection{
				command: command, log: log, code: codeMatch[1], fingerprint: fingerprintMatch[1], done: done,
			}
		}
		select {
		case err := <-done:
			t.Fatalf("carry host connect stopped before showing a code: %v\n%s", err, log.String())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("carry host connect did not show a code\n%s", log.String())
	return pendingMachineConnection{}
}

func approveCarryMachineHTTP(t *testing.T, databaseURL, serverURL, caCertificatePath, userID, spaceID, code string) {
	t.Helper()
	sessionID := uuid.NewString()
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(t.Context(), `insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1, $2, 'email', transaction_timestamp() + interval '1 hour')`, sessionID, userID); err != nil {
		t.Fatalf("create Machine approval Browser Session: %v", err)
	}
	caPEM, err := os.ReadFile(caCertificatePath)
	if err != nil {
		t.Fatal(err)
	}
	client, err := testHTTPClient(caPEM)
	if err != nil {
		t.Fatal(err)
	}
	identityCredentials, _ := identity.NewCredentials(bytes.Repeat([]byte{3}, identity.IdentityRootBytes))
	cookie, _ := identityCredentials.BrowserSessionCredential(sessionID)
	post := func(path string, body any, key string, destination any) {
		encoded, _ := json.Marshal(body)
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, serverURL+path, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", serverURL)
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		request.AddCookie(&http.Cookie{Name: "__Host-carry_session", Value: cookie})
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("send Machine approval: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			failure, _ := io.ReadAll(response.Body)
			t.Fatalf("Machine approval %s = %d: %s", path, response.StatusCode, failure)
		}
		if destination != nil {
			if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
				t.Fatal(err)
			}
		}
	}
	var preview struct {
		RequestID   string `json:"request_id"`
		UserCode    string `json:"user_code"`
		Fingerprint string `json:"fingerprint"`
		DisplayName string `json:"display_name"`
	}
	post("/v1/machine-connections/lookup", map[string]string{"user_code": code}, "", &preview)
	if preview.UserCode != code || !strings.HasPrefix(preview.Fingerprint, "SHA256:") || preview.DisplayName == "" {
		t.Fatalf("Machine preview differs from terminal: %#v", preview)
	}
	post("/v1/machine-connections/"+preview.RequestID+"/approve", map[string]string{
		"request_id": preview.RequestID, "user_code": code, "space_id": spaceID,
	}, uuid.NewString(), nil)
}

func finishCarryMachineConnection(t *testing.T, pending pendingMachineConnection) string {
	t.Helper()
	select {
	case err := <-pending.done:
		if err != nil {
			t.Fatalf("complete Machine connection: %v\n%s", err, pending.log.String())
		}
	case <-time.After(35 * time.Second):
		_ = pending.command.Process.Kill()
		t.Fatalf("Machine connection timed out\n%s", pending.log.String())
	}
	output := pending.log.String()
	if !strings.Contains(output, " connected to Space ") {
		t.Fatalf("Machine connection output = %q", output)
	}
	return output
}

func connectCarryMachine(t *testing.T, root, carry, databaseURL, serverURL, caCertificatePath, configDirectory, userID, spaceID, name string) string {
	t.Helper()
	pending := startCarryMachineConnection(t, root, carry, configDirectory, serverURL, caCertificatePath, name)
	approveCarryMachineHTTP(t, databaseURL, serverURL, caCertificatePath, userID, spaceID, pending.code)
	return finishCarryMachineConnection(t, pending)
}

func startServer(
	t *testing.T,
	root string,
	carryServer string,
	address string,
	databaseURL string,
	pkiDirectory string,
) (func(), *lockedBuffer, string) {
	t.Helper()
	return startServerWithOrigin(t, root, carryServer, address, databaseURL, pkiDirectory, "https://"+address)
}

func startServerWithOrigin(
	t *testing.T,
	root string,
	carryServer string,
	address string,
	databaseURL string,
	pkiDirectory string,
	externalOrigin string,
) (func(), *lockedBuffer, string) {
	t.Helper()
	captureFile := filepath.Join(t.TempDir(), "latest-email-code")
	resendFixture := newResendFixture(t, captureFile)
	serverCtx, cancel := context.WithCancel(t.Context())
	serverLog := &lockedBuffer{}
	serverCommand := exec.CommandContext(serverCtx, carryServer, "serve", "--listen", address)
	serverCommand.Dir = root
	identityRoot := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	serverCommand.Env = append(os.Environ(),
		"CARRY_DATABASE_URL="+databaseURL,
		"CARRY_PKI_DIR="+pkiDirectory,
		"CARRY_IDENTITY_ROOT="+identityRoot,
		"CARRY_EXTERNAL_ORIGIN="+externalOrigin,
		"CARRY_GOOGLE_CLIENT_ID=test-google-client",
		"CARRY_GOOGLE_CLIENT_SECRET=test-google-secret",
		"CARRY_GITHUB_CLIENT_ID=test-github-client",
		"CARRY_GITHUB_CLIENT_SECRET=test-github-secret",
		"CARRY_RESEND_API_KEY=test-restricted-key",
		"CARRY_RESEND_API_URL="+resendFixture.URL,
		"CARRY_EMAIL_FROM=Carry <login@example.com>",
	)
	serverCommand.Stdout = serverLog
	serverCommand.Stderr = serverLog
	if err := serverCommand.Start(); err != nil {
		resendFixture.Close()
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
		resendFixture.Close()
	}
	return stop, serverLog, captureFile
}

func newResendFixture(t *testing.T, captureFile string) *httptest.Server {
	t.Helper()
	var mutex sync.Mutex
	accepted := map[string][]byte{}
	codePattern := regexp.MustCompile(`\b[0-9]{6}\b`)
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/emails" ||
			request.Header.Get("Authorization") != "Bearer test-restricted-key" {
			http.Error(response, "rejected", http.StatusUnauthorized)
			return
		}
		var body struct {
			To   []string `json:"to"`
			Text string   `json:"text"`
		}
		encoded, err := io.ReadAll(http.MaxBytesReader(response, request.Body, 64<<10))
		if err != nil || json.Unmarshal(encoded, &body) != nil || len(body.To) != 1 {
			http.Error(response, "invalid", http.StatusBadRequest)
			return
		}
		key := request.Header.Get("Idempotency-Key")
		mutex.Lock()
		previous, exists := accepted[key]
		if exists && !bytes.Equal(previous, encoded) {
			mutex.Unlock()
			http.Error(response, "idempotency conflict", http.StatusConflict)
			return
		}
		accepted[key] = append([]byte(nil), encoded...)
		mutex.Unlock()
		if code := codePattern.FindString(body.Text); code != "" {
			temporary := captureFile + ".tmp"
			if err := os.WriteFile(temporary, []byte(code), 0o600); err != nil || os.Rename(temporary, captureFile) != nil {
				http.Error(response, "capture failed", http.StatusInternalServerError)
				return
			}
		}
		writeJSONFixture(response, map[string]string{"id": "resend-fixture-" + key})
	}))
}

func writeJSONFixture(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(value)
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

func attachTestEmailIdentity(t *testing.T, databaseURL string, userID string, email string) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open email Identity fixture database: %v", err)
	}
	defer pool.Close()
	transaction, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin email Identity fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(t.Context()) }()
	if _, err := transaction.Exec(t.Context(), `
		delete from email_identities
		where user_id = $2 and canonical_email <> $1
	`, email, userID); err != nil {
		t.Fatalf("remove prior email Identity fixture: %v", err)
	}
	if _, err := transaction.Exec(t.Context(), `
		insert into email_identities (canonical_email, user_id)
		values ($1, $2)
		on conflict (canonical_email) do update set user_id = excluded.user_id
	`, email, userID); err != nil {
		t.Fatalf("attach email Identity fixture: %v", err)
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatalf("commit email Identity fixture: %v", err)
	}
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
		"machine_connection_lookup_failures",
		"machine_connection_requests",
		"cli_login_lookup_failures",
		"email_login_attempts",
		"email_login_challenges",
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
