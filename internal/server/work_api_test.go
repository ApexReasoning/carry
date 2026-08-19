package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/work"
)

func TestCreateWorkTakesOwnerFromAuthenticatedMember(t *testing.T) {
	t.Parallel()

	const (
		spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID  = "3ce2b155-b998-458e-9e5e-f022ca509135"
	)
	commands := &recordingWorkCommands{created: work.Work{
		WorkID: workID, SpaceID: spaceID, Goal: "Prepare the renewal analysis",
		Lifecycle: work.LifecycleOpen, OwnerUserID: "member-9", CreatorUserID: "member-9",
		InputHeadSeq: 1,
	}}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+spaceID+"/works",
		bytes.NewBufferString(`{"goal":"Prepare the renewal analysis"}`),
	)
	request.Header.Set("Authorization", "Bearer member-token")
	request.Header.Set("Idempotency-Key", "renewal-analysis")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if commands.create.CreatorUserID != "member-9" || commands.create.SpaceID != spaceID {
		t.Fatalf("create command = %#v", commands.create)
	}
	if commands.create.IdempotencyKey != "renewal-analysis" {
		t.Fatalf("idempotency key = %q", commands.create.IdempotencyKey)
	}
}

func TestCreateWorkRejectsCallerNominatedOwner(t *testing.T) {
	t.Parallel()

	const spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
	commands := &recordingWorkCommands{}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+spaceID+"/works",
		bytes.NewBufferString(`{"goal":"Prepare the renewal analysis","owner_user_id":"other-member"}`),
	)
	request.Header.Set("Authorization", "Bearer member-token")
	request.Header.Set("Idempotency-Key", "renewal-analysis")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if commands.create.Goal != "" {
		t.Fatalf("invalid request reached Work command: %#v", commands.create)
	}
}

func TestAppendWorkMessageUsesAuthenticatedAuthor(t *testing.T) {
	t.Parallel()

	const (
		spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID  = "3ce2b155-b998-458e-9e5e-f022ca509135"
	)
	commands := &recordingWorkCommands{message: work.Message{
		MessageID: "4f7d55d9-157a-42dc-b339-e415487a1d60", WorkID: workID,
		AuthorUserID: "member-9", Text: "The renewal date is 30 September", InputSeq: 2,
	}}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+spaceID+"/works/"+workID+"/messages",
		bytes.NewBufferString(`{"text":"The renewal date is 30 September"}`),
	)
	request.Header.Set("Authorization", "Bearer member-token")
	request.Header.Set("Idempotency-Key", "renewal-date")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if commands.append.AuthorUserID != "member-9" || commands.append.WorkID != workID {
		t.Fatalf("append command = %#v", commands.append)
	}
}

func workTestAPI(t *testing.T, commands WorkCommands, queries WorkQueries) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	member, err := NewMemberRoutes(
		&recordingUserTokens{user: identity.AuthenticatedUser{UserID: "member-9"}},
		unavailableBrowserSessions{}, emptyMemberships{}, &recordingMachineEnrollments{},
		commands, queries, authority,
	)
	if err != nil {
		t.Fatalf("compose member routes: %v", err)
	}
	runtimeStore := &recordingMachineRuntime{}
	machine, err := NewMachineRoutes(runtimeStore, runtimeStore)
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

type recordingWorkCommands struct {
	created work.Work
	message work.Message
	create  work.CreateCommand
	append  work.AppendMessageCommand
}

func (s *recordingWorkCommands) CreateWork(_ context.Context, command work.CreateCommand) (work.Work, error) {
	s.create = command
	return s.created, nil
}

func (s *recordingWorkCommands) AppendWorkMessage(_ context.Context, command work.AppendMessageCommand) (work.Message, error) {
	s.append = command
	return s.message, nil
}

type recordingWorkQueries struct {
	listed  []work.Work
	details work.Details
}

func (s *recordingWorkQueries) ListWorks(context.Context, string, string) ([]work.Work, error) {
	return s.listed, nil
}

func (s *recordingWorkQueries) LoadWork(context.Context, string, string, string) (work.Details, error) {
	return s.details, nil
}

var _ MachineEnrollmentStore = (*recordingMachineEnrollments)(nil)
var _ MachineRuntimeStore = (*recordingMachineRuntime)(nil)
var _ = host.RuntimeObservation{}
