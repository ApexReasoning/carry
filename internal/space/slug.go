package space

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/secure/precis"
)

const (
	MaxSpaceNameCodePoints  = 80
	MaxSpaceSlugCodePoints  = 32
	MaxSpaceSlugSuffix      = 9999
	SpaceSlugUnicodeVersion = "15.0.0"
)

var (
	ErrSpaceNameRequired      = errors.New("Space name must contain at least one letter or number")
	ErrSpaceNameTooLong       = errors.New("Space name must be at most 80 characters")
	ErrSpaceNameHasControl    = errors.New("Space name must not contain invisible or control characters")
	ErrSpaceSlugUnsupported   = errors.New("Space URL supports letters, numbers, spaces, underscore, and hyphen")
	ErrSpaceSlugMixedScripts  = errors.New("Space URL must use one writing system or a supported Latin and CJK combination")
	ErrSpaceSlugTooLong       = errors.New("Space URL must be at most 32 characters")
	ErrSpaceSlugUnstable      = errors.New("Space name cannot produce a stable Space URL")
	ErrSpaceSlugSuffixInvalid = errors.New("Space URL suffix must be between 2 and 9999")
)

// NormalizeSpaceName returns the stored display name and immutable URL slug.
func NormalizeSpaceName(value string, suffix int) (string, string, error) {
	if !utf8.ValidString(value) {
		return "", "", ErrSpaceSlugUnsupported
	}
	name := strings.TrimSpace(value)
	if name == "" {
		return "", "", ErrSpaceNameRequired
	}
	if utf8.RuneCountInString(name) > MaxSpaceNameCodePoints {
		return "", "", ErrSpaceNameTooLong
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", "", ErrSpaceNameHasControl
		}
	}
	if suffix != 0 && (suffix < 2 || suffix > MaxSpaceSlugSuffix) {
		return "", "", ErrSpaceSlugSuffixInvalid
	}

	prepared := collapseSlugSeparators(name)
	slug, err := precis.UsernameCaseMapped.String(prepared)
	if err != nil {
		return "", "", ErrSpaceSlugUnsupported
	}
	stable, err := precis.UsernameCaseMapped.String(slug)
	if err != nil || stable != slug {
		return "", "", ErrSpaceSlugUnstable
	}
	slug = collapseSlugSeparators(slug)
	if err := validateSlugGrammar(slug); err != nil {
		return "", "", err
	}
	if !validSlugScripts(slug) {
		return "", "", ErrSpaceSlugMixedScripts
	}
	if suffix != 0 {
		slug += "-" + strconv.Itoa(suffix)
	}
	if utf8.RuneCountInString(slug) > MaxSpaceSlugCodePoints {
		return "", "", ErrSpaceSlugTooLong
	}
	return name, slug, nil
}

func collapseSlugSeparators(value string) string {
	var result strings.Builder
	separator := false
	for _, r := range value {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			separator = result.Len() > 0
			continue
		}
		if separator {
			result.WriteByte('-')
			separator = false
		}
		result.WriteRune(r)
	}
	return result.String()
}

func validateSlugGrammar(slug string) error {
	if slug == "" {
		return ErrSpaceNameRequired
	}
	tokenHasBase := false
	var previous rune
	for _, r := range slug {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			tokenHasBase = true
		case unicode.IsMark(r):
			if !tokenHasBase {
				return ErrSpaceSlugUnsupported
			}
		case r == '-':
			if !tokenHasBase {
				return ErrSpaceSlugUnsupported
			}
			tokenHasBase = false
		default:
			return ErrSpaceSlugUnsupported
		}
		previous = r
	}
	if !unicode.IsLetter(previous) && !unicode.IsNumber(previous) {
		return ErrSpaceSlugUnsupported
	}
	return nil
}

func validSlugScripts(slug string) bool {
	scripts := make(map[string]struct{})
	for _, r := range slug {
		if !unicode.IsLetter(r) {
			continue
		}
		for name, table := range unicode.Scripts {
			if name == "Common" || name == "Inherited" || !unicode.Is(table, r) {
				continue
			}
			scripts[name] = struct{}{}
		}
	}
	if len(scripts) <= 1 {
		return true
	}

	for name := range scripts {
		switch name {
		case "Latin", "Han", "Hiragana", "Katakana", "Hangul", "Bopomofo":
		default:
			return false
		}
	}

	writingSystems := 0
	_, hasHiragana := scripts["Hiragana"]
	_, hasKatakana := scripts["Katakana"]
	if hasHiragana || hasKatakana {
		writingSystems++
	}
	if _, hasHangul := scripts["Hangul"]; hasHangul {
		writingSystems++
	}
	if _, hasBopomofo := scripts["Bopomofo"]; hasBopomofo {
		writingSystems++
	}
	return writingSystems <= 1
}
