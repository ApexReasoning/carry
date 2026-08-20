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
