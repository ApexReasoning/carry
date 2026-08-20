CREATE TABLE work_result_checks (
    review_id uuid PRIMARY KEY,
    work_id uuid NOT NULL REFERENCES works(work_id),
    understanding_version bigint NOT NULL CHECK (understanding_version > 0),
    content_digest bytea NOT NULL CHECK (octet_length(content_digest) = 32),
    requested_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    accepted_by_user_id uuid REFERENCES carry_users(user_id),
    accept_idempotency_key text,
    accept_request_digest bytea,
    accepted_at timestamptz,
    UNIQUE (work_id, understanding_version),
    UNIQUE (work_id, accepted_by_user_id, accept_idempotency_key),
    CHECK (
        (
            accepted_by_user_id IS NULL
            AND accept_idempotency_key IS NULL
            AND accept_request_digest IS NULL
            AND accepted_at IS NULL
        )
        OR (
            accepted_by_user_id IS NOT NULL
            AND accept_idempotency_key IS NOT NULL
            AND btrim(accept_idempotency_key) <> ''
            AND octet_length(accept_idempotency_key) <= 255
            AND octet_length(accept_request_digest) = 32
            AND accepted_at IS NOT NULL
            AND accepted_at >= requested_at
        )
    )
);

CREATE INDEX work_result_checks_current_idx
    ON work_result_checks (work_id, understanding_version)
    WHERE accepted_at IS NULL;
