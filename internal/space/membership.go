package space

import "errors"

var ErrForbidden = errors.New("member lacks permission")

type Membership struct {
	SpaceID           string
	Name              string
	Slug              string
	CanManageMembers  bool
	CanEnrollMachines bool
}
