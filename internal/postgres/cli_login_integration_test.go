//go:build integration

package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCLILoginSingleRedeemReplayRevokeAndCurrentMembership(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{6}, identity.IdentityRootBytes))
	login, err := identity.NewCLILogin(store, credentials, "https://carry.example")
	if err != nil {
		t.Fatalf("create CLI login: %v", err)
	}
	userID, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)
	begun := beginCLILogin(t, ctx, login, "Desk CLI")
	preview, err := login.Lookup(ctx, identity.LookupCLILoginRequest{BrowserSessionID: sessionID, UserCode: begun.UserCode, Source: "127.0.0.1"})
	if err != nil || preview.RequestID != begun.RequestID {
		t.Fatalf("lookup = %#v, %v", preview, err)
	}
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: begun.RequestID, UserCode: begun.UserCode,
		SpaceID: spaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatalf("approve CLI login: %v", err)
	}
	otherUserID, otherSessionID := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, 'Other User')`, otherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1, $2, 'email', transaction_timestamp() + interval '1 hour')`, otherSessionID, otherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := login.Lookup(ctx, identity.LookupCLILoginRequest{BrowserSessionID: otherSessionID, UserCode: begun.UserCode, Source: "127.0.0.2"}); !errors.Is(err, identity.ErrCLILoginUnavailable) {
		t.Fatalf("approved CLI login leaked across Browser Users: %v", err)
	}

	allowCLIPoll(t, ctx, pool, begun.RequestID)
	results := make(chan identity.CLICredentialResult, 2)
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, pollErr := login.Poll(ctx, begun.PollSecret)
			results <- result
			errorsFound <- pollErr
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	var first identity.CLICredentialResult
	for pollErr := range errorsFound {
		if pollErr != nil {
			t.Fatalf("concurrent redeem: %v", pollErr)
		}
	}
	for result := range results {
		if first.Credential == "" {
			first = result
		}
		if result.Credential != first.Credential || result.CredentialID != first.CredentialID {
			t.Fatal("concurrent redeem created different credentials")
		}
	}
	if first.UserID != userID || first.SpaceID != spaceID {
		t.Fatalf("redeemed = %#v", first)
	}
	var machineCount int
	if err := pool.QueryRow(ctx, `select count(*) from machines where space_id = $1`, spaceID).Scan(&machineCount); err != nil {
		t.Fatal(err)
	}
	if machineCount != 0 {
		t.Fatalf("CLI approval enrolled %d Machine(s)", machineCount)
	}
	credentialID, ok := credentials.ParseCLICredential(first.Credential, "https://carry.example")
	if !ok || credentialID != first.CredentialID {
		t.Fatal("final credential did not bind configured origin")
	}
	authenticated, err := store.AuthenticateCLICredential(ctx, credentialID)
	if err != nil || authenticated.UserID != userID {
		t.Fatalf("authenticate = %#v, %v", authenticated, err)
	}
	listed, err := login.ListCredentials(ctx, sessionID)
	if err != nil || len(listed) != 1 || listed[0].ApprovedSpaceID != spaceID || listed[0].ApprovedSpaceName == "" {
		t.Fatalf("listed credentials = %#v, %v", listed, err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = transaction_timestamp() where space_id = $1 and user_id = $2`, spaceID, userID); err != nil {
		t.Fatal(err)
	}
	listed, err = login.ListCredentials(ctx, sessionID)
	if err != nil || len(listed) != 1 || listed[0].ApprovedSpaceID != spaceID || listed[0].ApprovedSpaceName != "" {
		t.Fatalf("removed Membership leaked current Space name: %#v, %v", listed, err)
	}
	if _, err := pool.Exec(ctx, `update cli_login_requests set redeemed_at = transaction_timestamp() - interval '6 minutes', replay_until = transaction_timestamp() - interval '1 minute' where request_id = $1`, begun.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := login.Poll(ctx, begun.PollSecret); !errors.Is(err, identity.ErrCLICredentialUnavailable) {
		t.Fatalf("expired redeem replay window = %v", err)
	}

	revokeKey := uuid.NewString()
	if err := login.RevokeFromBrowser(ctx, sessionID, credentialID, revokeKey); err != nil {
		t.Fatalf("revoke CLI credential: %v", err)
	}
	if err := login.RevokeFromBrowser(ctx, sessionID, credentialID, revokeKey); err != nil {
		t.Fatalf("replay revoke: %v", err)
	}
	if _, err := store.AuthenticateCLICredential(ctx, credentialID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("revoked auth error = %v", err)
	}
	if _, err := login.Poll(ctx, begun.PollSecret); !errors.Is(err, identity.ErrCLICredentialUnavailable) {
		t.Fatalf("redeem replay resurrected revoked credential: %v", err)
	}
	if err := login.RevokeCurrent(ctx, first.Credential, uuid.NewString()); err != nil {
		t.Fatalf("self logout did not reconcile prior Browser revocation: %v", err)
	}
}

func TestCLILoginExactReplacementAndSelfRevokeHaveOneCredentialWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{7}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	userID, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)

	firstLogin := beginCLILogin(t, ctx, login, "First CLI")
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: firstLogin.RequestID, UserCode: firstLogin.UserCode,
		SpaceID: spaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	allowCLIPoll(t, ctx, pool, firstLogin.RequestID)
	first, err := login.Poll(ctx, firstLogin.PollSecret)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExpiresAt.Sub(time.Now()) < 89*24*time.Hour || first.ExpiresAt.Sub(time.Now()) > 91*24*time.Hour {
		t.Fatalf("CLI credential expiry = %s, want about 90 days", first.ExpiresAt)
	}

	replacements := make([]identity.BegunCLILogin, 2)
	for index := range replacements {
		replacements[index] = beginCLILogin(t, ctx, login, fmt.Sprintf("Replacement %d", index+1))
		if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
			BrowserSessionID: sessionID, RequestID: replacements[index].RequestID, UserCode: replacements[index].UserCode,
			SpaceID: spaceID, ReplacementCredentialID: first.CredentialID, IdempotencyKey: uuid.NewString(),
		}); err != nil {
			t.Fatalf("approve replacement %d: %v", index, err)
		}
		allowCLIPoll(t, ctx, pool, replacements[index].RequestID)
	}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	results := make(chan identity.CLICredentialResult, 2)
	for _, replacement := range replacements {
		go func(loginRequest identity.BegunCLILogin) {
			<-start
			result, pollErr := login.Poll(ctx, loginRequest.PollSecret)
			results <- result
			outcomes <- pollErr
		}(replacement)
	}
	close(start)
	firstErr, secondErr := <-outcomes, <-outcomes
	if (firstErr == nil) == (secondErr == nil) || (firstErr != nil && !errors.Is(firstErr, identity.ErrCLIReplacementInvalid)) ||
		(secondErr != nil && !errors.Is(secondErr, identity.ErrCLIReplacementInvalid)) {
		t.Fatalf("replacement outcomes = %v, %v", firstErr, secondErr)
	}
	var winner identity.CLICredentialResult
	for range 2 {
		if result := <-results; result.Credential != "" {
			winner = result
		}
	}
	if _, err := store.AuthenticateCLICredential(ctx, first.CredentialID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("replaced credential authenticated: %v", err)
	}
	key := uuid.NewString()
	if err := login.RevokeCurrent(ctx, winner.Credential, key); err != nil {
		t.Fatalf("self revoke: %v", err)
	}
	if err := login.RevokeCurrent(ctx, winner.Credential, key); err != nil {
		t.Fatalf("self revoke replay: %v", err)
	}
	if _, err := store.AuthenticateCLICredential(ctx, winner.CredentialID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("self-revoked credential authenticated: %v (user %s)", err, userID)
	}
}

func TestCLILoginCredentialExpiryRejectsAuthenticationAndReplay(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{11}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	_, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)
	begun := beginCLILogin(t, ctx, login, "Expiring CLI")
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: begun.RequestID, UserCode: begun.UserCode,
		SpaceID: spaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	allowCLIPoll(t, ctx, pool, begun.RequestID)
	redeemed, err := login.Poll(ctx, begun.PollSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update cli_credentials set created_at = transaction_timestamp() - interval '100 days', expires_at = transaction_timestamp() - interval '1 second' where credential_id = $1`, redeemed.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateCLICredential(ctx, redeemed.CredentialID); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("expired credential authentication = %v", err)
	}
	if _, err := login.Poll(ctx, begun.PollSecret); !errors.Is(err, identity.ErrCLICredentialUnavailable) {
		t.Fatalf("expired credential replay = %v", err)
	}
}

