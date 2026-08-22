package agent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVocabularyRecognizesBoundedFamiliesWithoutProcessFacts(t *testing.T) {
	vocabulary, err := NewVocabulary(
		Descriptor{
			Key:                     "pi",
			NameBase:                "Pi",
			MaxOccurrencesPerReport: 1,
		},
		Descriptor{
			Key:                     "deepseek-harness",
			NameBase:                "DeepSeek",
			MaxOccurrencesPerReport: 2,
		},
	)
	if err != nil {
		t.Fatalf("construct recognition vocabulary: %v", err)
	}

	descriptor, ok := vocabulary.Descriptor("deepseek-harness")
	if !ok || descriptor.NameBase != "DeepSeek" || descriptor.MaxOccurrencesPerReport != 2 {
		t.Fatalf("third-family descriptor = %#v, found = %t", descriptor, ok)
	}
	descriptor.NameBase = "Changed"
	unchanged, _ := vocabulary.Descriptor("deepseek-harness")
	if unchanged.NameBase != "DeepSeek" {
		t.Fatalf("returned descriptor mutated vocabulary: %#v", unchanged)
	}

	native := NativeVocabulary()
	pi, piOK := native.Descriptor("pi")
	codex, codexOK := native.Descriptor("codex")
	if !piOK || !codexOK || pi.NameBase != "Pi" || codex.NameBase != "Codex" {
		t.Fatalf("native vocabulary = Pi %#v/%t, Codex %#v/%t", pi, piOK, codex, codexOK)
	}
}

func TestVocabularyRejectsAmbiguousOrUnboundedRecognition(t *testing.T) {
	tests := []struct {
		name        string
		descriptors []Descriptor
	}{
		{
			name: "empty",
		},
		{
			name: "malformed key",
			descriptors: []Descriptor{{
				Key:                     "Pi",
				NameBase:                "Pi",
				MaxOccurrencesPerReport: 1,
			}},
		},
		{
			name: "duplicate key",
			descriptors: []Descriptor{
				{
					Key:                     "pi",
					NameBase:                "Pi",
					MaxOccurrencesPerReport: 1,
				},
				{
					Key:                     "pi",
					NameBase:                "Other Pi",
					MaxOccurrencesPerReport: 1,
				},
			},
		},
		{
			name: "normalized base collision",
			descriptors: []Descriptor{
				{
					Key:                     "fullwidth",
					NameBase:                "Ｐｉ",
					MaxOccurrencesPerReport: 1,
				},
				{
					Key:                     "pi",
					NameBase:                "pi",
					MaxOccurrencesPerReport: 1,
				},
			},
		},
		{
			name: "untrimmed base",
			descriptors: []Descriptor{{
				Key:                     "pi",
				NameBase:                " Pi ",
				MaxOccurrencesPerReport: 1,
			}},
		},
		{
			name: "unsafe base",
			descriptors: []Descriptor{{
				Key:                     "pi",
				NameBase:                "Pi\nAgent",
				MaxOccurrencesPerReport: 1,
			}},
		},
		{
			name: "oversize base",
			descriptors: []Descriptor{{
				Key:                     "pi",
				NameBase:                strings.Repeat("p", MaxNameBaseBytes+1),
				MaxOccurrencesPerReport: 1,
			}},
		},
		{
			name: "non-positive bound",
			descriptors: []Descriptor{{
				Key:                     "pi",
				NameBase:                "Pi",
				MaxOccurrencesPerReport: 0,
			}},
		},
		{
			name: "complete report bound",
			descriptors: []Descriptor{
				{
					Key:                     "first",
					NameBase:                "First",
					MaxOccurrencesPerReport: 31,
				},
				{
					Key:                     "second",
					NameBase:                "Second",
					MaxOccurrencesPerReport: 2,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewVocabulary(test.descriptors...); !errors.Is(err, ErrInvalidVocabulary) {
				t.Fatalf("NewVocabulary error = %v, want invalid vocabulary", err)
			}
		})
	}
}

func TestAgentKeysAndNamesHaveOneCanonicalIdentity(t *testing.T) {
	if !ValidAdapterKey("deepseek-harness") || ValidAdapterKey("DeepSeek") || ValidAdapterKey("-pi") {
		t.Fatal("adapter key grammar did not preserve lowercase stable identity")
	}
	if !ValidOccurrenceKey("install.default_2") || ValidOccurrenceKey("Default") || ValidOccurrenceKey(".default") {
		t.Fatal("occurrence key grammar did not preserve adapter-local stable identity")
	}

	firstName, firstKey, err := NameForOrdinal("Pi", 1)
	if err != nil {
		t.Fatalf("allocate first Pi name: %v", err)
	}
	secondName, secondKey, err := NameForOrdinal("Pi", 2)
	if err != nil {
		t.Fatalf("allocate second Pi name: %v", err)
	}
	fullwidthKey, err := NormalizeName("ＰＩ")
	if err != nil {
		t.Fatalf("normalize fullwidth Agent name: %v", err)
	}
	if firstName != "Pi" || firstKey != "pi" || fullwidthKey != firstKey {
		t.Fatalf("first name/key = %q/%q, fullwidth key = %q", firstName, firstKey, fullwidthKey)
	}
	if secondName != "Pi 2" || secondKey != "pi 2" {
		t.Fatalf("second name/key = %q/%q", secondName, secondKey)
	}
	if _, _, err := NameForOrdinal("Pi", 0); !errors.Is(err, ErrInvalidAgentName) {
		t.Fatalf("zero ordinal error = %v", err)
	}
}

func TestAvatarAndInventoryRemainStableAfterAgentRemoval(t *testing.T) {
	const agentID = "b95b20be-ae3d-4c29-a4dc-c073363a53d8"
	first, err := AvatarIndex(agentID)
	if err != nil {
		t.Fatalf("derive Agent avatar: %v", err)
	}
	second, err := AvatarIndex("B95B20BE-AE3D-4C29-A4DC-C073363A53D8")
	if err != nil || second != first || first < 0 || first >= AvatarPresetCount {
		t.Fatalf("avatar indexes = %d/%d, error = %v", first, second, err)
	}

	lastActive := time.Date(2026, time.August, 21, 12, 30, 0, 0, time.UTC)
	record, err := ProjectInventory(Agent{
		AgentID:     agentID,
		MachineID:   "machine-1",
		OwnerUserID: "member-1",
		Name:        "Pi",
		Lifecycle:   LifecycleRemoved,
	}, "Member owner", Presence{
		Online:       true,
		LastActiveAt: &lastActive,
	})
	if err != nil {
		t.Fatalf("project removed Agent inventory: %v", err)
	}
	if record.AvatarIndex != first || record.Online || record.LastActiveAt == nil || record.Name != "Pi" {
		t.Fatalf("removed Agent inventory = %#v", record)
	}
}
