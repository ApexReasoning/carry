package machine

import "errors"

// These errors are shared by the existing Run and Conversation authority
// transactions. Machine connection and revocation behavior lives in
// connection.go.
var (
	ErrMachineNotFound = errors.New("Machine does not exist")
	ErrMachineRevoked  = errors.New("Machine is revoked")
)
