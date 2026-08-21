-- name: LockInvitationActorMembership :one
SELECT can_manage_members, can_enroll_machines
FROM space_memberships
WHERE space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
    AND revoked_at IS NULL
FOR UPDATE;

-- name: LockSpaceInvitationRecipientKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    sqlc.arg(space_id)::text || '|' || sqlc.arg(recipient_email)::text,
    934771
));

-- name: LoadInvitationIssueReplay :one
SELECT
    i.invitation_id, i.space_id, i.recipient_email, i.can_manage_members,
    i.can_enroll_machines, i.created_at, i.expires_at, i.issue_request_digest,
    s.submission_id, s.recipient_email AS submission_recipient,
    s.payload_digest, s.provider_idempotency_key, s.state,
    s.provider_message_id, s.created_at AS submission_created_at,
    (s.state = 'prepared'
        AND s.created_at + interval '24 hours' > transaction_timestamp()
        AND i.accepted_at IS NULL AND i.revoked_at IS NULL
        AND i.expires_at > transaction_timestamp()) AS submit_eligible
FROM space_invitations AS i
INNER JOIN space_invitation_submissions AS s ON s.invitation_id = i.invitation_id
WHERE i.space_id = sqlc.arg(space_id)
    AND i.inviter_user_id = sqlc.arg(user_id)
    AND i.issue_idempotency_key = sqlc.arg(idempotency_key)
ORDER BY s.created_at, s.submission_id
LIMIT 1;

-- name: HasCurrentSpaceInvitation :one
SELECT exists(
    SELECT 1 FROM space_invitations
    WHERE space_id = sqlc.arg(space_id)
        AND recipient_email = sqlc.arg(recipient_email)
        AND accepted_at IS NULL
        AND revoked_at IS NULL
        AND expires_at > transaction_timestamp()
);

-- name: EmailOwnerIsActiveSpaceMember :one
SELECT exists(
    SELECT 1
    FROM email_identities AS e
    INNER JOIN space_memberships AS m ON m.user_id = e.user_id
    WHERE e.canonical_email = sqlc.arg(recipient_email)
        AND m.space_id = sqlc.arg(space_id)
        AND m.revoked_at IS NULL
);

-- name: InsertSpaceInvitation :one
INSERT INTO space_invitations (
    invitation_id, space_id, recipient_email, inviter_user_id,
    can_manage_members, can_enroll_machines,
    issue_idempotency_key, issue_request_digest
) VALUES (
    sqlc.arg(invitation_id), sqlc.arg(space_id), sqlc.arg(recipient_email),
    sqlc.arg(user_id), sqlc.arg(can_manage_members), sqlc.arg(can_enroll_machines),
    sqlc.arg(idempotency_key), sqlc.arg(request_digest)
)
RETURNING created_at, expires_at;

-- name: InsertSpaceInvitationSubmission :one
INSERT INTO space_invitation_submissions (
    submission_id, invitation_id, requested_by_user_id,
    idempotency_key, request_digest, recipient_email,
    payload_digest, provider_idempotency_key
) VALUES (
    sqlc.arg(submission_id), sqlc.arg(invitation_id), sqlc.arg(user_id),
    sqlc.arg(idempotency_key), sqlc.arg(request_digest), sqlc.arg(recipient_email),
    sqlc.arg(payload_digest), sqlc.arg(provider_idempotency_key)
)
RETURNING submission_id, recipient_email, payload_digest,
    provider_idempotency_key, state, provider_message_id, created_at,
    (state = 'prepared' AND created_at + interval '24 hours' > transaction_timestamp()) AS submit_eligible;

-- name: LoadInvitationSubmission :one
SELECT submission_id, invitation_id, recipient_email, payload_digest,
    provider_idempotency_key, state, provider_message_id, created_at,
    requested_by_user_id, idempotency_key, request_digest
FROM space_invitation_submissions
WHERE submission_id = sqlc.arg(submission_id);

-- name: RecordSpaceInvitationSubmission :one
UPDATE space_invitation_submissions
SET
    state = sqlc.arg(state),
    provider_message_id = sqlc.narg(provider_message_id),
    recorded_at = COALESCE(recorded_at, transaction_timestamp())
WHERE submission_id = sqlc.arg(submission_id)
    AND payload_digest = sqlc.arg(payload_digest)
    AND (
        state = 'prepared'
        OR (
            state = sqlc.arg(state)
            AND provider_message_id IS NOT DISTINCT FROM sqlc.narg(provider_message_id)
        )
    )
RETURNING submission_id, recipient_email, payload_digest,
    provider_idempotency_key, state, provider_message_id, created_at;

-- name: LoadSpaceInvitation :one
SELECT invitation_id, space_id, recipient_email, inviter_user_id,
    can_manage_members, can_enroll_machines, created_at, expires_at,
    accepted_by_user_id, accepted_at, accept_result,
    accept_idempotency_key, accept_request_digest,
    revoked_by_user_id, revoked_at, revoke_idempotency_key, revoke_request_digest
FROM space_invitations
WHERE invitation_id = sqlc.arg(invitation_id);

-- name: LoadSpaceInvitationForUpdate :one
SELECT invitation_id, space_id, recipient_email, inviter_user_id,
    can_manage_members, can_enroll_machines, created_at, expires_at,
    accepted_by_user_id, accepted_at, accept_result,
    accept_idempotency_key, accept_request_digest,
    revoked_by_user_id, revoked_at, revoke_idempotency_key, revoke_request_digest
