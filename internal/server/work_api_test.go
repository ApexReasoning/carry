package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		HasUnappliedInput: true,
	}}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+spaceID+"/works",
		bytes.NewBufferString(`{"goal":"Prepare the renewal analysis"}`),
	)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
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
	assertNoStore(t, response)
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
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "renewal-analysis")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if commands.create.Goal != "" {
		t.Fatalf("invalid request reached Work command: %#v", commands.create)
	}
	assertNoStore(t, response)
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
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
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

func TestAppendWorkMessageAcceptsWorstCaseEscapedValidEnvelope(t *testing.T) {
	t.Parallel()
	const (
		spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID  = "3ce2b155-b998-458e-9e5e-f022ca509135"
	)
	text := strings.Repeat("\x01", 60*1024)
	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		t.Fatalf("encode escaped message: %v", err)
	}
	if len(body) <= 64<<10 {
		t.Fatalf("escaped envelope size = %d, want above the prior command limit", len(body))
	}
	commands := &recordingWorkCommands{}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+spaceID+"/works/"+workID+"/messages",
		bytes.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "escaped-message")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if commands.append.Text != text {
		t.Fatalf("decoded message bytes = %d, want %d", len(commands.append.Text), len(text))
	}
}

func TestWorkReadCursorsAreBoundedAndExact(t *testing.T) {
	t.Parallel()
	const (
		spaceID  = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID   = "3ce2b155-b998-458e-9e5e-f022ca509135"
		cursorID = "4f7d55d9-157a-42dc-b339-e415487a1d60"
	)
	queries := &recordingWorkQueries{page: work.Page{
		Works: []work.Summary{{WorkID: workID, SpaceID: spaceID, Goal: "Prepare renewal", Lifecycle: work.LifecycleOpen,
			OwnerUserID: "member-9", OwnerDisplayName: "Mina", CreatorUserID: "member-9", CreatorDisplayName: "Mina"}},
		HasEarlier: true,
	}}
	handler := workTestAPI(t, &recordingWorkCommands{}, queries)
	request := httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works?before="+cursorID, nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || queries.listCommand.Before != cursorID ||
		!strings.Contains(response.Body.String(), `"has_earlier_works":true`) ||
		!strings.Contains(response.Body.String(), `"owner_display_name":"Mina"`) {
		t.Fatalf("list response = %d %s; command = %#v", response.Code, response.Body.String(), queries.listCommand)
	}
	assertNoStore(t, response)

	request = httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works/"+workID+"?before="+cursorID, nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || queries.loadCommand.BeforeMessage != cursorID {
		t.Fatalf("load response = %d %s; command = %#v", response.Code, response.Body.String(), queries.loadCommand)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works?before="+cursorID+"&before="+workID, nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate cursor status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestNeedsYouQueryIsExplicitAndOwnerScoped(t *testing.T) {
	t.Parallel()
	const spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
	queries := &recordingWorkQueries{page: work.Page{Works: []work.Summary{{
		WorkID: "3ce2b155-b998-458e-9e5e-f022ca509135", SpaceID: spaceID,
		Goal: "Prepare renewal", Lifecycle: work.LifecycleOpen, NeedsReview: true,
	}}}}
	handler := workTestAPI(t, &recordingWorkCommands{}, queries)
	request := httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works?needs_you=true", nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !queries.listCommand.NeedsYou ||
		!strings.Contains(response.Body.String(), `"needs_review":true`) {
		t.Fatalf("Needs You response = %d %s; command = %#v", response.Code, response.Body.String(), queries.listCommand)
	}

	for _, rawQuery := range []string{"needs_you=maybe", "needs_you=true&needs_you=false"} {
		request = httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works?"+rawQuery, nil)
		request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, body = %s", rawQuery, response.Code, response.Body.String())
		}
	}
}

func TestWorkDetailBindsCurrentReviewIdentityToCurrentContent(t *testing.T) {
	t.Parallel()
	const (
		spaceID  = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID   = "3ce2b155-b998-458e-9e5e-f022ca509135"
		reviewID = "4f7d55d9-157a-42dc-b339-e415487a1d60"
	)
	queries := &recordingWorkQueries{details: work.Details{Work: work.Work{
		WorkID: workID, SpaceID: spaceID, Goal: "Prepare renewal", Lifecycle: work.LifecycleOpen,
		Understanding: "The recommendation is ready.", NextStep: "Review the recommendation.",
		NeedsReview: true, ReviewID: reviewID,
	}}}
	handler := workTestAPI(t, &recordingWorkCommands{}, queries)
	request := httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works/"+workID, nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"understanding":"The recommendation is ready."`) ||
		!strings.Contains(response.Body.String(), `"needs_review":true`) ||
		!strings.Contains(response.Body.String(), `"review_id":"`+reviewID+`"`) {
		t.Fatalf("reviewable Work response = %d %s", response.Code, response.Body.String())
	}
}

