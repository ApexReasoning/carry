CREATE TABLE carry_users (
    user_id uuid PRIMARY KEY,
    display_name text NOT NULL CHECK (display_name <> ''),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE spaces (
    space_id uuid PRIMARY KEY,
    name text NOT NULL CHECK (name <> ''),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE space_memberships (
    space_id uuid NOT NULL REFERENCES spaces(space_id),
    user_id uuid NOT NULL REFERENCES carry_users(user_id),
    can_enroll_machines boolean NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    revoked_at timestamptz,
    PRIMARY KEY (space_id, user_id),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE TABLE user_tokens (
    token_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES carry_users(user_id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE TABLE machines (
    machine_id uuid PRIMARY KEY,
    space_id uuid NOT NULL REFERENCES spaces(space_id),
    display_name text NOT NULL CHECK (display_name <> ''),
    public_key_der bytea NOT NULL CHECK (octet_length(public_key_der) > 0),
    certificate_pem bytea NOT NULL CHECK (octet_length(certificate_pem) > 0),
    certificate_serial text NOT NULL UNIQUE CHECK (certificate_serial <> ''),
    enrolled_by_user_id uuid NOT NULL REFERENCES carry_users(user_id),
    enrollment_idempotency_key text NOT NULL CHECK (enrollment_idempotency_key <> ''),
    enrolled_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    revoked_at timestamptz,
    UNIQUE (space_id, enrolled_by_user_id, enrollment_idempotency_key),
    CHECK (revoked_at IS NULL OR revoked_at >= enrolled_at)
);

CREATE TABLE machine_runtime_observations (
    machine_id uuid NOT NULL REFERENCES machines(machine_id),
    runtime_kind text NOT NULL CHECK (runtime_kind IN ('pi', 'codex')),
    detection text NOT NULL CHECK (detection IN ('detected', 'not_found', 'probe_failed')),
    executable text,
    version text,
    diagnostic_code text,
    diagnostic_detail text,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (machine_id, runtime_kind),
    CHECK ((detection = 'detected') = (executable IS NOT NULL AND version IS NOT NULL))
);
