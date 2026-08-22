package host

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ApexReasoning/carry/internal/agent"
	"github.com/ApexReasoning/carry/internal/conversation"
	"github.com/ApexReasoning/carry/internal/machine"
)

func TestAdapterSetPublishesOneSortedCompleteSnapshotAndExactExecutors(t *testing.T) {
	piExecutor := &stubExecutor{}
	firstDeepSeek := &stubExecutor{}
	secondDeepSeek := &stubExecutor{}
	pi := &stubAdapter{
		key: "pi",
		occurrences: []Occurrence{{
			Key:      "default",
			Present:  true,
			Executor: piExecutor,
		}},
	}
	codex := &stubAdapter{key: "codex"}
	deepSeek := &stubAdapter{
		key: "deepseek-harness",
		occurrences: []Occurrence{
			{
				Key:      "second",
				Present:  true,
				Executor: secondDeepSeek,
			},
			{
				Key:      "first",
				Present:  true,
				Executor: firstDeepSeek,
			},
		},
	}
	set, err := NewAdapterSet(pi, codex, deepSeek)
	if err != nil {
		t.Fatalf("construct adapter set: %v", err)
	}

	snapshot, err := set.Observe(context.Background())
	if err != nil {
		t.Fatalf("observe complete adapter set: %v", err)
	}
	want := []machine.AgentObservation{
		{
			AdapterKey:    "deepseek-harness",
			OccurrenceKey: "first",
			Present:       true,
		},
		{
			AdapterKey:    "deepseek-harness",
			OccurrenceKey: "second",
			Present:       true,
		},
		{
			AdapterKey:    "pi",
			OccurrenceKey: "default",
			Present:       true,
		},
	}
	if !reflect.DeepEqual(snapshot.Observations(), want) {
		t.Fatalf("sorted snapshot = %#v", snapshot.Observations())
	}
	observations := snapshot.Observations()
	observations[0].Present = false
	if !snapshot.Observations()[0].Present {
		t.Fatal("caller mutated immutable snapshot")
	}
	if executor, ok := snapshot.Executor("pi", "default"); !ok || executor != piExecutor {
		t.Fatalf("Pi/default executor = %T/%t", executor, ok)
	}
	if _, ok := snapshot.Executor("codex", "default"); ok {
		t.Fatal("zero-occurrence Codex unexpectedly fell back to another executor")
	}
	if _, ok := snapshot.Executor("deepseek-harness", "missing"); ok {
		t.Fatal("missing occurrence unexpectedly selected a family executor")
	}
	if pi.calls != 1 || codex.calls != 1 || deepSeek.calls != 1 {
		t.Fatalf("adapter observation calls = Pi %d, Codex %d, DeepSeek %d", pi.calls, codex.calls, deepSeek.calls)
	}
}

func TestAdapterSetRejectsDuplicateOrMalformedFamiliesBeforeObservation(t *testing.T) {
	for _, test := range []struct {
		name     string
		adapters []Adapter
	}{
		{
			name: "empty",
		},
		{
			name:     "nil",
			adapters: []Adapter{nil},
		},
		{
			name: "malformed",
			adapters: []Adapter{&stubAdapter{
				key: "Pi",
			}},
		},
		{
			name: "duplicate",
			adapters: []Adapter{
				&stubAdapter{key: "pi"},
				&stubAdapter{key: "pi"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAdapterSet(test.adapters...); !errors.Is(err, ErrInvalidAdapterSet) {
				t.Fatalf("NewAdapterSet error = %v, want invalid set", err)
			}
			for _, adapter := range test.adapters {
				if typed, ok := adapter.(*stubAdapter); ok && typed.calls != 0 {
					t.Fatalf("invalid composition observed adapter %d times", typed.calls)
				}
			}
		})
	}
}

func TestAdapterSetNeverPublishesPartialOrStructurallyInvalidSnapshot(t *testing.T) {
	discoveryFailure := errors.New("installation metadata is unreadable")
	tests := []struct {
		name      string
		first     *stubAdapter
		second    *stubAdapter
		wantError error
	}{
		{
			name: "discovery failure",
			first: &stubAdapter{
				key: "pi",
				occurrences: []Occurrence{{
					Key:      "default",
					Present:  true,
					Executor: &stubExecutor{},
				}},
			},
			second: &stubAdapter{
				key: "codex",
				err: discoveryFailure,
			},
			wantError: discoveryFailure,
		},
		{
			name: "empty occurrence key",
			first: &stubAdapter{
				key: "pi",
				occurrences: []Occurrence{{
					Key:     "",
					Present: false,
				}},
			},
			second: &stubAdapter{
				key: "codex",
			},
			wantError: ErrInvalidAdapterSnapshot,
		},
		{
			name: "duplicate pair",
			first: &stubAdapter{
				key: "pi",
				occurrences: []Occurrence{
					{Key: "default"},
					{Key: "default"},
				},
			},
			second: &stubAdapter{
				key: "codex",
			},
			wantError: ErrInvalidAdapterSnapshot,
		},
		{
			name: "present without executor",
			first: &stubAdapter{
				key: "pi",
				occurrences: []Occurrence{{
					Key:     "default",
					Present: true,
				}},
			},
			second: &stubAdapter{
				key: "codex",
			},
			wantError: ErrInvalidAdapterSnapshot,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, err := NewAdapterSet(test.first, test.second)
			if err != nil {
				t.Fatalf("construct adapter set: %v", err)
			}
			snapshot, err := set.Observe(context.Background())
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Observe error = %v, want %v", err, test.wantError)
			}
			if len(snapshot.Observations()) != 0 {
				t.Fatalf("failed observation published partial snapshot: %#v", snapshot.Observations())
			}
			if test.first.calls != 1 || test.second.calls != 1 {
				t.Fatalf("failed complete observation calls = %d/%d", test.first.calls, test.second.calls)
			}
		})
	}
}

func TestAdapterSetRejectsOverBoundCompleteSnapshot(t *testing.T) {
	occurrences := make([]Occurrence, agent.MaxObservationsPerReport+1)
	for index := range occurrences {
		occurrences[index] = Occurrence{
			Key: agent.OccurrenceKey("occurrence-" + strconv.Itoa(index)),
		}
	}
	set, err := NewAdapterSet(&stubAdapter{
		key:         "future",
		occurrences: occurrences,
	})
	if err != nil {
		t.Fatalf("construct adapter set: %v", err)
	}
	snapshot, err := set.Observe(context.Background())
	if !errors.Is(err, ErrInvalidAdapterSnapshot) || len(snapshot.Observations()) != 0 {
		t.Fatalf("over-bound snapshot = %#v, error = %v", snapshot.Observations(), err)
	}
}

type stubAdapter struct {
	key         agent.AdapterKey
	occurrences []Occurrence
	err         error
	calls       int
}

func (adapter *stubAdapter) Key() agent.AdapterKey {
	return adapter.key
}

func (adapter *stubAdapter) Observe(context.Context) ([]Occurrence, error) {
	adapter.calls++
	return append([]Occurrence(nil), adapter.occurrences...), adapter.err
}

type stubExecutor struct{}

func (*stubExecutor) Diagnose(context.Context) error {
	return nil
}

func (*stubExecutor) Execute(context.Context, ExecutionRequest) (UnderstandingUpdate, error) {
	return UnderstandingUpdate{}, nil
}

func (*stubExecutor) Reply(context.Context, ConversationReplyRequest) (conversation.ReplyCandidate, error) {
	return conversation.ReplyCandidate{}, nil
}
