package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const IdentityProofLifetime = 10 * time.Minute

type Method string

const (
	EmailMethod  Method = "email"
	GoogleMethod Method = "google"
	GitHubMethod Method = "github"
)

type ProofPurpose string

const (
	LoginPurpose          ProofPurpose = "login"
	ReauthenticatePurpose ProofPurpose = "reauthenticate"
	LinkPurpose           ProofPurpose = "link"
)

var (
	ErrRecentIdentityProofRequired = errors.New("recent confirmation of a linked sign-in method is required")
	ErrIdentityMethodOccupied      = errors.New("sign-in method cannot be linked")
	ErrIdentityMethodAlreadyLinked = errors.New("sign-in method is already linked")
	ErrIdentityMethodNotLinked     = errors.New("sign-in method is not linked")
	ErrLastIdentityMethod          = errors.New("at least one sign-in method must remain")
)

type IdentityMethods struct {
	Methods                  []Method
	ReauthenticationRequired bool
}

type IdentityMethodPersistence interface {
	ListIdentityMethods(context.Context, string, string) (IdentityMethods, error)
	UnlinkIdentityMethod(context.Context, UnlinkIdentityMethodCommand) (BrowserSession, error)
}

type Methods struct {
	persistence IdentityMethodPersistence
	credentials Credentials
}

func NewMethods(persistence IdentityMethodPersistence, credentials Credentials) (*Methods, error) {
	if persistence == nil {
		return nil, errors.New("Identity method persistence is required")
	}
	return &Methods{persistence: persistence, credentials: credentials}, nil
}

func (methods *Methods) List(ctx context.Context, userID string, sessionID string) (IdentityMethods, error) {
	if uuid.Validate(userID) != nil || uuid.Validate(sessionID) != nil {
		return IdentityMethods{}, ErrUnauthenticated
	}
	return methods.persistence.ListIdentityMethods(ctx, userID, sessionID)
}

type UnlinkMethodCommand struct {
	SessionID      string
	Method         Method
	IdempotencyKey string
}

func (methods *Methods) Unlink(ctx context.Context, command UnlinkMethodCommand) (BrowserSession, error) {
	if uuid.Validate(command.SessionID) != nil ||
		!validMethod(command.Method) || strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 {
		return BrowserSession{}, ErrUnauthenticated
	}
	return methods.persistence.UnlinkIdentityMethod(ctx, UnlinkIdentityMethodCommand{
		InitiatingSessionID: command.SessionID,
		Method:              command.Method, IdempotencyKey: command.IdempotencyKey,
		RequestDigest: methods.credentials.RequestDigest(
			"unlink-identity-method", command.SessionID, string(command.Method),
		),
		ReplacementSessionID: uuid.NewString(),
	})
}

type UnlinkIdentityMethodCommand struct {
	InitiatingSessionID  string
	Method               Method
	IdempotencyKey       string
	RequestDigest        [32]byte
	ReplacementSessionID string
}

func validMethod(method Method) bool {
	return method == EmailMethod || method == GoogleMethod || method == GitHubMethod
}

func validProofPurpose(purpose ProofPurpose) bool {
	return purpose == LoginPurpose || purpose == ReauthenticatePurpose || purpose == LinkPurpose
}
