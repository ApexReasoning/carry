ALTER TABLE browser_sessions
    ADD COLUMN identity_proved_at timestamptz,
    ADD COLUMN identity_proof_method text;
UPDATE browser_sessions
SET
    identity_proved_at = created_at,
    identity_proof_method = CASE
        WHEN EXISTS (
            SELECT 1 FROM email_identities
            WHERE email_identities.user_id = browser_sessions.user_id
        ) THEN 'email'
        WHEN EXISTS (
            SELECT 1 FROM google_identities
            WHERE google_identities.user_id = browser_sessions.user_id
        ) THEN 'google'
        WHEN EXISTS (
            SELECT 1 FROM github_identities
            WHERE github_identities.user_id = browser_sessions.user_id
        ) THEN 'github'
    END;
ALTER TABLE browser_sessions
    ALTER COLUMN identity_proved_at SET NOT NULL,
    ALTER COLUMN identity_proved_at SET DEFAULT transaction_timestamp(),
    ALTER COLUMN identity_proof_method SET NOT NULL,
    ADD CONSTRAINT browser_sessions_identity_proof_time_check CHECK (
        identity_proved_at >= created_at
    ),
    ADD CONSTRAINT browser_sessions_identity_proof_method_check CHECK (
        identity_proof_method IN ('email', 'google', 'github')
    );

ALTER TABLE email_login_challenges
    ADD COLUMN purpose text NOT NULL DEFAULT 'login' CHECK (
        purpose IN ('login', 'reauthenticate', 'link')
    ),
    ADD COLUMN target_user_id uuid REFERENCES carry_users(user_id),
    ADD COLUMN initiating_session_id uuid REFERENCES browser_sessions(session_id),
    ADD CONSTRAINT email_challenge_purpose_target_check CHECK (
        (purpose = 'login' AND target_user_id IS NULL AND initiating_session_id IS NULL)
        OR
        (purpose IN ('reauthenticate', 'link')
            AND target_user_id IS NOT NULL
            AND initiating_session_id IS NOT NULL)
    );

DO $$
DECLARE
    existing_constraint text;
BEGIN
    FOR existing_constraint IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'external_login_transactions'::regclass
            AND contype = 'c'
            AND pg_get_constraintdef(oid) LIKE '%status%'
    LOOP
        EXECUTE format(
            'ALTER TABLE external_login_transactions DROP CONSTRAINT %I',
            existing_constraint
        );
    END LOOP;
END $$;

ALTER TABLE external_login_transactions
    ADD CONSTRAINT external_proof_status_check CHECK (
        status IN ('prepared', 'exchanging', 'succeeded', 'denied', 'rejected', 'unknown')
    ),
    ADD CONSTRAINT external_proof_callback_check CHECK (
        (status = 'prepared' AND callback_digest IS NULL)
        OR (status <> 'prepared' AND callback_digest IS NOT NULL)
    ),
    ADD CONSTRAINT external_proof_result_check CHECK (
        (status = 'succeeded'
            AND completed_at IS NOT NULL
            AND user_id IS NOT NULL
            AND browser_session_id IS NOT NULL)
        OR (status <> 'succeeded'
            AND user_id IS NULL
            AND browser_session_id IS NULL)
    ),
    ADD CONSTRAINT external_proof_completion_check CHECK (
        (status IN ('succeeded', 'denied', 'rejected', 'unknown'))
            = (completed_at IS NOT NULL)
    ),
    ADD COLUMN purpose text NOT NULL DEFAULT 'login' CHECK (
        purpose IN ('login', 'reauthenticate', 'link')
    ),
    ADD COLUMN target_user_id uuid REFERENCES carry_users(user_id),
    ADD COLUMN initiating_session_id uuid REFERENCES browser_sessions(session_id),
    ADD CONSTRAINT external_login_purpose_target_check CHECK (
        (purpose = 'login' AND target_user_id IS NULL AND initiating_session_id IS NULL)
        OR
        (purpose IN ('reauthenticate', 'link')
            AND target_user_id IS NOT NULL
            AND initiating_session_id IS NOT NULL)
    );

CREATE TABLE identity_method_unlinks (
    user_id uuid NOT NULL REFERENCES carry_users(user_id),
    initiating_session_id uuid NOT NULL REFERENCES browser_sessions(session_id),
    method text NOT NULL CHECK (method IN ('email', 'google', 'github')),
    idempotency_key text NOT NULL CHECK (
        idempotency_key <> '' AND octet_length(idempotency_key) <= 255
    ),
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    replacement_session_id uuid NOT NULL REFERENCES browser_sessions(session_id),
    completed_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (initiating_session_id, idempotency_key)
);