FROM space_invitations
WHERE invitation_id = sqlc.arg(invitation_id)
FOR UPDATE;

-- name: LoadInvitationResendReplay :one
SELECT
    i.invitation_id, i.space_id, i.recipient_email, i.can_manage_members,
    i.can_enroll_machines, i.created_at, i.expires_at,
    s.submission_id, s.recipient_email AS submission_recipient,
    s.payload_digest, s.provider_idempotency_key, s.state,
    s.provider_message_id, s.created_at AS submission_created_at,
    s.request_digest,
    (s.state = 'prepared'
        AND s.created_at + interval '24 hours' > transaction_timestamp()
        AND i.accepted_at IS NULL AND i.revoked_at IS NULL
        AND i.expires_at > transaction_timestamp()) AS submit_eligible
FROM space_invitation_submissions AS s
INNER JOIN space_invitations AS i ON i.invitation_id = s.invitation_id
WHERE s.invitation_id = sqlc.arg(invitation_id)
    AND i.space_id = sqlc.arg(space_id)
    AND s.requested_by_user_id = sqlc.arg(user_id)
    AND s.idempotency_key = sqlc.arg(idempotency_key);

-- name: LoadLatestInvitationSubmissionTime :one
SELECT created_at
FROM space_invitation_submissions
WHERE invitation_id = sqlc.arg(invitation_id)
ORDER BY created_at DESC, submission_id DESC
LIMIT 1;

-- name: ListManagedSpaceInvitations :many
SELECT
    i.invitation_id, i.space_id, i.recipient_email,
    u.display_name AS inviter_display_name,
    i.can_manage_members, i.can_enroll_machines,
    i.created_at, i.expires_at,
    s.submission_id, s.recipient_email AS submission_recipient,
    s.payload_digest, s.provider_idempotency_key, s.state,
    s.provider_message_id, s.created_at AS submission_created_at
FROM space_invitations AS i
INNER JOIN carry_users AS u ON u.user_id = i.inviter_user_id
INNER JOIN LATERAL (
    SELECT * FROM space_invitation_submissions
    WHERE invitation_id = i.invitation_id
    ORDER BY created_at DESC, submission_id DESC
    LIMIT 1
) AS s ON true
WHERE i.space_id = sqlc.arg(space_id)
    AND i.accepted_at IS NULL
    AND i.revoked_at IS NULL
    AND i.expires_at > transaction_timestamp()
ORDER BY i.created_at, i.invitation_id
LIMIT 50;

-- name: LoadInvitationInboxSession :one
SELECT bs.user_id, bs.identity_proved_at, bs.identity_proof_method,
    bs.expires_at, bs.revoked_at
FROM browser_sessions AS bs
WHERE bs.session_id = sqlc.arg(session_id)
FOR UPDATE;

-- name: ListInvitationsForEmailOwner :many
SELECT
    i.invitation_id, i.space_id, sp.name AS space_name,
    u.display_name AS inviter_display_name,
    i.can_manage_members, i.can_enroll_machines,
    i.created_at, i.expires_at
FROM email_identities AS e
INNER JOIN space_invitations AS i ON i.recipient_email = e.canonical_email
INNER JOIN spaces AS sp ON sp.space_id = i.space_id
INNER JOIN carry_users AS u ON u.user_id = i.inviter_user_id
WHERE e.user_id = sqlc.arg(user_id)
    AND i.accepted_at IS NULL
    AND i.revoked_at IS NULL
    AND i.expires_at > transaction_timestamp()
ORDER BY i.expires_at, i.invitation_id
LIMIT 50;

-- name: LockInvitationUser :one
SELECT user_id
FROM carry_users
WHERE user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: LoadInvitationEmailOwner :one
SELECT user_id
FROM email_identities
WHERE canonical_email = sqlc.arg(recipient_email);

-- name: LoadInvitationSpaceName :one
SELECT name FROM spaces WHERE space_id = sqlc.arg(space_id);

-- name: LoadMembershipForInvitation :one
SELECT can_manage_members, can_enroll_machines, revoked_at
FROM space_memberships
WHERE space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: CreateInvitationMembership :exec
INSERT INTO space_memberships (
    space_id, user_id, can_manage_members, can_enroll_machines
) VALUES (
    sqlc.arg(space_id), sqlc.arg(user_id),
    sqlc.arg(can_manage_members), sqlc.arg(can_enroll_machines)
);

-- name: AcceptSpaceInvitation :execrows
UPDATE space_invitations
SET
    accepted_by_user_id = sqlc.arg(user_id),
    accepted_at = transaction_timestamp(),
    accept_result = sqlc.arg(accept_result),
    accept_idempotency_key = sqlc.arg(idempotency_key),
    accept_request_digest = sqlc.arg(request_digest)
WHERE invitation_id = sqlc.arg(invitation_id)
    AND accepted_at IS NULL
    AND revoked_at IS NULL
    AND expires_at > transaction_timestamp();

-- name: RevokeSpaceInvitation :execrows
UPDATE space_invitations
SET
    revoked_by_user_id = sqlc.arg(user_id),
    revoked_at = transaction_timestamp(),
    revoke_idempotency_key = sqlc.arg(idempotency_key),
    revoke_request_digest = sqlc.arg(request_digest)
WHERE invitation_id = sqlc.arg(invitation_id)
    AND accepted_at IS NULL
    AND revoked_at IS NULL
    AND expires_at > transaction_timestamp();
