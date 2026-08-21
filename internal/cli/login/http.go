package login

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
	"strconv"
	"strings"
	"time"
)

const maxLoginResponseBytes = 1 << 20

type loginClient struct {
	origin *url.URL
	client *http.Client
}

type begunLogin struct {
	RequestID        string    `json:"request_id"`
	UserCode         string    `json:"user_code"`
	PollSecret       string    `json:"poll_secret"`
	VerificationPath string    `json:"verification_path"`
	ExpiresAt        time.Time `json:"expires_at"`
	IntervalSeconds  int       `json:"interval_seconds"`
}

type redeemedLogin struct {
	CredentialID string    `json:"credential_id"`
	Credential   string    `json:"credential"`
	UserID       string    `json:"user_id"`
	SpaceID      string    `json:"space_id"`
	Label        string    `json:"label"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type loginHTTPError struct {
	status     int
	retryAfter time.Duration
	message    string
}

func (err *loginHTTPError) Error() string { return err.message }

func newLoginClient(origin *url.URL, caCertificatePEM string) (*loginClient, error) {
	var roots *x509.CertPool
	if strings.TrimSpace(caCertificatePEM) != "" {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(caCertificatePEM)) {
			return nil, errors.New("CA certificate is invalid")
		}
	}
	return &loginClient{origin: origin, client: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}},
	}}, nil
}

func (client *loginClient) begin(ctx context.Context, requestID, key, label, replacementID string) (begunLogin, error) {
	body := struct {
		RequestID                       string `json:"request_id"`
		Label                           string `json:"label"`
		ProposedReplacementCredentialID string `json:"proposed_replacement_credential_id,omitempty"`
	}{requestID, label, replacementID}
	var result begunLogin
	if err := client.send(ctx, http.MethodPost, "/v1/cli-logins", key, "", body, &result, true); err != nil {
		return begunLogin{}, err
	}
	if result.RequestID != requestID || result.UserCode == "" || result.PollSecret == "" || result.VerificationPath != "/cli-login" ||
		result.IntervalSeconds < 5 || result.ExpiresAt.IsZero() {
		return begunLogin{}, errors.New("Carry server returned an invalid CLI login ceremony")
	}
	return result, nil
}

func (client *loginClient) poll(ctx context.Context, pollSecret string) (redeemedLogin, error) {
	var result redeemedLogin
	err := client.send(ctx, http.MethodPost, "/v1/cli-logins/poll", "", pollSecret, nil, &result, false)
	if err != nil {
		return redeemedLogin{}, err
	}
	if result.CredentialID == "" || !strings.HasPrefix(result.Credential, "carry_cli_") || result.UserID == "" || result.SpaceID == "" || result.ExpiresAt.IsZero() {
		return redeemedLogin{}, errors.New("Carry server returned an invalid CLI credential")
	}
	return result, nil
}

func (client *loginClient) cancel(ctx context.Context, pollSecret string) error {
	return client.send(ctx, http.MethodPost, "/v1/cli-logins/cancel", "", pollSecret, nil, nil, false)
}

func (client *loginClient) revoke(ctx context.Context, credential, key string) error {
	return client.sendWithBearer(ctx, http.MethodPost, "/v1/cli-credentials/current/revoke", key, credential)
}

func (client *loginClient) send(ctx context.Context, method, path, key, pollSecret string, body any, destination any, retry bool) error {
	encoded, err := encode(body)
	if err != nil {
		return err
	}
	attempts := 1
	if retry {
		attempts = 2
	}
	for attempt := range attempts {
		request, err := http.NewRequestWithContext(ctx, method, client.origin.String()+path, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("build CLI login request: %w", err)
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		if pollSecret != "" {
			request.Header.Set("X-Carry-CLI-Poll", pollSecret)
		}
		response, err := client.client.Do(request)
		if err != nil {
			if attempt+1 < attempts && ctx.Err() == nil {
				continue
			}
			return fmt.Errorf("send CLI login request: %w", err)
		}
		return decodeLoginResponse(response, destination)
	}
	return errors.New("CLI login request exhausted retries")
}

func (client *loginClient) sendWithBearer(ctx context.Context, method, path, key, credential string) error {
	request, err := http.NewRequestWithContext(ctx, method, client.origin.String()+path, nil)
	if err != nil {
		return fmt.Errorf("build CLI credential request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Idempotency-Key", key)
	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("send CLI credential request: %w", err)
	}
	return decodeLoginResponse(response, nil)
}

func encode(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode CLI login request: %w", err)
	}
	return encoded, nil
}

func decodeLoginResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxLoginResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read CLI login response: %w", err)
	}
	if len(body) > maxLoginResponseBytes {
		return errors.New("CLI login response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &failure)
		message := strings.TrimSpace(failure.Error)
		if message == "" {
			message = fmt.Sprintf("CLI login request failed (%d)", response.StatusCode)
		}
		retryAfter, _ := strconv.Atoi(response.Header.Get("Retry-After"))
		return &loginHTTPError{status: response.StatusCode, retryAfter: time.Duration(retryAfter) * time.Second, message: message}
	}
	if response.StatusCode == http.StatusAccepted {
		return &loginHTTPError{status: http.StatusAccepted, message: "CLI login is awaiting Browser approval"}
	}
	if destination == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode CLI login response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("CLI login response contains trailing JSON")
	}
	return nil
}
