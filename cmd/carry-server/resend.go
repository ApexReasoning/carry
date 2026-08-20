package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
)

var (
	errInvalidResendURL    = errors.New("Resend API URL must use HTTPS or explicit loopback HTTP")
	errMissingResendConfig = errors.New("Resend API key and sender are required")
)

type resendCodeSender struct {
	client  *http.Client
	baseURL string
	apiKey  string
	from    string
}

func newResendCodeSender(baseURL string, apiKey string, from string) (*resendCodeSender, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, errInvalidResendURL
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || !isLoopbackHost(host) {
			return nil, errInvalidResendURL
		}
	}
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(from) == "" {
		return nil, errMissingResendConfig
	}
	return &resendCodeSender{
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: parsed.String(), apiKey: strings.TrimSpace(apiKey), from: strings.TrimSpace(from),
	}, nil
}

func (sender *resendCodeSender) PayloadDigest(message identity.EmailCodeMessage) ([sha256.Size]byte, error) {
	body, err := sender.payload(message)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	canonical := append([]byte(message.IdempotencyKey+"\x00"), body...)
	return sha256.Sum256(canonical), nil
}

func (sender *resendCodeSender) SubmitEmailCode(
	ctx context.Context,
	message identity.EmailCodeMessage,
	expectedDigest [sha256.Size]byte,
) identity.EmailSubmission {
	body, err := sender.payload(message)
	if err != nil {
		return identity.EmailSubmission{State: identity.EmailSubmissionRejected}
	}
	actualDigest, err := sender.PayloadDigest(message)
	if err != nil || subtle.ConstantTimeCompare(actualDigest[:], expectedDigest[:]) != 1 {
		return identity.EmailSubmission{State: identity.EmailSubmissionRejected}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sender.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return identity.EmailSubmission{State: identity.EmailSubmissionRejected}
	}
	request.Header.Set("Authorization", "Bearer "+sender.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", message.IdempotencyKey)
	response, err := sender.client.Do(request)
	if err != nil {
		return identity.EmailSubmission{State: identity.EmailSubmissionUnknown}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if definitiveResendRejection(response.StatusCode) {
			return identity.EmailSubmission{State: identity.EmailSubmissionRejected}
		}
		return identity.EmailSubmission{State: identity.EmailSubmissionUnknown}
	}
	var accepted struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&accepted); err != nil || strings.TrimSpace(accepted.ID) == "" {
		return identity.EmailSubmission{State: identity.EmailSubmissionUnknown}
	}
	return identity.EmailSubmission{
		State: identity.EmailSubmissionAccepted, ProviderMessageID: strings.TrimSpace(accepted.ID),
	}
}

func (sender *resendCodeSender) payload(message identity.EmailCodeMessage) ([]byte, error) {
	return json.Marshal(struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}{
		From: sender.from, To: []string{message.Recipient}, Subject: "Your Carry sign-in code",
		Text: "Your Carry sign-in code is " + message.Code + ". It expires in 5 minutes. Only the newest code works.",
	})
}

func definitiveResendRejection(status int) bool {
	if status < 400 || status >= 500 {
		return false
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
		return false
	default:
		return true
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
