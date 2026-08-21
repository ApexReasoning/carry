package login

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
)

const testPollProof = "carry_cli_poll_11111111-1111-4111-8111-111111111111.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testFinalCredential = "carry_cli_22222222-2222-4222-8222-222222222222.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestRunCancellationConfirmsServerBeforeRemovingPendingProof(t *testing.T) {
	begun := make(chan struct{})
	cancelled := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli-logins":
			var body struct {
				RequestID string `json:"request_id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				http.Error(response, "invalid", http.StatusBadRequest)
				return
			}
			writeLoginJSON(response, http.StatusCreated, begunLogin{
				RequestID: body.RequestID, UserCode: "BCDF-GHJ-KLM",
				PollSecret:       "carry_cli_poll_" + body.RequestID + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				VerificationPath: "/cli-login", ExpiresAt: time.Now().Add(time.Minute), IntervalSeconds: 5,
			})
			select {
			case <-begun:
			default:
				close(begun)
			}
		case "/v1/cli-logins/cancel":
			cancelled <- struct{}{}
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	caPath := writeTestCA(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-begun
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var output bytes.Buffer
	directory := t.TempDir()
	err := run(ctx, directory, &output, loginFlags{serverURL: server.URL, label: "Desk CLI", caCertificatePath: caPath})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled login error = %v", err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("CLI did not submit cancellation")
	}
	if _, err := credentialfile.LoadPending(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed cancellation retained poll proof: %v", err)
	}
	if !strings.Contains(output.String(), "Login canceled") {
		t.Fatalf("cancellation output = %q", output.String())
	}
}

func TestRunCancellationRetainsPendingProofWhenOutcomeIsUnknown(t *testing.T) {
	begun := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli-logins":
			var body struct {
				RequestID string `json:"request_id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				http.Error(response, "invalid", http.StatusBadRequest)
				return
			}
			writeLoginJSON(response, http.StatusCreated, begunLogin{
				RequestID: body.RequestID, UserCode: "BCDF-GHJ-KLM",
				PollSecret:       "carry_cli_poll_" + body.RequestID + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				VerificationPath: "/cli-login", ExpiresAt: time.Now().Add(time.Minute), IntervalSeconds: 5,
			})
			select {
			case <-begun:
			default:
				close(begun)
			}
		case "/v1/cli-logins/cancel":
			http.Error(response, "unknown", http.StatusInternalServerError)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	caPath := writeTestCA(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-begun
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var output bytes.Buffer
	directory := t.TempDir()
	err := run(ctx, directory, &output, loginFlags{serverURL: server.URL, label: "Desk CLI", caCertificatePath: caPath})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled login error = %v", err)
	}
	if _, err := credentialfile.LoadPending(directory); err != nil {
		t.Fatalf("unknown cancellation removed pending proof: %v", err)
	}
	if !strings.Contains(output.String(), "Cancellation is unknown") {
		t.Fatalf("unknown cancellation output = %q", output.String())
	}
}

func TestRunResumesPendingRedeemAfterLocalPublicationFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli-logins/poll":
			if request.Header.Get("X-Carry-CLI-Poll") != testPollProof {
				http.Error(response, "invalid", http.StatusUnauthorized)
				return
			}
			writeLoginJSON(response, http.StatusOK, redeemedLogin{
				CredentialID: "22222222-2222-4222-8222-222222222222", Credential: testFinalCredential,
				UserID: "33333333-3333-4333-8333-333333333333", SpaceID: "44444444-4444-4444-8444-444444444444",
				Label: "Desk CLI", ExpiresAt: time.Now().Add(90 * 24 * time.Hour),
			})
		case "/v1/me":
			if request.Header.Get("Authorization") != "Bearer "+testFinalCredential {
				http.Error(response, "invalid", http.StatusUnauthorized)
				return
			}
			writeLoginJSON(response, http.StatusOK, map[string]any{
				"user_id": "33333333-3333-4333-8333-333333333333", "display_name": "Ada",
				"spaces": []map[string]any{{
					"space_id": "44444444-4444-4444-8444-444444444444", "name": "Research",
					"can_manage_members": false, "can_enroll_machines": false,
				}},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	directory := t.TempDir()
	ca := string(testCAPEM(t, server))
	if err := credentialfile.SavePending(directory, credentialfile.PendingLogin{
		ServerURL: server.URL, CACertificatePEM: ca, RequestID: "11111111-1111-4111-8111-111111111111",
		UserCode: "BCDF-GHJ-KLM", PollSecret: testPollProof, VerificationPath: "/cli-login",
		Label: "Desk CLI", ExpiresAt: time.Now().Add(time.Minute), IntervalSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte(ca), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), directory, &output, loginFlags{serverURL: server.URL, label: "Desk CLI", caCertificatePath: caPath}); err != nil {
		t.Fatalf("resume pending login: %v", err)
	}
	if _, err := credentialfile.Load(directory); err != nil {
		t.Fatalf("load recovered credential: %v", err)
	}
	if _, err := credentialfile.LoadPending(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending proof remains after recovery: %v", err)
	}
}

func TestLogoutRetainsCredentialUntilRemoteRevocationIsConfirmed(t *testing.T) {
	var status atomic.Int64
	status.Store(http.StatusInternalServerError)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/cli-credentials/current/revoke" || request.Header.Get("Authorization") != "Bearer "+testFinalCredential {
			http.Error(response, "invalid", http.StatusUnauthorized)
			return
		}
		response.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	directory := t.TempDir()
	credential := credentialfile.Credential{
		ServerURL: server.URL, CACertificatePEM: string(testCAPEM(t, server)), Credential: testFinalCredential,
		CredentialID: "22222222-2222-4222-8222-222222222222", UserID: "33333333-3333-4333-8333-333333333333",
		DefaultSpaceID: "44444444-4444-4444-8444-444444444444", Label: "Desk CLI", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := credentialfile.Save(directory, credential); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := NewLogoutCommand(directory, &output)
	command.SetArgs(nil)
	if err := command.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "local credential retained") {
		t.Fatalf("ambiguous logout error = %v", err)
	}
	retained, err := credentialfile.Load(directory)
	if err != nil || retained.LogoutIdempotencyKey == "" {
		t.Fatalf("logout retry material = %#v, %v", retained, err)
	}
	status.Store(http.StatusNoContent)
	command = NewLogoutCommand(directory, &output)
	command.SetArgs(nil)
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("confirmed logout: %v", err)
	}
	if _, err := credentialfile.Load(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed logout retained credential: %v", err)
	}
}

func writeTestCA(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, testCAPEM(t, server), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testCAPEM(t *testing.T, server *httptest.Server) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}
