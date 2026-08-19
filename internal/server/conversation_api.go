package server

import (
	"context"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type ConversationCommands interface {
	SendConversationMessage(context.Context, conversation.SendCommand) (conversation.Message, error)
}

type ConversationQueries interface {
	ListConversationMessages(context.Context, conversation.ListCommand) ([]conversation.Message, error)
}

type conversationAPI struct {
	commands ConversationCommands
	queries  ConversationQueries
}

type sendConversationMessageRequest struct {
	Text string `json:"text"`
}

type conversationMessageWire struct {
	MessageID     string              `json:"message_id"`
	Author        conversation.Author `json:"author"`
	Text          string              `json:"text"`
	RequestID     string              `json:"request_id,omitempty"`
	CreatedWorkID string              `json:"created_work_id,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
}

func (api conversationAPI) mount(router chi.Router) {
	router.Get("/spaces/{spaceID}/conversation/messages", api.listMessages)
	router.Post("/spaces/{spaceID}/conversation/messages", api.sendMessage)
}

func (api conversationAPI) sendMessage(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	member, ok := currentMember(response, request)
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
	var body sendConversationMessageRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	message, err := api.commands.SendConversationMessage(request.Context(), conversation.SendCommand{
		SpaceID: spaceID, MemberUserID: member.UserID,
		Text: body.Text, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, conversationMessageToWire(message))
}

func (api conversationAPI) listMessages(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	member, ok := currentMember(response, request)
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
	after, ok := queryUUID(response, request, "after")
	if !ok {
		return
	}
	if err := conversation.ValidateCursors(before, after); err != nil {
		writeStoreError(response, err)
		return
	}
	messages, err := api.queries.ListConversationMessages(request.Context(), conversation.ListCommand{
		SpaceID: spaceID, MemberUserID: member.UserID, Before: before, After: after,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	wired := make([]conversationMessageWire, 0, len(messages))
	for _, message := range messages {
		wired = append(wired, conversationMessageToWire(message))
	}
	writeJSON(response, http.StatusOK, struct {
		Messages []conversationMessageWire `json:"messages"`
	}{Messages: wired})
}

func queryUUID(response http.ResponseWriter, request *http.Request, name string) (string, bool) {
	values, present := request.URL.Query()[name]
	if !present {
		return "", true
	}
	if len(values) != 1 || uuid.Validate(values[0]) != nil {
		writeAPIError(response, http.StatusBadRequest, name+" cursor is invalid")
		return "", false
	}
	return values[0], true
}

func conversationMessageToWire(message conversation.Message) conversationMessageWire {
	return conversationMessageWire{
		MessageID: message.MessageID, Author: message.Author, Text: message.Text,
		RequestID: message.RequestID, CreatedWorkID: message.CreatedWorkID,
		CreatedAt: message.CreatedAt,
	}
}