func TestCLILoginBeginRejectsChangedIdempotencyIdentity(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{9}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(NewStore(pool), credentials, "https://carry.example")
	key := uuid.NewString()
	first := identity.BeginCLILoginRequest{RequestID: uuid.NewString(), IdempotencyKey: key, Label: "First", Source: "127.0.0.1"}
	if _, err := login.Begin(ctx, first); err != nil {
		t.Fatal(err)
	}
	first.RequestID = uuid.NewString()
	first.Label = "Changed"
	if _, err := login.Begin(ctx, first); !errors.Is(err, identity.ErrCLILoginConflict) {
		t.Fatalf("changed begin replay = %v", err)
	}
}

func TestCLILoginPacingDenyCancelExpiryAndLookupBudget(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{8}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	_, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)

	pending := beginCLILogin(t, ctx, login, "Pending CLI")
	if _, err := login.Poll(ctx, pending.PollSecret); !errors.Is(err, identity.ErrCLILoginSlowDown) {
		t.Fatalf("early first poll = %v", err)
	}
	if _, err := pool.Exec(ctx, `update cli_login_requests set created_at = transaction_timestamp() - interval '20 seconds', last_polled_at = transaction_timestamp() - interval '11 seconds' where request_id = $1`, pending.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := login.Poll(ctx, pending.PollSecret); !errors.Is(err, identity.ErrCLILoginPending) {
		t.Fatalf("paced pending poll = %v", err)
	}
	if _, err := login.Poll(ctx, pending.PollSecret); !errors.Is(err, identity.ErrCLILoginSlowDown) {
		t.Fatalf("repeated early poll = %v", err)
	}
	if err := login.Cancel(ctx, pending.PollSecret); err != nil {
		t.Fatalf("cancel pending: %v", err)
	}
	if _, err := login.Poll(ctx, pending.PollSecret); !errors.Is(err, identity.ErrCLILoginCancelled) {
		t.Fatalf("cancelled poll = %v", err)
	}

	denied := beginCLILogin(t, ctx, login, "Denied CLI")
	if err := login.Deny(ctx, identity.DenyCLILoginRequest{BrowserSessionID: sessionID, RequestID: denied.RequestID, UserCode: denied.UserCode, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if _, err := login.Poll(ctx, denied.PollSecret); !errors.Is(err, identity.ErrCLILoginDenied) {
		t.Fatalf("denied poll = %v", err)
	}

	approvedThenCancelled := beginCLILogin(t, ctx, login, "Abandoned CLI")
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{BrowserSessionID: sessionID, RequestID: approvedThenCancelled.RequestID, UserCode: approvedThenCancelled.UserCode, SpaceID: spaceID, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := login.Cancel(ctx, approvedThenCancelled.PollSecret); err != nil {
		t.Fatalf("cancel approved before redeem: %v", err)
	}
	if _, err := login.Poll(ctx, approvedThenCancelled.PollSecret); !errors.Is(err, identity.ErrCLILoginCancelled) {
		t.Fatalf("approved cancellation poll = %v", err)
	}

	expired := beginCLILogin(t, ctx, login, "Expired CLI")
	if _, err := pool.Exec(ctx, `update cli_login_requests set created_at = transaction_timestamp() - interval '20 minutes', expires_at = transaction_timestamp() - interval '5 minutes' where request_id = $1`, expired.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := login.Poll(ctx, expired.PollSecret); !errors.Is(err, identity.ErrCLILoginExpired) {
		t.Fatalf("expired poll = %v", err)
	}

	for attempt := range 5 {
		_, err := login.Lookup(ctx, identity.LookupCLILoginRequest{BrowserSessionID: sessionID, UserCode: "BCDF-GH-JKLM", Source: "10.0.0.1"})
		if !errors.Is(err, identity.ErrCLILoginUnavailable) {
			t.Fatalf("wrong lookup %d = %v", attempt, err)
		}
	}
	if _, err := login.Lookup(ctx, identity.LookupCLILoginRequest{BrowserSessionID: sessionID, UserCode: expired.UserCode, Source: "10.0.0.1"}); !errors.Is(err, identity.ErrCLILoginRateLimited) {
		t.Fatalf("lookup budget = %v", err)
	}
}

func TestCLILoginApprovalRereadsMembershipAndApproveDenyRaceHasOneWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{10}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	userID, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)

	removed := beginCLILogin(t, ctx, login, "Removed member CLI")
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = transaction_timestamp() where space_id = $1 and user_id = $2`, spaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{BrowserSessionID: sessionID, RequestID: removed.RequestID, UserCode: removed.UserCode, SpaceID: spaceID, IdempotencyKey: uuid.NewString()}); !errors.Is(err, identity.ErrCLILoginUnavailable) {
		t.Fatalf("removed Membership approval = %v", err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at = null where space_id = $1 and user_id = $2`, spaceID, userID); err != nil {
		t.Fatal(err)
	}

	tracing := beginCLILogin(t, ctx, login, "Racing CLI")
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	go func() {
		<-start
		outcomes <- login.Approve(ctx, identity.ApproveCLILoginRequest{BrowserSessionID: sessionID, RequestID: tracing.RequestID, UserCode: tracing.UserCode, SpaceID: spaceID, IdempotencyKey: uuid.NewString()})
	}()
	go func() {
		<-start
		outcomes <- login.Deny(ctx, identity.DenyCLILoginRequest{BrowserSessionID: sessionID, RequestID: tracing.RequestID, UserCode: tracing.UserCode, IdempotencyKey: uuid.NewString()})
	}()
	close(start)
	first, second := <-outcomes, <-outcomes
	if (first == nil) == (second == nil) {
		t.Fatalf("approve/deny outcomes = %v, %v", first, second)
	}
}

func TestCLILoginCancelAndRedeemRaceHasOneCredentialConsequence(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{12}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	_, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)
	begun := beginCLILogin(t, ctx, login, "Cancel redeem race")
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: begun.RequestID, UserCode: begun.UserCode,
		SpaceID: spaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	allowCLIPoll(t, ctx, pool, begun.RequestID)

	start := make(chan struct{})
	pollResults := make(chan identity.CLICredentialResult, 1)
	pollErrors := make(chan error, 1)
	cancelErrors := make(chan error, 1)
	go func() {
		<-start
		result, err := login.Poll(ctx, begun.PollSecret)
		pollResults <- result
		pollErrors <- err
	}()
	go func() {
		<-start
		cancelErrors <- login.Cancel(ctx, begun.PollSecret)
	}()
	close(start)
	result, pollErr, cancelErr := <-pollResults, <-pollErrors, <-cancelErrors
	var credentialCount int
	if err := pool.QueryRow(ctx, `select count(*) from cli_credentials where login_request_id = $1`, begun.RequestID).Scan(&credentialCount); err != nil {
		t.Fatal(err)
	}
	switch {
	case pollErr == nil:
		if !errors.Is(cancelErr, identity.ErrCLILoginRedeemed) || credentialCount != 1 || result.Credential == "" {
			t.Fatalf("redeem-first outcomes = result %#v, poll %v, cancel %v, credentials %d", result, pollErr, cancelErr, credentialCount)
		}
	case cancelErr == nil:
		if !errors.Is(pollErr, identity.ErrCLILoginCancelled) || credentialCount != 0 || result.Credential != "" {
			t.Fatalf("cancel-first outcomes = result %#v, poll %v, cancel %v, credentials %d", result, pollErr, cancelErr, credentialCount)
		}
	default:
		t.Fatalf("cancel/redeem produced no winner: poll %v, cancel %v", pollErr, cancelErr)
	}
}

