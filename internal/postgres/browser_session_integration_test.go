//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/google/uuid"
)

func TestEmailBrowserSessionAuthenticatesAndCanBeRevoked(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	challengeID, code := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "browser@example.com", "request-browser")
	session, err := verifyEmail(t, ctx, store, credentials, challengeID, code, "verify-browser", uuid.NewString())
	if err != nil {
		t.Fatalf("verify email: %v", err)
	}
	authenticated, err := store.AuthenticateBrowserSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("authenticate Browser Session: %v", err)
	}
	expectedName, err := identity.FallbackDisplayName(session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.UserID != session.UserID || authenticated.DisplayName != expectedName {
		t.Fatalf("authenticated User = %#v", authenticated)
	}
	credential, err := credentials.BrowserSessionCredential(session.SessionID)
	if err != nil {
		t.Fatalf("create Browser Session credential: %v", err)
	}
	var storedCredential bool
	if err := pool.QueryRow(ctx, `
		select exists(
			select 1 from browser_sessions
			where session_id::text = $1
		)
	`, credential).Scan(&storedCredential); err != nil {
		t.Fatalf("inspect Browser Session storage: %v", err)
	}
	if storedCredential {
		t.Fatal("Browser Session cookie credential was stored")
	}
	if err := store.RevokeBrowserSession(ctx, session.SessionID); err != nil {
		t.Fatalf("revoke Browser Session: %v", err)
	}
	if _, err := store.AuthenticateBrowserSession(ctx, session.SessionID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("revoked Browser Session error = %v", err)
	}
}
