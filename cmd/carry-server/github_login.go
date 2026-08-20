package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"golang.org/x/oauth2"
)

const (
	githubAuthorizationEndpoint = "https://github.com/login/oauth/authorize"
	githubTokenEndpoint         = "https://github.com/login/oauth/access_token"
	githubUserEndpoint          = "https://api.github.com/user"
)

type githubEndpoints struct {
	authorization string
	token         string
	user          string
}

type githubLogin struct {
	config       oauth2.Config
	userEndpoint string
	client       *http.Client
}

func newGitHubLogin(clientID string, clientSecret string, redirectURL string) (*githubLogin, error) {
	return newGitHubLoginAt(clientID, clientSecret, redirectURL, githubEndpoints{
		authorization: githubAuthorizationEndpoint,
		token:         githubTokenEndpoint,
		user:          githubUserEndpoint,
	})
}

func newGitHubLoginAt(
	clientID string,
	clientSecret string,
	redirectURL string,
	endpoints githubEndpoints,
) (*githubLogin, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" ||
		strings.TrimSpace(redirectURL) == "" || strings.TrimSpace(endpoints.authorization) == "" ||
		strings.TrimSpace(endpoints.token) == "" || strings.TrimSpace(endpoints.user) == "" {
		return nil, errors.New("GitHub OAuth configuration is incomplete")
	}
	return &githubLogin{
		config: oauth2.Config{
			ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
			Endpoint: oauth2.Endpoint{
				AuthURL: endpoints.authorization, TokenURL: endpoints.token,
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes: nil,
		},
		userEndpoint: endpoints.user,
		client:       newOAuthHTTPClient(),
	}, nil
}

func (login *githubLogin) AuthorizationURL(state string, codeChallenge string) string {
	return login.config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (login *githubLogin) Authenticate(
	ctx context.Context,
	code string,
	codeVerifier string,
) (identity.GitHubIdentityProof, error) {
	providerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	providerCtx = context.WithValue(providerCtx, oauth2.HTTPClient, login.client)
	token, err := login.config.Exchange(providerCtx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return identity.GitHubIdentityProof{}, fmt.Errorf("exchange GitHub authorization code: %w", err)
	}
	if token.AccessToken == "" {
		return identity.GitHubIdentityProof{}, errors.New("GitHub token response has no access token")
	}
	request, err := http.NewRequestWithContext(providerCtx, http.MethodGet, login.userEndpoint, nil)
	if err != nil {
		return identity.GitHubIdentityProof{}, fmt.Errorf("create GitHub User request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := login.client.Do(request)
	if err != nil {
		return identity.GitHubIdentityProof{}, fmt.Errorf("load GitHub User: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, providerResponseLimit+1))
	if err != nil {
		return identity.GitHubIdentityProof{}, fmt.Errorf("read GitHub User response: %w", err)
	}
	if len(body) > providerResponseLimit {
		return identity.GitHubIdentityProof{}, errors.New("GitHub User response is too large")
	}
	if response.StatusCode != http.StatusOK {
		return identity.GitHubIdentityProof{}, fmt.Errorf("GitHub User response status %d", response.StatusCode)
	}
	var payload struct {
		ID json.RawMessage `json:"id"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(&payload); err != nil {
		return identity.GitHubIdentityProof{}, errors.New("GitHub User response is invalid")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return identity.GitHubIdentityProof{}, errors.New("GitHub User response is invalid")
	}
	rawUserID := string(payload.ID)
	if rawUserID == "" || rawUserID[0] < '1' || rawUserID[0] > '9' {
		return identity.GitHubIdentityProof{}, errors.New("GitHub User identity is invalid")
	}
	userID, err := strconv.ParseInt(rawUserID, 10, 64)
	if err != nil || userID <= 0 {
		return identity.GitHubIdentityProof{}, errors.New("GitHub User identity is invalid")
	}
	return identity.GitHubIdentityProof{UserID: userID}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON response has trailing content")
	}
	return nil
}
