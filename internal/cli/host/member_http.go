package host

import (
	"context"
	"encoding/base64"
	"net/http"

	"github.com/ApexReasoning/carry/internal/identity/memberfile"
)

type memberHTTP struct {
	client    *http.Client
	serverURL string
	token     string
}

type enrollmentResponse struct {
	MachineID      string `json:"machine_id"`
	SpaceID        string `json:"space_id"`
	CertificatePEM string `json:"certificate_pem"`
}

func connectMember(credential memberfile.Credential) (*memberHTTP, error) {
	serverURL, err := parseServerURL(credential.ServerURL)
	if err != nil {
		return nil, err
	}
	client, err := newTLSClient(credential.CACertificatePEM, nil)
	if err != nil {
		return nil, err
	}
	return &memberHTTP{client: client, serverURL: serverURL, token: credential.Token}, nil
}

func (c *memberHTTP) enrollMachine(
	ctx context.Context,
	spaceID string,
	displayName string,
	idempotencyKey string,
	publicKeyDER []byte,
) (enrollmentResponse, error) {
	body := struct {
		SpaceID     string `json:"space_id"`
		DisplayName string `json:"display_name"`
		PublicKey   string `json:"public_key"`
	}{SpaceID: spaceID, DisplayName: displayName, PublicKey: base64.StdEncoding.EncodeToString(publicKeyDER)}
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/machines/enroll", body)
	if err != nil {
		return enrollmentResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	var enrollment enrollmentResponse
	if err := sendJSON(c.client, request, &enrollment); err != nil {
		return enrollmentResponse{}, err
	}
	return enrollment, nil
}

func (c *memberHTTP) revokeMachine(ctx context.Context, spaceID string, machineID string) error {
	body := struct {
		SpaceID   string `json:"space_id"`
		MachineID string `json:"machine_id"`
	}{SpaceID: spaceID, MachineID: machineID}
	request, err := newJSONRequest(ctx, http.MethodPost, c.serverURL+"/v1/machines/revoke", body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	return sendJSON(c.client, request, nil)
}
