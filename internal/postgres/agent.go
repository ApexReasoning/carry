package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/postgres/dbsqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type agentBinding struct {
	adapterKey    agent.AdapterKey
	occurrenceKey agent.OccurrenceKey
}

// ReconcileAgentPresence applies one complete Machine-authenticated occurrence report.
func (s *Store) ReconcileAgentPresence(ctx context.Context, command machine.AgentReportCommand) (machine.AgentReportResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return machine.AgentReportResult{}, knownAgentReportFailure("begin Agent report", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := s.queries.WithTx(tx)

	// Phase 1: pre-read immutable bindings only to choose the allocation lock path.
	pathMachine, err := queries.LoadAgentReportMachine(ctx, command.MachineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return machine.AgentReportResult{}, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.AgentReportResult{}, knownAgentReportFailure("load Agent report path", err)
	}
	bindings, err := queries.ListMachineAgentBindings(ctx, command.MachineID)
	if err != nil {
		return machine.AgentReportResult{}, knownAgentReportFailure("load Agent bindings", err)
	}
	existingBindings := make(map[agentBinding]struct{}, len(bindings))
	for _, binding := range bindings {
		existingBindings[agentBinding{adapterKey: agent.AdapterKey(binding.AdapterKey),
			occurrenceKey: agent.OccurrenceKey(binding.OccurrenceKey)}] = struct{}{}
	}
	needsAllocation := false
	for _, observation := range command.Recognized {
		if _, exists := existingBindings[agentBinding{adapterKey: observation.AdapterKey,
			occurrenceKey: observation.OccurrenceKey}]; !exists {
			needsAllocation = true
			break
		}
	}

	// Phase 2: lock allocation authority only when a recognized binding is missing.
	approverActive := true
	if needsAllocation {
		if _, err := queries.LockSpaceForAgentAllocation(ctx, pathMachine.SpaceID); err != nil {
			return machine.AgentReportResult{}, knownAgentReportFailure("lock Agent allocation Space", err)
		}
		membership, membershipErr := queries.LockAgentApproverMembership(ctx, dbsqlc.LockAgentApproverMembershipParams{
			SpaceID: pathMachine.SpaceID,
			UserID:  pathMachine.EnrolledByUserID,
		})
		if errors.Is(membershipErr, pgx.ErrNoRows) {
			approverActive = false
		} else if membershipErr != nil {
			return machine.AgentReportResult{}, knownAgentReportFailure("lock Agent approving Membership", membershipErr)
		} else {
			approverActive = !membership.RevokedAt.Valid
		}
	}

	// Phase 3: lock and authenticate the exact Machine, then arbitrate replay/staleness.
	storedMachine, err := queries.LockMachineForAgentReport(ctx, command.MachineID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && storedMachine.CertificateSerial != command.CertificateSerial) {
		return machine.AgentReportResult{}, machine.ErrMachineUnavailable
	}
	if err != nil {
		return machine.AgentReportResult{}, knownAgentReportFailure("lock Agent report Machine", err)
	}
	if storedMachine.RevokedAt.Valid {
		return machine.AgentReportResult{}, machine.ErrMachineRevoked
	}
	if storedMachine.LastAgentReportID.Valid && uuidValue(storedMachine.LastAgentReportID) == command.ReportID {
		if !bytes.Equal(storedMachine.LastAgentReportDigest, command.RequestDigest[:]) {
			return machine.AgentReportResult{}, machine.ErrAgentReportConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return machine.AgentReportResult{}, fmt.Errorf("commit Agent report replay: %w", err)
		}
		return machine.AgentReportResult{
			Revision:                 storedMachine.AgentReportRevision,
			UnsupportedAdapterKeys:   adapterKeys(storedMachine.LastAgentReportUnsupportedKeys),
			SetupRequiredAdapterKeys: adapterKeys(storedMachine.LastAgentReportSetupRequiredKeys),
		}, nil
	}
	if command.BaseRevision != storedMachine.AgentReportRevision {
		return machine.AgentReportResult{}, machine.AgentReportStaleError{CurrentRevision: storedMachine.AgentReportRevision}
	}

	// Phase 4: lock the complete Agent set in ascending identity order.
	lockedAgents, err := queries.LockAgentsForMachine(ctx, command.MachineID)
	if err != nil {
		return machine.AgentReportResult{}, knownAgentReportFailure("lock Machine Agents", err)
	}
	byBinding := make(map[agentBinding]dbsqlc.LockAgentsForMachineRow, len(lockedAgents))
	for _, stored := range lockedAgents {
		byBinding[agentBinding{adapterKey: agent.AdapterKey(stored.AdapterKey),
			occurrenceKey: agent.OccurrenceKey(stored.OccurrenceKey)}] = stored
	}

	// Phase 5: allocate missing immutable identities or record the one setup recovery.
	setupRequiredSet := make(map[agent.AdapterKey]struct{})
	for _, observation := range command.Recognized {
		binding := agentBinding{adapterKey: observation.AdapterKey,
			occurrenceKey: observation.OccurrenceKey}
		if _, exists := byBinding[binding]; exists {
			continue
		}
		if !approverActive {
			setupRequiredSet[observation.AdapterKey] = struct{}{}
			continue
		}
		name, nameKey, allocationErr := allocateAgentName(ctx, queries, pathMachine.SpaceID, observation.NameBase)
		if allocationErr != nil {
			return machine.AgentReportResult{}, knownAgentReportFailure("allocate Agent name", allocationErr)
		}
		agentID := uuid.NewString()
		created, createErr := queries.InsertAgent(ctx, dbsqlc.InsertAgentParams{
			AgentID:       agentID,
			SpaceID:       pathMachine.SpaceID,
			MachineID:     command.MachineID,
			OwnerUserID:   pathMachine.EnrolledByUserID,
			AdapterKey:    string(observation.AdapterKey),
			OccurrenceKey: string(observation.OccurrenceKey),
			Name:          name,
			NameKey:       nameKey,
		})
		if createErr != nil {
			return machine.AgentReportResult{}, fmt.Errorf("insert Agent identity: %w", createErr)
		}
		if err := queries.InsertAgentPresence(ctx, agentID); err != nil {
			return machine.AgentReportResult{}, fmt.Errorf("insert Agent presence: %w", err)
		}
		byBinding[binding] = dbsqlc.LockAgentsForMachineRow{
			AgentID:       created.AgentID,
			SpaceID:       created.SpaceID,
			MachineID:     created.MachineID,
			OwnerUserID:   created.OwnerUserID,
			AdapterKey:    created.AdapterKey,
			OccurrenceKey: created.OccurrenceKey,
			Name:          created.Name,
			NameKey:       created.NameKey,
			CreatedAt:     created.CreatedAt,
			RemovedAt:     created.RemovedAt,
		}
	}
	setupRequired := make([]agent.AdapterKey, 0, len(setupRequiredSet))
	for key := range setupRequiredSet {
		setupRequired = append(setupRequired, key)
	}
	slices.Sort(setupRequired)

	// Phase 6: replace Active presence completely, then record the database-time winner.
	if err := queries.SetMachineActiveAgentsAbsent(ctx, command.MachineID); err != nil {
		return machine.AgentReportResult{}, fmt.Errorf("clear Machine Agent presence: %w", err)
	}
	for _, observation := range command.Recognized {
		if !observation.Present {
			continue
		}
		stored, exists := byBinding[agentBinding{adapterKey: observation.AdapterKey,
			occurrenceKey: observation.OccurrenceKey}]
		if !exists || stored.RemovedAt.Valid {
			continue
		}
		if _, err := queries.SetActiveAgentPresent(ctx, stored.AgentID); err != nil {
			return machine.AgentReportResult{}, fmt.Errorf("set Agent present: %w", err)
		}
	}
	unsupported := stringAdapterKeys(command.UnsupportedAdapterKeys)
	setup := stringAdapterKeys(setupRequired)
	reportID, err := postgresUUID(command.ReportID)
	if err != nil {
		return machine.AgentReportResult{}, fmt.Errorf("parse Agent report identity: %w", err)
	}
	revision, err := queries.UpdateMachineAgentReport(ctx, dbsqlc.UpdateMachineAgentReportParams{
		ReportID:          reportID,
		ReportDigest:      command.RequestDigest[:],
		UnsupportedKeys:   unsupported,
		SetupRequiredKeys: setup,
		MachineID:         command.MachineID,
	})
	if err != nil {
		return machine.AgentReportResult{}, fmt.Errorf("record Agent report winner: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return machine.AgentReportResult{}, fmt.Errorf("commit Agent report: %w", err)
	}
	return machine.AgentReportResult{
		Revision:                 revision,
		UnsupportedAdapterKeys:   append([]agent.AdapterKey(nil), command.UnsupportedAdapterKeys...),
		SetupRequiredAdapterKeys: setupRequired,
	}, nil
}

func allocateAgentName(ctx context.Context, queries *dbsqlc.Queries, spaceID, nameBase string) (string, string, error) {
	for ordinal := 1; ; ordinal++ {
		name, nameKey, err := agent.NameForOrdinal(nameBase, ordinal)
		if err != nil {
			return "", "", err
		}
		exists, err := queries.AgentNameExists(ctx, dbsqlc.AgentNameExistsParams{SpaceID: spaceID,
			NameKey: nameKey})
		if err != nil {
			return "", "", err
		}
		if !exists {
			return name, nameKey, nil
		}
	}
}

func knownAgentReportFailure(operation string, err error) error {
	return fmt.Errorf("%w: %s: %v", machine.ErrAgentReportTemporarilyUnavailable, operation, err)
}

func stringAdapterKeys(keys []agent.AdapterKey) []string {
	result := make([]string, len(keys))
	for index, key := range keys {
		result[index] = string(key)
	}
	return result
}

func adapterKeys(keys []string) []agent.AdapterKey {
	result := make([]agent.AdapterKey, len(keys))
	for index, key := range keys {
		result[index] = agent.AdapterKey(key)
	}
	return result
}

func listInventoryAgents(ctx context.Context, queries *dbsqlc.Queries, machineIDs []string) ([]agent.InventoryRecord, error) {
	if len(machineIDs) == 0 {
		return []agent.InventoryRecord{}, nil
	}
	rows, err := queries.ListInventoryAgents(ctx, dbsqlc.ListInventoryAgentsParams{
		FreshnessSeconds: agentFreshnessSeconds(machine.AgentPresenceFreshness),
		MachineIds:       machineIDs,
	})
	if err != nil {
		return nil, err
	}
	records := make([]agent.InventoryRecord, 0, len(rows))
	for _, row := range rows {
		record, err := inventoryAgent(row)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func inventoryAgent(row dbsqlc.ListInventoryAgentsRow) (agent.InventoryRecord, error) {
	lifecycle := agent.LifecycleActive
	if row.RemovedAt.Valid {
		lifecycle = agent.LifecycleRemoved
	}
	return agent.ProjectInventory(agent.Agent{
		AgentID:     row.AgentID,
		MachineID:   row.MachineID,
		OwnerUserID: row.OwnerUserID,
		Name:        row.Name,
		Lifecycle:   lifecycle,
	}, row.OwnerName, agent.Presence{Online: row.Online != nil && *row.Online,
		LastActiveAt: postgresTimePointer(row.LastPresentAt)})
}

func agentFreshnessSeconds(duration time.Duration) int32 {
	return int32(duration / time.Second)
}
