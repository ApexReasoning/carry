//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	carryserver "github.com/ApexReasoning/carry/internal/server"
	"github.com/google/uuid"
)

func TestSpaceInvitationPublicBrowserJourney(t *testing.T) {
	ctx := context.Background()
	pool := openExternalLoginTestPool(t, ctx)
	store := carrypostgres.NewStore(pool)
	credentials, _ := identity.NewCredentials(bytes.Repeat([]byte{9}, identity.IdentityRootBytes))
	googleFixture := newGoogleProviderFixture(t)
	defer googleFixture.Close()
	githubFixture := newGitHubProviderFixture(t)
	defer githubFixture.Close()
	carry := httptest.NewUnstartedServer(nil)
	origin, _ := carryserver.ParseExternalOrigin("https://" + carry.Listener.Addr().String())
	google, _ := newGoogleLoginAt("google-client", "google-secret", origin.CallbackURL(identity.GoogleLoginProvider), googleEndpoints{authorization: googleFixture.URL + "/authorize", token: googleFixture.URL + "/token", jwks: googleFixture.URL + "/jwks"})
	github, _ := newGitHubLoginAt("github-client", "github-secret", origin.CallbackURL(identity.GitHubLoginProvider), githubEndpoints{authorization: githubFixture.URL + "/authorize", token: githubFixture.URL + "/token", user: githubFixture.URL + "/user"})
	var submitted struct {
		To   []string `json:"to"`
		Text string   `json:"text"`
	}
	resendFixture := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&submitted); err != nil {
			t.Fatalf("decode Resend invitation: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"concrete-invitation-email"}`))
	}))
	defer resendFixture.Close()
	emailSubmitter := &capturingEmailSubmitter{}
	resendSender, err := newResendCodeSender(resendFixture.URL, "restricted-key", "Carry <login@example.com>")
	if err != nil {
		t.Fatalf("compose concrete Resend fixture: %v", err)
	}
	carry.Config.Handler = composeExternalLoginTestAPI(t, pool, store, store, credentials, origin, google, github, resendSender, emailSubmitter)
	carry.StartTLS()
	defer carry.Close()

	managerID, managerSession, spaceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{`insert into carry_users (user_id, display_name) values ($1, 'Invitation Manager')`, []any{managerID}},
		{
			query: `insert into spaces (space_id, name, slug) values ($1::uuid, 'Research Space', replace(($1::uuid)::text, '-', ''))`,
			args: []any{
				spaceID,
			},
		},
		{`insert into space_memberships (space_id, user_id, can_manage_members, can_enroll_machines) values ($1, $2, true, true)`, []any{spaceID, managerID}},
		{`insert into browser_sessions (session_id, user_id, expires_at, identity_proof_method) values ($1, $2, transaction_timestamp() + interval '1 hour', 'email')`, []any{managerSession, managerID}},
	} {
		if _, err := pool.Exec(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seed manager: %v", err)
		}
	}
	managerCredential, _ := credentials.BrowserSessionCredential(managerSession)
	issued := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/spaces/"+spaceID+"/invitations", carry.URL, managerCredential, "issue-public", map[string]any{"email": "invitee@example.com", "can_manage_members": true, "can_enroll_machines": false})
	if issued.status != http.StatusCreated || issued.body["recipient_email"] != "invitee@example.com" || issued.body["submission"].(map[string]any)["state"] != "accepted" {
		t.Fatalf("issue = %d %#v", issued.status, issued.body)
	}
	invitationID := issued.body["invitation_id"].(string)
	exactInvitationURL := carry.URL + "/invitations/" + invitationID
	if len(submitted.To) != 1 || submitted.To[0] != "invitee@example.com" || !strings.Contains(submitted.Text, exactInvitationURL) {
		t.Fatalf("concrete invitation payload = %#v, want %q", submitted, exactInvitationURL)
	}

	loginChallenge := uuid.NewString()
	requestedLogin := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/auth/email/challenges", carry.URL, "", "request-login", map[string]any{"challenge_id": loginChallenge, "email": "invitee@example.com"})
	if requestedLogin.status != http.StatusAccepted || emailSubmitter.code == "" {
		t.Fatalf("request Email login = %d %#v", requestedLogin.status, requestedLogin.body)
	}
	verifiedLogin := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/auth/email/challenges/"+loginChallenge+"/verify", carry.URL, "", "verify-login", map[string]any{"code": emailSubmitter.code})
	if verifiedLogin.status != http.StatusNoContent || verifiedLogin.sessionCredential == "" {
		t.Fatalf("verify Email login = %d %#v", verifiedLogin.status, verifiedLogin.body)
	}
	inviteeCredential := verifiedLogin.sessionCredential
	var inviteeID string
	if err := pool.QueryRow(ctx, `select user_id from email_identities where canonical_email = 'invitee@example.com'`).Scan(&inviteeID); err != nil {
		t.Fatalf("load Email-login User: %v", err)
	}
	targeted := browserJSON(t, carry.Client(), http.MethodGet, carry.URL+"/v1/invitations/"+invitationID, "", inviteeCredential, "", nil)
	if targeted.status != http.StatusOK || targeted.body["state"] != "pending" || targeted.body["space_name"] != "Research Space" {
		t.Fatalf("targeted invitation = %d %#v", targeted.status, targeted.body)
	}
	inbox := browserJSON(t, carry.Client(), http.MethodGet, carry.URL+"/v1/invitations", "", inviteeCredential, "", nil)
	if inbox.status != http.StatusOK || len(inbox.body["invitations"].([]any)) != 1 {
		t.Fatalf("inbox = %d %#v", inbox.status, inbox.body)
	}
	var before int
	_ = pool.QueryRow(ctx, `select count(*) from space_memberships where space_id = $1 and user_id = $2`, spaceID, inviteeID).Scan(&before)
	if before != 0 {
		t.Fatalf("authentication auto-accepted Membership")
	}
	accepted := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/invitations/"+invitationID+"/accept", carry.URL, inviteeCredential, "accept-public", nil)
	if accepted.status != http.StatusOK || accepted.body["can_manage_members"] != true || accepted.body["can_enroll_machines"] != false {
		t.Fatalf("accept = %d %#v", accepted.status, accepted.body)
	}
	var manage, enroll bool
	if err := pool.QueryRow(ctx, `select can_manage_members, can_enroll_machines from space_memberships where space_id = $1 and user_id = $2 and revoked_at is null`, spaceID, inviteeID).Scan(&manage, &enroll); err != nil || !manage || enroll {
		t.Fatalf("Membership = %t/%t, %v", manage, enroll, err)
	}
	terminal := browserJSON(t, carry.Client(), http.MethodGet, carry.URL+"/v1/invitations/"+invitationID, "", inviteeCredential, "", nil)
	if terminal.status != http.StatusOK {
		t.Fatalf("terminal invitation status = %d", terminal.status)
	}
	if terminal.body["state"] != "accepted" {
		t.Fatalf("terminal invitation state = %#v", terminal.body)
	}
	if terminal.body["accept_result"] != "joined" {
		t.Fatalf("terminal invitation result = %#v", terminal.body)
	}
	if terminal.body["current_member"] != true {
		t.Fatalf("terminal invitation Membership = %#v", terminal.body)
	}

	for _, method := range []string{"google", "github"} {
		email := method + "-invitee@example.com"
		issuedProvider := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/spaces/"+spaceID+"/invitations", carry.URL, managerCredential, "issue-"+method, map[string]any{"email": email, "can_manage_members": false, "can_enroll_machines": false})
		if issuedProvider.status != http.StatusCreated {
			t.Fatalf("issue %s = %d %#v", method, issuedProvider.status, issuedProvider.body)
		}
		providerUser, providerSession := uuid.NewString(), uuid.NewString()
		if _, err := pool.Exec(ctx, `insert into carry_users (user_id, display_name) values ($1, $2)`, providerUser, method+" Member"); err != nil {
			t.Fatalf("seed %s User: %v", method, err)
		}
		if _, err := pool.Exec(ctx, `insert into email_identities (canonical_email, user_id) values ($1, $2)`, email, providerUser); err != nil {
			t.Fatalf("seed %s Email: %v", method, err)
		}
		if _, err := pool.Exec(ctx, `insert into browser_sessions (session_id, user_id, expires_at, identity_proof_method) values ($1, $2, transaction_timestamp() + interval '1 hour', $3)`, providerSession, providerUser, method); err != nil {
			t.Fatalf("seed %s Session: %v", method, err)
		}
		providerCredential, _ := credentials.BrowserSessionCredential(providerSession)
		providerInbox := browserJSON(t, carry.Client(), http.MethodGet, carry.URL+"/v1/invitations", "", providerCredential, "", nil)
		if providerInbox.status != http.StatusOK || providerInbox.body["reauthentication_required"] != true {
			t.Fatalf("%s inbox = %d %#v", method, providerInbox.status, providerInbox.body)
		}
		providerInvitationID := issuedProvider.body["invitation_id"].(string)
		blocked := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/invitations/"+providerInvitationID+"/accept", carry.URL, providerCredential, "accept-before-email-"+method, nil)
		if blocked.status != http.StatusPreconditionRequired {
			t.Fatalf("%s pre-Email accept = %d %#v", method, blocked.status, blocked.body)
		}
		if method == "github" {
			continue
		}
		reauthChallenge := uuid.NewString()
		requestedReauth := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/identity/reauthentication/email/challenges", carry.URL, providerCredential, "request-reauth-"+method, map[string]any{"challenge_id": reauthChallenge})
		if requestedReauth.status != http.StatusAccepted {
			t.Fatalf("request %s Email reauth = %d %#v", method, requestedReauth.status, requestedReauth.body)
		}
		verifiedReauth := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/identity/reauthentication/email/challenges/"+reauthChallenge+"/verify", carry.URL, providerCredential, "verify-reauth-"+method, map[string]any{"code": emailSubmitter.code})
		if verifiedReauth.status != http.StatusNoContent || verifiedReauth.sessionCredential == "" || verifiedReauth.sessionCredential == providerCredential {
			t.Fatalf("verify %s Email reauth = %d %#v", method, verifiedReauth.status, verifiedReauth.body)
		}
		confirmed := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/invitations/"+providerInvitationID+"/accept", carry.URL, verifiedReauth.sessionCredential, "accept-after-email-"+method, nil)
		if confirmed.status != http.StatusOK {
			t.Fatalf("%s confirmed accept = %d %#v", method, confirmed.status, confirmed.body)
		}
	}

	getAccept := browserJSON(t, carry.Client(), http.MethodGet, carry.URL+"/v1/invitations/"+invitationID+"/accept", "", inviteeCredential, "", nil)
	if getAccept.status != http.StatusMethodNotAllowed {
		t.Fatalf("GET accept = %d", getAccept.status)
	}
	wrongOrigin := browserJSON(t, carry.Client(), http.MethodPost, carry.URL+"/v1/spaces/"+spaceID+"/invitations", "https://wrong.example", managerCredential, "wrong-origin", map[string]any{"email": "other@example.com", "can_manage_members": false, "can_enroll_machines": false})
	if wrongOrigin.status != http.StatusForbidden && wrongOrigin.status != http.StatusBadRequest {
		t.Fatalf("wrong Origin = %d", wrongOrigin.status)
	}
	if inbox.cacheControl != "no-store" || accepted.cacheControl != "no-store" {
		t.Fatalf("no-store missing")
	}
}

