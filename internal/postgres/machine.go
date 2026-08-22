package postgres

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) BeginMachineConnection(ctx context.Context, command machine.BeginConnectionCommand) (machine.ConnectionRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("begin Machine connection admission: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	if err := queries.LockMachineConnectionAdmission(ctx); err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("lock Machine connection admission: %w", err)
	}
	existing, err := queries.LoadMachineConnectionForBegin(ctx, command.RequestID)
	if err == nil {
		if existing.BeginIdempotencyKey != command.IdempotencyKey || !bytes.Equal(existing.BeginRequestDigest, command.RequestDigest[:]) {
			return machine.ConnectionRequest{}, machine.ErrConnectionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("commit Machine connection replay: %w", err)
		}
		return machineConnectionRequest(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return machine.ConnectionRequest{}, fmt.Errorf("load Machine connection replay: %w", err)
	}
	perSource, err := queries.CountLiveMachineConnectionsForSource(ctx, command.SourceDigest[:])
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("count source Machine connections: %w", err)
	}
	global, err := queries.CountLiveMachineConnections(ctx)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("count live Machine connections: %w", err)
	}
	if perSource >= 5 || global >= 1000 {
		return machine.ConnectionRequest{}, machine.ErrConnectionRateLimited
	}
	created, err := queries.CreateMachineConnection(ctx, dbsqlc.CreateMachineConnectionParams{
		RequestID: command.RequestID, BeginIdempotencyKey: command.IdempotencyKey,
		BeginRequestDigest: command.RequestDigest[:], SourceDigest: command.SourceDigest[:],
		UserCodeDigest: command.CodeDigest[:], PollSecretDigest: command.PollDigest[:],
		DisplayName: command.DisplayName, PublicKeyDer: command.PublicKeyDER, KeyProof: command.KeyProof,
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			if postgresError.ConstraintName == "machine_connection_requests_user_code_digest_key" ||
				postgresError.ConstraintName == "machine_connection_requests_poll_secret_digest_key" {
				return machine.ConnectionRequest{}, machine.ErrConnectionRateLimited
			}
			return machine.ConnectionRequest{}, machine.ErrConnectionConflict
		}
		return machine.ConnectionRequest{}, fmt.Errorf("create Machine connection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("commit Machine connection: %w", err)
	}
	return machineConnectionRequest(created), nil
}

func (s *Store) LookupMachineConnection(ctx context.Context, command machine.LookupConnectionCommand) (machine.ConnectionRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("begin Machine connection lookup: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	if _, err := queries.LockBrowserSessionForMachineConnection(ctx, command.BrowserSessionID); errors.Is(err, pgx.ErrNoRows) {
		return machine.ConnectionRequest{}, machine.ErrMachineUnavailable
	} else if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("lock Browser Session for Machine lookup: %w", err)
	}
	lockKey := command.BrowserSessionID + ":" + fmt.Sprintf("%x", command.SourceDigest)
	if err := queries.LockMachineConnectionLookupBudget(ctx, lockKey); err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("lock Machine lookup budget: %w", err)
	}
	sessionFailures, err := queries.CountRecentMachineConnectionLookupFailuresForSession(ctx, command.BrowserSessionID)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("count Browser Machine lookup failures: %w", err)
	}
	sourceFailures, err := queries.CountRecentMachineConnectionLookupFailuresForSource(ctx, command.SourceDigest[:])
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("count source Machine lookup failures: %w", err)
	}
	if sessionFailures >= 10 || sourceFailures >= 50 {
		return machine.ConnectionRequest{}, machine.ErrConnectionRateLimited
	}
	found, err := queries.FindLiveMachineConnectionByCode(ctx, command.CodeDigest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		if recordErr := queries.RecordMachineConnectionLookupFailure(ctx, dbsqlc.RecordMachineConnectionLookupFailureParams{
			FailureID: uuid.NewString(), BrowserSessionID: command.BrowserSessionID, SourceDigest: command.SourceDigest[:],
		}); recordErr != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("record Machine lookup failure: %w", recordErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("commit Machine lookup failure: %w", err)
		}
		return machine.ConnectionRequest{}, machine.ErrConnectionUnavailable
	}
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("find Machine connection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("commit Machine lookup: %w", err)
	}
	return machineConnectionRequest(found), nil
}

