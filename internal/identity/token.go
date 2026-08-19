package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const userTokenPrefix = "carry_user_"

type UserToken struct {
	Secret string
	Hash   [sha256.Size]byte
}

func NewUserToken() (UserToken, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return UserToken{}, fmt.Errorf("generate user token: %w", err)
	}
	secret := userTokenPrefix + base64.RawURLEncoding.EncodeToString(secretBytes)
	return UserToken{Secret: secret, Hash: HashUserToken(secret)}, nil
}

func HashUserToken(secret string) [sha256.Size]byte {
	return sha256.Sum256([]byte(secret))
}
