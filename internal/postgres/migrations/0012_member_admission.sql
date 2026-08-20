CREATE TABLE space_invitations (
    invitation_id uuid PRIMARY KEY,
    space_id uuid NOT NULL REFERENCES spaces(space_id),
    recipient_email text NOT NULL CHECK (
        recipient_email = lower(btrim(recipient_email))
        AND recipient_email <> ''
        AND octet_length(recipient_email) <= 254
    ),
    inviter_user_id uuid NOT NULL REFERENCES carry_users(user_id),
    can_manage_members boolean NOT NULL,
    can_enroll_machines boolean NOT NULL,
    issue_idempotency_key text NOT NULL CHECK (
        issue_idempotency_key <> '' AND octet_length(issue_idempotency_key) <= 255
    ),
    issue_request_digest bytea NOT NULL CHECK (octet_length(issue_request_digest) = 32),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (transaction_timestamp() + interval '7 days'),
    accepted_by_user_id uuid REFERENCES carry_users(user_id),
    accepted_at timestamptz,
    accept_result text CHECK (accept_result IN ('joined', 'already_member')),
    accept_idempotency_key text,
    accept_request_digest bytea,
    revoked_by_user_id uuid REFERENCES carry_users(user_id),
    revoked_at timestamptz,
    revoke_idempotency_key text,
    revoke_request_digest bytea,
    UNIQUE (space_id, inviter_user_id, issue_idempotency_key),
    CHECK (expires_at = created_at + interval '7 days'),
    CHECK (
        (accepted_at IS NULL AND accepted_by_user_id IS NULL AND accept_result IS NULL
            AND accept_idempotency_key IS NULL AND accept_request_digest IS NULL)
        OR
        (accepted_at IS NOT NULL AND accepted_by_user_id IS NOT NULL AND accept_result IS NOT NULL
            AND accept_idempotency_key IS NOT NULL AND accept_idempotency_key <> ''
            AND octet_length(accept_idempotency_key) <= 255
            AND accept_request_digest IS NOT NULL AND octet_length(accept_request_digest) = 32)
    ),
    CHECK (
        (revoked_at IS NULL AND revoked_by_user_id IS NULL
            AND revoke_idempotency_key IS NULL AND revoke_request_digest IS NULL)
        OR
        (revoked_at IS NOT NULL AND revoked_by_user_id IS NOT NULL
            AND revoke_idempotency_key IS NOT NULL AND revoke_idempotency_key <> ''
            AND octet_length(revoke_idempotency_key) <= 255
            AND revoke_request_digest IS NOT NULL AND octet_length(revoke_request_digest) = 32)
    ),
    CHECK (accepted_at IS NULL OR revoked_at IS NULL),
    CHECK (accepted_at IS NULL OR accepted_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX space_invitations_recipient_current_idx
    ON space_invitations (recipient_email, expires_at, invitation_id)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX space_invitations_space_current_idx
    ON space_invitations (space_id, expires_at, invitation_id)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE space_invitation_submissions (
    submission_id uuid PRIMARY KEY,
    invitation_id uuid NOT NULL REFERENCES space_invitations(invitation_id),
    requested_by_user_id uuid NOT NULL REFERENCES carry_users(user_id),
    idempotency_key text NOT NULL CHECK (
        idempotency_key <> '' AND octet_length(idempotency_key) <= 255
    ),
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    recipient_email text NOT NULL CHECK (
        recipient_email = lower(btrim(recipient_email))
        AND recipient_email <> ''
        AND octet_length(recipient_email) <= 254
    ),
    payload_digest bytea NOT NULL CHECK (octet_length(payload_digest) = 32),
    provider_idempotency_key text NOT NULL UNIQUE CHECK (
        provider_idempotency_key <> '' AND octet_length(provider_idempotency_key) <= 255
    ),
    state text NOT NULL DEFAULT 'prepared' CHECK (state IN ('prepared', 'accepted', 'rejected', 'unknown')),
    provider_message_id text,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    recorded_at timestamptz,
    UNIQUE (invitation_id, requested_by_user_id, idempotency_key),
    CHECK (
        (state = 'prepared' AND provider_message_id IS NULL AND recorded_at IS NULL)
        OR (state = 'accepted' AND provider_message_id IS NOT NULL
            AND btrim(provider_message_id) <> '' AND octet_length(provider_message_id) <= 255
            AND recorded_at IS NOT NULL)
        OR (state IN ('rejected', 'unknown') AND provider_message_id IS NULL AND recorded_at IS NOT NULL)
    )
);

CREATE INDEX space_invitation_submissions_invitation_idx
    ON space_invitation_submissions (invitation_id, created_at DESC, submission_id DESC);
