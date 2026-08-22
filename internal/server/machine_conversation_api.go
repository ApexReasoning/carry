package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/go-chi/chi/v5"
)

// MachineConversations exposes the complete private-reply transactions consumed by a Host.
type MachineConversations interface {
	ClaimConversationReply(context.Context, string) (conversation.ReplyClaim, error)
	RenewConversationReply(context.Context, conversation.RenewReplyCommand) (time.Time, error)
	CommitConversationReply(context.Context, conversation.CommitReplyCommand) (conversation.CommitReplyResult, error)
}

type machineConversationAPI struct {
	conversations MachineConversations
}

type renewConversationReplyRequest struct {
	Fence int64 `json:"fence"`
}

type commitConversationReplyRequest struct {
	Fence          *int64          `json:"fence"`
	Reply          *string         `json:"reply"`
	DelegationGoal json.RawMessage `json:"delegation_goal"`
}

type conversationContextMessageWire struct {
	Author conversation.Author `json:"author"`
	Text   string              `json:"text"`
}

type conversationReplyClaimWire struct {
	SourceMessageID string                           `json:"source_message_id"`
	Fence           int64                            `json:"fence"`
	LeaseExpiresAt  time.Time                        `json:"lease_expires_at"`
	Messages        []conversationContextMessageWire `json:"messages"`
}

func (api machineConversationAPI) claim(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	claim, err := api.conversations.ClaimConversationReply(request.Context(), machineID)
	if errors.Is(err, conversation.ErrNoReplyAvailable) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeMachineStoreError(response, err)
		return
	}
	messages := make([]conversationContextMessageWire, 0, len(claim.Messages))
	for _, message := range claim.Messages {
		messages = append(messages, conversationContextMessageWire{Author: message.Author, Text: message.Text})
	}
	writeJSON(response, http.StatusOK, conversationReplyClaimWire{
		SourceMessageID: claim.SourceMessageID,
		Fence:           claim.Fence,
		LeaseExpiresAt:  claim.LeaseExpiresAt,
		Messages:        messages,
	})
}

func (api machineConversationAPI) renew(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	sourceMessageID, ok := pathUUID(response, request, "source_message_id")
	if !ok {
		return
	}
	var body renewConversationReplyRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	lease, err := api.conversations.RenewConversationReply(request.Context(), conversation.RenewReplyCommand{
		MachineID: machineID, SourceMessageID: sourceMessageID, Fence: body.Fence,
	})
	if err != nil {
		writeMachineStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}{LeaseExpiresAt: lease})
}

func (api machineConversationAPI) commit(response http.ResponseWriter, request *http.Request) {
	machineID, ok := currentMachine(response, request)
	if !ok {
		return
	}
	sourceMessageID, ok := pathUUID(response, request, "source_message_id")
	if !ok {
		return
	}
	var body commitConversationReplyRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if body.Fence == nil || body.Reply == nil || len(body.DelegationGoal) == 0 {
		writeAPIError(response, http.StatusBadRequest, "commit command requires fence, reply, and delegation_goal")
		return
	}
	candidate := conversation.ReplyCandidate{Reply: *body.Reply}
	if string(body.DelegationGoal) != "null" {
		var goal string
		if err := json.Unmarshal(body.DelegationGoal, &goal); err != nil {
			writeAPIError(response, http.StatusBadRequest, "delegation_goal must be a string or null")
			return
		}
		candidate.DelegationGoal = &goal
	}
	result, err := api.conversations.CommitConversationReply(request.Context(), conversation.CommitReplyCommand{
		MachineID: machineID, SourceMessageID: sourceMessageID, Fence: *body.Fence,
		Candidate: candidate,
	})
	if err != nil {
		writeMachineStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		ReplyMessageID string `json:"reply_message_id"`
		CreatedWorkID  string `json:"created_work_id,omitempty"`
	}{ReplyMessageID: result.ReplyMessageID, CreatedWorkID: result.CreatedWorkID})
}

func (api machineConversationAPI) mount(router chi.Router) {
	router.Post("/conversation-replies/claim", api.claim)
	router.Post("/conversation-replies/{source_message_id}/renew", api.renew)
	router.Post("/conversation-replies/{source_message_id}/commit", api.commit)
}
