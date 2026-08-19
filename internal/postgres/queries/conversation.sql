-- name: LockActiveConversationMembership :one
SELECT version
FROM space_memberships
WHERE
    space_id = sqlc.arg(space_id)
    AND user_id = sqlc.arg(user_id)
    AND revoked_at IS NULL
FOR SHARE;

-- name: EnsureConversation :one
INSERT INTO conversations (conversation_id, space_id, member_user_id)
VALUES (sqlc.arg(conversation_id), sqlc.arg(space_id), sqlc.arg(member_user_id))
ON CONFLICT (space_id, member_user_id) DO UPDATE
SET conversation_id = conversations.conversation_id
RETURNING conversation_id, message_head_seq;

-- name: FindConversation :one
SELECT conversation_id
FROM conversations
WHERE space_id = sqlc.arg(space_id) AND member_user_id = sqlc.arg(member_user_id);

-- name: LockConversation :one
SELECT conversation_id, message_head_seq
FROM conversations
WHERE conversation_id = sqlc.arg(conversation_id)
FOR UPDATE;

-- name: FindConversationMemberRequest :one
SELECT message_id, author, text, member_request_id, message_seq, created_at, request_digest
FROM conversation_messages
WHERE
    conversation_id = sqlc.arg(conversation_id)
    AND author = 'member'
    AND member_request_id = sqlc.arg(member_request_id);

-- name: ConversationHasUnresolvedReply :one
SELECT EXISTS (
    SELECT 1
    FROM conversation_reply_claims
    WHERE conversation_id = sqlc.arg(conversation_id)
      AND committed_reply_message_id IS NULL
);

-- name: AdvanceConversationMessageHead :one
UPDATE conversations
SET message_head_seq = message_head_seq + 1
WHERE conversation_id = sqlc.arg(conversation_id)
RETURNING message_head_seq;

-- name: CreateConversationMemberMessage :one
INSERT INTO conversation_messages (
    message_id,
    conversation_id,
    message_seq,
    author,
    author_user_id,
    text,
    member_request_id,
    request_digest
) VALUES (
    sqlc.arg(message_id),
    sqlc.arg(conversation_id),
    sqlc.arg(message_seq),
    'member',
    sqlc.arg(author_user_id),
    sqlc.arg(text),
    sqlc.arg(member_request_id),
    sqlc.arg(request_digest)
)
RETURNING message_id, author, text, member_request_id, message_seq, created_at;

-- name: CreateConversationReplyClaim :exec
INSERT INTO conversation_reply_claims (source_message_id, conversation_id)
VALUES (sqlc.arg(source_message_id), sqlc.arg(conversation_id));

-- name: ConversationCursorSequence :one
SELECT message_seq
FROM conversation_messages
WHERE conversation_id = sqlc.arg(conversation_id) AND message_id = sqlc.arg(message_id);

-- name: ListNewestConversationMessages :many
SELECT * FROM (
    SELECT
        message.message_id,
        message.author,
        message.text,
        message.member_request_id,
        claim.created_work_id,
        message.message_seq,
        message.created_at
    FROM conversation_messages AS message
    LEFT JOIN conversation_reply_claims AS claim
        ON claim.committed_reply_message_id = message.message_id
    WHERE message.conversation_id = sqlc.arg(conversation_id)
    ORDER BY message.message_seq DESC
    LIMIT 50
) AS newest
ORDER BY message_seq;

-- name: ListConversationMessagesBefore :many
SELECT * FROM (
    SELECT
        message.message_id,
        message.author,
        message.text,
        message.member_request_id,
        claim.created_work_id,
        message.message_seq,
        message.created_at
    FROM conversation_messages AS message
    LEFT JOIN conversation_reply_claims AS claim
        ON claim.committed_reply_message_id = message.message_id
    WHERE
        message.conversation_id = sqlc.arg(conversation_id)
        AND message.message_seq < sqlc.arg(cursor_sequence)
    ORDER BY message.message_seq DESC
    LIMIT 50
) AS prior
ORDER BY message_seq;

-- name: ListConversationMessagesAfter :many
SELECT
    message.message_id,
    message.author,
    message.text,
    message.member_request_id,
    claim.created_work_id,
    message.message_seq,
    message.created_at
FROM conversation_messages AS message
LEFT JOIN conversation_reply_claims AS claim
    ON claim.committed_reply_message_id = message.message_id
WHERE
    message.conversation_id = sqlc.arg(conversation_id)
    AND message.message_seq > sqlc.arg(cursor_sequence)
ORDER BY message.message_seq
LIMIT 50;

-- name: LockConversationReplyForClaim :one
SELECT
    claim.source_message_id,
    claim.conversation_id,
    claim.current_machine_id,
    claim.current_fence,
    claim.lease_expires_at,
    claim.context_start_seq,
    claim.context_end_seq,
    source.message_seq AS source_message_seq
FROM conversation_reply_claims AS claim
JOIN conversations AS conversation
    ON conversation.conversation_id = claim.conversation_id
JOIN conversation_messages AS source
    ON source.message_id = claim.source_message_id
JOIN space_memberships AS membership
    ON membership.space_id = conversation.space_id
    AND membership.user_id = conversation.member_user_id
