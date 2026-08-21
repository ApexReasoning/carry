package host

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
)

func parseServerURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", errors.New("server URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("server URL must not contain credentials, a path, query, or fragment")
	}
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func newTLSClient(caCertificatePEM string, certificate *tls.Certificate) (*http.Client, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(caCertificatePEM)) {
		return nil, errors.New("CA certificate is invalid")
	}
	configuration := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	if certificate != nil {
		configuration.Certificates = []tls.Certificate{*certificate}
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		// Neither a member token nor a Machine certificate follows redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{TLSClientConfig: configuration},
	}, nil
}

func newJSONRequest(ctx context.Context, method string, requestURL string, value any) (*http.Request, error) {
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(value); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, &encoded)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func sendJSON(client *http.Client, request *http.Request, destination any) error {
	response, err := client.Do(request)
	if err != nil {
		return controlPlaneRequestError(fmt.Sprintf("send %s %s", request.Method, request.URL), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return controlPlaneStatusError(request, response, strings.TrimSpace(string(limited)))
	}
	if destination == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode %s response: %w", request.URL, err)
	}
	return nil
}

func controlPlaneRequestError(action string, err error) error {
	var certificateVerificationError *tls.CertificateVerificationError
	var certificateInvalidError x509.CertificateInvalidError
	var unknownAuthorityError x509.UnknownAuthorityError
	var recordHeaderError tls.RecordHeaderError
	var alertError tls.AlertError
	if errors.As(err, &certificateVerificationError) ||
		errors.As(err, &certificateInvalidError) ||
		errors.As(err, &unknownAuthorityError) ||
		errors.As(err, &recordHeaderError) ||
		errors.As(err, &alertError) {
		return fmt.Errorf("%s: %w", action, err)
	}
	if isTemporaryControlPlaneTransportError(err) {
		return temporaryControlPlaneError(action, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func isTemporaryControlPlaneTransportError(err error) bool {
	if requestError, ok := errors.AsType[*url.Error](err); ok {
		err = requestError.Err
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	var dnsError *net.DNSError
	return errors.As(err, &dnsError)
}

func temporaryControlPlaneError(action string, err error) error {
	return fmt.Errorf("%w: %s: %v", hostdomain.ErrControlPlaneUnavailable, action, err)
}

func controlPlaneStatusError(request *http.Request, response *http.Response, detail string) error {
	message := fmt.Sprintf("%s %s returned %s", request.Method, request.URL, response.Status)
	if detail != "" {
		message += ": " + detail
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return temporaryControlPlaneError(message, errors.New("temporary server response"))
	}
	return errors.New(message)
}
