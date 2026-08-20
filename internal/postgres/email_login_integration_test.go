//go:build integration

package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
)

func TestEmailChallengeRequestReplayRequiresExactChallengeIdentity(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	canonical := "request-replay@example.com"
	challengeID := uuid.NewString()
	code, err := credentials.EmailCode(challengeID, canonical)
	if err != nil {
		t.Fatalf("derive email code: %v", err)
	}
	command := identity.PrepareEmailChallengeCommand{
		ChallengeID: challengeID, CanonicalEmail: canonical,
		CodeDigest: credentials.CodeDigest(challengeID, code), SourceDigest: credentials.SourceDigest("127.0.0.1"),
		IdempotencyKey: "request-replay", RequestDigest: credentials.RequestDigest("request-email-code", challengeID, canonical),
	}
	first, err := store.PrepareEmailChallenge(ctx, command)
	if err != nil {
		t.Fatalf("prepare challenge: %v", err)
	}
	replayed, err := store.PrepareEmailChallenge(ctx, command)
	if err != nil || replayed.ChallengeID != first.ChallengeID {
		t.Fatalf("exact request replay = %#v, %v", replayed, err)
	}
	changedID := uuid.NewString()
	changedCode, _ := credentials.EmailCode(changedID, canonical)
	command.ChallengeID = changedID
	command.CodeDigest = credentials.CodeDigest(changedID, changedCode)
	command.RequestDigest = credentials.RequestDigest("request-email-code", changedID, canonical)
	if _, err := store.PrepareEmailChallenge(ctx, command); !errors.Is(err, identity.ErrIdempotencyConflict) {
		t.Fatalf("changed challenge replay error = %v", err)
	}
}

func TestEmailChallengeRequestKeyConflictPrecedesPayloadDriftAcrossEmailSources(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	idempotencyKey := "request-key-cross-pair-sequential"

	firstID := uuid.NewString()
	firstEmail := "first-key-owner@example.com"
	firstCode, err := credentials.EmailCode(firstID, firstEmail)
	if err != nil {
		t.Fatalf("derive first email code: %v", err)
	}
	firstPayload := sha256.Sum256([]byte("first concrete payload"))
	_, err = store.PrepareEmailChallenge(ctx, identity.PrepareEmailChallengeCommand{
		ChallengeID: firstID, CanonicalEmail: firstEmail,
		CodeDigest: credentials.CodeDigest(firstID, firstCode), SourceDigest: credentials.SourceDigest("198.51.100.10"),
		PayloadDigest: firstPayload, IdempotencyKey: idempotencyKey,
		RequestDigest: credentials.RequestDigest("request-email-code", firstID, firstEmail),
	})
	if err != nil {
		t.Fatalf("prepare first challenge: %v", err)
	}

	secondID := uuid.NewString()
	secondEmail := "second-key-claimant@example.com"
	secondCode, err := credentials.EmailCode(secondID, secondEmail)
	if err != nil {
		t.Fatalf("derive second email code: %v", err)
	}
	secondPayload := sha256.Sum256([]byte("changed concrete payload"))
	_, err = store.PrepareEmailChallenge(ctx, identity.PrepareEmailChallengeCommand{
		ChallengeID: secondID, CanonicalEmail: secondEmail,
		CodeDigest: credentials.CodeDigest(secondID, secondCode), SourceDigest: credentials.SourceDigest("203.0.113.20"),
		PayloadDigest: secondPayload, IdempotencyKey: idempotencyKey,
		RequestDigest: credentials.RequestDigest("request-email-code", secondID, secondEmail),
	})
	if !errors.Is(err, identity.ErrIdempotencyConflict) {
		t.Fatalf("different semantic request with payload drift error = %v", err)
	}
}

