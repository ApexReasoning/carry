package work

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity/memberfile"
)

const maxResponseBytes = 1 << 20

type memberHTTP struct {
	origin *url.URL
	token  string
	client *http.Client
}

type outcomeUnknownError struct {
	cause error
}

func (err *outcomeUnknownError) Error() string {
	return "Work outcome is unknown; rerun the same command to reconcile: " + err.cause.Error()
}

func (err *outcomeUnknownError) Unwrap() error {
	return err.cause
}

type workWire struct {
	WorkID        string    `json:"work_id"`
	SpaceID       string    `json:"space_id"`
	Goal          string    `json:"goal"`
	Lifecycle     string    `json:"lifecycle"`
	OwnerUserID   string    `json:"owner_user_id"`
	CreatorUserID string    `json:"creator_user_id"`
	InputHeadSeq  int64     `json:"input_head_seq"`
	CreatedAt     time.Time `json:"created_at"`
}

type messageWire struct {
	MessageID    string    `json:"message_id"`
	WorkID       string    `json:"work_id"`
	AuthorUserID string    `json:"author_user_id"`
	Text         string    `json:"text"`
	InputSeq     int64     `json:"input_seq"`
	CreatedAt    time.Time `json:"created_at"`
}

type detailsWire struct {
	Work     workWire      `json:"work"`
	Messages []messageWire `json:"messages"`
}

func connectMember(credential memberfile.Credential) (*memberHTTP, error) {
	origin, err := parseServerURL(credential.ServerURL)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(credential.CACertificatePEM)) {
		return nil, errors.New("member credential contains an invalid CA certificate")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &memberHTTP{origin: origin, token: credential.Token, client: client}, nil
}

func (client *memberHTTP) create(
	ctx context.Context,
	spaceID string,
	goal string,
	idempotencyKey string,
) (workWire, error) {
	var created workWire
	err := client.sendJSON(
		ctx, http.MethodPost, "/v1/spaces/"+url.PathEscape(spaceID)+"/works",
		idempotencyKey, struct {
			Goal string `json:"goal"`
		}{Goal: goal}, &created,
	)
	return created, err
}

func (client *memberHTTP) list(ctx context.Context, spaceID string) ([]workWire, error) {
	var result struct {
		Works []workWire `json:"works"`
	}
	if err := client.sendJSON(
		ctx, http.MethodGet, "/v1/spaces/"+url.PathEscape(spaceID)+"/works", "", nil, &result,
	); err != nil {
		return nil, err
	}
	return result.Works, nil
}

func (client *memberHTTP) load(ctx context.Context, spaceID string, workID string) (detailsWire, error) {
	var result detailsWire
	err := client.sendJSON(
		ctx, http.MethodGet,
		"/v1/spaces/"+url.PathEscape(spaceID)+"/works/"+url.PathEscape(workID),
		"", nil, &result,
	)
	return result, err
}

func (client *memberHTTP) appendMessage(
	ctx context.Context,
	spaceID string,
	workID string,
	text string,
	idempotencyKey string,
) (messageWire, error) {
	var message messageWire
	err := client.sendJSON(
		ctx, http.MethodPost,
		"/v1/spaces/"+url.PathEscape(spaceID)+"/works/"+url.PathEscape(workID)+"/messages",
		idempotencyKey, struct {
			Text string `json:"text"`
		}{Text: text}, &message,
	)
	return message, err
}

func (client *memberHTTP) sendJSON(
	ctx context.Context,
	method string,
	path string,
	idempotencyKey string,
	body any,
	result any,
) error {
	encoded, err := encodeCommand(body)
	if err != nil {
		return err
	}
	attempts := 1
	if idempotencyKey != "" {
		// Mutations may be replayed once after response loss because both attempts
		// carry the exact same command bytes and idempotency identity.
		attempts = 2
	}
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, client.origin.String()+path, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("build Work request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+client.token)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response, err := client.client.Do(request)
		if err != nil {
			if attempt+1 < attempts && ctx.Err() == nil {
				continue
			}
			failure := fmt.Errorf("send Work request: %w", err)
			if idempotencyKey != "" {
				return &outcomeUnknownError{cause: failure}
			}
			return failure
		}
		return decodeResponse(response, result)
	}
	return errors.New("Work request exhausted retries")
}

func encodeCommand(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Work command: %w", err)
	}
	return encoded, nil
}

func decodeResponse(response *http.Response, result any) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Work response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return errors.New("Work response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && strings.TrimSpace(failure.Error) != "" {
			return fmt.Errorf("Work request failed (%d): %s", response.StatusCode, failure.Error)
		}
		return fmt.Errorf("Work request failed (%d)", response.StatusCode)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode Work response: %w", err)
	}
	return nil
}

func parseServerURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("server URL must be an HTTPS origin without credentials, path, query, or fragment")
	}
	parsed.Path = ""
	return parsed, nil
}
