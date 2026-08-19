package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) CreateBrowserSession(
	ctx context.Context,
	userToken string,
	requestedExpiresAt time.Time,
) (identity.BrowserSession, error) {
	if !requestedExpiresAt.After(time.Now()) {
		return identity.BrowserSession{}, errors.New("browser session expiry must be in the future")
	}
	secret, err := identity.NewBrowserSessionSecret()
	if err != nil {
		return identity.BrowserSession{}, err
	}
	tokenHash := identity.HashUserToken(userToken)
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("begin browser session creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	token, err := queries.LoadActiveUserTokenForBrowserSession(ctx, tokenHash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.BrowserSession{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("load browser session source token: %w", err)
	}
	expiresAt := requestedExpiresAt.UTC()
	if token.ExpiresAt.Time.Before(expiresAt) {
		expiresAt = token.ExpiresAt.Time
	}
	created, err := queries.CreateBrowserSession(ctx, dbsqlc.CreateBrowserSessionParams{
		SessionDigest: secret.Hash[:], UserID: token.UserID, SourceTokenID: token.TokenID,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return identity.BrowserSession{}, fmt.Errorf("insert browser session: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return identity.BrowserSession{}, fmt.Errorf("commit browser session creation: %w", err)
	}
	return identity.BrowserSession{Secret: secret.Secret, UserID: created.UserID, ExpiresAt: created.ExpiresAt.Time}, nil
}

func (s *Store) AuthenticateBrowserSession(ctx context.Context, secret string) (identity.AuthenticatedUser, error) {
	digest := identity.HashBrowserSessionSecret(secret)
	userID, err := s.queries.AuthenticateBrowserSession(ctx, digest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.AuthenticatedUser{}, fmt.Errorf("authenticate browser session: %w", err)
	}
	return identity.AuthenticatedUser{UserID: userID}, nil
}

func (s *Store) RevokeBrowserSession(ctx context.Context, secret string) error {
	digest := identity.HashBrowserSessionSecret(secret)
	if _, err := s.queries.RevokeBrowserSession(ctx, digest[:]); err != nil {
		return fmt.Errorf("revoke browser session: %w", err)
	}
	return nil
}
