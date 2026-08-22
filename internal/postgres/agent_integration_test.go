//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentReportOwnsGenericIdentityReplayReplacementNamesAndFreshness(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Agent Owner",
		SpaceName: "Agent Lab"})
	if err != nil {
		t.Fatal(err)
	}
	vocabulary, err := agent.NewVocabulary(
		agent.Descriptor{Key: "pi",
			NameBase:                "Pi",
			MaxOccurrencesPerReport: 1},
		agent.Descriptor{Key: "harness",
			NameBase:                "Harness",
			MaxOccurrencesPerReport: 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := machine.NewAgentPresence(store, vocabulary)
	if err != nil {
		t.Fatal(err)
	}
	first := enrollAgentMachine(t, ctx, store, member.SpaceID, member.UserID, "agent-serial-1")
	second := enrollAgentMachine(t, ctx, store, member.SpaceID, member.UserID, "agent-serial-2")

	request := machine.AgentReportRequest{
		MachineID:         first.MachineID,
		CertificateSerial: "agent-serial-1",
		ReportID:          uuid.NewString(),
		BaseRevision:      0,
		Observations: []machine.AgentObservation{
			{AdapterKey: "future",
				OccurrenceKey: "default",
				Present:       true},
			{AdapterKey: "harness",
				OccurrenceKey: "beta",
				Present:       false},
			{AdapterKey: "pi",
				OccurrenceKey: "default",
				Present:       true},
			{AdapterKey: "harness",
				OccurrenceKey: "alpha",
				Present:       true},
		},
	}
	result, err := reports.Report(ctx, request)
	if err != nil || result.Revision != 1 || len(result.UnsupportedAdapterKeys) != 1 || result.UnsupportedAdapterKeys[0] != "future" {
		t.Fatalf("initial report = %#v, %v", result, err)
	}
	type storedAgent struct {
		adapter, occurrence, name, owner string
		removed, present                 bool
		lastPresent                      *time.Time
	}
	load := func(machineID string) []storedAgent {
		rows, queryErr := pool.Query(ctx, `
			select a.adapter_key,a.occurrence_key,a.name,a.owner_user_id,
				a.removed_at is not null,p.present,p.last_present_at
			from agents a join agent_presence p using(agent_id)
			where a.machine_id=$1 order by a.adapter_key,a.occurrence_key
		`, machineID)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		defer rows.Close()
		var got []storedAgent
		for rows.Next() {
			var row storedAgent
			if err := rows.Scan(&row.adapter, &row.occurrence, &row.name, &row.owner, &row.removed, &row.present, &row.lastPresent); err != nil {
				t.Fatal(err)
			}
			got = append(got, row)
		}
		return got
	}
	got := load(first.MachineID)
	if len(got) != 3 || got[0].name != "Harness" || got[1].name != "Harness 2" || got[2].name != "Pi" ||
		!got[0].present || got[1].present || !got[2].present || got[0].owner != member.UserID {
		t.Fatalf("generic Agent rows = %#v", got)
	}
	var firstReportedAt time.Time
	if err := pool.QueryRow(ctx, `select agent_reported_at from machines where machine_id=$1`, first.MachineID).Scan(&firstReportedAt); err != nil {
		t.Fatal(err)
	}
	replay, err := reports.Report(ctx, request)
	if err != nil || replay.Revision != 1 {
		t.Fatalf("exact replay = %#v, %v", replay, err)
	}
	var replayReportedAt time.Time
	if err := pool.QueryRow(ctx, `select agent_reported_at from machines where machine_id=$1`, first.MachineID).Scan(&replayReportedAt); err != nil || !replayReportedAt.Equal(firstReportedAt) {
		t.Fatalf("replay refreshed database time: %v / %v / %v", firstReportedAt, replayReportedAt, err)
	}
	changed := request
	changed.Observations = append([]machine.AgentObservation(nil), request.Observations...)
	changed.Observations[0].Present = false
	if _, err := reports.Report(ctx, changed); !errors.Is(err, machine.ErrAgentReportConflict) {
		t.Fatalf("changed replay = %v", err)
	}
	stale := request
	stale.ReportID = uuid.NewString()
	stale.Observations = nil
	if _, err := reports.Report(ctx, stale); !isStaleRevision(err, 1) {
		t.Fatalf("stale report = %v", err)
	}

	secondResult, err := reports.Report(ctx, machine.AgentReportRequest{
		MachineID:         second.MachineID,
		CertificateSerial: "agent-serial-2",
		ReportID:          uuid.NewString(),
		BaseRevision:      0,
		Observations: []machine.AgentObservation{{AdapterKey: "pi",
			OccurrenceKey: "default",
			Present:       true}},
	})
	if err != nil || secondResult.Revision != 1 {
		t.Fatalf("second Host report = %#v, %v", secondResult, err)
	}
	secondAgents := load(second.MachineID)
	if len(secondAgents) != 1 || secondAgents[0].name != "Pi 2" {
		t.Fatalf("independent family name = %#v", secondAgents)
	}

	empty, err := reports.Report(ctx, machine.AgentReportRequest{
		MachineID:         first.MachineID,
		CertificateSerial: "agent-serial-1",
		ReportID:          uuid.NewString(),
		BaseRevision:      1,
		Observations:      []machine.AgentObservation{},
	})
	if err != nil || empty.Revision != 2 {
		t.Fatalf("empty complete report = %#v, %v", empty, err)
	}
	for _, row := range load(first.MachineID) {
		if row.present {
			t.Fatalf("empty report retained present Agent: %#v", row)
		}
	}
	presentAgain, err := reports.Report(ctx, machine.AgentReportRequest{
		MachineID:         first.MachineID,
		CertificateSerial: "agent-serial-1",
		ReportID:          uuid.NewString(),
		BaseRevision:      2,
		Observations: []machine.AgentObservation{{AdapterKey: "harness",
			OccurrenceKey: "alpha",
			Present:       true}},
	})
	if err != nil || presentAgain.Revision != 3 {
		t.Fatalf("presence replacement = %#v, %v", presentAgain, err)
	}
	var expectedLastActive time.Time
	if err := pool.QueryRow(ctx, `
		select presence.last_present_at
		from agents as stored_agent
		join agent_presence as presence using(agent_id)
		where stored_agent.machine_id=$1 and stored_agent.adapter_key='harness'
			and stored_agent.occurrence_key='alpha'
	`, first.MachineID).Scan(&expectedLastActive); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions(session_id,user_id,identity_proof_method,expires_at) values($1,$2,'email',transaction_timestamp()+interval '1 hour')`, sessionID, member.UserID); err != nil {
		t.Fatal(err)
	}
	connections := testMachineConnections(t, store)
	for _, boundary := range []struct {
		ageSeconds int
		wantOnline bool
	}{
		{
			ageSeconds: 44,
			wantOnline: true,
		},
		{
			ageSeconds: 46,
			wantOnline: false,
		},
	} {
		if _, err := pool.Exec(ctx, `
			update machines
			set agent_reported_at=transaction_timestamp()-make_interval(secs => $2::int)
			where machine_id=$1
		`, first.MachineID, boundary.ageSeconds); err != nil {
			t.Fatal(err)
		}
		page, inventory, err := connections.List(ctx, sessionID, member.SpaceID, "")
		if err != nil || len(page.Machines) != 2 || len(inventory) != 4 {
			t.Fatalf("owner-separated inventory = %#v / %#v / %v", page, inventory, err)
		}
		var harnessAlpha *agent.InventoryRecord
		for index := range inventory {
			if inventory[index].MachineID == first.MachineID && inventory[index].Name == "Harness" {
				harnessAlpha = &inventory[index]
				break
			}
		}
		if harnessAlpha == nil || harnessAlpha.Online != boundary.wantOnline || harnessAlpha.LastActiveAt == nil ||
			!harnessAlpha.LastActiveAt.Equal(expectedLastActive) {
			t.Fatalf("%ds database freshness boundary = %#v, last active = %s", boundary.ageSeconds, harnessAlpha, expectedLastActive)
		}
	}
	if _, err := pool.Exec(ctx, `update agents set removed_at=transaction_timestamp() where machine_id=$1 and adapter_key='pi'`, first.MachineID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update agent_presence set present=false where agent_id in(select agent_id from agents where machine_id=$1 and adapter_key='pi')`, first.MachineID); err != nil {
		t.Fatal(err)
	}
	third := enrollAgentMachine(t, ctx, store, member.SpaceID, member.UserID, "agent-serial-3")
	if _, err := reports.Report(ctx, reportRequest(third.MachineID, "agent-serial-3", 0,
		machine.AgentObservation{AdapterKey: "pi",
			OccurrenceKey: "default",
			Present:       true})); err != nil {
		t.Fatal(err)
	}
	thirdAgents := load(third.MachineID)
	if len(thirdAgents) != 1 || thirdAgents[0].name != "Pi 3" {
		t.Fatalf("Removed Agent name was reused: %#v", thirdAgents)
	}
}

