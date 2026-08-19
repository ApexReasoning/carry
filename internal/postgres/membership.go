package postgres

import (
	"context"
	"fmt"

	"github.com/ApexReasoning/carry/internal/space"
)

func (s *Store) ListMemberships(ctx context.Context, userID string) ([]space.Membership, error) {
	rows, err := s.queries.ListMemberships(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	memberships := make([]space.Membership, 0, len(rows))
	for _, row := range rows {
		memberships = append(memberships, space.Membership{
			SpaceID: row.SpaceID, Name: row.Name, CanEnrollMachines: row.CanEnrollMachines,
		})
	}
	return memberships, nil
}
