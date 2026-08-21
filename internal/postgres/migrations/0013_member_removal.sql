ALTER TABLE space_memberships
    ADD COLUMN removed_by_user_id uuid REFERENCES carry_users(user_id),
    ADD COLUMN removal_successor_user_id uuid,
    ADD COLUMN removal_idempotency_key text,
    ADD COLUMN removal_request_digest bytea,
    ADD CONSTRAINT space_memberships_removal_successor_fkey
        FOREIGN KEY (space_id, removal_successor_user_id)
        REFERENCES space_memberships(space_id, user_id),
    ADD CONSTRAINT space_memberships_removal_replay_shape_check CHECK (
        (removed_by_user_id IS NULL
            AND removal_successor_user_id IS NULL
            AND removal_idempotency_key IS NULL
            AND removal_request_digest IS NULL)
        OR
        (revoked_at IS NOT NULL
            AND removed_by_user_id IS NOT NULL
            AND removal_idempotency_key IS NOT NULL
            AND removal_idempotency_key <> ''
            AND octet_length(removal_idempotency_key) <= 255
            AND removal_request_digest IS NOT NULL
            AND octet_length(removal_request_digest) = 32
            AND (removal_successor_user_id IS NULL OR removal_successor_user_id <> user_id))
    );

CREATE UNIQUE INDEX space_memberships_removal_replay_key
    ON space_memberships (space_id, removed_by_user_id, removal_idempotency_key)
    WHERE removal_idempotency_key IS NOT NULL;

CREATE INDEX works_open_owner_idx
    ON works (space_id, owner_user_id, work_id)
    WHERE lifecycle = 'open';
