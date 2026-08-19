-- name: LockWorkForCoordination :one
SELECT
    w.work_id,
    w.space_id,
    w.applied_input_seq,
    w.input_head_seq,
    w.current_revision
FROM works AS w
WHERE
    w.lifecycle = 'open'
    AND w.applied_input_seq < w.input_head_seq
    AND NOT EXISTS (
        SELECT 1
        FROM coordinator_runs AS r
        WHERE r.work_id = w.work_id
          AND r.state IN ('pending', 'active', 'failed', 'unknown')
    )
ORDER BY w.created_at, w.work_id
LIMIT 1
FOR UPDATE OF w SKIP LOCKED;

-- name: CreateCoordinatorRun :one
INSERT INTO coordinator_runs (
    run_id,
    work_id,
    input_start_seq,
    input_end_seq,
    base_revision,
    writer_token
) VALUES (
    sqlc.arg(run_id),
    sqlc.arg(work_id),
    sqlc.arg(input_start_seq),
    sqlc.arg(input_end_seq),
    sqlc.arg(base_revision),
    sqlc.arg(writer_token)
)
RETURNING run_id, work_id, input_start_seq, input_end_seq,
    base_revision, state, created_at;

-- name: LockClaimingMachine :one
SELECT space_id, revoked_at
FROM machines
WHERE machine_id = sqlc.arg(machine_id)
FOR SHARE;

-- name: LockPendingCoordinatorRun :one
SELECT r.run_id, r.work_id, w.space_id, r.input_start_seq, r.input_end_seq,
    r.base_revision, r.writer_token, r.current_fence, r.created_at
FROM coordinator_runs AS r
JOIN works AS w ON w.work_id = r.work_id
WHERE w.space_id = sqlc.arg(space_id) AND r.state = 'pending'
ORDER BY r.created_at, r.run_id
LIMIT 1
FOR UPDATE OF r SKIP LOCKED;

-- name: ActivateCoordinatorRun :one
UPDATE coordinator_runs
SET state = 'active', current_fence = current_fence + 1
WHERE run_id = sqlc.arg(run_id) AND state = 'pending'
RETURNING current_fence;

-- name: CreateRunAttempt :one
INSERT INTO run_attempts (
    attempt_id,
    run_id,
    machine_id,
    fence,
    agent_credential_digest,
    lease_expires_at
) VALUES (
    sqlc.arg(attempt_id),
    sqlc.arg(run_id),
    sqlc.arg(machine_id),
    sqlc.arg(fence),
    sqlc.arg(agent_credential_digest),
    transaction_timestamp() + interval '5 minutes'
)
RETURNING attempt_id, fence, lease_expires_at;

-- name: ExtendRunAttemptLease :one
WITH current_machine AS (
    SELECT machines.machine_id
    FROM machines
    WHERE machines.machine_id = sqlc.arg(claiming_machine_id) AND machines.revoked_at IS NULL
    FOR SHARE OF machines
)
UPDATE run_attempts AS attempt
SET lease_expires_at = transaction_timestamp() + interval '5 minutes'
FROM coordinator_runs AS coordinator, current_machine
WHERE
    attempt.attempt_id = sqlc.arg(attempt_id)
    AND attempt.run_id = sqlc.arg(run_id)
    AND attempt.machine_id = current_machine.machine_id
    AND attempt.fence = sqlc.arg(fence)
    AND attempt.state = 'active'
    AND coordinator.run_id = attempt.run_id
    AND coordinator.current_fence = attempt.fence
    AND coordinator.state = 'active'
RETURNING attempt.lease_expires_at;

-- name: LoadActiveAttemptContext :one
SELECT
    r.run_id,
    a.attempt_id,
    r.work_id,
    w.space_id,
    w.goal,
    revision.understanding AS current_understanding,
    revision.next_step AS current_next_step,
    r.input_start_seq,
    r.input_end_seq,
    r.base_revision,
    a.fence
