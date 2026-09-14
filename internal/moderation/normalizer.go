package moderation

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type NormalizedText struct {
	OriginalText   string `json:"originalText"`
	NormalizedText string `json:"normalizedText"`
	CompactText    string `json:"compactText"`
}

func NormalizeText(text string) NormalizedText {
	decomposed := norm.NFKD.String(strings.ToLower(text))
	var normalized strings.Builder
	var compact strings.Builder
	spacePending := false

	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) || isZeroWidth(r) {
			continue
		}
		if mapped, ok := leetRune(r); ok {
			compact.WriteRune(mapped)
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact.WriteRune(r)
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if spacePending && normalized.Len() > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(r)
			spacePending = false
		} else {
			spacePending = true
		}
	}

	return NormalizedText{
		OriginalText:   text,
		NormalizedText: normalized.String(),
		CompactText:    compact.String(),
	}
}

func isZeroWidth(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
		return true
	default:
		return false
	}
}

func leetRune(r rune) (rune, bool) {
	switch r {
	case '0':
		return 'o', true
	case '1', '!':
		return 'i', true
	case '3':
		return 'e', true
	case '4', '@':
		return 'a', true
	case '5', '$':
		return 's', true
	case '7':
		return 't', true
	default:
		return 0, false
	}
}
