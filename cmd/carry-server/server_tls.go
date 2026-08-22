package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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
	caPrivateKeyPEM, err := readPrivateKey(caPrivateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA private key: %w", err)
	}
	authority, err := machine.LoadCertificateAuthority(caCertificatePEM, caPrivateKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	serverCertificatePEM, err := os.ReadFile(serverCertificatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read server certificate: %w", err)
	}
	serverPrivateKeyPEM, err := readPrivateKey(serverPrivateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read server private key: %w", err)
	}
	serverCertificate, err := tls.X509KeyPair(serverCertificatePEM, serverPrivateKeyPEM)
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

func readPrivateKey(path string) (contents []byte, err error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return nil, errors.New("private key is not a regular file")
	}
	if runtime.GOOS != "windows" && pathInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("private key is accessible by group or others")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(pathInfo, openedInfo) || !openedInfo.Mode().IsRegular() {
		return nil, errors.New("private key changed while opening")
	}
	const maximumPrivateKeyBytes = 1 << 20
	contents, err = io.ReadAll(io.LimitReader(file, maximumPrivateKeyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maximumPrivateKeyBytes {
		return nil, errors.New("private key is too large")
	}
	return contents, nil
}
