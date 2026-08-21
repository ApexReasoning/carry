package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/ApexReasoning/carry/internal/space"
)

// SpaceCreator is the Space creation behavior consumed by HTTP.
type SpaceCreator interface {
	Create(context.Context, space.CreateSpaceRequest) (space.CreatedSpace, error)
}

type spaceCreationAPI struct {
	creator SpaceCreator
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
	created, err := api.creator.Create(request.Context(), space.CreateSpaceRequest{
		UserID:         user.UserID,
		Name:           body.Name,
		Suffix:         body.Suffix,
		IdempotencyKey: idempotencyKey,
	})
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
		writeAPIError(response, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, space.ErrForbidden) {
		writeAPIError(response, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "create Space")
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