func TestConcurrentEmailChallengeRequestKeyConflictAcrossEmailSources(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	idempotencyKey := "request-key-cross-pair-concurrent"

	commands := make([]identity.PrepareEmailChallengeCommand, 2)
	for index := range commands {
		challengeID := uuid.NewString()
		canonicalEmail := fmt.Sprintf("request-key-race-%d@example.com", index)
		code, err := credentials.EmailCode(challengeID, canonicalEmail)
		if err != nil {
			t.Fatalf("derive racing email code: %v", err)
		}
		commands[index] = identity.PrepareEmailChallengeCommand{
			ChallengeID: challengeID, CanonicalEmail: canonicalEmail,
			CodeDigest:     credentials.CodeDigest(challengeID, code),
			SourceDigest:   credentials.SourceDigest(fmt.Sprintf("192.0.2.%d", index+1)),
			PayloadDigest:  sha256.Sum256([]byte(fmt.Sprintf("payload-%d", index))),
			IdempotencyKey: idempotencyKey,
			RequestDigest:  credentials.RequestDigest("request-email-code", challengeID, canonicalEmail),
		}
	}

	start := make(chan struct{})
	results := make(chan error, len(commands))
	var wait sync.WaitGroup
	for _, command := range commands {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.PrepareEmailChallenge(ctx, command)
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var succeeded, conflicted int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, identity.ErrIdempotencyConflict):
			conflicted++
		default:
			t.Fatalf("concurrent request-key outcome = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("request-key outcomes = %d succeeded, %d conflicted", succeeded, conflicted)
	}
	var persisted int
	if err := pool.QueryRow(ctx, `select count(*) from email_login_challenges where request_idempotency_key = $1`, idempotencyKey).Scan(&persisted); err != nil {
		t.Fatalf("count request-key challenges: %v", err)
	}
	if persisted != 1 {
		t.Fatalf("persisted request-key challenges = %d, want 1", persisted)
	}
}

