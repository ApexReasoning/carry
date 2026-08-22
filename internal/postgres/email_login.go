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

func (s *Store) PrepareEmailChallenge(
	ctx context.Context,
	command identity.PrepareEmailChallengeCommand,
) (identity.EmailChallenge, error) {
	if command.Purpose == "" {
		command.Purpose = identity.LoginPurpose
	}
	if uuid.Validate(command.ChallengeID) != nil || command.CanonicalEmail == "" ||
		strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 ||
		!validIdentityProofTarget(command.Purpose, command.TargetUserID, command.InitiatingSessionID) {
		return identity.EmailChallenge{}, errors.New("email challenge command is invalid")
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("begin email challenge: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if command.Purpose != identity.LoginPurpose {
		if _, err := queries.LockUserForIdentityChange(ctx, command.TargetUserID); errors.Is(err, pgx.ErrNoRows) {
			return identity.EmailChallenge{}, identity.ErrUnauthenticated
		} else if err != nil {
			return identity.EmailChallenge{}, fmt.Errorf("lock email proof User: %w", err)
		}
		if _, _, err := loadIdentitySession(
			ctx, queries, command.TargetUserID, command.InitiatingSessionID,
			command.Purpose == identity.LinkPurpose,
		); err != nil {
			return identity.EmailChallenge{}, err
		}
		if command.Purpose == identity.ReauthenticatePurpose {
			linkedEmail, err := queries.LoadEmailMethodForUser(ctx, command.TargetUserID)
			if errors.Is(err, pgx.ErrNoRows) || linkedEmail != command.CanonicalEmail {
				return identity.EmailChallenge{}, identity.ErrIdentityMethodNotLinked
			}
			if err != nil {
				return identity.EmailChallenge{}, fmt.Errorf("load reauthentication email: %w", err)
			}
		}
	}
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
	if emailCount >= identity.EmailAddressChallengeLimitPerHour {
		return identity.EmailChallenge{}, identity.ErrEmailAddressAdmissionLimited
	}
	if sourceCount >= identity.EmailSourceChallengeLimitPerHour {
		return identity.EmailChallenge{}, identity.ErrEmailSourceAdmissionLimited
	}
	if tooSoon {
		return identity.EmailChallenge{}, identity.ErrEmailResendDelayed
	}
	if err := queries.InvalidateCurrentEmailChallenges(ctx, command.CanonicalEmail); err != nil {
		return identity.EmailChallenge{}, fmt.Errorf("invalidate previous email challenge: %w", err)
	}
	targetUserID, err := nullablePostgresUUID(command.TargetUserID)
	if err != nil {
		return identity.EmailChallenge{}, errors.New("email proof target User is invalid")
	}
	initiatingSessionID, err := nullablePostgresUUID(command.InitiatingSessionID)
	if err != nil {
		return identity.EmailChallenge{}, errors.New("email proof initiating session is invalid")
	}
	created, err := queries.CreateEmailChallenge(ctx, dbsqlc.CreateEmailChallengeParams{
		ChallengeID: command.ChallengeID, CanonicalEmail: command.CanonicalEmail,
		CodeDigest: command.CodeDigest[:], SourceDigest: command.SourceDigest[:], PayloadDigest: command.PayloadDigest[:],
		RequestIdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:],
		Purpose: string(command.Purpose), TargetUserID: targetUserID, InitiatingSessionID: initiatingSessionID,
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
	if command.Purpose == "" {
		command.Purpose = identity.LoginPurpose
	}
	if uuid.Validate(command.ChallengeID) != nil || uuid.Validate(command.SessionID) != nil ||
		strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 ||
		!validVerifyEmailTarget(command.Purpose, command.InitiatingSessionID) {
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
	if challenge.Purpose != string(command.Purpose) ||
		uuidValue(challenge.InitiatingSessionID) != command.InitiatingSessionID {
		return identity.BrowserSession{}, identity.ErrInvalidCode
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
	if err != nil || !databaseTime.Valid {
		return identity.BrowserSession{}, fmt.Errorf("load email verification time: %w", err)
	}
	usable := !challenge.InvalidatedAt.Valid && !challenge.ConsumedAt.Valid &&
		challenge.ExpiresAt.Valid && challenge.ExpiresAt.Time.After(databaseTime.Time) &&
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

	var created dbsqlc.BrowserSession
	switch command.Purpose {
	case identity.LoginPurpose:
		created, err = completeEmailLogin(ctx, queries, challenge, command.SessionID)
	case identity.ReauthenticatePurpose:
		created, err = completeEmailReauthentication(ctx, queries, challenge, command.SessionID)
	case identity.LinkPurpose:
		created, err = completeEmailLink(ctx, queries, challenge, command.SessionID)
	default:
		err = identity.ErrInvalidCode
	}
	if err != nil {
		return identity.BrowserSession{}, err
	}
	userUUID, err := postgresUUID(created.UserID)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("parse email User identity: %w", err)
	}
	sessionUUID, err := postgresUUID(created.SessionID)
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
	return browserSession(created, command.Purpose, identity.EmailMethod), nil
}

func completeEmailLogin(
	ctx context.Context,
	queries *dbsqlc.Queries,
	challenge dbsqlc.EmailLoginChallenge,
	sessionID string,
) (dbsqlc.BrowserSession, error) {
	if err := queries.LockEmailLogin(ctx, challenge.CanonicalEmail); err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("lock email identity: %w", err)
	}
	userID, err := queries.LoadEmailIdentity(ctx, challenge.CanonicalEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		userID = uuid.NewString()
		displayName, err := identity.FallbackDisplayName(userID)
		if err != nil {
			return dbsqlc.BrowserSession{}, fmt.Errorf("derive email User label: %w", err)
		}
		if err := queries.CreateEmailUser(ctx, dbsqlc.CreateEmailUserParams{
			UserID:      userID,
			DisplayName: displayName,
		}); err != nil {
			return dbsqlc.BrowserSession{}, fmt.Errorf("create email User: %w", err)
		}
		if err := queries.CreateEmailIdentity(ctx, dbsqlc.CreateEmailIdentityParams{
			CanonicalEmail: challenge.CanonicalEmail, UserID: userID,
		}); err != nil {
			return dbsqlc.BrowserSession{}, fmt.Errorf("create email identity: %w", err)
		}
	} else if err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("load email identity: %w", err)
	} else {
		if _, err := queries.LockUserForIdentityChange(ctx, userID); err != nil {
			return dbsqlc.BrowserSession{}, identity.ErrUnauthenticated
		}
		currentOwner, err := queries.LoadEmailIdentity(ctx, challenge.CanonicalEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return dbsqlc.BrowserSession{}, fmt.Errorf("revalidate email identity: %w", err)
		}
		if currentOwner != userID {
			return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodNotLinked
		}
	}
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: sessionID, UserID: userID, IdentityProofMethod: string(identity.EmailMethod),
	})
	if err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("create email Browser Session: %w", err)
	}
	return created, nil
}

