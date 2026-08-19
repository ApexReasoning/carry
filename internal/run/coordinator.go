package run

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CoordinatorCreator atomically fixes the next pending Work input range.
type CoordinatorCreator interface {
	CreateCoordinatorRun(context.Context) (Coordinator, error)
}

// Coordinate continuously drains newly eligible Work into durable coordinator Runs.
func Coordinate(ctx context.Context, creator CoordinatorCreator, interval time.Duration) error {
	if creator == nil {
		return errors.New("coordinator creator is required")
	}
	if interval <= 0 {
		return errors.New("coordinator interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := createPendingCoordinators(ctx, creator); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func createPendingCoordinators(ctx context.Context, creator CoordinatorCreator) error {
	for {
		if _, err := creator.CreateCoordinatorRun(ctx); err != nil {
			if errors.Is(err, ErrNoCoordinatorNeeded) {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("create pending coordinator Run: %w", err)
		}
	}
}
