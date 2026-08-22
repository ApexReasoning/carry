package machine

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/google/uuid"
)

type connectionPersistenceStub struct {
	beginCommand BeginConnectionCommand
	beginResult  ConnectionRequest
	beginErr     error
}

func (stub *connectionPersistenceStub) BeginMachineConnection(_ context.Context, command BeginConnectionCommand) (ConnectionRequest, error) {
	stub.beginCommand = command
	return stub.beginResult, stub.beginErr
}
func (*connectionPersistenceStub) LookupMachineConnection(context.Context, LookupConnectionCommand) (ConnectionRequest, error) {
	return ConnectionRequest{}, errors.New("unexpected lookup")
}
func (*connectionPersistenceStub) DecideMachineConnection(context.Context, DecideConnectionCommand) (ConnectionRequest, error) {
	return ConnectionRequest{}, errors.New("unexpected decision")
}
func (*connectionPersistenceStub) PollMachineConnection(context.Context, PollConnectionCommand, CertificateIssuer) (ConnectedMachine, error) {
	return ConnectedMachine{}, errors.New("unexpected poll")
}
func (*connectionPersistenceStub) CancelMachineConnection(context.Context, CancelConnectionCommand) error {
	return errors.New("unexpected cancellation")
}
func (*connectionPersistenceStub) ListMachines(context.Context, ListMachinesCommand) (MachinePage, []agent.InventoryRecord, error) {
	return MachinePage{}, nil, errors.New("unexpected list")
}
func (*connectionPersistenceStub) RevokeMachineFromBrowser(context.Context, RevokeMachineCommand) (MachineRecord, []agent.InventoryRecord, error) {
	return MachineRecord{}, nil, errors.New("unexpected Browser revocation")
}
func (*connectionPersistenceStub) RevokeMachineFromHost(context.Context, SelfRevokeMachineCommand) (MachineRecord, error) {
	return MachineRecord{}, errors.New("unexpected Host revocation")
}

func TestBeginMachineConnectionRequiresExactOriginAndKeyProof(t *testing.T) {
	t.Parallel()
	requestID := uuid.NewString()
	code := "BCDF-GHJ-KLM"
	secret := "carry_machine_connect_" + requestID + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	origin, name := "https://carry.example", "Build Mac"
	proof := ed25519.Sign(privateKey, ConnectionKeyProofMessage(origin, requestID, name, publicKeyDER, code, secret))
	createdAt := time.Date(2026, time.August, 21, 6, 0, 0, 0, time.UTC)
	persistence := &connectionPersistenceStub{beginResult: ConnectionRequest{
		RequestID: requestID, DisplayName: name, PublicKeyDER: publicKeyDER,
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(ConnectionLifetime), PollInterval: ConnectionInitialInterval,
	}}
	root := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	credentials, err := ParseConnectionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := CreateCertificateBundle([]string{"carry.example"}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	hostAPIOrigin, err := ParseHostAPIOrigin("https://api.carry.example")
	if err != nil {
		t.Fatal(err)
	}
	connections, err := NewConnections(persistence, credentials, authority, origin, hostAPIOrigin)
	if err != nil {
		t.Fatal(err)
	}
	request := BeginConnectionRequest{
		RequestID: requestID, IdempotencyKey: uuid.NewString(), DisplayName: name,
		UserCode: code, PollSecret: secret, Source: "198.51.100.1", Origin: origin,
		PublicKeyDER: publicKeyDER, KeyProof: proof,
	}
	begun, err := connections.Begin(t.Context(), request)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if begun.UserCode != code || begun.PollSecret != secret || begun.VerificationURL != origin+"/machine-connect" || begun.Fingerprint != PublicKeyFingerprint(publicKeyDER) {
		t.Fatalf("begun connection = %#v", begun)
	}
	if persistence.beginCommand.CodeDigest == persistence.beginCommand.PollDigest || persistence.beginCommand.CodeDigest == persistence.beginCommand.SourceDigest {
		t.Fatal("connection audiences share a digest")
	}

	request.Origin = "https://other.example"
	if _, err := connections.Begin(t.Context(), request); !errors.Is(err, ErrInvalidConnection) {
		t.Fatalf("changed origin error = %v", err)
	}
	request.Origin = origin
	request.KeyProof[0] ^= 1
	if _, err := connections.Begin(t.Context(), request); !errors.Is(err, ErrInvalidConnection) {
		t.Fatalf("changed proof error = %v", err)
	}
}

func TestHostAPIOriginRequiresCanonicalHTTPSAuthority(t *testing.T) {
	t.Parallel()
	origin, err := ParseHostAPIOrigin("https://api.carry.example:8443")
	if err != nil || origin.String() != "https://api.carry.example:8443" {
		t.Fatalf("Host API origin = %#v, %v", origin, err)
	}
	for _, invalid := range []string{"", "http://api.carry.example", "https://api.carry.example/", " https://api.carry.example", "https://api.carry.example/path", "https://user@api.carry.example"} {
		if _, err := ParseHostAPIOrigin(invalid); err == nil {
			t.Errorf("invalid Host API origin %q was accepted", invalid)
		}
	}
}

func TestMachineConnectionSecretAndCodeAudiencesAreStrict(t *testing.T) {
	t.Parallel()
	requestID := uuid.NewString()
	secret := "carry_machine_connect_" + requestID + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if got, ok := ParseConnectionPollSecret(secret); !ok || got != requestID {
		t.Fatalf("poll secret = %q, %t", got, ok)
	}
	if _, ok := ParseConnectionPollSecret("BCDF-GHJ-KLM"); ok {
		t.Fatal("human code parsed as poll secret")
	}
	if got, ok := NormalizeConnectionCode("bcdf ghj klm"); !ok || got != "BCDF-GHJ-KLM" {
		t.Fatalf("normalized code = %q, %t", got, ok)
	}
	if _, ok := NormalizeConnectionCode("carry_machine_connect"); ok {
		t.Fatal("poll audience parsed as human code")
	}
}