func completeEmailReauthentication(
	ctx context.Context,
	queries *dbsqlc.Queries,
	challenge dbsqlc.EmailLoginChallenge,
	sessionID string,
) (dbsqlc.BrowserSession, error) {
	if err := queries.LockEmailLogin(ctx, challenge.CanonicalEmail); err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("lock reauthenticated email identity: %w", err)
	}
	owner, err := queries.LoadEmailIdentity(ctx, challenge.CanonicalEmail)
	userID := uuidValue(challenge.TargetUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodNotLinked
	}
	if err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("load reauthenticated email identity: %w", err)
	}
	if owner != userID {
		return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodNotLinked
	}
	if _, err := queries.LockUserForIdentityChange(ctx, userID); err != nil {
		return dbsqlc.BrowserSession{}, identity.ErrUnauthenticated
	}
	if _, _, err := loadIdentitySession(ctx, queries, userID, uuidValue(challenge.InitiatingSessionID), false); err != nil {
		return dbsqlc.BrowserSession{}, err
	}
	currentOwner, err := queries.LoadEmailIdentity(ctx, challenge.CanonicalEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodNotLinked
	}
	if err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("revalidate reauthenticated email identity: %w", err)
	}
	if currentOwner != userID {
		return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodNotLinked
	}
	if rows, err := queries.RevokeExactBrowserSession(ctx, uuidValue(challenge.InitiatingSessionID)); err != nil || rows != 1 {
		return dbsqlc.BrowserSession{}, identity.ErrUnauthenticated
	}
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: sessionID, UserID: userID, IdentityProofMethod: string(identity.EmailMethod),
	})
	if err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("create reauthenticated Browser Session: %w", err)
	}
	return created, nil
}

