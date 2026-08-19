package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
)

type pkiInitConfig struct {
	directory string
	hosts     string
}

func parsePKIInitConfig(arguments []string, stderr io.Writer) (pkiInitConfig, error) {
	flags := flag.NewFlagSet("carry-server pki init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var parsed pkiInitConfig
	flags.StringVar(&parsed.directory, "dir", "", "directory for generated certificates")
	flags.StringVar(&parsed.hosts, "hosts", "localhost,127.0.0.1", "comma-separated server DNS names or IP addresses")
	if err := flags.Parse(arguments); err != nil {
		return pkiInitConfig{}, fmt.Errorf("parse PKI flags: %w", err)
	}
	if flags.NArg() != 0 {
		return pkiInitConfig{}, fmt.Errorf("unexpected PKI arguments: %v", flags.Args())
	}
	if strings.TrimSpace(parsed.directory) == "" {
		return pkiInitConfig{}, errors.New("--dir is required")
	}
	return parsed, nil
}

func initializePKI(parsed pkiInitConfig) error {
	var serverNames []string
	for _, name := range strings.Split(parsed.hosts, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			serverNames = append(serverNames, name)
		}
	}
	bundle, err := host.CreatePKI(serverNames, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(parsed.directory, 0o700); err != nil {
		return fmt.Errorf("create PKI directory: %w", err)
	}

	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{name: "ca.pem", data: bundle.CACertificatePEM, mode: 0o644},
		{name: "ca-key.pem", data: bundle.CAPrivateKeyPEM, mode: 0o600},
		{name: "server.pem", data: bundle.ServerCertificatePEM, mode: 0o644},
		{name: "server-key.pem", data: bundle.ServerPrivateKeyPEM, mode: 0o600},
	}
	created := make([]string, 0, len(files))
	for _, file := range files {
		path := filepath.Join(parsed.directory, file.name)
		if err := writeExclusive(path, file.data, file.mode); err != nil {
			for _, createdPath := range created {
				_ = os.Remove(createdPath)
			}
			return err
		}
		created = append(created, path)
	}
	return nil
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	remove = false
	return nil
}

func loadServerPKI(
	caCertificatePath string,
	caPrivateKeyPath string,
	serverCertificatePath string,
	serverPrivateKeyPath string,
) (*tls.Config, *host.CertificateAuthority, error) {
	caCertificatePEM, err := os.ReadFile(caCertificatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA certificate: %w", err)
	}
	caPrivateKeyPEM, err := os.ReadFile(caPrivateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA private key: %w", err)
	}
	authority, err := host.LoadCertificateAuthority(caCertificatePEM, caPrivateKeyPEM)
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
