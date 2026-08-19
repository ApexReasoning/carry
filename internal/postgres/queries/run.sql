-- name: LockClaimingMachine :one
SELECT space_id, revoked_at
FROM machines
WHERE machine_id = sqlc.arg(machine_id)
FOR SHARE;

-- name: LockExpiredRunForClaim :one
SELECT
    run.run_id,
    run.work_id,
    work.space_id,
    work.goal,
    work.understanding,
    work.next_step,
    run.input_start_seq,
    run.input_end_seq,
    run.base_understanding_version,
    run.current_fence,
    attempt.attempt_id AS expired_attempt_id
FROM runs AS run
JOIN works AS work ON work.work_id = run.work_id
JOIN run_attempts AS attempt
    ON attempt.run_id = run.run_id AND attempt.fence = run.current_fence
WHERE
    work.space_id = sqlc.arg(space_id)
    AND work.lifecycle = 'open'
    AND run.state = 'active'
    AND attempt.state = 'active'
    AND attempt.lease_expires_at <= clock_timestamp()
ORDER BY attempt.lease_expires_at, run.created_at, run.run_id
LIMIT 1
FOR UPDATE OF run, attempt, work SKIP LOCKED;

-- name: LockWorkForRunClaim :one
SELECT
    work.work_id,
    work.space_id,
    work.goal,
    work.understanding,
    work.next_step,
    work.applied_input_seq,
    work.input_head_seq,
    work.understanding_version
FROM works AS work
WHERE
    work.space_id = sqlc.arg(space_id)
    AND work.lifecycle = 'open'
    AND work.applied_input_seq < work.input_head_seq
    AND NOT EXISTS (
        SELECT 1
        FROM runs AS run
        WHERE run.work_id = work.work_id
          AND run.state IN ('active', 'failed', 'unknown')
    )
ORDER BY work.created_at, work.work_id
LIMIT 1
FOR UPDATE OF work SKIP LOCKED;

-- name: CreateRun :one
INSERT INTO runs (
    run_id,
    work_id,
    input_start_seq,
    input_end_seq,
    base_understanding_version,
    state,
    current_fence
) VALUES (
    sqlc.arg(run_id),
    sqlc.arg(work_id),
    sqlc.arg(input_start_seq),
    sqlc.arg(input_end_seq),
    sqlc.arg(base_understanding_version),
    'active',
    1
)
RETURNING created_at;

-- name: ExpireRunAttempt :execrows
UPDATE run_attempts
SET state = 'expired', completed_at = clock_timestamp()
WHERE
    attempt_id = sqlc.arg(attempt_id)
    AND run_id = sqlc.arg(run_id)
    AND fence = sqlc.arg(fence)
    AND state = 'active'
    AND lease_expires_at <= clock_timestamp();

-- name: RotateRunFence :one
UPDATE runs
SET current_fence = current_fence + 1
WHERE
    run_id = sqlc.arg(run_id)
    AND state = 'active'
    AND current_fence = sqlc.arg(current_fence)
RETURNING current_fence;

-- name: CreateRunAttempt :one
INSERT INTO run_attempts (
    attempt_id,
    run_id,
    machine_id,
    fence,
    lease_expires_at
) VALUES (
    sqlc.arg(attempt_id),
    sqlc.arg(run_id),
    sqlc.arg(machine_id),
    sqlc.arg(fence),
    clock_timestamp() + interval '5 minutes'
)
RETURNING lease_expires_at;

-- name: ListRunInputMessages :many
SELECT author_user_id, text
FROM work_messages
WHERE
    work_id = sqlc.arg(work_id)
    AND input_seq >= sqlc.arg(input_start_seq)
    AND input_seq <= sqlc.arg(input_end_seq)
ORDER BY input_seq;

-- name: LockAttemptForRenew :one
SELECT attempt.attempt_id
FROM run_attempts AS attempt
JOIN runs AS run ON run.run_id = attempt.run_id
JOIN machines AS machine ON machine.machine_id = attempt.machine_id
WHERE
    run.run_id = sqlc.arg(run_id)
    AND attempt.attempt_id = sqlc.arg(attempt_id)
    AND attempt.machine_id = sqlc.arg(claiming_machine_id)
    AND attempt.fence = sqlc.arg(fence)
    AND run.current_fence = attempt.fence
    AND run.state = 'active'
    AND attempt.state = 'active'
    AND machine.revoked_at IS NULL
