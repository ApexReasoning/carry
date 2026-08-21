-- name: ExternalLoginDatabaseTime :one
SELECT transaction_timestamp()::timestamptz;

-- name: CreateExternalLogin :one
INSERT INTO external_login_transactions (
    transaction_id,
    provider,
    purpose,
    target_user_id,
    initiating_session_id,
    invitation_id,
    expires_at
) VALUES (
    sqlc.arg(transaction_id),
    sqlc.arg(provider),
    sqlc.arg(purpose),
    sqlc.narg(target_user_id),
    sqlc.narg(initiating_session_id),
    sqlc.narg(invitation_id),
    transaction_timestamp() + interval '10 minutes'
)
RETURNING expires_at;

-- name: LockExternalLogin :one
SELECT *
FROM external_login_transactions
WHERE transaction_id = sqlc.arg(transaction_id)
FOR UPDATE;

-- name: ClaimExternalLoginExchange :execrows
UPDATE external_login_transactions
SET
    status = 'exchanging',
    callback_digest = sqlc.arg(callback_digest)
WHERE transaction_id = sqlc.arg(transaction_id)
    AND provider = sqlc.arg(provider)
    AND status = 'prepared'
    AND expires_at > transaction_timestamp();

-- name: FinishExternalLoginWithoutAuthority :execrows
UPDATE external_login_transactions
SET
    status = sqlc.arg(status),
    callback_digest = sqlc.arg(callback_digest),
    completed_at = transaction_timestamp()
WHERE transaction_id = sqlc.arg(transaction_id)
    AND provider = sqlc.arg(provider)
    AND status = 'prepared'
    AND expires_at > transaction_timestamp();

-- name: RejectExternalLogin :execrows
UPDATE external_login_transactions
SET
    status = 'rejected',
    completed_at = transaction_timestamp()
WHERE transaction_id = sqlc.arg(transaction_id)
    AND provider = sqlc.arg(provider)
    AND status = 'exchanging'
    AND callback_digest = sqlc.arg(callback_digest);

-- name: MarkExternalLoginUnknown :execrows
UPDATE external_login_transactions
SET
    status = 'unknown',
    completed_at = transaction_timestamp()
WHERE transaction_id = sqlc.arg(transaction_id)
    AND provider = sqlc.arg(provider)
    AND status = 'exchanging'
    AND callback_digest = sqlc.arg(callback_digest);

-- name: LockGoogleIdentityKey :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(issuer)::text || ':' || sqlc.arg(subject)::text, 7)
);

-- name: LoadGoogleIdentity :one
SELECT user_id
FROM google_identities
WHERE issuer = sqlc.arg(issuer) AND subject = sqlc.arg(subject)
FOR UPDATE;

-- name: CreateGoogleIdentity :exec
INSERT INTO google_identities (issuer, subject, user_id)
VALUES (sqlc.arg(issuer), sqlc.arg(subject), sqlc.arg(user_id));

-- name: LockGitHubIdentityKey :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(github_user_id)::bigint::text, 8)
);

-- name: LoadGitHubIdentity :one
SELECT user_id
FROM github_identities
WHERE github_user_id = sqlc.arg(github_user_id)
FOR UPDATE;

-- name: CreateGitHubIdentity :exec
INSERT INTO github_identities (github_user_id, user_id)
VALUES (sqlc.arg(github_user_id), sqlc.arg(user_id));

-- name: CreateExternalLoginUser :exec
INSERT INTO carry_users (user_id, display_name)
VALUES (sqlc.arg(user_id), sqlc.arg(display_name));

-- name: CompleteExternalLogin :execrows
UPDATE external_login_transactions
SET
    status = 'succeeded',
    completed_at = transaction_timestamp(),
    user_id = sqlc.arg(user_id),
    browser_session_id = sqlc.arg(session_id)
WHERE transaction_id = sqlc.arg(transaction_id)
    AND provider = sqlc.arg(provider)
    AND status = 'exchanging'
    AND callback_digest = sqlc.arg(callback_digest)
    AND expires_at > transaction_timestamp();
