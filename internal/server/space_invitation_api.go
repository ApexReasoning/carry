package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/go-chi/chi/v5"
)

// SpaceInvitations is the Space-owned member admission behavior consumed by HTTP.
type SpaceInvitations interface {
	Issue(context.Context, space.IssueInvitationRequest) (space.IssuedInvitation, error)
	Resend(context.Context, space.ResendInvitationRequest) (space.IssuedInvitation, error)
	ListMembers(context.Context, string, string) ([]space.SpaceMember, error)
	ListForSpace(context.Context, string, string) ([]space.ManagedInvitation, error)
	ListForUser(context.Context, string, string) (space.InvitationInbox, error)
	Revoke(context.Context, space.RevokeInvitationCommand) error
	Accept(context.Context, space.AcceptInvitationCommand) (space.AcceptedInvitation, error)
}

type spaceInvitationAPI struct {
	invitations SpaceInvitations
	credentials identity.Credentials
	origin      ExternalOrigin
}

func (api spaceInvitationAPI) listMembers(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	members, err := api.invitations.ListMembers(request.Context(), user.UserID, chi.URLParam(request, "space_id"))
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	wire := make([]spaceMemberWire, len(members))
	for index, member := range members {
		wire[index] = spaceMemberWire{UserID: member.UserID, DisplayName: member.DisplayName, CanManageMembers: member.CanManageMembers, CanEnrollMachines: member.CanEnrollMachines, JoinedAt: member.JoinedAt}
	}
	writeJSON(response, http.StatusOK, struct {
		Members []spaceMemberWire `json:"members"`
	}{wire})
}

func (api spaceInvitationAPI) listManaged(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	items, err := api.invitations.ListForSpace(request.Context(), user.UserID, chi.URLParam(request, "space_id"))
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	wire := make([]managedInvitationWire, len(items))
	for index, item := range items {
		wire[index] = managedInvitation(item)
	}
	writeJSON(response, http.StatusOK, struct {
		Invitations []managedInvitationWire `json:"invitations"`
	}{wire})
}

