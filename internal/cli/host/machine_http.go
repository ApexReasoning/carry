package host

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/conversation"
	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
	"github.com/ApexReasoning/carry/internal/run"
	"github.com/ApexReasoning/carry/internal/work"
	"github.com/google/uuid"
)

const maxRunClaimWireBytes = 2 << 20

type machineHTTP struct {
	client    *http.Client
	serverURL string
}

type runMessageWire struct {
	AuthorUserID string `json:"author_user_id"`
	Text         string `json:"text"`
}

type runClaimWire struct {
	RunID                    string           `json:"run_id"`
	AttemptID                string           `json:"attempt_id"`
	WorkID                   string           `json:"work_id"`
	Fence                    int64            `json:"fence"`
	LeaseExpiresAt           time.Time        `json:"lease_expires_at"`
	Goal                     string           `json:"goal"`
	CurrentUnderstanding     string           `json:"current_understanding"`
	CurrentNextStep          string           `json:"current_next_step"`
	BaseUnderstandingVersion int64            `json:"base_understanding_version"`
	InputEndSeq              int64            `json:"input_end_seq"`
	Messages                 []runMessageWire `json:"messages"`
}

func connectMachine(credential machinefile.Credential) (*machineHTTP, error) {
	serverURL, err := parseServerURL(credential.ServerURL)
	if err != nil {
		return nil, err
	}
	certificate, err := tls.X509KeyPair([]byte(credential.CertificatePEM), []byte(credential.PrivateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("load Machine key pair: %w", err)
	}
	client, err := newTLSClient(credential.CACertificatePEM, &certificate)
	if err != nil {
		return nil, err
	}
	return &machineHTTP{client: client, serverURL: serverURL}, nil
}

func (c *machineHTTP) Claim(ctx context.Context) (run.Claim, error) {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/host/runs/claim", struct{}{})
	if err != nil {
		return run.Claim{}, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return run.Claim{}, fmt.Errorf("claim Run: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return run.Claim{}, run.ErrNoRunAvailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return run.Claim{}, fmt.Errorf(
			"POST %s returned %s: %s",
			request.URL,
			response.Status,
			strings.TrimSpace(string(limited)),
		)
	}
	var wire runClaimWire
	if err := decodeBoundedExactJSON(response.Body, maxRunClaimWireBytes, &wire); err != nil {
		return run.Claim{}, fmt.Errorf("decode Run claim: %w", err)
	}
	if uuid.Validate(wire.RunID) != nil || uuid.Validate(wire.AttemptID) != nil ||
		uuid.Validate(wire.WorkID) != nil || wire.Fence <= 0 || wire.LeaseExpiresAt.IsZero() ||
		wire.BaseUnderstandingVersion < 0 || wire.InputEndSeq <= 0 {
		return run.Claim{}, errors.New("decode Run claim: invalid authority")
	}
	goal, goalErr := work.NormalizeGoal(wire.Goal)
	if goalErr != nil || goal != wire.Goal {
		return run.Claim{}, errors.New("decode Run claim: invalid goal")
	}
	if (wire.CurrentUnderstanding == "") != (wire.CurrentNextStep == "") {
		return run.Claim{}, errors.New("decode Run claim: invalid current understanding")
	}
	if wire.CurrentUnderstanding != "" {
		understanding, nextStep, updateErr := run.ValidateUnderstandingUpdate(
			wire.CurrentUnderstanding, wire.CurrentNextStep,
		)
		if updateErr != nil || understanding != wire.CurrentUnderstanding || nextStep != wire.CurrentNextStep {
			return run.Claim{}, errors.New("decode Run claim: invalid current understanding")
		}
	}
	if len(wire.Messages) > run.MaxInputMessages {
		return run.Claim{}, errors.New("decode Run claim: invalid bounded messages")
	}
	messages := make([]run.Message, 0, len(wire.Messages))
	messageTextBytes := 0
	for _, message := range wire.Messages {
		messageTextBytes += len(message.Text)
		if uuid.Validate(message.AuthorUserID) != nil || work.ValidateMessage(message.Text) != nil ||
			messageTextBytes > run.MaxInputTextBytes {
			return run.Claim{}, errors.New("decode Run claim: invalid bounded messages")
		}
		messages = append(messages, run.Message{AuthorUserID: message.AuthorUserID, Text: message.Text})
	}
	return run.Claim{
		RunID: wire.RunID, AttemptID: wire.AttemptID, WorkID: wire.WorkID,
		Fence: wire.Fence, LeaseExpiresAt: wire.LeaseExpiresAt,
		Goal: wire.Goal, CurrentUnderstanding: wire.CurrentUnderstanding,
		CurrentNextStep:          wire.CurrentNextStep,
		BaseUnderstandingVersion: wire.BaseUnderstandingVersion,
		InputEndSeq:              wire.InputEndSeq, Messages: messages,
	}, nil
}

func (c *machineHTTP) Renew(ctx context.Context, claim run.Claim) (time.Time, error) {
	request, err := newJSONRequest(
		ctx,
		http.MethodPost,
		c.serverURL+attemptPath(claim)+"/renew",
		struct {
			Fence int64 `json:"fence"`
		}{Fence: claim.Fence},
	)
	if err != nil {
		return time.Time{}, err
	}
	var wire struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}
	if err := sendJSON(c.client, request, &wire); err != nil {
		return time.Time{}, err
	}
	return wire.LeaseExpiresAt, nil
}

