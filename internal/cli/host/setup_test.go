package host

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
)

func TestHostCommandExposesOnlyStartAndDisconnect(t *testing.T) {
	t.Parallel()
	command := NewCommand(t.TempDir(), &bytes.Buffer{}, &bytes.Buffer{}, hostdomain.AdapterSet{})
	got := make([]string, 0, len(command.Commands()))
	for _, child := range command.Commands() {
		got = append(got, child.Name())
	}
	want := []string{"disconnect", "start"}
	if len(got) != len(want) {
		t.Fatalf("Host commands = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Host commands = %v, want %v", got, want)
		}
	}
}

func TestSetupPersistsFreshProofBeforeNetworkAndRecoversExactPending(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "config")
	first, err := loadOrCreatePendingConnection(directory, "https://carry.example", "", "Desk Mac")
	if err != nil {
		t.Fatalf("create pending connection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "machine-connection.json")); err != nil {
		t.Fatalf("pending connection was not persisted: %v", err)
	}
	second, err := loadOrCreatePendingConnection(directory, "https://carry.example", "", "Desk Mac")
	if err != nil {
		t.Fatalf("recover pending connection: %v", err)
	}
	if second.RequestID != first.RequestID || second.IdempotencyKey != first.IdempotencyKey ||
		second.PollSecret != first.PollSecret || !bytes.Equal(second.PublicKeyDER, first.PublicKeyDER) ||
		second.PrivateKeyPEM != first.PrivateKeyPEM {
		t.Fatalf("recovered a different connection: first=%#v second=%#v", first, second)
	}
	if _, err := loadOrCreatePendingConnection(directory, "https://other.example", "", "Desk Mac"); err == nil {
		t.Fatal("changed origin was accepted for pending connection")
	}
}

func TestDisconnectLocalOnlyErasesAllLocalMaterialWithHonestWarning(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "config")
	if _, err := loadOrCreatePendingConnection(directory, "https://carry.example", "", "Desk Mac"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runDisconnect(t.Context(), directory, &output, true); err != nil {
		t.Fatalf("local-only disconnect: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("may still appear Active")) {
		t.Fatalf("local-only output = %q", output.String())
	}
	if _, err := machinefile.LoadPending(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending material remains: %v", err)
	}
}
