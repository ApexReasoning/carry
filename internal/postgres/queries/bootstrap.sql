-- name: LockBootstrap :exec
SELECT pg_advisory_xact_lock(sqlc.arg(lock_id)::bigint);

-- name: IsBootstrapped :one
SELECT exists(SELECT 1 FROM spaces);

-- name: CreateBootstrapUser :exec
INSERT INTO carry_users (user_id, display_name)
VALUES (sqlc.arg(user_id), sqlc.arg(display_name));

-- name: CreateBootstrapSpace :exec
INSERT INTO spaces (space_id, name)
VALUES (sqlc.arg(space_id), sqlc.arg(name));

-- name: CreateBootstrapMembership :exec
INSERT INTO space_memberships (space_id, user_id, can_enroll_machines)
VALUES (sqlc.arg(space_id), sqlc.arg(user_id), true);

-- name: CreateUserToken :exec
INSERT INTO user_tokens (token_id, user_id, token_hash, expires_at)
VALUES (
    sqlc.arg(token_id),
    sqlc.arg(user_id),
    sqlc.arg(token_hash),
    sqlc.arg(expires_at)
);

-- name: AuthenticateUserToken :one
SELECT user_id
FROM user_tokens
WHERE
    token_hash = sqlc.arg(token_hash)
    AND revoked_at IS null
    AND expires_at > transaction_timestamp();
