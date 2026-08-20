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
	if command.Purpose == "" {
		command.Purpose = identity.LoginPurpose
	}
	if uuid.Validate(command.TransactionID) != nil || !validExternalProvider(command.Provider) ||
		!validIdentityProofTarget(command.Purpose, command.TargetUserID, command.InitiatingSessionID) {
		return time.Time{}, identity.ErrExternalLoginInvalid
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin external proof: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	if command.Purpose != identity.LoginPurpose {
		if _, err := queries.LockUserForIdentityChange(ctx, command.TargetUserID); errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, identity.ErrUnauthenticated
		} else if err != nil {
			return time.Time{}, fmt.Errorf("lock external proof User: %w", err)
		}
		if _, _, err := loadIdentitySession(
			ctx, queries, command.TargetUserID, command.InitiatingSessionID,
			command.Purpose == identity.LinkPurpose,
		); err != nil {
			return time.Time{}, err
		}
		if command.Purpose == identity.ReauthenticatePurpose {
			var methodErr error
			switch command.Provider {
			case identity.GoogleLoginProvider:
				_, methodErr = queries.LoadGoogleMethodForUser(ctx, command.TargetUserID)
			case identity.GitHubLoginProvider:
				_, methodErr = queries.LoadGitHubMethodForUser(ctx, command.TargetUserID)
			}
			if errors.Is(methodErr, pgx.ErrNoRows) {
				return time.Time{}, identity.ErrIdentityMethodNotLinked
			}
			if methodErr != nil {
				return time.Time{}, fmt.Errorf("load reauthentication method: %w", methodErr)
			}
		}
	}
	targetUserID, _ := nullablePostgresUUID(command.TargetUserID)
	initiatingSessionID, _ := nullablePostgresUUID(command.InitiatingSessionID)
	expiresAt, err := queries.CreateExternalLogin(ctx, dbsqlc.CreateExternalLoginParams{
		TransactionID: command.TransactionID, Provider: command.Provider.String(),
		Purpose: string(command.Purpose), TargetUserID: targetUserID, InitiatingSessionID: initiatingSessionID,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("create external login transaction: %w", err)
	}
	if !expiresAt.Valid {
		return time.Time{}, errors.New("external proof expiry is invalid")
	}
	if err := transaction.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit external proof: %w", err)
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
	claim := identity.ExternalLoginClaim{Purpose: identity.ProofPurpose(current.Purpose)}
	if len(current.CallbackDigest) != 0 && !bytes.Equal(current.CallbackDigest, command.CallbackDigest[:]) {
		return claim, identity.ErrExternalLoginConflict
	}
	databaseTime, err := queries.ExternalLoginDatabaseTime(ctx)
	if err != nil || !databaseTime.Valid {
		return claim, fmt.Errorf("load external login time: %w", err)
	}

	switch current.Status {
	case "succeeded":
		if !current.BrowserSessionID.Valid {
			return claim, errors.New("completed external login has no Browser Session")
		}
		session, err := queries.LoadActiveBrowserSessionByID(ctx, uuidValue(current.BrowserSessionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return claim, identity.ErrExternalLoginInvalid
		}
		if err != nil {
			return claim, fmt.Errorf("load replayed external Browser Session: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return claim, fmt.Errorf("commit external login replay: %w", err)
		}
		return identity.ExternalLoginClaim{IsReplay: true, Session: identity.BrowserSession{
			SessionID:           session.SessionID,
			UserID:              session.UserID,
			ExpiresAt:           session.ExpiresAt.Time,
			IdentityProvedAt:    session.IdentityProvedAt.Time,
			IdentityProofMethod: identity.Method(current.Provider),
			Purpose:             identity.ProofPurpose(current.Purpose),
		}, Purpose: identity.ProofPurpose(current.Purpose)}, nil
	case "denied":
		return claim, identity.ErrExternalLoginDenied
	case "rejected":
		return claim, identity.ErrExternalLoginRejected
	case "unknown":
		return claim, identity.ErrExternalLoginUnavailable
	case "exchanging":
		if current.ExpiresAt.Valid && !current.ExpiresAt.Time.After(databaseTime.Time) {
			if _, err := queries.MarkExternalLoginUnknown(ctx, dbsqlc.MarkExternalLoginUnknownParams{
				TransactionID: command.TransactionID,
				Provider:      command.Provider.String(), CallbackDigest: command.CallbackDigest[:],
			}); err != nil {
				return claim, fmt.Errorf("expire external login exchange: %w", err)
			}
			if err := transaction.Commit(ctx); err != nil {
				return claim, fmt.Errorf("commit expired external login exchange: %w", err)
			}
			return claim, identity.ErrExternalLoginUnavailable
		}
		return claim, identity.ErrExternalLoginConflict
	case "prepared":
		if !current.ExpiresAt.Valid || !current.ExpiresAt.Time.After(databaseTime.Time) {
			return claim, identity.ErrExternalLoginInvalid
		}
	default:
		return claim, errors.New("external login status is invalid")
	}

	if command.Outcome == identity.ExternalCallbackCode {
		rows, err := queries.ClaimExternalLoginExchange(ctx, dbsqlc.ClaimExternalLoginExchangeParams{
			TransactionID: command.TransactionID,
			Provider:      command.Provider.String(), CallbackDigest: command.CallbackDigest[:],
		})
		if err != nil {
			return claim, fmt.Errorf("claim external provider exchange: %w", err)
		}
		if rows != 1 {
			return claim, identity.ErrExternalLoginInvalid
		}
		if err := transaction.Commit(ctx); err != nil {
			return claim, fmt.Errorf("commit external provider exchange claim: %w", err)
		}
		return claim, nil
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
		return claim, fmt.Errorf("finish external login without authority: %w", err)
	}
	if rows != 1 {
		return claim, identity.ErrExternalLoginInvalid
	}
	if err := transaction.Commit(ctx); err != nil {
		return claim, fmt.Errorf("commit external login without authority: %w", err)
	}
	return claim, outcomeErr
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
		return identity.BrowserSession{}, fmt.Errorf("begin Google proof completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	current, err := validateExternalCompletion(ctx, queries, command.TransactionID, identity.GoogleLoginProvider, command.CallbackDigest[:])
	if err != nil {
		return identity.BrowserSession{}, err
	}
	userID, err := resolveGoogleIdentity(ctx, queries, current, command.Issuer, command.Subject)
	if err != nil {
		return identity.BrowserSession{}, err
	}
	return completeExternalProofTransaction(
		ctx, transaction, queries, current, identity.GoogleLoginProvider,
		command.CallbackDigest[:], command.SessionID, userID,
	)
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
		return identity.BrowserSession{}, fmt.Errorf("begin GitHub proof completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	current, err := validateExternalCompletion(ctx, queries, command.TransactionID, identity.GitHubLoginProvider, command.CallbackDigest[:])
	if err != nil {
		return identity.BrowserSession{}, err
	}
	userID, err := resolveGitHubIdentity(ctx, queries, current, command.GitHubUserID)
	if err != nil {
		return identity.BrowserSession{}, err
	}
	return completeExternalProofTransaction(
		ctx, transaction, queries, current, identity.GitHubLoginProvider,
		command.CallbackDigest[:], command.SessionID, userID,
	)
}

func validateExternalCompletion(
	ctx context.Context,
	queries *dbsqlc.Queries,
	transactionID string,
	provider identity.ExternalLoginProvider,
	callbackDigest []byte,
) (dbsqlc.ExternalLoginTransaction, error) {
	current, err := queries.LockExternalLogin(ctx, transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbsqlc.ExternalLoginTransaction{}, identity.ErrExternalLoginInvalid
	}
	if err != nil {
		return dbsqlc.ExternalLoginTransaction{}, fmt.Errorf("lock external proof completion: %w", err)
	}
	if current.Provider != provider.String() || current.Status != "exchanging" ||
		!bytes.Equal(current.CallbackDigest, callbackDigest) {
		return dbsqlc.ExternalLoginTransaction{}, identity.ErrExternalLoginConflict
	}
	databaseTime, err := queries.ExternalLoginDatabaseTime(ctx)
	if err != nil || !databaseTime.Valid {
		return dbsqlc.ExternalLoginTransaction{}, fmt.Errorf("load external proof completion time: %w", err)
	}
	if !current.ExpiresAt.Valid || !current.ExpiresAt.Time.After(databaseTime.Time) {
		return dbsqlc.ExternalLoginTransaction{}, identity.ErrExternalLoginUnavailable
	}
	return current, nil
}

func resolveGoogleIdentity(
	ctx context.Context,
	queries *dbsqlc.Queries,
	current dbsqlc.ExternalLoginTransaction,
	issuer string,
	subject string,
) (string, error) {
	if err := queries.LockGoogleIdentityKey(ctx, dbsqlc.LockGoogleIdentityKeyParams{Issuer: issuer, Subject: subject}); err != nil {
		return "", fmt.Errorf("lock Google identity: %w", err)
	}
	key := dbsqlc.LoadGoogleIdentityParams{Issuer: issuer, Subject: subject}
	owner, ownerErr := queries.LoadGoogleIdentity(ctx, key)
	purpose := identity.ProofPurpose(current.Purpose)
	if purpose == identity.LoginPurpose {
		if errors.Is(ownerErr, pgx.ErrNoRows) {
			owner = uuid.NewString()
			if err := queries.CreateExternalLoginUser(ctx, owner); err != nil {
				return "", fmt.Errorf("create Google User: %w", err)
			}
			if err := queries.CreateGoogleIdentity(ctx, dbsqlc.CreateGoogleIdentityParams{Issuer: issuer, Subject: subject, UserID: owner}); err != nil {
				return "", fmt.Errorf("create Google identity: %w", err)
			}
			return owner, nil
		}
		if ownerErr != nil {
			return "", fmt.Errorf("load Google identity: %w", ownerErr)
		}
		if _, err := queries.LockUserForIdentityChange(ctx, owner); err != nil {
			return "", identity.ErrUnauthenticated
		}
		currentOwner, err := queries.LoadGoogleIdentity(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return "", fmt.Errorf("revalidate Google identity: %w", err)
		}
		if currentOwner != owner {
			return "", identity.ErrIdentityMethodNotLinked
		}
		return owner, nil
	}

	userID := uuidValue(current.TargetUserID)
	if _, err := queries.LockUserForIdentityChange(ctx, userID); err != nil {
		return "", identity.ErrUnauthenticated
	}
	if _, _, err := loadIdentitySession(
		ctx, queries, userID, uuidValue(current.InitiatingSessionID), purpose == identity.LinkPurpose,
	); err != nil {
		return "", err
	}
	switch purpose {
	case identity.ReauthenticatePurpose:
		if errors.Is(ownerErr, pgx.ErrNoRows) {
			return "", identity.ErrIdentityMethodNotLinked
		}
		if ownerErr != nil {
			return "", fmt.Errorf("load reauthenticated Google identity: %w", ownerErr)
		}
		if owner != userID {
			return "", identity.ErrIdentityMethodNotLinked
		}
		currentOwner, err := queries.LoadGoogleIdentity(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return "", fmt.Errorf("revalidate Google identity: %w", err)
		}
		if currentOwner != userID {
			return "", identity.ErrIdentityMethodNotLinked
		}
		return userID, nil
	case identity.LinkPurpose:
		_, existingErr := queries.LoadGoogleMethodForUser(ctx, userID)
		if existingErr == nil {
			return "", identity.ErrIdentityMethodAlreadyLinked
		} else if !errors.Is(existingErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("load existing Google method: %w", existingErr)
		}
		if ownerErr == nil {
			if owner == userID {
				return "", identity.ErrIdentityMethodAlreadyLinked
			}
			return "", identity.ErrIdentityMethodOccupied
		}
		if !errors.Is(ownerErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("load candidate Google identity: %w", ownerErr)
		}
		if err := queries.CreateGoogleIdentity(ctx, dbsqlc.CreateGoogleIdentityParams{Issuer: issuer, Subject: subject, UserID: userID}); err != nil {
			return "", fmt.Errorf("link Google identity: %w", err)
		}
		return userID, nil
	default:
		return "", identity.ErrExternalLoginInvalid
	}
}

func resolveGitHubIdentity(
	ctx context.Context,
	queries *dbsqlc.Queries,
	current dbsqlc.ExternalLoginTransaction,
	githubUserID int64,
) (string, error) {
	if err := queries.LockGitHubIdentityKey(ctx, githubUserID); err != nil {
		return "", fmt.Errorf("lock GitHub identity: %w", err)
	}
	owner, ownerErr := queries.LoadGitHubIdentity(ctx, githubUserID)
	purpose := identity.ProofPurpose(current.Purpose)
	if purpose == identity.LoginPurpose {
		if errors.Is(ownerErr, pgx.ErrNoRows) {
			owner = uuid.NewString()
			if err := queries.CreateExternalLoginUser(ctx, owner); err != nil {
				return "", fmt.Errorf("create GitHub User: %w", err)
			}
			if err := queries.CreateGitHubIdentity(ctx, dbsqlc.CreateGitHubIdentityParams{GithubUserID: githubUserID, UserID: owner}); err != nil {
				return "", fmt.Errorf("create GitHub identity: %w", err)
			}
			return owner, nil
		}
		if ownerErr != nil {
			return "", fmt.Errorf("load GitHub identity: %w", ownerErr)
		}
		if _, err := queries.LockUserForIdentityChange(ctx, owner); err != nil {
			return "", identity.ErrUnauthenticated
		}
		currentOwner, err := queries.LoadGitHubIdentity(ctx, githubUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return "", fmt.Errorf("revalidate GitHub identity: %w", err)
		}
		if currentOwner != owner {
			return "", identity.ErrIdentityMethodNotLinked
		}
		return owner, nil
	}

	userID := uuidValue(current.TargetUserID)
	if _, err := queries.LockUserForIdentityChange(ctx, userID); err != nil {
		return "", identity.ErrUnauthenticated
	}
	if _, _, err := loadIdentitySession(
		ctx, queries, userID, uuidValue(current.InitiatingSessionID), purpose == identity.LinkPurpose,
	); err != nil {
		return "", err
	}
	switch purpose {
	case identity.ReauthenticatePurpose:
		if errors.Is(ownerErr, pgx.ErrNoRows) {
			return "", identity.ErrIdentityMethodNotLinked
		}
		if ownerErr != nil {
			return "", fmt.Errorf("load reauthenticated GitHub identity: %w", ownerErr)
		}
		if owner != userID {
			return "", identity.ErrIdentityMethodNotLinked
		}
		currentOwner, err := queries.LoadGitHubIdentity(ctx, githubUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrIdentityMethodNotLinked
		}
		if err != nil {
			return "", fmt.Errorf("revalidate GitHub identity: %w", err)
		}
		if currentOwner != userID {
			return "", identity.ErrIdentityMethodNotLinked
		}
		return userID, nil
	case identity.LinkPurpose:
		_, existingErr := queries.LoadGitHubMethodForUser(ctx, userID)
		if existingErr == nil {
			return "", identity.ErrIdentityMethodAlreadyLinked
		} else if !errors.Is(existingErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("load existing GitHub method: %w", existingErr)
		}
		if ownerErr == nil {
			if owner == userID {
				return "", identity.ErrIdentityMethodAlreadyLinked
			}
			return "", identity.ErrIdentityMethodOccupied
		}
		if !errors.Is(ownerErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("load candidate GitHub identity: %w", ownerErr)
		}
		if err := queries.CreateGitHubIdentity(ctx, dbsqlc.CreateGitHubIdentityParams{GithubUserID: githubUserID, UserID: userID}); err != nil {
			return "", fmt.Errorf("link GitHub identity: %w", err)
		}
		return userID, nil
	default:
		return "", identity.ErrExternalLoginInvalid
	}
}

func completeExternalProofTransaction(
	ctx context.Context,
	transaction pgx.Tx,
	queries *dbsqlc.Queries,
	current dbsqlc.ExternalLoginTransaction,
	provider identity.ExternalLoginProvider,
	callbackDigest []byte,
	sessionID string,
	userID string,
) (identity.BrowserSession, error) {
	purpose := identity.ProofPurpose(current.Purpose)
	if purpose == identity.ReauthenticatePurpose {
		if rows, err := queries.RevokeExactBrowserSession(ctx, uuidValue(current.InitiatingSessionID)); err != nil || rows != 1 {
			return identity.BrowserSession{}, identity.ErrUnauthenticated
		}
	} else if purpose == identity.LinkPurpose {
		if _, err := queries.RevokeUserBrowserSessions(ctx, userID); err != nil {
			return identity.BrowserSession{}, fmt.Errorf("revoke Browser Sessions after external link: %w", err)
		}
	}
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionID: sessionID, UserID: userID, IdentityProofMethod: provider.String(),
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("create external Browser Session: %w", err)
	}
	userUUID, _ := postgresUUID(userID)
	sessionUUID, _ := postgresUUID(sessionID)
	rows, err := queries.CompleteExternalLogin(ctx, dbsqlc.CompleteExternalLoginParams{
		UserID: userUUID, SessionID: sessionUUID, TransactionID: current.TransactionID,
		Provider: provider.String(), CallbackDigest: callbackDigest,
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("complete external proof: %w", err)
	}
	if rows != 1 {
		return identity.BrowserSession{}, identity.ErrExternalLoginConflict
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("commit external proof: %w", err)
	}
	return browserSession(created, purpose, identity.Method(provider.String())), nil
}

func (s *Store) RejectExternalLogin(
	ctx context.Context,
	command identity.MarkExternalLoginUnknownCommand,
) error {
	if uuid.Validate(command.TransactionID) != nil || !validExternalProvider(command.Provider) {
		return identity.ErrExternalLoginInvalid
	}
	rows, err := s.queries.RejectExternalLogin(ctx, dbsqlc.RejectExternalLoginParams{
		TransactionID: command.TransactionID, Provider: command.Provider.String(),
		CallbackDigest: command.CallbackDigest[:],
	})
	if err != nil {
		return fmt.Errorf("reject external proof: %w", err)
	}
	if rows != 1 {
		return identity.ErrExternalLoginConflict
	}
	return nil
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
