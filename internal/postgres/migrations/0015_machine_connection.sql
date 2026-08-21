ALTER TABLE machines
    DROP COLUMN enrollment_idempotency_key CASCADE,
    ADD COLUMN revocation_actor_kind text,
    ADD COLUMN revoked_by_user_id uuid REFERENCES carry_users(user_id),
    ADD COLUMN revocation_idempotency_key text,
    ADD COLUMN revocation_request_digest bytea;

UPDATE machines
SET revocation_actor_kind = 'not_recorded'
WHERE revoked_at IS NOT NULL;

ALTER TABLE machines
    ADD CONSTRAINT machines_revocation_facts_check CHECK (
        (revoked_at IS NULL
            AND revocation_actor_kind IS NULL
            AND revoked_by_user_id IS NULL
            AND revocation_idempotency_key IS NULL
            AND revocation_request_digest IS NULL)
        OR
        (revoked_at IS NOT NULL AND (
            (revocation_actor_kind = 'not_recorded'
                AND revoked_by_user_id IS NULL
                AND revocation_idempotency_key IS NULL
                AND revocation_request_digest IS NULL)
            OR
            (revocation_actor_kind = 'user'
                AND revoked_by_user_id IS NOT NULL
                AND revocation_idempotency_key IS NOT NULL
                AND revocation_idempotency_key <> ''
                AND octet_length(revocation_idempotency_key) <= 255
                AND revocation_request_digest IS NOT NULL
                AND octet_length(revocation_request_digest) = 32)
            OR
            (revocation_actor_kind = 'machine'
                AND revoked_by_user_id IS NULL
                AND revocation_idempotency_key IS NOT NULL
                AND revocation_idempotency_key <> ''
                AND octet_length(revocation_idempotency_key) <= 255
                AND revocation_request_digest IS NOT NULL
                AND octet_length(revocation_request_digest) = 32)
        ))
    );

CREATE UNIQUE INDEX machines_user_revocation_idempotency_idx
    ON machines (revoked_by_user_id, revocation_idempotency_key)
    WHERE revocation_actor_kind = 'user';

CREATE TABLE machine_connection_requests (
    request_id uuid PRIMARY KEY,
    begin_idempotency_key text NOT NULL UNIQUE CHECK (
        begin_idempotency_key <> '' AND octet_length(begin_idempotency_key) <= 255
    ),
    begin_request_digest bytea NOT NULL CHECK (octet_length(begin_request_digest) = 32),
    source_digest bytea NOT NULL CHECK (octet_length(source_digest) = 32),
    user_code_digest bytea NOT NULL UNIQUE CHECK (octet_length(user_code_digest) = 32),
    poll_secret_digest bytea NOT NULL UNIQUE CHECK (octet_length(poll_secret_digest) = 32),
    display_name text NOT NULL CHECK (btrim(display_name) <> '' AND octet_length(display_name) <= 128),
    public_key_der bytea NOT NULL CHECK (octet_length(public_key_der) > 0),
    key_proof bytea NOT NULL CHECK (octet_length(key_proof) = 64),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (transaction_timestamp() + interval '15 minutes'),
    last_polled_at timestamptz,
    poll_interval_seconds smallint NOT NULL DEFAULT 5 CHECK (
        poll_interval_seconds >= 5 AND poll_interval_seconds <= 30
    ),
    decision text CHECK (decision IN ('approved', 'denied')),
    decided_at timestamptz,
    decided_by_user_id uuid REFERENCES carry_users(user_id),
    decided_space_id uuid REFERENCES spaces(space_id),
    decision_idempotency_key text,
    decision_request_digest bytea,
    prepared_machine_id uuid,
    cancelled_at timestamptz,
    resulting_machine_id uuid REFERENCES machines(machine_id),
    redeemed_at timestamptz,
    replay_until timestamptz,
    CHECK (expires_at > created_at),
    CHECK (last_polled_at IS NULL OR last_polled_at >= created_at),
    CHECK (
        (decision IS NULL AND decided_at IS NULL AND decided_by_user_id IS NULL
            AND decided_space_id IS NULL AND decision_idempotency_key IS NULL
            AND decision_request_digest IS NULL AND prepared_machine_id IS NULL)
        OR
        (decision = 'approved' AND decided_at IS NOT NULL AND decided_by_user_id IS NOT NULL
            AND decided_space_id IS NOT NULL AND decision_idempotency_key IS NOT NULL
            AND decision_idempotency_key <> '' AND octet_length(decision_idempotency_key) <= 255
            AND decision_request_digest IS NOT NULL AND octet_length(decision_request_digest) = 32
            AND prepared_machine_id IS NOT NULL)
        OR
        (decision = 'denied' AND decided_at IS NOT NULL AND decided_by_user_id IS NOT NULL
            AND decided_space_id IS NULL AND decision_idempotency_key IS NOT NULL
            AND decision_idempotency_key <> '' AND octet_length(decision_idempotency_key) <= 255
            AND decision_request_digest IS NOT NULL AND octet_length(decision_request_digest) = 32
            AND prepared_machine_id IS NULL)
    ),
    CHECK (cancelled_at IS NULL OR decision IS NULL),
    CHECK (
        (redeemed_at IS NULL AND resulting_machine_id IS NULL AND replay_until IS NULL)
        OR
        (redeemed_at IS NOT NULL AND decision = 'approved' AND resulting_machine_id IS NOT NULL
            AND replay_until IS NOT NULL AND replay_until > redeemed_at)
    )
);

CREATE INDEX machine_connection_requests_live_source_idx
    ON machine_connection_requests (source_digest, expires_at)
    WHERE decision IS NULL AND cancelled_at IS NULL;

CREATE UNIQUE INDEX machine_connection_requests_decision_idempotency_idx
    ON machine_connection_requests (decided_by_user_id, decision_idempotency_key)
    WHERE decision IS NOT NULL;

CREATE TABLE machine_connection_lookup_failures (
    failure_id uuid PRIMARY KEY,
    browser_session_id uuid NOT NULL REFERENCES browser_sessions(session_id),
    source_digest bytea NOT NULL CHECK (octet_length(source_digest) = 32),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE INDEX machine_connection_lookup_failures_session_time_idx
    ON machine_connection_lookup_failures (browser_session_id, created_at DESC);
CREATE INDEX machine_connection_lookup_failures_source_time_idx
    ON machine_connection_lookup_failures (source_digest, created_at DESC);
