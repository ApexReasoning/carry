package host

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"strings"
	"time"
)

type PKIBundle struct {
	CACertificatePEM     []byte
	CAPrivateKeyPEM      []byte
	ServerCertificatePEM []byte
	ServerPrivateKeyPEM  []byte
}

type IssuedMachineCertificate struct {
	CertificatePEM []byte
	Serial         string
}

type CertificateAuthority struct {
	certificate *x509.Certificate
	privateKey  crypto.Signer
}

func CreatePKI(serverNames []string, now time.Time) (PKIBundle, error) {
	if len(serverNames) == 0 {
		return PKIBundle{}, errors.New("at least one server name is required")
	}

	caPublicKey, caPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PKIBundle{}, fmt.Errorf("generate CA key: %w", err)
	}
	caSerial, err := certificateSerial()
	if err != nil {
		return PKIBundle{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "Carry Machine CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPublicKey, caPrivateKey)
	if err != nil {
		return PKIBundle{}, fmt.Errorf("create CA certificate: %w", err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		return PKIBundle{}, fmt.Errorf("parse created CA certificate: %w", err)
	}

	serverPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return PKIBundle{}, fmt.Errorf("generate server key: %w", err)
	}
	serverPublicKey := &serverPrivateKey.PublicKey
	serverSerial, err := certificateSerial()
	if err != nil {
		return PKIBundle{}, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject:      pkix.Name{CommonName: serverNames[0]},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, name := range serverNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return PKIBundle{}, errors.New("server name cannot be empty")
		}
		if address := net.ParseIP(name); address != nil {
			serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, address)
		} else {
			serverTemplate.DNSNames = append(serverTemplate.DNSNames, name)
		}
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader, serverTemplate, caCertificate, serverPublicKey, caPrivateKey,
	)
	if err != nil {
		return PKIBundle{}, fmt.Errorf("create server certificate: %w", err)
	}

	caKeyPEM, err := encodePrivateKey(caPrivateKey)
	if err != nil {
		return PKIBundle{}, err
	}
	serverKeyPEM, err := encodePrivateKey(serverPrivateKey)
	if err != nil {
		return PKIBundle{}, err
	}
	return PKIBundle{
		CACertificatePEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		CAPrivateKeyPEM:      caKeyPEM,
		ServerCertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		ServerPrivateKeyPEM:  serverKeyPEM,
	}, nil
}

func LoadCertificateAuthority(certificatePEM []byte, privateKeyPEM []byte) (*CertificateAuthority, error) {
	certificate, err := parseCertificatePEM(certificatePEM)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	if !certificate.IsCA {
		return nil, errors.New("CA certificate is not a certificate authority")
	}
	privateKey, err := parsePrivateKeyPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse CA private key: %w", err)
	}
	certificatePublicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal CA certificate public key: %w", err)
	}
	privatePublicKey, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		return nil, fmt.Errorf("marshal CA private public key: %w", err)
	}
	if !bytes.Equal(certificatePublicKey, privatePublicKey) {
		return nil, errors.New("CA certificate and private key do not match")
	}
	return &CertificateAuthority{certificate: certificate, privateKey: privateKey}, nil
}

func (a *CertificateAuthority) IssueMachineCertificate(
	machineID string,
	publicKeyDER []byte,
	now time.Time,
) (IssuedMachineCertificate, error) {
	if strings.TrimSpace(machineID) == "" {
		return IssuedMachineCertificate{}, errors.New("Machine ID is required")
	}
	parsedPublicKey, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return IssuedMachineCertificate{}, fmt.Errorf("parse Machine public key: %w", err)
	}
	publicKey, ok := parsedPublicKey.(ed25519.PublicKey)
	if !ok {
		return IssuedMachineCertificate{}, errors.New("Machine public key must be Ed25519")
	}
	serial, err := certificateSerial()
	if err != nil {
		return IssuedMachineCertificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: machineID},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs: []*url.URL{{
			Scheme: "spiffe",
			Host:   "carry",
			Path:   "/machine/" + machineID,
		}},
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader, template, a.certificate, publicKey, a.privateKey,
	)
	if err != nil {
		return IssuedMachineCertificate{}, fmt.Errorf("create Machine certificate: %w", err)
	}
	return IssuedMachineCertificate{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		Serial:         serial.String(),
	}, nil
}

func MachineIDFromCertificate(certificate *x509.Certificate) (string, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return "", errors.New("Machine certificate must contain one URI SAN")
	}
	identityURI := certificate.URIs[0]
	const prefix = "/machine/"
	if identityURI.Scheme != "spiffe" || identityURI.Host != "carry" ||
		!strings.HasPrefix(identityURI.Path, prefix) || len(identityURI.Path) == len(prefix) {
		return "", errors.New("Machine certificate URI SAN is invalid")
	}
	return strings.TrimPrefix(identityURI.Path, prefix), nil
}

func certificateSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	return serial, nil
}

func encodePrivateKey(privateKey crypto.Signer) ([]byte, error) {
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}), nil
}

func parsePrivateKeyPEM(privateKeyPEM []byte) (crypto.Signer, error) {
	block, rest := pem.Decode(privateKeyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("private key PEM is invalid")
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
	}
	signer, ok := privateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("private key cannot sign")
	}
	return signer, nil
}

func parseCertificatePEM(certificatePEM []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse X.509 certificate: %w", err)
	}
	return certificate, nil
}
