//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/google/uuid"
)

func TestIdentityMethodListUnlinkAndExactReplay(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	userID, sessions := seedIdentityUser(t, ctx, store, "unlink@example.com", 2)
	if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', 'unlink-google', $1)`, userID); err != nil {
		t.Fatalf("seed Google method: %v", err)
	}

	listed, err := store.ListIdentityMethods(ctx, userID, sessions[0])
	if err != nil {
		t.Fatalf("list Identity methods: %v", err)
	}
	if fmt.Sprint(listed.Methods) != "[email google]" || listed.ReauthenticationRequired {
		t.Fatalf("listed methods = %#v", listed)
	}
	methods, err := identity.NewMethods(store, credentials)
	if err != nil {
		t.Fatalf("compose Identity methods: %v", err)
	}
	if _, err := methods.Unlink(ctx, identity.UnlinkMethodCommand{
		SessionID: sessions[0], Method: identity.EmailMethod, IdempotencyKey: "remove-proved-email",
	}); !errors.Is(err, identity.ErrRecentIdentityProofRequired) {
		t.Fatalf("unlink method used for current proof error = %v", err)
	}
	command := identity.UnlinkMethodCommand{
		SessionID: sessions[0], Method: identity.GoogleMethod, IdempotencyKey: "remove-google",
	}
	first, err := methods.Unlink(ctx, command)
	if err != nil {
		t.Fatalf("unlink Google: %v", err)
	}
	for _, oldSessionID := range sessions {
		if _, err := store.AuthenticateBrowserSession(ctx, oldSessionID); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("old Session %s authentication error = %v", oldSessionID, err)
		}
	}
	if current, err := store.AuthenticateBrowserSession(ctx, first.SessionID); err != nil || current.UserID != userID {
		t.Fatalf("replacement Session = %#v, %v", current, err)
	}
	replayed, err := methods.Unlink(ctx, command)
	if err != nil || replayed.SessionID != first.SessionID {
		t.Fatalf("exact unlink replay = %#v, %v; first = %#v", replayed, err, first)
	}
	if _, err := methods.Unlink(ctx, identity.UnlinkMethodCommand{
		SessionID: sessions[0], Method: identity.EmailMethod, IdempotencyKey: command.IdempotencyKey,
	}); !errors.Is(err, identity.ErrIdempotencyConflict) {
		t.Fatalf("changed unlink replay error = %v", err)
	}
	if _, err := methods.Unlink(ctx, identity.UnlinkMethodCommand{
		SessionID: first.SessionID, Method: identity.EmailMethod, IdempotencyKey: "remove-final-email",
	}); !errors.Is(err, identity.ErrLastIdentityMethod) {
		t.Fatalf("final-method unlink error = %v", err)
	}
	if err := store.RevokeBrowserSession(ctx, first.SessionID); err != nil {
		t.Fatalf("revoke replacement Session: %v", err)
	}
	if _, err := methods.Unlink(ctx, command); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("revoked replacement replay error = %v", err)
	}
}

