CREATE TABLE cli_login_requests (
    request_id uuid PRIMARY KEY,
    begin_idempotency_key text NOT NULL UNIQUE CHECK (
        begin_idempotency_key <> '' AND octet_length(begin_idempotency_key) <= 255
    ),
    begin_request_digest bytea NOT NULL CHECK (octet_length(begin_request_digest) = 32),
    user_code_digest bytea NOT NULL UNIQUE CHECK (octet_length(user_code_digest) = 32),
    code_generation smallint NOT NULL CHECK (code_generation >= 0 AND code_generation < 5),
    source_digest bytea NOT NULL CHECK (octet_length(source_digest) = 32),
    label text NOT NULL CHECK (btrim(label) <> '' AND octet_length(label) <= 128),
    proposed_replacement_credential_id uuid,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (transaction_timestamp() + interval '15 minutes'),
    last_polled_at timestamptz,
    poll_interval_seconds smallint NOT NULL DEFAULT 5 CHECK (
        poll_interval_seconds >= 5 AND poll_interval_seconds <= 30
    ),
    approved_at timestamptz,
    approved_by_user_id uuid REFERENCES carry_users(user_id),
    approved_space_id uuid REFERENCES spaces(space_id),
    approval_idempotency_key text,
    approval_request_digest bytea,
    prepared_credential_id uuid,
    denied_at timestamptz,
    denied_by_user_id uuid REFERENCES carry_users(user_id),
    denial_idempotency_key text,
    denial_request_digest bytea,
    cancelled_at timestamptz,
    resulting_credential_id uuid,
    redeemed_at timestamptz,
    replay_until timestamptz,
    CHECK (expires_at > created_at),
    CHECK (last_polled_at IS NULL OR last_polled_at >= created_at),
    CHECK (
        (approved_at IS NULL AND approved_by_user_id IS NULL AND approved_space_id IS NULL
            AND approval_idempotency_key IS NULL AND approval_request_digest IS NULL AND prepared_credential_id IS NULL)
        OR
        (approved_at IS NOT NULL AND approved_by_user_id IS NOT NULL AND approved_space_id IS NOT NULL
            AND approval_idempotency_key IS NOT NULL AND approval_idempotency_key <> ''
            AND octet_length(approval_idempotency_key) <= 255
            AND approval_request_digest IS NOT NULL AND octet_length(approval_request_digest) = 32
            AND prepared_credential_id IS NOT NULL)
    ),
    CHECK (
        (denied_at IS NULL AND denied_by_user_id IS NULL AND denial_idempotency_key IS NULL AND denial_request_digest IS NULL)
        OR
        (denied_at IS NOT NULL AND denied_by_user_id IS NOT NULL
            AND denial_idempotency_key IS NOT NULL AND denial_idempotency_key <> ''
            AND octet_length(denial_idempotency_key) <= 255
            AND denial_request_digest IS NOT NULL AND octet_length(denial_request_digest) = 32)
    ),
    CHECK (approved_at IS NULL OR denied_at IS NULL),
    CHECK (denied_at IS NULL OR (cancelled_at IS NULL AND redeemed_at IS NULL)),
    CHECK (cancelled_at IS NULL OR redeemed_at IS NULL),
    CHECK (
        (redeemed_at IS NULL AND resulting_credential_id IS NULL AND replay_until IS NULL)
        OR
        (redeemed_at IS NOT NULL AND approved_at IS NOT NULL AND resulting_credential_id IS NOT NULL
            AND replay_until IS NOT NULL AND replay_until > redeemed_at)
    )
);

CREATE INDEX cli_login_requests_live_source_idx
    ON cli_login_requests (source_digest, expires_at)
    WHERE denied_at IS NULL AND cancelled_at IS NULL AND redeemed_at IS NULL;

CREATE TABLE cli_credentials (
    credential_id uuid PRIMARY KEY,
    login_request_id uuid NOT NULL UNIQUE REFERENCES cli_login_requests(request_id),
    user_id uuid NOT NULL REFERENCES carry_users(user_id),
    label text NOT NULL CHECK (btrim(label) <> '' AND octet_length(label) <= 128),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (transaction_timestamp() + interval '90 days'),
    revoked_at timestamptz,
    revoked_by_user_id uuid REFERENCES carry_users(user_id),
    revocation_idempotency_key text,
    revocation_request_digest bytea,
    CHECK (expires_at > created_at),
    CHECK (
        (revoked_at IS NULL AND revoked_by_user_id IS NULL
            AND revocation_idempotency_key IS NULL AND revocation_request_digest IS NULL)
        OR
        (revoked_at IS NOT NULL AND revoked_by_user_id IS NOT NULL
            AND revocation_idempotency_key IS NOT NULL AND revocation_idempotency_key <> ''
            AND octet_length(revocation_idempotency_key) <= 255
            AND revocation_request_digest IS NOT NULL AND octet_length(revocation_request_digest) = 32)
    )
);

ALTER TABLE cli_login_requests
    ADD CONSTRAINT cli_login_requests_proposed_replacement_fkey
        FOREIGN KEY (proposed_replacement_credential_id) REFERENCES cli_credentials(credential_id),
    ADD CONSTRAINT cli_login_requests_resulting_credential_fkey
        FOREIGN KEY (resulting_credential_id) REFERENCES cli_credentials(credential_id);

CREATE INDEX cli_credentials_active_user_idx
    ON cli_credentials (user_id, created_at DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE cli_login_lookup_failures (
    failure_id uuid PRIMARY KEY,
    browser_session_id uuid NOT NULL REFERENCES browser_sessions(session_id),
    source_digest bytea NOT NULL CHECK (octet_length(source_digest) = 32),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE INDEX cli_login_lookup_failures_session_time_idx
    ON cli_login_lookup_failures (browser_session_id, created_at DESC);
CREATE INDEX cli_login_lookup_failures_source_time_idx
    ON cli_login_lookup_failures (source_digest, created_at DESC);

DROP TABLE user_tokens;
