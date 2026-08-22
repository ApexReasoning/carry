package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
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
	for name := range strings.SplitSeq(parsed.hosts, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			serverNames = append(serverNames, name)
		}
	}
	root, err := openPrivatePKIDirectory(parsed.directory)
	if err != nil {
		return err
	}
	defer root.Close()
	bundle, err := machine.CreateCertificateBundle(serverNames, time.Now().UTC())
	if err != nil {
		return err
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
		if err := writeExclusive(root, file.name, file.data, file.mode); err != nil {
			for _, createdName := range created {
				_ = root.Remove(createdName)
			}
			return err
		}
		created = append(created, file.name)
	}
	directory, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("open PKI directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync PKI directory: %w", err)
	}
	return nil
}

func openPrivatePKIDirectory(directory string) (*os.Root, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create PKI directory: %w", err)
	}
	pathInfo, err := os.Lstat(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect PKI directory: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
		return nil, errors.New("PKI directory is not a directory")
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open PKI directory: %w", err)
	}
	openedInfo, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("inspect opened PKI directory: %w", err)
	}
	if !os.SameFile(pathInfo, openedInfo) || !openedInfo.IsDir() {
		root.Close()
		return nil, errors.New("PKI directory changed while opening")
	}
	if runtime.GOOS != "windows" && openedInfo.Mode().Perm()&0o077 != 0 {
		if err := root.Chmod(".", 0o700); err != nil {
			root.Close()
			return nil, fmt.Errorf("protect PKI directory: %w", err)
		}
	}
	return root, nil
}

func writeExclusive(root *os.Root, name string, data []byte, mode os.FileMode) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = root.Remove(name)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	remove = false
	return nil
}
