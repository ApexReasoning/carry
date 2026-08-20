package machinefile

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrAlreadyEnrolled = errors.New("this Machine is already enrolled")

const (
	credentialFilename        = "machine.json"
	revokedCredentialFilename = "machine-revoked.json"
)

// Credential is the installed Machine identity. Its private key is generated
// and consumed locally and must never be sent to carry-server.
type Credential struct {
	MachineID        string `json:"machine_id"`
	SpaceID          string `json:"space_id"`
	ServerURL        string `json:"server_url"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	CertificatePEM   string `json:"certificate_pem"`
	PrivateKeyPEM    string `json:"private_key_pem"`
}

// PendingEnrollment is the durable input for an exact enrollment retry when
// the server outcome is unknown.
type PendingEnrollment struct {
	ServerURL        string `json:"server_url"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	EnrolledByUserID string `json:"enrolled_by_user_id"`
	SpaceID          string `json:"space_id"`
	DisplayName      string `json:"display_name"`
	IdempotencyKey   string `json:"idempotency_key"`
	PublicKeyDER     []byte `json:"public_key_der"`
	PrivateKeyPEM    string `json:"private_key_pem"`
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

// Save atomically publishes a mode-0600 Machine credential.
func Save(directory string, credential Credential) error {
	return saveJSON(directory, credentialFilename, ".machine-*.json", "Machine credential", credential)
}

// Load reads the installed Machine credential.
func Load(directory string) (Credential, error) {
	return loadCredential(filepath.Join(directory, credentialFilename), "Machine credential")
}

// LoadForRevocation resumes local cleanup after the server has confirmed
// revocation. confirmed is true only when the active credential was already
// durably retired by an earlier invocation.
func LoadForRevocation(directory string) (credential Credential, confirmed bool, err error) {
	credential, err = Load(directory)
	if err == nil {
		return credential, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Credential{}, false, err
	}
	credential, err = loadCredential(
		filepath.Join(directory, revokedCredentialFilename),
		"revoked Machine credential",
	)
	if err != nil {
		return Credential{}, false, err
	}
	return credential, true, nil
}

// MarkRevoked durably removes the credential from the active Host path while
// retaining enough local state to retry cleanup after a crash.
func MarkRevoked(directory string) error {
	if err := os.Rename(
		filepath.Join(directory, credentialFilename),
		filepath.Join(directory, revokedCredentialFilename),
	); err != nil {
		return fmt.Errorf("retire revoked Machine credential: %w", err)
	}
	return syncDirectory(directory)
}

// RemoveRevoked destroys a credential only after server revocation is known.
func RemoveRevoked(directory string) error {
	path := filepath.Join(directory, revokedCredentialFilename)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("remove revoked Machine credential: %w", err)
	}
	return syncDirectory(directory)
}

// SavePending persists the private key and idempotency identity before the
// request, so a lost enrollment response can be reconciled by an exact retry.
func SavePending(directory string, pending PendingEnrollment) error {
	return saveJSON(directory, "machine-enrollment.json", ".machine-enrollment-*.json", "pending Machine enrollment", pending)
}

func LoadPending(directory string) (PendingEnrollment, error) {
	var pending PendingEnrollment
	if err := loadJSON(filepath.Join(directory, "machine-enrollment.json"), "pending Machine enrollment", &pending); err != nil {
		return PendingEnrollment{}, err
	}
	return pending, nil
}

func RemovePending(directory string) error {
	path := filepath.Join(directory, "machine-enrollment.json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pending Machine enrollment: %w", err)
	}
	return syncDirectory(directory)
}

func saveJSON(directory string, filename string, pattern string, description string, value any) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create %s directory: %w", description, err)
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

func loadCredential(path string, description string) (Credential, error) {
	var credential Credential
	if err := loadJSON(path, description, &credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func loadJSON(path string, description string, destination any) error {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", description, err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		return fmt.Errorf("decode %s: %w", description, err)
	}
	return nil
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
