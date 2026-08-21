package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) CreateSpace(ctx context.Context, command space.CreateSpaceCommand) (space.CreatedSpace, error) {
	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("begin Space creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	queries := s.queries.WithTx(transaction)

	lockedUserID, err := queries.LockSpaceCreator(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return space.CreatedSpace{}, space.ErrForbidden
	}
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("lock Space creator: %w", err)
	}
	userID, err := postgresUUID(lockedUserID)
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("parse locked Space creator: %w", err)
	}

	idempotencyKey := command.IdempotencyKey
	existing, err := queries.LoadCreatedSpaceByRequest(ctx, dbsqlc.LoadCreatedSpaceByRequestParams{
		UserID:         userID,
		IdempotencyKey: &idempotencyKey,
	})
	if err == nil {
		if !bytes.Equal(existing.CreateRequestDigest, command.RequestDigest[:]) {
			return space.CreatedSpace{}, space.ErrIdempotencyConflict
		}
		if err := transaction.Commit(ctx); err != nil {
			return space.CreatedSpace{}, fmt.Errorf("commit Space creation replay: %w", err)
		}
		return restoreCreatedSpace(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return space.CreatedSpace{}, fmt.Errorf("load Space creation replay: %w", err)
	}

	err = queries.CreateSpace(ctx, dbsqlc.CreateSpaceParams{
		SpaceID:        command.SpaceID,
		Name:           command.Name,
		Slug:           command.Slug,
		UserID:         userID,
		IdempotencyKey: &idempotencyKey,
		RequestDigest:  command.RequestDigest[:],
	})
	if isSlugUniqueViolation(err) {
		return space.CreatedSpace{}, space.NewSlugConflictError(command)
	}
	if err != nil {
		return space.CreatedSpace{}, fmt.Errorf("create Space: %w", err)
	}
	if err := queries.CreateSpaceMembership(ctx, dbsqlc.CreateSpaceMembershipParams{
		SpaceID: command.SpaceID,
		UserID:  command.UserID,
	}); err != nil {
		return space.CreatedSpace{}, fmt.Errorf("create Space Membership: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return space.CreatedSpace{}, fmt.Errorf("commit Space creation: %w", err)
	}
	return space.CreatedSpace{
		SpaceID:           command.SpaceID,
		Name:              command.Name,
		Slug:              command.Slug,
		CanManageMembers:  true,
		CanEnrollMachines: true,
	}, nil
}

func isSlugUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "spaces_slug_unique"
}

func restoreCreatedSpace(row dbsqlc.LoadCreatedSpaceByRequestRow) space.CreatedSpace {
	return space.CreatedSpace{
		SpaceID:           row.SpaceID,
		Name:              row.Name,
		Slug:              row.Slug,
		CanManageMembers:  row.CanManageMembers,
		CanEnrollMachines: row.CanEnrollMachines,
	}
}
