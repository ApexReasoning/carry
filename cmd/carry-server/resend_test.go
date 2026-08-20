package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
)

func TestResendCodeSenderKeepsExactRecipientPayloadAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	var mutex sync.Mutex
	var keys []string
	var payloads [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode Resend request: %v", err)
		}
		encoded, _ := body.MarshalJSON()
		mutex.Lock()
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		payloads = append(payloads, encoded)
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"resend-message-1"}`))
	}))
	defer server.Close()
	sender, err := newResendCodeSender(server.URL, "restricted-key", "Carry <login@example.com>")
	if err != nil {
		t.Fatalf("create Resend sender: %v", err)
	}
	message := identity.EmailCodeMessage{
		Recipient: "person@example.com", Code: "123456", IdempotencyKey: "carry-email-challenge-1",
	}
	digest, err := sender.PayloadDigest(message)
	if err != nil {
		t.Fatalf("digest Resend payload: %v", err)
	}
	for range 2 {
		submission := sender.SubmitEmailCode(context.Background(), message, digest)
		if submission.State != identity.EmailSubmissionAccepted || submission.ProviderMessageID != "resend-message-1" {
			t.Fatalf("submission = %#v", submission)
		}
	}
	if len(keys) != 2 || keys[0] != keys[1] || string(payloads[0]) != string(payloads[1]) {
		t.Fatalf("keys = %#v, payloads equal = %t", keys, string(payloads[0]) == string(payloads[1]))
	}
}

func TestResendInvitationUsesFixedRouteWithoutAuthorityOrCredential(t *testing.T) {
	t.Parallel()
	var body struct {
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}
	var key string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		key = request.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode invitation: %v", err)
		}
		_, _ = response.Write([]byte(`{"id":"invitation-message-1"}`))
	}))
	defer server.Close()
	sender, err := newResendCodeSender(server.URL, "restricted-key", "Carry <login@example.com>")
	if err != nil {
		t.Fatalf("create sender: %v", err)
	}
	message := space.InvitationMessage{
		Recipient: "teammate@example.com", DestinationURL: "https://carry.example/invitations",
		IdempotencyKey: "space-invitation/one",
	}
	digest, err := sender.InvitationPayloadDigest(message)
	if err != nil {
		t.Fatalf("digest invitation: %v", err)
	}
	observed := sender.SubmitInvitation(context.Background(), message, digest)
	if observed.State != space.InvitationSubmissionAccepted || observed.ProviderMessageID != "invitation-message-1" {
		t.Fatalf("submission = %#v", observed)
	}
	if key != message.IdempotencyKey || len(body.To) != 1 || body.To[0] != message.Recipient {
		t.Fatalf("key = %q, to = %#v", key, body.To)
	}
	if body.Subject != "You have a Carry Space invitation" || body.Text != space.InvitationMessageText(message.DestinationURL) {
		t.Fatalf("subject/text = %q / %q", body.Subject, body.Text)
	}
	for _, forbidden := range []string{"teammate@example.com", "space_id", "session", "credential", "otp"} {
		if strings.Contains(message.DestinationURL, forbidden) {
			t.Fatalf("destination contains %q", forbidden)
		}
	}
}

func TestResendInvitationRejectsChangedSenderBeforeHTTP(t *testing.T) {
	t.Parallel()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	original, _ := newResendCodeSender(server.URL, "restricted-key", "Carry <first@example.com>")
	changed, _ := newResendCodeSender(server.URL, "restricted-key", "Carry <second@example.com>")
	message := space.InvitationMessage{Recipient: "person@example.com", DestinationURL: "https://carry.example/invitations", IdempotencyKey: "space-invitation/exact"}
	digest, err := original.InvitationPayloadDigest(message)
	if err != nil {
		t.Fatalf("digest original invitation: %v", err)
	}
	if result := changed.SubmitInvitation(context.Background(), message, digest); result.State != space.InvitationSubmissionRejected {
		t.Fatalf("changed sender result = %#v", result)
	}
	if calls != 0 {
		t.Fatalf("HTTP calls = %d, want 0", calls)
	}
}

func TestResendCodeSenderDistinguishesRejectedAndUnknown(t *testing.T) {
	t.Parallel()
	rejectedServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer rejectedServer.Close()
	rejected, err := newResendCodeSender(rejectedServer.URL, "restricted-key", "login@example.com")
	if err != nil {
		t.Fatalf("create rejected sender: %v", err)
	}
	message := identity.EmailCodeMessage{Recipient: "person@example.com", Code: "123456", IdempotencyKey: "key"}
	rejectedDigest, err := rejected.PayloadDigest(message)
	if err != nil {
		t.Fatalf("digest rejected payload: %v", err)
	}
	if submission := rejected.SubmitEmailCode(context.Background(), message, rejectedDigest); submission.State != identity.EmailSubmissionRejected {
		t.Fatalf("rejected submission = %#v", submission)
	}

	unknownServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer unknownServer.Close()
	unknown, err := newResendCodeSender(unknownServer.URL, "restricted-key", "login@example.com")
	if err != nil {
		t.Fatalf("create unknown sender: %v", err)
	}
	unknown.client.Timeout = 10 * time.Millisecond
	unknownDigest, err := unknown.PayloadDigest(message)
	if err != nil {
		t.Fatalf("digest unknown payload: %v", err)
	}
	if submission := unknown.SubmitEmailCode(context.Background(), message, unknownDigest); submission.State != identity.EmailSubmissionUnknown {
		t.Fatalf("unknown submission = %#v", submission)
	}
}

func TestResendCodeSenderTreatsRedirectRetryableAndMalformedResponsesAsUnknown(t *testing.T) {
	t.Parallel()
	message := identity.EmailCodeMessage{Recipient: "person@example.com", Code: "123456", IdempotencyKey: "key"}
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "redirect", status: http.StatusTemporaryRedirect},
		{name: "request timeout", status: http.StatusRequestTimeout},
		{name: "conflict", status: http.StatusConflict},
		{name: "too early", status: http.StatusTooEarly},
		{name: "rate limited", status: http.StatusTooManyRequests},
		{name: "server failure", status: http.StatusBadGateway},
		{name: "malformed success", status: http.StatusOK, body: `{"unexpected":true}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			sender, err := newResendCodeSender(server.URL, "restricted-key", "login@example.com")
			if err != nil {
				t.Fatalf("create Resend sender: %v", err)
			}
			digest, digestErr := sender.PayloadDigest(message)
			if digestErr != nil {
				t.Fatalf("digest Resend payload: %v", digestErr)
			}
			if submission := sender.SubmitEmailCode(context.Background(), message, digest); submission.State != identity.EmailSubmissionUnknown {
				t.Fatalf("submission = %#v", submission)
			}
		})
	}
}

