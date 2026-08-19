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
	UserID         string
	SpaceID        string
	TokenID        string
	UserToken      string
}

type BootstrapResult struct {
	UserID    string
	SpaceID   string
	UserToken string
}

// PrepareBootstrap creates the valid bootstrap identity before persistence begins.
func PrepareBootstrap(displayName string, spaceName string, tokenExpiresAt time.Time) (BootstrapCommand, error) {
	displayName = strings.TrimSpace(displayName)
	spaceName = strings.TrimSpace(spaceName)
	if displayName == "" {
		return BootstrapCommand{}, errors.New("display name is required")
	}
	if spaceName == "" {
		return BootstrapCommand{}, errors.New("space name is required")
	}
	if !tokenExpiresAt.After(time.Now()) {
		return BootstrapCommand{}, errors.New("token expiry must be in the future")
	}
	token, err := identity.NewUserToken()
	if err != nil {
		return BootstrapCommand{}, err
	}
	return BootstrapCommand{
		DisplayName: displayName, SpaceName: spaceName,
		TokenExpiresAt: tokenExpiresAt.UTC().Truncate(time.Microsecond),
		UserID:         uuid.NewString(), SpaceID: uuid.NewString(), TokenID: uuid.NewString(), UserToken: token.Secret,
	}, nil
}

func (s *Store) Bootstrap(ctx context.Context, command BootstrapCommand) (BootstrapResult, error) {
	displayName := strings.TrimSpace(command.DisplayName)
	spaceName := strings.TrimSpace(command.SpaceName)
	if displayName == "" || spaceName == "" || !command.TokenExpiresAt.After(time.Now()) ||
		uuid.Validate(command.UserID) != nil || uuid.Validate(command.SpaceID) != nil ||
		uuid.Validate(command.TokenID) != nil || !strings.HasPrefix(command.UserToken, "carry_user_") {
		return BootstrapResult{}, errors.New("prepared bootstrap identity is invalid")
	}
	tokenHash := identity.HashUserToken(command.UserToken)
	result := BootstrapResult{UserID: command.UserID, SpaceID: command.SpaceID, UserToken: command.UserToken}

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
		existing, loadErr := queries.LoadPreparedBootstrap(ctx, dbsqlc.LoadPreparedBootstrapParams{
			UserID: command.UserID, SpaceID: command.SpaceID, TokenID: command.TokenID,
		})
		if errors.Is(loadErr, pgx.ErrNoRows) {
			return BootstrapResult{}, ErrAlreadyBootstrapped
		}
		if loadErr != nil {
			return BootstrapResult{}, fmt.Errorf("load prepared bootstrap: %w", loadErr)
		}
		if existing.DisplayName != displayName || existing.SpaceName != spaceName ||
			!existing.CanEnrollMachines || !bytes.Equal(existing.TokenHash, tokenHash[:]) ||
			!existing.ExpiresAt.Time.Equal(command.TokenExpiresAt.UTC()) {
			return BootstrapResult{}, ErrAlreadyBootstrapped
		}
		if err := transaction.Commit(ctx); err != nil {
			return BootstrapResult{}, fmt.Errorf("commit idempotent bootstrap: %w", err)
		}
		return result, nil
	}

	if err := queries.CreateBootstrapUser(ctx, dbsqlc.CreateBootstrapUserParams{
		UserID: command.UserID, DisplayName: displayName,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap user: %w", err)
	}
	if err := queries.CreateBootstrapSpace(ctx, dbsqlc.CreateBootstrapSpaceParams{
		SpaceID: command.SpaceID, Name: spaceName,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap Space: %w", err)
	}
	if err := queries.CreateBootstrapMembership(ctx, dbsqlc.CreateBootstrapMembershipParams{
		SpaceID: command.SpaceID, UserID: command.UserID,
	}); err != nil {
		return BootstrapResult{}, fmt.Errorf("create bootstrap membership: %w", err)
	}
	if err := queries.CreateUserToken(ctx, dbsqlc.CreateUserTokenParams{
		TokenID: command.TokenID, UserID: command.UserID, TokenHash: tokenHash[:],
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