func TestConcurrentFinalIdentityMethodUnlinkHasOneWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	userID, sessions := seedIdentityUser(t, ctx, store, "final-race@example.com", 2)
	if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', 'final-race-google', $1)`, userID); err != nil {
		t.Fatalf("seed Google method: %v", err)
	}
	if _, err := pool.Exec(ctx, `update browser_sessions set identity_proof_method = 'google' where session_id = $1`, sessions[0]); err != nil {
		t.Fatalf("set alternate recent proof: %v", err)
	}
	methods, _ := identity.NewMethods(store, credentials)
	commands := []identity.UnlinkMethodCommand{
		{SessionID: sessions[0], Method: identity.EmailMethod, IdempotencyKey: "remove-email"},
		{SessionID: sessions[1], Method: identity.GoogleMethod, IdempotencyKey: "remove-google"},
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, command := range commands {
		wait.Add(1)
		go func(command identity.UnlinkMethodCommand) {
			defer wait.Done()
			<-start
			_, err := methods.Unlink(ctx, command)
			results <- err
		}(command)
	}
	close(start)
	wait.Wait()
	close(results)
	var succeeded, rejected int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, identity.ErrUnauthenticated),
			errors.Is(err, identity.ErrLastIdentityMethod),
			errors.Is(err, identity.ErrRecentIdentityProofRequired):
			rejected++
		default:
			t.Fatalf("concurrent unlink error = %v", err)
		}
	}
	var methodCount, activeSessions int
	if err := pool.QueryRow(ctx, `
		select
			(select count(*) from email_identities where user_id = $1)
			+ (select count(*) from google_identities where user_id = $1)
			+ (select count(*) from github_identities where user_id = $1),
			(select count(*) from browser_sessions where user_id = $1 and revoked_at is null and expires_at > transaction_timestamp())
	`, userID).Scan(&methodCount, &activeSessions); err != nil {
		t.Fatalf("inspect concurrent unlink: %v", err)
	}
	if succeeded != 1 || rejected != 1 || methodCount != 1 || activeSessions != 1 {
		t.Fatalf("outcomes = %d succeeded/%d rejected, methods = %d, active Sessions = %d", succeeded, rejected, methodCount, activeSessions)
	}
}

func TestConcurrentExternalIdentityLinkHasOneOwnerAndReauthenticationCannotSwitchUser(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	firstUser, firstSessions := seedIdentityUser(t, ctx, store, "first-linker@example.com", 1)
	secondUser, secondSessions := seedIdentityUser(t, ctx, store, "second-linker@example.com", 1)

	type proof struct {
		userID        string
		sessionID     string
		transactionID string
		digest        [32]byte
	}
	proofs := []proof{
		newClaimedExternalProof(t, ctx, store, credentials, firstUser, firstSessions[0], identity.GoogleLoginProvider, identity.LinkPurpose, "first-link"),
		newClaimedExternalProof(t, ctx, store, credentials, secondUser, secondSessions[0], identity.GoogleLoginProvider, identity.LinkPurpose, "second-link"),
	}
	start := make(chan struct{})
	type outcome struct {
		proof   proof
		session identity.BrowserSession
		err     error
	}
	results := make(chan outcome, 2)
	for _, item := range proofs {
		go func(item proof) {
			<-start
			session, err := store.CompleteGoogleLogin(ctx, identity.CompleteGoogleLoginCommand{
				TransactionID: item.transactionID, CallbackDigest: item.digest,
				Issuer: "https://accounts.google.com", Subject: "one-link-owner", SessionID: uuid.NewString(),
			})
			results <- outcome{proof: item, session: session, err: err}
		}(item)
	}
	close(start)
	var winner outcome
	var succeeded, occupied int
	for range 2 {
		result := <-results
		if result.err == nil {
			succeeded++
			winner = result
		} else if errors.Is(result.err, identity.ErrIdentityMethodOccupied) {
			occupied++
		} else {
			t.Fatalf("concurrent external link error = %v", result.err)
		}
	}
	var googleOwner string
	if err := pool.QueryRow(ctx, `select user_id from google_identities where subject = 'one-link-owner'`).Scan(&googleOwner); err != nil {
		t.Fatalf("load Google owner: %v", err)
	}
	if succeeded != 1 || occupied != 1 || googleOwner != winner.proof.userID || winner.session.UserID != googleOwner {
		t.Fatalf("outcomes = %d/%d, owner = %s, winner = %#v", succeeded, occupied, googleOwner, winner)
	}

	reauth := newClaimedExternalProof(
		t, ctx, store, credentials, winner.proof.userID, winner.session.SessionID,
		identity.GoogleLoginProvider, identity.ReauthenticatePurpose, "winner-reauthentication",
	)
	confirmed, err := store.CompleteGoogleLogin(ctx, identity.CompleteGoogleLoginCommand{
		TransactionID: reauth.transactionID, CallbackDigest: reauth.digest,
		Issuer: "https://accounts.google.com", Subject: "one-link-owner", SessionID: uuid.NewString(),
	})
	if err != nil || confirmed.UserID != winner.proof.userID || confirmed.SessionID == winner.session.SessionID {
		t.Fatalf("external reauthentication = %#v, %v", confirmed, err)
	}
	loserUser := firstUser
	loserSession := firstSessions[0]
	if loserUser == winner.proof.userID {
		loserUser, loserSession = secondUser, secondSessions[0]
	}
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: uuid.NewString(), Provider: identity.GoogleLoginProvider,
		Purpose: identity.ReauthenticatePurpose, TargetUserID: loserUser, InitiatingSessionID: loserSession,
	}); !errors.Is(err, identity.ErrIdentityMethodNotLinked) {
		t.Fatalf("cross-User reauthentication start error = %v", err)
	}
}