FOR UPDATE OF run, attempt
FOR SHARE OF machine;

-- name: ExtendRunAttemptLease :one
WITH current_machine AS (
    SELECT machines.machine_id
    FROM machines
    WHERE machines.machine_id = sqlc.arg(claiming_machine_id) AND machines.revoked_at IS NULL
    FOR SHARE
)
UPDATE run_attempts AS attempt
SET lease_expires_at = clock_timestamp() + interval '5 minutes'
FROM runs AS run, current_machine
WHERE
    attempt.attempt_id = sqlc.arg(attempt_id)
    AND attempt.run_id = sqlc.arg(run_id)
    AND attempt.machine_id = current_machine.machine_id
    AND attempt.fence = sqlc.arg(fence)
    AND attempt.state = 'active'
    AND attempt.lease_expires_at > clock_timestamp()
    AND run.run_id = attempt.run_id
    AND run.current_fence = attempt.fence
    AND run.state = 'active'
RETURNING attempt.lease_expires_at;

-- name: LockAttemptForCommit :one
SELECT
    run.work_id,
    run.input_start_seq,
    run.input_end_seq,
    run.base_understanding_version,
    work.applied_input_seq,
    work.input_head_seq,
    work.understanding_version,
    work.lifecycle
FROM runs AS run
JOIN run_attempts AS attempt ON attempt.run_id = run.run_id
JOIN works AS work ON work.work_id = run.work_id
JOIN machines AS machine ON machine.machine_id = attempt.machine_id
WHERE
    run.run_id = sqlc.arg(run_id)
    AND attempt.attempt_id = sqlc.arg(attempt_id)
    AND attempt.machine_id = sqlc.arg(claiming_machine_id)
    AND attempt.fence = sqlc.arg(fence)
    AND run.current_fence = attempt.fence
    AND run.state = 'active'
    AND attempt.state = 'active'
    AND machine.revoked_at IS NULL
FOR UPDATE OF run, attempt, work
FOR SHARE OF machine;

-- name: RunAttemptLeaseIsCurrent :one
SELECT lease_expires_at > clock_timestamp()
FROM run_attempts
WHERE attempt_id = sqlc.arg(attempt_id);

-- name: CommitCurrentUnderstanding :execrows
UPDATE works
SET
    understanding = sqlc.arg(understanding),
    next_step = sqlc.arg(next_step),
    applied_input_seq = sqlc.arg(applied_input_seq),
    understanding_version = understanding_version + 1
WHERE
    work_id = sqlc.arg(work_id)
    AND lifecycle = 'open'
    AND applied_input_seq = sqlc.arg(expected_applied_input_seq)
    AND understanding_version = sqlc.arg(expected_understanding_version)
    AND input_head_seq >= sqlc.arg(applied_input_seq);

-- name: SucceedRunAttempt :execrows
UPDATE run_attempts
SET state = 'succeeded', completed_at = clock_timestamp()
WHERE attempt_id = sqlc.arg(attempt_id) AND state = 'active';

-- name: SucceedRun :execrows
UPDATE runs
SET state = 'succeeded', completed_at = clock_timestamp()
WHERE run_id = sqlc.arg(run_id) AND state = 'active';

-- name: LockAttemptForFinish :one
SELECT run.run_id
FROM runs AS run
JOIN run_attempts AS attempt ON attempt.run_id = run.run_id
JOIN machines AS machine ON machine.machine_id = attempt.machine_id
WHERE
    run.run_id = sqlc.arg(run_id)
    AND attempt.attempt_id = sqlc.arg(attempt_id)
    AND attempt.machine_id = sqlc.arg(claiming_machine_id)
    AND attempt.fence = sqlc.arg(fence)
    AND run.current_fence = attempt.fence
    AND run.state = 'active'
    AND attempt.state = 'active'
    AND machine.revoked_at IS NULL
FOR UPDATE OF run, attempt
FOR SHARE OF machine;

-- name: FinishRunAttempt :execrows
UPDATE run_attempts
SET state = sqlc.arg(outcome), completed_at = clock_timestamp()
WHERE attempt_id = sqlc.arg(attempt_id) AND state = 'active';

-- name: FinishRun :execrows
UPDATE runs
SET state = sqlc.arg(outcome), completed_at = clock_timestamp()
WHERE run_id = sqlc.arg(run_id) AND state = 'active';
