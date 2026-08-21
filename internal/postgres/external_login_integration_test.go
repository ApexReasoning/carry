//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/google/uuid"
)

func TestExternalLoginPersistsOpaqueInvitationContinuationAcrossOutcomeAndReplay(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	invitationID := uuid.NewString()
	transactionID := uuid.NewString()
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: transactionID,
		Provider:      identity.GitHubLoginProvider,
		Purpose:       identity.LoginPurpose,
		InvitationID:  invitationID,
	}); err != nil {
		t.Fatal(err)
	}
	digest := testEmailCredentials(t).RequestDigest("external-login-callback", "invitation-success")
	claim, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID:  transactionID,
		Provider:       identity.GitHubLoginProvider,
		CallbackDigest: digest,
		Outcome:        identity.ExternalCallbackCode,
	})
	if err != nil || claim.InvitationID != invitationID {
		t.Fatalf("claim continuation = %#v, %v", claim, err)
	}
	completed, err := store.CompleteGitHubLogin(ctx, identity.CompleteGitHubLoginCommand{
		TransactionID:  transactionID,
		CallbackDigest: digest,
		GitHubUserID:   140001,
		SessionID:      uuid.NewString(),
	})
	if err != nil || completed.InvitationID != invitationID {
		t.Fatalf("completion continuation = %#v, %v", completed, err)
	}
	replayed, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID:  transactionID,
		Provider:       identity.GitHubLoginProvider,
		CallbackDigest: digest,
		Outcome:        identity.ExternalCallbackCode,
	})
	if err != nil {
		t.Fatalf("replay continuation: %v", err)
	}
	if !replayed.IsReplay {
		t.Fatalf("replay claim = %#v", replayed)
	}
	if replayed.InvitationID != invitationID {
		t.Fatalf("replay continuation = %#v", replayed)
	}
	if replayed.Session.InvitationID != invitationID {
		t.Fatalf("replay Session continuation = %#v", replayed)
	}

	deniedID := uuid.NewString()
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: deniedID,
		Provider:      identity.GoogleLoginProvider,
		Purpose:       identity.LoginPurpose,
		InvitationID:  invitationID,
	}); err != nil {
		t.Fatal(err)
	}
	deniedDigest := testEmailCredentials(t).RequestDigest("external-login-callback", "invitation-denied")
	denied, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID:  deniedID,
		Provider:       identity.GoogleLoginProvider,
		CallbackDigest: deniedDigest,
		Outcome:        identity.ExternalCallbackDenied,
	})
	if !errors.Is(err, identity.ErrExternalLoginDenied) || denied.InvitationID != invitationID {
		t.Fatalf("denied continuation = %#v, %v", denied, err)
	}
}

func TestExternalLoginExactReplayReturnsSameActiveBrowserSession(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	transactionID, digest := claimedExternalLogin(t, ctx, store, credentials, identity.GoogleLoginProvider, "exact-replay")
	first, err := store.CompleteGoogleLogin(ctx, identity.CompleteGoogleLoginCommand{
		TransactionID: transactionID, CallbackDigest: digest,
		Issuer: "https://accounts.google.com", Subject: "replay-subject", SessionID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("complete Google login: %v", err)
	}
	replayed, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: transactionID, Provider: identity.GoogleLoginProvider,
		CallbackDigest: digest, Outcome: identity.ExternalCallbackCode,
	})
	if err != nil || !replayed.IsReplay || replayed.Session != first {
		t.Fatalf("replayed login = %#v, %v; first = %#v", replayed, err, first)
	}
	if err := store.RevokeBrowserSession(ctx, first.SessionID); err != nil {
		t.Fatalf("revoke replayed Browser Session: %v", err)
	}
	if _, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: transactionID, Provider: identity.GoogleLoginProvider,
		CallbackDigest: digest, Outcome: identity.ExternalCallbackCode,
	}); !errors.Is(err, identity.ErrExternalLoginInvalid) {
		t.Fatalf("revoked session replay error = %v", err)
	}
	changed := credentials.RequestDigest("external-login-callback", "changed")
	if _, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: transactionID, Provider: identity.GoogleLoginProvider,
		CallbackDigest: changed, Outcome: identity.ExternalCallbackCode,
	}); !errors.Is(err, identity.ErrExternalLoginConflict) {
		t.Fatalf("changed callback replay error = %v", err)
	}
}