WHERE
    conversation.space_id = sqlc.arg(space_id)
    AND membership.revoked_at IS NULL
    AND claim.committed_reply_message_id IS NULL
    AND (
        claim.current_machine_id IS NULL
        OR claim.lease_expires_at <= clock_timestamp()
    )
ORDER BY source.created_at, source.message_id
LIMIT 1
FOR UPDATE OF claim SKIP LOCKED
FOR SHARE OF membership;

-- name: ListConversationContextCandidates :many
SELECT message_seq, author, text
FROM (
    SELECT message_seq, author, text
    FROM conversation_messages
    WHERE
        conversation_id = sqlc.arg(conversation_id)
        AND message_seq <= sqlc.arg(context_end_seq)
    ORDER BY message_seq DESC
    LIMIT 32
) AS newest
ORDER BY message_seq;

-- name: AssignConversationReply :one
UPDATE conversation_reply_claims
SET
    current_machine_id = sqlc.arg(machine_id),
    current_fence = current_fence + 1,
    lease_expires_at = clock_timestamp() + interval '5 minutes',
    context_start_seq = sqlc.arg(context_start_seq),
    context_end_seq = sqlc.arg(context_end_seq)
WHERE
    source_message_id = sqlc.arg(source_message_id)
    AND current_fence = sqlc.arg(expected_fence)
    AND committed_reply_message_id IS NULL
    AND (
        current_machine_id IS NULL
        OR lease_expires_at <= clock_timestamp()
    )
RETURNING current_fence, lease_expires_at;

-- name: ListFixedConversationReplyContext :many
SELECT author, text
FROM conversation_messages
WHERE
    conversation_id = sqlc.arg(conversation_id)
    AND message_seq >= sqlc.arg(context_start_seq)
    AND message_seq <= sqlc.arg(context_end_seq)
ORDER BY message_seq;

-- name: LockConversationReplyForRenew :one
SELECT claim.source_message_id
FROM conversation_reply_claims AS claim
JOIN conversations AS conversation
    ON conversation.conversation_id = claim.conversation_id
JOIN space_memberships AS membership
    ON membership.space_id = conversation.space_id
    AND membership.user_id = conversation.member_user_id
WHERE
    claim.source_message_id = sqlc.arg(source_message_id)
    AND conversation.space_id = sqlc.arg(space_id)
    AND membership.revoked_at IS NULL
    AND claim.current_machine_id = sqlc.arg(machine_id)
    AND claim.current_fence = sqlc.arg(fence)
    AND claim.lease_expires_at > clock_timestamp()
    AND claim.committed_reply_message_id IS NULL
FOR UPDATE OF claim
FOR SHARE OF membership;

-- name: ExtendConversationReplyLease :one
UPDATE conversation_reply_claims
SET lease_expires_at = clock_timestamp() + interval '5 minutes'
WHERE
    source_message_id = sqlc.arg(source_message_id)
    AND current_machine_id = sqlc.arg(machine_id)
    AND current_fence = sqlc.arg(fence)
    AND lease_expires_at > clock_timestamp()
    AND committed_reply_message_id IS NULL
RETURNING lease_expires_at;

-- name: LockConversationReplyForCommit :one
SELECT
    claim.conversation_id,
    conversation.space_id,
    conversation.member_user_id,
    conversation.message_head_seq,
    source.message_seq AS source_message_seq,
    claim.current_machine_id,
    claim.current_fence,
    claim.output_digest,
    claim.committed_reply_message_id,
    claim.created_work_id
FROM conversation_reply_claims AS claim
JOIN conversations AS conversation
    ON conversation.conversation_id = claim.conversation_id
JOIN conversation_messages AS source
    ON source.message_id = claim.source_message_id
JOIN space_memberships AS membership
    ON membership.space_id = conversation.space_id
    AND membership.user_id = conversation.member_user_id
WHERE
    claim.source_message_id = sqlc.arg(source_message_id)
    AND conversation.space_id = sqlc.arg(space_id)
    AND membership.revoked_at IS NULL
FOR UPDATE OF claim, conversation
FOR SHARE OF membership;

-- name: ConversationReplyLeaseIsCurrent :one
SELECT lease_expires_at > clock_timestamp()
FROM conversation_reply_claims
WHERE source_message_id = sqlc.arg(source_message_id)
    AND lease_expires_at IS NOT NULL;

-- name: CreateConversationCarryReply :one
INSERT INTO conversation_messages (
    message_id,
    conversation_id,
    message_seq,
    author,
    text,
    reply_to_member_message_id
) VALUES (
    sqlc.arg(message_id),
    sqlc.arg(conversation_id),
    sqlc.arg(message_seq),
    'carry',
    sqlc.arg(text),
    sqlc.arg(source_message_id)
)
RETURNING message_id;

-- name: CompleteConversationReply :execrows
UPDATE conversation_reply_claims
SET
    output_digest = sqlc.arg(output_digest),
    committed_reply_message_id = sqlc.arg(reply_message_id),
    committed_reply_author = 'carry',
    created_work_id = sqlc.narg(created_work_id)
WHERE
    source_message_id = sqlc.arg(source_message_id)
    AND current_machine_id = sqlc.arg(machine_id)
    AND current_fence = sqlc.arg(fence)
    AND lease_expires_at > clock_timestamp()
    AND committed_reply_message_id IS NULL;
