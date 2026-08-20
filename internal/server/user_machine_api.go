package server

import (
	"context"
	"encoding/base64"
	"net/http"

	"github.com/ApexReasoning/carry/internal/machine"
	"github.com/google/uuid"
)

// MachineEnrollment is the Machine owner behavior consumed by HTTP.
type MachineEnrollment interface {
	Enroll(context.Context, machine.EnrollmentRequest) (machine.MachineEnrollment, error)
}

// MachineRevocation is the complete PostgreSQL use case consumed directly by HTTP.
type MachineRevocation interface {
	RevokeMachine(context.Context, string, string, string) error
}

type userMachineAPI struct {
	enrollment MachineEnrollment
	revocation MachineRevocation
}

type enrollMachineRequest struct {
	SpaceID     string `json:"space_id"`
	DisplayName string `json:"display_name"`
	PublicKey   string `json:"public_key"`
}

type revokeMachineRequest struct {
	SpaceID   string `json:"space_id"`
	MachineID string `json:"machine_id"`
}

func (api userMachineAPI) enroll(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requireIdempotencyKey(response, request)
	if !ok {
		return
	}
	var body enrollMachineRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if uuid.Validate(body.SpaceID) != nil {
		writeAPIError(response, http.StatusBadRequest, "space_id is invalid")
		return
	}
	publicKeyDER, err := base64.StdEncoding.DecodeString(body.PublicKey)
	if err != nil || len(publicKeyDER) == 0 {
		writeAPIError(response, http.StatusBadRequest, "public_key is invalid")
		return
	}
	enrollment, err := api.enrollment.Enroll(request.Context(), machine.EnrollmentRequest{
		SpaceID: body.SpaceID, DisplayName: body.DisplayName, PublicKeyDER: publicKeyDER,
		EnrolledByUserID: user.UserID, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, struct {
		MachineID      string `json:"machine_id"`
		SpaceID        string `json:"space_id"`
		CertificatePEM string `json:"certificate_pem"`
	}{
		MachineID: enrollment.MachineID, SpaceID: enrollment.SpaceID,
		CertificatePEM: string(enrollment.CertificatePEM),
	})
}

func (api userMachineAPI) revoke(response http.ResponseWriter, request *http.Request) {
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	var body revokeMachineRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if uuid.Validate(body.SpaceID) != nil || uuid.Validate(body.MachineID) != nil {
		writeAPIError(response, http.StatusBadRequest, "Machine revocation identity is invalid")
		return
	}
	if err := api.revocation.RevokeMachine(request.Context(), user.UserID, body.SpaceID, body.MachineID); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "revoked"})
}
