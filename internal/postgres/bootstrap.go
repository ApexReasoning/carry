package postgres

import (
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
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAlreadyBootstrapped = errors.New("carry is already bootstrapped")

type Store struct {
	pool    *pgxpool.Pool
	queries *dbsqlc.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: dbsqlc.New(pool)}
}

type BootstrapCommand struct {
	DisplayName    string
	SpaceName      string
	TokenExpiresAt time.Time
}

type BootstrapResult struct {
	UserID    string
	SpaceID   string
	UserToken string
}

func (s *Store) Bootstrap(ctx context.Context, command BootstrapCommand) (BootstrapResult, error) {
	displayName := strings.TrimSpace(command.DisplayName)
	spaceName := strings.TrimSpace(command.SpaceName)
	if displayName == "" {
		return BootstrapResult{}, errors.New("display name is required")
	}
	if spaceName == "" {
		return BootstrapResult{}, errors.New("space name is required")
	}
	if !command.TokenExpiresAt.After(time.Now()) {
		return BootstrapResult{}, errors.New("token expiry must be in the future")
	}

	token, err := identity.NewUserToken()
	if err != nil {
		return BootstrapResult{}, err
	}
	result := BootstrapResult{
		UserID:    uuid.NewString(),
		SpaceID:   uuid.NewString(),
		UserToken: token.Secret,
	}

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("begin bootstrap: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	queries := s.queries.WithTx(transaction)

	const bootstrapLockID int64 = 181_417_715
	if err := queries.LockBootstrap(ctx, bootstrapLockID); err != nil {
		return BootstrapResult{}, fmt.Errorf("lock bootstrap: %w", err)
	}
	alreadyBootstrapped, err := queries.IsBootstrapped(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("check bootstrap state: %w", err)
	}
	if alreadyBootstrapped {
		return BootstrapResult{}, ErrAlreadyBootstrapped
	}

	if err := queries.CreateBootstrapUser(ctx, dbsqlc.CreateBootstrapUserParams{
		UserID: result.UserID, DisplayName: displayName,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap user: %w", err)
	}
	if err := queries.CreateBootstrapSpace(ctx, dbsqlc.CreateBootstrapSpaceParams{
		SpaceID: result.SpaceID, Name: spaceName,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap Space: %w", err)
	}
	if err := queries.CreateBootstrapMembership(ctx, dbsqlc.CreateBootstrapMembershipParams{
		SpaceID: result.SpaceID, UserID: result.UserID,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap membership: %w", err)
	}
	if err := queries.CreateUserToken(ctx, dbsqlc.CreateUserTokenParams{
		TokenID: uuid.NewString(), UserID: result.UserID, TokenHash: token.Hash[:],
		ExpiresAt: pgtype.Timestamptz{Time: command.TokenExpiresAt.UTC(), Valid: true},
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap token: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("commit bootstrap: %w", err)
	}
	return result, nil
}

func (s *Store) AuthenticateUserToken(ctx context.Context, secret string) (identity.AuthenticatedUser, error) {
	hash := identity.HashUserToken(secret)
	userID, err := s.queries.AuthenticateUserToken(ctx, hash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.AuthenticatedUser{}, fmt.Errorf("authenticate user token: %w", err)
	}
	return identity.AuthenticatedUser{UserID: userID}, nil
}
