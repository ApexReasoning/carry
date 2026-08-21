package machinefile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/google/uuid"
)

func TestPendingAndCredentialCleanupAreCrashRecoverable(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "private")
	requestID := uuid.NewString()
	publicKeyDER, privateKeyPEM, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := NewPollSecret(requestID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := NewUserCode()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := SignConnectionProof(string(privateKeyPEM), "https://carry.example", requestID, "Desk Mac", publicKeyDER, code, secret)
	if err != nil {
		t.Fatal(err)
	}
	pending := PendingConnection{
		ServerURL: "https://carry.example", RequestID: requestID, IdempotencyKey: uuid.NewString(), DisplayName: "Desk Mac",
		UserCode: code, PollSecret: secret, PublicKeyDER: publicKeyDER, PrivateKeyPEM: string(privateKeyPEM), KeyProof: proof,
		Fingerprint: machine.PublicKeyFingerprint(publicKeyDER), ExpiresAt: time.Now().Add(machine.ConnectionLifetime),
		IntervalSeconds: int(machine.ConnectionInitialInterval / time.Second),
	}
	if err := SavePending(directory, pending); err != nil {
		t.Fatalf("save pending: %v", err)
	}
	loaded, err := LoadPending(directory)
	if err != nil || loaded.RequestID != requestID || loaded.Fingerprint != pending.Fingerprint {
		t.Fatalf("load pending = %#v, %v", loaded, err)
	}

	now := time.Date(2026, time.August, 21, 8, 0, 0, 0, time.UTC)
	bundle, err := machine.CreateCertificateBundle([]string{"carry.example"}, now)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := machine.LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	machineID := uuid.NewString()
	issued, err := authority.IssueMachineCertificate(machineID, publicKeyDER, now)
	if err != nil {
		t.Fatal(err)
	}
	credential := Credential{
		MachineID: machineID, SpaceID: uuid.NewString(), ServerURL: "https://carry.example",
		CACertificatePEM: string(bundle.CACertificatePEM), CertificatePEM: string(issued.CertificatePEM), PrivateKeyPEM: string(privateKeyPEM),
		DisconnectIdempotencyKey: uuid.NewString(),
	}
	if err := Save(directory, credential); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := RemovePending(directory); err != nil {
		t.Fatalf("remove pending: %v", err)
	}
	if _, err := Load(directory); err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if err := MarkRevoked(directory); err != nil {
		t.Fatalf("mark revoked: %v", err)
	}
	retained, confirmed, err := LoadForDisconnection(directory)
	if err != nil || !confirmed || retained.MachineID != machineID {
		t.Fatalf("load confirmed revocation = %#v, %t, %v", retained, confirmed, err)
	}
	if err := RemoveRevoked(directory); err != nil {
		t.Fatalf("remove revoked: %v", err)
	}
	if _, _, err := LoadForDisconnection(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential remains after cleanup: %v", err)
	}
}

func TestPendingConnectionRejectsMismatchedPrivateKey(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	requestID := uuid.NewString()
	publicKeyDER, privateKeyPEM, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, otherPrivateKeyPEM, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := NewPollSecret(requestID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := NewUserCode()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := SignConnectionProof(string(privateKeyPEM), "https://carry.example", requestID, "Desk Mac", publicKeyDER, code, secret)
	if err != nil {
		t.Fatal(err)
	}
	pending := PendingConnection{
		ServerURL: "https://carry.example", RequestID: requestID, IdempotencyKey: uuid.NewString(), DisplayName: "Desk Mac",
		UserCode: code, PollSecret: secret, PublicKeyDER: publicKeyDER, PrivateKeyPEM: string(otherPrivateKeyPEM), KeyProof: proof,
		Fingerprint: machine.PublicKeyFingerprint(publicKeyDER), ExpiresAt: time.Now().Add(machine.ConnectionLifetime),
		IntervalSeconds: int(machine.ConnectionInitialInterval / time.Second),
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, pendingFilename), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPending(directory); err == nil || !strings.Contains(err.Error(), "private key does not match") {
		t.Fatalf("mismatched pending private key error = %v", err)
	}
}

func TestMachineFilesRejectSymlinksAndUnknownJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions differ on Windows")
	}
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, pendingFilename)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPending(directory); !errors.Is(err, ErrUnsafeCredential) {
		t.Fatalf("symlink error = %v", err)
	}
	if err := os.Remove(filepath.Join(directory, pendingFilename)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, pendingFilename), []byte(`{"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPending(directory); err == nil {
		t.Fatal("unknown pending JSON field was accepted")
	}
}