func (c *machineHTTP) Commit(ctx context.Context, claim run.Claim, update hostdomain.UnderstandingUpdate) error {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+attemptPath(claim)+"/understanding", struct {
		Fence                    int64  `json:"fence"`
		BaseUnderstandingVersion int64  `json:"base_understanding_version"`
		InputEndSeq              int64  `json:"input_end_seq"`
		Understanding            string `json:"understanding"`
		NextStep                 string `json:"next_step"`
	}{
		Fence: claim.Fence, BaseUnderstandingVersion: claim.BaseUnderstandingVersion,
		InputEndSeq: claim.InputEndSeq, Understanding: update.Understanding, NextStep: update.NextStep,
	})
	if err != nil {
		return err
	}
	return sendJSON(c.client, request, nil)
}

func (c *machineHTTP) Finish(ctx context.Context, claim run.Claim, outcome run.State) error {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+attemptPath(claim)+"/outcome", struct {
		Fence   int64     `json:"fence"`
		Outcome run.State `json:"outcome"`
	}{Fence: claim.Fence, Outcome: outcome})
	if err != nil {
		return err
	}
	return sendJSON(c.client, request, nil)
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

func (c *machineHTTP) ClaimConversation(ctx context.Context) (conversation.ReplyClaim, error) {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/host/conversation-replies/claim", struct{}{})
	if err != nil {
		return conversation.ReplyClaim{}, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return conversation.ReplyClaim{}, fmt.Errorf("claim private Conversation reply: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return conversation.ReplyClaim{}, conversation.ErrNoReplyAvailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return conversation.ReplyClaim{}, fmt.Errorf("POST %s returned %s", request.URL, response.Status)
	}
	const maxClaimWireBytes = conversation.MaxContextTextBytes*6 + 64*1024
	var wire conversationReplyClaimWire
	if err := decodeBoundedExactJSON(response.Body, maxClaimWireBytes, &wire); err != nil {
		return conversation.ReplyClaim{}, fmt.Errorf("decode private Conversation reply claim: %w", err)
	}
	if uuid.Validate(wire.SourceMessageID) != nil || wire.Fence <= 0 || wire.LeaseExpiresAt.IsZero() {
		return conversation.ReplyClaim{}, errors.New("decode private Conversation reply claim: invalid authority")
	}
	messages := make([]conversation.ContextMessage, 0, len(wire.Messages))
	for _, message := range wire.Messages {
		messages = append(messages, conversation.ContextMessage{Author: message.Author, Text: message.Text})
	}
	fixed, err := conversation.FixedContextSuffix(messages)
	if err != nil || len(fixed) != len(messages) || messages[len(messages)-1].Author != conversation.AuthorMember {
		return conversation.ReplyClaim{}, errors.New("decode private Conversation reply claim: invalid bounded context")
	}
	for index := 1; index < len(messages); index++ {
		if messages[index-1].Author == messages[index].Author {
			return conversation.ReplyClaim{}, errors.New("decode private Conversation reply claim: invalid message order")
		}
	}
	return conversation.ReplyClaim{
		SourceMessageID: wire.SourceMessageID,
		Fence:           wire.Fence,
		LeaseExpiresAt:  wire.LeaseExpiresAt,
		Messages:        messages,
	}, nil
}

func (c *machineHTTP) RenewConversation(ctx context.Context, claim conversation.ReplyClaim) (time.Time, error) {
	request, err := newJSONRequest(
		ctx,
		http.MethodPost,
		c.serverURL+conversationReplyPath(claim)+"/renew",
		struct {
			Fence int64 `json:"fence"`
		}{Fence: claim.Fence},
	)
	if err != nil {
		return time.Time{}, err
	}
	var wire struct {
		LeaseExpiresAt time.Time `json:"lease_expires_at"`
	}
	if err := sendExactConversationJSON(c.client, request, &wire); err != nil {
		return time.Time{}, err
	}
	if wire.LeaseExpiresAt.IsZero() {
		return time.Time{}, errors.New("decode private Conversation renewal: missing lease expiry")
	}
	return wire.LeaseExpiresAt, nil
}

func (c *machineHTTP) CommitConversation(
	ctx context.Context,
	claim conversation.ReplyClaim,
	candidate conversation.ReplyCandidate,
) (conversation.CommitReplyResult, error) {
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+conversationReplyPath(claim)+"/commit", struct {
		Fence          int64   `json:"fence"`
		Reply          string  `json:"reply"`
		DelegationGoal *string `json:"delegation_goal"`
	}{Fence: claim.Fence, Reply: candidate.Reply, DelegationGoal: candidate.DelegationGoal})
	if err != nil {
		return conversation.CommitReplyResult{}, err
	}
	var wire struct {
		ReplyMessageID string          `json:"reply_message_id"`
		CreatedWorkID  json.RawMessage `json:"created_work_id"`
	}
	if err := sendExactConversationJSON(c.client, request, &wire); err != nil {
		return conversation.CommitReplyResult{}, err
	}
	if uuid.Validate(wire.ReplyMessageID) != nil {
		return conversation.CommitReplyResult{}, errors.New("decode private Conversation commit: invalid reply identity")
	}
	createdWorkID := ""
	if len(wire.CreatedWorkID) != 0 {
		if string(wire.CreatedWorkID) == "null" || json.Unmarshal(wire.CreatedWorkID, &createdWorkID) != nil ||
			uuid.Validate(createdWorkID) != nil {
			return conversation.CommitReplyResult{}, errors.New("decode private Conversation commit: invalid Work identity")
		}
	}
	return conversation.CommitReplyResult{ReplyMessageID: wire.ReplyMessageID, CreatedWorkID: createdWorkID}, nil
}

func sendExactConversationJSON(client *http.Client, request *http.Request, destination any) error {
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send %s %s: %w", request.Method, request.URL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s %s returned %s", request.Method, request.URL, response.Status)
	}
	if err := decodeBoundedExactJSON(response.Body, 1<<20, destination); err != nil {
		return fmt.Errorf("decode %s response: %w", request.URL, err)
	}
	return nil
}

func decodeBoundedExactJSON(reader io.Reader, maxBytes int64, destination any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return errors.New("response exceeds its byte limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("response contains trailing JSON")
	}
	return nil
}

func attemptPath(claim run.Claim) string {
	return "/v1/host/runs/" + url.PathEscape(claim.RunID) + "/attempts/" + url.PathEscape(claim.AttemptID)
}

func conversationReplyPath(claim conversation.ReplyClaim) string {
	return "/v1/host/conversation-replies/" + url.PathEscape(claim.SourceMessageID)
}

var _ hostdomain.RunClient = (*machineHTTP)(nil)
var _ hostdomain.ConversationClient = (*machineHTTP)(nil)
