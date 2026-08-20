ALTER TABLE carry_users
    ALTER COLUMN display_name DROP NOT NULL;
ALTER TABLE carry_users
    DROP CONSTRAINT carry_users_display_name_check;
ALTER TABLE carry_users
    ADD CONSTRAINT carry_users_display_name_check CHECK (
        display_name IS NULL OR btrim(display_name) <> ''
    );

ALTER TABLE space_memberships
    ADD COLUMN can_manage_members boolean NOT NULL DEFAULT false;
UPDATE space_memberships
SET can_manage_members = true
WHERE revoked_at IS NULL;

ALTER TABLE spaces
    ADD COLUMN created_by_user_id uuid REFERENCES carry_users(user_id),
    ADD COLUMN create_idempotency_key text,
    ADD COLUMN create_request_digest bytea,
    ADD CONSTRAINT spaces_create_request_complete CHECK (
        (created_by_user_id IS NULL AND create_idempotency_key IS NULL AND create_request_digest IS NULL)
        OR
        (created_by_user_id IS NOT NULL
            AND create_idempotency_key IS NOT NULL
            AND create_idempotency_key <> ''
            AND octet_length(create_idempotency_key) <= 255
            AND create_request_digest IS NOT NULL
            AND octet_length(create_request_digest) = 32)
    ),
    ADD CONSTRAINT spaces_creator_request_unique UNIQUE (
        created_by_user_id,
        create_idempotency_key
    );

DELETE FROM browser_sessions;
ALTER TABLE browser_sessions
    DROP CONSTRAINT browser_sessions_source_token_id_user_id_fkey,
    DROP CONSTRAINT browser_sessions_pkey,
    DROP COLUMN session_digest,
    DROP COLUMN source_token_id,
    ADD COLUMN session_id uuid PRIMARY KEY;

CREATE TABLE email_identities (
    canonical_email text PRIMARY KEY CHECK (
        canonical_email = lower(btrim(canonical_email))
        AND canonical_email <> ''
        AND octet_length(canonical_email) <= 254
    ),
    user_id uuid NOT NULL UNIQUE REFERENCES carry_users(user_id),
    verified_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE email_login_challenges (
    challenge_id uuid PRIMARY KEY,
    canonical_email text NOT NULL CHECK (
        canonical_email = lower(btrim(canonical_email))
        AND canonical_email <> ''
        AND octet_length(canonical_email) <= 254
    ),
    code_digest bytea NOT NULL CHECK (octet_length(code_digest) = 32),
    source_digest bytea NOT NULL CHECK (octet_length(source_digest) = 32),
    payload_digest bytea NOT NULL CHECK (octet_length(payload_digest) = 32),
    request_idempotency_key text NOT NULL UNIQUE CHECK (
        request_idempotency_key <> ''
        AND octet_length(request_idempotency_key) <= 255
    ),
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    submission_state text NOT NULL DEFAULT 'prepared' CHECK (
        submission_state IN ('prepared', 'accepted', 'rejected', 'unknown')
    ),
    provider_message_id text,
    attempts_used integer NOT NULL DEFAULT 0 CHECK (
        attempts_used >= 0 AND attempts_used <= 5
    ),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    invalidated_at timestamptz,
    consumed_at timestamptz,
    user_id uuid REFERENCES carry_users(user_id),
    browser_session_id uuid REFERENCES browser_sessions(session_id),
    CHECK (expires_at > created_at),
    CHECK (invalidated_at IS NULL OR invalidated_at >= created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK ((submission_state = 'accepted') = (provider_message_id IS NOT NULL)),
    CHECK (
        (consumed_at IS NULL AND user_id IS NULL AND browser_session_id IS NULL)
        OR
        (consumed_at IS NOT NULL AND user_id IS NOT NULL AND browser_session_id IS NOT NULL)
    )
);

CREATE INDEX email_login_challenges_email_created_idx
    ON email_login_challenges (canonical_email, created_at DESC);
CREATE INDEX email_login_challenges_source_created_idx
    ON email_login_challenges (source_digest, created_at DESC);
CREATE UNIQUE INDEX email_login_challenges_current_email_idx
    ON email_login_challenges (canonical_email)
    WHERE invalidated_at IS NULL AND consumed_at IS NULL;

CREATE TABLE email_login_attempts (
    challenge_id uuid NOT NULL REFERENCES email_login_challenges(challenge_id),
    idempotency_key text NOT NULL CHECK (
        idempotency_key <> '' AND octet_length(idempotency_key) <= 255
    ),
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    result text NOT NULL CHECK (result IN ('invalid', 'succeeded')),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (challenge_id, idempotency_key)
);
