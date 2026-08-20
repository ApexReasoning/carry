CREATE TABLE google_identities (
    issuer text NOT NULL CHECK (issuer = 'https://accounts.google.com'),
    subject text NOT NULL CHECK (
        subject <> '' AND octet_length(subject) <= 255
    ),
    user_id uuid NOT NULL UNIQUE REFERENCES carry_users(user_id),
    verified_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    PRIMARY KEY (issuer, subject)
);

CREATE TABLE github_identities (
    github_user_id bigint PRIMARY KEY CHECK (github_user_id > 0),
    user_id uuid NOT NULL UNIQUE REFERENCES carry_users(user_id),
    verified_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE external_login_transactions (
    transaction_id uuid PRIMARY KEY,
    provider text NOT NULL CHECK (provider IN ('google', 'github')),
    status text NOT NULL DEFAULT 'prepared' CHECK (
        status IN ('prepared', 'exchanging', 'succeeded', 'denied', 'unknown')
    ),
    callback_digest bytea CHECK (
        callback_digest IS NULL OR octet_length(callback_digest) = 32
    ),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    user_id uuid REFERENCES carry_users(user_id),
    browser_session_id uuid REFERENCES browser_sessions(session_id),
    CHECK (expires_at > created_at),
    CHECK (
        (status = 'prepared' AND callback_digest IS NULL)
        OR (status <> 'prepared' AND callback_digest IS NOT NULL)
    ),
    CHECK (
        (status = 'succeeded'
            AND completed_at IS NOT NULL
            AND user_id IS NOT NULL
            AND browser_session_id IS NOT NULL)
        OR (status <> 'succeeded'
            AND user_id IS NULL
            AND browser_session_id IS NULL)
    ),
    CHECK (
        (status IN ('succeeded', 'denied', 'unknown')) = (completed_at IS NOT NULL)
    )
);
