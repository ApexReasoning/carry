package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/machine"
	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	carryserver "github.com/ApexReasoning/carry/internal/server"
	"github.com/ApexReasoning/carry/internal/space"
)

type config struct {
	listenAddress      string
	databaseURL        string
	pkiDirectory       string
	identityRoot       string
	externalOrigin     carryserver.ExternalOrigin
	googleClientID     string
	googleClientSecret string
	githubClientID     string
	githubClientSecret string
	resendAPIKey       string
	resendAPIURL       string
	emailFrom          string
	trustedProxyCIDRs  []netip.Prefix
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
	parsed.identityRoot = os.Getenv("CARRY_IDENTITY_ROOT")
	externalOrigin, err := carryserver.ParseExternalOrigin(os.Getenv("CARRY_EXTERNAL_ORIGIN"))
	if err != nil {
		return config{}, fmt.Errorf("configure CARRY_EXTERNAL_ORIGIN: %w", err)
	}
	parsed.externalOrigin = externalOrigin
	parsed.googleClientID = os.Getenv("CARRY_GOOGLE_CLIENT_ID")
	parsed.googleClientSecret = os.Getenv("CARRY_GOOGLE_CLIENT_SECRET")
	parsed.githubClientID = os.Getenv("CARRY_GITHUB_CLIENT_ID")
	parsed.githubClientSecret = os.Getenv("CARRY_GITHUB_CLIENT_SECRET")
	parsed.resendAPIKey = os.Getenv("CARRY_RESEND_API_KEY")
	parsed.resendAPIURL = os.Getenv("CARRY_RESEND_API_URL")
	if strings.TrimSpace(parsed.resendAPIURL) == "" {
		parsed.resendAPIURL = "https://api.resend.com"
	}
	parsed.emailFrom = os.Getenv("CARRY_EMAIL_FROM")
	trustedProxyCIDRs := os.Getenv("CARRY_TRUSTED_PROXY_CIDRS")
	flags.StringVar(
		&trustedProxyCIDRs,
		"trusted-proxy-cidrs",
		trustedProxyCIDRs,
		"comma-separated reverse proxy CIDRs trusted to supply X-Forwarded-For",
	)
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
	if strings.TrimSpace(parsed.identityRoot) == "" {
		return config{}, errors.New("CARRY_IDENTITY_ROOT is required")
	}
	if strings.TrimSpace(parsed.googleClientID) == "" || strings.TrimSpace(parsed.googleClientSecret) == "" {
		return config{}, errors.New("CARRY_GOOGLE_CLIENT_ID and CARRY_GOOGLE_CLIENT_SECRET are required")
	}
	if strings.TrimSpace(parsed.githubClientID) == "" || strings.TrimSpace(parsed.githubClientSecret) == "" {
		return config{}, errors.New("CARRY_GITHUB_CLIENT_ID and CARRY_GITHUB_CLIENT_SECRET are required")
	}
	if strings.TrimSpace(parsed.resendAPIKey) == "" {
		return config{}, errors.New("CARRY_RESEND_API_KEY is required")
	}
	if strings.TrimSpace(parsed.emailFrom) == "" {
		return config{}, errors.New("CARRY_EMAIL_FROM is required")
	}
	parsedCIDRs, err := parseTrustedProxyCIDRs(trustedProxyCIDRs)
	if err != nil {
		return config{}, err
	}
	parsed.trustedProxyCIDRs = parsedCIDRs
	return parsed, nil
}

