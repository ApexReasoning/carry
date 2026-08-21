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
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ space.InvitationPersistence = (*Store)(nil)

func (s *Store) PrepareInvitation(ctx context.Context, command space.PrepareInvitationCommand) (space.IssuedInvitation, error) {
	if !validInvitationPreparation(command) {
		return space.IssuedInvitation{}, space.ErrInvalidInvitation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("begin Space invitation issue: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	authority, err := q.LockInvitationActorMembership(ctx, dbsqlc.LockInvitationActorMembershipParams{SpaceID: command.SpaceID, UserID: command.ActorUserID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !authority.CanManageMembers) {
		return space.IssuedInvitation{}, space.ErrForbidden
	}
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("lock invitation manager: %w", err)
	}
	replay, err := q.LoadInvitationIssueReplay(ctx, dbsqlc.LoadInvitationIssueReplayParams{
		SpaceID: command.SpaceID, UserID: command.ActorUserID, IdempotencyKey: command.IdempotencyKey,
	})
	if err == nil {
		if !bytes.Equal(replay.IssueRequestDigest, command.RequestDigest[:]) || replay.RecipientEmail != command.RecipientEmail || replay.CanManageMembers != command.CanManageMembers || replay.CanEnrollMachines != command.CanEnrollMachines {
			return space.IssuedInvitation{}, space.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return space.IssuedInvitation{}, fmt.Errorf("commit invitation issue replay: %w", err)
		}
		return restoreIssuedInvitation(replay.InvitationID, replay.SpaceID, replay.RecipientEmail, replay.CanManageMembers, replay.CanEnrollMachines, replay.CreatedAt.Time, replay.ExpiresAt.Time, replay.SubmissionID, replay.SubmissionRecipient, replay.PayloadDigest, replay.ProviderIdempotencyKey, replay.State, replay.ProviderMessageID, replay.SubmissionCreatedAt.Time, replay.SubmitEligible), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return space.IssuedInvitation{}, fmt.Errorf("load invitation issue replay: %w", err)
	}
	if command.CanManageMembers && !authority.CanManageMembers || command.CanEnrollMachines && !authority.CanEnrollMachines {
		return space.IssuedInvitation{}, space.ErrForbidden
	}
	if err := q.LockSpaceInvitationRecipientKey(ctx, dbsqlc.LockSpaceInvitationRecipientKeyParams{SpaceID: command.SpaceID, RecipientEmail: command.RecipientEmail}); err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("lock Space invitation recipient: %w", err)
	}
	current, err := q.HasCurrentSpaceInvitation(ctx, dbsqlc.HasCurrentSpaceInvitationParams{SpaceID: command.SpaceID, RecipientEmail: command.RecipientEmail})
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("check current invitation: %w", err)
	}
	if current {
		return space.IssuedInvitation{}, space.ErrInvitationConflict
	}
	alreadyMember, err := q.EmailOwnerIsActiveSpaceMember(ctx, dbsqlc.EmailOwnerIsActiveSpaceMemberParams{RecipientEmail: command.RecipientEmail, SpaceID: command.SpaceID})
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("check invited active member: %w", err)
	}
	if alreadyMember {
		return space.IssuedInvitation{}, space.ErrInvitationAlreadyMember
	}
	created, err := q.InsertSpaceInvitation(ctx, dbsqlc.InsertSpaceInvitationParams{
		InvitationID: command.InvitationID, SpaceID: command.SpaceID, RecipientEmail: command.RecipientEmail,
		UserID: command.ActorUserID, CanManageMembers: command.CanManageMembers, CanEnrollMachines: command.CanEnrollMachines,
		IdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:],
	})
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("insert Space invitation: %w", err)
	}
	submission, err := q.InsertSpaceInvitationSubmission(ctx, dbsqlc.InsertSpaceInvitationSubmissionParams{
		SubmissionID: command.SubmissionID, InvitationID: command.InvitationID, UserID: command.ActorUserID,
		IdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:], RecipientEmail: command.RecipientEmail,
		PayloadDigest: command.PayloadDigest[:], ProviderIdempotencyKey: command.ProviderIdempotencyKey,
	})
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("insert initial invitation submission: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("commit Space invitation issue: %w", err)
	}
	return restoreIssuedInvitation(command.InvitationID, command.SpaceID, command.RecipientEmail, command.CanManageMembers, command.CanEnrollMachines, created.CreatedAt.Time, created.ExpiresAt.Time, submission.SubmissionID, submission.RecipientEmail, submission.PayloadDigest, submission.ProviderIdempotencyKey, submission.State, submission.ProviderMessageID, submission.CreatedAt.Time, submission.SubmitEligible), nil
}

