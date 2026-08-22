ALTER TABLE machines
    ADD COLUMN agent_report_revision bigint NOT NULL DEFAULT 0 CHECK (agent_report_revision >= 0),
    ADD COLUMN agent_reported_at timestamptz,
    ADD COLUMN last_agent_report_id uuid,
    ADD COLUMN last_agent_report_digest bytea,
    ADD COLUMN last_agent_report_unsupported_keys text[] NOT NULL DEFAULT '{}',
    ADD COLUMN last_agent_report_setup_required_keys text[] NOT NULL DEFAULT '{}',
    ADD CONSTRAINT machines_agent_report_shape_check CHECK (
        cardinality(last_agent_report_unsupported_keys) <= 32
        AND cardinality(last_agent_report_setup_required_keys) <= 32
        AND (
            (agent_report_revision = 0
                AND agent_reported_at IS NULL
                AND last_agent_report_id IS NULL
                AND last_agent_report_digest IS NULL
                AND cardinality(last_agent_report_unsupported_keys) = 0
                AND cardinality(last_agent_report_setup_required_keys) = 0)
            OR
            (agent_report_revision > 0
                AND agent_reported_at IS NOT NULL
                AND last_agent_report_id IS NOT NULL
                AND last_agent_report_digest IS NOT NULL
                AND octet_length(last_agent_report_digest) = 32)
        )
    ),
    ADD CONSTRAINT machines_identity_space_unique UNIQUE (machine_id, space_id);

CREATE TABLE agents (
    agent_id uuid PRIMARY KEY,
    space_id uuid NOT NULL,
    machine_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    adapter_key text NOT NULL CHECK (
        adapter_key <> '' AND octet_length(adapter_key) <= 63
    ),
    occurrence_key text NOT NULL CHECK (
        occurrence_key <> '' AND octet_length(occurrence_key) <= 127
    ),
    name text NOT NULL CHECK (
        btrim(name) <> '' AND name = btrim(name) AND octet_length(name) <= 128
    ),
    name_key text NOT NULL CHECK (
        name_key <> '' AND octet_length(name_key) <= 128
    ),
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    removed_at timestamptz,
    CONSTRAINT agents_machine_space_fkey
        FOREIGN KEY (machine_id, space_id) REFERENCES machines(machine_id, space_id),
    CONSTRAINT agents_owner_membership_fkey
        FOREIGN KEY (space_id, owner_user_id) REFERENCES space_memberships(space_id, user_id),
    CONSTRAINT agents_removed_time_check CHECK (removed_at IS NULL OR removed_at >= created_at),
    CONSTRAINT agents_machine_occurrence_unique UNIQUE (machine_id, adapter_key, occurrence_key),
    CONSTRAINT agents_space_name_unique UNIQUE (space_id, name_key)
);

CREATE INDEX agents_machine_inventory_idx
    ON agents (machine_id, agent_id);
CREATE INDEX agents_active_owner_idx
    ON agents (space_id, owner_user_id, agent_id)
    WHERE removed_at IS NULL;

CREATE TABLE agent_presence (
    agent_id uuid PRIMARY KEY REFERENCES agents(agent_id),
    present boolean NOT NULL,
    last_present_at timestamptz,
    CONSTRAINT agent_presence_present_time_check CHECK (NOT present OR last_present_at IS NOT NULL)
);