func TestEmailLinkAndReauthenticationRotateWithoutCreatingOrSwitchingUser(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into carry_users (user_id) values ($1)`, userID); err != nil {
		t.Fatalf("seed Google-only User: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', 'email-link-owner', $1)`, userID); err != nil {
		t.Fatalf("seed Google method: %v", err)
	}
	initiating := createIdentityTestSession(t, ctx, store, userID, identity.GoogleMethod)
	challengeID, code := prepareIdentityEmailProof(
		t, ctx, store, credentials, "candidate@example.com", identity.LinkPurpose, userID, initiating, "email-link",
	)
	first, err := verifyIdentityEmailProof(
		ctx, store, credentials, challengeID, code, initiating, identity.LinkPurpose, "verify-email-link",
	)
	if err != nil || first.UserID != userID {
		t.Fatalf("complete email link = %#v, %v", first, err)
	}
	replayed, err := verifyIdentityEmailProof(
		ctx, store, credentials, challengeID, code, initiating, identity.LinkPurpose, "verify-email-link",
	)
	if err != nil || replayed.SessionID != first.SessionID {
		t.Fatalf("replay email link = %#v, %v", replayed, err)
	}
	changedCode := "000000"
	if changedCode == code {
		changedCode = "999999"
	}
	if _, err := verifyIdentityEmailProof(
		ctx, store, credentials, challengeID, changedCode, initiating, identity.LinkPurpose, "verify-email-link",
	); !errors.Is(err, identity.ErrIdempotencyConflict) {
		t.Fatalf("changed email link replay error = %v", err)
	}
	var users int
	if err := pool.QueryRow(ctx, `select count(*) from carry_users`).Scan(&users); err != nil {
		t.Fatalf("count Users after link: %v", err)
	}
	if users != 1 {
		t.Fatalf("Users after email link = %d", users)
	}
	if _, err := pool.Exec(ctx, `
		update email_login_challenges
		set created_at = transaction_timestamp() - interval '2 minutes'
		where challenge_id = $1
	`, challengeID); err != nil {
		t.Fatalf("age linked email challenge: %v", err)
	}

	reauthChallenge, reauthCode := prepareIdentityEmailProof(
		t, ctx, store, credentials, "candidate@example.com", identity.ReauthenticatePurpose,
		userID, first.SessionID, "email-reauthentication",
	)
	confirmed, err := verifyIdentityEmailProof(
		ctx, store, credentials, reauthChallenge, reauthCode, first.SessionID,
		identity.ReauthenticatePurpose, "verify-email-reauthentication",
	)
	if err != nil || confirmed.UserID != userID || confirmed.SessionID == first.SessionID {
		t.Fatalf("email reauthentication = %#v, %v", confirmed, err)
	}
	if _, err := pool.Exec(ctx, `
		update browser_sessions
		set created_at = transaction_timestamp() - interval '31 days',
		    identity_proved_at = transaction_timestamp() - interval '31 days',
		    expires_at = transaction_timestamp() - interval '1 minute'
		where session_id = $1
	`, confirmed.SessionID); err != nil {
		t.Fatalf("expire replacement Session: %v", err)
	}
	if _, err := verifyIdentityEmailProof(
		ctx, store, credentials, reauthChallenge, reauthCode, first.SessionID,
		identity.ReauthenticatePurpose, "verify-email-reauthentication",
	); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("expired email replacement replay error = %v", err)
	}
}

