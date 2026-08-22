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
	"github.com/ApexReasoning/carry/internal/machine"
)

const machineConversationSourceID = "8fd9dfd3-35db-4384-b674-e462aee445ef"

func TestMachineConversationClaimBindsCertificateAndHidesPrivateOwnership(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, "machine-private-1")
	store := &recordingMachineConversations{claim: conversation.ReplyClaim{
		SourceMessageID: machineConversationSourceID,
		Fence:           3,
		LeaseExpiresAt:  time.Date(2026, time.August, 22, 8, 0, 0, 0, time.UTC),
		Messages: []conversation.ContextMessage{
			{Author: conversation.AuthorMember, Text: "Can you help me prepare?"},
			{Author: conversation.AuthorCarry, Text: "What outcome do you need?"},
		},
	}}
	request := httptest.NewRequest(http.MethodPost, "/v1/host/conversation-replies/claim", nil)
	request.TLS = verifiedMachineTLS(certificate)
	response := httptest.NewRecorder()

	machineConversationTestAPI(t, authority, store).ServeHTTP(response, request)

	if response.Code != http.StatusOK || store.claimMachineID != "machine-private-1" {
		t.Fatalf("status = %d, machine = %q, body = %s", response.Code, store.claimMachineID, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"conversation_id", "member_user_id", "space_id", "machine_id"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("claim leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"source_message_id":"`+machineConversationSourceID+`"`) ||
		!strings.Contains(body, `"author":"member"`) {
		t.Fatalf("claim body = %s", body)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestMachineConversationMutationsBindMachineAndRejectNominatedAuthority(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, "machine-private-2")
	store := &recordingMachineConversations{
		lease: time.Date(2026, time.August, 22, 8, 5, 0, 0, time.UTC),
		result: conversation.CommitReplyResult{
			ReplyMessageID: "7bcb26b0-b74c-438c-af1e-4710481867da",
			CreatedWorkID:  "7bf76f7a-b1a7-4538-96bf-ab30b60726be",
		},
	}
	renew := httptest.NewRequest(
		http.MethodPost,
		"/v1/host/conversation-replies/"+machineConversationSourceID+"/renew",
		bytes.NewBufferString(`{"fence":4}`),
	)
	renew.TLS = verifiedMachineTLS(certificate)
	renewResponse := httptest.NewRecorder()
	machineConversationTestAPI(t, authority, store).ServeHTTP(renewResponse, renew)
	if renewResponse.Code != http.StatusOK || store.renew.MachineID != "machine-private-2" || store.renew.Fence != 4 {
		t.Fatalf("renew status = %d, command = %#v, body = %s", renewResponse.Code, store.renew, renewResponse.Body.String())
	}

	goal := "Prepare the renewal packet"
	commit := httptest.NewRequest(
		http.MethodPost,
		"/v1/host/conversation-replies/"+machineConversationSourceID+"/commit",
		bytes.NewBufferString(`{"fence":4,"reply":"I will prepare it.","delegation_goal":"`+goal+`"}`),
	)
	commit.TLS = verifiedMachineTLS(certificate)
	commitResponse := httptest.NewRecorder()
	machineConversationTestAPI(t, authority, store).ServeHTTP(commitResponse, commit)
	if commitResponse.Code != http.StatusOK || store.commit.MachineID != "machine-private-2" ||
		store.commit.SourceMessageID != machineConversationSourceID || store.commit.Candidate.DelegationGoal == nil ||
		*store.commit.Candidate.DelegationGoal != goal {
		t.Fatalf("commit status = %d, command = %#v, body = %s", commitResponse.Code, store.commit, commitResponse.Body.String())
	}
	if !strings.Contains(commitResponse.Body.String(), `"created_work_id"`) {
		t.Fatalf("commit response = %s", commitResponse.Body.String())
	}

	for _, field := range []string{"actor_user_id", "owner_user_id", "space_id", "idempotency_key"} {
		invalid := httptest.NewRequest(
			http.MethodPost,
			"/v1/host/conversation-replies/"+machineConversationSourceID+"/commit",
			bytes.NewBufferString(`{"fence":4,"reply":"private","delegation_goal":null,"`+field+`":"forged"}`),
		)
		invalid.TLS = verifiedMachineTLS(certificate)
		invalidResponse := httptest.NewRecorder()
		machineConversationTestAPI(t, authority, store).ServeHTTP(invalidResponse, invalid)
		if invalidResponse.Code != http.StatusBadRequest {
			t.Fatalf("field %q status = %d, body = %s", field, invalidResponse.Code, invalidResponse.Body.String())
		}
	}
}

func TestMachineConversationCommitRequiresAllWireFields(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, "machine-private-fields")
	for _, testCase := range []struct {
		name        string
		body        string
		wantStatus  int
		wantGoal    string
		wantNilGoal bool
	}{
		{
			name: "omitted fence", body: `{"reply":"private","delegation_goal":null}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "omitted reply", body: `{"fence":4,"delegation_goal":null}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "omitted delegation goal", body: `{"fence":4,"reply":"private"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "explicit null", body: `{"fence":4,"reply":"private","delegation_goal":null}`,
			wantStatus: http.StatusOK, wantNilGoal: true,
		},
		{
			name: "explicit string", body: `{"fence":4,"reply":"private","delegation_goal":"Prepare the brief"}`,
			wantStatus: http.StatusOK, wantGoal: "Prepare the brief",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := &recordingMachineConversations{}
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/host/conversation-replies/"+machineConversationSourceID+"/commit",
				bytes.NewBufferString(testCase.body),
			)
			request.TLS = verifiedMachineTLS(certificate)
			response := httptest.NewRecorder()

			machineConversationTestAPI(t, authority, store).ServeHTTP(response, request)

			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			if testCase.wantStatus != http.StatusOK {
				if store.commit.MachineID != "" {
					t.Fatalf("invalid command reached Store: %#v", store.commit)
				}
				return
			}
			if store.commit.MachineID != "machine-private-fields" || store.commit.Fence != 4 ||
				store.commit.Candidate.Reply != "private" {
				t.Fatalf("commit command = %#v", store.commit)
			}
			if testCase.wantNilGoal {
				if store.commit.Candidate.DelegationGoal != nil {
					t.Fatalf("explicit null delegation goal = %q", *store.commit.Candidate.DelegationGoal)
				}
				return
			}
			if store.commit.Candidate.DelegationGoal == nil || *store.commit.Candidate.DelegationGoal != testCase.wantGoal {
				t.Fatalf("delegation goal = %#v, want %q", store.commit.Candidate.DelegationGoal, testCase.wantGoal)
			}
		})
	}
}

func TestMachineConversationRoutesRejectMemberCredentialsAndMapReplyErrors(t *testing.T) {
	t.Parallel()
	authority, certificate := testMachineCertificate(t, "machine-private-3")
	memberRequest := httptest.NewRequest(http.MethodPost, "/v1/host/conversation-replies/claim", nil)
	memberRequest.Header.Set("Authorization", "Bearer "+testCLIBearer(t))
	memberRequest.TLS = verifiedMachineTLS(certificate)
	memberResponse := httptest.NewRecorder()
	store := &recordingMachineConversations{}
	machineConversationTestAPI(t, authority, store).ServeHTTP(memberResponse, memberRequest)
	if memberResponse.Code != http.StatusUnauthorized || store.claimMachineID != "" {
		t.Fatalf("member claim status = %d, machine = %q", memberResponse.Code, store.claimMachineID)
	}

	for _, testCase := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "empty", err: conversation.ErrNoReplyAvailable, status: http.StatusNoContent},
		{name: "revoked", err: machine.ErrMachineRevoked, status: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			errorStore := &recordingMachineConversations{err: testCase.err}
			request := httptest.NewRequest(http.MethodPost, "/v1/host/conversation-replies/claim", nil)
			request.TLS = verifiedMachineTLS(certificate)
			response := httptest.NewRecorder()
			machineConversationTestAPI(t, authority, errorStore).ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.status, response.Body.String())
			}
		})
	}

	for _, testCase := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "stale", err: conversation.ErrStaleReplyClaim, status: http.StatusConflict},
		{name: "altered", err: conversation.ErrReplyConflict, status: http.StatusConflict},
		{name: "invalid", err: conversation.ErrInvalidText, status: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			errorStore := &recordingMachineConversations{err: testCase.err}
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/host/conversation-replies/"+machineConversationSourceID+"/commit",
				bytes.NewBufferString(`{"fence":1,"reply":"private","delegation_goal":null}`),
			)
			request.TLS = verifiedMachineTLS(certificate)
			response := httptest.NewRecorder()
			machineConversationTestAPI(t, authority, errorStore).ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.status, response.Body.String())
			}
		})
	}
}

func machineConversationTestAPI(
	t *testing.T,
	authority *machine.CertificateAuthority,
	conversations *recordingMachineConversations,
) http.Handler {
	t.Helper()
	member := testUserRoutes(t, authority)
	machine, err := NewMachineRoutes(&recordingMachineRuns{}, conversations, unavailableMachineConnections{}, unavailableMachineAgentReports{})
	if err != nil {
		t.Fatalf("compose Machine routes: %v", err)
	}
	return mustAPI(t, member, machine)
}

type recordingMachineConversations struct {
	claimMachineID string
	claim          conversation.ReplyClaim
	renew          conversation.RenewReplyCommand
	commit         conversation.CommitReplyCommand
	lease          time.Time
	result         conversation.CommitReplyResult
	err            error
}

func (store *recordingMachineConversations) ClaimConversationReply(
	_ context.Context,
	machineID string,
) (conversation.ReplyClaim, error) {
	store.claimMachineID = machineID
	return store.claim, store.err
}

func (store *recordingMachineConversations) RenewConversationReply(
	_ context.Context,
	command conversation.RenewReplyCommand,
) (time.Time, error) {
	store.renew = command
	return store.lease, store.err
}

func (store *recordingMachineConversations) CommitConversationReply(
	_ context.Context,
	command conversation.CommitReplyCommand,
) (conversation.CommitReplyResult, error) {
	store.commit = command
	return store.result, store.err
}
