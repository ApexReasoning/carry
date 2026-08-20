package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/work"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// WorkCommands persists member-authored Work changes.
type WorkCommands interface {
	CreateWork(context.Context, work.CreateCommand) (work.Work, error)
	AppendWorkMessage(context.Context, work.AppendMessageCommand) (work.Message, error)
	RequestWorkRetry(context.Context, work.RetryCommand) error
}

// WorkQueries loads bounded Work facts under current Space membership.
type WorkQueries interface {
	ListWorks(context.Context, work.ListCommand) (work.Page, error)
	LoadWork(context.Context, work.LoadCommand) (work.Details, error)
}

type workAPI struct {
	commands WorkCommands
	queries  WorkQueries
}

type createWorkRequest struct {
	Goal string `json:"goal"`
}

type appendWorkMessageRequest struct {
	Text string `json:"text"`
}

type workSummaryWire struct {
	WorkID             string         `json:"work_id"`
	SpaceID            string         `json:"space_id"`
	Goal               string         `json:"goal"`
	Lifecycle          work.Lifecycle `json:"lifecycle"`
	OwnerUserID        string         `json:"owner_user_id"`
	OwnerDisplayName   string         `json:"owner_display_name"`
	CreatorUserID      string         `json:"creator_user_id"`
	CreatorDisplayName string         `json:"creator_display_name"`
	HasUnappliedInput  bool           `json:"has_unapplied_input"`
	NeedsRetry         bool           `json:"needs_retry"`
	CreatedAt          time.Time      `json:"created_at"`
}

type workWire struct {
	WorkID             string         `json:"work_id"`
	SpaceID            string         `json:"space_id"`
	Goal               string         `json:"goal"`
	Lifecycle          work.Lifecycle `json:"lifecycle"`
	OwnerUserID        string         `json:"owner_user_id"`
	OwnerDisplayName   string         `json:"owner_display_name"`
	CreatorUserID      string         `json:"creator_user_id"`
	CreatorDisplayName string         `json:"creator_display_name"`
	Understanding      string         `json:"understanding"`
	NextStep           string         `json:"next_step"`
	HasUnappliedInput  bool           `json:"has_unapplied_input"`
	NeedsRetry         bool           `json:"needs_retry"`
	CreatedAt          time.Time      `json:"created_at"`
}

type workMessageWire struct {
	MessageID         string    `json:"message_id"`
	WorkID            string    `json:"work_id"`
	AuthorUserID      string    `json:"author_user_id"`
	AuthorDisplayName string    `json:"author_display_name"`
	Text              string    `json:"text"`
	CreatedAt         time.Time `json:"created_at"`
}

func (api workAPI) mount(router chi.Router) {
	router.Post("/spaces/{spaceID}/works", api.create)
	router.Get("/spaces/{spaceID}/works", api.list)
	router.Get("/spaces/{spaceID}/works/{workID}", api.load)
	router.Post("/spaces/{spaceID}/works/{workID}/messages", api.appendMessage)
	router.Post("/spaces/{spaceID}/works/{workID}/retry", api.retry)
}

