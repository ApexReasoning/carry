package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

const BrowserSessionSecretBytes = 32

type BrowserSessionSecret struct {
	Secret string
	Hash   [sha256.Size]byte
}

type BrowserSession struct {
	Secret    string
	UserID    string
	ExpiresAt time.Time
}

func NewBrowserSessionSecret() (BrowserSessionSecret, error) {
	random := make([]byte, BrowserSessionSecretBytes)
	if _, err := rand.Read(random); err != nil {
		return BrowserSessionSecret{}, fmt.Errorf("generate browser session secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(random)
	return BrowserSessionSecret{Secret: secret, Hash: HashBrowserSessionSecret(secret)}, nil
}

func HashBrowserSessionSecret(secret string) [sha256.Size]byte {
	return sha256.Sum256([]byte(secret))
}