func (s *Store) DecideMachineConnection(ctx context.Context, command machine.DecideConnectionCommand) (machine.ConnectionRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("begin Machine connection decision: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	session, err := queries.LockBrowserSessionForMachineConnection(ctx, command.BrowserSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.ConnectionRequest{}, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("lock Browser Session for Machine decision: %w", err)
	}
	userID := session.UserID
	var spaceID pgtype.UUID
	var preparedMachineID pgtype.UUID
	if command.Decision == "approved" {
		spaceID, err = postgresUUID(command.SpaceID)
		if err != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("parse approved Machine Space identity: %w", err)
		}
		preparedMachineID, err = postgresUUID(command.PreparedMachineID)
		if err != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("parse prepared Machine identity: %w", err)
		}
		if _, err := queries.LockSpaceForMachineConnection(ctx, command.SpaceID); errors.Is(err, pgx.ErrNoRows) {
			return machine.ConnectionRequest{}, machine.ErrMachineAuthority
		} else if err != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("lock Space for Machine decision: %w", err)
		}
		membership, err := queries.LockMachineEnrollmentMembership(ctx, dbsqlc.LockMachineEnrollmentMembershipParams{
			SpaceID: command.SpaceID, UserID: userID,
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !membership.CanEnrollMachines) {
			return machine.ConnectionRequest{}, machine.ErrMachineAuthority
		}
		if err != nil {
			return machine.ConnectionRequest{}, fmt.Errorf("lock Machine enrollment Membership: %w", err)
		}
	}
	request, err := queries.LockMachineConnectionRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.ConnectionRequest{}, machine.ErrConnectionUnavailable
	}
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("lock Machine connection decision: %w", err)
	}
	nowValue, err := queries.MachineDatabaseTime(ctx)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("load Machine decision time: %w", err)
	}
	if subtle.ConstantTimeCompare(request.UserCodeDigest, command.CodeDigest[:]) != 1 {
		return machine.ConnectionRequest{}, machine.ErrConnectionUnavailable
	}
	if request.Decision != nil {
		if *request.Decision == command.Decision && uuidValue(request.DecidedByUserID) == userID &&
			request.DecisionIdempotencyKey != nil && *request.DecisionIdempotencyKey == command.IdempotencyKey &&
			bytes.Equal(request.DecisionRequestDigest, command.RequestDigest[:]) {
			if err := tx.Commit(ctx); err != nil {
				return machine.ConnectionRequest{}, fmt.Errorf("commit Machine decision replay: %w", err)
			}
			return machineConnectionRequest(request), nil
		}
		return machine.ConnectionRequest{}, machine.ErrConnectionAlreadyDecided
	}
	if request.CancelledAt.Valid {
		return machine.ConnectionRequest{}, machine.ErrConnectionCancelled
	}
	if !request.ExpiresAt.Time.After(nowValue.Time) {
		return machine.ConnectionRequest{}, machine.ErrConnectionExpired
	}
	actorUserID, err := postgresUUID(userID)
	if err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("parse Machine decision User identity: %w", err)
	}
	decision := command.Decision
	key := command.IdempotencyKey
	updated, err := queries.DecideMachineConnection(ctx, dbsqlc.DecideMachineConnectionParams{
		Decision:          &decision,
		UserID:            actorUserID,
		SpaceID:           spaceID,
		IdempotencyKey:    &key,
		RequestDigest:     command.RequestDigest[:],
		PreparedMachineID: preparedMachineID,
		RequestID:         command.RequestID,
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return machine.ConnectionRequest{}, machine.ErrConnectionConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return machine.ConnectionRequest{}, machine.ErrConnectionAlreadyDecided
		}
		return machine.ConnectionRequest{}, fmt.Errorf("record Machine connection decision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.ConnectionRequest{}, fmt.Errorf("commit Machine connection decision: %w", err)
	}
	return machineConnectionRequest(updated), nil
}

