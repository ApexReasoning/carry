-- name: CLIDatabaseTime :one
SELECT transaction_timestamp()::timestamptz;

-- name: LockCLILoginAdmission :exec
SELECT pg_advisory_xact_lock(551917301::bigint);

-- name: LoadCLILoginForBegin :one
SELECT * FROM cli_login_requests WHERE request_id = sqlc.arg(request_id);

-- name: CountLiveCLILoginsForSource :one
SELECT count(*) FROM cli_login_requests
WHERE source_digest = sqlc.arg(source_digest)
    AND expires_at > transaction_timestamp()
    AND denied_at IS NULL AND cancelled_at IS NULL AND redeemed_at IS NULL;

-- name: CountLiveCLILogins :one
SELECT count(*) FROM cli_login_requests
WHERE expires_at > transaction_timestamp()
    AND denied_at IS NULL AND cancelled_at IS NULL AND redeemed_at IS NULL;

-- name: CreateCLILogin :one
INSERT INTO cli_login_requests (
    request_id, begin_idempotency_key, begin_request_digest, user_code_digest,
    code_generation, source_digest, label, proposed_replacement_credential_id
) VALUES (
    sqlc.arg(request_id), sqlc.arg(begin_idempotency_key), sqlc.arg(begin_request_digest),
    sqlc.arg(user_code_digest), sqlc.arg(code_generation), sqlc.arg(source_digest),
    sqlc.arg(label), sqlc.narg(proposed_replacement_credential_id)
)
RETURNING *;

-- name: LockBrowserSessionForCLILogin :one
SELECT session.user_id, carry_user.display_name AS display_name
FROM browser_sessions AS session
INNER JOIN carry_users AS carry_user ON carry_user.user_id = session.user_id
WHERE session.session_id = sqlc.arg(session_id)
    AND session.revoked_at IS NULL
    AND session.expires_at > transaction_timestamp()
FOR UPDATE OF session;

-- name: LockCLILoginLookupBudget :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 11471));

-- name: CountRecentCLILookupFailuresForSession :one
SELECT count(*) FROM cli_login_lookup_failures
WHERE browser_session_id = sqlc.arg(browser_session_id)
    AND created_at > transaction_timestamp() - interval '10 minutes';

-- name: CountRecentCLILookupFailuresForSource :one
SELECT count(*) FROM cli_login_lookup_failures
WHERE source_digest = sqlc.arg(source_digest)
    AND created_at > transaction_timestamp() - interval '10 minutes';

-- name: RecordCLILoginLookupFailure :exec
INSERT INTO cli_login_lookup_failures (failure_id, browser_session_id, source_digest)
VALUES (sqlc.arg(failure_id), sqlc.arg(browser_session_id), sqlc.arg(source_digest));

-- name: FindLiveCLILoginByCode :one
SELECT * FROM cli_login_requests
WHERE user_code_digest = sqlc.arg(user_code_digest)
    AND expires_at > transaction_timestamp()
    AND denied_at IS NULL AND cancelled_at IS NULL;

-- name: LockCLILoginRequest :one
SELECT * FROM cli_login_requests
WHERE request_id = sqlc.arg(request_id)
FOR UPDATE;

-- name: LockSpaceForCLILogin :one
SELECT space_id FROM spaces WHERE space_id = sqlc.arg(space_id) FOR KEY SHARE;