func (api workAPI) create(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	spaceID, ok := pathUUID(response, request, "spaceID")
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body createWorkRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	created, err := api.commands.CreateWork(request.Context(), work.CreateCommand{
		SpaceID: spaceID, CreatorUserID: user.UserID,
		Goal: body.Goal, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, workToWire(created))
}

func (api workAPI) list(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	spaceID, ok := pathUUID(response, request, "spaceID")
	if !ok {
		return
	}
	before, ok := queryUUID(response, request, "before")
	if !ok {
		return
	}
	page, err := api.queries.ListWorks(request.Context(), work.ListCommand{
		UserID: user.UserID, SpaceID: spaceID, Before: before,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	works := make([]workSummaryWire, 0, len(page.Works))
	for _, item := range page.Works {
		works = append(works, summaryToWire(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Works      []workSummaryWire `json:"works"`
		HasEarlier bool              `json:"has_earlier_works"`
	}{Works: works, HasEarlier: page.HasEarlier})
}

func (api workAPI) load(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	spaceID, ok := pathUUID(response, request, "spaceID")
	if !ok {
		return
	}
	workID, ok := pathUUID(response, request, "workID")
	if !ok {
		return
	}
	before, ok := queryUUID(response, request, "before")
	if !ok {
		return
	}
	details, err := api.queries.LoadWork(request.Context(), work.LoadCommand{
		UserID: user.UserID, SpaceID: spaceID, WorkID: workID, BeforeMessage: before,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	messages := make([]workMessageWire, 0, len(details.Messages))
	for _, message := range details.Messages {
		messages = append(messages, messageToWire(message))
	}
	writeJSON(response, http.StatusOK, struct {
		Work               workWire          `json:"work"`
		Messages           []workMessageWire `json:"messages"`
		HasEarlierMessages bool              `json:"has_earlier_messages"`
	}{Work: workToWire(details.Work), Messages: messages, HasEarlierMessages: details.HasEarlierMessages})
}

func (api workAPI) appendMessage(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	spaceID, ok := pathUUID(response, request, "spaceID")
	if !ok {
		return
	}
	workID, ok := pathUUID(response, request, "workID")
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body appendWorkMessageRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	message, err := api.commands.AppendWorkMessage(request.Context(), work.AppendMessageCommand{
		WorkID: workID, SpaceID: spaceID, AuthorUserID: user.UserID,
		Text: body.Text, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, messageToWire(message))
}

func (api workAPI) retry(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	spaceID, ok := pathUUID(response, request, "spaceID")
	if !ok {
		return
	}
	workID, ok := pathUUID(response, request, "workID")
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	if err := api.commands.RequestWorkRetry(request.Context(), work.RetryCommand{
		WorkID: workID, SpaceID: spaceID, RequestedBy: user.UserID, IdempotencyKey: idempotencyKey,
	}); err != nil {
		writeStoreError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func pathUUID(response http.ResponseWriter, request *http.Request, name string) (string, bool) {
	value := chi.URLParam(request, name)
	if uuid.Validate(value) != nil {
		writeAPIError(response, http.StatusBadRequest, name+" is invalid")
		return "", false
	}
	return value, true
}

func requireIdempotencyKey(response http.ResponseWriter, request *http.Request) (string, bool) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAPIError(response, http.StatusBadRequest, "Idempotency-Key is required")
		return "", false
	}
	return key, true
}

func summaryToWire(value work.Summary) workSummaryWire {
	return workSummaryWire{
		WorkID: value.WorkID, SpaceID: value.SpaceID, Goal: value.Goal, Lifecycle: value.Lifecycle,
		OwnerUserID: value.OwnerUserID, OwnerDisplayName: value.OwnerDisplayName,
		CreatorUserID: value.CreatorUserID, CreatorDisplayName: value.CreatorDisplayName,
		HasUnappliedInput: value.HasUnappliedInput, NeedsRetry: value.NeedsRetry, CreatedAt: value.CreatedAt,
	}
}

func workToWire(value work.Work) workWire {
	return workWire{
		WorkID: value.WorkID, SpaceID: value.SpaceID, Goal: value.Goal, Lifecycle: value.Lifecycle,
		OwnerUserID: value.OwnerUserID, OwnerDisplayName: value.OwnerDisplayName,
		CreatorUserID: value.CreatorUserID, CreatorDisplayName: value.CreatorDisplayName,
		Understanding: value.Understanding, NextStep: value.NextStep,
		HasUnappliedInput: value.HasUnappliedInput, NeedsRetry: value.NeedsRetry, CreatedAt: value.CreatedAt,
	}
}

func messageToWire(value work.Message) workMessageWire {
	return workMessageWire{
		MessageID: value.MessageID, WorkID: value.WorkID,
		AuthorUserID: value.AuthorUserID, AuthorDisplayName: value.AuthorDisplayName,
		Text: value.Text, CreatedAt: value.CreatedAt,
	}
}