func TestExternalMethodCallbackTerminalOutcomesRetainStoredPurpose(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	userID, sessions := seedIdentityUser(t, ctx, store, "method-outcomes@example.com", 1)

	for _, outcome := range []struct {
		name string
		want error
	}{
		{name: "denied", want: identity.ErrExternalLoginDenied},
		{name: "unavailable", want: identity.ErrExternalLoginUnavailable},
		{name: "rejected", want: identity.ErrExternalLoginRejected},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			transactionID := uuid.NewString()
			if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
				TransactionID: transactionID, Provider: identity.GoogleLoginProvider,
				Purpose: identity.LinkPurpose, TargetUserID: userID, InitiatingSessionID: sessions[0],
			}); err != nil {
				t.Fatalf("create %s method proof: %v", outcome.name, err)
			}
			digest := credentials.RequestDigest("method-callback", outcome.name)
			command := identity.ClaimExternalLoginCommand{
				TransactionID: transactionID, Provider: identity.GoogleLoginProvider, CallbackDigest: digest,
			}
			if outcome.name == "rejected" {
				command.Outcome = identity.ExternalCallbackCode
				if _, err := store.ClaimExternalLogin(ctx, command); err != nil {
					t.Fatalf("claim rejected method proof: %v", err)
				}
				if err := store.RejectExternalLogin(ctx, identity.MarkExternalLoginUnknownCommand{
					TransactionID: transactionID, Provider: identity.GoogleLoginProvider, CallbackDigest: digest,
				}); err != nil {
					t.Fatalf("record rejected method proof: %v", err)
				}
			} else if outcome.name == "denied" {
				command.Outcome = identity.ExternalCallbackDenied
			} else {
				command.Outcome = identity.ExternalCallbackUnavailable
			}
			claim, err := store.ClaimExternalLogin(ctx, command)
			if !errors.Is(err, outcome.want) || claim.Purpose != identity.LinkPurpose {
				t.Fatalf("%s claim = %#v, %v", outcome.name, claim, err)
			}
		})
	}
}

func TestIdentityMethodProofExpiryUsesDatabaseTime(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	userID, sessions := seedIdentityUser(t, ctx, store, "stale-proof@example.com", 1)
	if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', 'stale-proof-google', $1)`, userID); err != nil {
		t.Fatalf("seed stale-proof Google method: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update browser_sessions
		set created_at = transaction_timestamp() - interval '10 minutes 1 second',
		    identity_proved_at = transaction_timestamp() - interval '10 minutes 1 second'
		where session_id = $1
	`, sessions[0]); err != nil {
		t.Fatalf("age Identity proof: %v", err)
	}

	listed, err := store.ListIdentityMethods(ctx, userID, sessions[0])
	if err != nil || !listed.ReauthenticationRequired {
		t.Fatalf("stale proof listing = %#v, %v", listed, err)
	}
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: uuid.NewString(), Provider: identity.GitHubLoginProvider,
		Purpose: identity.LinkPurpose, TargetUserID: userID, InitiatingSessionID: sessions[0],
	}); !errors.Is(err, identity.ErrRecentIdentityProofRequired) {
		t.Fatalf("stale proof link start error = %v", err)
	}
	methods, err := identity.NewMethods(store, credentials)
	if err != nil {
		t.Fatalf("compose Identity methods: %v", err)
	}
	if _, err := methods.Unlink(ctx, identity.UnlinkMethodCommand{
		SessionID: sessions[0], Method: identity.GoogleMethod, IdempotencyKey: "stale-proof-unlink",
	}); !errors.Is(err, identity.ErrRecentIdentityProofRequired) {
		t.Fatalf("stale proof unlink error = %v", err)
	}
	if _, err := store.AuthenticateBrowserSession(ctx, sessions[0]); err != nil {
		t.Fatalf("stale but active Browser Session = %v", err)
	}
}

func TestOccupiedEmailLinkPreservesOwnersMethodsAndSessions(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	ownerUser, ownerSessions := seedIdentityUser(t, ctx, store, "occupied-link@example.com", 1)
	targetUser, _ := seedIdentityUser(t, ctx, store, "", 0)
	if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', 'occupied-email-target', $1)`, targetUser); err != nil {
		t.Fatalf("seed target Google method: %v", err)
	}
	targetSession := createIdentityTestSession(t, ctx, store, targetUser, identity.GoogleMethod)
	challengeID, code := prepareIdentityEmailProof(
		t, ctx, store, credentials, "occupied-link@example.com", identity.LinkPurpose,
		targetUser, targetSession, "occupied-email-link",
	)
	if _, err := verifyIdentityEmailProof(
		ctx, store, credentials, challengeID, code, targetSession,
		identity.LinkPurpose, "verify-occupied-email-link",
	); !errors.Is(err, identity.ErrIdentityMethodOccupied) {
		t.Fatalf("occupied email link error = %v", err)
	}

	var owner string
	if err := pool.QueryRow(ctx, `select user_id from email_identities where canonical_email = 'occupied-link@example.com'`).Scan(&owner); err != nil {
		t.Fatalf("load occupied email owner: %v", err)
	}
	var targetEmailMethods, ownerActiveSessions, targetActiveSessions int
	if err := pool.QueryRow(ctx, `
		select
			(select count(*) from email_identities where user_id = $1),
			(select count(*) from browser_sessions where user_id = $2 and revoked_at is null and expires_at > transaction_timestamp()),
			(select count(*) from browser_sessions where user_id = $1 and revoked_at is null and expires_at > transaction_timestamp())
	`, targetUser, ownerUser).Scan(&targetEmailMethods, &ownerActiveSessions, &targetActiveSessions); err != nil {
		t.Fatalf("inspect occupied email rejection: %v", err)
	}
	if owner != ownerUser || targetEmailMethods != 0 || ownerActiveSessions != len(ownerSessions) || targetActiveSessions != 1 {
		t.Fatalf("owner = %s, target email methods = %d, active Sessions = %d/%d", owner, targetEmailMethods, ownerActiveSessions, targetActiveSessions)
	}
}

