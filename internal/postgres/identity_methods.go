package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) ListIdentityMethods(
	ctx context.Context,
	userID string,
	sessionID string,
) (identity.IdentityMethods, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.IdentityMethods{}, fmt.Errorf("begin Identity method list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	session, databaseTime, err := loadIdentitySession(ctx, queries, userID, sessionID, false)
	if err != nil {
		return identity.IdentityMethods{}, err
	}
	listed, err := queries.LoadIdentityMethods(ctx, userID)
	if err != nil {
		return identity.IdentityMethods{}, fmt.Errorf("load Identity methods: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.IdentityMethods{}, fmt.Errorf("commit Identity method list: %w", err)
	}
	methods := make([]identity.Method, 0, 3)
	if listed.HasEmail {
		methods = append(methods, identity.EmailMethod)
	}
	if listed.HasGoogle {
		methods = append(methods, identity.GoogleMethod)
	}
	if listed.HasGithub {
		methods = append(methods, identity.GitHubMethod)
	}
	return identity.IdentityMethods{
		Methods: methods,
		ReauthenticationRequired: !session.IdentityProvedAt.Valid ||
			!session.IdentityProvedAt.Time.Add(identity.IdentityProofLifetime).After(databaseTime),
	}, nil
}

func (s *Store) EmailForReauthentication(
	ctx context.Context,
	userID string,
	sessionID string,
) (string, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin email reauthentication: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if _, _, err := loadIdentitySession(ctx, queries, userID, sessionID, false); err != nil {
		return "", err
	}
	email, err := queries.LoadEmailMethodForUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrIdentityMethodNotLinked
	}
	if err != nil {
		return "", fmt.Errorf("load email method: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit email reauthentication lookup: %w", err)
	}
	return email, nil
}

func (s *Store) UnlinkIdentityMethod(
	ctx context.Context,
	command identity.UnlinkIdentityMethodCommand,
) (identity.BrowserSession, error) {
	if uuid.Validate(command.InitiatingSessionID) != nil || uuid.Validate(command.ReplacementSessionID) != nil ||
		strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 {
		return identity.BrowserSession{}, identity.ErrUnauthenticated
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("begin Identity method unlink: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	replay, err := queries.LoadIdentityMethodUnlinkReplay(ctx, dbsqlc.LoadIdentityMethodUnlinkReplayParams{
		InitiatingSessionID: command.InitiatingSessionID, IdempotencyKey: command.IdempotencyKey,
	})
	if err == nil {
		if replay.Method != string(command.Method) || !bytes.Equal(replay.RequestDigest, command.RequestDigest[:]) {
			return identity.BrowserSession{}, identity.ErrIdempotencyConflict
		}
		session, err := queries.LoadActiveBrowserSessionByID(ctx, replay.ReplacementSessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.BrowserSession{}, identity.ErrUnauthenticated
		}
		if err != nil {
			return identity.BrowserSession{}, fmt.Errorf("load unlink replacement session: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("commit Identity method unlink replay: %w", err)
		}
		return identity.BrowserSession{
			SessionID: session.SessionID, UserID: session.UserID,
			ExpiresAt: session.ExpiresAt.Time, IdentityProvedAt: session.IdentityProvedAt.Time,
			IdentityProofMethod: identity.Method(session.IdentityProofMethod), Purpose: identity.LinkPurpose,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.BrowserSession{}, fmt.Errorf("load Identity method unlink replay: %w", err)
	}

	initiating, err := queries.LoadBrowserSessionByID(ctx, command.InitiatingSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.BrowserSession{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load unlink initiating Session: %w", err)
	}
	userID := initiating.UserID
	methodKey, err := lockIdentityMethodKey(ctx, queries, userID, command.Method)
	if err != nil {
		return identity.BrowserSession{}, err
	}
	if _, err := queries.LockUserForIdentityChange(ctx, userID); errors.Is(err, pgx.ErrNoRows) {
		return identity.BrowserSession{}, identity.ErrUnauthenticated
	} else if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("lock unlink User: %w", err)
	}
	lockedInitiating, _, err := loadIdentitySession(
		ctx, queries, userID, command.InitiatingSessionID, true,
	)
	if err != nil {
		return identity.BrowserSession{}, err
	}
	if err := revalidateIdentityMethodKey(ctx, queries, userID, methodKey); err != nil {
		return identity.BrowserSession{}, err
	}
	count, err := queries.CountIdentityMethods(ctx, userID)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("count Identity methods: %w", err)
	}
	if count <= 1 {
		return identity.BrowserSession{}, identity.ErrLastIdentityMethod
	}
	if identity.Method(lockedInitiating.IdentityProofMethod) == command.Method {
		return identity.BrowserSession{}, identity.ErrRecentIdentityProofRequired
	}
	var removed int64
	switch command.Method {
	case identity.EmailMethod:
		removed, err = queries.DeleteEmailMethod(ctx, userID)
	case identity.GoogleMethod:
		removed, err = queries.DeleteGoogleMethod(ctx, userID)
	case identity.GitHubMethod:
		removed, err = queries.DeleteGitHubMethod(ctx, userID)
	default:
		return identity.BrowserSession{}, identity.ErrIdentityMethodNotLinked
	}
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("remove Identity method: %w", err)
	}
	if removed != 1 {
		return identity.BrowserSession{}, identity.ErrIdentityMethodNotLinked
	}
	if _, err := queries.RevokeUserBrowserSessions(ctx, userID); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("revoke Browser Sessions after unlink: %w", err)
	}
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: command.ReplacementSessionID, UserID: userID,
		IdentityProofMethod: lockedInitiating.IdentityProofMethod,
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("create unlink replacement session: %w", err)
	}
	if err := queries.RecordIdentityMethodUnlink(ctx, dbsqlc.RecordIdentityMethodUnlinkParams{
		UserID: userID, InitiatingSessionID: command.InitiatingSessionID,
		Method: string(command.Method), IdempotencyKey: command.IdempotencyKey,
		RequestDigest: command.RequestDigest[:], ReplacementSessionID: command.ReplacementSessionID,
	}); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("record Identity method unlink: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("commit Identity method unlink: %w", err)
	}
	return browserSession(
		created, identity.LinkPurpose, identity.Method(lockedInitiating.IdentityProofMethod),
	), nil
}

