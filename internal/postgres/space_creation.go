package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateFirstSpace(ctx context.Context, command space.CreateFirstCommand) (space.CreatedSpace, error) {
	displayName := strings.TrimSpace(command.DisplayName)
	spaceName := strings.TrimSpace(command.SpaceName)
	if uuid.Validate(command.SpaceID) != nil || uuid.Validate(command.UserID) != nil ||
		displayName == "" || spaceName == "" || strings.TrimSpace(command.IdempotencyKey) == "" ||
		len(command.IdempotencyKey) > 255 {
		return space.CreatedSpace{}, space.ErrInvalidSpaceCreation
	}
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("begin first Space creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)
	existingName, err := queries.LockSpaceCreator(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return space.CreatedSpace{}, space.ErrForbidden
	}
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("lock first Space creator: %w", err)
	}
	userUUID, err := postgresUUID(command.UserID)
	if err != nil {
		return space.CreatedSpace{}, space.ErrInvalidSpaceCreation
	}
	idempotencyKey := command.IdempotencyKey
	existing, err := queries.LoadCreatedSpaceByRequest(ctx, dbsqlc.LoadCreatedSpaceByRequestParams{
		UserID: userUUID, IdempotencyKey: &idempotencyKey,
	})
	if err == nil {
		if existing.Name != spaceName || !bytes.Equal(existing.CreateRequestDigest, command.RequestDigest[:]) {
			return space.CreatedSpace{}, space.ErrIdempotencyConflict
		}
		if err := transaction.Commit(ctx); err != nil {
			return space.CreatedSpace{}, fmt.Errorf("commit first Space replay: %w", err)
		}
		return restoreCreatedSpace(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return space.CreatedSpace{}, fmt.Errorf("load first Space replay: %w", err)
	}
	hasMembership, err := queries.HasActiveMembership(ctx, command.UserID)
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("check first Space eligibility: %w", err)
	}
	if hasMembership {
		return space.CreatedSpace{}, space.ErrAlreadyHasSpace
	}
	if existingName != nil && *existingName != displayName {
		return space.CreatedSpace{}, space.ErrIdempotencyConflict
	}
	if existingName == nil {
		if err := queries.SetInitialDisplayName(ctx, dbsqlc.SetInitialDisplayNameParams{
			DisplayName: &displayName, UserID: command.UserID,
		}); err != nil {
			return space.CreatedSpace{}, fmt.Errorf("set initial User name: %w", err)
		}
	}
	if err := queries.CreateFirstSpace(ctx, dbsqlc.CreateFirstSpaceParams{
		SpaceID: command.SpaceID, Name: spaceName, UserID: userUUID,
		IdempotencyKey: &idempotencyKey, RequestDigest: command.RequestDigest[:],
	}); err != nil {
		return space.CreatedSpace{}, fmt.Errorf("create first Space: %w", err)
	}
	if err := queries.CreateFirstSpaceMembership(ctx, dbsqlc.CreateFirstSpaceMembershipParams{
		SpaceID: command.SpaceID, UserID: command.UserID,
	}); err != nil {
		return space.CreatedSpace{}, fmt.Errorf("create first Space Membership: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return space.CreatedSpace{}, fmt.Errorf("commit first Space: %w", err)
	}
	return space.CreatedSpace{
		SpaceID: command.SpaceID, Name: spaceName,
		CanManageMembers: true, CanEnrollMachines: true,
	}, nil
}

func restoreCreatedSpace(row dbsqlc.LoadCreatedSpaceByRequestRow) space.CreatedSpace {
	return space.CreatedSpace{
		SpaceID: row.SpaceID, Name: row.Name,
		CanManageMembers: row.CanManageMembers, CanEnrollMachines: row.CanEnrollMachines,
	}
}