-- name: LockActiveMembershipForCLILogin :one
SELECT user_id FROM space_memberships
WHERE space_id = sqlc.arg(space_id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL
FOR UPDATE;

-- name: LoadCLICredentialForUser :one
SELECT * FROM cli_credentials
WHERE credential_id = sqlc.arg(credential_id) AND user_id = sqlc.arg(user_id);

-- name: ApproveCLILogin :one
UPDATE cli_login_requests
SET approved_at = transaction_timestamp(),
    approved_by_user_id = sqlc.arg(user_id),
    approved_space_id = sqlc.arg(space_id),
    approval_idempotency_key = sqlc.arg(idempotency_key),
    approval_request_digest = sqlc.arg(request_digest),
    prepared_credential_id = sqlc.arg(credential_id),
    proposed_replacement_credential_id = sqlc.narg(replacement_credential_id)
WHERE request_id = sqlc.arg(request_id)
    AND approved_at IS NULL AND denied_at IS NULL AND cancelled_at IS NULL
    AND redeemed_at IS NULL AND expires_at > transaction_timestamp()
RETURNING *;

-- name: DenyCLILogin :one
UPDATE cli_login_requests
SET denied_at = transaction_timestamp(), denied_by_user_id = sqlc.arg(user_id),
    denial_idempotency_key = sqlc.arg(idempotency_key),
    denial_request_digest = sqlc.arg(request_digest)
WHERE request_id = sqlc.arg(request_id)
    AND approved_at IS NULL AND denied_at IS NULL AND cancelled_at IS NULL
    AND redeemed_at IS NULL AND expires_at > transaction_timestamp()
RETURNING *;

-- name: RecordCLILoginPoll :one
UPDATE cli_login_requests
SET last_polled_at = transaction_timestamp()
WHERE request_id = sqlc.arg(request_id)
RETURNING *;

-- name: SlowDownCLILoginPoll :one
UPDATE cli_login_requests
SET last_polled_at = transaction_timestamp(),
    poll_interval_seconds = least(30, poll_interval_seconds + 5)
WHERE request_id = sqlc.arg(request_id)
RETURNING *;

-- name: CreateCLICredential :one
INSERT INTO cli_credentials (credential_id, login_request_id, user_id, label)
VALUES (sqlc.arg(credential_id), sqlc.arg(login_request_id), sqlc.arg(user_id), sqlc.arg(label))
RETURNING *;

-- name: RedeemCLILogin :one
UPDATE cli_login_requests
SET resulting_credential_id = sqlc.arg(credential_id),
    redeemed_at = transaction_timestamp(),
    replay_until = transaction_timestamp() + interval '5 minutes'
WHERE request_id = sqlc.arg(request_id)
    AND approved_at IS NOT NULL AND cancelled_at IS NULL AND denied_at IS NULL
    AND redeemed_at IS NULL AND expires_at > transaction_timestamp()
RETURNING *;

-- name: LoadCLICredential :one
SELECT * FROM cli_credentials WHERE credential_id = sqlc.arg(credential_id);

-- name: LockCLICredential :one
SELECT * FROM cli_credentials WHERE credential_id = sqlc.arg(credential_id) FOR UPDATE;

-- name: RevokeCLICredential :one
UPDATE cli_credentials
SET revoked_at = transaction_timestamp(), revoked_by_user_id = sqlc.arg(user_id),
    revocation_idempotency_key = sqlc.arg(idempotency_key),
    revocation_request_digest = sqlc.arg(request_digest)
WHERE credential_id = sqlc.arg(credential_id) AND revoked_at IS NULL
RETURNING *;

-- name: CancelCLILogin :one
UPDATE cli_login_requests
SET cancelled_at = transaction_timestamp()
WHERE request_id = sqlc.arg(request_id)
    AND denied_at IS NULL AND cancelled_at IS NULL AND redeemed_at IS NULL
    AND expires_at > transaction_timestamp()
RETURNING *;

-- name: ListActiveCLICredentials :many
SELECT credential.credential_id, credential.label, credential.created_at, credential.expires_at,
    request.approved_space_id, coalesce(space.name, '') AS approved_space_name
FROM cli_credentials AS credential
INNER JOIN cli_login_requests AS request ON request.request_id = credential.login_request_id
LEFT JOIN space_memberships AS membership
    ON membership.space_id = request.approved_space_id
    AND membership.user_id = credential.user_id
    AND membership.revoked_at IS NULL
LEFT JOIN spaces AS space
    ON space.space_id = membership.space_id
WHERE credential.user_id = sqlc.arg(user_id)
    AND credential.revoked_at IS NULL AND credential.expires_at > transaction_timestamp()
ORDER BY credential.created_at DESC, credential.credential_id;

-- name: AuthenticateCLICredential :one
SELECT credential.user_id, carry_user.display_name AS display_name
FROM cli_credentials AS credential
INNER JOIN carry_users AS carry_user ON carry_user.user_id = credential.user_id
WHERE credential.credential_id = sqlc.arg(credential_id)
    AND credential.revoked_at IS NULL AND credential.expires_at > transaction_timestamp();
