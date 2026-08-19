package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/go-chi/chi/v5"
)

// MachineRunStore owns the exact transactions used by one mTLS-authenticated Host.
type MachineRunStore interface {
	ClaimRun(context.Context, string) (run.Claim, error)
	RenewRunAttempt(context.Context, string, string, string, int64) (time.Time, error)
	CommitWorkUnderstanding(context.Context, run.CommitCommand) error
	FinishUnresolvedAttempt(context.Context, run.FinishCommand) error
}

type machineAPI struct {
	store MachineRunStore
}

type machineContextKey struct{}

type renewRunAttemptRequest struct {
	Fence int64 `json:"fence"`
}

type commitUnderstandingRequest struct {
	Fence                    int64  `json:"fence"`
	BaseUnderstandingVersion int64  `json:"base_understanding_version"`
	InputEndSeq              int64  `json:"input_end_seq"`
	Understanding            string `json:"understanding"`
	NextStep                 string `json:"next_step"`
}

type finishAttemptRequest struct {
	Fence   int64     `json:"fence"`
	Outcome run.State `json:"outcome"`
}

type runMessageWire struct {
	AuthorUserID string `json:"author_user_id"`
	Text         string `json:"text"`
}

type runClaimWire struct {
	RunID                    string           `json:"run_id"`
	AttemptID                string           `json:"attempt_id"`
	WorkID                   string           `json:"work_id"`
	Fence                    int64            `json:"fence"`
	LeaseExpiresAt           time.Time        `json:"lease_expires_at"`
	Goal                     string           `json:"goal"`
	CurrentUnderstanding     string           `json:"current_understanding"`
	CurrentNextStep          string           `json:"current_next_step"`
	BaseUnderstandingVersion int64            `json:"base_understanding_version"`
	InputEndSeq              int64            `json:"input_end_seq"`
	Messages                 []runMessageWire `json:"messages"`
}

func requireMachine(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// Member authentication is never accepted as additional or fallback
		// authority on the Machine surface.
		_, browserCookieErr := request.Cookie(browserSessionCookie)
		if strings.TrimSpace(request.Header.Get("Authorization")) != "" || browserCookieErr == nil {
			writeAPIError(response, http.StatusUnauthorized, "Machine route does not accept member authentication")
			return
		}
		if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.PeerCertificates) == 0 {
			writeAPIError(response, http.StatusUnauthorized, "Machine certificate is required")
			return
		}
		machineID, err := host.MachineIDFromCertificate(request.TLS.PeerCertificates[0])
		if err != nil {
			writeAPIError(response, http.StatusUnauthorized, "Machine certificate is invalid")
			return
		}
		ctx := context.WithValue(request.Context(), machineContextKey{}, machineID)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func currentMachine(response http.ResponseWriter, request *http.Request) (string, bool) {
	machineID, ok := request.Context().Value(machineContextKey{}).(string)
	if !ok || machineID == "" {
		writeAPIError(response, http.StatusInternalServerError, "Machine authentication context is missing")
		return "", false
	}
	return machineID, true
}

func (api machineAPI) claimRun(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	claim, err := api.store.ClaimRun(request.Context(), machineID)
	if errors.Is(err, run.ErrNoRunAvailable) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	messages := make([]runMessageWire, 0, len(claim.Messages))
	for _, message := range claim.Messages {
		messages = append(messages, runMessageWire{AuthorUserID: message.AuthorUserID, Text: message.Text})
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, runClaimWire{
		RunID: claim.RunID, AttemptID: claim.AttemptID, WorkID: claim.WorkID,
		Fence: claim.Fence, LeaseExpiresAt: claim.LeaseExpiresAt,
		Goal: claim.Goal, CurrentUnderstanding: claim.CurrentUnderstanding,
		CurrentNextStep:          claim.CurrentNextStep,
		BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
		InputEndSeq:              claim.InputEndSeq, Messages: messages,
	})
}

func (api machineAPI) renewRun(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	var body renewRunAttemptRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	leaseExpiresAt, err := api.store.RenewRunAttempt(
		request.Context(), machineID,
		chi.URLParam(request, "run_id"), chi.URLParam(request, "attempt_id"), body.Fence,
	)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}{LeaseExpiresAt: leaseExpiresAt})
}

func (api machineAPI) commitUnderstanding(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	var body commitUnderstandingRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := api.store.CommitWorkUnderstanding(request.Context(), run.CommitCommand{
		MachineID: machineID, RunID: chi.URLParam(request, "run_id"),
		AttemptID: chi.URLParam(request, "attempt_id"), Fence: body.Fence,
		BaseUnderstandingVersion: body.BaseUnderstandingVersion,
		InputEndSeq:              body.InputEndSeq, Understanding: body.Understanding, NextStep: body.NextStep,
	}); err != nil {
		writeStoreError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api machineAPI) finishAttempt(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	var body finishAttemptRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := api.store.FinishUnresolvedAttempt(request.Context(), run.FinishCommand{
		MachineID: machineID, RunID: chi.URLParam(request, "run_id"),
		AttemptID: chi.URLParam(request, "attempt_id"), Fence: body.Fence, Outcome: body.Outcome,
	}); err != nil {
		writeStoreError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