func TestCLILoginReplacementAndBrowserRevokeRaceCannotLeaveTwoCredentials(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{13}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	userID, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)
	firstRequest := beginCLILogin(t, ctx, login, "Original CLI")
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: firstRequest.RequestID, UserCode: firstRequest.UserCode,
		SpaceID: spaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	allowCLIPoll(t, ctx, pool, firstRequest.RequestID)
	first, err := login.Poll(ctx, firstRequest.PollSecret)
	if err != nil {
		t.Fatal(err)
	}

	replacementRequest := beginCLILogin(t, ctx, login, "Replacement CLI")
	if err := login.Approve(ctx, identity.ApproveCLILoginRequest{
		BrowserSessionID: sessionID, RequestID: replacementRequest.RequestID, UserCode: replacementRequest.UserCode,
		SpaceID: spaceID, ReplacementCredentialID: first.CredentialID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	allowCLIPoll(t, ctx, pool, replacementRequest.RequestID)

	start := make(chan struct{})
	pollResults := make(chan identity.CLICredentialResult, 1)
	pollErrors := make(chan error, 1)
	revokeErrors := make(chan error, 1)
	go func() {
		<-start
		result, err := login.Poll(ctx, replacementRequest.PollSecret)
		pollResults <- result
		pollErrors <- err
	}()
	go func() {
		<-start
		revokeErrors <- login.RevokeFromBrowser(ctx, sessionID, first.CredentialID, uuid.NewString())
	}()
	close(start)
	result, pollErr, revokeErr := <-pollResults, <-pollErrors, <-revokeErrors
	var activeCount int
	if err := pool.QueryRow(ctx, `select count(*) from cli_credentials where user_id = $1 and revoked_at is null and expires_at > transaction_timestamp()`, userID).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	switch {
	case pollErr == nil:
		if !errors.Is(revokeErr, identity.ErrCLILoginConflict) || activeCount != 1 || result.CredentialID == first.CredentialID {
			t.Fatalf("replacement-first outcomes = result %#v, poll %v, revoke %v, active %d", result, pollErr, revokeErr, activeCount)
		}
	case revokeErr == nil:
		if !errors.Is(pollErr, identity.ErrCLIReplacementInvalid) || activeCount != 0 || result.Credential != "" {
			t.Fatalf("revoke-first outcomes = result %#v, poll %v, revoke %v, active %d", result, pollErr, revokeErr, activeCount)
		}
	default:
		t.Fatalf("replacement/revoke produced no winner: poll %v, revoke %v", pollErr, revokeErr)
	}
}

func TestCLILoginApprovalAndMembershipRemovalHaveOneValidOrder(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{11}, identity.IdentityRootBytes))
	login, _ := identity.NewCLILogin(store, credentials, "https://carry.example")
	userID, spaceID, sessionID := seedCLILoginMember(t, ctx, pool)
	managerID := seedRemovalMember(t, ctx, pool, spaceID, true, true)
	begun := beginCLILogin(t, ctx, login, "Removal race CLI")
	if _, err := pool.Exec(ctx, `update cli_login_requests set created_at = transaction_timestamp() - interval '6 seconds' where request_id = $1`, begun.RequestID); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	approveResult := make(chan error, 1)
	removeResult := make(chan error, 1)
	go func() {
		<-start
		approveResult <- login.Approve(ctx, identity.ApproveCLILoginRequest{
			BrowserSessionID: sessionID, RequestID: begun.RequestID, UserCode: begun.UserCode,
			SpaceID: spaceID, IdempotencyKey: uuid.NewString(),
		})
	}()
	go func() {
		<-start
		removeResult <- store.RemoveSpaceMember(ctx, removalCommand(t, space.RemoveMemberRequest{
			SpaceID: spaceID, ActorUserID: managerID, TargetUserID: userID,
			IdempotencyKey: "cli-approval-removal-race",
		}))
	}()
	close(start)
	approveErr, removeErr := <-approveResult, <-removeResult
	if removeErr != nil {
		t.Fatalf("concurrent removal = %v", removeErr)
	}
	if approveErr != nil && !errors.Is(approveErr, identity.ErrCLILoginUnavailable) {
		t.Fatalf("concurrent approval = %v", approveErr)
	}
	if _, err := store.ListWorks(ctx, work.ListCommand{UserID: userID, SpaceID: spaceID}); !errors.Is(err, space.ErrForbidden) {
		t.Fatalf("removed member Work access = %v", err)
	}
	if approveErr == nil {
		result, err := login.Poll(ctx, begun.PollSecret)
		if err != nil || result.UserID != userID {
			t.Fatalf("approval-first credential result = %#v, %v", result, err)
		}
	} else if err := login.Cancel(ctx, begun.PollSecret); err != nil {
		t.Fatalf("cancel removal-first request: %v", err)
	}
}

