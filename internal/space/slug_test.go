package space

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/secure/precis"
)

func TestSpaceSlugUnicodeProfileIsPinned(t *testing.T) {
	t.Parallel()

	if unicode.Version != SpaceSlugUnicodeVersion {
		t.Fatalf("stdlib Unicode version = %s, want %s", unicode.Version, SpaceSlugUnicodeVersion)
	}
	if precis.UnicodeVersion != SpaceSlugUnicodeVersion {
		t.Fatalf("PRECIS Unicode version = %s, want %s", precis.UnicodeVersion, SpaceSlugUnicodeVersion)
	}
}

func TestNormalizeSpaceNameDerivesReadableUnicodeSlug(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		value    string
		suffix   int
		wantName string
		wantSlug string
	}{
		{
			name:     "ASCII case",
			value:    "  Acme Platform  ",
			wantName: "Acme Platform",
			wantSlug: "acme-platform",
		},
		{
			name:     "full width",
			value:    "ＡＣＭＥ＿研究",
			wantName: "ＡＣＭＥ＿研究",
			wantSlug: "acme-研究",
		},
		{
			name:     "canonical composition",
			value:    "Cafe\u0301",
			wantName: "Café",
			wantSlug: "café",
		},
		{
			name:     "Chinese",
			value:    "碳纤维小组",
			wantName: "碳纤维小组",
			wantSlug: "碳纤维小组",
		},
		{
			name:     "Latin Japanese",
			value:    "Carry 日本語チーム",
			wantName: "Carry 日本語チーム",
			wantSlug: "carry-日本語チーム",
		},
		{
			name:     "Latin Korean",
			value:    "AI 한국어",
			wantName: "AI 한국어",
			wantSlug: "ai-한국어",
		},
		{
			name:     "Latin Bopomofo",
			value:    "Carry 漢ㄅ",
			wantName: "Carry 漢ㄅ",
			wantSlug: "carry-漢ㄅ",
		},
		{
			name:     "digits",
			value:    "2024",
			wantName: "2024",
			wantSlug: "2024",
		},
		{
			name:     "collapsed separators",
			value:    "alpha _ - beta",
			wantName: "alpha _ - beta",
			wantSlug: "alpha-beta",
		},
		{
			name:     "suggested suffix",
			value:    "Acme",
			suffix:   2,
			wantName: "Acme",
			wantSlug: "acme-2",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			name, slug, err := NormalizeSpaceName(test.value, test.suffix)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if name != test.wantName || slug != test.wantSlug {
				t.Fatalf("normalized = %q / %q, want %q / %q", name, slug, test.wantName, test.wantSlug)
			}
		})
	}
}

func TestNormalizeSpaceNameRejectsUnsafeOrUnrecoverableSlug(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		value  string
		suffix int
		want   error
	}{
		{
			name:  "empty",
			value: " --- ",
			want:  ErrSpaceNameRequired,
		},
		{
			name:  "control",
			value: "Acme\nTeam",
			want:  ErrSpaceNameHasControl,
		},
		{
			name:  "format",
			value: "a\u200db",
			want:  ErrSpaceNameHasControl,
		},
		{
			name:  "path delimiter",
			value: "Acme/Team",
			want:  ErrSpaceSlugUnsupported,
		},
		{
			name:  "dot",
			value: "..",
			want:  ErrSpaceSlugUnsupported,
		},
		{
			name:  "symbol",
			value: "R&D",
			want:  ErrSpaceSlugUnsupported,
		},
		{
			name:  "emoji",
			value: "Team 👋",
			want:  ErrSpaceSlugUnsupported,
		},
		{
			name:  "Latin Cyrillic",
			value: "cаrry",
			want:  ErrSpaceSlugMixedScripts,
		},
		{
			name:  "Greek Cyrillic",
			value: "Ελλкир",
			want:  ErrSpaceSlugMixedScripts,
		},
		{
			name:  "Japanese Korean",
			value: "Carry ひ한",
			want:  ErrSpaceSlugMixedScripts,
		},
		{
			name:  "Japanese Bopomofo",
			value: "Carry ひㄅ",
			want:  ErrSpaceSlugMixedScripts,
		},
		{
			name:  "Korean Bopomofo",
			value: "Carry 한ㄅ",
			want:  ErrSpaceSlugMixedScripts,
		},
		{
			name:  "slug too long",
			value: strings.Repeat("界", 33),
			want:  ErrSpaceSlugTooLong,
		},
		{
			name:  "name too long",
			value: strings.Repeat("a", 81),
			want:  ErrSpaceNameTooLong,
		},
		{
			name:   "suffix below range",
			value:  "Acme",
			suffix: 1,
			want:   ErrSpaceSlugSuffixInvalid,
		},
		{
			name:   "suffix above range",
			value:  "Acme",
			suffix: 10000,
			want:   ErrSpaceSlugSuffixInvalid,
		},
		{
			name:   "suffix has no room",
			value:  strings.Repeat("a", 32),
			suffix: 2,
			want:   ErrSpaceSlugTooLong,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := NormalizeSpaceName(test.value, test.suffix)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNormalizeSpaceNameAcceptsMaximumCJKSlug(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("界", MaxSpaceSlugCodePoints)
	_, slug, err := NormalizeSpaceName(value, 0)
	if err != nil {
		t.Fatalf("normalize maximum CJK slug: %v", err)
	}
	if slug != value {
		t.Fatalf("slug = %q", slug)
	}
}