func TestConcurrentLoginAndUnlinkShareIdentityThenUserLockOrder(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	methods, err := identity.NewMethods(store, credentials)
	if err != nil {
		t.Fatalf("compose Identity methods: %v", err)
	}

	for _, method := range []identity.Method{identity.EmailMethod, identity.GoogleMethod, identity.GitHubMethod} {
		t.Run(string(method), func(t *testing.T) {
			name := "login-unlink-" + string(method) + "-" + uuid.NewString()
			email := name + "@example.com"
			userID, _ := seedIdentityUser(t, ctx, store, email, 0)
			proofMethod := identity.EmailMethod
			if method == identity.EmailMethod {
				proofMethod = identity.GoogleMethod
				if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', $1, $2)`, name+"-alternate", userID); err != nil {
					t.Fatalf("seed alternate Google method: %v", err)
				}
			} else if method == identity.GoogleMethod {
				if _, err := pool.Exec(ctx, `insert into google_identities (issuer, subject, user_id) values ('https://accounts.google.com', $1, $2)`, name, userID); err != nil {
					t.Fatalf("seed Google method: %v", err)
				}
			} else {
				if _, err := pool.Exec(ctx, `insert into github_identities (github_user_id, user_id) values ($1, $2)`, 900000+time.Now().UnixNano()%100000, userID); err != nil {
					t.Fatalf("seed GitHub method: %v", err)
				}
			}
			initiatingSession := createIdentityTestSession(t, ctx, store, userID, proofMethod)

			var login func(context.Context) (identity.BrowserSession, error)
			switch method {
			case identity.EmailMethod:
				challengeID, code := prepareIdentityEmailProof(
					t, ctx, store, credentials, email, identity.LoginPurpose, "", "", name+"-login",
				)
				login = func(loginCtx context.Context) (identity.BrowserSession, error) {
					return verifyIdentityEmailProof(
						loginCtx, store, credentials, challengeID, code, "", identity.LoginPurpose, name+"-verify",
					)
				}
			case identity.GoogleMethod:
				proof := newClaimedExternalProof(
					t, ctx, store, credentials, "", "", identity.GoogleLoginProvider, identity.LoginPurpose, name,
				)
				login = func(loginCtx context.Context) (identity.BrowserSession, error) {
					return store.CompleteGoogleLogin(loginCtx, identity.CompleteGoogleLoginCommand{
						TransactionID: proof.transactionID, CallbackDigest: proof.digest,
						Issuer: "https://accounts.google.com", Subject: name, SessionID: uuid.NewString(),
					})
				}
			case identity.GitHubMethod:
				var githubID int64
				if err := pool.QueryRow(ctx, `select github_user_id from github_identities where user_id = $1`, userID).Scan(&githubID); err != nil {
					t.Fatalf("load GitHub method: %v", err)
				}
				proof := newClaimedExternalProof(
					t, ctx, store, credentials, "", "", identity.GitHubLoginProvider, identity.LoginPurpose, name,
				)
				login = func(loginCtx context.Context) (identity.BrowserSession, error) {
					return store.CompleteGitHubLogin(loginCtx, identity.CompleteGitHubLoginCommand{
						TransactionID: proof.transactionID, CallbackDigest: proof.digest,
						GitHubUserID: githubID, SessionID: uuid.NewString(),
					})
				}
			}

			raceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			start := make(chan struct{})
			type loginOutcome struct {
				session identity.BrowserSession
				err     error
			}
			loginResult := make(chan loginOutcome, 1)
			unlinkResult := make(chan struct {
				session identity.BrowserSession
				err     error
			}, 1)
			go func() {
				<-start
				session, err := login(raceCtx)
				loginResult <- loginOutcome{session: session, err: err}
			}()
			go func() {
				<-start
				session, err := methods.Unlink(raceCtx, identity.UnlinkMethodCommand{
					SessionID: initiatingSession, Method: method, IdempotencyKey: name + "-unlink",
				})
				unlinkResult <- struct {
					session identity.BrowserSession
					err     error
				}{session: session, err: err}
			}()
			close(start)
			loggedIn := <-loginResult
			unlinked := <-unlinkResult
			if loggedIn.err != nil {
				t.Fatalf("concurrent login: %v", loggedIn.err)
			}
			if unlinked.err != nil || unlinked.session.UserID != userID {
				t.Fatalf("concurrent unlink = %#v, %v", unlinked.session, unlinked.err)
			}
			var activeOldUserSessions, remainingMethod int
			if err := pool.QueryRow(ctx, `
				select
					(select count(*) from browser_sessions where user_id = $1 and revoked_at is null and expires_at > transaction_timestamp()),
					case $2::text
						when 'email' then (select count(*) from email_identities where user_id = $1)
						when 'google' then (select count(*) from google_identities where user_id = $1)
						when 'github' then (select count(*) from github_identities where user_id = $1)
						else -1
					end
			`, userID, string(method)).Scan(&activeOldUserSessions, &remainingMethod); err != nil {
				t.Fatalf("inspect login/unlink race: %v", err)
			}
			if activeOldUserSessions != 1 || remainingMethod != 0 {
				t.Fatalf("active old-User Sessions = %d, remaining method = %d", activeOldUserSessions, remainingMethod)
			}
			if loggedIn.session.UserID == userID {
				if _, err := store.AuthenticateBrowserSession(ctx, loggedIn.session.SessionID); !errors.Is(err, identity.ErrUnauthenticated) {
					t.Fatalf("pre-unlink login Session remained active: %v", err)
				}
			}
			if current, err := store.AuthenticateBrowserSession(ctx, unlinked.session.SessionID); err != nil || current.UserID != userID {
				t.Fatalf("unlink replacement = %#v, %v", current, err)
			}
		})
	}
}

