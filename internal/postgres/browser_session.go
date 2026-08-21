package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/jackc/pgx/v5"
)

func (s *Store) AuthenticateBrowserSession(ctx context.Context, sessionID string) (identity.AuthenticatedUser, error) {
	user, err := s.queries.AuthenticateBrowserSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.AuthenticatedUser{}, fmt.Errorf("authenticate browser session: %w", err)
	}
	return identity.AuthenticatedUser{
		UserID:      user.UserID,
		DisplayName: user.DisplayName,
	}, nil
}

func (s *Store) RevokeBrowserSession(ctx context.Context, sessionID string) error {
	if _, err := s.queries.RevokeBrowserSession(ctx, sessionID); err != nil {
		return fmt.Errorf("revoke browser session: %w", err)
	}
	return nil
}
