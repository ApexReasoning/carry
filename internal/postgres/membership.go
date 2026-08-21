package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) ListMemberships(ctx context.Context, userID string) ([]space.Membership, error) {
	rows, err := s.queries.ListMemberships(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	memberships := make([]space.Membership, 0, len(rows))
	for _, row := range rows {
		memberships = append(memberships, space.Membership{
			SpaceID: row.SpaceID, Name: row.Name,
			CanManageMembers: row.CanManageMembers, CanEnrollMachines: row.CanEnrollMachines,
		})
	}
	return memberships, nil
}

func (s *Store) ListSpaceMembers(ctx context.Context, command space.ListMembersCommand) (space.MemberPage, error) {
	if uuid.Validate(command.ActorUserID) != nil || uuid.Validate(command.SpaceID) != nil {
		return space.MemberPage{}, space.ErrForbidden
	}
	if command.AfterUserID != "" && uuid.Validate(command.AfterUserID) != nil {
		return space.MemberPage{}, space.ErrInvalidMemberCursor
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return space.MemberPage{}, fmt.Errorf("begin Space member list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	if _, err := q.LockInvitationActorMembership(ctx, dbsqlc.LockInvitationActorMembershipParams{SpaceID: command.SpaceID, UserID: command.ActorUserID}); errors.Is(err, pgx.ErrNoRows) {
		return space.MemberPage{}, space.ErrForbidden
	} else if err != nil {
		return space.MemberPage{}, fmt.Errorf("lock member list authority: %w", err)
	}
	cursorTime := pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}
	cursorUserID := "00000000-0000-0000-0000-000000000000"
	if command.AfterUserID != "" {
		cursorTime, err = q.SpaceMemberCursor(ctx, dbsqlc.SpaceMemberCursorParams{SpaceID: command.SpaceID, UserID: command.AfterUserID})
		if errors.Is(err, pgx.ErrNoRows) {
			return space.MemberPage{}, space.ErrInvalidMemberCursor
		}
		if err != nil {
			return space.MemberPage{}, fmt.Errorf("load Space member cursor: %w", err)
		}
		cursorUserID = command.AfterUserID
	}
	rows, err := q.ListActiveSpaceMembers(ctx, dbsqlc.ListActiveSpaceMembersParams{SpaceID: command.SpaceID, CursorCreatedAt: cursorTime, CursorUserID: cursorUserID})
	if err != nil {
		return space.MemberPage{}, fmt.Errorf("list active Space members: %w", err)
	}
	page := space.MemberPage{Members: make([]space.SpaceMember, 0, len(rows))}
	for _, row := range rows {
		displayName := ""
		if row.DisplayName != nil {
			displayName = *row.DisplayName
		}
		page.Members = append(page.Members, space.SpaceMember{
			UserID: row.UserID, DisplayName: displayName,
			CanManageMembers: row.CanManageMembers, CanEnrollMachines: row.CanEnrollMachines,
			OpenWorkCount: row.OpenWorkCount, JoinedAt: row.JoinedAt.Time,
		})
	}
	if len(page.Members) > space.MemberPageSize {
		page.Members = page.Members[:space.MemberPageSize]
		page.NextCursor = page.Members[len(page.Members)-1].UserID
	}
	if err := tx.Commit(ctx); err != nil {
		return space.MemberPage{}, fmt.Errorf("commit Space member list: %w", err)
	}
	return page, nil
}

func (s *Store) RemoveSpaceMember(ctx context.Context, command space.RemoveMemberCommand) error {
	validated, err := space.NewRemoveMemberCommand(space.RemoveMemberRequest{
		SpaceID: command.SpaceID, ActorUserID: command.ActorUserID, TargetUserID: command.TargetUserID,
		SuccessorUserID: command.SuccessorUserID, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil || validated.RequestDigest != command.RequestDigest {
		return space.ErrInvalidMemberRemoval
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Space member removal: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	if _, err := q.LockSpaceForMemberRemoval(ctx, command.SpaceID); errors.Is(err, pgx.ErrNoRows) {
		return space.ErrMemberUnavailable
	} else if err != nil {
		return fmt.Errorf("lock Space for member removal: %w", err)
	}
	actorUUID, _ := postgresUUID(command.ActorUserID)
	key := command.IdempotencyKey
	replay, err := q.LoadMemberRemovalReplay(ctx, dbsqlc.LoadMemberRemovalReplayParams{SpaceID: command.SpaceID, ActorUserID: actorUUID, IdempotencyKey: &key})
	if err == nil {
		if replay.UserID != command.TargetUserID || uuidValue(replay.RemovalSuccessorUserID) != command.SuccessorUserID || !bytes.Equal(replay.RemovalRequestDigest, command.RequestDigest[:]) {
			return space.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit member removal replay: %w", err)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load member removal replay: %w", err)
	}

	participantIDs := []string{command.ActorUserID, command.TargetUserID}
	if command.SuccessorUserID != "" {
		participantIDs = append(participantIDs, command.SuccessorUserID)
	}
	sort.Strings(participantIDs)
	memberships := make(map[string]dbsqlc.LockMembershipForRemovalRow, len(participantIDs))
	for _, userID := range participantIDs {
		if _, exists := memberships[userID]; exists {
			continue
		}
		row, lockErr := q.LockMembershipForRemoval(ctx, dbsqlc.LockMembershipForRemovalParams{SpaceID: command.SpaceID, UserID: userID})
		if errors.Is(lockErr, pgx.ErrNoRows) {
			continue
		}
		if lockErr != nil {
			return fmt.Errorf("lock member removal participant: %w", lockErr)
		}
		memberships[userID] = row
	}
	actor, exists := memberships[command.ActorUserID]
	if !exists || actor.RevokedAt.Valid || !actor.CanManageMembers {
		return space.ErrForbidden
	}
	target, exists := memberships[command.TargetUserID]
	if !exists || target.RevokedAt.Valid {
		return space.ErrMemberUnavailable
	}
	if command.SuccessorUserID != "" {
		successor, exists := memberships[command.SuccessorUserID]
		if command.SuccessorUserID == command.TargetUserID || !exists || successor.RevokedAt.Valid {
			return space.ErrRemovalSuccessorInvalid
		}
	}
	authorities, err := q.CountActiveSpaceAuthorities(ctx, command.SpaceID)
	if err != nil {
		return fmt.Errorf("count Space removal authorities: %w", err)
	}
	if target.CanManageMembers && authorities.MemberManagers <= 1 {
		return space.ErrLastMemberManager
	}
	if target.CanEnrollMachines && authorities.MachineEnrollers <= 1 {
		return space.ErrLastMachineEnroller
	}
	workIDs, err := q.LockOpenWorksOwnedByMember(ctx, dbsqlc.LockOpenWorksOwnedByMemberParams{SpaceID: command.SpaceID, UserID: command.TargetUserID})
	if err != nil {
		return fmt.Errorf("lock removed member Open Work: %w", err)
	}
	if len(workIDs) == 0 && command.SuccessorUserID != "" {
		return space.ErrRemovalSuccessorUnexpected
	}
	if len(workIDs) > 0 && command.SuccessorUserID == "" {
		return space.ErrRemovalSuccessorRequired
	}
	if len(workIDs) > 0 {
		updated, err := q.TransferRemovedMemberOpenWorks(ctx, dbsqlc.TransferRemovedMemberOpenWorksParams{SuccessorUserID: command.SuccessorUserID, SpaceID: command.SpaceID, TargetUserID: command.TargetUserID})
		if err != nil {
			return fmt.Errorf("transfer removed member Open Work: %w", err)
		}
		if updated != int64(len(workIDs)) {
			return fmt.Errorf("transfer removed member Open Work: locked %d, updated %d", len(workIDs), updated)
		}
	}
	successorUUID := pgtype.UUID{}
	if command.SuccessorUserID != "" {
		successorUUID, _ = postgresUUID(command.SuccessorUserID)
	}
	updated, err := q.RevokeSpaceMembership(ctx, dbsqlc.RevokeSpaceMembershipParams{
		ActorUserID: actorUUID, SuccessorUserID: successorUUID, IdempotencyKey: &key,
		RequestDigest: command.RequestDigest[:], SpaceID: command.SpaceID, TargetUserID: command.TargetUserID,
	})
	if err != nil {
		return fmt.Errorf("revoke Space Membership: %w", err)
	}
	if updated != 1 {
		return space.ErrMemberUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Space member removal: %w", err)
	}
	return nil
}