FROM coordinator_runs AS r
JOIN run_attempts AS a ON a.run_id = r.run_id
JOIN works AS w ON w.work_id = r.work_id
LEFT JOIN work_understanding_revisions AS revision
    ON revision.work_id = r.work_id AND revision.revision = r.base_revision
JOIN machines AS m ON m.machine_id = a.machine_id AND m.revoked_at IS NULL
WHERE
    r.run_id = sqlc.arg(run_id)
    AND a.attempt_id = sqlc.arg(attempt_id)
    AND a.fence = sqlc.arg(fence)
    AND a.agent_credential_digest = sqlc.arg(agent_credential_digest)
    AND r.state = 'active'
    AND a.state = 'active'
    AND a.lease_expires_at > transaction_timestamp();

-- name: ListRunInputMessages :many
SELECT message_id, author_user_id, text, input_seq, created_at
FROM work_messages
WHERE
    work_id = sqlc.arg(work_id)
    AND input_seq >= sqlc.arg(input_start_seq)
    AND input_seq <= sqlc.arg(input_end_seq)
ORDER BY input_seq;

-- name: LockAttemptForCommit :one
SELECT
    r.work_id,
    r.input_start_seq,
    r.input_end_seq,
    r.base_revision,
    w.applied_input_seq,
    w.input_head_seq,
    w.current_revision,
    w.lifecycle
FROM coordinator_runs AS r
JOIN run_attempts AS a ON a.run_id = r.run_id
JOIN works AS w ON w.work_id = r.work_id
JOIN machines AS m ON m.machine_id = a.machine_id AND m.revoked_at IS NULL
WHERE
    r.run_id = sqlc.arg(run_id)
    AND a.attempt_id = sqlc.arg(attempt_id)
    AND a.fence = sqlc.arg(fence)
    AND r.current_fence = sqlc.arg(fence)
    AND r.writer_token = sqlc.arg(writer_token)
    AND a.agent_credential_digest = sqlc.arg(agent_credential_digest)
    AND r.state = 'active'
    AND a.state = 'active'
    AND a.lease_expires_at > transaction_timestamp()
FOR UPDATE OF r, a, w
FOR SHARE OF m;

-- name: CreateWorkUnderstandingRevision :exec
INSERT INTO work_understanding_revisions (
    work_id,
    revision,
    source_run_id,
    understanding,
    next_step,
    applied_input_seq
) VALUES (
    sqlc.arg(work_id),
    sqlc.arg(revision),
    sqlc.arg(source_run_id),
    sqlc.arg(understanding),
    sqlc.arg(next_step),
    sqlc.arg(applied_input_seq)
);

-- name: CommitWorkRevision :execrows
UPDATE works
SET
    applied_input_seq = sqlc.arg(applied_input_seq),
    current_revision = sqlc.arg(current_revision)
WHERE
    work_id = sqlc.arg(work_id)
    AND lifecycle = 'open'
    AND applied_input_seq = sqlc.arg(expected_applied_input_seq)
    AND current_revision = sqlc.arg(expected_current_revision)
    AND input_head_seq >= sqlc.arg(applied_input_seq);

-- name: SucceedRunAttempt :execrows
UPDATE run_attempts
SET state = 'succeeded', completed_at = transaction_timestamp()
WHERE attempt_id = sqlc.arg(attempt_id) AND state = 'active';

-- name: SucceedCoordinatorRun :execrows
UPDATE coordinator_runs
SET state = 'succeeded', completed_at = transaction_timestamp()
WHERE run_id = sqlc.arg(run_id) AND state = 'active';

-- name: FinishUnresolvedRunAttempt :execrows
UPDATE run_attempts
SET state = sqlc.arg(outcome), completed_at = transaction_timestamp()
WHERE attempt_id = sqlc.arg(attempt_id) AND state = 'active';

-- name: FinishUnresolvedCoordinatorRun :execrows
UPDATE coordinator_runs
SET state = sqlc.arg(outcome)
WHERE run_id = sqlc.arg(run_id) AND state = 'active';
