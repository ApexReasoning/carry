package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/ApexReasoning/carry/internal/run"
	"github.com/go-chi/chi/v5"
)

// AgentRunStore exposes only the context and terminal mutations authorized by
// one short-lived Attempt credential.
type AgentRunStore interface {
	LoadAttemptContext(context.Context, string, string, int64, string) (run.Context, error)
	CommitWorkUnderstanding(context.Context, run.CommitCommand) error
	FinishUnresolvedAttempt(context.Context, run.FinishCommand) error
}

type agentAPI struct {
	store AgentRunStore
}

type agentContextKey struct{}

type commitRevisionRequest struct {
	Fence         int64  `json:"fence"`
	WriterToken   string `json:"writer_token"`
	BaseRevision  int64  `json:"base_revision"`
	InputEndSeq   int64  `json:"input_end_seq"`
	Understanding string `json:"understanding"`
	NextStep      string `json:"next_step"`
}

type finishOutcomeRequest struct {
	Fence       int64     `json:"fence"`
	WriterToken string    `json:"writer_token"`
	Outcome     run.State `json:"outcome"`
}

type runInputWire struct {
	Sequence     int64         `json:"sequence"`
	Kind         run.InputKind `json:"kind"`
	AuthorUserID string        `json:"author_user_id,omitempty"`
	Text         string        `json:"text"`
}

func requireAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token, hasToken := bearerToken(request)
		_, browserCookieErr := request.Cookie(browserSessionCookie)
		hasMachineCertificate := request.TLS != nil && len(request.TLS.PeerCertificates) != 0
		if !hasToken || browserCookieErr == nil || hasMachineCertificate {
			writeAPIError(response, http.StatusUnauthorized, "Agent credential is required")
			return
		}
		ctx := context.WithValue(request.Context(), agentContextKey{}, token)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func currentAgentCredential(response http.ResponseWriter, request *http.Request) (string, bool) {
	credential, ok := request.Context().Value(agentContextKey{}).(string)
	if !ok || strings.TrimSpace(credential) == "" {
		writeAPIError(response, http.StatusInternalServerError, "Agent authentication context is missing")
		return "", false
	}
	return credential, true
}

func (api agentAPI) loadContext(response http.ResponseWriter, request *http.Request) {
	credential, ok := currentAgentCredential(response, request)
	if !ok {
		return
	}
	fence, err := strconv.ParseInt(request.URL.Query().Get("fence"), 10, 64)
	if err != nil || fence <= 0 {
		writeAPIError(response, http.StatusBadRequest, "fence is invalid")
		return
	}
	value, err := api.store.LoadAttemptContext(
		request.Context(),
		chi.URLParam(request, "run_id"),
		chi.URLParam(request, "attempt_id"),
		fence,
		credential,
	)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	inputs := make([]runInputWire, 0, len(value.Inputs))
	for _, input := range value.Inputs {
		inputs = append(inputs, runInputWire{
			Sequence: input.Sequence, Kind: input.Kind,
			AuthorUserID: input.AuthorUserID, Text: input.Text,
		})
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, struct {
		RunID                string         `json:"run_id"`
		AttemptID            string         `json:"attempt_id"`
		WorkID               string         `json:"work_id"`
		SpaceID              string         `json:"space_id"`
		Goal                 string         `json:"goal"`
		CurrentUnderstanding string         `json:"current_understanding"`
		CurrentNextStep      string         `json:"current_next_step"`
		InputStartSeq        int64          `json:"input_start_seq"`
		InputEndSeq          int64          `json:"input_end_seq"`
		BaseRevision         int64          `json:"base_revision"`
		Fence                int64          `json:"fence"`
		Inputs               []runInputWire `json:"inputs"`
	}{
		RunID: value.RunID, AttemptID: value.AttemptID, WorkID: value.WorkID, SpaceID: value.SpaceID,
		Goal: value.Goal, CurrentUnderstanding: value.CurrentUnderstanding,
		CurrentNextStep: value.CurrentNextStep, InputStartSeq: value.InputStartSeq,
		InputEndSeq: value.InputEndSeq, BaseRevision: value.BaseRevision,
		Fence: value.Fence, Inputs: inputs,
	})
}

func (api agentAPI) commitRevision(response http.ResponseWriter, request *http.Request) {
	credential, ok := currentAgentCredential(response, request)
	if !ok {
		return
	}
	var body commitRevisionRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := api.store.CommitWorkUnderstanding(request.Context(), run.CommitCommand{
		RunID: chi.URLParam(request, "run_id"), AttemptID: chi.URLParam(request, "attempt_id"),
		Fence: body.Fence, WriterToken: body.WriterToken, AgentCredential: credential,
		BaseRevision: body.BaseRevision, InputEndSeq: body.InputEndSeq,
		Understanding: body.Understanding, NextStep: body.NextStep,
	}); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "committed"})
}

func (api agentAPI) finishOutcome(response http.ResponseWriter, request *http.Request) {
	credential, ok := currentAgentCredential(response, request)
	if !ok {
		return
	}
	var body finishOutcomeRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := api.store.FinishUnresolvedAttempt(request.Context(), run.FinishCommand{
		RunID: chi.URLParam(request, "run_id"), AttemptID: chi.URLParam(request, "attempt_id"),
		Fence: body.Fence, WriterToken: body.WriterToken,
		AgentCredential: credential, Outcome: body.Outcome,
	}); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Status run.State `json:"status"`
	}{Status: body.Outcome})
}