func TestAgentReportInactiveApproverRemovedIdentityAndConcurrentWinners(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Lifecycle Owner",
		SpaceName: "Lifecycle Space"})
	if err != nil {
		t.Fatal(err)
	}
	vocabulary, _ := agent.NewVocabulary(
		agent.Descriptor{Key: "pi",
			NameBase:                "Pi",
			MaxOccurrencesPerReport: 1},
		agent.Descriptor{Key: "harness",
			NameBase:                "Harness",
			MaxOccurrencesPerReport: 1},
	)
	reports, _ := machine.NewAgentPresence(store, vocabulary)
	storedMachine := enrollAgentMachine(t, ctx, store, member.SpaceID, member.UserID, "lifecycle-serial")
	initial, err := reports.Report(ctx, reportRequest(storedMachine.MachineID, "lifecycle-serial", 0,
		machine.AgentObservation{AdapterKey: "pi",
			OccurrenceKey: "default",
			Present:       true}))
	if err != nil || initial.Revision != 1 {
		t.Fatal(initial, err)
	}
	if _, err := pool.Exec(ctx, `update space_memberships set revoked_at=transaction_timestamp() where space_id=$1 and user_id=$2`, member.SpaceID, member.UserID); err != nil {
		t.Fatal(err)
	}
	inactive, err := reports.Report(ctx, reportRequest(storedMachine.MachineID, "lifecycle-serial", 1,
		machine.AgentObservation{AdapterKey: "pi",
			OccurrenceKey: "default",
			Present:       true},
		machine.AgentObservation{AdapterKey: "harness",
			OccurrenceKey: "default",
			Present:       true}))
	if err != nil || inactive.Revision != 2 || len(inactive.SetupRequiredAdapterKeys) != 1 || inactive.SetupRequiredAdapterKeys[0] != "harness" {
		t.Fatalf("inactive approver result = %#v, %v", inactive, err)
	}
	var piPresent bool
	var harnessCount int
	if err := pool.QueryRow(ctx, `select p.present from agents a join agent_presence p using(agent_id) where a.machine_id=$1 and a.adapter_key='pi'`, storedMachine.MachineID).Scan(&piPresent); err != nil || !piPresent {
		t.Fatalf("existing Agent reconciliation = %t, %v", piPresent, err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from agents where machine_id=$1 and adapter_key='harness'`, storedMachine.MachineID).Scan(&harnessCount); err != nil || harnessCount != 0 {
		t.Fatalf("inactive approver allocated %d Agents, %v", harnessCount, err)
	}
	if _, err := pool.Exec(ctx, `update agents set removed_at=transaction_timestamp() where machine_id=$1 and adapter_key='pi'`, storedMachine.MachineID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update agent_presence set present=false where agent_id in(select agent_id from agents where machine_id=$1)`, storedMachine.MachineID); err != nil {
		t.Fatal(err)
	}
	removedResult, err := reports.Report(ctx, reportRequest(storedMachine.MachineID, "lifecycle-serial", 2,
		machine.AgentObservation{AdapterKey: "pi",
			OccurrenceKey: "default",
			Present:       true}))
	if err != nil || removedResult.Revision != 3 {
		t.Fatalf("Removed report = %#v, %v", removedResult, err)
	}
	var removed, present bool
	if err := pool.QueryRow(ctx, `select a.removed_at is not null,p.present from agents a join agent_presence p using(agent_id) where a.machine_id=$1`, storedMachine.MachineID).Scan(&removed, &present); err != nil || !removed || present {
		t.Fatalf("Removed non-revival = removed %t present %t err %v", removed, present, err)
	}

	activeMember, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Concurrent Owner",
		SpaceName: "Concurrent Space"})
	if err != nil {
		t.Fatal(err)
	}
	concurrentMachine := enrollAgentMachine(t, ctx, store, activeMember.SpaceID, activeMember.UserID, "concurrent-serial")
	concurrentReports, _ := machine.NewAgentPresence(store, agent.NativeVocabulary())
	same := reportRequest(concurrentMachine.MachineID, "concurrent-serial", 0,
		machine.AgentObservation{AdapterKey: "pi",
			OccurrenceKey: "default",
			Present:       true})
	results := make(chan machine.AgentReportResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := concurrentReports.Report(ctx, same)
			results <- result
			errs <- err
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent exact replay = %v", err)
		}
		if result := <-results; result.Revision != 1 {
			t.Fatalf("concurrent replay result = %#v", result)
		}
	}
	winnerErrors := make(chan error, 2)
	for range 2 {
		go func() {
			request := reportRequest(concurrentMachine.MachineID, "concurrent-serial", 1)
			_, err := concurrentReports.Report(ctx, request)
			winnerErrors <- err
		}()
	}
	var successes, stales int
	for range 2 {
		err := <-winnerErrors
		if err == nil {
			successes++
		} else if isStaleRevision(err, 2) {
			stales++
		} else {
			t.Fatalf("concurrent winner error = %v", err)
		}
	}
	if successes != 1 || stales != 1 {
		t.Fatalf("concurrent winners = success %d stale %d", successes, stales)
	}
}

