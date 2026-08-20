package work

import (
	"testing"

	"github.com/ApexReasoning/carry/internal/space"
)

func TestSelectSpaceUsesCurrentMemberships(t *testing.T) {
	t.Parallel()

	memberships := []space.Membership{
		{SpaceID: "11111111-1111-4111-8111-111111111111", Name: "Research"},
		{SpaceID: "22222222-2222-4222-8222-222222222222", Name: "Operations"},
	}
	selected, err := selectSpace(memberships, "22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("select Space: %v", err)
	}
	if selected != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("selected Space = %q", selected)
	}
	if _, err := selectSpace(memberships, "33333333-3333-4333-8333-333333333333"); err == nil {
		t.Fatal("selected Space outside current memberships")
	}
	if _, err := selectSpace(memberships, ""); err == nil {
		t.Fatal("multiple Spaces did not require explicit selection")
	}
}
