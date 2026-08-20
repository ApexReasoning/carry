package server

import (
	"context"
	"net/http"

	"github.com/ApexReasoning/carry/internal/space"
)

// MembershipReader lists the current Spaces visible to one User.
type MembershipReader interface {
	ListMemberships(context.Context, string) ([]space.Membership, error)
}

type userAPI struct {
	memberships MembershipReader
}

type membershipWire struct {
	SpaceID           string `json:"space_id"`
	Name              string `json:"name"`
	CanManageMembers  bool   `json:"can_manage_members"`
	CanEnrollMachines bool   `json:"can_enroll_machines"`
}

func (api userAPI) me(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
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
			CanManageMembers:  membership.CanManageMembers,
			CanEnrollMachines: membership.CanEnrollMachines,
		})
	}
	var displayName *string
	if user.DisplayName != "" {
		displayName = &user.DisplayName
	}
	writeJSON(response, http.StatusOK, struct {
		UserID      string           `json:"user_id"`
		DisplayName *string          `json:"display_name"`
		Spaces      []membershipWire `json:"spaces"`
	}{UserID: user.UserID, DisplayName: displayName, Spaces: spaces})
}
