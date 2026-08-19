ALTER TABLE works
    ADD COLUMN applied_input_seq bigint NOT NULL DEFAULT 0 CHECK (applied_input_seq >= 0),
    ADD COLUMN current_revision bigint NOT NULL DEFAULT 0 CHECK (current_revision >= 0),
    ADD CONSTRAINT works_applied_input_bound CHECK (applied_input_seq <= input_head_seq);

CREATE TABLE coordinator_runs (
    run_id uuid PRIMARY KEY,
    work_id uuid NOT NULL REFERENCES works(work_id),
    input_start_seq bigint NOT NULL CHECK (input_start_seq > 0),
    input_end_seq bigint NOT NULL CHECK (input_end_seq >= input_start_seq),
    base_revision bigint NOT NULL CHECK (base_revision >= 0),
    writer_token uuid NOT NULL UNIQUE,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'active', 'succeeded', 'failed', 'unknown')),
    current_fence bigint NOT NULL DEFAULT 0 CHECK (current_fence >= 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    UNIQUE (run_id, work_id),
    CHECK ((state = 'succeeded') = (completed_at IS NOT NULL))
);

CREATE UNIQUE INDEX coordinator_runs_unresolved_work_idx
    ON coordinator_runs (work_id)
    WHERE state IN ('pending', 'active', 'failed', 'unknown');

CREATE INDEX coordinator_runs_pending_idx
    ON coordinator_runs (created_at, run_id)
    WHERE state = 'pending';

CREATE TABLE run_attempts (
    attempt_id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES coordinator_runs(run_id),
    machine_id uuid NOT NULL REFERENCES machines(machine_id),
    fence bigint NOT NULL CHECK (fence > 0),
    agent_credential_digest bytea NOT NULL UNIQUE CHECK (octet_length(agent_credential_digest) = 32),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'succeeded', 'failed', 'unknown')),
    lease_expires_at timestamptz NOT NULL,
    claimed_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    completed_at timestamptz,
    UNIQUE (run_id, fence),
    CHECK ((state = 'active') = (completed_at IS NULL)),
    CHECK (lease_expires_at > claimed_at)
);

CREATE UNIQUE INDEX run_attempts_active_run_idx
    ON run_attempts (run_id)
    WHERE state = 'active';

CREATE TABLE work_understanding_revisions (
    work_id uuid NOT NULL REFERENCES works(work_id),
    revision bigint NOT NULL CHECK (revision > 0),
    source_run_id uuid NOT NULL,
    understanding text NOT NULL CHECK (btrim(understanding) <> '' AND octet_length(understanding) <= 61440),
    next_step text NOT NULL CHECK (btrim(next_step) <> '' AND octet_length(next_step) <= 8192),
    applied_input_seq bigint NOT NULL CHECK (applied_input_seq > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (work_id, revision),
    FOREIGN KEY (source_run_id, work_id) REFERENCES coordinator_runs(run_id, work_id),
    UNIQUE (source_run_id)
);
