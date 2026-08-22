package space

import (
	"bytes"
	"errors"
	"testing"
)

func TestNewRemoveMemberCommandBindsExactRemovalFacts(t *testing.T) {
	t.Parallel()
	request := RemoveMemberRequest{
		SpaceID: "10000000-0000-4000-8000-000000000001", ActorUserID: "20000000-0000-4000-8000-000000000001",
		TargetUserID: "30000000-0000-4000-8000-000000000001", SuccessorUserID: "40000000-0000-4000-8000-000000000001",
		IdempotencyKey: "  remove-member-1  ",
	}
	command, err := NewRemoveMemberCommand(request)
	if err != nil {
		t.Fatalf("new removal command: %v", err)
	}
	if command.SpaceID != request.SpaceID || command.ActorUserID != request.ActorUserID || command.TargetUserID != request.TargetUserID || command.SuccessorUserID != request.SuccessorUserID || command.IdempotencyKey != "remove-member-1" {
		t.Fatalf("command = %#v", command)
	}
	changed := request
	changed.SuccessorUserID = "50000000-0000-4000-8000-000000000001"
	changedCommand, err := NewRemoveMemberCommand(changed)
	if err != nil {
		t.Fatalf("new changed removal command: %v", err)
	}
	if bytes.Equal(command.RequestDigest[:], changedCommand.RequestDigest[:]) {
		t.Fatal("changed successor did not change canonical digest")
	}
}

func TestNewRemoveMemberCommandRejectsInvalidFacts(t *testing.T) {
	valid := RemoveMemberRequest{
		SpaceID: "10000000-0000-4000-8000-000000000001", ActorUserID: "20000000-0000-4000-8000-000000000001",
		TargetUserID: "30000000-0000-4000-8000-000000000001", IdempotencyKey: "remove-member-1",
	}
	for name, mutate := range map[string]func(*RemoveMemberRequest){
		"space":     func(request *RemoveMemberRequest) { request.SpaceID = "invalid" },
		"actor":     func(request *RemoveMemberRequest) { request.ActorUserID = "invalid" },
		"target":    func(request *RemoveMemberRequest) { request.TargetUserID = "invalid" },
		"successor": func(request *RemoveMemberRequest) { request.SuccessorUserID = "invalid" },
		"key":       func(request *RemoveMemberRequest) { request.IdempotencyKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if _, err := NewRemoveMemberCommand(request); !errors.Is(err, ErrInvalidMemberRemoval) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
