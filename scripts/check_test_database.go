package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var testDatabaseName = regexp.MustCompile(`^carry_test_[0-9]{14}_[0-9a-f]{12}_postgres$`)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/check_test_database.go DATABASE_URL")
		os.Exit(2)
	}
	if err := validateTestDatabaseURL(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateTestDatabaseURL(databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse PostgreSQL test URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return errors.New("PostgreSQL test URL must use postgres or postgresql scheme")
	}
	hostname := parsed.Hostname()
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		return fmt.Errorf("refusing non-local PostgreSQL test host %q", hostname)
	}
	databaseName := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if strings.Contains(databaseName, "/") || !testDatabaseName.MatchString(databaseName) {
		return fmt.Errorf("refusing PostgreSQL test database %q", databaseName)
	}
	return nil
}
