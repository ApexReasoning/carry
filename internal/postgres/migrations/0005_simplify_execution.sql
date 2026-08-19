DROP TABLE machine_runtime_observations;

ALTER TABLE works
    ADD COLUMN understanding text,
    ADD COLUMN next_step text;

UPDATE works AS work
SET
    understanding = revision.understanding,
    next_step = revision.next_step
FROM work_understanding_revisions AS revision
WHERE
    revision.work_id = work.work_id
    AND revision.revision = work.current_revision;

ALTER TABLE works
    RENAME COLUMN current_revision TO understanding_version;

ALTER TABLE works
    ADD CONSTRAINT works_understanding_pair CHECK (
        (understanding IS NULL AND next_step IS NULL AND understanding_version = 0)
        OR (
            understanding IS NOT NULL
            AND next_step IS NOT NULL
            AND understanding_version > 0
            AND btrim(understanding) <> ''
            AND octet_length(understanding) <= 61440
            AND btrim(next_step) <> ''
            AND octet_length(next_step) <= 8192
        )
    );

DROP TABLE work_understanding_revisions;

DELETE FROM coordinator_runs
WHERE state = 'pending';

ALTER TABLE coordinator_runs
    DROP CONSTRAINT coordinator_runs_check1;

UPDATE coordinator_runs
SET completed_at = created_at
WHERE state IN ('failed', 'unknown') AND completed_at IS NULL;

ALTER TABLE coordinator_runs
    DROP CONSTRAINT coordinator_runs_state_check,
    DROP CONSTRAINT coordinator_runs_writer_token_key,
    DROP COLUMN writer_token;

ALTER TABLE coordinator_runs
    RENAME COLUMN base_revision TO base_understanding_version;

ALTER TABLE coordinator_runs
    RENAME TO runs;

ALTER TABLE runs
    ALTER COLUMN state SET DEFAULT 'active',
    ADD CONSTRAINT runs_state_check CHECK (state IN ('active', 'succeeded', 'failed', 'unknown')),
    ADD CONSTRAINT runs_completion_check CHECK ((state = 'active') = (completed_at IS NULL));

ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_pkey TO runs_pkey;
ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_work_id_fkey TO runs_work_id_fkey;
ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_input_start_seq_check TO runs_input_start_seq_check;
ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_check TO runs_input_end_seq_check;
ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_base_revision_check TO runs_base_understanding_version_check;
ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_current_fence_check TO runs_current_fence_check;
ALTER TABLE runs RENAME CONSTRAINT coordinator_runs_run_id_work_id_key TO runs_run_id_work_id_key;

DROP INDEX coordinator_runs_unresolved_work_idx;
DROP INDEX coordinator_runs_pending_idx;

CREATE UNIQUE INDEX runs_unresolved_work_idx
    ON runs (work_id)
    WHERE state IN ('active', 'failed', 'unknown');

ALTER TABLE run_attempts
    DROP CONSTRAINT run_attempts_state_check,
    DROP CONSTRAINT run_attempts_check,
    DROP CONSTRAINT run_attempts_agent_credential_digest_key,
    DROP CONSTRAINT run_attempts_agent_credential_digest_check,
    DROP COLUMN agent_credential_digest;

ALTER TABLE run_attempts
    ADD CONSTRAINT run_attempts_state_check CHECK (state IN ('active', 'succeeded', 'failed', 'unknown', 'expired')),
    ADD CONSTRAINT run_attempts_completion_check CHECK ((state = 'active') = (completed_at IS NULL));