func TestConcurrentGoogleFirstLoginConvergesWithoutOrphanUser(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	type command struct {
		transactionID string
		digest        [32]byte
	}
	commands := make([]command, 2)
	for index := range commands {
		transactionID, digest := claimedExternalLogin(
			t, ctx, store, credentials, identity.GoogleLoginProvider, fmt.Sprintf("google-race-%d", index),
		)
		commands[index] = command{transactionID: transactionID, digest: digest}
	}
	start := make(chan struct{})
	results := make(chan identity.BrowserSession, len(commands))
	errorsFound := make(chan error, len(commands))
	var wait sync.WaitGroup
	for _, command := range commands {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			session, err := store.CompleteGoogleLogin(ctx, identity.CompleteGoogleLoginCommand{
				TransactionID: command.transactionID, CallbackDigest: command.digest,
				Issuer: "https://accounts.google.com", Subject: "one-concurrent-subject", SessionID: uuid.NewString(),
			})
			if err != nil {
				errorsFound <- err
				return
			}
			results <- session
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("complete concurrent Google login: %v", err)
	}
	var userID string
	var sessions int
	for session := range results {
		if userID == "" {
			userID = session.UserID
		}
		if session.UserID != userID {
			t.Fatalf("concurrent Google Users = %q and %q", userID, session.UserID)
		}
		sessions++
	}
	if sessions != 2 {
		t.Fatalf("concurrent Browser Sessions = %d", sessions)
	}
	var users, identities, browserSessions int
	if err := pool.QueryRow(ctx, `select count(*) from carry_users`).Scan(&users); err != nil {
		t.Fatalf("count Google Users: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from google_identities`).Scan(&identities); err != nil {
		t.Fatalf("count Google identities: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from browser_sessions`).Scan(&browserSessions); err != nil {
		t.Fatalf("count Google Browser Sessions: %v", err)
	}
	if users != 1 || identities != 1 || browserSessions != 2 {
		t.Fatalf("Users = %d, identities = %d, Browser Sessions = %d", users, identities, browserSessions)
	}
}

func TestConcurrentGitHubFirstLoginConvergesWithoutOrphanUser(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	type command struct {
		transactionID string
		digest        [32]byte
	}
	commands := make([]command, 2)
	for index := range commands {
		transactionID, digest := claimedExternalLogin(
			t, ctx, store, credentials, identity.GitHubLoginProvider, fmt.Sprintf("github-race-%d", index),
		)
		commands[index] = command{transactionID: transactionID, digest: digest}
	}
	start := make(chan struct{})
	results := make(chan identity.BrowserSession, len(commands))
	errorsFound := make(chan error, len(commands))
	var wait sync.WaitGroup
	for _, command := range commands {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			session, err := store.CompleteGitHubLogin(ctx, identity.CompleteGitHubLoginCommand{
				TransactionID: command.transactionID, CallbackDigest: command.digest,
				GitHubUserID: 4242, SessionID: uuid.NewString(),
			})
			if err != nil {
				errorsFound <- err
				return
			}
			results <- session
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("complete concurrent GitHub login: %v", err)
	}
	var userID string
	var sessions int
	for session := range results {
		if userID == "" {
			userID = session.UserID
		}
		if session.UserID != userID {
			t.Fatalf("concurrent GitHub Users = %q and %q", userID, session.UserID)
		}
		sessions++
	}
	var users, identities, browserSessions int
	if err := pool.QueryRow(ctx, `select count(*) from carry_users`).Scan(&users); err != nil {
		t.Fatalf("count GitHub Users: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from github_identities`).Scan(&identities); err != nil {
		t.Fatalf("count GitHub identities: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from browser_sessions`).Scan(&browserSessions); err != nil {
		t.Fatalf("count GitHub Browser Sessions: %v", err)
	}
	if sessions != 2 || users != 1 || identities != 1 || browserSessions != 2 {
		t.Fatalf("sessions = %d, Users = %d, identities = %d, Browser Sessions = %d", sessions, users, identities, browserSessions)
	}
}

func TestGitHubRepeatReplayAndInactiveSessionParity(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)

	firstTransaction, firstDigest := claimedExternalLogin(t, ctx, store, credentials, identity.GitHubLoginProvider, "github-first")
	first, err := store.CompleteGitHubLogin(ctx, identity.CompleteGitHubLoginCommand{
		TransactionID: firstTransaction, CallbackDigest: firstDigest,
		GitHubUserID: 9898, SessionID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("complete first GitHub login: %v", err)
	}
	replayed, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: firstTransaction, Provider: identity.GitHubLoginProvider,
		CallbackDigest: firstDigest, Outcome: identity.ExternalCallbackCode,
	})
	if err != nil || !replayed.IsReplay || replayed.Session != first {
		t.Fatalf("GitHub replay = %#v, %v; first = %#v", replayed, err, first)
	}
	if err := store.RevokeBrowserSession(ctx, first.SessionID); err != nil {
		t.Fatalf("revoke first GitHub session: %v", err)
	}
	if _, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: firstTransaction, Provider: identity.GitHubLoginProvider,
		CallbackDigest: firstDigest, Outcome: identity.ExternalCallbackCode,
	}); !errors.Is(err, identity.ErrExternalLoginInvalid) {
		t.Fatalf("revoked GitHub replay error = %v", err)
	}

	secondTransaction, secondDigest := claimedExternalLogin(t, ctx, store, credentials, identity.GitHubLoginProvider, "github-repeat")
	second, err := store.CompleteGitHubLogin(ctx, identity.CompleteGitHubLoginCommand{
		TransactionID: secondTransaction, CallbackDigest: secondDigest,
		GitHubUserID: 9898, SessionID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("complete repeat GitHub login: %v", err)
	}
	if second.UserID != first.UserID || second.SessionID == first.SessionID {
		t.Fatalf("first = %#v, repeat = %#v", first, second)
	}
	if _, err := pool.Exec(ctx, `
		update browser_sessions
		set created_at = transaction_timestamp() - interval '31 days',
		    expires_at = transaction_timestamp() - interval '1 minute'
		where session_id = $1
	`, second.SessionID); err != nil {
		t.Fatalf("expire repeat GitHub session: %v", err)
	}
	if _, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: secondTransaction, Provider: identity.GitHubLoginProvider,
		CallbackDigest: secondDigest, Outcome: identity.ExternalCallbackCode,
	}); !errors.Is(err, identity.ErrExternalLoginInvalid) {
		t.Fatalf("expired GitHub replay error = %v", err)
	}
}

