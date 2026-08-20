// Package reference owns the fixed, read-only Reference Catalog transport used by native adapters.
package reference

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxKeyBytes      = 1024
	MaxResponseBytes = 64 << 10
	requestTimeout   = 5 * time.Second
)

var (
	ErrInvalidBaseURL = errors.New("reference base URL is invalid")
	ErrInvalidKey     = errors.New("reference key is invalid")
	ErrRedirect       = errors.New("reference lookup redirected")
	ErrResponse       = errors.New("reference lookup returned an invalid response")
)

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

// New creates a client for one fixed Reference Catalog base URL. Plain HTTP is
// accepted only for loopback fixtures; operator-configured remote catalogs must
// use HTTPS.
func New(baseURL string) (*Client, error) {
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Go can transparently replay idempotent requests on reused HTTP/1.1
	// connections and retry HTTP/2 streams. Fresh HTTP/1.1 connections keep
	// this transport to the single catalog attempt promised by the contract.
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	return &Client{
		baseURL: parsed,
		http: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return nil, ErrInvalidBaseURL
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost(parsed.Hostname())) {
		return nil, ErrInvalidBaseURL
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// Lookup performs exactly one bounded GET. The returned text is untrusted
// context for the native Agent and is never persisted by this package.
func (client *Client) Lookup(ctx context.Context, key string) (string, error) {
	if client == nil || client.baseURL == nil || client.http == nil {
		return "", ErrInvalidBaseURL
	}
	if !validKey(key) {
		return "", ErrInvalidKey
	}
	endpoint := *client.baseURL
	escapedKey := url.PathEscape(key)
	endpoint.Path = "/v1/references/" + key
	endpoint.RawPath = "/v1/references/" + escapedKey
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build reference lookup: %w", err)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("lookup reference: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < 400 {
		return "", ErrRedirect
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("%w: HTTP %d", ErrResponse, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read reference response: %w", err)
	}
	if len(body) > MaxResponseBytes {
		return "", fmt.Errorf("%w: response exceeds %d bytes", ErrResponse, MaxResponseBytes)
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("%w: response is not valid UTF-8", ErrResponse)
	}
	return string(body), nil
}

func validKey(key string) bool {
	return len(key) > 0 && len(key) <= MaxKeyBytes && key != "." && key != ".." &&
		utf8.ValidString(key) && !strings.ContainsRune(key, 0)
}
