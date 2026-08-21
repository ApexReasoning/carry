//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/identity"
	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	carryserver "github.com/ApexReasoning/carry/internal/server"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRemovedMemberCredentialsRetainUserButLoseExactSpaceHTTPAccess(t *testing.T) {
	ctx := context.Background()
	pool := openExternalLoginTestPool(t, ctx)
	store := carrypostgres.NewStore(pool)
	credentials, err := identity.NewCredentials(bytes.Repeat([]byte{9}, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}
	googleFixture := newGoogleProviderFixture(t)
	defer googleFixture.Close()
	githubFixture := newGitHubProviderFixture(t)
	defer githubFixture.Close()
	carry := httptest.NewUnstartedServer(nil)
	origin, err := carryserver.ParseExternalOrigin("https://" + carry.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse Carry origin: %v", err)
	}
	google, err := newGoogleLoginAt(
		"google-client", "google-secret", origin.CallbackURL(identity.GoogleLoginProvider),
		googleEndpoints{authorization: googleFixture.URL + "/authorize", token: googleFixture.URL + "/token", jwks: googleFixture.URL + "/jwks"},
	)
	if err != nil {
		t.Fatalf("configure Google fixture: %v", err)
	}
	github, err := newGitHubLoginAt(
		"github-client", "github-secret", origin.CallbackURL(identity.GitHubLoginProvider),
		githubEndpoints{authorization: githubFixture.URL + "/authorize", token: githubFixture.URL + "/token", user: githubFixture.URL + "/user"},
	)
	if err != nil {
		t.Fatalf("configure GitHub fixture: %v", err)
	}
	carry.Config.Handler = composeExternalLoginTestAPI(t, pool, store, store, credentials, origin, google, github)
	carry.StartTLS()
	defer carry.Close()

	managerID := uuid.NewString()
	targetID := uuid.NewString()
	removedSpaceID := uuid.NewString()
	retainedSpaceID := uuid.NewString()
	managerSessionID := uuid.NewString()
	targetSessionID := uuid.NewString()
	targetCredentialID := uuid.NewString()
	targetCredential, err := credentials.CLICredential(targetCredentialID, origin.String())
	if err != nil {
		t.Fatalf("create target CLI credential: %v", err)
	}
	seedMemberRemovalHTTPFixture(
		t, ctx, pool, managerID, targetID, removedSpaceID, retainedSpaceID,
		managerSessionID, targetSessionID, targetCredentialID,
	)
	if _, err := store.SendConversationMessage(ctx, conversation.SendCommand{
		SpaceID: removedSpaceID, MemberUserID: targetID, Text: "Retained private fixture",
		IdempotencyKey: "retained-private-http",
	}); err != nil {
		t.Fatalf("seed retained private message: %v", err)
	}

	manager := browserClient(t, carry, credentials, managerSessionID)
	removeBody := bytes.NewBufferString(`{}`)
	removeRequest, err := http.NewRequest(http.MethodPost, carry.URL+"/v1/spaces/"+removedSpaceID+"/members/"+targetID+"/remove", removeBody)
	if err != nil {
		t.Fatalf("build public removal request: %v", err)
	}
	removeRequest.Header.Set("Content-Type", "application/json")
	removeRequest.Header.Set("Origin", carry.URL)
	removeRequest.Header.Set("Idempotency-Key", "public-remove-member")
	removeResponse, err := manager.Do(removeRequest)
	if err != nil {
		t.Fatalf("send public removal: %v", err)
	}
	defer removeResponse.Body.Close()
	if removeResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("public removal status = %d, body = %s", removeResponse.StatusCode, readBody(removeResponse))
	}

	targetBrowser := browserClient(t, carry, credentials, targetSessionID)
	assertCurrentUserSpaces(t, targetBrowser, carry.URL, targetID, []string{retainedSpaceID})
	assertHTTPStatus(t, targetBrowser, http.MethodGet, carry.URL+"/v1/spaces/"+removedSpaceID+"/members", "", nil, http.StatusForbidden)
	assertHTTPStatus(t, targetBrowser, http.MethodGet, carry.URL+"/v1/spaces/"+removedSpaceID+"/conversation/messages", "", nil, http.StatusForbidden)
	assertHTTPStatus(t, targetBrowser, http.MethodGet, carry.URL+"/v1/spaces/"+retainedSpaceID+"/members", "", nil, http.StatusOK)

	bearer := *carry.Client()
	bearer.Jar = nil
	assertCurrentUserSpacesWithBearer(t, &bearer, carry.URL, targetCredential, targetID, []string{retainedSpaceID})
	assertHTTPStatus(t, &bearer, http.MethodGet, carry.URL+"/v1/spaces/"+removedSpaceID+"/works", targetCredential, nil, http.StatusForbidden)
	assertHTTPStatus(t, &bearer, http.MethodGet, carry.URL+"/v1/spaces/"+removedSpaceID+"/conversation/messages", targetCredential, nil, http.StatusForbidden)
	machineBody := bytes.NewBufferString(`{"space_id":"` + removedSpaceID + `","machine_id":"` + uuid.NewString() + `"}`)
	assertHTTPStatus(t, &bearer, http.MethodPost, carry.URL+"/v1/machines/revoke", targetCredential, machineBody, http.StatusForbidden)
}

func seedMemberRemovalHTTPFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	managerID, targetID, removedSpaceID, retainedSpaceID, managerSessionID, targetSessionID string,
	credentialID string,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin public removal fixture: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	statements := []struct {
		sql  string
		args []any
	}{
		{`insert into carry_users (user_id, display_name) values ($1, 'Removal Manager'), ($2, 'Former Member')`, []any{managerID, targetID}},
		{`insert into spaces (space_id, name) values ($1, 'Removed Space'), ($2, 'Retained Space')`, []any{removedSpaceID, retainedSpaceID}},
		{`insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines) values ($1, $2, true, true), ($1, $3, true, true), ($4, $3, false, false)`, []any{removedSpaceID, managerID, targetID, retainedSpaceID}},
		{`insert into browser_sessions (session_id, user_id, identity_proof_method, expires_at) values ($1, $2, 'email', transaction_timestamp() + interval '30 days'), ($3, $4, 'email', transaction_timestamp() + interval '30 days')`, []any{managerSessionID, managerID, targetSessionID, targetID}},
		{`insert into cli_login_requests (
			request_id, begin_idempotency_key, begin_request_digest, user_code_digest, code_generation, source_digest, label,
			created_at, expires_at, approved_at, approved_by_user_id, approved_space_id,
			approval_idempotency_key, approval_request_digest, prepared_credential_id
		) values ($1, $2, decode(repeat('00', 32), 'hex'), decode(repeat('01', 32), 'hex'), 0, decode(repeat('02', 32), 'hex'),
			'former member CLI', transaction_timestamp(), transaction_timestamp() + interval '15 minutes', transaction_timestamp(),
			$3, $4, 'fixture-approval', decode(repeat('03', 32), 'hex'), $5)`, []any{uuid.NewString(), "fixture-cli-" + credentialID, targetID, retainedSpaceID, credentialID}},
		{`insert into cli_credentials (credential_id, login_request_id, user_id, label)
			select $1, request_id, $2, 'former member CLI' from cli_login_requests where prepared_credential_id = $1`, []any{credentialID, targetID}},
		{`update cli_login_requests set resulting_credential_id = $1, redeemed_at = transaction_timestamp(), replay_until = transaction_timestamp() + interval '5 minutes' where prepared_credential_id = $1`, []any{credentialID}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed public removal fixture: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit public removal fixture: %v", err)
	}
}

func browserClient(t *testing.T, carry *httptest.Server, credentials identity.Credentials, sessionID string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create Browser cookie jar: %v", err)
	}
	credential, err := credentials.BrowserSessionCredential(sessionID)
	if err != nil {
		t.Fatalf("create Browser credential: %v", err)
	}
	origin, _ := url.Parse(carry.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "__Host-carry_session", Value: credential, Path: "/", Secure: true}})
	client := *carry.Client()
	client.Jar = jar
	client.CheckRedirect = noRedirect
	return &client
}

func assertCurrentUserSpaces(t *testing.T, client *http.Client, serverURL, userID string, wantSpaces []string) {
	t.Helper()
	assertCurrentUserSpacesRequest(t, client, serverURL, "", userID, wantSpaces)
}

func assertCurrentUserSpacesWithBearer(t *testing.T, client *http.Client, serverURL, bearer, userID string, wantSpaces []string) {
	t.Helper()
	assertCurrentUserSpacesRequest(t, client, serverURL, bearer, userID, wantSpaces)
}

func assertCurrentUserSpacesRequest(t *testing.T, client *http.Client, serverURL, bearer, userID string, wantSpaces []string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, serverURL+"/v1/me", nil)
	if err != nil {
		t.Fatalf("build current User request: %v", err)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("load current User: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("current User status = %d, body = %s", response.StatusCode, readBody(response))
	}
	var body struct {
		UserID string `json:"user_id"`
		Spaces []struct {
			SpaceID string `json:"space_id"`
		} `json:"spaces"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode current User: %v", err)
	}
	if body.UserID != userID || len(body.Spaces) != len(wantSpaces) {
		t.Fatalf("current User = %s spaces = %#v", body.UserID, body.Spaces)
	}
	for index, spaceID := range wantSpaces {
		if body.Spaces[index].SpaceID != spaceID {
			t.Fatalf("current User Space %d = %s, want %s", index, body.Spaces[index].SpaceID, spaceID)
		}
	}
}

func assertHTTPStatus(t *testing.T, client *http.Client, method, requestURL, bearer string, body io.Reader, want int) {
	t.Helper()
	request, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		t.Fatalf("build HTTP fixture request: %v", err)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "removed-machine-operation")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send HTTP fixture request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, requestURL, response.StatusCode, want, readBody(response))
	}
}