type browserJSONResponse struct {
	status            int
	body              map[string]any
	cacheControl      string
	sessionCredential string
}

func browserJSON(t *testing.T, client *http.Client, method, requestURL, origin, credential, key string, body any) browserJSONResponse {
	t.Helper()
	var encoded []byte
	if body != nil {
		encoded, _ = json.Marshal(body)
	}
	request, _ := http.NewRequest(method, requestURL, bytes.NewReader(encoded))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if credential != "" {
		request.AddCookie(&http.Cookie{Name: "__Host-carry_session", Value: credential, Expires: time.Now().Add(time.Hour)})
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", requestURL, err)
	}
	defer response.Body.Close()
	decoded := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	replacement := ""
	if cookie := responseCookie(response, "__Host-carry_session"); cookie != nil {
		replacement = cookie.Value
	}
	return browserJSONResponse{status: response.StatusCode, body: decoded, cacheControl: response.Header.Get("Cache-Control"), sessionCredential: replacement}
}

type capturingEmailSubmitter struct{ code string }

func (submitter *capturingEmailSubmitter) PayloadDigest(message identity.EmailCodeMessage) ([32]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.Code + "\x00" + message.IdempotencyKey)), nil
}
func (submitter *capturingEmailSubmitter) SubmitEmailCode(_ context.Context, message identity.EmailCodeMessage, _ [32]byte) identity.EmailSubmission {
	submitter.code = message.Code
	return identity.EmailSubmission{State: identity.EmailSubmissionAccepted, ProviderMessageID: "email-code-fixture"}
}
