package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) BeginCLILogin(ctx context.Context, command identity.BeginCLILoginCommand) (identity.CLILoginRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("begin CLI login request: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	if err := queries.LockCLILoginAdmission(ctx); err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("lock CLI login admission: %w", err)
	}
	existing, err := queries.LoadCLILoginForBegin(ctx, command.RequestID)
	if err == nil {
		if existing.BeginIdempotencyKey != command.IdempotencyKey || !bytes.Equal(existing.BeginRequestDigest, command.RequestDigest[:]) {
			return identity.CLILoginRequest{}, identity.ErrCLILoginConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return identity.CLILoginRequest{}, fmt.Errorf("commit CLI login replay: %w", err)
		}
		return cliLoginRequest(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.CLILoginRequest{}, fmt.Errorf("load CLI login replay: %w", err)
	}
	perSource, err := queries.CountLiveCLILoginsForSource(ctx, command.SourceDigest[:])
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("count source CLI logins: %w", err)
	}
	global, err := queries.CountLiveCLILogins(ctx)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("count live CLI logins: %w", err)
	}
	if perSource >= 5 || global >= 1000 {
		return identity.CLILoginRequest{}, identity.ErrCLILoginRateLimited
	}
	replacement, err := nullablePostgresUUID(command.ProposedReplacementCredentialID)
	if err != nil {
		return identity.CLILoginRequest{}, identity.ErrInvalidCLILogin
	}
	created, err := queries.CreateCLILogin(ctx, dbsqlc.CreateCLILoginParams{
		RequestID: command.RequestID, BeginIdempotencyKey: command.IdempotencyKey,
		BeginRequestDigest: command.RequestDigest[:], UserCodeDigest: command.UserCodeDigest[:],
		CodeGeneration: command.CodeGeneration, SourceDigest: command.SourceDigest[:], Label: command.Label,
		ProposedReplacementCredentialID: replacement,
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			switch postgresError.ConstraintName {
			case "cli_login_requests_user_code_digest_key":
				return identity.CLILoginRequest{}, identity.CLIUserCodeCollision()
			case "cli_login_requests_begin_idempotency_key_key":
				return identity.CLILoginRequest{}, identity.ErrCLILoginConflict
			}
		}
		return identity.CLILoginRequest{}, fmt.Errorf("create CLI login: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("commit CLI login: %w", err)
	}
	return cliLoginRequest(created), nil
}

func (s *Store) LookupCLILogin(ctx context.Context, command identity.LookupCLILoginCommand) (identity.CLILoginRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("begin CLI login lookup: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	actor, err := queries.LockBrowserSessionForCLILogin(ctx, command.BrowserSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.CLILoginRequest{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("lock CLI lookup Browser Session: %w", err)
	}
	sourceDigest := command.SourceDigest[:]
	locks := []string{"session:" + command.BrowserSessionID, "source:" + fmt.Sprintf("%x", sourceDigest)}
	sort.Strings(locks)
	for _, lock := range locks {
		if err := queries.LockCLILoginLookupBudget(ctx, lock); err != nil {
			return identity.CLILoginRequest{}, fmt.Errorf("lock CLI lookup budget: %w", err)
		}
	}
	sessionFailures, err := queries.CountRecentCLILookupFailuresForSession(ctx, command.BrowserSessionID)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("count CLI lookup session failures: %w", err)
	}
	sourceFailures, err := queries.CountRecentCLILookupFailuresForSource(ctx, sourceDigest)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("count CLI lookup source failures: %w", err)
	}
	if sessionFailures >= 5 || sourceFailures >= 20 {
		return identity.CLILoginRequest{}, identity.ErrCLILoginRateLimited
	}
	found, err := queries.FindLiveCLILoginByCode(ctx, command.UserCodeDigest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		if recordErr := queries.RecordCLILoginLookupFailure(ctx, dbsqlc.RecordCLILoginLookupFailureParams{
			FailureID: uuid.NewString(), BrowserSessionID: command.BrowserSessionID, SourceDigest: sourceDigest,
		}); recordErr != nil {
			return identity.CLILoginRequest{}, fmt.Errorf("record CLI lookup failure: %w", recordErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return identity.CLILoginRequest{}, fmt.Errorf("commit CLI lookup failure: %w", commitErr)
		}
		return identity.CLILoginRequest{}, identity.ErrCLILoginUnavailable
	}
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("find CLI login by code: %w", err)
	}
	if found.ApprovedByUserID.Valid && uuidValue(found.ApprovedByUserID) != actor.UserID {
		return identity.CLILoginRequest{}, identity.ErrCLILoginUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("commit CLI login lookup: %w", err)
	}
	return cliLoginRequest(found), nil
}

