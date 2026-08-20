//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnerReviewsResultProducedThroughNativeExecution(t *testing.T) {
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
	run(
		t, root, clientEnvironment, carry, "login",
		"--server", serverURL,
		"--ca-cert", filepath.Join(pkiDirectory, "ca.pem"),
		"--token", bootstrap.UserToken,
	)
	run(
		t, root, clientEnvironment, carry, "host", "enroll",
		"--space", bootstrap.SpaceID,
		"--name", "native-execution-host",
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
		"CARRY_FAKE_PI_REVIEW_REQUIRED=true",
	)
	hostCtx, cancelHost := context.WithCancel(t.Context())
	hostLog := &lockedBuffer{}
	hostCommand := exec.CommandContext(hostCtx, carry, "host", "start")
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
			for _, forbidden := range []string{`"run_id"`, `"attempt_id"`, `"fence"`, `"machine_id"`, `"input_end_seq"`} {
				if strings.Contains(string(prompt), forbidden) {
					t.Fatalf("Agent prompt leaked authority field %s: %s", forbidden, prompt)
				}
			}

			caCertificate, err := os.ReadFile(filepath.Join(pkiDirectory, "ca.pem"))
			if err != nil {
				t.Fatalf("read test CA: %v", err)
			}
			client, err := testHTTPClient(caCertificate)
			if err != nil {
				t.Fatalf("build test HTTP client: %v", err)
			}
			memberRequest := func(method string, path string, idempotencyKey string) (int, []byte) {
				t.Helper()
				request, err := http.NewRequestWithContext(t.Context(), method, serverURL+path, nil)
				if err != nil {
					t.Fatalf("build member request: %v", err)
				}
				request.Header.Set("Authorization", "Bearer "+bootstrap.UserToken)
				if idempotencyKey != "" {
					request.Header.Set("Idempotency-Key", idempotencyKey)
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatalf("send member request: %v", err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatalf("read member response: %v", err)
				}
				return response.StatusCode, body
			}

			status, body := memberRequest(
				http.MethodGet,
				"/v1/spaces/"+bootstrap.SpaceID+"/works?needs_you=true",
				"",
			)
			var needsYou struct {
				Works []struct {
					WorkID      string `json:"work_id"`
					NeedsReview bool   `json:"needs_review"`
				} `json:"works"`
			}
			if status != http.StatusOK || json.Unmarshal(body, &needsYou) != nil ||
				len(needsYou.Works) != 1 || needsYou.Works[0].WorkID != workID ||
				!needsYou.Works[0].NeedsReview {
				t.Fatalf("Needs You response = %d %s", status, body)
			}

			run(t, root, nil, "pnpm", "--dir", "apps/web", "build")
			webAddress := freeAddress(t)
			stopWeb, webLog := startWeb(t, root, webAddress, serverURL, pkiDirectory)
			defer stopWeb()
			webURL := "https://" + webAddress
			waitForServer(t, webURL, filepath.Join(pkiDirectory, "ca.pem"), webLog)
			playwrightOutput, playwrightErr := runError(
				root,
				[]string{
					"CARRY_WEB_URL=" + webURL,
					"CARRY_MEMBER_TOKEN=" + bootstrap.UserToken,
					"CARRY_REVIEW_WORK_GOAL=Prepare a customer renewal recommendation",
				},
				"pnpm", "--dir", "apps/web", "exec", "playwright", "test", "e2e/result-review.spec.ts",
			)
			if playwrightErr != nil || !strings.Contains(playwrightOutput, "1 passed") {
				t.Fatalf("run result-review browser journey: %v\n%s\nHost log:\n%s", playwrightErr, playwrightOutput, hostLog.String())
			}

			status, body = memberRequest(
				http.MethodGet,
				"/v1/spaces/"+bootstrap.SpaceID+"/works/"+workID,
				"",
			)
			var accepted struct {
				Work struct {
					Lifecycle   string `json:"lifecycle"`
					NeedsReview bool   `json:"needs_review"`
				} `json:"work"`
			}
			if status != http.StatusOK || json.Unmarshal(body, &accepted) != nil ||
				accepted.Work.NeedsReview || accepted.Work.Lifecycle != "open" {
				t.Fatalf("accepted Work response = %d %s", status, body)
			}

			status, body = memberRequest(
				http.MethodGet,
				"/v1/spaces/"+bootstrap.SpaceID+"/works?needs_you=true",
				"",
			)
			if status != http.StatusOK || json.Unmarshal(body, &needsYou) != nil || len(needsYou.Works) != 0 {
				t.Fatalf("settled Needs You response = %d %s", status, body)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Work was not advanced before deadline\nHost log:\n%s\nServer log:\n%s", hostLog.String(), serverLog.String())
}
