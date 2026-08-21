package identity

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrUnauthenticated = errors.New("user authentication is not active")

// FallbackDisplayName returns the stable non-personal label for one User.
func FallbackDisplayName(userID string) (string, error) {
	parsed, err := uuid.Parse(userID)
	if err != nil {
		return "", errors.New("User ID is invalid")
	}
	return "Member " + strings.ReplaceAll(parsed.String(), "-", "")[:8], nil
}

type AuthenticatedUser struct {
	UserID      string
	DisplayName string
}