func seedIdentityUser(
	t *testing.T,
	ctx context.Context,
	store *Store,
	email string,
	sessionCount int,
) (string, []string) {
	t.Helper()
	userID := uuid.NewString()
	if _, err := store.pool.Exec(ctx, `insert into carry_users (user_id) values ($1)`, userID); err != nil {
		t.Fatalf("seed Identity User: %v", err)
	}
	if email != "" {
		if _, err := store.pool.Exec(ctx, `insert into email_identities (canonical_email, user_id) values ($1, $2)`, email, userID); err != nil {
			t.Fatalf("seed email method: %v", err)
		}
	}
	sessions := make([]string, sessionCount)
	for index := range sessions {
		sessions[index] = createIdentityTestSession(t, ctx, store, userID, identity.EmailMethod)
	}
	return userID, sessions
}

func createIdentityTestSession(
	t *testing.T,
	ctx context.Context,
	store *Store,
	userID string,
	proofMethod identity.Method,
) string {
	t.Helper()
	sessionID := uuid.NewString()
	if _, err := store.queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: sessionID, UserID: userID, IdentityProofMethod: string(proofMethod),
	}); err != nil {
		t.Fatalf("create Identity test Session: %v", err)
	}
	return sessionID
}

func newClaimedExternalProof(
	t *testing.T,
	ctx context.Context,
	store *Store,
	credentials identity.Credentials,
	userID string,
	sessionID string,
	provider identity.ExternalLoginProvider,
	purpose identity.ProofPurpose,
	name string,
) struct {
	userID        string
	sessionID     string
	transactionID string
	digest        [32]byte
} {
	t.Helper()
	transactionID := uuid.NewString()
	if _, err := store.CreateExternalLogin(ctx, identity.CreateExternalLoginCommand{
		TransactionID: transactionID, Provider: provider, Purpose: purpose,
		TargetUserID: userID, InitiatingSessionID: sessionID,
	}); err != nil {
		t.Fatalf("create %s external proof: %v", name, err)
	}
	digest := credentials.RequestDigest("external-proof", name)
	if _, err := store.ClaimExternalLogin(ctx, identity.ClaimExternalLoginCommand{
		TransactionID: transactionID, Provider: provider, CallbackDigest: digest,
		Outcome: identity.ExternalCallbackCode,
	}); err != nil {
		t.Fatalf("claim %s external proof: %v", name, err)
	}
	return struct {
		userID        string
		sessionID     string
		transactionID string
		digest        [32]byte
	}{userID: userID, sessionID: sessionID, transactionID: transactionID, digest: digest}
}

