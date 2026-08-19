package run

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoordinateDrainsEligibleWorkBeforeWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	creator := &recordingCoordinatorCreator{resultsBeforeEmpty: 2, onEmpty: cancel}

	if err := Coordinate(ctx, creator, time.Hour); err != nil {
		t.Fatalf("coordinate pending Work: %v", err)
	}
	if creator.calls != 3 {
		t.Fatalf("coordinator creation calls = %d, want 3", creator.calls)
	}
}

func TestCoordinateReturnsStoreFailure(t *testing.T) {
	creator := &recordingCoordinatorCreator{err: errors.New("database unavailable")}

	err := Coordinate(context.Background(), creator, time.Hour)
	if err == nil || !errors.Is(err, creator.err) {
		t.Fatalf("coordinator error = %v", err)
	}
}

type recordingCoordinatorCreator struct {
	resultsBeforeEmpty int
	calls              int
	err                error
	onEmpty            func()
}

func (c *recordingCoordinatorCreator) CreateCoordinatorRun(context.Context) (Coordinator, error) {
	c.calls++
	if c.err != nil {
		return Coordinator{}, c.err
	}
	if c.calls <= c.resultsBeforeEmpty {
		return Coordinator{RunID: "run-created"}, nil
	}
	if c.onEmpty != nil {
		c.onEmpty()
	}
	return Coordinator{}, ErrNoCoordinatorNeeded
}
