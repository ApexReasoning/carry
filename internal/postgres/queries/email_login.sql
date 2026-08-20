-- name: LockEmailRequest :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(request_idempotency_key)::text, 2));

-- name: LockEmailLogin :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(canonical_email)::text, 0));

-- name: LockEmailSource :exec
SELECT pg_advisory_xact_lock(hashtextextended(encode(sqlc.arg(source_digest)::bytea, 'hex'), 1));

-- name: LoadEmailChallengeByRequestKey :one
SELECT *
FROM email_login_challenges
WHERE request_idempotency_key = sqlc.arg(request_idempotency_key);

-- name: LoadLatestEmailChallenge :one
SELECT *
FROM email_login_challenges
WHERE canonical_email = sqlc.arg(canonical_email)
ORDER BY created_at DESC, challenge_id DESC
LIMIT 1;

-- name: CountRecentEmailChallenges :one
SELECT count(*)
FROM email_login_challenges
WHERE canonical_email = sqlc.arg(canonical_email)
    AND created_at > transaction_timestamp() - interval '1 hour';

-- name: CountRecentSourceChallenges :one
SELECT count(*)
FROM email_login_challenges
WHERE source_digest = sqlc.arg(source_digest)
    AND created_at > transaction_timestamp() - interval '1 hour';

-- name: InvalidateCurrentEmailChallenges :exec
UPDATE email_login_challenges
SET invalidated_at = transaction_timestamp()
WHERE canonical_email = sqlc.arg(canonical_email)
    AND invalidated_at IS NULL
    AND consumed_at IS NULL;

-- name: CreateEmailChallenge :one
INSERT INTO email_login_challenges (
    challenge_id,
    canonical_email,
    code_digest,
    source_digest,
    payload_digest,
    request_idempotency_key,
    request_digest,
    purpose,
    target_user_id,
    initiating_session_id,
    expires_at
) VALUES (
    sqlc.arg(challenge_id),
    sqlc.arg(canonical_email),
    sqlc.arg(code_digest),
    sqlc.arg(source_digest),
    sqlc.arg(payload_digest),
    sqlc.arg(request_idempotency_key),
    sqlc.arg(request_digest),
    sqlc.arg(purpose),
    sqlc.narg(target_user_id),
    sqlc.narg(initiating_session_id),
    transaction_timestamp() + interval '5 minutes'
)
RETURNING *;

-- name: RecordEmailSubmission :one
UPDATE email_login_challenges
SET
    submission_state = sqlc.arg(submission_state),
    provider_message_id = sqlc.narg(provider_message_id)
WHERE challenge_id = sqlc.arg(challenge_id)
    AND payload_digest = sqlc.arg(payload_digest)
    AND submission_state IN ('prepared', 'unknown')
RETURNING *;

-- name: LockEmailChallenge :one
SELECT *
FROM email_login_challenges
WHERE challenge_id = sqlc.arg(challenge_id)
FOR UPDATE;

-- name: LoadEmailAttempt :one
SELECT *
FROM email_login_attempts
WHERE challenge_id = sqlc.arg(challenge_id)
    AND idempotency_key = sqlc.arg(idempotency_key);

-- name: RecordInvalidEmailAttempt :exec
INSERT INTO email_login_attempts (
    challenge_id,
    idempotency_key,
    request_digest,
    result
) VALUES (
    sqlc.arg(challenge_id),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_digest),
    'invalid'
);

-- name: SpendEmailChallengeAttempt :execrows
UPDATE email_login_challenges
SET attempts_used = attempts_used + 1
WHERE challenge_id = sqlc.arg(challenge_id)
    AND attempts_used < 5;

-- name: LoadEmailIdentity :one
SELECT user_id
FROM email_identities
WHERE canonical_email = sqlc.arg(canonical_email)
FOR UPDATE;

-- name: CreateEmailUser :exec
INSERT INTO carry_users (user_id, display_name)
VALUES (sqlc.arg(user_id), NULL);

-- name: CreateEmailIdentity :exec
INSERT INTO email_identities (canonical_email, user_id)
VALUES (sqlc.arg(canonical_email), sqlc.arg(user_id));

-- name: ConsumeEmailChallenge :execrows
UPDATE email_login_challenges
SET
    consumed_at = transaction_timestamp(),
    user_id = sqlc.arg(user_id),
    browser_session_id = sqlc.arg(session_id)
WHERE challenge_id = sqlc.arg(challenge_id)
    AND consumed_at IS NULL
    AND invalidated_at IS NULL
    AND expires_at > transaction_timestamp()
    AND attempts_used < 5;

-- name: RecordSuccessfulEmailAttempt :exec
INSERT INTO email_login_attempts (
    challenge_id,
    idempotency_key,
    request_digest,
    result
) VALUES (
    sqlc.arg(challenge_id),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_digest),
    'succeeded'
);

-- name: EmailLoginDatabaseTime :one
SELECT transaction_timestamp()::timestamptz;
