package machinefile

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/google/uuid"
)

var (
	ErrAlreadyEnrolled  = errors.New("this Machine is already connected")
	ErrUnsafeCredential = errors.New("Machine credential file is not a private regular file")
)

const (
	credentialFilename        = "machine.json"
	revokedCredentialFilename = "machine-revoked.json"
	pendingFilename           = "machine-connection.json"
	maximumFileBytes          = 1 << 20
)

// Credential is the installed Machine identity. Its private key is generated
// and consumed locally and must never be sent to carry-server.
type Credential struct {
	MachineID                string `json:"machine_id"`
	SpaceID                  string `json:"space_id"`
	HostAPIOrigin            string `json:"host_api_origin"`
	CACertificatePEM         string `json:"ca_certificate_pem,omitempty"`
	CertificatePEM           string `json:"certificate_pem"`
	PrivateKeyPEM            string `json:"private_key_pem"`
	DisconnectIdempotencyKey string `json:"disconnect_idempotency_key,omitempty"`
}

// PendingConnection retains the exact key and separate ceremony audiences so
// begin, poll, cancellation, and local installation can resume without minting
// another Machine identity when a response is lost.
type PendingConnection struct {
	ExternalOrigin   string    `json:"external_origin"`
	CACertificatePEM string    `json:"ca_certificate_pem,omitempty"`
	RequestID        string    `json:"request_id"`
	IdempotencyKey   string    `json:"idempotency_key"`
	DisplayName      string    `json:"display_name"`
	UserCode         string    `json:"user_code"`
	PollSecret       string    `json:"poll_secret"`
	PublicKeyDER     []byte    `json:"public_key_der"`
	PrivateKeyPEM    string    `json:"private_key_pem"`
	KeyProof         []byte    `json:"key_proof"`
	Fingerprint      string    `json:"fingerprint"`
	ExpiresAt        time.Time `json:"expires_at"`
	IntervalSeconds  int       `json:"interval_seconds"`
}

// GenerateKey creates a new local Machine key and exports only its public half.
func GenerateKey() (publicKeyDER []byte, privateKeyPEM []byte, err error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate Machine key: %w", err)
	}
	publicKeyDER, err = x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal Machine public key: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal Machine private key: %w", err)
	}
	return publicKeyDER, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}), nil
}

func NewPollSecret(requestID string) (string, error) {
	if uuid.Validate(requestID) != nil {
		return "", errors.New("Machine request identity is invalid")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate Machine poll secret: %w", err)
	}
	return "carry_machine_connect_" + requestID + "." + base64.RawURLEncoding.EncodeToString(secret), nil
}

func NewUserCode() (string, error) {
	const alphabet = "BCDFGHJKLMNPQRSTVWXZ"
	random := make([]byte, 10)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate Machine connection code: %w", err)
	}
	for index := range random {
		random[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return string(random[:4]) + "-" + string(random[4:7]) + "-" + string(random[7:]), nil
}

func SignConnectionProof(privateKeyPEM string, origin, requestID, displayName string, publicKeyDER []byte, code, pollSecret string) ([]byte, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, errors.New("Machine private key is invalid")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("Machine private key is invalid")
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("Machine private key is not Ed25519")
	}
	return ed25519.Sign(privateKey, machine.ConnectionKeyProofMessage(origin, requestID, displayName, publicKeyDER, code, pollSecret)), nil
}

// Save atomically publishes a mode-0600 Machine credential.
func Save(directory string, credential Credential) error {
	if err := validateCredential(credential); err != nil {
		return err
	}
	return saveJSON(directory, credentialFilename, ".machine-*.json", "Machine credential", credential)
}

func Load(directory string) (Credential, error) {
	return loadCredential(filepath.Join(directory, credentialFilename), directory, "Machine credential")
}

func LoadForDisconnection(directory string) (credential Credential, confirmed bool, err error) {
	credential, err = Load(directory)
	if err == nil {
		return credential, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Credential{}, false, err
	}
	credential, err = loadCredential(filepath.Join(directory, revokedCredentialFilename), directory, "revoked Machine credential")
	if err != nil {
		return Credential{}, false, err
	}
	return credential, true, nil
}

func MarkRevoked(directory string) error {
	if err := inspectPrivateDirectory(directory); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(directory, credentialFilename), filepath.Join(directory, revokedCredentialFilename)); err != nil {
		return fmt.Errorf("retire revoked Machine credential: %w", err)
	}
	return syncDirectory(directory)
}

func RemoveRevoked(directory string) error {
	return removeFile(directory, revokedCredentialFilename, "revoked Machine credential")
}

func SavePending(directory string, pending PendingConnection) error {
	if err := validatePending(pending); err != nil {
		return err
	}
	return saveJSON(directory, pendingFilename, ".machine-connection-*.json", "pending Machine connection", pending)
}

func LoadPending(directory string) (PendingConnection, error) {
	var pending PendingConnection
	if err := loadJSON(filepath.Join(directory, pendingFilename), directory, "pending Machine connection", &pending); err != nil {
		return PendingConnection{}, err
	}
	if err := validatePending(pending); err != nil {
		return PendingConnection{}, err
	}
	return pending, nil
}

func RemovePending(directory string) error {
	return removeFile(directory, pendingFilename, "pending Machine connection")
}

// RemoveLocalOnly erases local Machine material without changing or claiming
// anything about the server-side Machine authority.
func RemoveLocalOnly(directory string) error {
	if err := inspectPrivateDirectory(directory); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, filename := range []string{pendingFilename, credentialFilename, revokedCredentialFilename} {
		if err := os.Remove(filepath.Join(directory, filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove local Machine material: %w", err)
		}
	}
	return syncDirectory(directory)
}

func saveJSON(directory, filename, pattern, description string, value any) error {
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", description, err)
	}
	temporary, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return fmt.Errorf("create %s: %w", description, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect %s: %w", description, err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %s: %w", description, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync %s: %w", description, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", description, err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, filename)); err != nil {
		return fmt.Errorf("publish %s: %w", description, err)
	}
	return syncDirectory(directory)
}

