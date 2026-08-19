-- name: FindEnrollmentByIdempotency :one
SELECT machine_id, space_id, display_name, certificate_pem, public_key_der
FROM machines
WHERE space_id = sqlc.arg(space_id)
  AND enrolled_by_user_id = sqlc.arg(enrolled_by_user_id)
  AND enrollment_idempotency_key = sqlc.arg(enrollment_idempotency_key);

-- name: CreateMachine :one
INSERT INTO machines (
    machine_id, space_id, display_name, public_key_der, certificate_pem,
    certificate_serial, enrolled_by_user_id, enrollment_idempotency_key
) VALUES (
    sqlc.arg(machine_id), sqlc.arg(space_id), sqlc.arg(display_name),
    sqlc.arg(public_key_der), sqlc.arg(certificate_pem), sqlc.arg(certificate_serial),
    sqlc.arg(enrolled_by_user_id), sqlc.arg(enrollment_idempotency_key)
)
ON CONFLICT (space_id, enrolled_by_user_id, enrollment_idempotency_key) DO NOTHING
RETURNING machine_id, space_id, display_name, certificate_pem, public_key_der;

-- name: RevokeMachine :execrows
UPDATE machines
SET revoked_at = coalesce(revoked_at, transaction_timestamp())
WHERE machine_id = sqlc.arg(machine_id)
  AND space_id = sqlc.arg(space_id);

-- name: DeleteRuntimeObservations :exec
DELETE FROM machine_runtime_observations
WHERE machine_id = sqlc.arg(machine_id);

-- name: CreateRuntimeObservation :exec
INSERT INTO machine_runtime_observations (
    machine_id, runtime_kind, detection, executable, version,
    diagnostic_code, diagnostic_detail, observed_at
) VALUES (
    sqlc.arg(machine_id), sqlc.arg(runtime_kind), sqlc.arg(detection),
    sqlc.narg(executable), sqlc.narg(version), sqlc.narg(diagnostic_code),
    sqlc.narg(diagnostic_detail), sqlc.arg(observed_at)
);

-- name: LoadMachineStatus :one
SELECT machine_id, space_id, display_name, enrolled_at, revoked_at
FROM machines
WHERE machine_id = sqlc.arg(machine_id)
FOR UPDATE;

-- name: ListRuntimeObservations :many
SELECT runtime_kind, detection, executable, version,
       diagnostic_code, diagnostic_detail, observed_at
FROM machine_runtime_observations
WHERE machine_id = sqlc.arg(machine_id)
ORDER BY runtime_kind;
