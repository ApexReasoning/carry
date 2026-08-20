package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/ApexReasoning/carry/internal/space"
)

// FirstSpace is the Space behavior consumed by HTTP.
type FirstSpace interface {
	Create(context.Context, space.CreateFirstRequest) (space.CreatedSpace, error)
}

type spaceCreationAPI struct {
	creator FirstSpace
}

func (api spaceCreationAPI) createFirst(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	created, err := api.creator.Create(request.Context(), space.CreateFirstRequest{
		UserID: user.UserID, DisplayName: body.DisplayName, SpaceName: body.Name,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, space.ErrInvalidSpaceCreation) {
		writeAPIError(response, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, space.ErrAlreadyHasSpace) || errors.Is(err, space.ErrIdempotencyConflict) {
		writeAPIError(response, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, space.ErrForbidden) {
		writeAPIError(response, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "create first Space")
		return
	}
	writeJSON(response, http.StatusCreated, membershipWire{
		SpaceID: created.SpaceID, Name: created.Name,
		CanManageMembers: created.CanManageMembers, CanEnrollMachines: created.CanEnrollMachines,
	})
}
