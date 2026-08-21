package identity

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCLICredentialsKeepThreeAudiencesAndBindFinalCredentialToOrigin(t *testing.T) {
	credentials, err := NewCredentials(bytes.Repeat([]byte{4}, IdentityRootBytes))
	if err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	requestID := uuid.NewString()
	code := credentials.CLIUserCode(requestID, 0)
	normalized, ok := NormalizeCLIUserCode(code)
	if !ok || normalized != code || len(code) != 12 {
		t.Fatalf("CLI code = %q, normalized = %q", code, normalized)
	}
	poll, err := credentials.CLIPollCredential(requestID, "https://carry.example")
	if err != nil {
		t.Fatalf("create poll credential: %v", err)
	}
	if parsed, ok := credentials.ParseCLIPollCredential(poll, "https://carry.example"); !ok || parsed != requestID {
		t.Fatal("poll credential did not round trip")
	}
	if _, ok := credentials.ParseCLIPollCredential(poll, "https://other.example"); ok {
		t.Fatal("poll credential crossed server origin")
	}
	credentialID := uuid.NewString()
	final, err := credentials.CLICredential(credentialID, "https://carry.example")
	if err != nil {
		t.Fatalf("create final credential: %v", err)
	}
	if final == poll || final == code {
		t.Fatal("CLI credential audiences overlapped")
	}
	if parsed, ok := credentials.ParseCLICredential(final, "https://carry.example"); !ok || parsed != credentialID {
		t.Fatal("final credential did not round trip")
	}
	if _, ok := credentials.ParseCLICredential(final, "https://other.example"); ok {
		t.Fatal("final credential crossed server origin")
	}
	if _, ok := credentials.ParseCLICredential(poll, "https://carry.example"); ok {
		t.Fatal("poll credential authenticated as final CLI credential")
	}
}

func TestCLILoginBeginReplaysDerivedCodeAndPollCredential(t *testing.T) {
	credentials, _ := NewCredentials(bytes.Repeat([]byte{5}, IdentityRootBytes))
	persistence := &recordingCLILoginPersistence{}
	login, err := NewCLILogin(persistence, credentials, "https://carry.example")
	if err != nil {
		t.Fatalf("create CLI login: %v", err)
	}
	request := BeginCLILoginRequest{RequestID: uuid.NewString(), IdempotencyKey: uuid.NewString(), Label: "Desk CLI", Source: "127.0.0.1"}
	first, err := login.Begin(context.Background(), request)
	if err != nil {
		t.Fatalf("begin CLI login: %v", err)
	}
	second, err := login.Begin(context.Background(), request)
	if err != nil {
		t.Fatalf("replay CLI login: %v", err)
	}
	if first.UserCode != second.UserCode || first.PollSecret != second.PollSecret || first.RequestID != second.RequestID {
		t.Fatalf("begin replay changed: %#v then %#v", first, second)
	}
	if persistence.begins != 2 {
		t.Fatalf("begin calls = %d", persistence.begins)
	}
}

type recordingCLILoginPersistence struct {
	begins int
	stored CLILoginRequest
}

func (store *recordingCLILoginPersistence) BeginCLILogin(_ context.Context, command BeginCLILoginCommand) (CLILoginRequest, error) {
	store.begins++
	if store.stored.RequestID == "" {
		store.stored = CLILoginRequest{RequestID: command.RequestID, Label: command.Label, CodeGeneration: command.CodeGeneration, PollInterval: CLILoginInitialInterval}
	}
	return store.stored, nil
}
func (*recordingCLILoginPersistence) LookupCLILogin(context.Context, LookupCLILoginCommand) (CLILoginRequest, error) {
	return CLILoginRequest{}, errors.New("unused")
}
func (*recordingCLILoginPersistence) ApproveCLILogin(context.Context, ApproveCLILoginCommand) (CLILoginRequest, error) {
	return CLILoginRequest{}, errors.New("unused")
}
func (*recordingCLILoginPersistence) DenyCLILogin(context.Context, DenyCLILoginCommand) error {
	return errors.New("unused")
}
func (*recordingCLILoginPersistence) PollCLILogin(context.Context, PollCLILoginCommand) (RedeemedCLICredential, error) {
	return RedeemedCLICredential{}, errors.New("unused")
}
func (*recordingCLILoginPersistence) CancelCLILogin(context.Context, CancelCLILoginCommand) error {
	return errors.New("unused")
}
func (*recordingCLILoginPersistence) ListCLICredentials(context.Context, ListCLICredentialsCommand) ([]CLICredential, error) {
	return nil, errors.New("unused")
}
func (*recordingCLILoginPersistence) RevokeCLICredential(context.Context, RevokeCLICredentialCommand) error {
	return errors.New("unused")
}
