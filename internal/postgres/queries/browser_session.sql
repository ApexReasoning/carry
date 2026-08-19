-- name: LoadActiveUserTokenForBrowserSession :one
SELECT token_id, user_id, expires_at
FROM user_tokens
WHERE
    token_hash = sqlc.arg(token_hash)
    AND revoked_at IS NULL
    AND expires_at > transaction_timestamp()
FOR SHARE;

-- name: CreateBrowserSession :one
INSERT INTO browser_sessions (
    session_digest,
    user_id,
    source_token_id,
    expires_at
) VALUES (
    sqlc.arg(session_digest),
    sqlc.arg(user_id),
    sqlc.arg(source_token_id),
    sqlc.arg(expires_at)
)
RETURNING user_id, expires_at;

-- name: AuthenticateBrowserSession :one
SELECT browser_session.user_id
FROM browser_sessions AS browser_session
INNER JOIN user_tokens AS user_token
    ON user_token.token_id = browser_session.source_token_id
    AND user_token.user_id = browser_session.user_id
WHERE
    browser_session.session_digest = sqlc.arg(session_digest)
    AND browser_session.revoked_at IS NULL
    AND browser_session.expires_at > transaction_timestamp()
    AND user_token.revoked_at IS NULL
    AND user_token.expires_at > transaction_timestamp();

-- name: RevokeBrowserSession :execrows
UPDATE browser_sessions
SET revoked_at = transaction_timestamp()
WHERE session_digest = sqlc.arg(session_digest) AND revoked_at IS NULL;