func TestExpiredPreparedReplayCannotRequestSubmission(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	canonical := "expired-prepared@example.com"
	challengeID := uuid.NewString()
	code, err := credentials.EmailCode(challengeID, canonical)
	if err != nil {
		t.Fatalf("derive email code: %v", err)
	}
	command := identity.PrepareEmailChallengeCommand{
		ChallengeID: challengeID, CanonicalEmail: canonical,
		CodeDigest: credentials.CodeDigest(challengeID, code), SourceDigest: credentials.SourceDigest("127.0.0.1"),
		IdempotencyKey: "expired-prepared", RequestDigest: credentials.RequestDigest("request-email-code", challengeID, canonical),
	}
	if _, err := store.PrepareEmailChallenge(ctx, command); err != nil {
		t.Fatalf("prepare challenge: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update email_login_challenges
		set created_at = transaction_timestamp() - interval '10 minutes',
		    expires_at = transaction_timestamp() - interval '5 minutes'
		where challenge_id = $1
	`, challengeID); err != nil {
		t.Fatalf("expire challenge: %v", err)
	}
	replayed, err := store.PrepareEmailChallenge(ctx, command)
	if err != nil {
		t.Fatalf("replay expired challenge: %v", err)
	}
	if replayed.CanSubmit {
		t.Fatal("expired prepared replay retained submission authority")
	}
}

func TestConcurrentEmailChallengeRequestsEnforceSharedSourceLimitAcrossEmails(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	sourceDigest := credentials.SourceDigest("198.51.100.27")

	start := make(chan struct{})
	results := make(chan error, maxSourceChallengesPerHour+12)
	var wait sync.WaitGroup
	for index := range maxSourceChallengesPerHour + 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			email := fmt.Sprintf("source-race-%02d@example.com", index)
			challengeID := uuid.NewString()
			code, err := credentials.EmailCode(challengeID, email)
			if err != nil {
				results <- err
				return
			}
			payloadDigest := sha256.Sum256([]byte(email))
			_, err = store.PrepareEmailChallenge(ctx, identity.PrepareEmailChallengeCommand{
				ChallengeID: challengeID, CanonicalEmail: email,
				CodeDigest: credentials.CodeDigest(challengeID, code), SourceDigest: sourceDigest,
				PayloadDigest: payloadDigest, IdempotencyKey: fmt.Sprintf("source-race-%02d", index),
				RequestDigest: credentials.RequestDigest("source-race", challengeID, email),
			})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var succeeded, limited int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, identity.ErrEmailRateLimited):
			limited++
		default:
			t.Fatalf("parallel source-limited request: %v", err)
		}
	}
	if succeeded != maxSourceChallengesPerHour || limited != 12 {
		t.Fatalf("source outcomes = %d succeeded, %d limited", succeeded, limited)
	}
	var persisted int
	if err := pool.QueryRow(ctx, `select count(*) from email_login_challenges where source_digest = $1`, sourceDigest[:]).Scan(&persisted); err != nil {
		t.Fatalf("count source challenges: %v", err)
	}
	if persisted != maxSourceChallengesPerHour {
		t.Fatalf("persisted source challenges = %d, want %d", persisted, maxSourceChallengesPerHour)
	}
}

func TestConcurrentEmailVerificationConsumesChallengeOnce(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	challengeID, code := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "concurrent@example.com", "request-concurrent")

	start := make(chan struct{})
	results := make(chan error, 16)
	var wait sync.WaitGroup
	for index := range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := verifyEmail(t, ctx, store, credentials, challengeID, code, fmt.Sprintf("verify-%d", index), uuid.NewString())
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var succeeded int
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, identity.ErrInvalidCode) {
			t.Fatalf("concurrent verification error = %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful verifications = %d, want 1", succeeded)
	}
	var sessions, users int
	if err := pool.QueryRow(ctx, `select count(*) from browser_sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count Browser Sessions: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from email_identities`).Scan(&users); err != nil {
		t.Fatalf("count email identities: %v", err)
	}
	if sessions != 1 || users != 1 {
		t.Fatalf("sessions = %d, email identities = %d", sessions, users)
	}
}

func TestEmailVerificationReplayAndAttemptBudget(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	challengeID, code := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "replay@example.com", "request-replay")

	wrongDigest := credentials.CodeDigest(challengeID, "000000")
	wrongRequest := credentials.RequestDigest("verify-email-code", challengeID, "000000")
	wrong := identity.VerifyEmailChallengeCommand{
		ChallengeID: challengeID, CodeDigest: wrongDigest, IdempotencyKey: "wrong-once",
		RequestDigest: wrongRequest, SessionID: uuid.NewString(),
	}
	for range 2 {
		if _, err := store.VerifyEmailChallenge(ctx, wrong); !errors.Is(err, identity.ErrInvalidCode) {
			t.Fatalf("wrong replay error = %v", err)
		}
	}
	var attempts int
	if err := pool.QueryRow(ctx, `select attempts_used from email_login_challenges where challenge_id = $1`, challengeID).Scan(&attempts); err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts after exact replay = %d, want 1", attempts)
	}
	for index := 1; index < identity.EmailCodeAttemptLimit; index++ {
		wrongCode := fmt.Sprintf("%06d", index)
		digest := credentials.CodeDigest(challengeID, wrongCode)
		requestDigest := credentials.RequestDigest("verify-email-code", challengeID, wrongCode)
		_, err := store.VerifyEmailChallenge(ctx, identity.VerifyEmailChallengeCommand{
			ChallengeID: challengeID, CodeDigest: digest, IdempotencyKey: fmt.Sprintf("wrong-%d", index),
			RequestDigest: requestDigest, SessionID: uuid.NewString(),
		})
		if !errors.Is(err, identity.ErrInvalidCode) {
			t.Fatalf("wrong attempt %d error = %v", index, err)
		}
	}
	if _, err := verifyEmail(t, ctx, store, credentials, challengeID, code, "correct-too-late", uuid.NewString()); !errors.Is(err, identity.ErrInvalidCode) {
		t.Fatalf("correct code after attempt exhaustion error = %v", err)
	}
}

func TestEmailVerificationExactReplayReturnsSameSession(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	challengeID, code := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "session@example.com", "request-session")

	first, err := verifyEmail(t, ctx, store, credentials, challengeID, code, "verify-session", uuid.NewString())
	if err != nil {
		t.Fatalf("verify email: %v", err)
	}
	replayed, err := verifyEmail(t, ctx, store, credentials, challengeID, code, "verify-session", uuid.NewString())
	if err != nil {
		t.Fatalf("replay email verification: %v", err)
	}
	if first.SessionID != replayed.SessionID || first.UserID != replayed.UserID {
		t.Fatalf("replayed session = %#v, first = %#v", replayed, first)
	}
	firstCredential, _ := credentials.BrowserSessionCredential(first.SessionID)
	replayedCredential, _ := credentials.BrowserSessionCredential(replayed.SessionID)
	if firstCredential != replayedCredential {
		t.Fatal("exact verification replay changed the Browser Session credential")
	}
}

func TestExpiredAndResentEmailChallengesCannotVerify(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)

	expiredID, expiredCode := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "expired@example.com", "request-expired")
	if _, err := pool.Exec(ctx, `
		update email_login_challenges
		set created_at = transaction_timestamp() - interval '10 minutes',
		    expires_at = transaction_timestamp() - interval '5 minutes'
		where challenge_id = $1
	`, expiredID); err != nil {
		t.Fatalf("expire challenge: %v", err)
	}
	if _, err := verifyEmail(t, ctx, store, credentials, expiredID, expiredCode, "verify-expired", uuid.NewString()); !errors.Is(err, identity.ErrInvalidCode) {
		t.Fatalf("expired verification error = %v", err)
	}

	oldID, oldCode := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "resend@example.com", "request-old")
	if _, err := pool.Exec(ctx, `
		update email_login_challenges
		set created_at = transaction_timestamp() - interval '2 minutes'
		where challenge_id = $1
	`, oldID); err != nil {
		t.Fatalf("age old challenge: %v", err)
	}
	newID, newCode := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "resend@example.com", "request-new")
	if _, err := verifyEmail(t, ctx, store, credentials, oldID, oldCode, "verify-old", uuid.NewString()); !errors.Is(err, identity.ErrInvalidCode) {
		t.Fatalf("invalidated old code error = %v", err)
	}
	if _, err := verifyEmail(t, ctx, store, credentials, newID, newCode, "verify-new", uuid.NewString()); err != nil {
		t.Fatalf("verify newest code: %v", err)
	}
}

func TestUnknownEmailSubmissionReplaysSamePersistedPayloadAndRejectsDrift(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	command := identity.RequestEmailCodeCommand{
		ChallengeID: uuid.NewString(), Email: "payload-replay@example.com", Source: "198.51.100.41",
		IdempotencyKey: "payload-replay",
	}
	submitter := &persistentEmailSubmitter{configuration: "from=Carry <login@example.com>;template=v1"}
	login, err := identity.NewEmailLogin(store, submitter, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	for range 2 {
		challenge, err := login.RequestCode(ctx, command)
		if err != nil {
			t.Fatalf("request unknown email submission: %v", err)
		}
		if challenge.SubmissionState != identity.EmailSubmissionUnknown {
			t.Fatalf("submission state = %q", challenge.SubmissionState)
		}
	}
	if len(submitter.messages) != 2 || submitter.messages[0] != submitter.messages[1] ||
		submitter.digests[0] != submitter.digests[1] {
		t.Fatalf("messages = %#v, digests = %#v", submitter.messages, submitter.digests)
	}

	drifted := &persistentEmailSubmitter{configuration: "from=Carry <new@example.com>;template=v2"}
	driftedLogin, err := identity.NewEmailLogin(store, drifted, credentials)
	if err != nil {
		t.Fatalf("compose drifted email login: %v", err)
	}
	if _, err := driftedLogin.RequestCode(ctx, command); !errors.Is(err, identity.ErrEmailPayloadChanged) {
		t.Fatalf("drifted payload error = %v", err)
	}
	if len(drifted.messages) != 0 {
		t.Fatalf("drifted payload made %d external submissions", len(drifted.messages))
	}
}

func TestRepeatEmailLoginKeepsUserAndCreatesFreshBrowserSession(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	firstChallenge, firstCode := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "returning@example.com", "request-returning-first")
	first, err := verifyEmail(t, ctx, store, credentials, firstChallenge, firstCode, "verify-returning-first", uuid.NewString())
	if err != nil {
		t.Fatalf("verify first login: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update email_login_challenges
		set created_at = transaction_timestamp() - interval '2 minutes'
		where challenge_id = $1
	`, firstChallenge); err != nil {
		t.Fatalf("age first login challenge: %v", err)
	}
	secondChallenge, secondCode := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "returning@example.com", "request-returning-second")
	second, err := verifyEmail(t, ctx, store, credentials, secondChallenge, secondCode, "verify-returning-second", uuid.NewString())
	if err != nil {
		t.Fatalf("verify repeat login: %v", err)
	}
	if second.UserID != first.UserID || second.SessionID == first.SessionID {
		t.Fatalf("first session = %#v, repeat session = %#v", first, second)
	}
	var users, sessions int
	if err := pool.QueryRow(ctx, `select count(*) from carry_users`).Scan(&users); err != nil {
		t.Fatalf("count returning Users: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from browser_sessions where user_id = $1`, first.UserID).Scan(&sessions); err != nil {
		t.Fatalf("count returning Browser Sessions: %v", err)
	}
	if users != 1 || sessions != 2 {
		t.Fatalf("returning login users = %d, sessions = %d", users, sessions)
	}
}

func TestFirstSpaceCreationIsAtomicIdempotentAndSingleWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	credentials := testEmailCredentials(t)
	challengeID, code := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "creator@example.com", "request-creator")
	session, err := verifyEmail(t, ctx, store, credentials, challengeID, code, "verify-creator", uuid.NewString())
	if err != nil {
		t.Fatalf("verify creator: %v", err)
	}
	creator, err := space.NewFirstSpace(store)
	if err != nil {
		t.Fatalf("compose first Space: %v", err)
	}
	request := space.CreateFirstRequest{
		UserID: session.UserID, DisplayName: "Ada", SpaceName: "Research", IdempotencyKey: "create-research",
	}
	first, err := creator.Create(ctx, request)
	if err != nil {
		t.Fatalf("create first Space: %v", err)
	}
	replayed, err := creator.Create(ctx, request)
	if err != nil {
		t.Fatalf("replay first Space: %v", err)
	}
	if replayed.SpaceID != first.SpaceID || !replayed.CanManageMembers || !replayed.CanEnrollMachines {
		t.Fatalf("replayed first Space = %#v, first = %#v", replayed, first)
	}
	request.SpaceName = "Operations"
	if _, err := creator.Create(ctx, request); !errors.Is(err, space.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}

	otherChallenge, otherCode := prepareAcceptedEmailChallenge(t, ctx, store, credentials, "race-space@example.com", "request-race-space")
	otherSession, err := verifyEmail(t, ctx, store, credentials, otherChallenge, otherCode, "verify-race-space", uuid.NewString())
	if err != nil {
		t.Fatalf("verify racing creator: %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := range 2 {
		go func() {
			<-start
			_, err := creator.Create(ctx, space.CreateFirstRequest{
				UserID: otherSession.UserID, DisplayName: "Grace", SpaceName: fmt.Sprintf("Space %d", index),
				IdempotencyKey: fmt.Sprintf("space-%d", index),
			})
			results <- err
		}()
	}
	close(start)
	var won, rejected int
	for range 2 {
		if err := <-results; err == nil {
			won++
		} else if errors.Is(err, space.ErrAlreadyHasSpace) {
			rejected++
		} else {
			t.Fatalf("concurrent first Space error = %v", err)
		}
	}
	if won != 1 || rejected != 1 {
		t.Fatalf("first Space outcomes = %d won, %d rejected", won, rejected)
	}
}