func completeEmailLink(
	ctx context.Context,
	queries *dbsqlc.Queries,
	challenge dbsqlc.EmailLoginChallenge,
	sessionID string,
) (dbsqlc.BrowserSession, error) {
	if err := queries.LockEmailLogin(ctx, challenge.CanonicalEmail); err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("lock linked email identity: %w", err)
	}
	candidateOwner, candidateErr := queries.LoadEmailIdentity(ctx, challenge.CanonicalEmail)
	if candidateErr != nil && !errors.Is(candidateErr, pgx.ErrNoRows) {
		return dbsqlc.BrowserSession{}, fmt.Errorf("load candidate email identity: %w", candidateErr)
	}
	userID := uuidValue(challenge.TargetUserID)
	if _, err := queries.LockUserForIdentityChange(ctx, userID); err != nil {
		return dbsqlc.BrowserSession{}, identity.ErrUnauthenticated
	}
	if _, _, err := loadIdentitySession(ctx, queries, userID, uuidValue(challenge.InitiatingSessionID), true); err != nil {
		return dbsqlc.BrowserSession{}, err
	}
	if _, err := queries.LoadEmailMethodForUser(ctx, userID); err == nil {
		return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodAlreadyLinked
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return dbsqlc.BrowserSession{}, fmt.Errorf("load existing email method: %w", err)
	}
	if candidateErr == nil {
		if candidateOwner == userID {
			return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodAlreadyLinked
		}
		return dbsqlc.BrowserSession{}, identity.ErrIdentityMethodOccupied
	}
	if err := queries.CreateEmailIdentity(ctx, dbsqlc.CreateEmailIdentityParams{
		CanonicalEmail: challenge.CanonicalEmail, UserID: userID,
	}); err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("link email identity: %w", err)
	}
	if _, err := queries.RevokeUserBrowserSessions(ctx, userID); err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("revoke Browser Sessions after email link: %w", err)
	}
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: sessionID, UserID: userID, IdentityProofMethod: string(identity.EmailMethod),
	})
	if err != nil {
		return dbsqlc.BrowserSession{}, fmt.Errorf("create email link replacement Session: %w", err)
	}
	return created, nil
}

func loadEmailBrowserSession(
	ctx context.Context,
	queries *dbsqlc.Queries,
	challenge dbsqlc.EmailLoginChallenge,
) (identity.BrowserSession, error) {
	if !challenge.BrowserSessionID.Valid {
		return identity.BrowserSession{}, errors.New("consumed email challenge has no Browser Session")
	}
	session, err := queries.LoadActiveBrowserSessionByID(ctx, uuidValue(challenge.BrowserSessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.BrowserSession{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load replayed active Browser Session: %w", err)
	}
	return identity.BrowserSession{
		SessionID: session.SessionID, UserID: session.UserID,
		ExpiresAt: session.ExpiresAt.Time, IdentityProvedAt: session.IdentityProvedAt.Time,
		IdentityProofMethod: identity.EmailMethod,
		Purpose:             identity.ProofPurpose(challenge.Purpose),
	}, nil
}

func validVerifyEmailTarget(purpose identity.ProofPurpose, initiatingSessionID string) bool {
	if purpose == identity.LoginPurpose {
		return initiatingSessionID == ""
	}
	return (purpose == identity.ReauthenticatePurpose || purpose == identity.LinkPurpose) &&
		uuid.Validate(initiatingSessionID) == nil
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
