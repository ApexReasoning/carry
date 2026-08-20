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
)

func (s *Store) CreateExternalLogin(
	ctx context.Context,
	command identity.CreateExternalLoginCommand,
) (time.Time, error) {
	if uuid.Validate(command.TransactionID) != nil || !validExternalProvider(command.Provider) {
		return time.Time{}, identity.ErrExternalLoginInvalid
	}
	expiresAt, err := s.queries.CreateExternalLogin(ctx, dbsqlc.CreateExternalLoginParams{
		TransactionID: command.TransactionID,
		Provider:      command.Provider.String(),
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("create external login transaction: %w", err)
	}
	if !expiresAt.Valid {
		return time.Time{}, errors.New("external login expiry is invalid")
	}
	return expiresAt.Time, nil
}

func (s *Store) ClaimExternalLogin(
	ctx context.Context,
	command identity.ClaimExternalLoginCommand,
) (identity.ExternalLoginClaim, error) {
	if err := command.Validate(); err != nil {
		return identity.ExternalLoginClaim{}, err
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.ExternalLoginClaim{}, fmt.Errorf("begin external login claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	current, err := queries.LockExternalLogin(ctx, command.TransactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginInvalid
	}
	if err != nil {
		return identity.ExternalLoginClaim{}, fmt.Errorf("lock external login: %w", err)
	}
	if current.Provider != command.Provider.String() {
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginInvalid
	}
	if len(current.CallbackDigest) != 0 && !bytes.Equal(current.CallbackDigest, command.CallbackDigest[:]) {
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginConflict
	}
	databaseTime, err := queries.ExternalLoginDatabaseTime(ctx)
	if err != nil || !databaseTime.Valid {
		return identity.ExternalLoginClaim{}, fmt.Errorf("load external login time: %w", err)
	}

	switch current.Status {
	case "succeeded":
		if !current.BrowserSessionID.Valid {
			return identity.ExternalLoginClaim{}, errors.New("completed external login has no Browser Session")
		}
		session, err := queries.LoadActiveBrowserSessionByID(ctx, uuidValue(current.BrowserSessionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ExternalLoginClaim{}, identity.ErrExternalLoginInvalid
		}
		if err != nil {
			return identity.ExternalLoginClaim{}, fmt.Errorf("load replayed external Browser Session: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return identity.ExternalLoginClaim{}, fmt.Errorf("commit external login replay: %w", err)
		}
		return identity.ExternalLoginClaim{IsReplay: true, Session: identity.BrowserSession{
			SessionID: session.SessionID,
			UserID:    session.UserID,
			ExpiresAt: session.ExpiresAt.Time,
		}}, nil
	case "denied":
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginDenied
	case "unknown":
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginUnavailable
	case "exchanging":
		if current.ExpiresAt.Valid && !current.ExpiresAt.Time.After(databaseTime.Time) {
			if _, err := queries.MarkExternalLoginUnknown(ctx, dbsqlc.MarkExternalLoginUnknownParams{
				TransactionID: command.TransactionID,
				Provider:      command.Provider.String(), CallbackDigest: command.CallbackDigest[:],
			}); err != nil {
				return identity.ExternalLoginClaim{}, fmt.Errorf("expire external login exchange: %w", err)
			}
			if err := transaction.Commit(ctx); err != nil {
				return identity.ExternalLoginClaim{}, fmt.Errorf("commit expired external login exchange: %w", err)
			}
			return identity.ExternalLoginClaim{}, identity.ErrExternalLoginUnavailable
		}
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginConflict
	case "prepared":
		if !current.ExpiresAt.Valid || !current.ExpiresAt.Time.After(databaseTime.Time) {
			return identity.ExternalLoginClaim{}, identity.ErrExternalLoginInvalid
		}
	default:
		return identity.ExternalLoginClaim{}, errors.New("external login status is invalid")
	}

	if command.Outcome == identity.ExternalCallbackCode {
		rows, err := queries.ClaimExternalLoginExchange(ctx, dbsqlc.ClaimExternalLoginExchangeParams{
			TransactionID: command.TransactionID,
			Provider:      command.Provider.String(), CallbackDigest: command.CallbackDigest[:],
		})
		if err != nil {
			return identity.ExternalLoginClaim{}, fmt.Errorf("claim external provider exchange: %w", err)
		}
		if rows != 1 {
			return identity.ExternalLoginClaim{}, identity.ErrExternalLoginInvalid
		}
		if err := transaction.Commit(ctx); err != nil {
			return identity.ExternalLoginClaim{}, fmt.Errorf("commit external provider exchange claim: %w", err)
		}
		return identity.ExternalLoginClaim{}, nil
	}

	status := "unknown"
	outcomeErr := identity.ErrExternalLoginUnavailable
	if command.Outcome == identity.ExternalCallbackDenied {
		status = "denied"
		outcomeErr = identity.ErrExternalLoginDenied
	}
	rows, err := queries.FinishExternalLoginWithoutAuthority(ctx, dbsqlc.FinishExternalLoginWithoutAuthorityParams{
		Status: status, CallbackDigest: command.CallbackDigest[:],
		TransactionID: command.TransactionID, Provider: command.Provider.String(),
	})
	if err != nil {
		return identity.ExternalLoginClaim{}, fmt.Errorf("finish external login without authority: %w", err)
	}
	if rows != 1 {
		return identity.ExternalLoginClaim{}, identity.ErrExternalLoginInvalid
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.ExternalLoginClaim{}, fmt.Errorf("commit external login without authority: %w", err)
	}
	return identity.ExternalLoginClaim{}, outcomeErr
}

func (s *Store) CompleteGoogleLogin(
	ctx context.Context,
	command identity.CompleteGoogleLoginCommand,
) (identity.BrowserSession, error) {
	if command.Issuer != "https://accounts.google.com" || strings.TrimSpace(command.Subject) == "" ||
		len(command.Subject) > 255 || uuid.Validate(command.TransactionID) != nil || uuid.Validate(command.SessionID) != nil {
		return identity.BrowserSession{}, identity.ErrExternalLoginInvalid
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("begin Google login completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := validateExternalCompletion(ctx, queries, command.TransactionID, identity.GoogleLoginProvider, command.CallbackDigest[:]); err != nil {
		return identity.BrowserSession{}, err
	}
	if err := queries.LockGoogleIdentityKey(ctx, dbsqlc.LockGoogleIdentityKeyParams{
		Issuer: command.Issuer, Subject: command.Subject,
	}); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("lock Google identity: %w", err)
	}
	userID, err := queries.LoadGoogleIdentity(ctx, dbsqlc.LoadGoogleIdentityParams{
		Issuer: command.Issuer, Subject: command.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		userID = uuid.NewString()
		if err := queries.CreateExternalLoginUser(ctx, userID); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("create Google User: %w", err)
		}
		if err := queries.CreateGoogleIdentity(ctx, dbsqlc.CreateGoogleIdentityParams{
			Issuer: command.Issuer, Subject: command.Subject, UserID: userID,
		}); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("create Google identity: %w", err)
		}
	} else if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load Google identity: %w", err)
	}
	return completeExternalLoginTransaction(ctx, transaction, queries, command.TransactionID, identity.GoogleLoginProvider, command.CallbackDigest[:], command.SessionID, userID)
}

func (s *Store) CompleteGitHubLogin(
	ctx context.Context,
	command identity.CompleteGitHubLoginCommand,
) (identity.BrowserSession, error) {
	if command.GitHubUserID <= 0 || uuid.Validate(command.TransactionID) != nil || uuid.Validate(command.SessionID) != nil {
		return identity.BrowserSession{}, identity.ErrExternalLoginInvalid
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("begin GitHub login completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if err := validateExternalCompletion(ctx, queries, command.TransactionID, identity.GitHubLoginProvider, command.CallbackDigest[:]); err != nil {
		return identity.BrowserSession{}, err
	}
	if err := queries.LockGitHubIdentityKey(ctx, command.GitHubUserID); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("lock GitHub identity: %w", err)
	}
	userID, err := queries.LoadGitHubIdentity(ctx, command.GitHubUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		userID = uuid.NewString()
		if err := queries.CreateExternalLoginUser(ctx, userID); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("create GitHub User: %w", err)
		}
		if err := queries.CreateGitHubIdentity(ctx, dbsqlc.CreateGitHubIdentityParams{
			GithubUserID: command.GitHubUserID, UserID: userID,
		}); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("create GitHub identity: %w", err)
		}
	} else if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load GitHub identity: %w", err)
	}
	return completeExternalLoginTransaction(ctx, transaction, queries, command.TransactionID, identity.GitHubLoginProvider, command.CallbackDigest[:], command.SessionID, userID)
}

