package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxEmailChallengesPerHour  = 5
	maxSourceChallengesPerHour = 20
)

func (s *Store) PrepareEmailChallenge(
	ctx context.Context,
	command identity.PrepareEmailChallengeCommand,
) (identity.EmailChallenge, error) {
	if uuid.Validate(command.ChallengeID) != nil || command.CanonicalEmail == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 {
		return identity.EmailChallenge{}, errors.New("email challenge command is invalid")
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("begin email challenge: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	// Every request takes advisory locks in request-key, email, then source order.
	// The global request-key lock makes a conflicting insert observable as a
	// semantic replay instead of leaking the unique constraint as a database error.
	if err := queries.LockEmailRequest(ctx, command.IdempotencyKey); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("lock email request key: %w", err)
	}
	if err := queries.LockEmailLogin(ctx, command.CanonicalEmail); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("lock email challenge: %w", err)
	}
	if err := queries.LockEmailSource(ctx, command.SourceDigest[:]); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("lock email challenge source: %w", err)
	}
	existing, err := queries.LoadEmailChallengeByRequestKey(ctx, command.IdempotencyKey)
	if err == nil {
		if existing.CanonicalEmail != command.CanonicalEmail || !bytes.Equal(existing.RequestDigest, command.RequestDigest[:]) {
			return identity.EmailChallenge{}, identity.ErrIdempotencyConflict
		}
		if !bytes.Equal(existing.PayloadDigest, command.PayloadDigest[:]) {
			return identity.EmailChallenge{}, identity.ErrEmailPayloadChanged
		}
		databaseTime, timeErr := queries.EmailLoginDatabaseTime(ctx)
		if timeErr != nil {
			return identity.EmailChallenge{}, fmt.Errorf("load email challenge replay time: %w", timeErr)
		}
		if !databaseTime.Valid {
			return identity.EmailChallenge{}, errors.New("email challenge replay database time is invalid")
		}
		if err := transaction.Commit(ctx); err != nil {
			return identity.EmailChallenge{}, fmt.Errorf("commit email challenge replay: %w", err)
		}
		return restoreEmailChallenge(existing, databaseTime.Time), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.EmailChallenge{}, fmt.Errorf("load email challenge replay: %w", err)
	}

	emailCount, err := queries.CountRecentEmailChallenges(ctx, command.CanonicalEmail)
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("count recent email challenges: %w", err)
	}
	sourceCount, err := queries.CountRecentSourceChallenges(ctx, command.SourceDigest[:])
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("count recent email source challenges: %w", err)
	}
	latest, latestErr := queries.LoadLatestEmailChallenge(ctx, command.CanonicalEmail)
	databaseTime, timeErr := queries.EmailLoginDatabaseTime(ctx)
	if timeErr != nil {
		return identity.EmailChallenge{}, fmt.Errorf("load email challenge time: %w", timeErr)
	}
	if !databaseTime.Valid {
		return identity.EmailChallenge{}, errors.New("email challenge database time is invalid")
	}
	tooSoon := latestErr == nil && latest.CreatedAt.Valid && latest.CreatedAt.Time.Add(identity.EmailCodeResendDelay).After(databaseTime.Time)
	if latestErr != nil && !errors.Is(latestErr, pgx.ErrNoRows) {
		return identity.EmailChallenge{}, fmt.Errorf("load latest email challenge: %w", latestErr)
	}
	if emailCount >= maxEmailChallengesPerHour || sourceCount >= maxSourceChallengesPerHour || tooSoon {
		return identity.EmailChallenge{}, identity.ErrEmailRateLimited
	}
	if err := queries.InvalidateCurrentEmailChallenges(ctx, command.CanonicalEmail); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("invalidate previous email challenge: %w", err)
	}
	created, err := queries.CreateEmailChallenge(ctx, dbsqlc.CreateEmailChallengeParams{
		ChallengeID: command.ChallengeID, CanonicalEmail: command.CanonicalEmail,
		CodeDigest: command.CodeDigest[:], SourceDigest: command.SourceDigest[:], PayloadDigest: command.PayloadDigest[:],
		RequestIdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:],
	})
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("create email challenge: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("commit email challenge: %w", err)
	}
	return restoreEmailChallenge(created, databaseTime.Time), nil
}

