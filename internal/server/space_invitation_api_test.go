package server

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/go-chi/chi/v5"
)

func TestInvitationRoutesRequireBrowserOriginAndNeverMutateOnGET(t *testing.T) {
	credentials, err := identity.NewCredentials(make([]byte, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	origin, err := ParseExternalOrigin("https://carry.example")
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	behavior := &invitationBehaviorStub{}
	routes, err := NewUserSpaceRoutesWithInvitations(firstSpaceStub{}, behavior, credentials, origin)
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	auth := userAuthenticator{sessions: invitationBrowserSessions{}, credentials: credentials}
	router := chi.NewRouter()
	router.Route("/v1", func(version chi.Router) {
		version.Use(rejectMachinePrincipal)
		version.Group(func(browser chi.Router) { browser.Use(auth.requireBrowserUser); routes.mount(browser) })
	})
	handler := noStoreV1(http.NewCrossOriginProtection().Handler(router))
	credential, _ := credentials.BrowserSessionCredential("10000000-0000-0000-0000-000000000001")
	request := func(method, path string) *http.Request {
		req := httptest.NewRequest(method, "https://carry.example"+path, nil)
		req.Host = "carry.example"
		req.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
		return req
	}

	missingOrigin := request(http.MethodPost, "/v1/spaces/20000000-0000-0000-0000-000000000001/invitations")
	missingOrigin.Header.Set("Idempotency-Key", "issue")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missingOrigin)
	if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" || behavior.issues != 0 {
		t.Fatalf("missing Origin = %d, issues = %d", response.Code, behavior.issues)
	}

	get := request(http.MethodGet, "/v1/spaces/20000000-0000-0000-0000-000000000001/members")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, get)
	if response.Code != http.StatusOK || behavior.issues != 0 {
		t.Fatalf("GET = %d, issues = %d", response.Code, behavior.issues)
	}

	bearer := request(http.MethodGet, "/v1/invitations")
	bearer.Header.Set("Authorization", "Bearer transitional")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, bearer)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("bearer status = %d", response.Code)
	}

	machine := request(http.MethodGet, "/v1/invitations")
	machine.TLS.PeerCertificates = []*x509.Certificate{{}}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, machine)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("Machine status = %d", response.Code)
	}
}

type invitationBrowserSessions struct{}

func (invitationBrowserSessions) AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error) {
	return identity.AuthenticatedUser{UserID: "30000000-0000-0000-0000-000000000001", DisplayName: "Member"}, nil
}
func (invitationBrowserSessions) RevokeBrowserSession(context.Context, string) error { return nil }

type firstSpaceStub struct{}

func (firstSpaceStub) Create(context.Context, space.CreateFirstRequest) (space.CreatedSpace, error) {
	return space.CreatedSpace{}, nil
}

type invitationBehaviorStub struct{ issues int }

func (stub *invitationBehaviorStub) Issue(context.Context, space.IssueInvitationRequest) (space.IssuedInvitation, error) {
	stub.issues++
	return space.IssuedInvitation{}, nil
}
func (*invitationBehaviorStub) Resend(context.Context, space.ResendInvitationRequest) (space.IssuedInvitation, error) {
	return space.IssuedInvitation{}, nil
}
func (*invitationBehaviorStub) ListMembers(context.Context, string, string) ([]space.SpaceMember, error) {
	return []space.SpaceMember{{UserID: "30000000-0000-0000-0000-000000000001", DisplayName: "Member", JoinedAt: time.Now()}}, nil
}
func (*invitationBehaviorStub) ListForSpace(context.Context, string, string) ([]space.ManagedInvitation, error) {
	return nil, nil
}
func (*invitationBehaviorStub) ListForUser(context.Context, string, string) (space.InvitationInbox, error) {
	return space.InvitationInbox{}, nil
}
func (*invitationBehaviorStub) Revoke(context.Context, space.RevokeInvitationCommand) error {
	return nil
}
func (*invitationBehaviorStub) Accept(context.Context, space.AcceptInvitationCommand) (space.AcceptedInvitation, error) {
	return space.AcceptedInvitation{}, nil
}
