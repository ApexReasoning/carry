package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	carryserver "github.com/ApexReasoning/carry/internal/server"
)

type config struct {
	listenAddress string
	databaseURL   string
	pkiDirectory  string
}

type bootstrapConfig struct {
	databaseURL    string
	displayName    string
	spaceName      string
	tokenTTL       time.Duration
	credentialFile string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		slog.Error("carry-server stopped", "error", err)
		os.Exit(1)
	}
}

func parseConfig(arguments []string, stderr io.Writer) (config, error) {
	flags := flag.NewFlagSet("carry-server", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var parsed config
	flags.StringVar(&parsed.listenAddress, "listen", "127.0.0.1:8080", "HTTPS listen address")
	flags.StringVar(&parsed.databaseURL, "database-url", os.Getenv("CARRY_DATABASE_URL"), "PostgreSQL connection URL")
	flags.StringVar(&parsed.pkiDirectory, "pki-dir", os.Getenv("CARRY_PKI_DIR"), "Carry PKI directory")
	if err := flags.Parse(arguments); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if strings.TrimSpace(parsed.databaseURL) == "" {
		return config{}, errors.New("database URL is required through --database-url or CARRY_DATABASE_URL")
	}
	if strings.TrimSpace(parsed.pkiDirectory) == "" {
		return config{}, errors.New("PKI directory is required through --pki-dir or CARRY_PKI_DIR")
	}
	return parsed, nil
}

func parseBootstrapConfig(arguments []string, stderr io.Writer) (bootstrapConfig, error) {
	flags := flag.NewFlagSet("carry-server bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var parsed bootstrapConfig
	flags.StringVar(&parsed.databaseURL, "database-url", os.Getenv("CARRY_DATABASE_URL"), "PostgreSQL connection URL")
	flags.StringVar(&parsed.displayName, "display-name", "", "initial member display name")
	flags.StringVar(&parsed.spaceName, "space", "", "initial Space name")
	flags.DurationVar(&parsed.tokenTTL, "token-ttl", 90*24*time.Hour, "initial member token lifetime")
	flags.StringVar(&parsed.credentialFile, "credential-file", "", "durable initial member credential file")
	if err := flags.Parse(arguments); err != nil {
		return bootstrapConfig{}, fmt.Errorf("parse bootstrap flags: %w", err)
	}
	if flags.NArg() != 0 {
		return bootstrapConfig{}, fmt.Errorf("unexpected bootstrap arguments: %v", flags.Args())
	}
	if strings.TrimSpace(parsed.databaseURL) == "" {
		return bootstrapConfig{}, errors.New("database URL is required through --database-url or CARRY_DATABASE_URL")
	}
	if strings.TrimSpace(parsed.displayName) == "" {
		return bootstrapConfig{}, errors.New("--display-name is required")
	}
	if strings.TrimSpace(parsed.spaceName) == "" {
		return bootstrapConfig{}, errors.New("--space is required")
	}
	if parsed.tokenTTL <= 0 {
		return bootstrapConfig{}, errors.New("--token-ttl must be positive")
	}
	if strings.TrimSpace(parsed.credentialFile) == "" {
		return bootstrapConfig{}, errors.New("--credential-file is required")
	}
	return parsed, nil
}

func run(ctx context.Context, arguments []string, stdout io.Writer, stderr io.Writer) error {
	if len(arguments) >= 2 && arguments[0] == "pki" && arguments[1] == "init" {
		parsed, err := parsePKIInitConfig(arguments[2:], stderr)
		if err != nil {
			return err
		}
		return initializePKI(parsed)
	}
	if len(arguments) != 0 && arguments[0] == "bootstrap" {
		parsed, err := parseBootstrapConfig(arguments[1:], stderr)
		if err != nil {
			return err
		}
		return bootstrap(ctx, parsed, stdout)
	}
	if len(arguments) != 0 && arguments[0] == "serve" {
		arguments = arguments[1:]
	}

	parsed, err := parseConfig(arguments, stderr)
	if err != nil {
		return err
	}
	pool, err := carrypostgres.Open(ctx, parsed.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := carrypostgres.Migrate(ctx, pool); err != nil {
		return err
	}
	tlsConfig, authority, err := loadServerPKI(
		filepath.Join(parsed.pkiDirectory, "ca.pem"),
		filepath.Join(parsed.pkiDirectory, "ca-key.pem"),
		filepath.Join(parsed.pkiDirectory, "server.pem"),
		filepath.Join(parsed.pkiDirectory, "server-key.pem"),
	)
	if err != nil {
		return err
	}

	store := carrypostgres.NewStore(pool)
	memberRoutes, err := carryserver.NewMemberRoutes(store, store, store, store, store, store, authority)
	if err != nil {
		return fmt.Errorf("compose member routes: %w", err)
	}
	machineRoutes, err := carryserver.NewMachineRoutes(store)
	if err != nil {
		return fmt.Errorf("compose Machine routes: %w", err)
	}
	apiServer, err := carryserver.NewAPI(pool, memberRoutes, machineRoutes)
	if err != nil {
		return fmt.Errorf("compose Carry API: %w", err)
	}
	httpServer := &http.Server{
		Addr:              parsed.listenAddress,
		Handler:           apiServer.Handler(),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- httpServer.ListenAndServeTLS("", "")
	}()

	var result error
	serverStopped := false
	select {
	case err := <-serveResult:
		serverStopped = true
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			result = fmt.Errorf("serve HTTP: %w", err)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && result == nil {
		result = fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if !serverStopped {
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) && result == nil {
			result = fmt.Errorf("serve HTTP after shutdown: %w", err)
		}
	}
	return result
}

func bootstrap(ctx context.Context, parsed bootstrapConfig, stdout io.Writer) error {
	command, err := loadOrCreateBootstrapCredential(
		parsed.credentialFile, parsed.displayName, parsed.spaceName, time.Now().Add(parsed.tokenTTL),
	)
	if err != nil {
		return fmt.Errorf("prepare bootstrap credential: %w", err)
	}
	pool, err := carrypostgres.Open(ctx, parsed.databaseURL)
	if err != nil {
		return fmt.Errorf("open PostgreSQL for bootstrap: %w", err)
	}
	defer pool.Close()
	if err := carrypostgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate PostgreSQL for bootstrap: %w", err)
	}

	result, err := carrypostgres.NewStore(pool).Bootstrap(ctx, command)
	if err != nil {
		return fmt.Errorf("bootstrap Carry: %w", err)
	}
	if err := json.NewEncoder(stdout).Encode(struct {
		UserID    string `json:"user_id"`
		SpaceID   string `json:"space_id"`
		UserToken string `json:"user_token"`
	}{
		UserID:    result.UserID,
		SpaceID:   result.SpaceID,
		UserToken: result.UserToken,
	}); err != nil {
		return fmt.Errorf("write bootstrap credential: %w", err)
	}
	return nil
}
