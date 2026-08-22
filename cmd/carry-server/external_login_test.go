package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestProviderConfigurationRequiresCanonicalExternalOriginAndCredentials(t *testing.T) {
	setRequiredServerEnvironment(t)
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "HTTP origin", key: "CARRY_EXTERNAL_ORIGIN", value: "http://carry.example"},
		{name: "origin path", key: "CARRY_EXTERNAL_ORIGIN", value: "https://carry.example/path"},
		{name: "missing Host API origin",
			key:   "CARRY_HOST_API_ORIGIN",
			value: ""},
		{name: "Host API origin path",
			key:   "CARRY_HOST_API_ORIGIN",
			value: "https://api.carry.example/path"},
		{name: "missing Google ID", key: "CARRY_GOOGLE_CLIENT_ID", value: ""},
		{name: "missing Google secret", key: "CARRY_GOOGLE_CLIENT_SECRET", value: ""},
		{name: "missing GitHub ID", key: "CARRY_GITHUB_CLIENT_ID", value: ""},
		{name: "missing GitHub secret", key: "CARRY_GITHUB_CLIENT_SECRET", value: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			if _, err := parseConfig(nil, io.Discard); err == nil {
				t.Fatal("invalid provider configuration was accepted")
			}
		})
	}
}

func TestGoogleAuthorizationAndProofUseOpenIDNoncePKCEAndStableSubject(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	nonce := "transaction-bound-nonce"
	clientID := "google-client"
	var tokenCalls int
	fixture := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenCalls++
			if err := request.ParseForm(); err != nil {
				http.Error(response, "invalid form", http.StatusBadRequest)
				return
			}
			code := request.Form.Get("code")
			if code == "" || request.Form.Get("code_verifier") != "pkce-verifier" ||
				request.Form.Get("redirect_uri") != "https://carry.example/v1/auth/google/callback" ||
				request.Form.Get("client_id") != clientID || request.Form.Get("client_secret") != "google-secret" ||
				request.Header.Get("Authorization") != "" {
				http.Error(response, "wrong exchange", http.StatusBadRequest)
				return
			}
			now := time.Now()
			var audience any = clientID
			expiresAt := now.Add(5 * time.Minute)
			issuedAt := now
			switch code {
			case "wrong-audience":
				audience = "another-client"
			case "multiple-audiences":
				audience = []string{clientID, "another-client"}
			case "expired":
				expiresAt = now.Add(-time.Minute)
			case "future-issued":
				issuedAt = now.Add(2 * time.Minute)
			}
			token := signedGoogleIDToken(t, privateKey, audience, nonce, issuedAt, expiresAt)
			if code == "wrong-signature" {
				token = corruptJWTSignature(t, token)
			}
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(map[string]any{
				"access_token": "transient-google-token", "token_type": "Bearer", "expires_in": 300,
				"id_token": token,
			})
		case "/jwks":
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(map[string]any{"keys": []jose.JSONWebKey{{
				Key: &privateKey.PublicKey, KeyID: "carry-test-key", Algorithm: "RS256", Use: "sig",
			}}})
		default:
			http.Error(response, "unexpected provider request", http.StatusNotFound)
		}
	}))
	defer fixture.Close()

	login, err := newGoogleLoginAt(
		clientID,
		"google-secret",
		"https://carry.example/v1/auth/google/callback",
		googleEndpoints{authorization: fixture.URL + "/authorize", token: fixture.URL + "/token", jwks: fixture.URL + "/jwks"},
	)
	if err != nil {
		t.Fatalf("configure Google login: %v", err)
	}
	authorization, err := url.Parse(login.AuthorizationURL("opaque-state", nonce, "s256-challenge"))
	if err != nil {
		t.Fatalf("parse Google authorization URL: %v", err)
	}
	query := authorization.Query()
	if query.Get("scope") != "openid" || query.Get("state") != "opaque-state" || query.Get("nonce") != nonce ||
		query.Get("code_challenge") != "s256-challenge" || query.Get("code_challenge_method") != "S256" ||
		query.Get("access_type") != "online" {
		t.Fatalf("Google authorization query = %#v", query)
	}
	proof, err := login.Authenticate(context.Background(), "provider-code", "pkce-verifier", nonce)
	if err != nil {
		t.Fatalf("authenticate Google User: %v", err)
	}
	if proof.Issuer != canonicalGoogleIssuer || proof.Subject != "stable-google-subject" {
		t.Fatalf("Google proof = %#v", proof)
	}
	if _, err := login.Authenticate(context.Background(), "provider-code", "pkce-verifier", "wrong-nonce"); err == nil {
		t.Fatal("wrong Google nonce was accepted")
	}
	for _, invalidCode := range []string{
		"wrong-audience", "multiple-audiences", "expired", "future-issued", "wrong-signature",
	} {
		if _, err := login.Authenticate(context.Background(), invalidCode, "pkce-verifier", nonce); err == nil {
			t.Fatalf("Google proof %q was accepted", invalidCode)
		}
	}
	if tokenCalls != 7 {
		t.Fatalf("Google token calls = %d, want one per authentication", tokenCalls)
	}
}

