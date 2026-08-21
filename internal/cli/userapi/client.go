// Package userapi implements the member-principal Carry User API transport used by CLI commands.
package userapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/cli/credentialfile"
	"github.com/ApexReasoning/carry/internal/space"
)

const maxResponseBytes = 4 << 20

type Client struct {
	origin     *url.URL
	credential string
	client     *http.Client
}

type Member struct {
	UserID      string
	DisplayName string
	Spaces      []space.Membership
}

type MachineEnrollment struct {
	MachineID      string `json:"machine_id"`
	SpaceID        string `json:"space_id"`
	CertificatePEM string `json:"certificate_pem"`
}

type Work struct {
	WorkID             string    `json:"work_id"`
	SpaceID            string    `json:"space_id"`
	Goal               string    `json:"goal"`
	Lifecycle          string    `json:"lifecycle"`
	OwnerUserID        string    `json:"owner_user_id"`
	OwnerDisplayName   string    `json:"owner_display_name"`
	CreatorUserID      string    `json:"creator_user_id"`
	CreatorDisplayName string    `json:"creator_display_name"`
	Understanding      string    `json:"understanding"`
	NextStep           string    `json:"next_step"`
	HasUnappliedInput  bool      `json:"has_unapplied_input"`
	NeedsRetry         bool      `json:"needs_retry"`
	NeedsReview        bool      `json:"needs_review"`
	ReviewID           string    `json:"review_id"`
	CreatedAt          time.Time `json:"created_at"`
}

type WorkSummary struct {
	WorkID             string    `json:"work_id"`
	SpaceID            string    `json:"space_id"`
	Goal               string    `json:"goal"`
	Lifecycle          string    `json:"lifecycle"`
	OwnerUserID        string    `json:"owner_user_id"`
	OwnerDisplayName   string    `json:"owner_display_name"`
	CreatorUserID      string    `json:"creator_user_id"`
	CreatorDisplayName string    `json:"creator_display_name"`
	HasUnappliedInput  bool      `json:"has_unapplied_input"`
	NeedsRetry         bool      `json:"needs_retry"`
	NeedsReview        bool      `json:"needs_review"`
	CreatedAt          time.Time `json:"created_at"`
}

type WorkPage struct {
	Works      []WorkSummary `json:"works"`
	HasEarlier bool          `json:"has_earlier_works"`
}

type WorkMessage struct {
	MessageID         string    `json:"message_id"`
	WorkID            string    `json:"work_id"`
	AuthorUserID      string    `json:"author_user_id"`
	AuthorDisplayName string    `json:"author_display_name"`
	Text              string    `json:"text"`
	CreatedAt         time.Time `json:"created_at"`
}

type WorkDetails struct {
	Work               Work          `json:"work"`
	Messages           []WorkMessage `json:"messages"`
	HasEarlierMessages bool          `json:"has_earlier_messages"`
}

type OutcomeUnknownError struct {
	cause error
}

func (err *OutcomeUnknownError) Error() string {
	return "Work outcome is unknown; rerun the same command to reconcile: " + err.cause.Error()
}

func (err *OutcomeUnknownError) Unwrap() error { return err.cause }

func New(serverURL string, caCertificatePEM string, credential string) (*Client, error) {
	origin, err := ParseServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	var roots *x509.CertPool
	if strings.TrimSpace(caCertificatePEM) != "" {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(caCertificatePEM)) {
			return nil, errors.New("CLI credential contains an invalid CA certificate")
		}
	}
	return &Client{
		origin:     origin,
		credential: credential,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    roots,
			}},
		},
	}, nil
}

func FromCredential(credential credentialfile.Credential) (*Client, error) {
	return New(credential.ServerURL, credential.CACertificatePEM, credential.Credential)
}

