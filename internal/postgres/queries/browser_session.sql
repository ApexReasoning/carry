-- name: CreateBrowserSession :one
INSERT INTO browser_sessions (session_id, user_id, expires_at)
VALUES (sqlc.arg(session_id), sqlc.arg(user_id), transaction_timestamp() + interval '30 days')
RETURNING *;

-- name: LoadBrowserSessionByID :one
SELECT session_id, user_id, expires_at, revoked_at
FROM browser_sessions
WHERE session_id = sqlc.arg(session_id);

-- name: LoadActiveBrowserSessionByID :one
SELECT session_id, user_id, expires_at, revoked_at
FROM browser_sessions
WHERE session_id = sqlc.arg(session_id)
    AND revoked_at IS NULL
    AND expires_at > transaction_timestamp();

-- name: AuthenticateBrowserSession :one
SELECT
    browser_session.user_id,
    carry_user.display_name
FROM browser_sessions AS browser_session
INNER JOIN
    carry_users AS carry_user
    ON browser_session.user_id = carry_user.user_id
WHERE
    browser_session.session_id = sqlc.arg(session_id)
    AND browser_session.revoked_at IS NULL
    AND browser_session.expires_at > transaction_timestamp();

-- name: RevokeBrowserSession :execrows
UPDATE browser_sessions
SET revoked_at = transaction_timestamp()
WHERE session_id = sqlc.arg(session_id) AND revoked_at IS NULL;