func prepareIdentityEmailProof(
	t *testing.T,
	ctx context.Context,
	store *Store,
	credentials identity.Credentials,
	email string,
	purpose identity.ProofPurpose,
	userID string,
	sessionID string,
	name string,
) (string, string) {
	t.Helper()
	challengeID := uuid.NewString()
	code, err := credentials.EmailCode(challengeID, email)
	if err != nil {
		t.Fatalf("derive %s email code: %v", name, err)
	}
	challenge, err := store.PrepareEmailChallenge(ctx, identity.PrepareEmailChallengeCommand{
		ChallengeID: challengeID, CanonicalEmail: email,
		CodeDigest: credentials.CodeDigest(challengeID, code), SourceDigest: credentials.SourceDigest(name),
		IdempotencyKey: "request-" + name, RequestDigest: credentials.RequestDigest("request", name),
		Purpose: purpose, TargetUserID: userID, InitiatingSessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("prepare %s email proof: %v", name, err)
	}
	if _, err := store.RecordEmailSubmission(ctx, challengeID, challenge.PayloadDigest, identity.EmailSubmission{
		State: identity.EmailSubmissionAccepted, ProviderMessageID: "accepted-" + name,
	}); err != nil {
		t.Fatalf("accept %s email submission: %v", name, err)
	}
	return challengeID, code
}

func verifyIdentityEmailProof(
	ctx context.Context,
	store *Store,
	credentials identity.Credentials,
	challengeID string,
	code string,
	initiatingSessionID string,
	purpose identity.ProofPurpose,
	idempotencyKey string,
) (identity.BrowserSession, error) {
	return store.VerifyEmailChallenge(ctx, identity.VerifyEmailChallengeCommand{
		ChallengeID: challengeID, CodeDigest: credentials.CodeDigest(challengeID, code),
		IdempotencyKey: idempotencyKey,
		RequestDigest:  credentials.RequestDigest("verify", string(purpose), challengeID, code, initiatingSessionID),
		SessionID:      uuid.NewString(), Purpose: purpose, InitiatingSessionID: initiatingSessionID,
	})
}
