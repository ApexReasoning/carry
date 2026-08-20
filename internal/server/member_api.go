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

type membershipWire struct {
	SpaceID           string `json:"space_id"`
	Name              string `json:"name"`
	CanEnrollMachines bool   `json:"can_enroll_machines"`
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
	spaces := make([]membershipWire, 0, len(memberships))
	for _, membership := range memberships {
		spaces = append(spaces, membershipWire{
			SpaceID: membership.SpaceID, Name: membership.Name,
			CanEnrollMachines: membership.CanEnrollMachines,
		})
	}
	writeJSON(response, http.StatusOK, struct {
		UserID      string           `json:"user_id"`
		DisplayName string           `json:"display_name"`
		Spaces      []membershipWire `json:"spaces"`
	}{UserID: user.UserID, DisplayName: user.DisplayName, Spaces: spaces})
}
