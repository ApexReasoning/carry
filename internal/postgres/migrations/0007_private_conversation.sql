CREATE TABLE conversations (
    conversation_id uuid PRIMARY KEY,
    space_id uuid NOT NULL,
    member_user_id uuid NOT NULL,
    message_head_seq bigint NOT NULL DEFAULT 0 CHECK (message_head_seq >= 0),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    FOREIGN KEY (space_id, member_user_id) REFERENCES space_memberships(space_id, user_id),
    UNIQUE (space_id, member_user_id),
    UNIQUE (conversation_id, member_user_id)
);

CREATE TABLE conversation_messages (
    message_id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations(conversation_id),
    message_seq bigint NOT NULL CHECK (message_seq > 0),
    author text NOT NULL CHECK (author IN ('member', 'carry')),
    author_user_id uuid REFERENCES carry_users(user_id),
    text text NOT NULL CHECK (
        btrim(text) <> ''
        AND octet_length(text) <= 16384
    ),
    member_request_id text,
    request_digest bytea,
    reply_to_member_message_id uuid,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CONSTRAINT conversation_messages_author_shape CHECK (
        (
            author = 'member'
            AND author_user_id IS NOT NULL
            AND member_request_id IS NOT NULL
            AND btrim(member_request_id) <> ''
            AND octet_length(member_request_id) <= 255
            AND request_digest IS NOT NULL
            AND octet_length(request_digest) = 32
            AND reply_to_member_message_id IS NULL
        )
        OR (
            author = 'carry'
            AND author_user_id IS NULL
            AND member_request_id IS NULL
            AND request_digest IS NULL
            AND reply_to_member_message_id IS NOT NULL
        )
    ),
    FOREIGN KEY (conversation_id, author_user_id)
        REFERENCES conversations(conversation_id, member_user_id),
    FOREIGN KEY (reply_to_member_message_id, conversation_id)
        REFERENCES conversation_messages(message_id, conversation_id),
    UNIQUE (conversation_id, message_seq),
    UNIQUE (message_id, conversation_id),
    UNIQUE (message_id, conversation_id, author),
    UNIQUE (message_id, conversation_id, author, reply_to_member_message_id),
    UNIQUE (reply_to_member_message_id)
);

CREATE UNIQUE INDEX conversation_messages_member_request_idx
    ON conversation_messages (conversation_id, member_request_id)
    WHERE author = 'member';

CREATE INDEX conversation_messages_page_idx
    ON conversation_messages (conversation_id, message_seq);

CREATE TABLE conversation_reply_claims (
    source_message_id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations(conversation_id),
    source_message_author text NOT NULL DEFAULT 'member' CHECK (source_message_author = 'member'),
    current_machine_id uuid REFERENCES machines(machine_id),
    current_fence bigint NOT NULL DEFAULT 0 CHECK (current_fence >= 0),
    lease_expires_at timestamptz,
    context_start_seq bigint,
    context_end_seq bigint,
    output_digest bytea,
    committed_reply_message_id uuid UNIQUE REFERENCES conversation_messages(message_id),
    committed_reply_author text CHECK (committed_reply_author = 'carry'),
    created_work_id uuid UNIQUE REFERENCES works(work_id),
    FOREIGN KEY (source_message_id, conversation_id, source_message_author)
        REFERENCES conversation_messages(message_id, conversation_id, author),
    FOREIGN KEY (
        committed_reply_message_id,
        conversation_id,
        committed_reply_author,
        source_message_id
    ) REFERENCES conversation_messages(
        message_id,
        conversation_id,
        author,
        reply_to_member_message_id
    ),
    CONSTRAINT conversation_reply_claims_context CHECK (
        (
            context_start_seq IS NULL
            AND context_end_seq IS NULL
        )
        OR (
            context_start_seq > 0
            AND context_end_seq >= context_start_seq
        )
    ),
    CONSTRAINT conversation_reply_claims_authority_shape CHECK (
        (
            current_machine_id IS NULL
            AND current_fence = 0
            AND lease_expires_at IS NULL
            AND context_start_seq IS NULL
            AND context_end_seq IS NULL
            AND output_digest IS NULL
            AND committed_reply_message_id IS NULL
            AND committed_reply_author IS NULL
            AND created_work_id IS NULL
        )
        OR (
            current_machine_id IS NOT NULL
            AND current_fence > 0
            AND lease_expires_at IS NOT NULL
            AND context_start_seq IS NOT NULL
            AND context_end_seq IS NOT NULL
            AND (
                (
                    output_digest IS NULL
                    AND committed_reply_message_id IS NULL
                    AND committed_reply_author IS NULL
                    AND created_work_id IS NULL
                )
                OR (
                    output_digest IS NOT NULL
                    AND octet_length(output_digest) = 32
                    AND committed_reply_message_id IS NOT NULL
                    AND committed_reply_author = 'carry'
                )
            )
        )
    )
);

CREATE UNIQUE INDEX conversation_reply_claims_unresolved_idx
    ON conversation_reply_claims (conversation_id)
    WHERE committed_reply_message_id IS NULL;
