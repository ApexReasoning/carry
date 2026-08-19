package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var testDatabaseName = regexp.MustCompile(`^carry_test_[0-9]{14}_[0-9a-f]{12}_postgres$`)

func main() {
	if len(os.Args) != 2 && (len(os.Args) != 4 || os.Args[1] != "--wait") {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/check_test_database.go [--wait DURATION] DATABASE_URL")
		os.Exit(2)
	}

	databaseURL := os.Args[len(os.Args)-1]
	if err := validateTestDatabaseURL(databaseURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) == 2 {
		return
	}

	wait, err := time.ParseDuration(os.Args[2])
	if err != nil || wait <= 0 {
		fmt.Fprintln(os.Stderr, "PostgreSQL wait duration must be positive")
		os.Exit(2)
	}
	if err := waitForTestDatabase(context.Background(), databaseURL, wait); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func waitForTestDatabase(parent context.Context, databaseURL string, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()

	var lastErr error
	for {
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, 2*time.Second)
		connection, err := pgx.Connect(attemptCtx, databaseURL)
		if err == nil {
			err = connection.Ping(attemptCtx)
			if closeErr := connection.Close(attemptCtx); err == nil {
				err = closeErr
			}
		}
		cancelAttempt()
		if err == nil {
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return fmt.Errorf("PostgreSQL test database did not become reachable within %s: %w", wait, lastErr)
		case <-time.After(250 * time.Millisecond):
		}
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
