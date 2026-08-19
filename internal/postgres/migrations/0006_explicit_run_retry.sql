ALTER TABLE runs
    ADD COLUMN retry_requested_at timestamptz,
    ADD COLUMN retry_requested_by_user_id uuid REFERENCES carry_users(user_id),
    ADD COLUMN retry_idempotency_key text,
    ADD CONSTRAINT runs_retry_fields_together CHECK (
        (retry_requested_at IS NULL AND retry_requested_by_user_id IS NULL AND retry_idempotency_key IS NULL)
        OR (
            retry_requested_at IS NOT NULL
            AND retry_requested_by_user_id IS NOT NULL
            AND retry_idempotency_key IS NOT NULL
        )
    ),
    ADD CONSTRAINT runs_retry_terminal_only CHECK (
        retry_requested_at IS NULL OR state IN ('failed', 'unknown')
    ),
    ADD CONSTRAINT runs_retry_key_length CHECK (
        retry_idempotency_key IS NULL
        OR (btrim(retry_idempotency_key) <> '' AND octet_length(retry_idempotency_key) <= 255)
    ),
    ADD CONSTRAINT runs_retry_after_completion CHECK (
        retry_requested_at IS NULL OR retry_requested_at >= completed_at
    );

DROP INDEX runs_unresolved_work_idx;

CREATE UNIQUE INDEX runs_unresolved_work_idx
    ON runs (work_id)
    WHERE state = 'active'
       OR (state IN ('failed', 'unknown') AND retry_requested_at IS NULL);

CREATE UNIQUE INDEX runs_retry_idempotency_idx
    ON runs (work_id, retry_idempotency_key)
    WHERE retry_idempotency_key IS NOT NULL;