type identityMethodKey struct {
	method       identity.Method
	email        string
	googleIssuer string
	googleSub    string
	githubID     int64
}

func lockIdentityMethodKey(
	ctx context.Context,
	queries *dbsqlc.Queries,
	userID string,
	method identity.Method,
) (identityMethodKey, error) {
	key := identityMethodKey{method: method}
	var owner string
	switch method {
	case identity.EmailMethod:
		email, err := queries.LoadEmailMethodForUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return identityMethodKey{}, fmt.Errorf("load email method for unlink: %w", err)
		}
		key.email = email
		if err := queries.LockEmailLogin(ctx, email); err != nil {
			return identityMethodKey{}, fmt.Errorf("lock email method for unlink: %w", err)
		}
		owner, err = queries.LoadEmailIdentity(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return identityMethodKey{}, fmt.Errorf("load email method owner for unlink: %w", err)
		}
	case identity.GoogleMethod:
		google, err := queries.LoadGoogleMethodForUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return identityMethodKey{}, fmt.Errorf("load Google method for unlink: %w", err)
		}
		key.googleIssuer, key.googleSub = google.Issuer, google.Subject
		if err := queries.LockGoogleIdentityKey(ctx, dbsqlc.LockGoogleIdentityKeyParams{
			Issuer: google.Issuer, Subject: google.Subject,
		}); err != nil {
			return identityMethodKey{}, fmt.Errorf("lock Google method for unlink: %w", err)
		}
		owner, err = queries.LoadGoogleIdentity(ctx, dbsqlc.LoadGoogleIdentityParams{
			Issuer: google.Issuer, Subject: google.Subject,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return identityMethodKey{}, fmt.Errorf("load Google method owner for unlink: %w", err)
		}
	case identity.GitHubMethod:
		githubID, err := queries.LoadGitHubMethodForUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return identityMethodKey{}, fmt.Errorf("load GitHub method for unlink: %w", err)
		}
		key.githubID = githubID
		if err := queries.LockGitHubIdentityKey(ctx, githubID); err != nil {
			return identityMethodKey{}, fmt.Errorf("lock GitHub method for unlink: %w", err)
		}
		owner, err = queries.LoadGitHubIdentity(ctx, githubID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return identityMethodKey{}, fmt.Errorf("load GitHub method owner for unlink: %w", err)
		}
	default:
		return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
	}
	if owner != userID {
		return identityMethodKey{}, identity.ErrIdentityMethodNotLinked
	}
	return key, nil
}

