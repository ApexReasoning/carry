package identity

import (
	"bytes"
	"testing"
)

func TestNewUserTokenIsHighEntropyAndHashable(t *testing.T) {
	t.Parallel()

	token, err := NewUserToken()
	if err != nil {
		t.Fatalf("new user token: %v", err)
	}
	if len(token.Secret) < len("carry_user_")+40 {
		t.Fatalf("token length = %d, want at least %d", len(token.Secret), len("carry_user_")+40)
	}
	if got := token.Secret[:len("carry_user_")]; got != "carry_user_" {
		t.Fatalf("token prefix = %q", got)
	}
	if bytes.Equal(token.Hash[:], make([]byte, len(token.Hash))) {
		t.Fatal("token hash is all zeroes")
	}
	if got := HashUserToken(token.Secret); got != token.Hash {
		t.Fatal("HashUserToken does not reproduce token hash")
	}
}