func TestGoogleGitHubAndEmailIdentitiesRemainSeparateWithoutMembership(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)

	challengeID, code := prepareAcceptedEmailChallenge(
		t, ctx, store, credentials, "same@example.com", "same-email-request",
	)
	emailSession, err := verifyEmail(t, ctx, store, credentials, challengeID, code, "same-email-verify", uuid.NewString())
	if err != nil {
		t.Fatalf("create email User: %v", err)
	}
	googleTransaction, googleDigest := claimedExternalLogin(t, ctx, store, credentials, identity.GoogleLoginProvider, "same-google")
	googleSession, err := store.CompleteGoogleLogin(ctx, identity.CompleteGoogleLoginCommand{
		TransactionID: googleTransaction, CallbackDigest: googleDigest,
		Issuer: "https://accounts.google.com", Subject: "42", SessionID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("complete Google login: %v", err)
	}
	githubTransaction, githubDigest := claimedExternalLogin(t, ctx, store, credentials, identity.GitHubLoginProvider, "same-github")
	githubSession, err := store.CompleteGitHubLogin(ctx, identity.CompleteGitHubLoginCommand{
		TransactionID: githubTransaction, CallbackDigest: githubDigest,
		GitHubUserID: 42, SessionID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("complete GitHub login: %v", err)
	}
	if emailSession.UserID == googleSession.UserID || emailSession.UserID == githubSession.UserID || googleSession.UserID == githubSession.UserID {
		t.Fatalf("email = %s, Google = %s, GitHub = %s", emailSession.UserID, googleSession.UserID, githubSession.UserID)
	}
	var users, memberships int
	if err := pool.QueryRow(ctx, `select count(*) from carry_users`).Scan(&users); err != nil {
		t.Fatalf("count separate Users: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from space_memberships`).Scan(&memberships); err != nil {
		t.Fatalf("count provider Memberships: %v", err)
	}
	if users != 3 || memberships != 0 {
		t.Fatalf("separate Users = %d, Memberships = %d", users, memberships)
	}
}

func TestExternalLoginExchangeHasOneWinnerAndExpiresToUnknown(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	transactionID := uuid.NewString()
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: transactionID, Provider: identity.GitHubLoginProvider,
	}); err != nil {
		t.Fatalf("create external login: %v", err)
	}
	digest := credentials.RequestDigest("external-login-callback", "one-winner")
	command := identity.ClaimExternalLoginCommand{
		TransactionID: transactionID, Provider: identity.GitHubLoginProvider,
		CallbackDigest: digest, Outcome: identity.ExternalCallbackCode,
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.ClaimExternalLogin(ctx, command)
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var winners, conflicts int
	for err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, identity.ErrExternalLoginConflict):
			conflicts++
		default:
			t.Fatalf("concurrent callback claim error = %v", err)
		}
	}
	if winners != 1 || conflicts != 7 {
		t.Fatalf("callback claims = %d winners, %d conflicts", winners, conflicts)
	}
	if _, err := pool.Exec(ctx, `
		update external_login_transactions
		set created_at = transaction_timestamp() - interval '20 minutes',
		    expires_at = transaction_timestamp() - interval '10 minutes'
		where transaction_id = $1
	`, transactionID); err != nil {
		t.Fatalf("expire exchange claim: %v", err)
	}
	if _, err := store.ClaimExternalLogin(ctx, command); !errors.Is(err, identity.ErrExternalLoginUnavailable) {
		t.Fatalf("expired exchange replay error = %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `select status from external_login_transactions where transaction_id = $1`, transactionID).Scan(&status); err != nil {
		t.Fatalf("load expired exchange: %v", err)
	}
	if status != "unknown" {
		t.Fatalf("expired exchange status = %q", status)
	}
}

func claimedExternalLogin(
	t *testing.T,
	ctx context.Context,
	store *Store,
	credentials identity.Credentials,
	provider identity.ExternalLoginProvider,
	name string,
) (string, [32]byte) {
	t.Helper()
	transactionID := uuid.NewString()
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: transactionID, Provider: provider,
	}); err != nil {
		t.Fatalf("create %s external login: %v", name, err)
	}
	digest := credentials.RequestDigest("external-login-callback", name)
	if _, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: transactionID, Provider: provider,
		CallbackDigest: digest, Outcome: identity.ExternalCallbackCode,
	}); err != nil {
		t.Fatalf("claim %s external login: %v", name, err)
	}
	return transactionID, digest
}
