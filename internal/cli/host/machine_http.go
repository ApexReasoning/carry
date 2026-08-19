package host

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
	"github.com/ApexReasoning/carry/internal/host/machinefile"
	"github.com/ApexReasoning/carry/internal/run"
)

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
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&wire); err != nil {
		return run.Claim{}, fmt.Errorf("decode Run claim: %w", err)
	}
	messages := make([]run.Message, 0, len(wire.Messages))
	for _, message := range wire.Messages {
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

func attemptPath(claim run.Claim) string {
	return "/v1/host/runs/" + url.PathEscape(claim.RunID) + "/attempts/" + url.PathEscape(claim.AttemptID)
}

var _ hostdomain.RunClient = (*machineHTTP)(nil)