func (s *Store) ApproveCLILogin(ctx context.Context, command identity.ApproveCLILoginCommand) (identity.CLILoginRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("begin CLI approval: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	request, err := queries.LockCLILoginRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !bytes.Equal(request.UserCodeDigest, command.UserCodeDigest[:])) {
		return identity.CLILoginRequest{}, identity.ErrCLILoginUnavailable
	}
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("lock CLI approval: %w", err)
	}
	actor, err := queries.LockBrowserSessionForCLILogin(ctx, command.BrowserSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.CLILoginRequest{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("lock CLI approval Browser Session: %w", err)
	}
	if request.ApprovedAt.Valid {
		if request.ApprovedByUserID.Valid && uuidValue(request.ApprovedByUserID) == actor.UserID &&
			request.ApprovalIdempotencyKey != nil && *request.ApprovalIdempotencyKey == command.IdempotencyKey &&
			bytes.Equal(request.ApprovalRequestDigest, command.RequestDigest[:]) {
			if err := tx.Commit(ctx); err != nil {
				return identity.CLILoginRequest{}, fmt.Errorf("commit CLI approval replay: %w", err)
			}
			return cliLoginRequest(request), nil
		}
		return identity.CLILoginRequest{}, identity.ErrCLILoginAlreadyApproved
	}
	databaseTime, err := queries.CLIDatabaseTime(ctx)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("load CLI approval database time: %w", err)
	}
	if err := liveCLILoginDecisionError(request, databaseTime.Time); err != nil {
		return identity.CLILoginRequest{}, err
	}
	if _, err := queries.LockSpaceForCLILogin(ctx, command.SpaceID); errors.Is(err, pgx.ErrNoRows) {
		return identity.CLILoginRequest{}, identity.ErrCLILoginUnavailable
	} else if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("lock CLI approval Space: %w", err)
	}
	if _, err := queries.LockActiveMembershipForCLILogin(ctx, dbsqlc.LockActiveMembershipForCLILoginParams{SpaceID: command.SpaceID, UserID: actor.UserID}); errors.Is(err, pgx.ErrNoRows) {
		return identity.CLILoginRequest{}, identity.ErrCLILoginUnavailable
	} else if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("lock CLI approval Membership: %w", err)
	}
	replacement, err := nullablePostgresUUID(command.ReplacementCredentialID)
	if err != nil {
		return identity.CLILoginRequest{}, identity.ErrCLIReplacementInvalid
	}
	if command.ReplacementCredentialID != "" {
		credential, lockErr := queries.LockCLICredential(ctx, command.ReplacementCredentialID)
		if lockErr != nil || credential.UserID != actor.UserID || credential.RevokedAt.Valid {
			return identity.CLILoginRequest{}, identity.ErrCLIReplacementInvalid
		}
		now, timeErr := queries.CLIDatabaseTime(ctx)
		if timeErr != nil || !credential.ExpiresAt.Time.After(now.Time) {
			return identity.CLILoginRequest{}, identity.ErrCLIReplacementInvalid
		}
	}
	actorUserID, err := postgresUUID(actor.UserID)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("parse CLI approval User identity: %w", err)
	}
	spaceID, err := postgresUUID(command.SpaceID)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("parse CLI approval Space identity: %w", err)
	}
	credentialID, err := postgresUUID(command.CredentialID)
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("parse prepared CLI credential identity: %w", err)
	}
	approved, err := queries.ApproveCLILogin(ctx, dbsqlc.ApproveCLILoginParams{
		UserID:                  actorUserID,
		SpaceID:                 spaceID,
		IdempotencyKey:          &command.IdempotencyKey,
		RequestDigest:           command.RequestDigest[:],
		CredentialID:            credentialID,
		ReplacementCredentialID: replacement,
		RequestID:               command.RequestID,
	})
	if err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("approve CLI login: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.CLILoginRequest{}, fmt.Errorf("commit CLI approval: %w", err)
	}
	return cliLoginRequest(approved), nil
}

