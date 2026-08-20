package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ApexReasoning/carry/internal/machine"
)

func loadServerTLS(
	caCertificatePath string,
	caPrivateKeyPath string,
	serverCertificatePath string,
	serverPrivateKeyPath string,
) (*tls.Config, *machine.CertificateAuthority, error) {
	caCertificatePEM, err := os.ReadFile(caCertificatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA certificate: %w", err)
	}
	caPrivateKeyPEM, err := os.ReadFile(caPrivateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA private key: %w", err)
	}
	authority, err := machine.LoadCertificateAuthority(caCertificatePEM, caPrivateKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	serverCertificate, err := tls.LoadX509KeyPair(serverCertificatePath, serverPrivateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load server certificate: %w", err)
	}
	block, rest := pem.Decode(caCertificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, nil, errors.New("CA certificate PEM is invalid")
	}
	caCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(caCertificate)
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCertificate},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}, authority, nil
}
