package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/ApexReasoning/carry/internal/work"
)

// maxCommandBytes covers the worst-case JSON escaping of the largest valid native update.
// Domain byte limits remain authoritative after decoding.
const maxCommandBytes = 512 << 10

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxCommandBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeAPIError(response, http.StatusBadRequest, "Carry could not read this request.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(response, http.StatusBadRequest, "Carry could not read this request.")
		return false
	}
	return true
}

type userFailureRecovery uint8

const (
	userReadFailure userFailureRecovery = iota
	userMutationFailure
)

func writeUserStoreError(response http.ResponseWriter, err error, recovery userFailureRecovery, operation string) {
	switch {
	case errors.Is(err, space.ErrForbidden):
		writeAPIError(response, http.StatusForbidden, "You do not have access to this Space.")
	case errors.Is(err, work.ErrNotFound):
		writeAPIError(response, http.StatusNotFound, "This Work is unavailable.")
	case errors.Is(err, work.ErrIdempotencyConflict), errors.Is(err, conversation.ErrIdempotencyConflict):
		writeAPIError(response, http.StatusConflict, "This action no longer matches the saved request. Reload before trying again.")
	case errors.Is(err, conversation.ErrReplyPending):
		writeAPIError(response, http.StatusConflict, "Wait for Carry's reply before sending another message.")
	case errors.Is(err, conversation.ErrReplyConflict):
		writeAPIError(response, http.StatusConflict, "Reload the Conversation before trying again.")
	case errors.Is(err, work.ErrNotOpen), errors.Is(err, work.ErrRetryNotNeeded), errors.Is(err, work.ErrReviewNotCurrent):
		writeAPIError(response, http.StatusConflict, "Reload this Work before choosing again.")
	case errors.Is(err, work.ErrInvalidGoal), errors.Is(err, work.ErrInvalidMessage), errors.Is(err, conversation.ErrInvalidText):
		writeAPIError(response, http.StatusBadRequest, "Check the entered value and try again.")
	case errors.Is(err, work.ErrInvalidIdempotency), errors.Is(err, work.ErrInvalidCursor),
		errors.Is(err, conversation.ErrInvalidIdempotency), errors.Is(err, conversation.ErrInvalidCursor),
		errors.Is(err, conversation.ErrInvalidContext):
		writeAPIError(response, http.StatusBadRequest, "Reload before trying again.")
	default:
		writeUserInternalError(response, recovery, operation, err)
	}
}

func writeMachineStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, space.ErrForbidden), errors.Is(err, machine.ErrMachineRevoked):
		writeAPIError(response, http.StatusForbidden, err.Error())
	case errors.Is(err, machine.ErrMachineNotFound), errors.Is(err, work.ErrNotFound):
		writeAPIError(response, http.StatusNotFound, err.Error())
	case errors.Is(err, run.ErrStaleAttempt):
		writeAPIError(response, http.StatusConflict, "Run Attempt is stale or expired")
	case errors.Is(err, conversation.ErrStaleReplyClaim):
		writeAPIError(response, http.StatusConflict, "private Conversation reply claim is stale or expired")
	case errors.Is(err, work.ErrIdempotencyConflict), errors.Is(err, conversation.ErrIdempotencyConflict), errors.Is(err, conversation.ErrReplyPending),
		errors.Is(err, conversation.ErrReplyConflict), errors.Is(err, work.ErrNotOpen), errors.Is(err, work.ErrRetryNotNeeded),
		errors.Is(err, work.ErrReviewNotCurrent):
		writeAPIError(response, http.StatusConflict, err.Error())
	case errors.Is(err, run.ErrInvalidUpdate), errors.Is(err, run.ErrInvalidOutcome), errors.Is(err, work.ErrInvalidGoal),
		errors.Is(err, work.ErrInvalidMessage), errors.Is(err, work.ErrInvalidIdempotency), errors.Is(err, work.ErrInvalidCursor),
		errors.Is(err, conversation.ErrInvalidText), errors.Is(err, conversation.ErrInvalidIdempotency),
		errors.Is(err, conversation.ErrInvalidCursor), errors.Is(err, conversation.ErrInvalidContext):
		writeAPIError(response, http.StatusBadRequest, err.Error())
	default:
		writeAPIError(response, http.StatusInternalServerError, "request failed")
	}
}

func writeUserInternalError(response http.ResponseWriter, recovery userFailureRecovery, operation string, err error) {
	slog.Error("user request failed", "operation", operation, "error", err)
	if recovery == userMutationFailure {
		writeAPIError(response, http.StatusInternalServerError, "Carry could not confirm whether this change finished. Check the current page before trying again.")
		return
	}
	writeAPIError(response, http.StatusInternalServerError, "Carry could not load this right now. Reload to try again.")
}

func writeAPIError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		slog.Error("encode committed HTTP response", "error", err)
	}
}