func (s *Store) DenyCLILogin(ctx context.Context, command identity.DenyCLILoginCommand) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin CLI denial: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	request, err := queries.LockCLILoginRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !bytes.Equal(request.UserCodeDigest, command.UserCodeDigest[:])) {
		return identity.ErrCLILoginUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock CLI denial: %w", err)
	}
	actor, err := queries.LockBrowserSessionForCLILogin(ctx, command.BrowserSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("lock CLI denial Browser Session: %w", err)
	}
	if request.DeniedAt.Valid {
		if request.DeniedByUserID.Valid && uuidValue(request.DeniedByUserID) == actor.UserID && request.DenialIdempotencyKey != nil &&
			*request.DenialIdempotencyKey == command.IdempotencyKey && bytes.Equal(request.DenialRequestDigest, command.RequestDigest[:]) {
			return tx.Commit(ctx)
		}
		return identity.ErrCLILoginConflict
	}
	if request.ApprovedAt.Valid {
		return identity.ErrCLILoginAlreadyApproved
	}
	databaseTime, err := queries.CLIDatabaseTime(ctx)
	if err != nil {
		return fmt.Errorf("load CLI denial database time: %w", err)
	}
	if err := liveCLILoginDecisionError(request, databaseTime.Time); err != nil {
		return err
	}
	actorUserID, err := postgresUUID(actor.UserID)
	if err != nil {
		return fmt.Errorf("parse CLI denial User identity: %w", err)
	}
	if _, err := queries.DenyCLILogin(ctx, dbsqlc.DenyCLILoginParams{
		UserID:         actorUserID,
		IdempotencyKey: &command.IdempotencyKey,
		RequestDigest:  command.RequestDigest[:],
		RequestID:      command.RequestID,
	}); err != nil {
		return fmt.Errorf("deny CLI login: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit CLI denial: %w", err)
	}
	return nil
}

func (s *Store) PollCLILogin(ctx context.Context, command identity.PollCLILoginCommand) (identity.RedeemedCLICredential, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("begin CLI poll: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	request, err := queries.LockCLILoginRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.RedeemedCLICredential{}, identity.ErrCLILoginUnavailable
	}
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("lock CLI poll: %w", err)
	}
	nowValue, err := queries.CLIDatabaseTime(ctx)
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("load CLI poll database time: %w", err)
	}
	now := nowValue.Time
	if request.RedeemedAt.Valid {
		if !request.ReplayUntil.Valid || !request.ReplayUntil.Time.After(now) || !request.ResultingCredentialID.Valid {
			return identity.RedeemedCLICredential{}, identity.ErrCLICredentialUnavailable
		}
		credential, loadErr := queries.LoadCLICredential(ctx, uuidValue(request.ResultingCredentialID))
		if loadErr != nil || credential.RevokedAt.Valid || !credential.ExpiresAt.Time.After(now) {
			return identity.RedeemedCLICredential{}, identity.ErrCLICredentialUnavailable
		}
		if err := tx.Commit(ctx); err != nil {
			return identity.RedeemedCLICredential{}, fmt.Errorf("commit CLI redeem replay: %w", err)
		}
		return redeemedCredential(request, credential), nil
	}
	if request.DeniedAt.Valid {
		return identity.RedeemedCLICredential{}, identity.ErrCLILoginDenied
	}
	if request.CancelledAt.Valid {
		return identity.RedeemedCLICredential{}, identity.ErrCLILoginCancelled
	}
	if !request.ExpiresAt.Time.After(now) {
		return identity.RedeemedCLICredential{}, identity.ErrCLILoginExpired
	}
	interval := time.Duration(request.PollIntervalSeconds) * time.Second
	nextPollAt := request.CreatedAt.Time.Add(interval)
	if request.LastPolledAt.Valid {
		nextPollAt = request.LastPolledAt.Time.Add(interval)
	}
	if now.Before(nextPollAt) {
		updated, updateErr := queries.SlowDownCLILoginPoll(ctx, command.RequestID)
		if updateErr != nil {
			return identity.RedeemedCLICredential{}, fmt.Errorf("slow CLI polling: %w", updateErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return identity.RedeemedCLICredential{}, fmt.Errorf("commit CLI slow down: %w", err)
		}
		return identity.RedeemedCLICredential{}, identity.NewCLISlowDownError(time.Duration(updated.PollIntervalSeconds) * time.Second)
	}
	request, err = queries.RecordCLILoginPoll(ctx, command.RequestID)
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("record CLI poll: %w", err)
	}
	if !request.ApprovedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return identity.RedeemedCLICredential{}, fmt.Errorf("commit pending CLI poll: %w", err)
		}
		return identity.RedeemedCLICredential{}, identity.ErrCLILoginPending
	}
	userID := uuidValue(request.ApprovedByUserID)
	if request.ProposedReplacementCredentialID.Valid {
		replacementID := uuidValue(request.ProposedReplacementCredentialID)
		replacement, lockErr := queries.LockCLICredential(ctx, replacementID)
		if lockErr != nil || replacement.UserID != userID || replacement.RevokedAt.Valid || !replacement.ExpiresAt.Time.After(now) {
			return identity.RedeemedCLICredential{}, identity.ErrCLIReplacementInvalid
		}
		replacementUserID, parseErr := postgresUUID(userID)
		if parseErr != nil {
			return identity.RedeemedCLICredential{}, fmt.Errorf("parse replaced CLI credential User identity: %w", parseErr)
		}
		key := "replacement:" + request.RequestID
		digest := identityRequestDigest(replacementID, request.RequestID)
		if _, revokeErr := queries.RevokeCLICredential(ctx, dbsqlc.RevokeCLICredentialParams{
			UserID:         replacementUserID,
			IdempotencyKey: &key,
			RequestDigest:  digest[:],
			CredentialID:   replacementID,
		}); revokeErr != nil {
			return identity.RedeemedCLICredential{}, fmt.Errorf("revoke replaced CLI credential: %w", revokeErr)
		}
	}
	credentialID := uuidValue(request.PreparedCredentialID)
	credential, err := queries.CreateCLICredential(ctx, dbsqlc.CreateCLICredentialParams{
		CredentialID: credentialID, LoginRequestID: request.RequestID, UserID: userID, Label: request.Label,
	})
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("create CLI credential: %w", err)
	}
	redeemedCredentialID, err := postgresUUID(credentialID)
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("parse redeemed CLI credential identity: %w", err)
	}
	request, err = queries.RedeemCLILogin(ctx, dbsqlc.RedeemCLILoginParams{
		CredentialID: redeemedCredentialID,
		RequestID:    request.RequestID,
	})
	if err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("redeem CLI login: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.RedeemedCLICredential{}, fmt.Errorf("commit CLI redemption: %w", err)
	}
	return redeemedCredential(request, credential), nil
}