func loadCredential(path, directory, description string) (Credential, error) {
	var credential Credential
	if err := loadJSON(path, directory, description, &credential); err != nil {
		return Credential{}, err
	}
	if err := validateCredential(credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func loadJSON(path, directory, description string, destination any) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", description, err)
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", description, err)
	}
	if err := inspectPrivateDirectory(directory); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		(runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) || info.Size() > maximumFileBytes {
		return ErrUnsafeCredential
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", description, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maximumFileBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", description, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: expected one JSON value", description)
	}
	return nil
}

func validateCredential(credential Credential) error {
	if !validCanonicalHTTPSOrigin(credential.HostAPIOrigin) || uuid.Validate(credential.MachineID) != nil || uuid.Validate(credential.SpaceID) != nil ||
		strings.TrimSpace(credential.CertificatePEM) == "" || strings.TrimSpace(credential.PrivateKeyPEM) == "" ||
		(credential.DisconnectIdempotencyKey != "" && uuid.Validate(credential.DisconnectIdempotencyKey) != nil) {
		return errors.New("Machine credential content is invalid")
	}
	pair, err := tls.X509KeyPair([]byte(credential.CertificatePEM), []byte(credential.PrivateKeyPEM))
	if err != nil || pair.Leaf == nil {
		return errors.New("Machine credential key and certificate are invalid")
	}
	machineID, err := machine.MachineIDFromCertificate(pair.Leaf)
	if err != nil || machineID != credential.MachineID {
		return errors.New("Machine credential certificate identity does not match")
	}
	if credential.CACertificatePEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(credential.CACertificatePEM)) {
			return errors.New("Machine CA certificate is invalid")
		}
	}
	return nil
}

func validatePending(pending PendingConnection) error {
	if !validCanonicalHTTPSOrigin(pending.ExternalOrigin) {
		return errors.New("pending Machine external origin is invalid")
	}
	if uuid.Validate(pending.RequestID) != nil {
		return errors.New("pending Machine request identity is invalid")
	}
	if uuid.Validate(pending.IdempotencyKey) != nil {
		return errors.New("pending Machine idempotency identity is invalid")
	}
	requestID, secretOK := machine.ParseConnectionPollSecret(pending.PollSecret)
	if !secretOK {
		return errors.New("pending Machine poll secret is invalid")
	}
	if requestID != pending.RequestID {
		return errors.New("pending Machine poll secret belongs to another request")
	}
	code, codeOK := machine.NormalizeConnectionCode(pending.UserCode)
	if !codeOK {
		return errors.New("pending Machine connection code is invalid")
	}
	if code != pending.UserCode {
		return errors.New("pending Machine connection code is not canonical")
	}
	if strings.TrimSpace(pending.DisplayName) == "" {
		return errors.New("pending Machine display name is empty")
	}
	if len([]byte(pending.DisplayName)) > machine.DisplayNameMaximumBytes {
		return errors.New("pending Machine display name is too long")
	}
	if len(pending.PublicKeyDER) == 0 {
		return errors.New("pending Machine public key is empty")
	}
	if len(pending.KeyProof) != ed25519.SignatureSize {
		return errors.New("pending Machine key proof has an invalid size")
	}
	if pending.Fingerprint != machine.PublicKeyFingerprint(pending.PublicKeyDER) {
		return errors.New("pending Machine fingerprint does not match its public key")
	}
	if pending.ExpiresAt.IsZero() {
		return errors.New("pending Machine expiry is missing")
	}
	minimumInterval := int(machine.ConnectionInitialInterval / time.Second)
	if pending.IntervalSeconds < minimumInterval {
		return errors.New("pending Machine polling interval is too short")
	}
	maximumInterval := int(machine.ConnectionMaximumInterval / time.Second)
	if pending.IntervalSeconds > maximumInterval {
		return errors.New("pending Machine polling interval is too long")
	}
	publicKey, err := x509.ParsePKIXPublicKey(pending.PublicKeyDER)
	if err != nil {
		return errors.New("pending Machine public key is invalid")
	}
	key, ok := publicKey.(ed25519.PublicKey)
	if !ok {
		return errors.New("pending Machine public key is not Ed25519")
	}
	proofMessage := machine.ConnectionKeyProofMessage(
		pending.ExternalOrigin,
		pending.RequestID,
		pending.DisplayName,
		pending.PublicKeyDER,
		pending.UserCode,
		pending.PollSecret,
	)
	if !ed25519.Verify(key, proofMessage, pending.KeyProof) {
		return errors.New("pending Machine key proof is invalid")
	}
	proof, err := SignConnectionProof(pending.PrivateKeyPEM, pending.ExternalOrigin, pending.RequestID, pending.DisplayName, pending.PublicKeyDER, pending.UserCode, pending.PollSecret)
	if err != nil {
		return err
	}
	if !bytes.Equal(proof, pending.KeyProof) {
		return errors.New("pending Machine private key does not match its approved public key")
	}
	return nil
}

func validCanonicalHTTPSOrigin(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Machine credential directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect Machine credential directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrUnsafeCredential
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("protect Machine credential directory: %w", err)
		}
	}
	return nil
}

func inspectPrivateDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect Machine credential directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return ErrUnsafeCredential
	}
	return nil
}

func removeFile(directory, filename, description string) error {
	if err := os.Remove(filepath.Join(directory, filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", description, err)
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open Machine credential directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync Machine credential directory: %w", err)
	}
	return nil
}
