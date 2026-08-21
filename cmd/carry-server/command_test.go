package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializePKIWritesPrivateKeysOnce(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "pki")
	if err := initializePKI(pkiInitConfig{directory: directory, hosts: "localhost,127.0.0.1"}); err != nil {
		t.Fatalf("initialize PKI: %v", err)
	}
	for _, name := range []string{"ca.pem", "ca-key.pem", "server.pem", "server-key.pem"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if name == "ca-key.pem" || name == "server-key.pem" {
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("%s mode = %o, want 600", name, got)
			}
		}
	}
	if err := initializePKI(pkiInitConfig{directory: directory, hosts: "localhost"}); err == nil {
		t.Fatal("second PKI initialization succeeded")
	}
}

func TestParseConfigRejectsUnexpectedArguments(t *testing.T) {
	setRequiredServerEnvironment(t)

	var stderr bytes.Buffer

	_, err := parseConfig([]string{"unexpected"}, &stderr)

	if err == nil {
		t.Fatal("parseConfig returned nil error")
	}
}

func TestParseTrustedProxyCIDRsAcceptsIPv4AndIPv6AndRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	prefixes, err := parseTrustedProxyCIDRs("10.0.0.0/8, 2001:db8:ffff::/48")
	if err != nil {
		t.Fatalf("parse trusted proxy CIDRs: %v", err)
	}
	if len(prefixes) != 2 || prefixes[0].String() != "10.0.0.0/8" || prefixes[1].String() != "2001:db8:ffff::/48" {
		t.Fatalf("trusted proxy CIDRs = %#v", prefixes)
	}
	for _, invalid := range []string{"not-a-cidr", "10.0.0.0/8,"} {
		if _, err := parseTrustedProxyCIDRs(invalid); err == nil {
			t.Fatalf("invalid trusted proxy CIDRs %q were accepted", invalid)
		}
	}
}