func (s *Store) CancelCLILogin(ctx context.Context, command identity.CancelCLILoginCommand) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin CLI cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	request, err := queries.LockCLILoginRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrCLILoginUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock CLI cancellation: %w", err)
	}
	if request.CancelledAt.Valid {
		return tx.Commit(ctx)
	}
	if request.RedeemedAt.Valid {
		return identity.ErrCLILoginRedeemed
	}
	if request.DeniedAt.Valid {
		return identity.ErrCLILoginDenied
	}
	now, err := queries.CLIDatabaseTime(ctx)
	if err != nil {
		return fmt.Errorf("load CLI cancellation database time: %w", err)
	}
	if !request.ExpiresAt.Time.After(now.Time) {
		return identity.ErrCLILoginExpired
	}
	if _, err := queries.CancelCLILogin(ctx, command.RequestID); err != nil {
		return fmt.Errorf("cancel CLI login: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit CLI cancellation: %w", err)
	}
	return nil
}

func (s *Store) ListCLICredentials(ctx context.Context, command identity.ListCLICredentialsCommand) ([]identity.CLICredential, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin CLI credential list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	actor, err := queries.LockBrowserSessionForCLILogin(ctx, command.BrowserSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("lock CLI credential list Browser Session: %w", err)
	}
	rows, err := queries.ListActiveCLICredentials(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list CLI credentials: %w", err)
	}
	result := make([]identity.CLICredential, 0, len(rows))
	for _, row := range rows {
		result = append(result, identity.CLICredential{
			CredentialID: row.CredentialID, Label: row.Label, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
			ApprovedSpaceID: uuidValue(row.ApprovedSpaceID), ApprovedSpaceName: row.ApprovedSpaceName,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit CLI credential list: %w", err)
	}
	return result, nil
}

func (s *Store) RevokeCLICredential(ctx context.Context, command identity.RevokeCLICredentialCommand) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin CLI credential revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	actorUserID := ""
	if !command.Self {
		actor, lockErr := queries.LockBrowserSessionForCLILogin(ctx, command.BrowserSessionID)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			return identity.ErrUnauthenticated
		}
		if lockErr != nil {
			return fmt.Errorf("lock CLI revocation Browser Session: %w", lockErr)
		}
		actorUserID = actor.UserID
	}
	credential, err := queries.LockCLICredential(ctx, command.CredentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrCLICredentialUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock CLI credential revocation: %w", err)
	}
	if command.Self {
		actorUserID = credential.UserID
	}
	if credential.UserID != actorUserID {
		return identity.ErrCLICredentialUnavailable
	}
	if credential.RevokedAt.Valid {
		// Possession of the exact MAC-bound credential is enough to reconcile
		// local logout after Browser revocation; it cannot repeat a consequence.
		if command.Self {
			return tx.Commit(ctx)
		}
		if credential.RevokedByUserID.Valid && uuidValue(credential.RevokedByUserID) == actorUserID && credential.RevocationIdempotencyKey != nil &&
			*credential.RevocationIdempotencyKey == command.IdempotencyKey && bytes.Equal(credential.RevocationRequestDigest, command.RequestDigest[:]) {
			return tx.Commit(ctx)
		}
		return identity.ErrCLILoginConflict
	}
	revokingUserID, err := postgresUUID(actorUserID)
	if err != nil {
		return fmt.Errorf("parse CLI revocation User identity: %w", err)
	}
	if _, err := queries.RevokeCLICredential(ctx, dbsqlc.RevokeCLICredentialParams{
		UserID:         revokingUserID,
		IdempotencyKey: &command.IdempotencyKey,
		RequestDigest:  command.RequestDigest[:],
		CredentialID:   command.CredentialID,
	}); err != nil {
		return fmt.Errorf("revoke CLI credential: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit CLI credential revocation: %w", err)
	}
	return nil
}

