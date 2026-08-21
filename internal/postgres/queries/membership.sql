-- name: ListMemberships :many
SELECT
    m.space_id,
    s.name,
    m.can_manage_members,
    m.can_enroll_machines
FROM space_memberships AS m
INNER JOIN spaces AS s ON m.space_id = s.space_id
WHERE
    m.user_id = sqlc.arg(user_id)
    AND m.revoked_at IS NULL
ORDER BY s.name, m.space_id;

-- name: SpaceMemberCursor :one
SELECT created_at
FROM space_memberships
WHERE space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
    AND revoked_at IS NULL;

-- name: ListActiveSpaceMembers :many
SELECT m.user_id, u.display_name, m.can_manage_members,
    m.can_enroll_machines, m.created_at AS joined_at,
    (SELECT count(*) FROM works AS w
        WHERE w.space_id = m.space_id
            AND w.owner_user_id = m.user_id
            AND w.lifecycle = 'open') AS open_work_count
FROM space_memberships AS m
INNER JOIN carry_users AS u ON u.user_id = m.user_id
WHERE m.space_id = sqlc.arg(space_id)
    AND m.revoked_at IS NULL
    AND (m.created_at, m.user_id) > (sqlc.arg(cursor_created_at), sqlc.arg(cursor_user_id)::uuid)
ORDER BY m.created_at, m.user_id
LIMIT 51;

-- name: LockSpaceForMemberRemoval :one
SELECT space_id
FROM spaces
WHERE space_id = sqlc.arg(space_id)
FOR NO KEY UPDATE;

-- name: LoadMemberRemovalReplay :one
SELECT user_id, removal_successor_user_id, removal_request_digest
FROM space_memberships
WHERE space_id = sqlc.arg(space_id)
    AND removed_by_user_id = sqlc.arg(actor_user_id)
    AND removal_idempotency_key = sqlc.arg(idempotency_key);

-- name: LockMembershipForRemoval :one
SELECT user_id, can_manage_members, can_enroll_machines, revoked_at
FROM space_memberships
WHERE space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: CountActiveSpaceAuthorities :one
SELECT
    count(*) FILTER (WHERE can_manage_members) AS member_managers,
    count(*) FILTER (WHERE can_enroll_machines) AS machine_enrollers
FROM space_memberships
WHERE space_id = sqlc.arg(space_id)
    AND revoked_at IS NULL;

-- name: LockOpenWorksOwnedByMember :many
SELECT work_id
FROM works
WHERE space_id = sqlc.arg(space_id)
    AND owner_user_id = sqlc.arg(user_id)
    AND lifecycle = 'open'
ORDER BY work_id
FOR UPDATE;

-- name: TransferRemovedMemberOpenWorks :execrows
UPDATE works
SET owner_user_id = sqlc.arg(successor_user_id)
WHERE space_id = sqlc.arg(space_id)
    AND owner_user_id = sqlc.arg(target_user_id)
    AND lifecycle = 'open';

-- name: RevokeSpaceMembership :execrows
UPDATE space_memberships
SET revoked_at = transaction_timestamp(),
    version = version + 1,
    removed_by_user_id = sqlc.arg(actor_user_id),
    removal_successor_user_id = sqlc.narg(successor_user_id),
    removal_idempotency_key = sqlc.arg(idempotency_key),
    removal_request_digest = sqlc.arg(request_digest)
WHERE space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(target_user_id)
    AND revoked_at IS NULL;
