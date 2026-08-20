package identity

import (
	"bytes"
	"testing"
)

func TestBrowserSessionCredentialIsStableAndRejectsTampering(t *testing.T) {
	t.Parallel()

	credentials, err := NewCredentials(bytes.Repeat([]byte{7}, IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}
	const sessionID = "11111111-1111-4111-8111-111111111111"
	first, err := credentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatalf("create browser session credential: %v", err)
	}
	second, err := credentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatalf("recreate browser session credential: %v", err)
	}
	if first != second {
		t.Fatal("browser session credential was not replayable")
	}
	parsed, ok := credentials.ParseBrowserSessionCredential(first)
	if !ok || parsed != sessionID {
		t.Fatalf("parsed browser session = %q, %t", parsed, ok)
	}
	tampered := first[:len(first)-1] + "A"
	if _, ok := credentials.ParseBrowserSessionCredential(tampered); ok {
		t.Fatal("tampered browser session credential was accepted")
	}
}
