package work

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
)

func TestRetryCommandReusesUnknownIdentityBeforeANewChoice(t *testing.T) {
	t.Parallel()

	const (
		spaceID = "11111111-1111-4111-8111-111111111111"
		workID  = "22222222-2222-4222-8222-222222222222"
	)
	var retryKeys []string
	needsRetry := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/me":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"user_id":"member-1","spaces":[{"space_id":%q,"name":"Research","can_enroll_machines":true}]}`, spaceID)
		case "/v1/spaces/" + spaceID + "/works/" + workID + "/retry":
			retryKeys = append(retryKeys, request.Header.Get("Idempotency-Key"))
			// Go's Transport can replay an Idempotency-Key request once when a
			// reused connection disappears. Drop both transport-level sends for
			// each of the client's two bounded mutation attempts.
			if len(retryKeys) <= 4 {
				connection, _, err := response.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack lost retry response: %v", err)
					return
				}
				_ = connection.Close()
				return
			}
			if len(retryKeys) == 6 {
				needsRetry = false
			}
			response.WriteHeader(http.StatusNoContent)
		case "/v1/spaces/" + spaceID + "/works/" + workID:
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"work":{"work_id":%q,"space_id":%q,"goal":"Review renewals","lifecycle":"open","owner_user_id":"member-1","creator_user_id":"member-1","understanding":"","next_step":"","has_unapplied_input":true,"needs_retry":%t,"created_at":"2026-08-19T00:00:00Z"},"messages":[]}`, workID, spaceID, needsRetry)
		default:
			http.Error(response, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	configDirectory := t.TempDir()
	if err := credentialfile.Save(configDirectory, credentialfile.Credential{
		ServerURL: server.URL, Credential: "carry_cli_33333333-3333-4333-8333-333333333333.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", CACertificatePEM: string(certificatePEM),
		CredentialID: "33333333-3333-4333-8333-333333333333", UserID: "44444444-4444-4444-8444-444444444444",
		DefaultSpaceID: spaceID, Label: "test CLI", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save member credential: %v", err)
	}

	runRetry := func() (string, error) {
		var output bytes.Buffer
		command := newRetryCommand(configDirectory, &output)
		command.SetArgs([]string{workID})
		err := command.ExecuteContext(context.Background())
		return output.String(), err
	}
	if _, err := runRetry(); err == nil || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("first retry error = %v, want unknown outcome", err)
	}
	if _, err := runRetry(); err == nil || !strings.Contains(err.Error(), "needs a new choice") {
		t.Fatalf("reconciled retry error = %v, want new explicit choice", err)
	}
	output, err := runRetry()
	if err != nil {
		t.Fatalf("new explicit retry: %v", err)
	}
	if !strings.Contains(output, "Carry will try Work") {
		t.Fatalf("retry output = %q", output)
	}
	if len(retryKeys) != 6 || retryKeys[0] == "" {
		t.Fatalf("reconciled retry keys = %#v", retryKeys)
	}
	for index := 1; index < 5; index++ {
		if retryKeys[index] != retryKeys[0] {
			t.Fatalf("old retry changed identity: %#v", retryKeys)
		}
	}
	if retryKeys[5] == retryKeys[0] {
		t.Fatalf("new member choice reused old retry key: %#v", retryKeys)
	}
}
