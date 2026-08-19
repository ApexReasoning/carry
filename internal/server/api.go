package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/ApexReasoning/carry/internal/host"
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
		writeAPIError(response, http.StatusBadRequest, "invalid JSON command")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(response, http.StatusBadRequest, "command must contain one JSON value")
		return false
	}
	return true
}

func writeStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, space.ErrForbidden), errors.Is(err, host.ErrMachineRevoked):
		writeAPIError(response, http.StatusForbidden, err.Error())
	case errors.Is(err, host.ErrMachineNotFound), errors.Is(err, work.ErrNotFound):
		writeAPIError(response, http.StatusNotFound, err.Error())
	case errors.Is(err, run.ErrStaleAttempt):
		writeAPIError(response, http.StatusConflict, "Run Attempt is stale or expired")
	case errors.Is(err, host.ErrIdempotencyConflict), errors.Is(err, work.ErrIdempotencyConflict),
		errors.Is(err, work.ErrNotOpen), errors.Is(err, work.ErrRetryNotNeeded):
		writeAPIError(response, http.StatusConflict, err.Error())
	case errors.Is(err, run.ErrInvalidUpdate), errors.Is(err, run.ErrInvalidOutcome), errors.Is(err, work.ErrInvalidGoal),
		errors.Is(err, work.ErrInvalidMessage), errors.Is(err, work.ErrInvalidIdempotency):
		writeAPIError(response, http.StatusBadRequest, err.Error())
	default:
		writeAPIError(response, http.StatusInternalServerError, "request failed")
	}
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