func (s *Store) InvitationRecipient(ctx context.Context, spaceID, invitationID, actorUserID string) (string, error) {
	if uuid.Validate(spaceID) != nil || uuid.Validate(invitationID) != nil || uuid.Validate(actorUserID) != nil {
		return "", space.ErrInvalidInvitation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin invitation recipient lookup: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	authority, err := q.LockInvitationActorMembership(ctx, dbsqlc.LockInvitationActorMembershipParams{SpaceID: spaceID, UserID: actorUserID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !authority.CanManageMembers) {
		return "", space.ErrForbidden
	}
	if err != nil {
		return "", fmt.Errorf("lock resend manager: %w", err)
	}
	invitation, err := q.LoadSpaceInvitationForUpdate(ctx, invitationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && invitation.SpaceID != spaceID) {
		return "", space.ErrInvitationUnavailable
	}
	if err != nil {
		return "", fmt.Errorf("load invitation recipient: %w", err)
	}
	now, err := q.InvitationDatabaseTime(ctx)
	if err != nil {
		return "", fmt.Errorf("load invitation recipient time: %w", err)
	}
	if invitation.AcceptedAt.Valid {
		return "", space.ErrInvitationAccepted
	}
	if invitation.RevokedAt.Valid {
		return "", space.ErrInvitationRevoked
	}
	if !invitation.ExpiresAt.Time.After(now.Time) {
		return "", space.ErrInvitationExpired
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit invitation recipient lookup: %w", err)
	}
	return invitation.RecipientEmail, nil
}

func (s *Store) PrepareInvitationResend(ctx context.Context, command space.PrepareInvitationResendCommand) (space.IssuedInvitation, error) {
	if uuid.Validate(command.SpaceID) != nil || uuid.Validate(command.InvitationID) != nil || uuid.Validate(command.ActorUserID) != nil || uuid.Validate(command.SubmissionID) != nil || !validInvitationKey(command.IdempotencyKey) || !validInvitationKey(command.ProviderIdempotencyKey) {
		return space.IssuedInvitation{}, space.ErrInvalidInvitation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("begin invitation resend: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	authority, err := q.LockInvitationActorMembership(ctx, dbsqlc.LockInvitationActorMembershipParams{SpaceID: command.SpaceID, UserID: command.ActorUserID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !authority.CanManageMembers) {
		return space.IssuedInvitation{}, space.ErrForbidden
	}
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("lock resend manager: %w", err)
	}
	replay, err := q.LoadInvitationResendReplay(ctx, dbsqlc.LoadInvitationResendReplayParams{InvitationID: command.InvitationID, SpaceID: command.SpaceID, UserID: command.ActorUserID, IdempotencyKey: command.IdempotencyKey})
	if err == nil {
		if !bytes.Equal(replay.RequestDigest, command.RequestDigest[:]) {
			return space.IssuedInvitation{}, space.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return space.IssuedInvitation{}, fmt.Errorf("commit invitation resend replay: %w", err)
		}
		return restoreIssuedInvitation(replay.InvitationID, replay.SpaceID, replay.RecipientEmail, replay.CanManageMembers, replay.CanEnrollMachines, replay.CreatedAt.Time, replay.ExpiresAt.Time, replay.SubmissionID, replay.SubmissionRecipient, replay.PayloadDigest, replay.ProviderIdempotencyKey, replay.State, replay.ProviderMessageID, replay.SubmissionCreatedAt.Time, replay.SubmitEligible), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return space.IssuedInvitation{}, fmt.Errorf("load invitation resend replay: %w", err)
	}
	invitation, err := q.LoadSpaceInvitationForUpdate(ctx, command.InvitationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && invitation.SpaceID != command.SpaceID) {
		return space.IssuedInvitation{}, space.ErrInvitationUnavailable
	}
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("lock invitation for resend: %w", err)
	}
	now, err := q.InvitationDatabaseTime(ctx)
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("load resend database time: %w", err)
	}
	if invitation.AcceptedAt.Valid {
		return space.IssuedInvitation{}, space.ErrInvitationAccepted
	}
	if invitation.RevokedAt.Valid {
		return space.IssuedInvitation{}, space.ErrInvitationRevoked
	}
	if !invitation.ExpiresAt.Time.After(now.Time) {
		return space.IssuedInvitation{}, space.ErrInvitationExpired
	}
	latest, err := q.LoadLatestInvitationSubmissionTime(ctx, command.InvitationID)
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("load latest invitation submission: %w", err)
	}
	if latest.Time.Add(space.InvitationResendCooldown).After(now.Time) {
		return space.IssuedInvitation{}, space.ErrInvitationResendCooldown
	}
	submission, err := q.InsertSpaceInvitationSubmission(ctx, dbsqlc.InsertSpaceInvitationSubmissionParams{
		SubmissionID: command.SubmissionID, InvitationID: command.InvitationID, UserID: command.ActorUserID,
		IdempotencyKey: command.IdempotencyKey, RequestDigest: command.RequestDigest[:], RecipientEmail: invitation.RecipientEmail,
		PayloadDigest: command.PayloadDigest[:], ProviderIdempotencyKey: command.ProviderIdempotencyKey,
	})
	if err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("insert invitation resend submission: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return space.IssuedInvitation{}, fmt.Errorf("commit invitation resend: %w", err)
	}
	return restoreIssuedInvitation(invitation.InvitationID, invitation.SpaceID, invitation.RecipientEmail, invitation.CanManageMembers, invitation.CanEnrollMachines, invitation.CreatedAt.Time, invitation.ExpiresAt.Time, submission.SubmissionID, submission.RecipientEmail, submission.PayloadDigest, submission.ProviderIdempotencyKey, submission.State, submission.ProviderMessageID, submission.CreatedAt.Time, submission.SubmitEligible), nil
}

