package server

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
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
	routes, err := NewUserSpaceRoutesWithInvitations(spaceCreationStub{}, behavior, behavior, behavior, credentials, origin)
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

	unauthenticated := httptest.NewRequest(http.MethodGet, "https://carry.example/v1/invitations/40000000-0000-4000-8000-000000000001", nil)
	unauthenticated.Host = "carry.example"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthenticated)
	if response.Code != http.StatusUnauthorized || behavior.loads != 0 {
		t.Fatalf("pre-auth preview = %d, loads = %d", response.Code, behavior.loads)
	}

	missingOrigin := request(http.MethodPost, "/v1/spaces/20000000-0000-0000-0000-000000000001/invitations")
	missingOrigin.Header.Set("Idempotency-Key", "issue")
	response = httptest.NewRecorder()
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
	bearer.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
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

	behavior.loadError = space.ErrInvitationUnavailable
	unavailable := request(http.MethodGet, "/v1/invitations/40000000-0000-4000-8000-000000000001")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unavailable)
	if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"Space invitation is unavailable\"}\n" {
		t.Fatalf("uniform unavailable = %d %q", response.Code, response.Body.String())
	}
	behavior.loadError = nil
	behavior.loaded = space.RecipientInvitation{
		InvitationID:       "40000000-0000-4000-8000-000000000001",
		SpaceID:            "20000000-0000-0000-0000-000000000001",
		SpaceName:          "Research",
		InviterDisplayName: "Manager",
		State:              space.InvitationPending,
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unavailable)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"pending"`) {
		t.Fatalf("owner projection = %d %s", response.Code, response.Body.String())
	}
}

func TestInvitationTerminalErrorsHaveExactRecoveryMappings(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		err     error
		status  int
		message string
	}{
		"expired": {
			err:     space.ErrInvitationExpired,
			status:  http.StatusGone,
			message: "Space invitation has expired",
		},
		"revoked": {
			err:     space.ErrInvitationRevoked,
			status:  http.StatusGone,
			message: "Space invitation was revoked",
		},
		"accepted": {
			err:     space.ErrInvitationAccepted,
			status:  http.StatusConflict,
			message: "Space invitation was already accepted",
		},
		"unavailable": {
			err:     space.ErrInvitationUnavailable,
			status:  http.StatusNotFound,
			message: "Space invitation is unavailable",
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeInvitationError(response, test.err)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if response.Body.String() != "{\"error\":\""+test.message+"\"}\n" {
				t.Fatalf("body = %q", response.Body.String())
			}
		})
	}
}

func TestRemoveMemberRouteRequiresOriginAndMapsRemovalOutcomes(t *testing.T) {
	credentials, err := identity.NewCredentials(make([]byte, identity.IdentityRootBytes))
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	origin, err := ParseExternalOrigin("https://carry.example")
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	behavior := &invitationBehaviorStub{}
	routes, err := NewUserSpaceRoutesWithInvitations(spaceCreationStub{}, behavior, behavior, behavior, credentials, origin)
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
	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "https://carry.example/v1/spaces/20000000-0000-0000-0000-000000000001/members/40000000-0000-0000-0000-000000000001/remove", strings.NewReader(`{"open_work_new_owner_user_id":"50000000-0000-0000-0000-000000000001"}`))
		req.Host = "carry.example"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "remove-member")
		req.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: credential})
		return req
	}

	missingOrigin := request()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missingOrigin)
	if response.Code != http.StatusBadRequest || behavior.removals != 0 {
		t.Fatalf("missing Origin = %d, removals = %d", response.Code, behavior.removals)
	}

	success := request()
	success.Header.Set("Origin", "https://carry.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, success)
	if response.Code != http.StatusNoContent || behavior.removals != 1 || behavior.removalCommand.TargetUserID != "40000000-0000-0000-0000-000000000001" || behavior.removalCommand.SuccessorUserID != "50000000-0000-0000-0000-000000000001" {
		t.Fatalf("success = %d, command = %#v", response.Code, behavior.removalCommand)
	}

	for name, test := range map[string]struct {
		err    error
		status int
	}{
		"forbidden": {space.ErrForbidden, http.StatusForbidden},
		"invalid":   {space.ErrInvalidMemberRemoval, http.StatusBadRequest},
		"missing":   {space.ErrMemberUnavailable, http.StatusNotFound},
		"conflict":  {space.ErrLastMemberManager, http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			behavior.removalError = test.err
			req := request()
			req.Header.Set("Origin", "https://carry.example")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

type invitationBrowserSessions struct{}

func (invitationBrowserSessions) AuthenticateBrowserSession(context.Context, string) (identity.AuthenticatedUser, error) {
	return identity.AuthenticatedUser{UserID: "30000000-0000-0000-0000-000000000001", DisplayName: "Member"}, nil
}
func (invitationBrowserSessions) RevokeBrowserSession(context.Context, string) error { return nil }

type spaceCreationStub struct{}

func (spaceCreationStub) Create(context.Context, space.CreateSpaceRequest) (space.CreatedSpace, error) {
	return space.CreatedSpace{}, nil
}

type invitationBehaviorStub struct {
	issues         int
	removals       int
	removalCommand space.RemoveMemberCommand
	removalError   error
	loaded         space.RecipientInvitation
	loadError      error
	loads          int
}

func (stub *invitationBehaviorStub) Issue(context.Context, space.IssueInvitationRequest) (space.IssuedInvitation, error) {
	stub.issues++
	return space.IssuedInvitation{}, nil
}
func (*invitationBehaviorStub) Resend(context.Context, space.ResendInvitationRequest) (space.IssuedInvitation, error) {
	return space.IssuedInvitation{}, nil
}
func (*invitationBehaviorStub) ListSpaceMembers(context.Context, space.ListMembersCommand) (space.MemberPage, error) {
	return space.MemberPage{Members: []space.SpaceMember{{UserID: "30000000-0000-0000-0000-000000000001", DisplayName: "Member", JoinedAt: time.Now()}}}, nil
}
func (stub *invitationBehaviorStub) RemoveSpaceMember(_ context.Context, command space.RemoveMemberCommand) error {
	stub.removals++
	stub.removalCommand = command
	return stub.removalError
}
func (*invitationBehaviorStub) ListSpaceInvitations(context.Context, string, string) ([]space.ManagedInvitation, error) {
	return nil, nil
}
func (*invitationBehaviorStub) ListUserInvitations(context.Context, string, string) (space.InvitationInbox, error) {
	return space.InvitationInbox{}, nil
}
func (stub *invitationBehaviorStub) LoadInvitationForUser(context.Context, string, string, string) (space.RecipientInvitation, error) {
	stub.loads++
	return stub.loaded, stub.loadError
}
func (*invitationBehaviorStub) Revoke(context.Context, space.RevokeInvitationCommand) error {
	return nil
}
func (*invitationBehaviorStub) Accept(context.Context, space.AcceptInvitationCommand) (space.AcceptedInvitation, error) {
	return space.AcceptedInvitation{}, nil
}