func ParseServerURL(raw string) (*url.URL, error) {
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

func (client *Client) LoadMember(ctx context.Context) (Member, error) {
	var wire struct {
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
		Spaces      []struct {
			SpaceID           string `json:"space_id"`
			Name              string `json:"name"`
			CanManageMembers  bool   `json:"can_manage_members"`
			CanEnrollMachines bool   `json:"can_enroll_machines"`
		} `json:"spaces"`
	}
	if err := client.send(ctx, http.MethodGet, "/v1/me", "", nil, &wire, false); err != nil {
		return Member{}, err
	}
	member := Member{UserID: wire.UserID, DisplayName: wire.DisplayName, Spaces: make([]space.Membership, 0, len(wire.Spaces))}
	for _, membership := range wire.Spaces {
		member.Spaces = append(member.Spaces, space.Membership{
			SpaceID: membership.SpaceID, Name: membership.Name,
			CanManageMembers:  membership.CanManageMembers,
			CanEnrollMachines: membership.CanEnrollMachines,
		})
	}
	return member, nil
}

func (client *Client) EnrollMachine(
	ctx context.Context,
	spaceID string,
	displayName string,
	idempotencyKey string,
	publicKeyDER []byte,
) (MachineEnrollment, error) {
	var enrollment MachineEnrollment
	err := client.send(ctx, http.MethodPost, "/v1/machines/enroll", idempotencyKey, struct {
		SpaceID     string `json:"space_id"`
		DisplayName string `json:"display_name"`
		PublicKey   string `json:"public_key"`
	}{SpaceID: spaceID, DisplayName: displayName, PublicKey: base64.StdEncoding.EncodeToString(publicKeyDER)}, &enrollment, false)
	return enrollment, err
}

func (client *Client) RevokeMachine(ctx context.Context, spaceID string, machineID string) error {
	return client.send(ctx, http.MethodPost, "/v1/machines/revoke", "", struct {
		SpaceID   string `json:"space_id"`
		MachineID string `json:"machine_id"`
	}{SpaceID: spaceID, MachineID: machineID}, nil, false)
}

func (client *Client) CreateWork(ctx context.Context, spaceID string, goal string, idempotencyKey string) (Work, error) {
	var created Work
	err := client.send(ctx, http.MethodPost, "/v1/spaces/"+url.PathEscape(spaceID)+"/works", idempotencyKey, struct {
		Goal string `json:"goal"`
	}{Goal: goal}, &created, true)
	return created, err
}

func (client *Client) ListWorks(ctx context.Context, spaceID string, before string) (WorkPage, error) {
	path := "/v1/spaces/" + url.PathEscape(spaceID) + "/works"
	if before != "" {
		path += "?before=" + url.QueryEscape(before)
	}
	var page WorkPage
	err := client.send(ctx, http.MethodGet, path, "", nil, &page, false)
	return page, err
}

func (client *Client) LoadWork(ctx context.Context, spaceID string, workID string, beforeMessage string) (WorkDetails, error) {
	path := "/v1/spaces/" + url.PathEscape(spaceID) + "/works/" + url.PathEscape(workID)
	if beforeMessage != "" {
		path += "?before=" + url.QueryEscape(beforeMessage)
	}
	var details WorkDetails
	err := client.send(ctx, http.MethodGet, path, "", nil, &details, false)
	return details, err
}

func (client *Client) RetryWork(ctx context.Context, spaceID string, workID string, idempotencyKey string) error {
	return client.send(ctx, http.MethodPost, "/v1/spaces/"+url.PathEscape(spaceID)+"/works/"+url.PathEscape(workID)+"/retry", idempotencyKey, nil, nil, true)
}

func (client *Client) AppendWorkMessage(
	ctx context.Context,
	spaceID string,
	workID string,
	text string,
	idempotencyKey string,
) error {
	return client.send(ctx, http.MethodPost, "/v1/spaces/"+url.PathEscape(spaceID)+"/works/"+url.PathEscape(workID)+"/messages", idempotencyKey, struct {
		Text string `json:"text"`
	}{Text: text}, nil, true)
}

func (client *Client) send(
	ctx context.Context,
	method string,
	path string,
	idempotencyKey string,
	body any,
	destination any,
	replayAfterResponseLoss bool,
) error {
	encoded, err := encodeCommand(body)
	if err != nil {
		return err
	}
	attempts := 1
	if replayAfterResponseLoss && idempotencyKey != "" {
		attempts = 2
	}
	for attempt := range attempts {
		request, err := http.NewRequestWithContext(ctx, method, client.origin.String()+path, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("build User API request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+client.credential)
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
			failure := fmt.Errorf("send User API request: %w", err)
			if replayAfterResponseLoss && idempotencyKey != "" {
				return &OutcomeUnknownError{cause: failure}
			}
			return failure
		}
		return decodeResponse(response, destination)
	}
	return errors.New("User API request exhausted retries")
}

func encodeCommand(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode User API command: %w", err)
	}
	return encoded, nil
}

func decodeResponse(response *http.Response, destination any) error {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read User API response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return errors.New("User API response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && strings.TrimSpace(failure.Error) != "" {
			return fmt.Errorf("User API request failed (%d): %s", response.StatusCode, failure.Error)
		}
		return fmt.Errorf("User API request failed (%d)", response.StatusCode)
	}
	if destination == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode User API response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("User API response contains trailing JSON")
	}
	return nil
}
