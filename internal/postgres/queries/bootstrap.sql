-- name: LockBootstrap :exec
SELECT pg_advisory_xact_lock(sqlc.arg(lock_id)::bigint);

-- name: IsBootstrapped :one
SELECT exists(SELECT 1 FROM spaces);

-- name: LoadPreparedBootstrap :one
SELECT
    carry_user.display_name,
    space.name AS space_name,
    membership.can_enroll_machines,
    user_token.token_hash,
    user_token.expires_at
FROM carry_users AS carry_user
JOIN space_memberships AS membership ON membership.user_id = carry_user.user_id
JOIN spaces AS space ON space.space_id = membership.space_id
JOIN user_tokens AS user_token ON user_token.user_id = carry_user.user_id
WHERE
    carry_user.user_id = sqlc.arg(user_id)
    AND space.space_id = sqlc.arg(space_id)
    AND user_token.token_id = sqlc.arg(token_id);

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
