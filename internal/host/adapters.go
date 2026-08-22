package host

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/machine"
)

var (
	ErrInvalidAdapterSet      = errors.New("Host adapter set is invalid")
	ErrInvalidAdapterSnapshot = errors.New("Host Agent observation is structurally invalid")
)

// Adapter observes the stable occurrences exposed by one concrete native Agent family.
type Adapter interface {
	Key() agent.AdapterKey
	Observe(context.Context) ([]Occurrence, error)
}

// Occurrence is one adapter-local discovery result.
type Occurrence struct {
	Key      agent.OccurrenceKey
	Present  bool
	Executor Executor
}

type adapterOccurrenceKey struct {
	adapterKey    agent.AdapterKey
	occurrenceKey agent.OccurrenceKey
}

// Snapshot is one complete, sorted local observation with exact present executors.
type Snapshot struct {
	observations []machine.AgentObservation
	executors    map[adapterOccurrenceKey]Executor
}

// Observations returns a copy of the complete sorted Machine report body.
func (snapshot Snapshot) Observations() []machine.AgentObservation {
	return append([]machine.AgentObservation(nil), snapshot.observations...)
}

// Executor returns only the exact present adapter occurrence; it never falls back.
func (snapshot Snapshot) Executor(adapterKey agent.AdapterKey, occurrenceKey agent.OccurrenceKey) (Executor, bool) {
	executor, ok := snapshot.executors[adapterOccurrenceKey{
		adapterKey:    adapterKey,
		occurrenceKey: occurrenceKey,
	}]
	return executor, ok
}

// AdapterSet is an immutable explicitly composed set of concrete Host adapters.
type AdapterSet struct {
	adapters []Adapter
}

// NewAdapterSet copies an explicit adapter composition and rejects duplicate families.
func NewAdapterSet(adapters ...Adapter) (AdapterSet, error) {
	if len(adapters) == 0 || len(adapters) > agent.MaxObservationsPerReport {
		return AdapterSet{}, ErrInvalidAdapterSet
	}
	copied := append([]Adapter(nil), adapters...)
	families := make(map[agent.AdapterKey]struct{}, len(copied))
	for _, adapter := range copied {
		if adapter == nil {
			return AdapterSet{}, ErrInvalidAdapterSet
		}
		key := adapter.Key()
		if !agent.ValidAdapterKey(key) {
			return AdapterSet{}, ErrInvalidAdapterSet
		}
		if _, duplicate := families[key]; duplicate {
			return AdapterSet{}, ErrInvalidAdapterSet
		}
		families[key] = struct{}{}
	}
	return AdapterSet{adapters: copied}, nil
}

// Observe collects every adapter before either publishing one complete snapshot or failing it.
func (set AdapterSet) Observe(ctx context.Context) (Snapshot, error) {
	observations := make([]machine.AgentObservation, 0, len(set.adapters))
	executors := make(map[adapterOccurrenceKey]Executor)
	pairs := make(map[adapterOccurrenceKey]struct{})
	var observationErrors []error
	for _, adapter := range set.adapters {
		family := adapter.Key()
		occurrences, err := adapter.Observe(ctx)
		if err != nil {
			observationErrors = append(observationErrors, fmt.Errorf("observe %s adapter: %w", family, err))
			continue
		}
		for _, occurrence := range occurrences {
			pair := adapterOccurrenceKey{
				adapterKey:    family,
				occurrenceKey: occurrence.Key,
			}
			if !agent.ValidOccurrenceKey(occurrence.Key) {
				observationErrors = append(observationErrors, ErrInvalidAdapterSnapshot)
				continue
			}
			if _, duplicate := pairs[pair]; duplicate {
				observationErrors = append(observationErrors, ErrInvalidAdapterSnapshot)
				continue
			}
			pairs[pair] = struct{}{}
			if occurrence.Present && occurrence.Executor == nil {
				observationErrors = append(observationErrors, ErrInvalidAdapterSnapshot)
				continue
			}
			observations = append(observations, machine.AgentObservation{
				AdapterKey:    family,
				OccurrenceKey: occurrence.Key,
				Present:       occurrence.Present,
			})
			if occurrence.Present {
				executors[pair] = occurrence.Executor
			}
		}
	}
	if len(observations) > agent.MaxObservationsPerReport {
		observationErrors = append(observationErrors, ErrInvalidAdapterSnapshot)
	}
	if len(observationErrors) != 0 {
		return Snapshot{}, errors.Join(observationErrors...)
	}
	sort.Slice(observations, func(left, right int) bool {
		if observations[left].AdapterKey != observations[right].AdapterKey {
			return observations[left].AdapterKey < observations[right].AdapterKey
		}
		return observations[left].OccurrenceKey < observations[right].OccurrenceKey
	})
	return Snapshot{
		observations: observations,
		executors:    executors,
	}, nil
}