func allowCLIPoll(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `update cli_login_requests set created_at = transaction_timestamp() - interval '31 seconds', last_polled_at = null where request_id = $1`, requestID); err != nil {
		t.Fatalf("make CLI poll eligible: %v", err)
	}
}

func beginCLILogin(t *testing.T, ctx context.Context, login *identity.CLILogin, label string) identity.BegunCLILogin {
	t.Helper()
	request := identity.BeginCLILoginRequest{RequestID: uuid.NewString(), IdempotencyKey: uuid.NewString(), Label: label, Source: "127.0.0.1"}
	begun, err := login.Begin(ctx, request)
	if err != nil {
		t.Fatalf("begin CLI login: %v", err)
	}
	replayed, err := login.Begin(ctx, request)
	if err != nil {
		t.Fatalf("replay begin: %v", err)
	}
	if replayed.UserCode != begun.UserCode || replayed.PollSecret != begun.PollSecret {
		t.Fatal("begin response-loss replay changed secrets")
	}
	return begun
}

func seedCLILoginMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	userID, spaceID, sessionID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	statements := []struct {
		sql  string
		args []any
	}{
		{`insert into carry_users (user_id, display_name) values ($1, 'CLI Member')`, []any{userID}},
		{
			sql: `insert into spaces (space_id, name, slug) values ($1::uuid, 'CLI Space', replace(($1::uuid)::text, '-', ''))`,
			args: []any{
				spaceID,
			},
		},
		{`insert into space_memberships (space_id, user_id, can_enroll_machines, can_manage_members) values ($1, $2, true, true)`, []any{spaceID, userID}},
		{`insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1, $2, 'email', transaction_timestamp() + interval '30 days')`, []any{sessionID, userID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed CLI login member: %v", err)
		}
	}
	return userID, spaceID, sessionID
}
