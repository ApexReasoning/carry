package space

import "errors"

var ErrForbidden = errors.New("member lacks permission")

type Membership struct {
	SpaceID           string `json:"space_id"`
	Name              string `json:"name"`
	CanEnrollMachines bool   `json:"can_enroll_machines"`
}