func TestAgentSchemaConstraintsRollbackIncompleteIdentity(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Constraint Owner",
		SpaceName: "Constraint Space"})
	if err != nil {
		t.Fatal(err)
	}
	storedMachine := enrollAgentMachine(t, ctx, store, member.SpaceID, member.UserID, "constraint-serial")
	if _, err := pool.Exec(ctx, `update machines set agent_report_revision=1 where machine_id=$1`, storedMachine.MachineID); err == nil {
		t.Fatal("Machine report revision without replay authority was accepted")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	agentID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		insert into agents(agent_id,space_id,machine_id,owner_user_id,adapter_key,occurrence_key,name,name_key)
		values($1,$2,$3,$4,'pi','default','Pi','pi')
	`, agentID, member.SpaceID, storedMachine.MachineID, member.UserID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into agent_presence(agent_id,present,last_present_at) values($1,true,null)`, agentID); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("present Agent without database last-present time was accepted")
	}
	_ = tx.Rollback(ctx)
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from agents where agent_id=$1`, agentID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed Agent transaction retained %d identities, %v", count, err)
	}
	if _, err := pool.Exec(ctx, `
		insert into agents(agent_id,space_id,machine_id,owner_user_id,adapter_key,occurrence_key,name,name_key)
		values($1,$2,$3,$4,repeat('a',64),'default','Invalid','invalid')
	`, uuid.NewString(), member.SpaceID, storedMachine.MachineID, member.UserID); err == nil {
		t.Fatal("oversize adapter key was accepted")
	}
}

func TestAgentTerminalConsequencesSurviveRevokeRemovalRaces(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	manager := invitationManagerFixture(t, ctx, store)
	target := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	browserSessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions(session_id,user_id,identity_proof_method,expires_at) values($1,$2,'email',transaction_timestamp()+interval '1 hour')`, browserSessionID, manager.UserID); err != nil {
		t.Fatal(err)
	}
	reports, _ := machine.NewAgentPresence(store, agent.NativeVocabulary())
	browserMachine := enrollAgentMachine(t, ctx, store, manager.SpaceID, target, "browser-revoke-serial")
	selfMachine := enrollAgentMachine(t, ctx, store, manager.SpaceID, target, "self-revoke-serial")
	for _, fixture := range []struct{ id, serial string }{{browserMachine.MachineID, "browser-revoke-serial"}, {selfMachine.MachineID, "self-revoke-serial"}} {
		if _, err := reports.Report(ctx, reportRequest(fixture.id, fixture.serial, 0,
			machine.AgentObservation{AdapterKey: "pi",
				OccurrenceKey: "default",
				Present:       true})); err != nil {
			t.Fatal(err)
		}
	}
	connections := testMachineConnections(t, store)
	_, removedAgents, err := connections.RevokeFromBrowser(ctx, browserSessionID, manager.SpaceID, browserMachine.MachineID, uuid.NewString())
	if err != nil {
		t.Fatalf("Browser revoke: %v", err)
	}
	if len(removedAgents) != 1 || removedAgents[0].MachineID != browserMachine.MachineID ||
		removedAgents[0].Lifecycle != agent.LifecycleRemoved || removedAgents[0].Online || removedAgents[0].OwnerUserID != target {
		t.Fatalf("Browser revoke Agent projection = %#v", removedAgents)
	}
	assertMachineAgentsTerminal(t, ctx, pool, browserMachine.MachineID)

	for name, revoke := range map[string]func(machine.MachineRecord, string) error{
		"Browser": func(stored machine.MachineRecord, _ string) error {
			_, _, err := connections.RevokeFromBrowser(ctx, browserSessionID, manager.SpaceID, stored.MachineID, uuid.NewString())
			return err
		},
		"self": func(stored machine.MachineRecord, serial string) error {
			_, err := connections.RevokeFromHost(ctx, stored.MachineID, serial, uuid.NewString())
			return err
		},
	} {
		t.Run("report vs "+name+" revoke", func(t *testing.T) {
			serial := "report-" + name + "-serial"
			stored := enrollAgentMachine(t, ctx, store, manager.SpaceID, manager.UserID, serial)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := reports.Report(ctx, reportRequest(stored.MachineID, serial, 0,
					machine.AgentObservation{AdapterKey: "pi",
						OccurrenceKey: "default",
						Present:       true}))
				results <- err
			}()
			go func() {
				<-start
				results <- revoke(stored, serial)
			}()
			close(start)
			first, second := <-results, <-results
			for _, err := range []error{first, second} {
				if err != nil && !errors.Is(err, machine.ErrMachineRevoked) {
					t.Fatalf("report/%s revoke race = %v / %v", name, first, second)
				}
			}
			assertMachineAgentsTerminal(t, ctx, pool, stored.MachineID)
		})
	}

	remove := removalCommand(t, space.RemoveMemberRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		TargetUserID:   target,
		IdempotencyKey: "agent-race-remove",
	})
	outcomes := make(chan error, 2)
	go func() {
		_, err := connections.RevokeFromHost(ctx, selfMachine.MachineID, "self-revoke-serial", uuid.NewString())
		outcomes <- err
	}()
	go func() { outcomes <- store.RemoveSpaceMember(ctx, remove) }()
	for range 2 {
		if err := <-outcomes; err != nil {
			t.Fatalf("self revoke/member removal race = %v", err)
		}
	}
	assertMachineAgentsTerminal(t, ctx, pool, selfMachine.MachineID)
	var membershipRemoved, machineRevoked bool
	if err := pool.QueryRow(ctx, `select revoked_at is not null from space_memberships where space_id=$1 and user_id=$2`, manager.SpaceID, target).Scan(&membershipRemoved); err != nil || !membershipRemoved {
		t.Fatalf("Membership terminal = %t, %v", membershipRemoved, err)
	}
	if err := pool.QueryRow(ctx, `select revoked_at is not null from machines where machine_id=$1`, selfMachine.MachineID).Scan(&machineRevoked); err != nil || !machineRevoked {
		t.Fatalf("Machine terminal = %t, %v", machineRevoked, err)
	}

	// A removed approver racing a new allocation can either create then be removed,
	// or commit setup-required; it can never leave an Active Agent behind.
	target2 := seedRemovalMember(t, ctx, pool, manager.SpaceID, false, false)
	raceMachine := enrollAgentMachine(t, ctx, store, manager.SpaceID, target2, "report-removal-serial")
	route := removalCommand(t, space.RemoveMemberRequest{
		SpaceID:        manager.SpaceID,
		ActorUserID:    manager.UserID,
		TargetUserID:   target2,
		IdempotencyKey: "report-removal-race",
	})
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, _ = reports.Report(ctx, reportRequest(raceMachine.MachineID, "report-removal-serial", 0,
			machine.AgentObservation{AdapterKey: "pi",
				OccurrenceKey: "default",
				Present:       true}))
	}()
	go func() {
		defer wait.Done()
		<-start
		if err := store.RemoveSpaceMember(ctx, route); err != nil {
			t.Errorf("report/removal race removal: %v", err)
		}
	}()
	close(start)
	wait.Wait()
	var active int
	if err := pool.QueryRow(ctx, `select count(*) from agents where space_id=$1 and owner_user_id=$2 and removed_at is null`, manager.SpaceID, target2).Scan(&active); err != nil || active != 0 {
		t.Fatalf("report/removal race left %d Active Agents, %v", active, err)
	}
}

