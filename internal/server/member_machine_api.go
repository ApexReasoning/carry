package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/google/uuid"
)

// MachineEnrollmentStore handles only member-authorized Machine enrollment
// and revocation; it cannot report Host runtime state.
type MachineEnrollmentStore interface {
	EnrollMachine(context.Context, host.EnrollMachineCommand) (host.MachineEnrollment, error)
	RevokeMachine(context.Context, string, string, string) error
}

type memberMachineAPI struct {
	store     MachineEnrollmentStore
	authority *host.CertificateAuthority
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

func (api memberMachineAPI) enroll(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
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
	publicKeyDER, err := base64.StdEncoding.DecodeString(body.PublicKey)
	if err != nil || len(publicKeyDER) == 0 {
		writeAPIError(response, http.StatusBadRequest, "public_key is invalid")
		return
	}
	machineID := uuid.NewString()
	issued, err := api.authority.IssueMachineCertificate(machineID, publicKeyDER, time.Now().UTC())
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "public_key is invalid")
		return
	}
	enrollment, err := api.store.EnrollMachine(request.Context(), host.EnrollMachineCommand{
		MachineID: machineID, SpaceID: body.SpaceID, DisplayName: body.DisplayName,
		PublicKeyDER: publicKeyDER, CertificatePEM: issued.CertificatePEM,
		CertificateSerial: issued.Serial, EnrolledByUserID: user.UserID,
		IdempotencyKey: idempotencyKey,
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

func (api memberMachineAPI) revoke(response http.ResponseWriter, request *http.Request) {
	user, ok := currentMember(response, request)
	if !ok {
		return
	}
	var body revokeMachineRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := api.store.RevokeMachine(request.Context(), user.UserID, body.SpaceID, body.MachineID); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "revoked"})
}
