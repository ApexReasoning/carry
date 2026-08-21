-- name: LockActiveWorkMembership :one
SELECT version
FROM space_memberships
WHERE
    space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
    AND revoked_at IS NULL
FOR SHARE;

-- name: CreateWork :one
WITH created AS (
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
    RETURNING *
)
SELECT
    created.work_id,
    created.space_id,
    created.goal,
    created.lifecycle,
    created.owner_user_id,
    owner.display_name AS owner_display_name,
    created.creator_user_id,
    creator.display_name AS creator_display_name,
    created.input_head_seq,
    created.applied_input_seq,
    created.understanding,
    created.next_step,
    created.created_at
FROM created
JOIN carry_users AS owner ON owner.user_id = created.owner_user_id
JOIN carry_users AS creator ON creator.user_id = created.creator_user_id;

-- name: FindWorkByCreateIdempotency :one
SELECT
    work.work_id,
    work.space_id,
    work.goal,
    work.lifecycle,
    work.owner_user_id,
    owner.display_name AS owner_display_name,
    work.creator_user_id,
    creator.display_name AS creator_display_name,
    work.input_head_seq,
    work.applied_input_seq,
    work.understanding,
    work.next_step,
    work.created_at,
    work.create_request_digest,
    EXISTS (
        SELECT 1 FROM runs
        WHERE runs.work_id = work.work_id
          AND runs.state IN ('failed', 'unknown')
          AND runs.retry_requested_at IS NULL
    ) AS needs_retry,
    EXISTS (
        SELECT 1 FROM work_result_checks AS result_check
        WHERE result_check.work_id = work.work_id
          AND result_check.understanding_version = work.understanding_version
          AND result_check.accepted_at IS NULL
          AND work.applied_input_seq = work.input_head_seq
    ) AS needs_review
FROM works AS work
JOIN carry_users AS owner ON owner.user_id = work.owner_user_id
JOIN carry_users AS creator ON creator.user_id = work.creator_user_id
WHERE
    work.space_id = sqlc.arg(space_id)
    AND work.creator_user_id = sqlc.arg(creator_user_id)
    AND work.create_idempotency_key = sqlc.arg(create_idempotency_key);

-- name: WorkListCursor :one
SELECT created_at, work_id
FROM works
WHERE space_id = sqlc.arg(space_id) AND work_id = sqlc.arg(work_id);

-- name: ListNewestWorks :many
SELECT
    work.work_id,
    work.space_id,
    work.goal,
    work.lifecycle,
    work.owner_user_id,
    owner.display_name AS owner_display_name,
    work.creator_user_id,
    creator.display_name AS creator_display_name,
    work.input_head_seq,
    work.applied_input_seq,
    work.created_at,
    EXISTS (
        SELECT 1 FROM runs
        WHERE runs.work_id = work.work_id
          AND runs.state IN ('failed', 'unknown')
          AND runs.retry_requested_at IS NULL
    ) AS needs_retry,
    EXISTS (
        SELECT 1 FROM work_result_checks AS result_check
        WHERE result_check.work_id = work.work_id
          AND result_check.understanding_version = work.understanding_version
          AND result_check.accepted_at IS NULL
          AND work.applied_input_seq = work.input_head_seq
    ) AS needs_review
FROM works AS work
JOIN carry_users AS owner ON owner.user_id = work.owner_user_id
JOIN carry_users AS creator ON creator.user_id = work.creator_user_id
WHERE
    work.space_id = sqlc.arg(space_id)
    AND (
        NOT sqlc.arg(needs_you)::boolean
        OR (
            work.owner_user_id = sqlc.arg(user_id)::uuid
            AND (
                EXISTS (
                    SELECT 1 FROM runs
                    WHERE runs.work_id = work.work_id
                      AND runs.state IN ('failed', 'unknown')
                      AND runs.retry_requested_at IS NULL
                )
                OR EXISTS (
                    SELECT 1 FROM work_result_checks AS result_check
                    WHERE result_check.work_id = work.work_id
                      AND result_check.understanding_version = work.understanding_version
                      AND result_check.accepted_at IS NULL
                      AND work.applied_input_seq = work.input_head_seq
                )
            )
        )
    )
ORDER BY work.created_at DESC, work.work_id DESC
LIMIT 51;