func (api spaceInvitationAPI) issue(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusBadRequest, "request origin is invalid")
		return
	}
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body struct {
		Email             string `json:"email"`
		CanManageMembers  bool   `json:"can_manage_members"`
		CanEnrollMachines bool   `json:"can_enroll_machines"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	issued, err := api.invitations.Issue(request.Context(), space.IssueInvitationRequest{
		SpaceID: chi.URLParam(request, "space_id"), ActorUserID: user.UserID, RecipientEmail: body.Email,
		CanManageMembers: body.CanManageMembers, CanEnrollMachines: body.CanEnrollMachines, IdempotencyKey: key,
	})
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, managedInvitation(issued))
}

func (api spaceInvitationAPI) resend(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusBadRequest, "request origin is invalid")
		return
	}
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	issued, err := api.invitations.Resend(request.Context(), space.ResendInvitationRequest{
		SpaceID: chi.URLParam(request, "space_id"), InvitationID: chi.URLParam(request, "invitation_id"),
		ActorUserID: user.UserID, IdempotencyKey: key,
	})
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, managedInvitation(issued))
}

func (api spaceInvitationAPI) revoke(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusBadRequest, "request origin is invalid")
		return
	}
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	err := api.invitations.Revoke(request.Context(), space.RevokeInvitationCommand{
		SpaceID: chi.URLParam(request, "space_id"), InvitationID: chi.URLParam(request, "invitation_id"),
		ActorUserID: user.UserID, IdempotencyKey: key,
	})
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api spaceInvitationAPI) inbox(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	inbox, err := api.invitations.ListForUser(request.Context(), user.UserID, sessionID)
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	items := make([]recipientInvitationWire, len(inbox.Invitations))
	for index, item := range inbox.Invitations {
		items[index] = recipientInvitationWire{InvitationID: item.InvitationID, SpaceID: item.SpaceID, SpaceName: item.SpaceName, InviterDisplayName: item.InviterDisplayName, CanManageMembers: item.CanManageMembers, CanEnrollMachines: item.CanEnrollMachines, CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt}
	}
	writeJSON(response, http.StatusOK, struct {
		Invitations              []recipientInvitationWire `json:"invitations"`
		ReauthenticationRequired bool                      `json:"reauthentication_required"`
	}{items, inbox.ReauthenticationRequired})
}

func (api spaceInvitationAPI) accept(response http.ResponseWriter, request *http.Request) {
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusBadRequest, "request origin is invalid")
		return
	}
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	sessionID, ok := api.browserSessionID(response, request)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	accepted, err := api.invitations.Accept(request.Context(), space.AcceptInvitationCommand{
		InvitationID: chi.URLParam(request, "invitation_id"), UserID: user.UserID, SessionID: sessionID,
		DisplayName: body.DisplayName, IdempotencyKey: key,
	})
	if err != nil {
		writeInvitationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, acceptedInvitationWire{InvitationID: accepted.InvitationID, SpaceID: accepted.SpaceID, SpaceName: accepted.SpaceName, CanManageMembers: accepted.CanManageMembers, CanEnrollMachines: accepted.CanEnrollMachines, AlreadyMember: accepted.AlreadyMember})
}

func (api spaceInvitationAPI) browserSessionID(response http.ResponseWriter, request *http.Request) (string, bool) {
	if strings.TrimSpace(request.Header.Get("Authorization")) != "" {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is required")
		return "", false
	}
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is required")
		return "", false
	}
	sessionID, ok := api.credentials.ParseBrowserSessionCredential(cookie.Value)
	if !ok {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is invalid")
		return "", false
	}
	return sessionID, true
}

type spaceMemberWire struct {
	UserID            string    `json:"user_id"`
	DisplayName       string    `json:"display_name"`
	CanManageMembers  bool      `json:"can_manage_members"`
	CanEnrollMachines bool      `json:"can_enroll_machines"`
	JoinedAt          time.Time `json:"joined_at"`
}
type invitationSubmissionWire struct {
	State string `json:"state"`
}
type managedInvitationWire struct {
	InvitationID      string                   `json:"invitation_id"`
	SpaceID           string                   `json:"space_id"`
	RecipientEmail    string                   `json:"recipient_email"`
	CanManageMembers  bool                     `json:"can_manage_members"`
	CanEnrollMachines bool                     `json:"can_enroll_machines"`
	CreatedAt         time.Time                `json:"created_at"`
	ExpiresAt         time.Time                `json:"expires_at"`
	Submission        invitationSubmissionWire `json:"submission"`
}
type recipientInvitationWire struct {
	InvitationID       string    `json:"invitation_id"`
	SpaceID            string    `json:"space_id"`
	SpaceName          string    `json:"space_name"`
	InviterDisplayName string    `json:"inviter_display_name"`
	CanManageMembers   bool      `json:"can_manage_members"`
	CanEnrollMachines  bool      `json:"can_enroll_machines"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}
type acceptedInvitationWire struct {
	InvitationID      string `json:"invitation_id"`
	SpaceID           string `json:"space_id"`
	SpaceName         string `json:"space_name"`
	CanManageMembers  bool   `json:"can_manage_members"`
	CanEnrollMachines bool   `json:"can_enroll_machines"`
	AlreadyMember     bool   `json:"already_member"`
}

func managedInvitation(item space.IssuedInvitation) managedInvitationWire {
	return managedInvitationWire{InvitationID: item.InvitationID, SpaceID: item.SpaceID, RecipientEmail: item.RecipientEmail, CanManageMembers: item.CanManageMembers, CanEnrollMachines: item.CanEnrollMachines, CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt, Submission: invitationSubmissionWire{State: string(item.Submission.State)}}
}

func writeInvitationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthenticated):
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is invalid")
	case errors.Is(err, space.ErrForbidden):
		writeAPIError(response, http.StatusForbidden, "member lacks permission")
	case errors.Is(err, space.ErrInvalidInvitation):
		writeAPIError(response, http.StatusBadRequest, "invitation request is invalid")
	case errors.Is(err, space.ErrInvitationProofRequired):
		writeAPIError(response, http.StatusPreconditionRequired, err.Error())
	case errors.Is(err, space.ErrInvitationResendCooldown):
		writeAPIError(response, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, space.ErrInvitationUnavailable):
		writeAPIError(response, http.StatusNotFound, "invitation is unavailable")
	case errors.Is(err, space.ErrInvitationConflict), errors.Is(err, space.ErrInvitationAlreadyMember), errors.Is(err, space.ErrIdempotencyConflict), errors.Is(err, space.ErrInvitationSubmissionConflict):
		writeAPIError(response, http.StatusConflict, "invitation cannot be changed")
	default:
		writeAPIError(response, http.StatusInternalServerError, "manage Space invitation")
	}
}