func (s *Store) PollMachineConnection(ctx context.Context, command machine.PollConnectionCommand, issuer machine.CertificateIssuer) (machine.ConnectedMachine, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("begin Machine connection poll: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	request, err := queries.LockMachineConnectionRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.ConnectedMachine{}, machine.ErrConnectionUnavailable
	}
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("lock Machine connection poll: %w", err)
	}
	if subtle.ConstantTimeCompare(request.PollSecretDigest, command.PollDigest[:]) != 1 {
		return machine.ConnectedMachine{}, machine.ErrMachineUnavailable
	}
	nowValue, err := queries.MachineDatabaseTime(ctx)
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("load Machine poll time: %w", err)
	}
	now := nowValue.Time
	if request.RedeemedAt.Valid {
		if !request.ReplayUntil.Valid || !request.ReplayUntil.Time.After(now) || !request.ResultingMachineID.Valid {
			return machine.ConnectedMachine{}, machine.ErrConnectionReplayExpired
		}
		stored, loadErr := queries.LoadMachine(ctx, uuidValue(request.ResultingMachineID))
		if loadErr != nil || stored.RevokedAt.Valid {
			return machine.ConnectedMachine{}, machine.ErrMachineUnavailable
		}
		if err := tx.Commit(ctx); err != nil {
			return machine.ConnectedMachine{}, fmt.Errorf("commit Machine certificate replay: %w", err)
		}
		return connectedMachine(request, stored), nil
	}
	if request.Decision != nil && *request.Decision == "denied" {
		return machine.ConnectedMachine{}, machine.ErrConnectionDenied
	}
	if request.CancelledAt.Valid {
		return machine.ConnectedMachine{}, machine.ErrConnectionCancelled
	}
	if !request.ExpiresAt.Time.After(now) {
		return machine.ConnectedMachine{}, machine.ErrConnectionExpired
	}
	interval := time.Duration(request.PollIntervalSeconds) * time.Second
	nextPollAt := request.CreatedAt.Time.Add(interval)
	if request.LastPolledAt.Valid {
		nextPollAt = request.LastPolledAt.Time.Add(interval)
	}
	if now.Before(nextPollAt) {
		updated, updateErr := queries.SlowDownMachineConnectionPoll(ctx, command.RequestID)
		if updateErr != nil {
			return machine.ConnectedMachine{}, fmt.Errorf("slow Machine connection polling: %w", updateErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return machine.ConnectedMachine{}, fmt.Errorf("commit Machine poll slow down: %w", err)
		}
		return machine.ConnectedMachine{}, machine.NewConnectionSlowDownError(time.Duration(updated.PollIntervalSeconds) * time.Second)
	}
	request, err = queries.RecordMachineConnectionPoll(ctx, command.RequestID)
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("record Machine connection poll: %w", err)
	}
	if request.Decision == nil {
		if err := tx.Commit(ctx); err != nil {
			return machine.ConnectedMachine{}, fmt.Errorf("commit pending Machine poll: %w", err)
		}
		return machine.ConnectedMachine{}, machine.ErrConnectionPending
	}
	machineID := uuidValue(request.PreparedMachineID)
	issued, err := issuer(machineID, request.PublicKeyDer, request.DecidedAt.Time)
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("issue approved Machine certificate: %w", err)
	}
	stored, err := queries.CreateConnectedMachine(ctx, dbsqlc.CreateConnectedMachineParams{
		MachineID: machineID, SpaceID: uuidValue(request.DecidedSpaceID), DisplayName: request.DisplayName,
		PublicKeyDer: request.PublicKeyDer, CertificatePem: issued.CertificatePEM,
		CertificateSerial: issued.Serial, EnrolledByUserID: uuidValue(request.DecidedByUserID),
	})
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("create approved Machine: %w", err)
	}
	redeemedMachineID, err := postgresUUID(machineID)
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("parse redeemed Machine identity: %w", err)
	}
	request, err = queries.RedeemMachineConnection(ctx, dbsqlc.RedeemMachineConnectionParams{
		MachineID: redeemedMachineID,
		RequestID: command.RequestID,
	})
	if err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("redeem Machine connection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.ConnectedMachine{}, fmt.Errorf("commit Machine connection redemption: %w", err)
	}
	return connectedMachine(request, stored), nil
}

