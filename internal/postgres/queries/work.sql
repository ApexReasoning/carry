-- name: LockActiveWorkMembership :one
SELECT version
FROM space_memberships
WHERE
    space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
    AND revoked_at IS NULL
FOR SHARE;

-- name: CreateWork :one
INSERT INTO works (
    work_id,
    space_id,
    goal,
    owner_user_id,
    creator_user_id,
    create_idempotency_key,
    create_request_digest
) VALUES (
    sqlc.arg(work_id),
    sqlc.arg(space_id),
    sqlc.arg(goal),
    sqlc.arg(owner_user_id),
    sqlc.arg(creator_user_id),
    sqlc.arg(create_idempotency_key),
    sqlc.arg(create_request_digest)
)
ON CONFLICT (space_id, creator_user_id, create_idempotency_key) DO NOTHING
RETURNING work_id, space_id, goal, lifecycle, owner_user_id, creator_user_id, created_at;

-- name: FindWorkByCreateIdempotency :one
SELECT work.work_id, work.space_id, work.goal, work.lifecycle, work.owner_user_id, work.creator_user_id,
    work.input_head_seq, work.applied_input_seq, work.understanding_version, work.understanding, work.next_step,
    work.created_at, work.create_request_digest
FROM works AS work
WHERE
    work.space_id = sqlc.arg(space_id)
    AND work.creator_user_id = sqlc.arg(creator_user_id)
    AND work.create_idempotency_key = sqlc.arg(create_idempotency_key);

-- name: ListWorks :many
SELECT work.work_id, work.space_id, work.goal, work.lifecycle, work.owner_user_id, work.creator_user_id,
    work.input_head_seq, work.applied_input_seq, work.understanding_version, work.understanding, work.next_step, work.created_at
FROM works AS work
WHERE work.space_id = sqlc.arg(space_id)
ORDER BY work.created_at DESC, work.work_id DESC;

-- name: LoadWork :one
SELECT work.work_id, work.space_id, work.goal, work.lifecycle, work.owner_user_id, work.creator_user_id,
    work.input_head_seq, work.applied_input_seq, work.understanding_version, work.understanding, work.next_step, work.created_at
FROM works AS work
WHERE work.space_id = sqlc.arg(space_id) AND work.work_id = sqlc.arg(work_id)
FOR SHARE OF work;

-- name: WorkNeedsRetry :one
SELECT EXISTS (
    SELECT 1 FROM runs
    WHERE work_id = sqlc.arg(work_id)
      AND state IN ('failed', 'unknown')
      AND retry_requested_at IS NULL
);

-- name: LockWork :one
SELECT work_id, space_id, goal, lifecycle, owner_user_id, creator_user_id,
    input_head_seq, applied_input_seq, understanding_version, understanding, next_step, created_at
FROM works
WHERE space_id = sqlc.arg(space_id) AND work_id = sqlc.arg(work_id)
FOR UPDATE;

-- name: FindWorkMessageByIdempotency :one
SELECT message_id, work_id, author_user_id, text, input_seq, created_at, request_digest
FROM work_messages
WHERE
    work_id = sqlc.arg(work_id)
    AND author_user_id = sqlc.arg(author_user_id)
    AND idempotency_key = sqlc.arg(idempotency_key);

-- name: AdvanceWorkInputHead :one
UPDATE works
SET input_head_seq = input_head_seq + 1
WHERE work_id = sqlc.arg(work_id) AND lifecycle = 'open'
RETURNING input_head_seq;

-- name: CreateWorkMessage :one
INSERT INTO work_messages (
    message_id,
    work_id,
    author_user_id,
    text,
    input_seq,
    idempotency_key,
    request_digest
) VALUES (
    sqlc.arg(message_id),
    sqlc.arg(work_id),
    sqlc.arg(author_user_id),
    sqlc.arg(text),
    sqlc.arg(input_seq),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_digest)
)
RETURNING message_id, work_id, author_user_id, text, input_seq, created_at;

-- name: ListWorkMessages :many
SELECT message_id, work_id, author_user_id, text, input_seq, created_at
FROM work_messages
WHERE work_id = sqlc.arg(work_id)
ORDER BY input_seq ASC;
