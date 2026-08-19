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
}

// WorkQueries loads Work facts under current Space membership.
type WorkQueries interface {
	ListWorks(context.Context, string, string) ([]work.Work, error)
	LoadWork(context.Context, string, string, string) (work.Details, error)
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

type workWire struct {
	WorkID          string         `json:"work_id"`
	SpaceID         string         `json:"space_id"`
	Goal            string         `json:"goal"`
	Lifecycle       work.Lifecycle `json:"lifecycle"`
	OwnerUserID     string         `json:"owner_user_id"`
	CreatorUserID   string         `json:"creator_user_id"`
	InputHeadSeq    int64          `json:"input_head_seq"`
	AppliedInputSeq int64          `json:"applied_input_seq"`
	CurrentRevision int64          `json:"current_revision"`
	Understanding   string         `json:"understanding"`
	NextStep        string         `json:"next_step"`
	CreatedAt       time.Time      `json:"created_at"`
}

type workMessageWire struct {
	MessageID    string    `json:"message_id"`
	WorkID       string    `json:"work_id"`
	AuthorUserID string    `json:"author_user_id"`
	Text         string    `json:"text"`
	InputSeq     int64     `json:"input_seq"`
	CreatedAt    time.Time `json:"created_at"`
}

func (api workAPI) mount(router chi.Router) {
	router.Post("/spaces/{spaceID}/works", api.create)
	router.Get("/spaces/{spaceID}/works", api.list)
	router.Get("/spaces/{spaceID}/works/{workID}", api.load)
	router.Post("/spaces/{spaceID}/works/{workID}/messages", api.appendMessage)
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
	listed, err := api.queries.ListWorks(request.Context(), user.UserID, spaceID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	works := make([]workWire, 0, len(listed))
	for _, item := range listed {
		works = append(works, workToWire(item))
	}
	writeJSON(response, http.StatusOK, struct {
		Works []workWire `json:"works"`
	}{Works: works})
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
	details, err := api.queries.LoadWork(request.Context(), user.UserID, spaceID, workID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	messages := make([]workMessageWire, 0, len(details.Messages))
	for _, message := range details.Messages {
		messages = append(messages, messageToWire(message))
	}
	writeJSON(response, http.StatusOK, struct {
		Work     workWire          `json:"work"`
		Messages []workMessageWire `json:"messages"`
	}{Work: workToWire(details.Work), Messages: messages})
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

func workToWire(value work.Work) workWire {
	return workWire{
		WorkID: value.WorkID, SpaceID: value.SpaceID, Goal: value.Goal,
		Lifecycle: value.Lifecycle, OwnerUserID: value.OwnerUserID,
		CreatorUserID: value.CreatorUserID, InputHeadSeq: value.InputHeadSeq,
		AppliedInputSeq: value.AppliedInputSeq, CurrentRevision: value.CurrentRevision,
		Understanding: value.Understanding, NextStep: value.NextStep,
		CreatedAt: value.CreatedAt,
	}
}

func messageToWire(value work.Message) workMessageWire {
	return workMessageWire{
		MessageID: value.MessageID, WorkID: value.WorkID, AuthorUserID: value.AuthorUserID,
		Text: value.Text, InputSeq: value.InputSeq, CreatedAt: value.CreatedAt,
	}
}
