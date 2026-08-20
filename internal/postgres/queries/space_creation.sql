-- name: LockSpaceCreator :one
SELECT display_name
FROM carry_users
WHERE user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: LoadCreatedSpaceByRequest :one
SELECT
    s.space_id,
    s.name,
    s.created_by_user_id,
    s.create_request_digest,
    m.can_manage_members,
    m.can_enroll_machines
FROM spaces AS s
INNER JOIN space_memberships AS m
    ON m.space_id = s.space_id
    AND m.user_id = s.created_by_user_id
WHERE s.created_by_user_id = sqlc.arg(user_id)
    AND s.create_idempotency_key = sqlc.arg(idempotency_key);

-- name: HasActiveMembership :one
SELECT exists(
    SELECT 1
    FROM space_memberships
    WHERE user_id = sqlc.arg(user_id)
        AND revoked_at IS NULL
);

-- name: SetInitialDisplayName :exec
UPDATE carry_users
SET display_name = sqlc.arg(display_name)
WHERE user_id = sqlc.arg(user_id)
    AND display_name IS NULL;

-- name: CreateFirstSpace :exec
INSERT INTO spaces (
    space_id,
    name,
    created_by_user_id,
    create_idempotency_key,
    create_request_digest
) VALUES (
    sqlc.arg(space_id),
    sqlc.arg(name),
    sqlc.arg(user_id),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_digest)
);

-- name: CreateFirstSpaceMembership :exec
INSERT INTO space_memberships (
    space_id,
    user_id,
    can_enroll_machines,
    can_manage_members
) VALUES (
    sqlc.arg(space_id),
    sqlc.arg(user_id),
    true,
    true
);
