//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
)

func TestBrowserSessionStoresOnlyHashAndFollowsSourceTokenRevocation(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Mary", SpaceName: "Browser Research", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	session, err := store.CreateBrowserSession(ctx, bootstrap.UserToken, time.Now().Add(8*time.Hour))
	if err != nil {
		t.Fatalf("create browser session: %v", err)
	}
	if !session.ExpiresAt.Before(time.Now().Add(2 * time.Hour)) {
		t.Fatalf("session expiry %s exceeds source token expiry", session.ExpiresAt)
	}
	authenticated, err := store.AuthenticateBrowserSession(ctx, session.Secret)
	if err != nil {
		t.Fatalf("authenticate browser session: %v", err)
	}
	if authenticated.UserID != bootstrap.UserID {
		t.Fatalf("browser session user = %s, want %s", authenticated.UserID, bootstrap.UserID)
	}
	var plaintextMatches int
	if err := pool.QueryRow(ctx, `
		select count(*) from browser_sessions where session_digest = convert_to($1, 'UTF8')
	`, session.Secret).Scan(&plaintextMatches); err != nil {
		t.Fatalf("inspect browser session storage: %v", err)
	}
	if plaintextMatches != 0 {
		t.Fatal("browser session plaintext was stored")
	}
	if _, err := pool.Exec(ctx, `
		update user_tokens set revoked_at = transaction_timestamp()
		where user_id = $1
	`, bootstrap.UserID); err != nil {
		t.Fatalf("revoke source token: %v", err)
	}
	if _, err := store.AuthenticateBrowserSession(ctx, session.Secret); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("session after source token revocation error = %v", err)
	}
}

func TestBrowserSessionCanBeRevoked(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	bootstrap, err := store.Bootstrap(ctx, BootstrapCommand{
		DisplayName: "Betty", SpaceName: "Session Research", TokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	session, err := store.CreateBrowserSession(ctx, bootstrap.UserToken, time.Now().Add(30*time.Minute))
	if err != nil {
		t.Fatalf("create browser session: %v", err)
	}
	if err := store.RevokeBrowserSession(ctx, session.Secret); err != nil {
		t.Fatalf("revoke browser session: %v", err)
	}
	if _, err := store.AuthenticateBrowserSession(ctx, session.Secret); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("revoked browser session error = %v", err)
	}
}