func parseTrustedProxyCIDRs(value string) ([]netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR %q is invalid", strings.TrimSpace(part))
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func run(ctx context.Context, arguments []string, _ io.Writer, stderr io.Writer) error {
	if len(arguments) >= 2 && arguments[0] == "pki" && arguments[1] == "init" {
		parsed, err := parsePKIInitConfig(arguments[2:], stderr)
		if err != nil {
			return err
		}
		return initializePKI(parsed)
	}
	if len(arguments) != 0 && arguments[0] == "serve" {
		arguments = arguments[1:]
	}

	parsed, err := parseConfig(arguments, stderr)
	if err != nil {
		return err
	}
	credentials, err := identity.ParseIdentityRoot(parsed.identityRoot)
	if err != nil {
		return fmt.Errorf("configure Identity root: %w", err)
	}
	machineCredentials, err := machine.ParseConnectionRoot(parsed.identityRoot)
	if err != nil {
		return fmt.Errorf("configure Machine connection root: %w", err)
	}
	resendSubmitter, err := newResendCodeSender(parsed.resendAPIURL, parsed.resendAPIKey, parsed.emailFrom)
	if err != nil {
		return fmt.Errorf("configure Resend: %w", err)
	}
	googleLogin, err := newGoogleLogin(
		parsed.googleClientID,
		parsed.googleClientSecret,
		parsed.externalOrigin.CallbackURL(identity.GoogleLoginProvider),
	)
	if err != nil {
		return fmt.Errorf("configure Google login: %w", err)
	}
	githubLogin, err := newGitHubLogin(
		parsed.githubClientID,
		parsed.githubClientSecret,
		parsed.externalOrigin.CallbackURL(identity.GitHubLoginProvider),
	)
	if err != nil {
		return fmt.Errorf("configure GitHub login: %w", err)
	}
	pool, err := carrypostgres.Open(ctx, parsed.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := carrypostgres.Migrate(ctx, pool); err != nil {
		return err
	}
	tlsConfig, authority, err := loadServerTLS(
		filepath.Join(parsed.pkiDirectory, "ca.pem"),
		filepath.Join(parsed.pkiDirectory, "ca-key.pem"),
		filepath.Join(parsed.pkiDirectory, "server.pem"),
		filepath.Join(parsed.pkiDirectory, "server-key.pem"),
	)
	if err != nil {
		return err
	}

	store := carrypostgres.NewStore(pool)
	emailLogin, err := identity.NewEmailLogin(store, resendSubmitter, credentials)
	if err != nil {
		return fmt.Errorf("compose email login: %w", err)
	}
	externalLogin, err := identity.NewExternalLogin(store, googleLogin, githubLogin, credentials)
	if err != nil {
		return fmt.Errorf("compose external login: %w", err)
	}
	identityMethods, err := identity.NewMethods(store, credentials)
	if err != nil {
		return fmt.Errorf("compose Identity methods: %w", err)
	}
	cliLogin, err := identity.NewCLILogin(store, credentials, parsed.externalOrigin.String())
	if err != nil {
		return fmt.Errorf("compose CLI login: %w", err)
	}
	spaceCreator, err := space.NewCreator(store)
	if err != nil {
		return fmt.Errorf("compose Space creator: %w", err)
	}
	spaceInvitations, err := space.NewInvitations(store, resendSubmitter, parsed.externalOrigin.String())
	if err != nil {
		return fmt.Errorf("compose Space invitations: %w", err)
	}
	machineConnections, err := machine.NewConnections(store, machineCredentials, authority, parsed.externalOrigin.String())
	if err != nil {
		return fmt.Errorf("compose Machine connections: %w", err)
	}
	requestSources := carryserver.NewRequestSource(parsed.trustedProxyCIDRs)
	userAuthentication, err := carryserver.NewUserAuthentication(store, store, credentials, parsed.externalOrigin)
	if err != nil {
		return fmt.Errorf("compose User authentication: %w", err)
	}
	userIdentityRoutes, err := carryserver.NewUserIdentityRoutes(
		emailLogin,
		externalLogin,
		identityMethods,
		store,
		cliLogin,
		credentials,
		parsed.externalOrigin,
		requestSources,
		store,
	)
	if err != nil {
		return fmt.Errorf("compose User identity routes: %w", err)
	}
	userSpaceRoutes, err := carryserver.NewUserSpaceRoutesWithInvitations(
		spaceCreator,
		spaceInvitations,
		store,
		store,
		credentials,
		parsed.externalOrigin,
	)
	if err != nil {
		return fmt.Errorf("compose User Space routes: %w", err)
	}
	userMachineRoutes, err := carryserver.NewUserMachineRoutes(machineConnections, credentials, parsed.externalOrigin, requestSources)
	if err != nil {
		return fmt.Errorf("compose User Machine routes: %w", err)
	}
	conversationRoutes, err := carryserver.NewConversationRoutes(store, store)
	if err != nil {
		return fmt.Errorf("compose Conversation routes: %w", err)
	}
	workRoutes, err := carryserver.NewWorkRoutes(store, store)
	if err != nil {
		return fmt.Errorf("compose Work routes: %w", err)
	}
	userRoutes, err := carryserver.NewUserRoutes(
		userAuthentication,
		userIdentityRoutes,
		userSpaceRoutes,
		userMachineRoutes,
		conversationRoutes,
		workRoutes,
	)
	if err != nil {
		return fmt.Errorf("compose User routes: %w", err)
	}
	machineRoutes, err := carryserver.NewMachineRoutes(store, store, machineConnections)
	if err != nil {
		return fmt.Errorf("compose Machine routes: %w", err)
	}
	apiServer, err := carryserver.NewAPI(pool, userRoutes, machineRoutes)
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
