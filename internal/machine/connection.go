package machine

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/google/uuid"
)

const (
	ConnectionLifetime        = 15 * time.Minute
	ConnectionReplayLifetime  = 15 * time.Minute
	ConnectionInitialInterval = 5 * time.Second
	ConnectionMaximumInterval = 30 * time.Second
	DisplayNameMaximumBytes   = 128
)

var (
	ErrInvalidConnection        = errors.New("Machine connection request is invalid")
	ErrConnectionUnavailable    = errors.New("Machine connection request is unavailable")
	ErrConnectionRateLimited    = errors.New("Machine connection attempts are temporarily limited")
	ErrConnectionConflict       = errors.New("Machine connection idempotency key was reused for a different request")
	ErrConnectionPending        = errors.New("Machine connection is awaiting Browser approval")
	ErrConnectionDenied         = errors.New("Machine connection was denied")
	ErrConnectionCancelled      = errors.New("Machine connection was cancelled")
	ErrConnectionExpired        = errors.New("Machine connection expired")
	ErrConnectionSlowDown       = errors.New("Machine connection polling is too frequent")
	ErrConnectionAlreadyDecided = errors.New("Machine connection was already decided")
	ErrConnectionReplayExpired  = errors.New("Machine connection certificate is no longer retrievable")
	ErrMachineUnavailable       = errors.New("Machine is unavailable")
	ErrMachineAuthority         = errors.New("current member cannot manage Machines in this Space")
)

type ConnectionSlowDownError struct{ RetryAfter time.Duration }

func (err ConnectionSlowDownError) Error() string        { return ErrConnectionSlowDown.Error() }
func (err ConnectionSlowDownError) Is(target error) bool { return target == ErrConnectionSlowDown }

func NewConnectionSlowDownError(after time.Duration) error {
	return ConnectionSlowDownError{RetryAfter: after}
}

// ConnectionCredentials derive only Machine-connection digests. They never
// create a User, Browser, CLI, or durable Machine credential.
type ConnectionCredentials struct{ root [32]byte }

func ParseConnectionRoot(encoded string) (ConnectionCredentials, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(decoded) != len(ConnectionCredentials{}.root) {
		return ConnectionCredentials{}, errors.New("Machine connection root must be 32 bytes of raw URL-safe base64")
	}
	var credentials ConnectionCredentials
	copy(credentials.root[:], decoded)
	return credentials, nil
}

