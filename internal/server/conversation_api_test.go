package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
)

const (
	conversationSpaceID   = "58d1172b-a71c-456f-96de-135e08c3b9fa"
	conversationMessageID = "ad4d5ae5-3fe4-42d3-b0ec-8651d0d527b7"
)

func TestSendConversationMessageUsesAuthenticatedMember(t *testing.T) {
	t.Parallel()
	store := &recordingConversations{sent: conversation.Message{
		MessageID: conversationMessageID, Author: conversation.AuthorMember,
		Text: "How should I prepare the renewal?", RequestID: "private-request-1",
		Sequence: 41, CreatedAt: time.Date(2026, time.August, 21, 8, 0, 0, 0, time.UTC),
	}}
	handler := conversationTestAPI(t, store, store)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+conversationSpaceID+"/conversation/messages",
		strings.NewReader(`{"text":"How should I prepare the renewal?"}`),
	)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "private-request-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	assertNoStore(t, response)
	if store.send.MemberUserID != "member-private" || store.send.SpaceID != conversationSpaceID ||
		store.send.IdempotencyKey != "private-request-1" {
		t.Fatalf("send command = %#v", store.send)
	}
	body := response.Body.String()
	if strings.Contains(body, "conversation_id") || strings.Contains(body, "sequence") || strings.Contains(body, "digest") {
		t.Fatalf("private wire leaked internal fact: %s", body)
	}
	if !strings.Contains(body, `"author":"member"`) || !strings.Contains(body, `"request_id":"private-request-1"`) {
		t.Fatalf("private wire = %s", body)
	}
}

func TestSendConversationMessageRejectsCallerNominatedParticipant(t *testing.T) {
	t.Parallel()
	store := &recordingConversations{}
	handler := conversationTestAPI(t, store, store)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/spaces/"+conversationSpaceID+"/conversation/messages",
		bytes.NewBufferString(`{"text":"private","member_user_id":"another-member"}`),
	)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	request.Header.Set("Idempotency-Key", "private-request-2")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || store.send.MemberUserID != "" {
		t.Fatalf("status = %d, command = %#v, body = %s", response.Code, store.send, response.Body.String())
	}
	assertNoStore(t, response)
}

func TestListConversationMessagesUsesOnlyValidatedCursors(t *testing.T) {
	t.Parallel()
	before := "4a959b1a-a1f7-456f-96dd-d28ae7264d9c"
	store := &recordingConversations{listed: []conversation.Message{{
		MessageID: conversationMessageID, Author: conversation.AuthorCarry,
		Text: "Start with the confirmed renewal date.", CreatedWorkID: "d29de3c8-40f3-4fcc-a37c-20b52ebdf2b6",
		Sequence: 12, CreatedAt: time.Date(2026, time.August, 21, 8, 1, 0, 0, time.UTC),
	}}}
	handler := conversationTestAPI(t, store, store)
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/spaces/"+conversationSpaceID+"/conversation/messages?before="+before,
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || store.list.Before != before || store.list.MemberUserID != "member-private" {
		t.Fatalf("status = %d, list = %#v, body = %s", response.Code, store.list, response.Body.String())
	}
	assertNoStore(t, response)
	body := response.Body.String()
	if strings.Contains(body, "conversation_id") || strings.Contains(body, "sequence") ||
		strings.Contains(body, "request_digest") || strings.Contains(body, `"request_id"`) {
		t.Fatalf("private Carry reply leaked internal fact: %s", body)
	}
	if !strings.Contains(body, `"author":"carry"`) || !strings.Contains(body, `"created_work_id"`) {
		t.Fatalf("private list wire = %s", body)
	}

	for _, target := range []string{
		"?before=not-a-uuid",
		"?after=not-a-uuid",
		"?before=" + before + "&before=" + conversationMessageID,
		"?before=" + before + "&after=" + conversationMessageID,
	} {
		invalid := httptest.NewRequest(
			http.MethodGet,
			"/v1/spaces/"+conversationSpaceID+"/conversation/messages"+target,
			nil,
		)
		invalid.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
		invalidResponse := httptest.NewRecorder()
		handler.ServeHTTP(invalidResponse, invalid)
		if invalidResponse.Code != http.StatusBadRequest {
			t.Fatalf("target %q status = %d; body = %s", target, invalidResponse.Code, invalidResponse.Body.String())
		}
		assertNoStore(t, invalidResponse)
	}
}

func TestConversationStoreErrorsRemainDistinct(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid text", err: conversation.ErrInvalidText, status: http.StatusBadRequest},
		{name: "invalid cursor", err: conversation.ErrInvalidCursor, status: http.StatusBadRequest},
		{name: "idempotency conflict", err: conversation.ErrIdempotencyConflict, status: http.StatusConflict},
		{name: "reply pending", err: conversation.ErrReplyPending, status: http.StatusConflict},
		{name: "former member", err: space.ErrForbidden, status: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := &recordingConversations{err: testCase.err}
			handler := conversationTestAPI(t, store, store)
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/spaces/"+conversationSpaceID+"/conversation/messages",
				strings.NewReader(`{"text":"private question"}`),
			)
			request.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
			request.Header.Set("Idempotency-Key", "private-request-error")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.status, response.Body.String())
			}
			assertNoStore(t, response)
		})
	}
}

func assertNoStore(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func conversationTestAPI(
	t *testing.T,
	commands ConversationCommands,
	queries ConversationQueries,
) http.Handler {
	t.Helper()
	authority := testAuthority(t)
	member := testUserRoutes(t, authority)
	authentication, err := NewUserAuthentication(
		&recordingCLICredentials{user: identity.AuthenticatedUser{UserID: "member-private"}},
		unavailableBrowserSessions{},
		testIdentityCredentials(t),
		testExternalOrigin(t),
	)
	if err != nil {
		t.Fatalf("compose User authentication: %v", err)
	}
	conversationRoutes, err := NewConversationRoutes(commands, queries)
	if err != nil {
		t.Fatalf("compose Conversation routes: %v", err)
	}
	member.authentication = authentication
	member.conversations = conversationRoutes
	machine, err := NewMachineRoutes(&recordingMachineRuns{}, unavailableMachineConversations{})
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

type recordingConversations struct {
	send   conversation.SendCommand
	list   conversation.ListCommand
	sent   conversation.Message
	listed []conversation.Message
	err    error
}

func (store *recordingConversations) SendConversationMessage(
	_ context.Context,
	command conversation.SendCommand,
) (conversation.Message, error) {
	store.send = command
	return store.sent, store.err
}

func (store *recordingConversations) ListConversationMessages(
	_ context.Context,
	command conversation.ListCommand,
) ([]conversation.Message, error) {
	store.list = command
	return store.listed, store.err
}
