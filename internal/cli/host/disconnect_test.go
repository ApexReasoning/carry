package host

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/google/uuid"
)

func TestDisconnectRetainsExactCredentialWhenServerIsUnreachable(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	publicKeyDER, privateKeyPEM, err := machinefile.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	bundle, err := machine.CreateCertificateBundle([]string{"127.0.0.1"}, now)
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
	if err := machinefile.Save(directory, machinefile.Credential{
		MachineID: machineID, SpaceID: uuid.NewString(), ServerURL: "https://127.0.0.1:1",
		CACertificatePEM: string(bundle.CACertificatePEM), CertificatePEM: string(issued.CertificatePEM),
		PrivateKeyPEM: string(privateKeyPEM),
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var output bytes.Buffer
	if err := runDisconnect(ctx, directory, &output, false); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unreachable disconnect error = %v", err)
	}
	retained, err := machinefile.Load(directory)
	if err != nil {
		t.Fatalf("active credential was not retained: %v", err)
	}
	if retained.DisconnectIdempotencyKey == "" {
		t.Fatal("disconnect idempotency identity was not retained")
	}
	firstKey := retained.DisconnectIdempotencyKey

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runDisconnect(ctx, directory, &output, false); err == nil {
		t.Fatal("unreachable disconnect retry unexpectedly succeeded")
	}
	retried, err := machinefile.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if retried.DisconnectIdempotencyKey != firstKey {
		t.Fatalf("disconnect retry identity changed: %q != %q", retried.DisconnectIdempotencyKey, firstKey)
	}
	if _, confirmed, err := machinefile.LoadForDisconnection(directory); err != nil || confirmed {
		t.Fatalf("unreachable disconnect became confirmed: confirmed=%t err=%v", confirmed, err)
	}
}