func (credentials ConnectionCredentials) digest(label string, values ...string) [sha256.Size]byte {
	mac := hmac.New(sha256.New, credentials.root[:])
	_, _ = mac.Write([]byte(label))
	for _, value := range values {
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write([]byte(value))
	}
	var result [sha256.Size]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func (credentials ConnectionCredentials) CodeDigest(code string) [sha256.Size]byte {
	return credentials.digest("carry/machine-connect-code-digest/v1", code)
}

func (credentials ConnectionCredentials) PollDigest(secret string) [sha256.Size]byte {
	return credentials.digest("carry/machine-connect-poll-digest/v1", secret)
}

func (credentials ConnectionCredentials) SourceDigest(source string) [sha256.Size]byte {
	return credentials.digest("carry/machine-connect-source/v1", source)
}

// ConnectionPersistence is the complete PostgreSQL authority consumed by the
// Machine connection, inventory, and revocation journey.
type ConnectionPersistence interface {
	BeginMachineConnection(context.Context, BeginConnectionCommand) (ConnectionRequest, error)
	LookupMachineConnection(context.Context, LookupConnectionCommand) (ConnectionRequest, error)
	DecideMachineConnection(context.Context, DecideConnectionCommand) (ConnectionRequest, error)
	PollMachineConnection(context.Context, PollConnectionCommand, CertificateIssuer) (ConnectedMachine, error)
	CancelMachineConnection(context.Context, CancelConnectionCommand) error
	ListMachines(context.Context, ListMachinesCommand) (MachinePage, []agent.InventoryRecord, error)
	RevokeMachineFromBrowser(context.Context, RevokeMachineCommand) (MachineRecord, []agent.InventoryRecord, error)
	RevokeMachineFromHost(context.Context, SelfRevokeMachineCommand) (MachineRecord, error)
}

type CertificateIssuer func(machineID string, publicKeyDER []byte, approvedAt time.Time) (IssuedMachineCertificate, error)

// HostAPIOrigin is the canonical HTTPS authority used only by Machine mTLS traffic.
type HostAPIOrigin struct{ value string }

func ParseHostAPIOrigin(value string) (HostAPIOrigin, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return HostAPIOrigin{}, errors.New("Host API origin must be a canonical HTTPS origin")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Opaque != "" || parsed.Host != strings.ToLower(parsed.Host) || value != "https://"+parsed.Host {
		return HostAPIOrigin{}, errors.New("Host API origin must be a canonical HTTPS origin")
	}
	return HostAPIOrigin{value: value}, nil
}

func (origin HostAPIOrigin) String() string { return origin.value }

type Connections struct {
	persistence    ConnectionPersistence
	credentials    ConnectionCredentials
	authority      *CertificateAuthority
	externalOrigin string
	hostAPIOrigin  HostAPIOrigin
}

func NewConnections(persistence ConnectionPersistence, credentials ConnectionCredentials, authority *CertificateAuthority, externalOrigin string, hostAPIOrigin HostAPIOrigin) (*Connections, error) {
	if persistence == nil || authority == nil || !validOrigin(externalOrigin) || hostAPIOrigin.value == "" {
		return nil, errors.New("Machine connection dependencies are required")
	}
	return &Connections{
		persistence:    persistence,
		credentials:    credentials,
		authority:      authority,
		externalOrigin: externalOrigin,
		hostAPIOrigin:  hostAPIOrigin,
	}, nil
}

type BeginConnectionRequest struct {
	RequestID, IdempotencyKey, DisplayName, UserCode, PollSecret, Source, Origin string
	PublicKeyDER, KeyProof                                                       []byte
}

type BeginConnectionCommand struct {
	RequestID, IdempotencyKey, DisplayName string
	PublicKeyDER, KeyProof                 []byte
	RequestDigest, SourceDigest            [sha256.Size]byte
	CodeDigest, PollDigest                 [sha256.Size]byte
}

type ConnectionRequest struct {
	RequestID, DisplayName, ApprovedByUserID, ApprovedSpaceID, Decision string
	PublicKeyDER, KeyProof                                              []byte
	CreatedAt, ExpiresAt                                                time.Time
	PollInterval                                                        time.Duration
	ApprovedAt, DeniedAt, CancelledAt, RedeemedAt, ReplayUntil          *time.Time
	PreparedMachineID, ResultingMachineID                               string
}

type BegunConnection struct {
	RequestID, DisplayName, UserCode, PollSecret, Fingerprint, VerificationURL string
	ExpiresAt                                                                  time.Time
	PollInterval                                                               time.Duration
}

// Begin deliberately reports every malformed request as ErrInvalidConnection: an unauthenticated caller can only discard it and restart setup, while field-level errors would enlarge the probing surface.
func (connections *Connections) Begin(ctx context.Context, request BeginConnectionRequest) (BegunConnection, error) {
	code, ok := NormalizeConnectionCode(request.UserCode)
	if !ok {
		return BegunConnection{}, ErrInvalidConnection
	}
	requestID, secretOK := ParseConnectionPollSecret(request.PollSecret)
	if !secretOK {
		return BegunConnection{}, ErrInvalidConnection
	}
	if requestID != request.RequestID {
		return BegunConnection{}, ErrInvalidConnection
	}
	if uuid.Validate(request.RequestID) != nil {
		return BegunConnection{}, ErrInvalidConnection
	}
	if !validIdempotencyKey(request.IdempotencyKey) {
		return BegunConnection{}, ErrInvalidConnection
	}
	name := strings.TrimSpace(request.DisplayName)
	if name == "" {
		return BegunConnection{}, ErrInvalidConnection
	}
	if len([]byte(name)) > DisplayNameMaximumBytes {
		return BegunConnection{}, ErrInvalidConnection
	}
	if !utf8.ValidString(name) {
		return BegunConnection{}, ErrInvalidConnection
	}
	if request.Origin != connections.externalOrigin {
		return BegunConnection{}, ErrInvalidConnection
	}
	if strings.TrimSpace(request.Source) == "" {
		return BegunConnection{}, ErrInvalidConnection
	}
	publicKey, err := parseEd25519PublicKey(request.PublicKeyDER)
	if err != nil {
		return BegunConnection{}, ErrInvalidConnection
	}
	if len(request.KeyProof) != ed25519.SignatureSize {
		return BegunConnection{}, ErrInvalidConnection
	}
	proofMessage := ConnectionKeyProofMessage(
		request.Origin,
		request.RequestID,
		name,
		request.PublicKeyDER,
		code,
		request.PollSecret,
	)
	if !ed25519.Verify(publicKey, proofMessage, request.KeyProof) {
		return BegunConnection{}, ErrInvalidConnection
	}
	digest := connectionDigest(
		request.RequestID,
		request.Origin,
		name,
		code,
		request.PollSecret,
		base64.RawStdEncoding.EncodeToString(request.PublicKeyDER),
		base64.RawStdEncoding.EncodeToString(request.KeyProof),
	)
	created, err := connections.persistence.BeginMachineConnection(ctx, BeginConnectionCommand{
		RequestID:      request.RequestID,
		IdempotencyKey: request.IdempotencyKey,
		DisplayName:    name,
		PublicKeyDER:   append([]byte(nil), request.PublicKeyDER...),
		KeyProof:       append([]byte(nil), request.KeyProof...),
		RequestDigest:  digest,
		SourceDigest:   connections.credentials.SourceDigest(request.Source),
		CodeDigest:     connections.credentials.CodeDigest(code),
		PollDigest:     connections.credentials.PollDigest(request.PollSecret),
	})
	if err != nil {
		return BegunConnection{}, err
	}
	return BegunConnection{
		RequestID:       created.RequestID,
		DisplayName:     created.DisplayName,
		UserCode:        code,
		PollSecret:      request.PollSecret,
		Fingerprint:     PublicKeyFingerprint(created.PublicKeyDER),
		VerificationURL: connections.externalOrigin + "/machine-connect",
		ExpiresAt:       created.ExpiresAt,
		PollInterval:    created.PollInterval,
	}, nil
}

type LookupConnectionRequest struct{ BrowserSessionID, UserCode, Source string }

type LookupConnectionCommand struct {
	BrowserSessionID         string
	CodeDigest, SourceDigest [sha256.Size]byte
}

type ConnectionPreview struct {
	RequestID, UserCode, DisplayName, Fingerprint, Server, Decision, ApprovedSpaceID string
	CreatedAt, ExpiresAt                                                             time.Time
}

func (connections *Connections) Lookup(ctx context.Context, request LookupConnectionRequest) (ConnectionPreview, error) {
	code, ok := NormalizeConnectionCode(request.UserCode)
	if !ok || uuid.Validate(request.BrowserSessionID) != nil || strings.TrimSpace(request.Source) == "" {
		return ConnectionPreview{}, ErrInvalidConnection
	}
	found, err := connections.persistence.LookupMachineConnection(ctx, LookupConnectionCommand{
		BrowserSessionID: request.BrowserSessionID,
		CodeDigest:       connections.credentials.CodeDigest(code), SourceDigest: connections.credentials.SourceDigest(request.Source),
	})
	if err != nil {
		return ConnectionPreview{}, err
	}
	return ConnectionPreview{
		RequestID: found.RequestID, UserCode: code, DisplayName: found.DisplayName,
		Fingerprint:     PublicKeyFingerprint(found.PublicKeyDER),
		Server:          connections.externalOrigin,
		Decision:        found.Decision,
		ApprovedSpaceID: found.ApprovedSpaceID,
		CreatedAt:       found.CreatedAt,
		ExpiresAt:       found.ExpiresAt,
	}, nil
}

type DecideConnectionRequest struct {
	BrowserSessionID, RequestID, UserCode, SpaceID, Decision, IdempotencyKey string
}

type DecideConnectionCommand struct {
	BrowserSessionID, RequestID, SpaceID, Decision, IdempotencyKey, PreparedMachineID string
	CodeDigest, RequestDigest                                                         [sha256.Size]byte
}

func (connections *Connections) Approve(ctx context.Context, request DecideConnectionRequest) error {
	request.Decision = "approved"
	return connections.decide(ctx, request)
}

func (connections *Connections) Deny(ctx context.Context, request DecideConnectionRequest) error {
	request.Decision, request.SpaceID = "denied", ""
	return connections.decide(ctx, request)
}

func (connections *Connections) decide(ctx context.Context, request DecideConnectionRequest) error {
	code, ok := NormalizeConnectionCode(request.UserCode)
	if !ok || uuid.Validate(request.BrowserSessionID) != nil || uuid.Validate(request.RequestID) != nil ||
		!validIdempotencyKey(request.IdempotencyKey) ||
		(request.Decision == "approved" && uuid.Validate(request.SpaceID) != nil) ||
		(request.Decision != "approved" && request.Decision != "denied") {
		return ErrInvalidConnection
	}
	_, err := connections.persistence.DecideMachineConnection(ctx, DecideConnectionCommand{
		BrowserSessionID: request.BrowserSessionID, RequestID: request.RequestID,
		SpaceID: request.SpaceID, Decision: request.Decision, IdempotencyKey: request.IdempotencyKey,
		PreparedMachineID: uuid.NewString(), CodeDigest: connections.credentials.CodeDigest(code),
		RequestDigest: connectionDigest(request.RequestID, code, request.SpaceID, request.Decision),
	})
	return err
}

type PollConnectionCommand struct {
	RequestID  string
	PollDigest [sha256.Size]byte
}

type ConnectedMachine struct {
	MachineID, SpaceID, DisplayName string
	HostAPIOrigin                   HostAPIOrigin
	CertificatePEM                  []byte
	RedeemedAt, ReplayUntil         time.Time
}

func (connections *Connections) Poll(ctx context.Context, pollSecret string) (ConnectedMachine, error) {
	requestID, ok := ParseConnectionPollSecret(pollSecret)
	if !ok {
		return ConnectedMachine{}, ErrMachineUnavailable
	}
	connected, err := connections.persistence.PollMachineConnection(ctx, PollConnectionCommand{
		RequestID:  requestID,
		PollDigest: connections.credentials.PollDigest(pollSecret),
	}, connections.authority.IssueMachineCertificate)
	if err != nil {
		return ConnectedMachine{}, err
	}
	connected.HostAPIOrigin = connections.hostAPIOrigin
	return connected, nil
}

type CancelConnectionCommand struct {
	RequestID  string
	PollDigest [sha256.Size]byte
}

func (connections *Connections) Cancel(ctx context.Context, pollSecret string) error {
	requestID, ok := ParseConnectionPollSecret(pollSecret)
	if !ok {
		return ErrMachineUnavailable
	}
	return connections.persistence.CancelMachineConnection(ctx, CancelConnectionCommand{
		RequestID: requestID, PollDigest: connections.credentials.PollDigest(pollSecret),
	})
}

type ListMachinesCommand struct{ BrowserSessionID, SpaceID, After string }

type MachineRecord struct {
	MachineID, SpaceID, SpaceName, DisplayName, Fingerprint, State string
	EnrolledByUserID, EnrolledByName                               string
	EnrolledAt                                                     time.Time
	RevocationActor, RevokedByUserID, RevokedByName                string
	RevokedAt                                                      *time.Time
	CanRevoke                                                      bool
}

type MachinePage struct {
	Machines   []MachineRecord
	NextCursor string
}

func (connections *Connections) List(ctx context.Context, browserSessionID, spaceID, after string) (MachinePage, []agent.InventoryRecord, error) {
	if uuid.Validate(browserSessionID) != nil || uuid.Validate(spaceID) != nil || (after != "" && uuid.Validate(after) != nil) {
		return MachinePage{}, nil, ErrInvalidConnection
	}
	return connections.persistence.ListMachines(ctx, ListMachinesCommand{BrowserSessionID: browserSessionID, SpaceID: spaceID, After: after})
}

type RevokeMachineCommand struct {
	BrowserSessionID, SpaceID, MachineID, IdempotencyKey string
	RequestDigest                                        [sha256.Size]byte
}

func (connections *Connections) RevokeFromBrowser(ctx context.Context, browserSessionID, spaceID, machineID, idempotencyKey string) (MachineRecord, []agent.InventoryRecord, error) {
	if uuid.Validate(browserSessionID) != nil || uuid.Validate(spaceID) != nil || uuid.Validate(machineID) != nil || !validIdempotencyKey(idempotencyKey) {
		return MachineRecord{}, nil, ErrInvalidConnection
	}
	return connections.persistence.RevokeMachineFromBrowser(ctx, RevokeMachineCommand{
		BrowserSessionID: browserSessionID, SpaceID: spaceID, MachineID: machineID, IdempotencyKey: idempotencyKey,
		RequestDigest: connectionDigest(spaceID, machineID, "browser-revoke"),
	})
}

type SelfRevokeMachineCommand struct {
	MachineID, CertificateSerial, IdempotencyKey string
	RequestDigest                                [sha256.Size]byte
}

func (connections *Connections) RevokeFromHost(ctx context.Context, machineID, certificateSerial, idempotencyKey string) (MachineRecord, error) {
	if uuid.Validate(machineID) != nil || strings.TrimSpace(certificateSerial) == "" || !validIdempotencyKey(idempotencyKey) {
		return MachineRecord{}, ErrInvalidConnection
	}
	return connections.persistence.RevokeMachineFromHost(ctx, SelfRevokeMachineCommand{
		MachineID:         machineID,
		CertificateSerial: certificateSerial,
		IdempotencyKey:    idempotencyKey,
		RequestDigest:     connectionDigest(machineID, certificateSerial, "self-revoke"),
	})
}

func PublicKeyFingerprint(publicKeyDER []byte) string {
	digest := sha256.Sum256(publicKeyDER)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

func ConnectionKeyProofMessage(origin, requestID, displayName string, publicKeyDER []byte, userCode, pollSecret string) []byte {
	parts := [][]byte{
		[]byte("carry/machine-connect-key-proof/v1"), []byte(origin), []byte(requestID),
		[]byte(displayName), publicKeyDER, []byte(userCode), []byte(pollSecret),
	}
	var encoded []byte
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		encoded = append(encoded, length[:]...)
		encoded = append(encoded, part...)
	}
	return encoded
}

func ParseConnectionPollSecret(secret string) (string, bool) {
	const prefix = "carry_machine_connect_"
	if !strings.HasPrefix(secret, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(secret, prefix), ".")
	if len(parts) != 2 || uuid.Validate(parts[0]) != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	return parts[0], err == nil && len(decoded) == 32
}

func NormalizeConnectionCode(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "", " ", "").Replace(value)
	if len(value) != 10 {
		return "", false
	}
	const alphabet = "BCDFGHJKLMNPQRSTVWXZ"
	for _, character := range value {
		if !strings.ContainsRune(alphabet, character) {
			return "", false
		}
	}
	return value[:4] + "-" + value[4:7] + "-" + value[7:], true
}

func parseEd25519PublicKey(publicKeyDER []byte) (ed25519.PublicKey, error) {
	parsed, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return nil, err
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("Machine public key must be Ed25519")
	}
	return publicKey, nil
}

func connectionDigest(parts ...string) [sha256.Size]byte {
	encoded, _ := json.Marshal(parts)
	return sha256.Sum256(encoded)
}

func validIdempotencyKey(value string) bool {
	return strings.TrimSpace(value) != "" && len([]byte(value)) <= 255
}

func validOrigin(value string) bool {
	return strings.HasPrefix(value, "https://") && !strings.Contains(strings.TrimPrefix(value, "https://"), "/")
}