func reportRequest(machineID, serial string, base int64, observations ...machine.AgentObservation) machine.AgentReportRequest {
	return machine.AgentReportRequest{
		MachineID:         machineID,
		CertificateSerial: serial,
		ReportID:          uuid.NewString(),
		BaseRevision:      base,
		Observations:      observations,
	}
}

func enrollAgentMachine(t *testing.T, ctx context.Context, store *Store, spaceID, ownerUserID, serial string) machine.MachineRecord {
	t.Helper()
	stored, err := enrollMachineForTest(ctx, store, testMachineCommand{
		MachineID:         uuid.NewString(),
		SpaceID:           spaceID,
		DisplayName:       serial,
		PublicKeyDER:      []byte(serial),
		CertificatePEM:    []byte("certificate-" + serial),
		CertificateSerial: serial,
		EnrolledByUserID:  ownerUserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func isStaleRevision(err error, revision int64) bool {
	var stale machine.AgentReportStaleError
	return errors.As(err, &stale) && stale.CurrentRevision == revision
}

func assertMachineAgentsTerminal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, machineID string) {
	t.Helper()
	var active, present int
	if err := pool.QueryRow(ctx, `
		select count(*) filter(where a.removed_at is null), count(*) filter(where p.present)
		from agents a join agent_presence p using(agent_id) where a.machine_id=$1
	`, machineID).Scan(&active, &present); err != nil {
		t.Fatal(err)
	}
	if active != 0 || present != 0 {
		t.Fatalf("terminal Agent facts = active %d present %d", active, present)
	}
}
