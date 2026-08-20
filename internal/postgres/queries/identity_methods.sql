-- name: IdentityMethodDatabaseTime :one
SELECT transaction_timestamp()::timestamptz;

-- name: LockUserForIdentityChange :one
SELECT user_id
FROM carry_users
WHERE user_id = sqlc.arg(target_user_id)
FOR UPDATE;

-- name: LockBrowserSessionForIdentityChange :one
SELECT *
FROM browser_sessions
WHERE session_id = sqlc.arg(session_id)
FOR UPDATE;

-- name: LoadEmailMethodForUser :one
SELECT canonical_email
FROM email_identities
WHERE user_id = sqlc.arg(target_user_id);

-- name: LoadGoogleMethodForUser :one
SELECT issuer, subject
FROM google_identities
WHERE user_id = sqlc.arg(target_user_id);

-- name: LoadGitHubMethodForUser :one
SELECT github_user_id
FROM github_identities
WHERE user_id = sqlc.arg(target_user_id);

-- name: LoadIdentityMethods :one
WITH target AS (SELECT sqlc.arg(target_user_id)::uuid AS target_user_id)
SELECT
    EXISTS(SELECT 1 FROM email_identities, target WHERE email_identities.user_id = target.target_user_id) AS has_email,
    EXISTS(SELECT 1 FROM google_identities, target WHERE google_identities.user_id = target.target_user_id) AS has_google,
    EXISTS(SELECT 1 FROM github_identities, target WHERE github_identities.user_id = target.target_user_id) AS has_github;

-- name: CountIdentityMethods :one
WITH target AS (SELECT sqlc.arg(target_user_id)::uuid AS target_user_id)
SELECT
    (SELECT count(*) FROM email_identities, target WHERE email_identities.user_id = target.target_user_id)
    + (SELECT count(*) FROM google_identities, target WHERE google_identities.user_id = target.target_user_id)
    + (SELECT count(*) FROM github_identities, target WHERE github_identities.user_id = target.target_user_id) AS method_count;

-- name: DeleteEmailMethod :execrows
DELETE FROM email_identities
WHERE user_id = sqlc.arg(target_user_id);

-- name: DeleteGoogleMethod :execrows
DELETE FROM google_identities
WHERE user_id = sqlc.arg(target_user_id);

-- name: DeleteGitHubMethod :execrows
DELETE FROM github_identities
WHERE user_id = sqlc.arg(target_user_id);

-- name: RevokeUserBrowserSessions :execrows
UPDATE browser_sessions
SET revoked_at = statement_timestamp()
WHERE user_id = sqlc.arg(target_user_id) AND revoked_at IS NULL;

-- name: RevokeExactBrowserSession :execrows
UPDATE browser_sessions
SET revoked_at = statement_timestamp()
WHERE session_id = sqlc.arg(session_id) AND revoked_at IS NULL;

-- name: LoadIdentityMethodUnlinkReplay :one
SELECT *
FROM identity_method_unlinks
WHERE initiating_session_id = sqlc.arg(initiating_session_id)
    AND idempotency_key = sqlc.arg(idempotency_key);

-- name: RecordIdentityMethodUnlink :exec
INSERT INTO identity_method_unlinks (
    user_id,
    initiating_session_id,
    method,
    idempotency_key,
    request_digest,
    replacement_session_id
) VALUES (
    sqlc.arg(user_id),
    sqlc.arg(initiating_session_id),
    sqlc.arg(method),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_digest),
    sqlc.arg(replacement_session_id)
);
