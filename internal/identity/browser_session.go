package identity

import "time"

type BrowserSession struct {
	SessionID string
	UserID    string
	ExpiresAt time.Time
}