func validateExternalCompletion(
	ctx context.Context,
	queries *dbsqlc.Queries,
	transactionID string,
	provider identity.ExternalLoginProvider,
	callbackDigest []byte,
) error {
	current, err := queries.LockExternalLogin(ctx, transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrExternalLoginInvalid
	}
	if err != nil {
		return fmt.Errorf("lock external login completion: %w", err)
	}
	if current.Provider != provider.String() || current.Status != "exchanging" ||
		!bytes.Equal(current.CallbackDigest, callbackDigest) {
		return identity.ErrExternalLoginConflict
	}
	databaseTime, err := queries.ExternalLoginDatabaseTime(ctx)
	if err != nil || !databaseTime.Valid {
		return fmt.Errorf("load external login completion time: %w", err)
	}
	if !current.ExpiresAt.Valid || !current.ExpiresAt.Time.After(databaseTime.Time) {
		return identity.ErrExternalLoginUnavailable
	}
	return nil
}

func completeExternalLoginTransaction(
	ctx context.Context,
	transaction pgx.Tx,
	queries *dbsqlc.Queries,
	transactionID string,
	provider identity.ExternalLoginProvider,
	callbackDigest []byte,
	sessionID string,
	userID string,
) (identity.BrowserSession, error) {
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: sessionID, UserID: userID,
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("create external Browser Session: %w", err)
	}
	userUUID, err := postgresUUID(userID)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("parse external User identity: %w", err)
	}
	sessionUUID, err := postgresUUID(sessionID)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("parse external Browser Session identity: %w", err)
	}
	rows, err := queries.CompleteExternalLogin(ctx, dbsqlc.CompleteExternalLoginParams{
		UserID: userUUID, SessionID: sessionUUID,
		TransactionID: transactionID, Provider: provider.String(), CallbackDigest: callbackDigest,
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("complete external login: %w", err)
	}
	if rows != 1 {
		return identity.BrowserSession{}, identity.ErrExternalLoginConflict
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("commit external login: %w", err)
	}
	return identity.BrowserSession{SessionID: created.SessionID, UserID: created.UserID, ExpiresAt: created.ExpiresAt.Time}, nil
}

func (s *Store) MarkExternalLoginUnknown(
	ctx context.Context,
	command identity.MarkExternalLoginUnknownCommand,
) error {
	if uuid.Validate(command.TransactionID) != nil || !validExternalProvider(command.Provider) {
		return identity.ErrExternalLoginInvalid
	}
	if _, err := s.queries.MarkExternalLoginUnknown(ctx, dbsqlc.MarkExternalLoginUnknownParams{
		TransactionID: command.TransactionID,
		Provider:      command.Provider.String(), CallbackDigest: command.CallbackDigest[:],
	}); err != nil {
		return fmt.Errorf("mark external login unknown: %w", err)
	}
	return nil
}

func validExternalProvider(provider identity.ExternalLoginProvider) bool {
	return provider == identity.GoogleLoginProvider || provider == identity.GitHubLoginProvider
}