func (s *Store) RecordEmailSubmission(
	ctx context.Context,
	challengeID string,
	payloadDigest [sha256.Size]byte,
	submission identity.EmailSubmission,
) (identity.EmailChallenge, error) {
	if uuid.Validate(challengeID) != nil || !validSubmission(submission) {
		return identity.EmailChallenge{}, errors.New("email submission is invalid")
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("begin email submission: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	current, err := queries.LockEmailChallenge(ctx, challengeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.EmailChallenge{}, identity.ErrInvalidCode
	}
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("lock email submission: %w", err)
	}
	if subtle.ConstantTimeCompare(current.PayloadDigest, payloadDigest[:]) != 1 {
		return identity.EmailChallenge{}, identity.ErrEmailPayloadChanged
	}
	if current.SubmissionState == string(identity.EmailSubmissionAccepted) || current.SubmissionState == string(identity.EmailSubmissionRejected) {
		if err := transaction.Commit(ctx); err != nil {
			return identity.EmailChallenge{}, fmt.Errorf("commit email submission replay: %w", err)
		}
		return restoreEmailChallenge(current, time.Time{}), nil
	}
	var providerMessageID *string
	if submission.State == identity.EmailSubmissionAccepted {
		providerMessageID = &submission.ProviderMessageID
	}
	updated, err := queries.RecordEmailSubmission(ctx, dbsqlc.RecordEmailSubmissionParams{
		ChallengeID: challengeID, PayloadDigest: payloadDigest[:],
		SubmissionState: string(submission.State), ProviderMessageID: providerMessageID,
	})
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("record email submission: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("commit email submission: %w", err)
	}
	return restoreEmailChallenge(updated, time.Time{}), nil
}

func (s *Store) VerifyEmailChallenge(
	ctx context.Context,
	command identity.VerifyEmailChallengeCommand,
) (identity.BrowserSession, error) {
	if uuid.Validate(command.ChallengeID) != nil || uuid.Validate(command.SessionID) != nil ||
		strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 {
		return identity.BrowserSession{}, identity.ErrInvalidCode
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("begin email verification: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	challenge, err := queries.LockEmailChallenge(ctx, command.ChallengeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.BrowserSession{}, identity.ErrInvalidCode
	}
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("lock email challenge: %w", err)
	}
	attempt, attemptErr := queries.LoadEmailAttempt(ctx, dbsqlc.LoadEmailAttemptParams{
		ChallengeID: command.ChallengeID, IdempotencyKey: command.IdempotencyKey,
	})
	if attemptErr == nil {
		if !bytes.Equal(attempt.RequestDigest, command.RequestDigest[:]) {
			return identity.BrowserSession{}, identity.ErrIdempotencyConflict
		}
		if attempt.Result != "succeeded" {
			return identity.BrowserSession{}, identity.ErrInvalidCode
		}
		session, err := loadEmailBrowserSession(ctx, queries, challenge)
		if err != nil {
			return identity.BrowserSession{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("commit email verification replay: %w", err)
		}
		return session, nil
	}
	if !errors.Is(attemptErr, pgx.ErrNoRows) {
		return identity.BrowserSession{}, fmt.Errorf("load email verification attempt: %w", attemptErr)
	}
	databaseTime, err := queries.EmailLoginDatabaseTime(ctx)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load email verification time: %w", err)
	}
	usable := !challenge.InvalidatedAt.Valid && !challenge.ConsumedAt.Valid &&
		challenge.ExpiresAt.Valid && databaseTime.Valid && challenge.ExpiresAt.Time.After(databaseTime.Time) &&
		challenge.AttemptsUsed < identity.EmailCodeAttemptLimit &&
		challenge.SubmissionState != string(identity.EmailSubmissionRejected)
	if !usable {
		return identity.BrowserSession{}, identity.ErrInvalidCode
	}
	if subtle.ConstantTimeCompare(challenge.CodeDigest, command.CodeDigest[:]) != 1 {
		if err := queries.RecordInvalidEmailAttempt(ctx, dbsqlc.RecordInvalidEmailAttemptParams{
			ChallengeID: command.ChallengeID, IdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:],
		}); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("record invalid email code: %w", err)
		}
		rows, err := queries.SpendEmailChallengeAttempt(ctx, command.ChallengeID)
		if err != nil {
			return identity.BrowserSession{}, fmt.Errorf("spend email code attempt: %w", err)
		}
		if rows != 1 {
			return identity.BrowserSession{}, identity.ErrInvalidCode
		}
		if err := transaction.Commit(ctx); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("commit invalid email code: %w", err)
		}
		return identity.BrowserSession{}, identity.ErrInvalidCode
	}

	userID, err := queries.LoadEmailIdentity(ctx, challenge.CanonicalEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		userID = uuid.NewString()
		if err := queries.CreateEmailUser(ctx, userID); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("create email User: %w", err)
		}
		if err := queries.CreateEmailIdentity(ctx, dbsqlc.CreateEmailIdentityParams{
			CanonicalEmail: challenge.CanonicalEmail, UserID: userID,
		}); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("create email identity: %w", err)
		}
	} else if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load email identity: %w", err)
	}
	created, err := queries.CreateEmailBrowserSession(ctx, dbsqlc.CreateEmailBrowserSessionParams{
		SessionID: command.SessionID, UserID: userID,
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("create email browser session: %w", err)
	}
	userUUID, err := postgresUUID(userID)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("parse email User identity: %w", err)
	}
	sessionUUID, err := postgresUUID(command.SessionID)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("parse browser session identity: %w", err)
	}
	rows, err := queries.ConsumeEmailChallenge(ctx, dbsqlc.ConsumeEmailChallengeParams{
		UserID: userUUID, SessionID: sessionUUID, ChallengeID: command.ChallengeID,
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("consume email challenge: %w", err)
	}
	if rows != 1 {
		return identity.BrowserSession{}, identity.ErrInvalidCode
	}
	if err := queries.RecordSuccessfulEmailAttempt(ctx, dbsqlc.RecordSuccessfulEmailAttemptParams{
		ChallengeID: command.ChallengeID, IdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:],
	}); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("record successful email verification: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("commit email verification: %w", err)
	}
	return identity.BrowserSession{SessionID: created.SessionID, UserID: created.UserID, ExpiresAt: created.ExpiresAt.Time}, nil
}