func TestResendCodeSenderRefusesPayloadDigestMismatchBeforeHTTP(t *testing.T) {
	t.Parallel()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	sender, err := newResendCodeSender(server.URL, "restricted-key", "Carry <new@example.com>")
	if err != nil {
		t.Fatalf("create Resend sender: %v", err)
	}
	message := identity.EmailCodeMessage{
		Recipient: "person@example.com", Code: "123456", IdempotencyKey: "carry-email-challenge-1",
	}
	oldSender, err := newResendCodeSender(server.URL, "restricted-key", "Carry <old@example.com>")
	if err != nil {
		t.Fatalf("create old Resend sender: %v", err)
	}
	oldDigest, err := oldSender.PayloadDigest(message)
	if err != nil {
		t.Fatalf("digest old payload: %v", err)
	}
	if submission := sender.SubmitEmailCode(context.Background(), message, oldDigest); submission.State != identity.EmailSubmissionRejected {
		t.Fatalf("mismatched submission = %#v", submission)
	}
	if calls != 0 {
		t.Fatalf("mismatched payload made %d HTTP calls", calls)
	}
}

func TestResendEndpointRequiresHTTPSUnlessLoopback(t *testing.T) {
	t.Parallel()
	if _, err := newResendCodeSender("http://mail.example.com", "key", "login@example.com"); err == nil {
		t.Fatal("insecure non-loopback Resend endpoint was accepted")
	}
}