-- name: ListWorksBefore :many
SELECT
    work.work_id,
    work.space_id,
    work.goal,
    work.lifecycle,
    work.owner_user_id,
    owner.display_name AS owner_display_name,
    work.creator_user_id,
    creator.display_name AS creator_display_name,
    work.input_head_seq,
    work.applied_input_seq,
    work.created_at,
    EXISTS (
        SELECT 1 FROM runs
        WHERE runs.work_id = work.work_id
          AND runs.state IN ('failed', 'unknown')
          AND runs.retry_requested_at IS NULL
    ) AS needs_retry,
    EXISTS (
        SELECT 1 FROM work_result_checks AS result_check
        WHERE result_check.work_id = work.work_id
          AND result_check.understanding_version = work.understanding_version
          AND result_check.accepted_at IS NULL
          AND work.applied_input_seq = work.input_head_seq
    ) AS needs_review
FROM works AS work
JOIN carry_users AS owner ON owner.user_id = work.owner_user_id
JOIN carry_users AS creator ON creator.user_id = work.creator_user_id
WHERE
    work.space_id = sqlc.arg(space_id)
    AND (
        NOT sqlc.arg(needs_you)::boolean
        OR (
            work.owner_user_id = sqlc.arg(user_id)::uuid
            AND (
                EXISTS (
                    SELECT 1 FROM runs
                    WHERE runs.work_id = work.work_id
                      AND runs.state IN ('failed', 'unknown')
                      AND runs.retry_requested_at IS NULL
                )
                OR EXISTS (
                    SELECT 1 FROM work_result_checks AS result_check
                    WHERE result_check.work_id = work.work_id
                      AND result_check.understanding_version = work.understanding_version
                      AND result_check.accepted_at IS NULL
                      AND work.applied_input_seq = work.input_head_seq
                )
            )
        )
    )
    AND (work.created_at, work.work_id) < (
        sqlc.arg(cursor_created_at)::timestamptz,
        sqlc.arg(cursor_work_id)::uuid
    )
ORDER BY work.created_at DESC, work.work_id DESC
LIMIT 51;

-- name: LoadWork :one
SELECT
    work.work_id,
    work.space_id,
    work.goal,
    work.lifecycle,
    work.owner_user_id,
    owner.display_name AS owner_display_name,
    work.creator_user_id,
    creator.display_name AS creator_display_name,
    work.input_head_seq,
    work.applied_input_seq,
    work.understanding,
    work.next_step,
    work.created_at,
    EXISTS (
        SELECT 1 FROM runs
        WHERE runs.work_id = work.work_id
          AND runs.state IN ('failed', 'unknown')
          AND runs.retry_requested_at IS NULL
    ) AS needs_retry,
    EXISTS (
        SELECT 1 FROM work_result_checks AS result_check
        WHERE result_check.work_id = work.work_id
          AND result_check.understanding_version = work.understanding_version
          AND result_check.accepted_at IS NULL
          AND work.applied_input_seq = work.input_head_seq
    ) AS needs_review,
    coalesce((
        SELECT result_check.review_id::text
        FROM work_result_checks AS result_check
        WHERE result_check.work_id = work.work_id
          AND result_check.understanding_version = work.understanding_version
          AND result_check.accepted_at IS NULL
          AND work.applied_input_seq = work.input_head_seq
        LIMIT 1
    ), '')::text AS review_id
FROM works AS work
JOIN carry_users AS owner ON owner.user_id = work.owner_user_id
JOIN carry_users AS creator ON creator.user_id = work.creator_user_id
WHERE work.space_id = sqlc.arg(space_id) AND work.work_id = sqlc.arg(work_id)
FOR SHARE OF work;

-- name: LockWork :one
SELECT work_id, space_id, goal, lifecycle, owner_user_id, creator_user_id,
    input_head_seq, applied_input_seq, understanding_version, understanding, next_step, created_at
FROM works
WHERE space_id = sqlc.arg(space_id) AND work_id = sqlc.arg(work_id)
FOR UPDATE;

-- name: FindWorkReviewAcceptanceByIdempotency :one
SELECT result_check.review_id::text AS review_id, result_check.accept_request_digest
FROM work_result_checks AS result_check
JOIN works AS work ON work.work_id = result_check.work_id
WHERE
    result_check.work_id = sqlc.arg(work_id)
    AND work.space_id = sqlc.arg(space_id)
    AND result_check.accepted_by_user_id = sqlc.arg(accepted_by_user_id)::uuid
    AND result_check.accept_idempotency_key = sqlc.arg(accept_idempotency_key)::text
    AND result_check.accepted_at IS NOT NULL;

