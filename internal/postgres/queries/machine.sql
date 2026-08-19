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
