package identity

import (
	"encoding/base64"
	"testing"
)

func TestBrowserSessionSecretHasFullRandomEntropy(t *testing.T) {
	t.Parallel()

	first, err := NewBrowserSessionSecret()
	if err != nil {
		t.Fatalf("create first browser session: %v", err)
	}
	second, err := NewBrowserSessionSecret()
	if err != nil {
		t.Fatalf("create second browser session: %v", err)
	}
	if first.Secret == second.Secret {
		t.Fatal("browser session secrets repeated")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.Secret)
	if err != nil {
		t.Fatalf("decode browser session secret: %v", err)
	}
	if len(decoded) != BrowserSessionSecretBytes {
		t.Fatalf("secret entropy bytes = %d, want %d", len(decoded), BrowserSessionSecretBytes)
	}
	if first.Hash != HashBrowserSessionSecret(first.Secret) {
		t.Fatal("browser session hash does not match secret")
	}
}