func (s *Store) AuthenticateCLICredential(ctx context.Context, credentialID string) (identity.AuthenticatedUser, error) {
	user, err := s.queries.AuthenticateCLICredential(ctx, credentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.AuthenticatedUser{}, identity.ErrUnauthenticated
	}
	if err != nil {
		return identity.AuthenticatedUser{}, fmt.Errorf("authenticate CLI credential: %w", err)
	}
	return identity.AuthenticatedUser{UserID: user.UserID, DisplayName: user.DisplayName}, nil
}

func cliLoginRequest(row dbsqlc.CliLoginRequest) identity.CLILoginRequest {
	result := identity.CLILoginRequest{
		RequestID: row.RequestID, Label: row.Label, CodeGeneration: row.CodeGeneration,
		ProposedReplacementCredentialID: uuidValue(row.ProposedReplacementCredentialID),
		CreatedAt:                       row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		PollInterval:     time.Duration(row.PollIntervalSeconds) * time.Second,
		ApprovedByUserID: uuidValue(row.ApprovedByUserID), ApprovedSpaceID: uuidValue(row.ApprovedSpaceID),
	}
	result.ApprovedAt = postgresTimePointer(row.ApprovedAt)
	result.DeniedAt = postgresTimePointer(row.DeniedAt)
	result.CancelledAt = postgresTimePointer(row.CancelledAt)
	result.RedeemedAt = postgresTimePointer(row.RedeemedAt)
	return result
}

func postgresTimePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func liveCLILoginDecisionError(request dbsqlc.CliLoginRequest, now time.Time) error {
	if request.DeniedAt.Valid {
		return identity.ErrCLILoginDenied
	}
	if request.CancelledAt.Valid {
		return identity.ErrCLILoginCancelled
	}
	if request.RedeemedAt.Valid {
		return identity.ErrCLILoginRedeemed
	}
	if !request.ExpiresAt.Time.After(now) {
		return identity.ErrCLILoginExpired
	}
	return nil
}

func redeemedCredential(request dbsqlc.CliLoginRequest, credential dbsqlc.CliCredential) identity.RedeemedCLICredential {
	return identity.RedeemedCLICredential{
		CredentialID: credential.CredentialID, UserID: credential.UserID,
		SpaceID: uuidValue(request.ApprovedSpaceID), Label: credential.Label, ExpiresAt: credential.ExpiresAt.Time,
	}
}

func identityRequestDigest(parts ...string) [sha256.Size]byte {
	encoded, _ := json.Marshal(parts)
	return sha256.Sum256(encoded)
}
