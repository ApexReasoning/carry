-- name: LoadAgentReportMachine :one
SELECT machine_id, space_id, enrolled_by_user_id
FROM machines
WHERE machine_id = sqlc.arg(machine_id);

-- name: ListMachineAgentBindings :many
SELECT adapter_key, occurrence_key
FROM agents
WHERE machine_id = sqlc.arg(machine_id);

-- name: LockSpaceForAgentAllocation :one
SELECT space_id
FROM spaces
WHERE space_id = sqlc.arg(space_id)
FOR NO KEY UPDATE;

-- name: LockAgentApproverMembership :one
SELECT user_id, revoked_at
FROM space_memberships
WHERE space_id = sqlc.arg(space_id) AND user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: LockMachineForAgentReport :one
SELECT *
FROM machines
WHERE machine_id = sqlc.arg(machine_id)
FOR UPDATE;

-- name: LockAgentsForMachine :many
SELECT agent.agent_id, agent.space_id, agent.machine_id, agent.owner_user_id,
    agent.adapter_key, agent.occurrence_key, agent.name, agent.name_key,
    agent.created_at, agent.removed_at,
    presence.present, presence.last_present_at
FROM agents AS agent
INNER JOIN agent_presence AS presence ON presence.agent_id = agent.agent_id
WHERE agent.machine_id = sqlc.arg(machine_id)
ORDER BY agent.agent_id
FOR UPDATE OF agent;

-- name: AgentNameExists :one
SELECT EXISTS(
    SELECT 1 FROM agents
    WHERE space_id = sqlc.arg(space_id) AND name_key = sqlc.arg(name_key)
);

-- name: InsertAgent :one
INSERT INTO agents (
    agent_id, space_id, machine_id, owner_user_id,
    adapter_key, occurrence_key, name, name_key
) VALUES (
    sqlc.arg(agent_id), sqlc.arg(space_id), sqlc.arg(machine_id), sqlc.arg(owner_user_id),
    sqlc.arg(adapter_key), sqlc.arg(occurrence_key), sqlc.arg(name), sqlc.arg(name_key)
)
RETURNING *;

-- name: InsertAgentPresence :exec
INSERT INTO agent_presence (agent_id, present)
VALUES (sqlc.arg(agent_id), false);

-- name: SetMachineActiveAgentsAbsent :exec
UPDATE agent_presence AS presence
SET present = false
FROM agents AS agent
WHERE presence.agent_id = agent.agent_id
    AND agent.machine_id = sqlc.arg(machine_id)
    AND agent.removed_at IS NULL;

-- name: SetActiveAgentPresent :execrows
UPDATE agent_presence AS presence
SET present = true, last_present_at = transaction_timestamp()
FROM agents AS agent
WHERE presence.agent_id = agent.agent_id
    AND agent.agent_id = sqlc.arg(agent_id)
    AND agent.removed_at IS NULL;

-- name: UpdateMachineAgentReport :one
UPDATE machines
SET agent_report_revision = agent_report_revision + 1,
    agent_reported_at = transaction_timestamp(),
    last_agent_report_id = sqlc.arg(report_id),
    last_agent_report_digest = sqlc.arg(report_digest),
    last_agent_report_unsupported_keys = sqlc.arg(unsupported_keys)::text[],
    last_agent_report_setup_required_keys = sqlc.arg(setup_required_keys)::text[]
WHERE machine_id = sqlc.arg(machine_id)
RETURNING agent_report_revision;

-- name: ListInventoryAgents :many
SELECT agent.agent_id, agent.machine_id, agent.name, agent.owner_user_id,
    owner.display_name AS owner_name, agent.removed_at,
    presence.last_present_at,
    (agent.removed_at IS NULL
        AND machine.revoked_at IS NULL
        AND presence.present
        AND machine.agent_reported_at > transaction_timestamp()
            - make_interval(secs => sqlc.arg(freshness_seconds)::int)) AS online
FROM agents AS agent
INNER JOIN agent_presence AS presence ON presence.agent_id = agent.agent_id
INNER JOIN machines AS machine ON machine.machine_id = agent.machine_id
INNER JOIN carry_users AS owner ON owner.user_id = agent.owner_user_id
WHERE agent.machine_id = ANY(sqlc.arg(machine_ids)::uuid[])
ORDER BY agent.machine_id, agent.agent_id;

-- name: LockActiveAgentsForMachineRemoval :many
SELECT agent_id
FROM agents
WHERE machine_id = sqlc.arg(machine_id) AND removed_at IS NULL
ORDER BY agent_id
FOR UPDATE;

-- name: RemoveActiveAgentsForMachine :exec
WITH removed AS (
    UPDATE agents
    SET removed_at = transaction_timestamp()
    WHERE machine_id = sqlc.arg(machine_id) AND removed_at IS NULL
    RETURNING agent_id
)
UPDATE agent_presence AS presence
SET present = false
FROM removed
WHERE presence.agent_id = removed.agent_id;

-- name: LockActiveAgentsForOwnerRemoval :many
SELECT agent_id
FROM agents
WHERE space_id = sqlc.arg(space_id)
    AND owner_user_id = sqlc.arg(owner_user_id)
    AND removed_at IS NULL
ORDER BY agent_id
FOR UPDATE;

-- name: RemoveActiveAgentsForOwner :exec
WITH removed AS (
    UPDATE agents
    SET removed_at = transaction_timestamp()
    WHERE space_id = sqlc.arg(space_id)
        AND owner_user_id = sqlc.arg(owner_user_id)
        AND removed_at IS NULL
    RETURNING agent_id
)
UPDATE agent_presence AS presence
SET present = false
FROM removed
WHERE presence.agent_id = removed.agent_id;
