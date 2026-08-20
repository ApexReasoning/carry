package identity

import "errors"

var ErrUnauthenticated = errors.New("user authentication is not active")

type AuthenticatedUser struct {
	UserID      string
	DisplayName string
}
