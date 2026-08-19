package login

import (
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

	"github.com/ApexReasoning/carry/internal/space"
)

type memberHTTP struct {
	client    *http.Client
	serverURL string
	token     string
}

type memberInfo struct {
	UserID string             `json:"user_id"`
	Spaces []space.Membership `json:"spaces"`
}

func newMemberHTTP(serverURL string, caCertificatePEM string, token string) (*memberHTTP, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(caCertificatePEM)) {
		return nil, errors.New("CA certificate is invalid")
	}
	return &memberHTTP{
		client: &http.Client{
			Timeout: 15 * time.Second,
			// Credential-bearing requests must remain on the configured origin.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    roots,
			}},
		},
		serverURL: serverURL,
		token:     token,
	}, nil
}

func (c *memberHTTP) loadInfo(ctx context.Context) (memberInfo, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serverURL+"/v1/me", nil)
	if err != nil {
		return memberInfo{}, fmt.Errorf("create member request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return memberInfo{}, fmt.Errorf("load member information: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return memberInfo{}, fmt.Errorf("GET /v1/me returned %s: %s", response.Status, strings.TrimSpace(string(limited)))
	}
	var info memberInfo
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&info); err != nil {
		return memberInfo{}, fmt.Errorf("decode member information: %w", err)
	}
	return info, nil
}

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
