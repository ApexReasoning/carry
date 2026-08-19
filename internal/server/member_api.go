package server

import (
	"context"
	"net/http"

	"github.com/ApexReasoning/carry/internal/space"
)

// MembershipReader lists the current Spaces visible to one member.
type MembershipReader interface {
	ListMemberships(context.Context, string) ([]space.Membership, error)
}

type memberAPI struct {
	memberships MembershipReader
}

func (api memberAPI) me(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	memberships, err := api.memberships.ListMemberships(request.Context(), user.UserID)
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "load memberships")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, struct {
		UserID string             `json:"user_id"`
		Spaces []space.Membership `json:"spaces"`
	}{UserID: user.UserID, Spaces: memberships})
}
