package host

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/ApexReasoning/carry/internal/machine/machinefile"
)

const maxConnectionResponseBytes = 1 << 20

type connectionClient struct {
	origin *url.URL
	client *http.Client
}

type begunConnection struct {
	RequestID        string    `json:"request_id"`
	DisplayName      string    `json:"display_name"`
	UserCode         string    `json:"user_code"`
	PollSecret       string    `json:"poll_secret"`
	Fingerprint      string    `json:"fingerprint"`
	VerificationPath string    `json:"verification_path"`
	ExpiresAt        time.Time `json:"expires_at"`
	IntervalSeconds  int       `json:"interval_seconds"`
}

type connectedMachine struct {
	MachineID      string    `json:"machine_id"`
	SpaceID        string    `json:"space_id"`
	DisplayName    string    `json:"display_name"`
	CertificatePEM string    `json:"certificate_pem"`
	RedeemedAt     time.Time `json:"redeemed_at"`
	ReplayUntil    time.Time `json:"replay_until"`
}

type connectionHTTPError struct {
	status     int
	retryAfter time.Duration
	message    string
}

func (err *connectionHTTPError) Error() string { return err.message }

type transientConnectionError struct{ err error }

func (err *transientConnectionError) Error() string { return err.err.Error() }
func (err *transientConnectionError) Unwrap() error { return err.err }

func newConnectionClient(serverURL, caCertificatePEM string) (*connectionClient, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	client, err := newTLSClient(caCertificatePEM, nil)
	if err != nil {
		return nil, err
	}
	client.Timeout = 30 * time.Second
	return &connectionClient{origin: parsed, client: client}, nil
}

func (client *connectionClient) begin(ctx context.Context, pending machinefile.PendingConnection) (begunConnection, error) {
	body := struct {
		RequestID   string `json:"request_id"`
		DisplayName string `json:"display_name"`
		UserCode    string `json:"user_code"`
		PollSecret  string `json:"poll_secret"`
		PublicKey   string `json:"public_key"`
		KeyProof    string `json:"key_proof"`
	}{
		RequestID: pending.RequestID, DisplayName: pending.DisplayName, UserCode: pending.UserCode,
		PollSecret: pending.PollSecret, PublicKey: base64.StdEncoding.EncodeToString(pending.PublicKeyDER),
		KeyProof: base64.StdEncoding.EncodeToString(pending.KeyProof),
	}
	var result begunConnection
	if err := client.send(ctx, "/v1/machine-connections", pending.IdempotencyKey, "", body, &result, true); err != nil {
		return begunConnection{}, err
	}
	if result.RequestID != pending.RequestID || result.DisplayName != pending.DisplayName || result.UserCode != pending.UserCode ||
		result.PollSecret != pending.PollSecret || result.Fingerprint != pending.Fingerprint || result.VerificationPath != "/machine-connect" ||
		result.IntervalSeconds < int(machine.ConnectionInitialInterval/time.Second) || result.IntervalSeconds > int(machine.ConnectionMaximumInterval/time.Second) || result.ExpiresAt.IsZero() {
		return begunConnection{}, errors.New("Carry server returned an invalid Machine connection ceremony")
	}
	return result, nil
}

func (client *connectionClient) poll(ctx context.Context, pollSecret string) (connectedMachine, error) {
	var result connectedMachine
	if err := client.send(ctx, "/v1/machine-connections/status", "", pollSecret, nil, &result, false); err != nil {
		return connectedMachine{}, err
	}
	if result.MachineID == "" || result.SpaceID == "" || result.DisplayName == "" || result.CertificatePEM == "" || result.ReplayUntil.IsZero() {
		return connectedMachine{}, errors.New("Carry server returned an invalid Machine certificate")
	}
	return result, nil
}

func (client *connectionClient) cancel(ctx context.Context, pollSecret string) error {
	return client.send(ctx, "/v1/machine-connections/cancel", "", pollSecret, nil, nil, false)
}

func (client *connectionClient) send(ctx context.Context, path, key, pollSecret string, body any, destination any, retry bool) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Machine connection request: %w", err)
		}
	}
	attempts := 1
	if retry {
		attempts = 2
	}
	for attempt := range attempts {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.origin.String()+path, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("build Machine connection request: %w", err)
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		if pollSecret != "" {
			request.Header.Set("X-Carry-Machine-Connection", pollSecret)
		}
		response, err := client.client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			wrapped := fmt.Errorf("send Machine connection request: %w", err)
			if !retryableConnectionTransportError(err) {
				return wrapped
			}
			if attempt+1 < attempts {
				continue
			}
			return &transientConnectionError{err: wrapped}
		}
		return decodeConnectionResponse(response, destination)
	}
	return errors.New("Machine connection request exhausted retries")
}

func decodeConnectionResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxConnectionResponseBytes+1))
	if err != nil {
		return &transientConnectionError{err: fmt.Errorf("read Machine connection response: %w", err)}
	}
	if len(body) > maxConnectionResponseBytes {
		return errors.New("Machine connection response exceeds size limit")
	}
	if response.StatusCode == http.StatusAccepted {
		return &connectionHTTPError{status: response.StatusCode, message: machine.ErrConnectionPending.Error()}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &failure)
		message := strings.TrimSpace(failure.Error)
		if message == "" {
			message = fmt.Sprintf("Machine connection request failed (%d)", response.StatusCode)
		}
		retryAfter, _ := strconv.Atoi(response.Header.Get("Retry-After"))
		return &connectionHTTPError{status: response.StatusCode, retryAfter: time.Duration(retryAfter) * time.Second, message: message}
	}
	if destination == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode Machine connection response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("Machine connection response contains trailing JSON")
	}
	return nil
}

func retryableConnectionTransportError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}
