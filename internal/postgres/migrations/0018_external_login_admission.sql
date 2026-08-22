ALTER TABLE external_login_transactions
ADD COLUMN source_digest bytea;

UPDATE external_login_transactions
SET source_digest = decode(repeat('00', 32), 'hex')
WHERE source_digest IS NULL;

ALTER TABLE external_login_transactions
ALTER COLUMN source_digest SET NOT NULL,
ADD CONSTRAINT external_login_source_digest_check CHECK (
    octet_length(source_digest) = 32
);

CREATE INDEX external_login_expiry_idx
ON external_login_transactions (expires_at);

CREATE INDEX external_login_live_source_idx
ON external_login_transactions (source_digest, expires_at)
WHERE status IN ('prepared', 'exchanging');