func TestGitHubAuthorizationUsesNoScopeAndEveryLoginLoadsNumericUserID(t *testing.T) {
	var tokenCalls, userCalls int
	fixture := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenCalls++
			if err := request.ParseForm(); err != nil || request.Form.Get("code") != "provider-code" ||
				request.Form.Get("code_verifier") != "pkce-verifier" ||
				request.Form.Get("redirect_uri") != "https://carry.example/v1/auth/github/callback" ||
				request.Form.Get("client_id") != "github-client" || request.Form.Get("client_secret") != "github-secret" ||
				request.Header.Get("Authorization") != "" {
				http.Error(response, "wrong exchange", http.StatusBadRequest)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(map[string]any{
				"access_token": "transient-github-token", "token_type": "bearer", "scope": "",
			})
		case "/user":
			userCalls++
			if request.Header.Get("Authorization") != "Bearer transient-github-token" {
				http.Error(response, "missing token", http.StatusUnauthorized)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":922337203685477580,"login":"renamed","email":"same@example.com"}`))
		default:
			http.Error(response, "unexpected provider request", http.StatusNotFound)
		}
	}))
	defer fixture.Close()
	login, err := newGitHubLoginAt(
		"github-client",
		"github-secret",
		"https://carry.example/v1/auth/github/callback",
		githubEndpoints{authorization: fixture.URL + "/authorize", token: fixture.URL + "/token", user: fixture.URL + "/user"},
	)
	if err != nil {
		t.Fatalf("configure GitHub login: %v", err)
	}
	authorization, err := url.Parse(login.AuthorizationURL("opaque-state", "s256-challenge"))
	if err != nil {
		t.Fatalf("parse GitHub authorization URL: %v", err)
	}
	query := authorization.Query()
	if _, exists := query["scope"]; exists || query.Get("state") != "opaque-state" ||
		query.Get("code_challenge") != "s256-challenge" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("GitHub authorization query = %#v", query)
	}
	for range 2 {
		proof, err := login.Authenticate(context.Background(), "provider-code", "pkce-verifier")
		if err != nil {
			t.Fatalf("authenticate GitHub User: %v", err)
		}
		if proof.UserID != 922337203685477580 {
			t.Fatalf("GitHub proof = %#v", proof)
		}
	}
	if tokenCalls != 2 || userCalls != 2 {
		t.Fatalf("token calls = %d, User calls = %d", tokenCalls, userCalls)
	}
}

func TestGitHubRejectsNonPositiveFractionalAndOverflowingUserIDs(t *testing.T) {
	for _, userResponse := range []string{
		`{"id":0}`, `{"id":-1}`, `{"id":1.5}`, `{"id":9223372036854775808}`, `{"id":"42"}`, `{}`,
	} {
		t.Run(strings.ReplaceAll(userResponse, "\"", ""), func(t *testing.T) {
			fixture := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/token" {
					_, _ = response.Write([]byte(`{"access_token":"token","token_type":"bearer"}`))
					return
				}
				_, _ = response.Write([]byte(userResponse))
			}))
			defer fixture.Close()
			login, err := newGitHubLoginAt("id", "secret", "https://carry.example/v1/auth/github/callback", githubEndpoints{
				authorization: fixture.URL + "/authorize", token: fixture.URL + "/token", user: fixture.URL + "/user",
			})
			if err != nil {
				t.Fatalf("configure GitHub login: %v", err)
			}
			if _, err := login.Authenticate(context.Background(), "code", "verifier"); err == nil {
				t.Fatal("invalid GitHub User identity was accepted")
			}
		})
	}
}

func signedGoogleIDToken(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	audience any,
	nonce string,
	issuedAt time.Time,
	expiresAt time.Time,
) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "carry-test-key"),
	)
	if err != nil {
		t.Fatalf("create Google ID token signer: %v", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iss": "accounts.google.com", "sub": "stable-google-subject", "aud": audience,
		"exp": expiresAt.Unix(), "iat": issuedAt.Unix(), "nonce": nonce,
	})
	if err != nil {
		t.Fatalf("encode Google ID token claims: %v", err)
	}
	signed, err := signer.Sign(claims)
	if err != nil {
		t.Fatalf("sign Google ID token: %v", err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize Google ID token: %v", err)
	}
	return compact
}

func corruptJWTSignature(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] == "" {
		t.Fatalf("signed token has invalid compact form")
	}
	parts[2] = alternateBase64URLCharacter(parts[2][0]) + parts[2][1:]
	return strings.Join(parts, ".")
}

func alternateBase64URLCharacter(current byte) string {
	if current == 'A' {
		return "B"
	}
	return "A"
}

func setRequiredServerEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("CARRY_DATABASE_URL", "postgres://carry-test")
	t.Setenv("CARRY_PKI_DIR", "/tmp/carry-test-pki")
	t.Setenv("CARRY_IDENTITY_ROOT", strings.Repeat("A", 43))
	t.Setenv("CARRY_EXTERNAL_ORIGIN", "https://carry.example")
	t.Setenv("CARRY_HOST_API_ORIGIN", "https://api.carry.example")
	t.Setenv("CARRY_GOOGLE_CLIENT_ID", "google-client")
	t.Setenv("CARRY_GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("CARRY_GITHUB_CLIENT_ID", "github-client")
	t.Setenv("CARRY_GITHUB_CLIENT_SECRET", "github-secret")
	t.Setenv("CARRY_RESEND_API_KEY", "resend-key")
	t.Setenv("CARRY_EMAIL_FROM", "Carry <login@example.com>")
}
