CREATE TABLE works (
    work_id uuid PRIMARY KEY,
    space_id uuid NOT NULL REFERENCES spaces(space_id),
    goal text NOT NULL CHECK (btrim(goal) <> '' AND octet_length(goal) <= 2000),
    lifecycle text NOT NULL DEFAULT 'open' CHECK (lifecycle IN ('open', 'paused', 'closed')),
    owner_user_id uuid NOT NULL,
    creator_user_id uuid NOT NULL REFERENCES carry_users(user_id),
    input_head_seq bigint NOT NULL DEFAULT 1 CHECK (input_head_seq >= 1),
    create_idempotency_key text NOT NULL CHECK (
        create_idempotency_key <> '' AND octet_length(create_idempotency_key) <= 255
    ),
    create_request_digest bytea NOT NULL CHECK (octet_length(create_request_digest) = 32),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    FOREIGN KEY (space_id, owner_user_id) REFERENCES space_memberships(space_id, user_id),
    UNIQUE (space_id, creator_user_id, create_idempotency_key)
);

CREATE INDEX works_space_created_idx
    ON works (space_id, created_at DESC, work_id DESC);

CREATE TABLE work_messages (
    message_id uuid PRIMARY KEY,
    work_id uuid NOT NULL REFERENCES works(work_id),
    author_user_id uuid NOT NULL REFERENCES carry_users(user_id),
    text text NOT NULL CHECK (btrim(text) <> '' AND octet_length(text) <= 61440),
    input_seq bigint NOT NULL CHECK (input_seq > 1),
    idempotency_key text NOT NULL CHECK (
        idempotency_key <> '' AND octet_length(idempotency_key) <= 255
    ),
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    UNIQUE (work_id, input_seq),
    UNIQUE (work_id, author_user_id, idempotency_key)
);

ALTER TABLE user_tokens
    ADD CONSTRAINT user_tokens_token_user_unique UNIQUE (token_id, user_id);

CREATE TABLE browser_sessions (
    session_digest bytea PRIMARY KEY CHECK (octet_length(session_digest) = 32),
    user_id uuid NOT NULL REFERENCES carry_users(user_id),
    source_token_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    FOREIGN KEY (source_token_id, user_id) REFERENCES user_tokens(token_id, user_id),
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX browser_sessions_user_idx ON browser_sessions (user_id, expires_at);
