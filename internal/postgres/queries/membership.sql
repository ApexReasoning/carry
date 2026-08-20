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

-- name: GetMachineEnrollmentPermission :one
SELECT can_enroll_machines
FROM space_memberships
WHERE
    space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
    AND revoked_at IS NULL
FOR SHARE;
