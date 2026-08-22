//go:build integration

package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMachineConnectionDatabaseOwnsCadenceSingleWinnerReplayInventoryAndRevocation(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Ada", SpaceName: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1,$2,'email',transaction_timestamp()+interval '1 hour')`, sessionID, member.UserID); err != nil {
		t.Fatal(err)
	}
	connections := testMachineConnections(t, store)
	requestID, code, secret, request := signedMachineConnectionRequest(t, "https://carry.example", "Desk Mac")
	begun, err := connections.Begin(ctx, request)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if begun.RequestID != requestID {
		t.Fatalf("request ID = %q", begun.RequestID)
	}
	if _, err := connections.Poll(ctx, secret); !errors.Is(err, machine.ErrConnectionSlowDown) {
		t.Fatalf("initial poll = %v, want database slow_down", err)
	}
	if _, err := pool.Exec(ctx, `update machine_connection_requests set created_at=created_at-interval '40 seconds', last_polled_at=null where request_id=$1`, requestID); err != nil {
		t.Fatal(err)
	}
	if _, err := connections.Poll(ctx, secret); !errors.Is(err, machine.ErrConnectionPending) {
		t.Fatalf("pending poll = %v", err)
	}
	preview, err := connections.Lookup(ctx, machine.LookupConnectionRequest{BrowserSessionID: sessionID, UserCode: code, Source: "198.51.100.10"})
	if err != nil || preview.Fingerprint != begun.Fingerprint || preview.Server != "https://carry.example" {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if err := connections.Approve(ctx, machine.DecideConnectionRequest{
		BrowserSessionID: sessionID, RequestID: requestID, UserCode: code, SpaceID: member.SpaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := pool.Exec(ctx, `update machine_connection_requests set last_polled_at=transaction_timestamp()-interval '40 seconds' where request_id=$1`, requestID); err != nil {
		t.Fatal(err)
	}

	const callers = 6
	results := make(chan machine.ConnectedMachine, callers)
	errs := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			connected, pollErr := connections.Poll(ctx, secret)
			if pollErr != nil {
				errs <- pollErr
				return
			}
			results <- connected
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for pollErr := range errs {
		t.Fatalf("concurrent redeem: %v", pollErr)
	}
	var winner machine.ConnectedMachine
	for connected := range results {
		if winner.MachineID == "" {
			winner = connected
		}
		if connected.MachineID != winner.MachineID || string(connected.CertificatePEM) != string(winner.CertificatePEM) {
			t.Fatalf("redeem returned different winner: %#v vs %#v", connected, winner)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from machines where space_id=$1`, member.SpaceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Machine count = %d, %v", count, err)
	}
	page, _, err := connections.List(ctx, sessionID, member.SpaceID, "")
	if err != nil || len(page.Machines) != 1 || page.Machines[0].Fingerprint != begun.Fingerprint || page.Machines[0].State != "Active" {
		t.Fatalf("inventory = %#v, %v", page, err)
	}
	revoked, _, err := connections.RevokeFromBrowser(ctx, sessionID, member.SpaceID, winner.MachineID, uuid.NewString())
	if err != nil || revoked.State != "Revoked" || revoked.RevocationActor != "user" || revoked.RevokedByUserID != member.UserID {
		t.Fatalf("Browser revocation = %#v, %v", revoked, err)
	}
	if _, err := connections.Poll(ctx, secret); !errors.Is(err, machine.ErrMachineUnavailable) {
		t.Fatalf("revoked certificate replay = %v", err)
	}
}

func TestMachineConnectionCancellationAndDecisionRaceHasOneDatabaseWinner(t *testing.T) {
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: "Grace", SpaceName: "Compilers"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1,$2,'email',transaction_timestamp()+interval '1 hour')`, sessionID, member.UserID); err != nil {
		t.Fatal(err)
	}
	connections := testMachineConnections(t, store)
	requestID, code, secret, request := signedMachineConnectionRequest(t, "https://carry.example", "Race Mac")
	if _, err := connections.Begin(ctx, request); err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan error, 2)
	go func() { outcomes <- connections.Cancel(ctx, secret) }()
	go func() {
		outcomes <- connections.Approve(ctx, machine.DecideConnectionRequest{BrowserSessionID: sessionID, RequestID: requestID, UserCode: code, SpaceID: member.SpaceID, IdempotencyKey: uuid.NewString()})
	}()
	first, second := <-outcomes, <-outcomes
	if first != nil && second != nil {
		t.Fatalf("both race participants lost: %v, %v", first, second)
	}
	var decision *string
	var cancelledAt *time.Time
	if err := pool.QueryRow(ctx, `select decision, cancelled_at from machine_connection_requests where request_id=$1`, requestID).Scan(&decision, &cancelledAt); err != nil {
		t.Fatal(err)
	}
	if (decision != nil) == (cancelledAt != nil) {
		t.Fatalf("decision=%v cancelled_at=%v, want exactly one", decision, cancelledAt)
	}
}

func TestMachineConnectionDecisionAndReplayBoundaries(t *testing.T) {
	fixture := newMachineConnectionFixture(t, "Boundary")
	requestID, code, secret, request := signedMachineConnectionRequest(t, "https://carry.example", "Boundary Mac")
	if _, err := fixture.connections.Begin(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	var machineCount int
	if err := fixture.pool.QueryRow(fixture.ctx, `select count(*) from machines`).Scan(&machineCount); err != nil || machineCount != 0 {
		t.Fatalf("pre-approval Machine count = %d, %v", machineCount, err)
	}
	replay := request
	replay.IdempotencyKey = uuid.NewString()
	if _, err := fixture.connections.Begin(fixture.ctx, replay); !errors.Is(err, machine.ErrConnectionConflict) {
		t.Fatalf("changed begin replay = %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `update machine_connection_requests set expires_at=transaction_timestamp() where request_id=$1`, requestID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.connections.Approve(fixture.ctx, machine.DecideConnectionRequest{
		BrowserSessionID: fixture.sessionID, RequestID: requestID, UserCode: code,
		SpaceID: fixture.member.SpaceID, IdempotencyKey: uuid.NewString(),
	}); !errors.Is(err, machine.ErrConnectionExpired) {
		t.Fatalf("expired approval = %v", err)
	}
	if err := fixture.pool.QueryRow(fixture.ctx, `select count(*) from machines`).Scan(&machineCount); err != nil || machineCount != 0 {
		t.Fatalf("expired approval Machine count = %d, %v", machineCount, err)
	}
	if _, err := fixture.connections.Poll(fixture.ctx, secret); !errors.Is(err, machine.ErrConnectionExpired) {
		t.Fatalf("expired poll = %v", err)
	}

	connected, _, replaySecret := connectMachineForTest(t, fixture, "Replay Mac")
	if _, err := fixture.connections.Poll(fixture.ctx, replaySecret); err != nil {
		t.Fatalf("exact certificate replay: %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `update machine_connection_requests set replay_until=transaction_timestamp() where resulting_machine_id=$1`, connected.MachineID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.connections.Poll(fixture.ctx, replaySecret); !errors.Is(err, machine.ErrConnectionReplayExpired) {
		t.Fatalf("expired certificate replay = %v", err)
	}
}

func TestMachineConnectionApproveDenyRaceHasOneDatabaseWinner(t *testing.T) {
	fixture := newMachineConnectionFixture(t, "Decision Race")
	requestID, code, secret, request := signedMachineConnectionRequest(t, "https://carry.example", "Decision Race Mac")
	if _, err := fixture.connections.Begin(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		decision string
		err      error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		outcomes <- outcome{decision: "approved", err: fixture.connections.Approve(fixture.ctx, machine.DecideConnectionRequest{
			BrowserSessionID: fixture.sessionID, RequestID: requestID, UserCode: code,
			SpaceID: fixture.member.SpaceID, IdempotencyKey: uuid.NewString(),
		})}
	}()
	go func() {
		outcomes <- outcome{decision: "denied", err: fixture.connections.Deny(fixture.ctx, machine.DecideConnectionRequest{
			BrowserSessionID: fixture.sessionID, RequestID: requestID, UserCode: code,
			IdempotencyKey: uuid.NewString(),
		})}
	}()
	first, second := <-outcomes, <-outcomes
	winners := 0
	for _, result := range []outcome{first, second} {
		if result.err == nil {
			winners++
		} else if !errors.Is(result.err, machine.ErrConnectionAlreadyDecided) {
			t.Fatalf("%s decision error = %v", result.decision, result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("decision winners = %d, outcomes = %#v / %#v", winners, first, second)
	}
	var decision string
	if err := fixture.pool.QueryRow(fixture.ctx, `select decision from machine_connection_requests where request_id=$1`, requestID).Scan(&decision); err != nil {
		t.Fatal(err)
	}
	if decision == "approved" {
		makeMachineConnectionPollReady(t, fixture, requestID)
		if _, err := fixture.connections.Poll(fixture.ctx, secret); err != nil {
			t.Fatalf("approved winner did not redeem: %v", err)
		}
	} else if _, err := fixture.connections.Poll(fixture.ctx, secret); !errors.Is(err, machine.ErrConnectionDenied) {
		t.Fatalf("denied winner poll = %v", err)
	}
}

func TestMachineConnectionApprovalAndMemberRemovalHaveOneValidOrder(t *testing.T) {
	fixture := newMachineConnectionFixture(t, "Removal Race")
	managerID := seedRemovalMember(t, fixture.ctx, fixture.pool, fixture.member.SpaceID, true, true)
	requestID, code, secret, request := signedMachineConnectionRequest(t, "https://carry.example", "Removal Race Mac")
	if _, err := fixture.connections.Begin(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		operation string
		err       error
	}
	removeCommand := removalCommand(t, space.RemoveMemberRequest{
		SpaceID: fixture.member.SpaceID, ActorUserID: managerID, TargetUserID: fixture.member.UserID,
		IdempotencyKey: uuid.NewString(),
	})
	outcomes := make(chan outcome, 2)
	go func() {
		outcomes <- outcome{operation: "approve", err: fixture.connections.Approve(fixture.ctx, machine.DecideConnectionRequest{
			BrowserSessionID: fixture.sessionID, RequestID: requestID, UserCode: code,
			SpaceID: fixture.member.SpaceID, IdempotencyKey: uuid.NewString(),
		})}
	}()
	go func() {
		outcomes <- outcome{operation: "remove", err: fixture.store.RemoveSpaceMember(fixture.ctx, removeCommand)}
	}()
	results := map[string]error{}
	for range 2 {
		result := <-outcomes
		results[result.operation] = result.err
	}
	if results["remove"] != nil {
		t.Fatalf("member removal lost: %v", results["remove"])
	}
	var decision *string
	if err := fixture.pool.QueryRow(fixture.ctx, `select decision from machine_connection_requests where request_id=$1`, requestID).Scan(&decision); err != nil {
		t.Fatal(err)
	}
	if decision != nil {
		if *decision != "approved" || results["approve"] != nil {
			t.Fatalf("committed decision/results = %v/%v", decision, results["approve"])
		}
		makeMachineConnectionPollReady(t, fixture, requestID)
		if _, err := fixture.connections.Poll(fixture.ctx, secret); err != nil {
			t.Fatalf("approval-before-removal consequence was lost: %v", err)
		}
	} else if !errors.Is(results["approve"], machine.ErrMachineAuthority) {
		t.Fatalf("removal-before-approval error = %v", results["approve"])
	}
}

func TestMachineRemoteAndSelfRevocationHaveOneDatabaseWinner(t *testing.T) {
	fixture := newMachineConnectionFixture(t, "Revoke Race")
	connected, _, _ := connectMachineForTest(t, fixture, "Revoke Race Mac")
	var serial string
	if err := fixture.pool.QueryRow(fixture.ctx, `select certificate_serial from machines where machine_id=$1`, connected.MachineID).Scan(&serial); err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan error, 2)
	go func() {
		_, _, err := fixture.connections.RevokeFromBrowser(fixture.ctx, fixture.sessionID, fixture.member.SpaceID, connected.MachineID, uuid.NewString())
		outcomes <- err
	}()
	go func() {
		_, err := fixture.connections.RevokeFromHost(fixture.ctx, connected.MachineID, serial, uuid.NewString())
		outcomes <- err
	}()
	if first, second := <-outcomes, <-outcomes; first != nil || second != nil {
		t.Fatalf("revocation race errors = %v / %v", first, second)
	}
	var actor string
	var revokedAt time.Time
	if err := fixture.pool.QueryRow(fixture.ctx, `select revocation_actor_kind, revoked_at from machines where machine_id=$1`, connected.MachineID).Scan(&actor, &revokedAt); err != nil {
		t.Fatal(err)
	}
	if actor != "user" && actor != "machine" || revokedAt.IsZero() {
		t.Fatalf("revocation winner = %q at %s", actor, revokedAt)
	}

	first, err := enrollMachineForTest(fixture.ctx, fixture.store, testMachineCommand{
		SpaceID: fixture.member.SpaceID, DisplayName: "Replay Revoke", EnrolledByUserID: fixture.member.UserID,
		PublicKeyDER: []byte("replay-revoke-key"), CertificatePEM: []byte("replay-revoke-cert"),
	})
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	revoked, _, err := fixture.connections.RevokeFromBrowser(fixture.ctx, fixture.sessionID, fixture.member.SpaceID, first.MachineID, key)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _, err := fixture.connections.RevokeFromBrowser(fixture.ctx, fixture.sessionID, fixture.member.SpaceID, first.MachineID, key)
	if err != nil || replayed.RevokedAt == nil || revoked.RevokedAt == nil || !replayed.RevokedAt.Equal(*revoked.RevokedAt) {
		t.Fatalf("revocation replay = %#v / %#v / %v", revoked, replayed, err)
	}
	second, err := enrollMachineForTest(fixture.ctx, fixture.store, testMachineCommand{
		SpaceID: fixture.member.SpaceID, DisplayName: "Conflict Revoke", EnrolledByUserID: fixture.member.UserID,
		PublicKeyDER: []byte("conflict-revoke-key"), CertificatePEM: []byte("conflict-revoke-cert"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.connections.RevokeFromBrowser(fixture.ctx, fixture.sessionID, fixture.member.SpaceID, second.MachineID, key); !errors.Is(err, machine.ErrConnectionConflict) {
		t.Fatalf("reused revocation idempotency key = %v", err)
	}
}

type machineConnectionFixture struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	store       *Store
	member      testMember
	sessionID   string
	connections *machine.Connections
}

func newMachineConnectionFixture(t *testing.T, name string) machineConnectionFixture {
	t.Helper()
	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	member, err := createMemberForTest(ctx, store, testMemberCommand{DisplayName: name + " Member", SpaceName: name + " Space"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id,user_id,identity_proof_method,expires_at) values ($1,$2,'email',transaction_timestamp()+interval '1 hour')`, sessionID, member.UserID); err != nil {
		t.Fatal(err)
	}
	return machineConnectionFixture{ctx: ctx, pool: pool, store: store, member: member, sessionID: sessionID, connections: testMachineConnections(t, store)}
}

func connectMachineForTest(t *testing.T, fixture machineConnectionFixture, name string) (machine.ConnectedMachine, string, string) {
	t.Helper()
	requestID, code, secret, request := signedMachineConnectionRequest(t, "https://carry.example", name)
	if _, err := fixture.connections.Begin(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := fixture.connections.Approve(fixture.ctx, machine.DecideConnectionRequest{
		BrowserSessionID: fixture.sessionID, RequestID: requestID, UserCode: code,
		SpaceID: fixture.member.SpaceID, IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatal(err)
	}
	makeMachineConnectionPollReady(t, fixture, requestID)
	connected, err := fixture.connections.Poll(fixture.ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	return connected, code, secret
}

func makeMachineConnectionPollReady(t *testing.T, fixture machineConnectionFixture, requestID string) {
	t.Helper()
	if _, err := fixture.pool.Exec(fixture.ctx, `update machine_connection_requests set created_at=created_at-interval '40 seconds', last_polled_at=transaction_timestamp()-interval '40 seconds' where request_id=$1`, requestID); err != nil {
		t.Fatal(err)
	}
}

func testMachineConnections(t *testing.T, store *Store) *machine.Connections {
	t.Helper()
	root, err := machine.ParseConnectionRoot(base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 21, 6, 0, 0, 0, time.UTC)
	bundle, err := machine.CreateCertificateBundle([]string{"carry.example"}, now)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := machine.LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	hostAPIOrigin, err := machine.ParseHostAPIOrigin("https://api.carry.example")
	if err != nil {
		t.Fatal(err)
	}
	connections, err := machine.NewConnections(store, root, authority, "https://carry.example", hostAPIOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return connections
}

func signedMachineConnectionRequest(t *testing.T, origin, name string) (string, string, string, machine.BeginConnectionRequest) {
	t.Helper()
	requestID := uuid.NewString()
	const alphabet = "BCDFGHJKLMNPQRSTVWXZ"
	codeBytes := make([]byte, 10)
	if _, err := rand.Read(codeBytes); err != nil {
		t.Fatal(err)
	}
	for index := range codeBytes {
		codeBytes[index] = alphabet[int(codeBytes[index])%len(alphabet)]
	}
	code := string(codeBytes[:4]) + "-" + string(codeBytes[4:7]) + "-" + string(codeBytes[7:])
	secret := "carry_machine_connect_" + requestID + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	proof := ed25519.Sign(privateKey, machine.ConnectionKeyProofMessage(origin, requestID, name, publicKeyDER, code, secret))
	return requestID, code, secret, machine.BeginConnectionRequest{
		RequestID: requestID, IdempotencyKey: uuid.NewString(), DisplayName: name, UserCode: code, PollSecret: secret,
		Source: "198.51.100.10", Origin: origin, PublicKeyDER: publicKeyDER, KeyProof: proof,
	}
}