type persistentEmailSubmitter struct {
	configuration string
	messages      []identity.EmailCodeMessage
	digests       [][sha256.Size]byte
}

func (submitter *persistentEmailSubmitter) PayloadDigest(message identity.EmailCodeMessage) ([sha256.Size]byte, error) {
	return sha256.Sum256([]byte(submitter.configuration + "\x00" + message.Recipient + "\x00" + message.Code + "\x00" + message.IdempotencyKey)), nil
}

func (submitter *persistentEmailSubmitter) SubmitEmailCode(
	_ context.Context,
	message identity.EmailCodeMessage,
	digest [sha256.Size]byte,
) identity.EmailSubmission {
	submitter.messages = append(submitter.messages, message)
	submitter.digests = append(submitter.digests, digest)
	return identity.EmailSubmission{State: identity.EmailSubmissionUnknown}
}

func testEmailCredentials(t *testing.T) identity.Credentials {
	t.Helper()
	credentials, err := identity.NewCredentials(bytes.Repeat([]byte{3}, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("create test Identity credentials: %v", err)
	}
	return credentials
}

func prepareAcceptedEmailChallenge(
	t *testing.T,
	ctx context.Context,
	store *Store,
	credentials identity.Credentials,
	email string,
	idempotencyKey string,
) (string, string) {
	t.Helper()
	canonical, err := identity.CanonicalEmail(email)
	if err != nil {
		t.Fatalf("canonicalize email: %v", err)
	}
	challengeID := uuid.NewString()
	code, err := credentials.EmailCode(challengeID, canonical)
	if err != nil {
		t.Fatalf("derive email code: %v", err)
	}
	codeDigest := credentials.CodeDigest(challengeID, code)
	sourceDigest := credentials.SourceDigest("127.0.0.1")
	requestDigest := credentials.RequestDigest("request-email-code", challengeID, canonical)
	challenge, err := store.PrepareEmailChallenge(ctx, identity.PrepareEmailChallengeCommand{
		ChallengeID: challengeID, CanonicalEmail: canonical, CodeDigest: codeDigest,
		SourceDigest: sourceDigest, IdempotencyKey: idempotencyKey, RequestDigest: requestDigest,
	})
	if err != nil {
		t.Fatalf("prepare email challenge: %v", err)
	}
	if _, err := store.RecordEmailSubmission(ctx, challenge.ChallengeID, challenge.PayloadDigest, identity.EmailSubmission{
		State: identity.EmailSubmissionAccepted, ProviderMessageID: "resend-" + challengeID,
	}); err != nil {
		t.Fatalf("accept email submission: %v", err)
	}
	return challengeID, code
}

func verifyEmail(
	t *testing.T,
	ctx context.Context,
	store *Store,
	credentials identity.Credentials,
	challengeID string,
	code string,
	idempotencyKey string,
	sessionID string,
) (identity.BrowserSession, error) {
	t.Helper()
	codeDigest := credentials.CodeDigest(challengeID, code)
	requestDigest := credentials.RequestDigest("verify-email-code", challengeID, code)
	return store.VerifyEmailChallenge(ctx, identity.VerifyEmailChallengeCommand{
		ChallengeID: challengeID, CodeDigest: codeDigest, IdempotencyKey: idempotencyKey,
		RequestDigest: requestDigest, SessionID: sessionID,
	})
}
