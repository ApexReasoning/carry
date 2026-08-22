package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
)

func TestSpaceCreationErrorsUseUserRecoveryLanguage(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		err     error
		status  int
		message string
	}{
		"saved request conflict": {
			space.ErrIdempotencyConflict,
			http.StatusConflict,
			"This action no longer matches the saved request. Reload before trying again.",
		},
		"unexpected mutation": {
			errors.New("database unavailable"),
			http.StatusInternalServerError,
			"Carry could not confirm whether this change finished. Check the current page before trying again.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			api := spaceCreationAPI{creation: failingSpaceCreation{err: test.err}}
			request := httptest.NewRequest(http.MethodPost, "/v1/spaces", strings.NewReader(`{"name":"Research","suffix":0}`))
			request.Header.Set("Idempotency-Key", "create-research")
			request = request.WithContext(context.WithValue(request.Context(), userContextKey{}, identity.AuthenticatedUser{
				UserID: "10000000-0000-4000-8000-000000000001",
			}))
			response := httptest.NewRecorder()

			api.create(response, request)

			assertUserFacingResponse(t, response, test.status, test.message)
		})
	}
}

type failingSpaceCreation struct {
	err error
}

func (creation failingSpaceCreation) CreateSpace(context.Context, space.CreateSpaceCommand) (space.CreatedSpace, error) {
	return space.CreatedSpace{}, creation.err
}
