package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMethodsUnlinkBuildsExactDatabaseCommand(t *testing.T) {
	t.Parallel()
	credentials := identityMethodTestCredentials(t)
	persistence := &recordingIdentityMethodPersistence{}
	methods, err := NewMethods(persistence, credentials)
	if err != nil {
		t.Fatalf("compose Identity methods: %v", err)
	}
	sessionID := uuid.NewString()
	result, err := methods.Unlink(context.Background(), UnlinkMethodCommand{
		SessionID: sessionID, Method: GoogleMethod, IdempotencyKey: "remove-google",
	})
	if err != nil {
		t.Fatalf("unlink Google: %v", err)
	}
	command := persistence.unlink
	if command.InitiatingSessionID != sessionID || command.Method != GoogleMethod ||
		command.IdempotencyKey != "remove-google" || uuid.Validate(command.ReplacementSessionID) != nil {
		t.Fatalf("unlink command = %#v", command)
	}
	expected := credentials.RequestDigest("unlink-identity-method", sessionID, string(GoogleMethod))
	if command.RequestDigest != expected || result.SessionID != command.ReplacementSessionID {
		t.Fatalf("unlink digest/session = %x/%#v", command.RequestDigest, result)
	}
}

func TestMethodsRejectsMalformedUnlinkBeforePersistence(t *testing.T) {
	t.Parallel()
	methods, err := NewMethods(&recordingIdentityMethodPersistence{}, identityMethodTestCredentials(t))
	if err != nil {
		t.Fatalf("compose Identity methods: %v", err)
	}
	for _, command := range []UnlinkMethodCommand{
		{SessionID: "invalid", Method: EmailMethod, IdempotencyKey: "key"},
		{SessionID: uuid.NewString(), Method: Method("provider"), IdempotencyKey: "key"},
		{SessionID: uuid.NewString(), Method: EmailMethod},
	} {
		if _, err := methods.Unlink(context.Background(), command); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("malformed command %#v error = %v", command, err)
		}
	}
}

func identityMethodTestCredentials(t *testing.T) Credentials {
	t.Helper()
	credentials, err := NewCredentials(make([]byte, IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}
	return credentials
}

type recordingIdentityMethodPersistence struct {
	unlink UnlinkIdentityMethodCommand
}

func (*recordingIdentityMethodPersistence) ListIdentityMethods(context.Context, string, string) (IdentityMethods, error) {
	return IdentityMethods{}, nil
}

func (persistence *recordingIdentityMethodPersistence) UnlinkIdentityMethod(
	_ context.Context,
	command UnlinkIdentityMethodCommand,
) (BrowserSession, error) {
	persistence.unlink = command
	return BrowserSession{
		SessionID: command.ReplacementSessionID,
		UserID:    "user",
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}