func revalidateIdentityMethodKey(
	ctx context.Context,
	queries *dbsqlc.Queries,
	userID string,
	key identityMethodKey,
) error {
	switch key.method {
	case identity.EmailMethod:
		email, err := queries.LoadEmailMethodForUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return fmt.Errorf("revalidate email method for unlink: %w", err)
		}
		if email != key.email {
			return identity.ErrIdentityMethodNotLinked
		}
	case identity.GoogleMethod:
		google, err := queries.LoadGoogleMethodForUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return fmt.Errorf("revalidate Google method for unlink: %w", err)
		}
		if google.Issuer != key.googleIssuer || google.Subject != key.googleSub {
			return identity.ErrIdentityMethodNotLinked
		}
	case identity.GitHubMethod:
		githubID, err := queries.LoadGitHubMethodForUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return fmt.Errorf("revalidate GitHub method for unlink: %w", err)
		}
		if githubID != key.githubID {
			return identity.ErrIdentityMethodNotLinked
		}
	default:
		return identity.ErrIdentityMethodNotLinked
	}
	return nil
}

func loadIdentitySession(
	ctx context.Context,
	queries *dbsqlc.Queries,
	userID string,
	sessionID string,
	requireRecent bool,
) (dbsqlc.BrowserSession, time.Time, error) {
	session, err := queries.LockBrowserSessionForIdentityChange(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbsqlc.BrowserSession{}, time.Time{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return dbsqlc.BrowserSession{}, time.Time{}, fmt.Errorf("lock Browser Session: %w", err)
	}
	databaseTime, err := queries.EmailLoginDatabaseTime(ctx)
	if err != nil || !databaseTime.Valid {
		return dbsqlc.BrowserSession{}, time.Time{}, fmt.Errorf("load Identity database time: %w", err)
	}
	if session.UserID != userID || session.RevokedAt.Valid || !session.ExpiresAt.Valid ||
		!session.ExpiresAt.Time.After(databaseTime.Time) {
		return dbsqlc.BrowserSession{}, time.Time{}, identity.ErrUnauthenticated
	}
	if requireRecent && (!session.IdentityProvedAt.Valid ||
		!session.IdentityProvedAt.Time.Add(identity.IdentityProofLifetime).After(databaseTime.Time)) {
		return dbsqlc.BrowserSession{}, time.Time{}, identity.ErrRecentIdentityProofRequired
	}
	return session, databaseTime.Time, nil
}

func validIdentityProofTarget(purpose identity.ProofPurpose, userID string, sessionID string) bool {
	if purpose == identity.LoginPurpose {
		return userID == "" && sessionID == ""
	}
	return (purpose == identity.ReauthenticatePurpose || purpose == identity.LinkPurpose) &&
		uuid.Validate(userID) == nil && uuid.Validate(sessionID) == nil
}

func nullablePostgresUUID(value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, nil
	}
	return postgresUUID(value)
}

func browserSession(
	row dbsqlc.BrowserSession,
	purpose identity.ProofPurpose,
	proofMethod identity.Method,
) identity.BrowserSession {
	return identity.BrowserSession{
		SessionID: row.SessionID, UserID: row.UserID, ExpiresAt: row.ExpiresAt.Time,
		IdentityProvedAt:    row.IdentityProvedAt.Time,
		IdentityProofMethod: proofMethod, Purpose: purpose,
	}
}
