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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 1 {
		return validateTestDatabaseURL(arguments[0])
	}
	if len(arguments) != 3 {
		return errors.New("usage: go run ./scripts/check_test_database.go [--wait DURATION | --create DATABASE_NAME | --drop DATABASE_NAME] DATABASE_URL")
	}

	switch arguments[0] {
	case "--wait":
		wait, err := time.ParseDuration(arguments[1])
		if err != nil || wait <= 0 {
			return errors.New("PostgreSQL wait duration must be positive")
		}
		if err := validateTestDatabaseURL(arguments[2]); err != nil {
			return err
		}
		return waitForTestDatabase(context.Background(), arguments[2], wait)
	case "--create":
		databaseURL, err := createTestDatabase(context.Background(), arguments[2], arguments[1])
		if err != nil {
			return err
		}
		fmt.Println(databaseURL)
		return nil
	case "--drop":
		return dropTestDatabase(context.Background(), arguments[2], arguments[1])
	default:
		return errors.New("usage: go run ./scripts/check_test_database.go [--wait DURATION | --create DATABASE_NAME | --drop DATABASE_NAME] DATABASE_URL")
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
	parsed, err := parseLocalPostgresURL(databaseURL)
	if err != nil {
		return err
	}
	databaseName := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if strings.Contains(databaseName, "/") || !testDatabaseName.MatchString(databaseName) {
		return fmt.Errorf("refusing PostgreSQL test database %q", databaseName)
	}
	return nil
}

func parseLocalPostgresURL(databaseURL string) (*url.URL, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL test URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return nil, errors.New("PostgreSQL test URL must use postgres or postgresql scheme")
	}
	hostname := parsed.Hostname()
	address := net.ParseIP(hostname)
	if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
		return nil, fmt.Errorf("refusing non-local PostgreSQL test host %q", hostname)
	}
	return parsed, nil
}

func createTestDatabase(ctx context.Context, serverURL, databaseName string) (string, error) {
	targetURL, err := testDatabaseURL(serverURL, databaseName)
	if err != nil {
		return "", err
	}
	connection, err := pgx.Connect(ctx, serverURL)
	if err != nil {
		return "", fmt.Errorf("connect to local PostgreSQL test server: %w", err)
	}
	defer connection.Close(context.Background())
	if _, err := connection.Exec(ctx, "create database "+pgx.Identifier{databaseName}.Sanitize()); err != nil {
		return "", fmt.Errorf("create isolated PostgreSQL test database: %w", err)
	}
	return targetURL, nil
}

func dropTestDatabase(ctx context.Context, serverURL, databaseName string) error {
	if _, err := testDatabaseURL(serverURL, databaseName); err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, serverURL)
	if err != nil {
		return fmt.Errorf("connect to local PostgreSQL test server: %w", err)
	}
	defer connection.Close(context.Background())
	if _, err := connection.Exec(ctx, "drop database "+pgx.Identifier{databaseName}.Sanitize()+" with (force)"); err != nil {
		return fmt.Errorf("drop isolated PostgreSQL test database: %w", err)
	}
	return nil
}

func testDatabaseURL(serverURL, databaseName string) (string, error) {
	if !testDatabaseName.MatchString(databaseName) {
		return "", fmt.Errorf("refusing PostgreSQL test database %q", databaseName)
	}
	parsed, err := parseLocalPostgresURL(serverURL)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + databaseName
	parsed.RawPath = ""
	return parsed.String(), nil
}