func TestAcceptWorkReviewUsesAuthenticatedOwnerAndIdempotency(t *testing.T) {
	t.Parallel()
	const (
		spaceID  = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID   = "3ce2b155-b998-458e-9e5e-f022ca509135"
		reviewID = "4f7d55d9-157a-42dc-b339-e415487a1d60"
	)
	commands := &recordingWorkCommands{}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+spaceID+"/works/"+workID+"/reviews/"+reviewID+"/accept",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "accept-renewal-result")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if commands.accept.WorkID != workID || commands.accept.SpaceID != spaceID ||
		commands.accept.ReviewID != reviewID || commands.accept.AcceptedBy != "member-9" ||
		commands.accept.IdempotencyKey != "accept-renewal-result" {
		t.Fatalf("accept command = %#v", commands.accept)
	}
}

func TestWorkStoreCursorErrorsAreBadRequests(t *testing.T) {
	t.Parallel()
	const spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
	handler := workTestAPI(t, &recordingWorkCommands{}, &recordingWorkQueries{listErr: work.ErrInvalidCursor})
	request := httptest.NewRequest(http.MethodGet, "/v1/spaces/"+spaceID+"/works", nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cursor error status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRetryWorkUsesAuthenticatedMemberAndIdempotency(t *testing.T) {
	t.Parallel()
	const (
		spaceID = "2ba3dd27-1b41-453c-8057-91f31a0d13b1"
		workID  = "3ce2b155-b998-458e-9e5e-f022ca509135"
	)
	commands := &recordingWorkCommands{}
	handler := workTestAPI(t, commands, &recordingWorkQueries{})
	request := httptest.NewRequest(http.MethodPost, "/v1/spaces/"+spaceID+"/works/"+workID+"/retry", nil)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "retry-renewal")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if commands.retry.WorkID != workID || commands.retry.SpaceID != spaceID ||
		commands.retry.RequestedBy != "member-9" || commands.retry.IdempotencyKey != "retry-renewal" {
		t.Fatalf("retry command = %#v", commands.retry)
	}
}

func workTestAPI(t *testing.T, commands WorkCommands, queries WorkQueries) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	member := testUserRoutes(t, authority)
	authentication, err := NewUserAuthentication(
		&recordingCLICredentials{user: identity.AuthenticatedUser{UserID: "member-9"}},
		unavailableBrowserSessions{},
		testIdentityCredentials(t),
		testExternalOrigin(t),
	)
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	workRoutes, err := NewWorkRoutes(commands, queries)
	if err != nil {
		t.Fatalf("compose Work routes: %v", err)
	}
	member.authentication = authentication
	member.works = workRoutes
	runStore := &recordingMachineRuns{}
	machine, err := NewMachineRoutes(runStore, unavailableMachineConversations{}, unavailableMachineConnections{})
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
	retry   work.RetryCommand
	accept  work.AcceptReviewCommand
}

func (s *recordingWorkCommands) CreateWork(_ context.Context, command work.CreateCommand) (work.Work, error) {
	s.create = command
	return s.created, nil
}

func (s *recordingWorkCommands) AppendWorkMessage(_ context.Context, command work.AppendMessageCommand) (work.Message, error) {
	s.append = command
	return s.message, nil
}

func (s *recordingWorkCommands) RequestWorkRetry(_ context.Context, command work.RetryCommand) error {
	s.retry = command
	return nil
}

func (s *recordingWorkCommands) AcceptWorkReview(_ context.Context, command work.AcceptReviewCommand) error {
	s.accept = command
	return nil
}

type recordingWorkQueries struct {
	page        work.Page
	details     work.Details
	listCommand work.ListCommand
	loadCommand work.LoadCommand
	listErr     error
	loadErr     error
}

func (s *recordingWorkQueries) ListWorks(_ context.Context, command work.ListCommand) (work.Page, error) {
	s.listCommand = command
	return s.page, s.listErr
}

func (s *recordingWorkQueries) LoadWork(_ context.Context, command work.LoadCommand) (work.Details, error) {
	s.loadCommand = command
	return s.details, s.loadErr
}
