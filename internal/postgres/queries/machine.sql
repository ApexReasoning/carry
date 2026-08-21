-- name: MachineDatabaseTime :one
SELECT transaction_timestamp()::timestamptz;

-- name: LockMachineConnectionAdmission :exec
SELECT pg_advisory_xact_lock(612440219::bigint);

-- name: LoadMachineConnectionForBegin :one
SELECT * FROM machine_connection_requests WHERE request_id = sqlc.arg(request_id);

-- name: CountLiveMachineConnectionsForSource :one
SELECT count(*) FROM machine_connection_requests
WHERE source_digest = sqlc.arg(source_digest)
  AND expires_at > transaction_timestamp()
  AND decision IS NULL AND cancelled_at IS NULL;

-- name: CountLiveMachineConnections :one
SELECT count(*) FROM machine_connection_requests
WHERE expires_at > transaction_timestamp()
  AND decision IS NULL AND cancelled_at IS NULL;

-- name: CreateMachineConnection :one
INSERT INTO machine_connection_requests (
    request_id, begin_idempotency_key, begin_request_digest, source_digest,
    user_code_digest, poll_secret_digest, display_name, public_key_der, key_proof
) VALUES (
    sqlc.arg(request_id), sqlc.arg(begin_idempotency_key), sqlc.arg(begin_request_digest),
    sqlc.arg(source_digest), sqlc.arg(user_code_digest), sqlc.arg(poll_secret_digest),
    sqlc.arg(display_name), sqlc.arg(public_key_der), sqlc.arg(key_proof)
)
RETURNING *;

-- name: LockBrowserSessionForMachineConnection :one
SELECT session.user_id, coalesce(carry_user.display_name, '') AS display_name
FROM browser_sessions AS session
INNER JOIN carry_users AS carry_user ON carry_user.user_id = session.user_id
WHERE session.session_id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL
  AND session.expires_at > transaction_timestamp()
FOR UPDATE OF session;

-- name: LockMachineConnectionLookupBudget :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 19237));

-- name: CountRecentMachineConnectionLookupFailuresForSession :one
SELECT count(*) FROM machine_connection_lookup_failures
WHERE browser_session_id = sqlc.arg(browser_session_id)
  AND created_at > transaction_timestamp() - interval '10 minutes';

-- name: CountRecentMachineConnectionLookupFailuresForSource :one
SELECT count(*) FROM machine_connection_lookup_failures
WHERE source_digest = sqlc.arg(source_digest)
  AND created_at > transaction_timestamp() - interval '10 minutes';

-- name: RecordMachineConnectionLookupFailure :exec
INSERT INTO machine_connection_lookup_failures (failure_id, browser_session_id, source_digest)
VALUES (sqlc.arg(failure_id), sqlc.arg(browser_session_id), sqlc.arg(source_digest));

-- name: FindLiveMachineConnectionByCode :one
SELECT * FROM machine_connection_requests
WHERE user_code_digest = sqlc.arg(user_code_digest)
  AND expires_at > transaction_timestamp()
  AND cancelled_at IS NULL;

-- name: LockMachineConnectionRequest :one
SELECT * FROM machine_connection_requests
WHERE request_id = sqlc.arg(request_id)
FOR UPDATE;

-- name: LockSpaceForMachineConnection :one
SELECT space_id FROM spaces WHERE space_id = sqlc.arg(space_id) FOR NO KEY UPDATE;