func (s *Store) CancelMachineConnection(ctx context.Context, command machine.CancelConnectionCommand) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Machine connection cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	request, err := queries.LockMachineConnectionRequest(ctx, command.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.ErrConnectionUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock Machine connection cancellation: %w", err)
	}
	if subtle.ConstantTimeCompare(request.PollSecretDigest, command.PollDigest[:]) != 1 {
		return machine.ErrMachineUnavailable
	}
	if request.CancelledAt.Valid {
		return tx.Commit(ctx)
	}
	if request.Decision != nil {
		return machine.ErrConnectionAlreadyDecided
	}
	nowValue, err := queries.MachineDatabaseTime(ctx)
	if err != nil {
		return fmt.Errorf("load Machine cancellation time: %w", err)
	}
	if !request.ExpiresAt.Time.After(nowValue.Time) {
		return machine.ErrConnectionExpired
	}
	if _, err := queries.CancelMachineConnection(ctx, command.RequestID); err != nil {
		return fmt.Errorf("cancel Machine connection: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Machine connection cancellation: %w", err)
	}
	return nil
}

func (s *Store) ListMachines(ctx context.Context, command machine.ListMachinesCommand) (machine.MachinePage, []agent.InventoryRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.MachinePage{}, nil, fmt.Errorf("begin Machine inventory: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)
	membership, err := queries.LoadMachineListMembership(ctx, dbsqlc.LoadMachineListMembershipParams{
		SessionID: command.BrowserSessionID, SpaceID: command.SpaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.MachinePage{}, nil, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.MachinePage{}, nil, fmt.Errorf("load Machine inventory Membership: %w", err)
	}
	after, err := nullablePostgresUUID(command.After)
	if err != nil {
		return machine.MachinePage{}, nil, machine.ErrInvalidConnection
	}
	rows, err := queries.ListSpaceMachines(ctx, dbsqlc.ListSpaceMachinesParams{SpaceID: command.SpaceID, AfterMachineID: after})
	if err != nil {
		return machine.MachinePage{}, nil, fmt.Errorf("list Space Machines: %w", err)
	}
	page := machine.MachinePage{Machines: make([]machine.MachineRecord, 0, min(len(rows), 50))}
	machineIDs := make([]string, 0, min(len(rows), 50))
	for index, row := range rows {
		if index == 50 {
			page.NextCursor = rows[49].MachineID
			break
		}
		page.Machines = append(page.Machines, inventoryRecord(row, row.PublicKeyDer, membership.CanEnrollMachines))
		machineIDs = append(machineIDs, row.MachineID)
	}
	agents, err := listInventoryAgents(ctx, queries, machineIDs)
	if err != nil {
		return machine.MachinePage{}, nil, fmt.Errorf("list Machine Agents: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.MachinePage{}, nil, fmt.Errorf("commit Machine inventory: %w", err)
	}
	return page, agents, nil
}

func (s *Store) RevokeMachineFromBrowser(ctx context.Context, command machine.RevokeMachineCommand) (machine.MachineRecord, []agent.InventoryRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("begin Browser Machine revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)

	// Phase 1: lock the Browser, Space, current authority, and exact Machine.
	session, err := queries.LockBrowserSessionForMachineConnection(ctx, command.BrowserSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.MachineRecord{}, nil, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("lock Browser Session for Machine revocation: %w", err)
	}
	userID := session.UserID
	if _, err := queries.LockSpaceForMachineConnection(ctx, command.SpaceID); errors.Is(err, pgx.ErrNoRows) {
		return machine.MachineRecord{}, nil, machine.ErrMachineUnavailable
	} else if err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("lock Space for Machine revocation: %w", err)
	}
	membership, err := queries.LockMachineEnrollmentMembership(ctx, dbsqlc.LockMachineEnrollmentMembershipParams{
		SpaceID: command.SpaceID,
		UserID:  userID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !membership.CanEnrollMachines) {
		return machine.MachineRecord{}, nil, machine.ErrMachineAuthority
	}
	if err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("lock Machine revocation Membership: %w", err)
	}
	stored, err := queries.LockMachineForRevocation(ctx, dbsqlc.LockMachineForRevocationParams{
		MachineID: command.MachineID,
		SpaceID:   command.SpaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.MachineRecord{}, nil, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("lock Machine for revocation: %w", err)
	}

	// Phase 2: reject conflicting replay or apply the one terminal transition.
	if stored.RevokedAt.Valid {
		if stored.RevocationActorKind != nil && *stored.RevocationActorKind == "user" && uuidValue(stored.RevokedByUserID) == userID &&
			stored.RevocationIdempotencyKey != nil && *stored.RevocationIdempotencyKey == command.IdempotencyKey &&
			!bytes.Equal(stored.RevocationRequestDigest, command.RequestDigest[:]) {
			return machine.MachineRecord{}, nil, machine.ErrConnectionConflict
		}
	} else {
		actorUserID, parseErr := postgresUUID(userID)
		if parseErr != nil {
			return machine.MachineRecord{}, nil, fmt.Errorf("parse Machine revocation User identity: %w", parseErr)
		}
		key := command.IdempotencyKey
		stored, err = queries.RevokeMachineByUser(ctx, dbsqlc.RevokeMachineByUserParams{
			UserID:         actorUserID,
			IdempotencyKey: &key,
			RequestDigest:  command.RequestDigest[:],
			MachineID:      command.MachineID,
		})
		if err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) && postgresError.Code == "23505" {
				return machine.MachineRecord{}, nil, machine.ErrConnectionConflict
			}
			return machine.MachineRecord{}, nil, fmt.Errorf("revoke Machine from Browser: %w", err)
		}
		if _, err := queries.LockActiveAgentsForMachineRemoval(ctx, command.MachineID); err != nil {
			return machine.MachineRecord{}, nil, fmt.Errorf("lock Browser-revoked Machine Agents: %w", err)
		}
		if err := queries.RemoveActiveAgentsForMachine(ctx, command.MachineID); err != nil {
			return machine.MachineRecord{}, nil, fmt.Errorf("remove Browser-revoked Machine Agents: %w", err)
		}
	}

	// Phase 3: project the exact removed identities and commit one response truth.
	agents, err := listInventoryAgents(ctx, queries, []string{command.MachineID})
	if err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("list Browser-revoked Machine Agents: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.MachineRecord{}, nil, fmt.Errorf("commit Browser Machine revocation: %w", err)
	}
	return storedMachineRecord(stored), agents, nil
}

func (s *Store) RevokeMachineFromHost(ctx context.Context, command machine.SelfRevokeMachineCommand) (machine.MachineRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.MachineRecord{}, fmt.Errorf("begin self Machine revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)

	// Phase 1: lock and authenticate the exact Machine.
	stored, err := queries.LockMachineByIDForSelfRevocation(ctx, command.MachineID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && stored.CertificateSerial != command.CertificateSerial) {
		return machine.MachineRecord{}, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.MachineRecord{}, fmt.Errorf("lock Machine self revocation: %w", err)
	}

	// Phase 2: reject conflicting replay or apply the terminal Machine and Agent transition.
	if stored.RevokedAt.Valid {
		if stored.RevocationActorKind != nil && *stored.RevocationActorKind == "machine" &&
			stored.RevocationIdempotencyKey != nil && *stored.RevocationIdempotencyKey == command.IdempotencyKey &&
			!bytes.Equal(stored.RevocationRequestDigest, command.RequestDigest[:]) {
			return machine.MachineRecord{}, machine.ErrConnectionConflict
		}
	} else {
		key := command.IdempotencyKey
		stored, err = queries.RevokeMachineBySelf(ctx, dbsqlc.RevokeMachineBySelfParams{
			IdempotencyKey: &key,
			RequestDigest:  command.RequestDigest[:],
			MachineID:      command.MachineID,
		})
		if err != nil {
			return machine.MachineRecord{}, fmt.Errorf("revoke Machine from Host: %w", err)
		}
		if _, err := queries.LockActiveAgentsForMachineRemoval(ctx, command.MachineID); err != nil {
			return machine.MachineRecord{}, fmt.Errorf("lock self-revoked Machine Agents: %w", err)
		}
		if err := queries.RemoveActiveAgentsForMachine(ctx, command.MachineID); err != nil {
			return machine.MachineRecord{}, fmt.Errorf("remove self-revoked Machine Agents: %w", err)
		}
	}

	// Phase 3: commit the terminal Machine and Agent lifecycle truth.
	if err := tx.Commit(ctx); err != nil {
		return machine.MachineRecord{}, fmt.Errorf("commit self Machine revocation: %w", err)
	}
	return storedMachineRecord(stored), nil
}

func machineConnectionRequest(row dbsqlc.MachineConnectionRequest) machine.ConnectionRequest {
	request := machine.ConnectionRequest{
		RequestID: row.RequestID, DisplayName: row.DisplayName, PublicKeyDER: row.PublicKeyDer, KeyProof: row.KeyProof,
		CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		PollInterval:     time.Duration(row.PollIntervalSeconds) * time.Second,
		ApprovedByUserID: uuidValue(row.DecidedByUserID), ApprovedSpaceID: uuidValue(row.DecidedSpaceID),
		PreparedMachineID: uuidValue(row.PreparedMachineID), ResultingMachineID: uuidValue(row.ResultingMachineID),
	}
	if row.Decision != nil {
		request.Decision = *row.Decision
	}
	request.ApprovedAt = postgresTimePointer(row.DecidedAt)
	if request.Decision == "denied" {
		request.DeniedAt, request.ApprovedAt = request.ApprovedAt, nil
	}
	request.CancelledAt = postgresTimePointer(row.CancelledAt)
	request.RedeemedAt = postgresTimePointer(row.RedeemedAt)
	request.ReplayUntil = postgresTimePointer(row.ReplayUntil)
	return request
}

func connectedMachine(request dbsqlc.MachineConnectionRequest, stored dbsqlc.Machine) machine.ConnectedMachine {
	return machine.ConnectedMachine{
		MachineID: stored.MachineID, SpaceID: stored.SpaceID, DisplayName: stored.DisplayName,
		CertificatePEM: stored.CertificatePem, RedeemedAt: request.RedeemedAt.Time, ReplayUntil: request.ReplayUntil.Time,
	}
}

func inventoryRecord(row dbsqlc.ListSpaceMachinesRow, publicKeyDER []byte, canRevoke bool) machine.MachineRecord {
	fingerprint := machine.PublicKeyFingerprint(publicKeyDER)
	record := machine.MachineRecord{
		MachineID: row.MachineID, SpaceID: row.SpaceID, SpaceName: row.SpaceName,
		DisplayName: row.DisplayName, Fingerprint: fingerprint, State: "Active", EnrolledByUserID: row.EnrolledByUserID,
		EnrolledByName: row.EnrolledByName, EnrolledAt: row.EnrolledAt.Time, CanRevoke: canRevoke,
	}
	if row.RevokedAt.Valid {
		record.State, record.RevokedAt = "Revoked", &row.RevokedAt.Time
		record.RevocationActor = row.RevocationActorKind
		record.RevokedByUserID, record.RevokedByName = uuidValue(row.RevokedByUserID), row.RevokedByName
	}
	return record
}

func storedMachineRecord(row dbsqlc.Machine) machine.MachineRecord {
	record := machine.MachineRecord{
		MachineID: row.MachineID, SpaceID: row.SpaceID, DisplayName: row.DisplayName,
		Fingerprint: machine.PublicKeyFingerprint(row.PublicKeyDer), State: "Active", EnrolledByUserID: row.EnrolledByUserID, EnrolledAt: row.EnrolledAt.Time,
	}
	if row.RevokedAt.Valid {
		record.State, record.RevokedAt = "Revoked", &row.RevokedAt.Time
		if row.RevocationActorKind != nil {
			record.RevocationActor = *row.RevocationActorKind
		}
		record.RevokedByUserID = uuidValue(row.RevokedByUserID)
	}
	return record
}
