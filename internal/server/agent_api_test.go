package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ApexReasoning/carry/internal/run"
)

func TestAgentContextUsesOnlyAttemptCredentialAndFixedAuthority(t *testing.T) {
	t.Parallel()

	store := &recordingAgentRuns{attemptContext: run.Context{
		RunID: "run-1", AttemptID: "attempt-1", WorkID: "work-1", SpaceID: "space-1",
		Goal: "Prepare the renewal brief", CurrentUnderstanding: "Finance approved the term.",
		CurrentNextStep: "Apply the legal wording.", InputStartSeq: 3, InputEndSeq: 4,
		BaseRevision: 1, Fence: 7,
		Inputs: []run.Input{{Sequence: 3, Kind: run.InputMessage, AuthorUserID: "member-1", Text: "Legal supplied final wording"}},
	}}
	handler := agentTestAPI(t, store)
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent/runs/run-1/attempts/attempt-1/context?fence=7",
		nil,
	)
	request.Header.Set("Authorization", "Bearer carry_agent_secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if store.runID != "run-1" || store.attemptID != "attempt-1" || store.fence != 7 || store.credential != "carry_agent_secret" {
		t.Fatalf("Agent context authority = %#v", store)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"current_understanding":"Finance approved the term."`)) {
		t.Fatalf("context body = %s", response.Body.String())
	}
}

func TestAgentRevisionTakesCredentialOnlyFromBearer(t *testing.T) {
	t.Parallel()

	store := &recordingAgentRuns{}
	handler := agentTestAPI(t, store)
	body := `{"fence":7,"writer_token":"writer-1","base_revision":1,"input_end_seq":4,"understanding":"The renewal is ready for legal review.","next_step":"Ask the owner to verify the wording."}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/agent/runs/run-1/attempts/attempt-1/revision",
		bytes.NewBufferString(body),
	)
	request.Header.Set("Authorization", "Bearer carry_agent_secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if store.commit.AgentCredential != "carry_agent_secret" || store.commit.WriterToken != "writer-1" ||
		store.commit.RunID != "run-1" || store.commit.AttemptID != "attempt-1" {
		t.Fatalf("revision command = %#v", store.commit)
	}
}

func TestAgentRouteRejectsMixedMachinePrincipal(t *testing.T) {
	t.Parallel()

	_, certificate := testMachineCertificate(t, "machine-19")
	store := &recordingAgentRuns{}
	handler := agentTestAPI(t, store)
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/agent/runs/run-1/attempts/attempt-1/context?fence=1",
		nil,
	)
	request.Header.Set("Authorization", "Bearer carry_agent_secret")
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if store.credential != "" {
		t.Fatal("mixed Machine principal reached Agent store")
	}
}

func agentTestAPI(t *testing.T, store AgentRunStore) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	member, err := NewMemberRoutes(
		&recordingUserTokens{}, unavailableBrowserSessions{}, emptyMemberships{},
		&recordingMachineEnrollments{}, unavailableWorkCommands{}, unavailableWorkQueries{}, authority,
	)
	if err != nil {
		t.Fatalf("compose member routes: %v", err)
	}
	runtimeStore := &recordingMachineRuntime{}
	machine, err := NewMachineRoutes(runtimeStore, runtimeStore)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	agent, err := NewAgentRoutes(store)
	if err != nil {
		t.Fatalf("compose Agent routes: %v", err)
	}
	server, err := NewAPI(nil, member, machine, agent)
	if err != nil {
		t.Fatalf("compose API: %v", err)
	}
	return server.Handler()
}

type recordingAgentRuns struct {
	attemptContext run.Context
	contextErr     error
	commitErr      error
	finishErr      error
	runID          string
	attemptID      string
	fence          int64
	credential     string
	commit         run.CommitCommand
	finish         run.FinishCommand
}

func (s *recordingAgentRuns) LoadAttemptContext(
	_ context.Context,
	runID string,
	attemptID string,
	fence int64,
	credential string,
) (run.Context, error) {
	s.runID = runID
	s.attemptID = attemptID
	s.fence = fence
	s.credential = credential
	return s.attemptContext, s.contextErr
}

func (s *recordingAgentRuns) CommitWorkUnderstanding(_ context.Context, command run.CommitCommand) error {
	s.commit = command
	return s.commitErr
}

func (s *recordingAgentRuns) FinishUnresolvedAttempt(_ context.Context, command run.FinishCommand) error {
	s.finish = command
	return s.finishErr
}

var _ AgentRunStore = (*recordingAgentRuns)(nil)