-- name: LockMachineEnrollmentMembership :one
SELECT user_id, can_enroll_machines
FROM space_memberships
WHERE space_id = sqlc.arg(space_id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL
FOR UPDATE;

-- name: DecideMachineConnection :one
UPDATE machine_connection_requests
SET decision = sqlc.arg(decision),
    decided_at = transaction_timestamp(),
    decided_by_user_id = sqlc.arg(user_id),
    decided_space_id = sqlc.narg(space_id),
    decision_idempotency_key = sqlc.arg(idempotency_key),
    decision_request_digest = sqlc.arg(request_digest),
    prepared_machine_id = sqlc.narg(prepared_machine_id)
WHERE request_id = sqlc.arg(request_id)
  AND decision IS NULL AND cancelled_at IS NULL
  AND expires_at > transaction_timestamp()
RETURNING *;

-- name: RecordMachineConnectionPoll :one
UPDATE machine_connection_requests
SET last_polled_at = transaction_timestamp()
WHERE request_id = sqlc.arg(request_id)
RETURNING *;

-- name: SlowDownMachineConnectionPoll :one
UPDATE machine_connection_requests
SET last_polled_at = transaction_timestamp(),
    poll_interval_seconds = least(30, poll_interval_seconds + 5)
WHERE request_id = sqlc.arg(request_id)
RETURNING *;

-- name: CreateConnectedMachine :one
INSERT INTO machines (
    machine_id, space_id, display_name, public_key_der, certificate_pem,
    certificate_serial, enrolled_by_user_id
) VALUES (
    sqlc.arg(machine_id), sqlc.arg(space_id), sqlc.arg(display_name),
    sqlc.arg(public_key_der), sqlc.arg(certificate_pem), sqlc.arg(certificate_serial),
    sqlc.arg(enrolled_by_user_id)
)
RETURNING *;

-- name: RedeemMachineConnection :one
UPDATE machine_connection_requests
SET resulting_machine_id = sqlc.arg(machine_id),
    redeemed_at = transaction_timestamp(),
    replay_until = transaction_timestamp() + interval '15 minutes'
WHERE request_id = sqlc.arg(request_id)
  AND decision = 'approved' AND cancelled_at IS NULL AND redeemed_at IS NULL
RETURNING *;

-- name: LoadMachine :one
SELECT * FROM machines WHERE machine_id = sqlc.arg(machine_id);

-- name: CancelMachineConnection :one
UPDATE machine_connection_requests
SET cancelled_at = transaction_timestamp()
WHERE request_id = sqlc.arg(request_id)
  AND decision IS NULL AND cancelled_at IS NULL
  AND expires_at > transaction_timestamp()
RETURNING *;

-- name: LoadMachineListMembership :one
SELECT session.user_id, membership.can_enroll_machines
FROM browser_sessions AS session
INNER JOIN space_memberships AS membership
    ON membership.user_id = session.user_id
WHERE session.session_id = sqlc.arg(session_id)
  AND session.revoked_at IS NULL AND session.expires_at > transaction_timestamp()
  AND membership.space_id = sqlc.arg(space_id) AND membership.revoked_at IS NULL;

-- name: ListSpaceMachines :many
SELECT machine.machine_id, machine.space_id, space.name AS space_name,
    machine.display_name, machine.public_key_der, machine.enrolled_by_user_id,
    coalesce(enroller.display_name, '') AS enrolled_by_name, machine.enrolled_at,
    machine.revoked_at, coalesce(machine.revocation_actor_kind, '') AS revocation_actor_kind,
    machine.revoked_by_user_id, coalesce(revoker.display_name, '') AS revoked_by_name
FROM machines AS machine
INNER JOIN spaces AS space ON space.space_id = machine.space_id
INNER JOIN carry_users AS enroller ON enroller.user_id = machine.enrolled_by_user_id
LEFT JOIN carry_users AS revoker ON revoker.user_id = machine.revoked_by_user_id
WHERE machine.space_id = sqlc.arg(space_id)
  AND (sqlc.narg(after_machine_id)::uuid IS NULL OR machine.machine_id > sqlc.narg(after_machine_id)::uuid)
ORDER BY machine.machine_id
LIMIT 51;

-- name: LockMachineForRevocation :one
SELECT * FROM machines
WHERE machine_id = sqlc.arg(machine_id) AND space_id = sqlc.arg(space_id)
FOR UPDATE;

-- name: RevokeMachineByUser :one
UPDATE machines
SET revoked_at = transaction_timestamp(),
    revocation_actor_kind = 'user',
    revoked_by_user_id = sqlc.arg(user_id),
    revocation_idempotency_key = sqlc.arg(idempotency_key),
    revocation_request_digest = sqlc.arg(request_digest)
WHERE machine_id = sqlc.arg(machine_id) AND revoked_at IS NULL
RETURNING *;

-- name: LockMachineByIDForSelfRevocation :one
SELECT * FROM machines WHERE machine_id = sqlc.arg(machine_id) FOR UPDATE;

-- name: RevokeMachineBySelf :one
UPDATE machines
SET revoked_at = transaction_timestamp(),
    revocation_actor_kind = 'machine',
    revoked_by_user_id = NULL,
    revocation_idempotency_key = sqlc.arg(idempotency_key),
    revocation_request_digest = sqlc.arg(request_digest)
WHERE machine_id = sqlc.arg(machine_id) AND revoked_at IS NULL
RETURNING *;