-- name: LockWorkResultCheck :one
SELECT
    understanding_version,
    content_digest,
    accepted_at,
    coalesce(accepted_by_user_id::text, '')::text AS accepted_by_user_id,
    coalesce(accept_idempotency_key, '')::text AS accept_idempotency_key,
    accept_request_digest
FROM work_result_checks
WHERE work_id = sqlc.arg(work_id) AND review_id = sqlc.arg(review_id)
FOR UPDATE;

-- name: AcceptWorkResultCheck :execrows
UPDATE work_result_checks
SET
    accepted_by_user_id = sqlc.arg(accepted_by_user_id)::uuid,
    accept_idempotency_key = sqlc.arg(accept_idempotency_key)::text,
    accept_request_digest = sqlc.arg(accept_request_digest),
    accepted_at = clock_timestamp()
WHERE
    work_id = sqlc.arg(work_id)
    AND review_id = sqlc.arg(review_id)
    AND accepted_at IS NULL;

-- name: FindWorkMessageByIdempotency :one
SELECT
    message.message_id,
    message.work_id,
    message.author_user_id,
    author.display_name AS author_display_name,
    message.text,
    message.input_seq,
    message.created_at,
    message.request_digest
FROM work_messages AS message
JOIN carry_users AS author ON author.user_id = message.author_user_id
WHERE
    message.work_id = sqlc.arg(work_id)
    AND message.author_user_id = sqlc.arg(author_user_id)
    AND message.idempotency_key = sqlc.arg(idempotency_key);

-- name: AdvanceWorkInputHead :one
UPDATE works
SET input_head_seq = input_head_seq + 1
WHERE work_id = sqlc.arg(work_id) AND lifecycle = 'open'
RETURNING input_head_seq;

-- name: CreateWorkMessage :one
WITH created AS (
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
    RETURNING *
)
SELECT
    created.message_id,
    created.work_id,
    created.author_user_id,
    author.display_name AS author_display_name,
    created.text,
    created.input_seq,
    created.created_at
FROM created
JOIN carry_users AS author ON author.user_id = created.author_user_id;

-- name: WorkMessageCursorSequence :one
SELECT input_seq
FROM work_messages
WHERE work_id = sqlc.arg(work_id) AND message_id = sqlc.arg(message_id);

-- name: ListNewestWorkMessages :many
WITH candidates AS (
    SELECT message_id, work_id, author_user_id, text, input_seq, created_at
    FROM work_messages
    WHERE work_id = sqlc.arg(work_id)
    ORDER BY input_seq DESC
    LIMIT 50
), bounded AS (
    SELECT
        candidates.*,
        sum(octet_length(text)) OVER (
            ORDER BY input_seq DESC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS text_bytes
    FROM candidates
)
SELECT
    bounded.message_id,
    bounded.work_id,
    bounded.author_user_id,
    author.display_name AS author_display_name,
    bounded.text,
    bounded.input_seq,
    bounded.created_at
FROM bounded
JOIN carry_users AS author ON author.user_id = bounded.author_user_id
WHERE bounded.text_bytes <= 262144
ORDER BY bounded.input_seq;

-- name: ListWorkMessagesBefore :many
WITH candidates AS (
    SELECT message_id, work_id, author_user_id, text, input_seq, created_at
    FROM work_messages
    WHERE work_id = sqlc.arg(work_id) AND input_seq < sqlc.arg(cursor_sequence)
    ORDER BY input_seq DESC
    LIMIT 50
), bounded AS (
    SELECT
        candidates.*,
        sum(octet_length(text)) OVER (
            ORDER BY input_seq DESC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS text_bytes
    FROM candidates
)
SELECT
    bounded.message_id,
    bounded.work_id,
    bounded.author_user_id,
    author.display_name AS author_display_name,
    bounded.text,
    bounded.input_seq,
    bounded.created_at
FROM bounded
JOIN carry_users AS author ON author.user_id = bounded.author_user_id
WHERE bounded.text_bytes <= 262144
ORDER BY bounded.input_seq;

-- name: WorkHasMessagesBefore :one
SELECT EXISTS (
    SELECT 1 FROM work_messages
    WHERE work_id = sqlc.arg(work_id) AND input_seq < sqlc.arg(input_seq)
);
