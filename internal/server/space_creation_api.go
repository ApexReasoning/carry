package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/ApexReasoning/carry/internal/space"
)

// SpaceCreation commits a Space-owned creation command.
type SpaceCreation interface {
	CreateSpace(context.Context, space.CreateSpaceCommand) (space.CreatedSpace, error)
}

type spaceCreationAPI struct {
	creation SpaceCreation
}

func (api spaceCreationAPI) create(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body struct {
		Name   string `json:"name"`
		Suffix int    `json:"suffix"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	command, err := space.NewCreateSpaceCommand(space.CreateSpaceRequest{
		UserID:         user.UserID,
		Name:           body.Name,
		Suffix:         body.Suffix,
		IdempotencyKey: idempotencyKey,
	})
	var created space.CreatedSpace
	if err == nil {
		created, err = api.creation.CreateSpace(request.Context(), command)
	}
	switch {
	case errors.Is(err, space.ErrSpaceNameRequired),
		errors.Is(err, space.ErrSpaceNameTooLong),
		errors.Is(err, space.ErrSpaceNameHasControl),
		errors.Is(err, space.ErrSpaceSlugUnsupported),
		errors.Is(err, space.ErrSpaceSlugMixedScripts),
		errors.Is(err, space.ErrSpaceSlugTooLong),
		errors.Is(err, space.ErrSpaceSlugUnstable),
		errors.Is(err, space.ErrSpaceSlugSuffixInvalid):
		writeAPIError(response, http.StatusBadRequest, err.Error())
		return
	}
	var conflict *space.SlugConflictError
	if errors.As(err, &conflict) {
		writeJSON(response, http.StatusConflict, struct {
			Error           string `json:"error"`
			Slug            string `json:"slug"`
			SuggestedSlug   string `json:"suggested_slug,omitempty"`
			SuggestedSuffix int    `json:"suggested_suffix,omitempty"`
		}{
			Error:           conflict.Error(),
			Slug:            conflict.Slug,
			SuggestedSlug:   conflict.SuggestedSlug,
			SuggestedSuffix: conflict.SuggestedSuffix,
		})
		return
	}
	if errors.Is(err, space.ErrIdempotencyConflict) {
		writeAPIError(response, http.StatusConflict, "This action no longer matches the saved request. Reload before trying again.")
		return
	}
	if errors.Is(err, space.ErrForbidden) {
		writeAPIError(response, http.StatusForbidden, "You do not have access to this Space.")
		return
	}
	if err != nil {
		writeUserInternalError(response, userMutationFailure, "create Space", err)
		return
	}
	membership := membershipWire{
		SpaceID:           created.SpaceID,
		Name:              created.Name,
		Slug:              created.Slug,
		CanManageMembers:  created.CanManageMembers,
		CanEnrollMachines: created.CanEnrollMachines,
	}
	writeJSON(response, http.StatusCreated, membership)
}