func loadEmailBrowserSession(
	ctx context.Context,
	queries *dbsqlc.Queries,
	challenge dbsqlc.EmailLoginChallenge,
) (identity.BrowserSession, error) {
	if !challenge.BrowserSessionID.Valid {
		return identity.BrowserSession{}, errors.New("consumed email challenge has no browser session")
	}
	session, err := queries.LoadBrowserSessionByID(ctx, uuidValue(challenge.BrowserSessionID))
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load replayed browser session: %w", err)
	}
	if session.RevokedAt.Valid {
		return identity.BrowserSession{}, identity.ErrUnauthenticated
	}
	return identity.BrowserSession{SessionID: session.SessionID, UserID: session.UserID, ExpiresAt: session.ExpiresAt.Time}, nil
}

func restoreEmailChallenge(row dbsqlc.EmailLoginChallenge, databaseTime time.Time) identity.EmailChallenge {
	providerMessageID := ""
	if row.ProviderMessageID != nil {
		providerMessageID = *row.ProviderMessageID
	}
	var payloadDigest [sha256.Size]byte
	copy(payloadDigest[:], row.PayloadDigest)
	return identity.EmailChallenge{
		ChallengeID: row.ChallengeID, CanonicalEmail: row.CanonicalEmail,
		ExpiresAt: row.ExpiresAt.Time, SubmissionState: identity.EmailSubmissionState(row.SubmissionState),
		ProviderMessageID: providerMessageID, PayloadDigest: payloadDigest,
		CanSubmit: databaseTime.IsZero() || (row.ExpiresAt.Valid && row.ExpiresAt.Time.After(databaseTime)),
	}
}

func validSubmission(submission identity.EmailSubmission) bool {
	switch submission.State {
	case identity.EmailSubmissionAccepted:
		return strings.TrimSpace(submission.ProviderMessageID) != ""
	case identity.EmailSubmissionRejected, identity.EmailSubmissionUnknown:
		return submission.ProviderMessageID == ""
	default:
		return false
	}
}