func (s *Store) RecordInvitationSubmission(ctx context.Context, command space.RecordInvitationSubmissionCommand) (space.InvitationSubmission, error) {
	if uuid.Validate(command.SubmissionID) != nil || !validInvitationSubmissionState(command.State, command.ProviderMessageID) {
		return space.InvitationSubmission{}, space.ErrInvitationSubmissionConflict
	}
	var providerID *string
	if command.ProviderMessageID != "" {
		providerID = &command.ProviderMessageID
	}
	row, err := s.queries.RecordSpaceInvitationSubmission(ctx, dbsqlc.RecordSpaceInvitationSubmissionParams{
		State: string(command.State), ProviderMessageID: providerID, SubmissionID: command.SubmissionID, PayloadDigest: command.PayloadDigest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return space.InvitationSubmission{}, space.ErrInvitationSubmissionConflict
	}
	if err != nil {
		return space.InvitationSubmission{}, fmt.Errorf("record invitation submission: %w", err)
	}
	return restoreSubmission(row.SubmissionID, row.RecipientEmail, row.PayloadDigest, row.ProviderIdempotencyKey, row.State, row.ProviderMessageID, row.CreatedAt.Time), nil
}

func (s *Store) ListSpaceInvitations(ctx context.Context, userID, spaceID string) ([]space.ManagedInvitation, error) {
	if uuid.Validate(userID) != nil || uuid.Validate(spaceID) != nil {
		return nil, space.ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin managed invitation list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	authority, err := q.LockInvitationActorMembership(ctx, dbsqlc.LockInvitationActorMembershipParams{SpaceID: spaceID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !authority.CanManageMembers) {
		return nil, space.ErrForbidden
	}
	if err != nil {
		return nil, fmt.Errorf("lock invitation list authority: %w", err)
	}
	rows, err := q.ListManagedSpaceInvitations(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("list managed invitations: %w", err)
	}
	result := make([]space.ManagedInvitation, 0, len(rows))
	for _, row := range rows {
		inviterDisplayName := row.InviterDisplayName
		result = append(result, restoreManagedInvitation(row, inviterDisplayName))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit managed invitation list: %w", err)
	}
	return result, nil
}

func (s *Store) LoadInvitationForUser(ctx context.Context, invitationID, userID, sessionID string) (space.RecipientInvitation, error) {
	if uuid.Validate(invitationID) != nil {
		return space.RecipientInvitation{}, space.ErrInvitationUnavailable
	}
	if uuid.Validate(userID) != nil {
		return space.RecipientInvitation{}, space.ErrInvitationUnavailable
	}
	if uuid.Validate(sessionID) != nil {
		return space.RecipientInvitation{}, space.ErrInvitationUnavailable
	}
	row, err := s.queries.LoadInvitationForUser(ctx, dbsqlc.LoadInvitationForUserParams{
		InvitationID: invitationID,
		UserID:       userID,
		SessionID:    sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return space.RecipientInvitation{}, space.ErrInvitationUnavailable
	}
	if err != nil {
		return space.RecipientInvitation{}, fmt.Errorf("load targeted invitation: %w", err)
	}
	if row.SessionRevokedAt.Valid || !row.SessionExpiresAt.Time.After(row.ObservedAt.Time) {
		return space.RecipientInvitation{}, identity.ErrUnauthenticated
	}
	state := space.InvitationPending
	if row.AcceptedAt.Valid {
		state = space.InvitationAccepted
	} else if row.RevokedAt.Valid {
		state = space.InvitationRevoked
	} else if !row.ExpiresAt.Time.After(row.ObservedAt.Time) {
		state = space.InvitationExpired
	}
	acceptResult := ""
	if row.AcceptResult != nil {
		acceptResult = *row.AcceptResult
	}
	currentMember := row.CurrentMember != nil && *row.CurrentMember
	return space.RecipientInvitation{
		InvitationID:             row.InvitationID,
		SpaceID:                  row.SpaceID,
		SpaceName:                row.SpaceName,
		InviterDisplayName:       row.InviterDisplayName,
		CanManageMembers:         row.CanManageMembers,
		CanEnrollMachines:        row.CanEnrollMachines,
		CreatedAt:                row.CreatedAt.Time,
		ExpiresAt:                row.ExpiresAt.Time,
		State:                    state,
		AcceptResult:             acceptResult,
		CurrentMember:            currentMember,
		ReauthenticationRequired: state == space.InvitationPending && (row.IdentityProofMethod != string(identity.EmailMethod) || !row.IdentityProvedAt.Time.Add(identity.IdentityProofLifetime).After(row.ObservedAt.Time)),
	}, nil
}

func (s *Store) ListUserInvitations(ctx context.Context, userID, sessionID string) (space.InvitationInbox, error) {
	if uuid.Validate(userID) != nil || uuid.Validate(sessionID) != nil {
		return space.InvitationInbox{}, identity.ErrUnauthenticated
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return space.InvitationInbox{}, fmt.Errorf("begin invitation inbox: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	session, err := q.LoadInvitationInboxSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return space.InvitationInbox{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return space.InvitationInbox{}, fmt.Errorf("load invitation inbox Session: %w", err)
	}
	now, err := q.IdentityMethodDatabaseTime(ctx)
	if err != nil {
		return space.InvitationInbox{}, fmt.Errorf("load invitation inbox time: %w", err)
	}
	if session.UserID != userID || session.RevokedAt.Valid || !session.ExpiresAt.Time.After(now.Time) {
		return space.InvitationInbox{}, identity.ErrUnauthenticated
	}
	rows, err := q.ListInvitationsForEmailOwner(ctx, userID)
	if err != nil {
		return space.InvitationInbox{}, fmt.Errorf("list invitation inbox: %w", err)
	}
	result := make([]space.RecipientInvitation, 0, len(rows))
	for _, row := range rows {
		inviterDisplayName := row.InviterDisplayName
		result = append(result, space.RecipientInvitation{
			InvitationID: row.InvitationID, SpaceID: row.SpaceID, SpaceName: row.SpaceName, InviterDisplayName: inviterDisplayName,
			CanManageMembers: row.CanManageMembers, CanEnrollMachines: row.CanEnrollMachines,
			CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return space.InvitationInbox{}, fmt.Errorf("commit invitation inbox: %w", err)
	}
	return space.InvitationInbox{Invitations: result, ReauthenticationRequired: session.IdentityProofMethod != string(identity.EmailMethod) || !session.IdentityProvedAt.Time.Add(identity.IdentityProofLifetime).After(now.Time)}, nil
}

func (s *Store) RevokeInvitation(ctx context.Context, command space.RevokeInvitationCommand) error {
	if uuid.Validate(command.SpaceID) != nil {
		return space.ErrInvalidInvitation
	}
	if uuid.Validate(command.InvitationID) != nil {
		return space.ErrInvalidInvitation
	}
	if uuid.Validate(command.ActorUserID) != nil {
		return space.ErrInvalidInvitation
	}
	if !validInvitationKey(command.IdempotencyKey) {
		return space.ErrInvalidInvitation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin invitation revoke: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	// Lock manager authority before revealing any invitation terminal state.
	authority, err := q.LockInvitationActorMembership(ctx, dbsqlc.LockInvitationActorMembershipParams{SpaceID: command.SpaceID, UserID: command.ActorUserID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !authority.CanManageMembers) {
		return space.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("lock revoke manager: %w", err)
	}
	// Lock the Space-scoped terminal fact before taking the authorizing clock.
	invitation, err := q.LoadSpaceInvitationForUpdate(ctx, command.InvitationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && invitation.SpaceID != command.SpaceID) {
		return space.ErrInvitationUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock invitation for revoke: %w", err)
	}
	if invitation.RevokedAt.Valid {
		if !invitation.RevokedByUserID.Valid {
			return space.ErrInvitationRevoked
		}
		if uuidValue(invitation.RevokedByUserID) != command.ActorUserID {
			return space.ErrInvitationRevoked
		}
		if invitation.RevokeIdempotencyKey == nil {
			return space.ErrInvitationRevoked
		}
		if *invitation.RevokeIdempotencyKey != command.IdempotencyKey {
			return space.ErrInvitationRevoked
		}
		if !bytes.Equal(invitation.RevokeRequestDigest, command.RequestDigest[:]) {
			return space.ErrInvitationRevoked
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit invitation revoke replay: %w", err)
		}
		return nil
	}
	if invitation.AcceptedAt.Valid {
		return space.ErrInvitationAccepted
	}
	// No later statement may wait before this single database clock is consumed.
	authorizationTime, err := q.InvitationDatabaseTime(ctx)
	if err != nil {
		return fmt.Errorf("load invitation revoke time: %w", err)
	}
	if !invitation.ExpiresAt.Time.After(authorizationTime.Time) {
		return space.ErrInvitationExpired
	}
	userUUID, _ := postgresUUID(command.ActorUserID)
	key := command.IdempotencyKey
	rows, err := q.RevokeSpaceInvitation(ctx, dbsqlc.RevokeSpaceInvitationParams{
		UserID:         userUUID,
		Now:            authorizationTime,
		IdempotencyKey: &key,
		RequestDigest:  command.RequestDigest[:],
		InvitationID:   command.InvitationID,
	})
	if err != nil {
		return fmt.Errorf("revoke Space invitation: %w", err)
	}
	if rows != 1 {
		return space.ErrInvitationUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit invitation revoke: %w", err)
	}
	return nil
}

func (s *Store) AcceptInvitation(ctx context.Context, command space.AcceptInvitationCommand) (space.AcceptedInvitation, error) {
	if uuid.Validate(command.InvitationID) != nil {
		return space.AcceptedInvitation{}, space.ErrInvalidInvitation
	}
	if uuid.Validate(command.UserID) != nil {
		return space.AcceptedInvitation{}, space.ErrInvalidInvitation
	}
	if uuid.Validate(command.SessionID) != nil {
		return space.AcceptedInvitation{}, space.ErrInvalidInvitation
	}
	if !validInvitationKey(command.IdempotencyKey) {
		return space.AcceptedInvitation{}, space.ErrInvalidInvitation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	// Observe the immutable recipient only to route the ordered authority locks.
	observed, err := q.LoadSpaceInvitation(ctx, command.InvitationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
	}
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("load invitation acceptance target: %w", err)
	}
	// Lock and validate time-free session and exact Email-owner authority first.
	if err := q.LockEmailLogin(ctx, observed.RecipientEmail); err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("lock invited Email identity: %w", err)
	}
	_, err = q.LockInvitationUser(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return space.AcceptedInvitation{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("lock invited User: %w", err)
	}
	session, err := q.LoadInvitationInboxSession(ctx, command.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return space.AcceptedInvitation{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("lock invitation Browser Session: %w", err)
	}
	if session.UserID != command.UserID {
		return space.AcceptedInvitation{}, identity.ErrUnauthenticated
	}
	if session.RevokedAt.Valid {
		return space.AcceptedInvitation{}, identity.ErrUnauthenticated
	}
	owner, err := q.LoadInvitationEmailOwner(ctx, observed.RecipientEmail)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != command.UserID) {
		return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
	}
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("load invited Email owner: %w", err)
	}
	if session.IdentityProofMethod != string(identity.EmailMethod) {
		return space.AcceptedInvitation{}, space.ErrInvitationProofRequired
	}
	// Lock invitation identity and terminal truth only after exact owner authority.
	invitation, err := q.LoadSpaceInvitationForUpdate(ctx, command.InvitationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && invitation.RecipientEmail != observed.RecipientEmail) {
		return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
	}
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("lock invitation acceptance: %w", err)
	}
	spaceName, err := q.LoadInvitationSpaceName(ctx, invitation.SpaceID)
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("load invitation Space: %w", err)
	}
	replay := false
	if invitation.AcceptedAt.Valid {
		if !invitation.AcceptedByUserID.Valid || uuidValue(invitation.AcceptedByUserID) != command.UserID {
			return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
		}
		if invitation.AcceptIdempotencyKey == nil || *invitation.AcceptIdempotencyKey != command.IdempotencyKey {
			return space.AcceptedInvitation{}, space.ErrInvitationAccepted
		}
		if !bytes.Equal(invitation.AcceptRequestDigest, command.RequestDigest[:]) {
			return space.AcceptedInvitation{}, space.ErrInvitationAccepted
		}
		replay = true
	}
	if invitation.RevokedAt.Valid {
		return space.AcceptedInvitation{}, space.ErrInvitationRevoked
	}
	// The Membership lock or insert is the final operation allowed to wait.
	membership, err := q.LoadMembershipForInvitation(ctx, dbsqlc.LoadMembershipForInvitationParams{SpaceID: invitation.SpaceID, UserID: command.UserID})
	membershipActive := err == nil && !membership.RevokedAt.Valid
	alreadyMember := membershipActive
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return space.AcceptedInvitation{}, fmt.Errorf("load invitation Membership: %w", err)
	}
	if err == nil && membership.RevokedAt.Valid {
		return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
	}
	result := "joined"
	canManage, canEnroll := invitation.CanManageMembers, invitation.CanEnrollMachines
	if replay {
		alreadyMember = invitation.AcceptResult != nil && *invitation.AcceptResult == "already_member"
		if invitation.AcceptResult != nil {
			result = *invitation.AcceptResult
		}
	}
	if membershipActive {
		canManage, canEnroll = membership.CanManageMembers, membership.CanEnrollMachines
		if !replay {
			result, alreadyMember = "already_member", true
		}
	} else if replay {
		return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
	} else if err := q.CreateInvitationMembership(ctx, dbsqlc.CreateInvitationMembershipParams{
		SpaceID: invitation.SpaceID, UserID: command.UserID,
		CanManageMembers: invitation.CanManageMembers, CanEnrollMachines: invitation.CanEnrollMachines,
	}); err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("create invitation Membership: %w", err)
	}
	// One post-wait database value owns every time predicate and terminal stamp.
	authorizationTime, err := q.InvitationDatabaseTime(ctx)
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("load acceptance database time: %w", err)
	}
	if !session.ExpiresAt.Time.After(authorizationTime.Time) {
		return space.AcceptedInvitation{}, identity.ErrUnauthenticated
	}
	if !session.IdentityProvedAt.Time.Add(identity.IdentityProofLifetime).After(authorizationTime.Time) {
		return space.AcceptedInvitation{}, space.ErrInvitationProofRequired
	}
	if !invitation.ExpiresAt.Time.After(authorizationTime.Time) {
		return space.AcceptedInvitation{}, space.ErrInvitationExpired
	}
	if replay {
		if err := tx.Commit(ctx); err != nil {
			return space.AcceptedInvitation{}, fmt.Errorf("commit invitation acceptance replay: %w", err)
		}
		return acceptedInvitation(invitation, spaceName, canManage, canEnroll, alreadyMember), nil
	}
	userUUID, _ := postgresUUID(command.UserID)
	key := command.IdempotencyKey
	rows, err := q.AcceptSpaceInvitation(ctx, dbsqlc.AcceptSpaceInvitationParams{
		UserID:         userUUID,
		Now:            authorizationTime,
		AcceptResult:   &result,
		IdempotencyKey: &key,
		RequestDigest:  command.RequestDigest[:],
		InvitationID:   command.InvitationID,
	})
	if err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("accept Space invitation: %w", err)
	}
	if rows != 1 {
		return space.AcceptedInvitation{}, space.ErrInvitationUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return space.AcceptedInvitation{}, fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return acceptedInvitation(invitation, spaceName, canManage, canEnroll, alreadyMember), nil
}

func validInvitationPreparation(command space.PrepareInvitationCommand) bool {
	return uuid.Validate(command.InvitationID) == nil && uuid.Validate(command.SubmissionID) == nil && uuid.Validate(command.SpaceID) == nil && uuid.Validate(command.ActorUserID) == nil && validInvitationKey(command.IdempotencyKey) && validInvitationKey(command.ProviderIdempotencyKey) && command.ExpiresIn == space.InvitationLifetime
}
func validInvitationKey(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 255
}
func validInvitationSubmissionState(state space.InvitationSubmissionState, providerID string) bool {
	switch state {
	case space.InvitationSubmissionAccepted:
		return strings.TrimSpace(providerID) != "" && len(providerID) <= 255
	case space.InvitationSubmissionRejected, space.InvitationSubmissionUnknown:
		return providerID == ""
	default:
		return false
	}
}
func restoreSubmission(id, recipient string, payload []byte, providerKey, state string, providerID *string, created time.Time) space.InvitationSubmission {
	var digest [32]byte
	copy(digest[:], payload)
	messageID := ""
	if providerID != nil {
		messageID = *providerID
	}
	return space.InvitationSubmission{SubmissionID: id, Recipient: recipient, PayloadDigest: digest, ProviderIdempotencyKey: providerKey, State: space.InvitationSubmissionState(state), ProviderMessageID: messageID, CreatedAt: created}
}
func restoreIssuedInvitation(invitationID, spaceID, recipient string, canManage, canEnroll bool, createdAt, expiresAt time.Time, submissionID, submissionRecipient string, payload []byte, providerKey, state string, providerID *string, submissionCreatedAt time.Time, submitEligible *bool) space.IssuedInvitation {
	result := space.IssuedInvitation{InvitationID: invitationID, SpaceID: spaceID, RecipientEmail: recipient, CanManageMembers: canManage, CanEnrollMachines: canEnroll, CreatedAt: createdAt, ExpiresAt: expiresAt, Submission: restoreSubmission(submissionID, submissionRecipient, payload, providerKey, state, providerID, submissionCreatedAt)}
	result.Submission.SubmitEligible = submitEligible != nil && *submitEligible
	return result
}
func restoreManagedInvitation(row dbsqlc.ListManagedSpaceInvitationsRow, inviter string) space.ManagedInvitation {
	result := restoreIssuedInvitation(row.InvitationID, row.SpaceID, row.RecipientEmail, row.CanManageMembers, row.CanEnrollMachines, row.CreatedAt.Time, row.ExpiresAt.Time, row.SubmissionID, row.SubmissionRecipient, row.PayloadDigest, row.ProviderIdempotencyKey, row.State, row.ProviderMessageID, row.SubmissionCreatedAt.Time, nil)
	result.InviterDisplayName = inviter
	return result
}
func acceptedInvitation(row dbsqlc.LoadSpaceInvitationForUpdateRow, spaceName string, canManage, canEnroll, already bool) space.AcceptedInvitation {
	return space.AcceptedInvitation{InvitationID: row.InvitationID, SpaceID: row.SpaceID, SpaceName: spaceName, CanManageMembers: canManage, CanEnrollMachines: canEnroll, AlreadyMember: already}
}
