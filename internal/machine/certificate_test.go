package machine

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestCreateCertificateBundleAndIssueMachineCertificate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 16, 0, 0, 0, time.UTC)
	bundle, err := CreateCertificateBundle([]string{"localhost", "127.0.0.1"}, now)
	if err != nil {
		t.Fatalf("create PKI: %v", err)
	}
	authority, err := LoadCertificateAuthority(bundle.CACertificatePEM, bundle.CAPrivateKeyPEM)
	if err != nil {
		t.Fatalf("load certificate authority: %v", err)
	}

	ca := parseCertificate(t, bundle.CACertificatePEM)
	server := parseCertificate(t, bundle.ServerCertificatePEM)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := server.Verify(x509.VerifyOptions{
		Roots: roots, DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("verify server certificate: %v", err)
	}
	if !server.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("server IP SAN = %v", server.IPAddresses)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Machine key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal Machine public key: %v", err)
	}
	issued, err := authority.IssueMachineCertificate("018f-machine", publicKeyDER, now)
	if err != nil {
		t.Fatalf("issue Machine certificate: %v", err)
	}
	machine := parseCertificate(t, issued.CertificatePEM)
	if _, err := machine.Verify(x509.VerifyOptions{
		Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("verify Machine certificate: %v", err)
	}
	wantURI := &url.URL{Scheme: "spiffe", Host: "carry", Path: "/machine/018f-machine"}
	if len(machine.URIs) != 1 || machine.URIs[0].String() != wantURI.String() {
		t.Fatalf("Machine URI SAN = %v, want %s", machine.URIs, wantURI)
	}
	machineID, err := MachineIDFromCertificate(machine)
	if err != nil {
		t.Fatalf("read Machine identity: %v", err)
	}
	if machineID != "018f-machine" {
		t.Fatalf("Machine identity = %q", machineID)
	}
	if !machine.PublicKey.(ed25519.PublicKey).Equal(privateKey.Public()) {
		t.Fatal("Machine certificate does not contain the local public key")
	}
}

func parseCertificate(t *testing.T, certificatePEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}
