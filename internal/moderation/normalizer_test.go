package moderation

import (
	"encoding/json"
	"testing"
)

func TestDocumentedConfigPayload(t *testing.T) {
	payload := []byte(`{
		"enabled":true,
		"rateLimit":{"messages10Sec":8,"messages60Sec":25,"messages5Min":80},
		"duplicate":{"windowSeconds":300,"warningCount":4,"blockCount":6},
		"recipients":{"windowSeconds":60,"warningCount":10,"blockCount":20},
		"fuzzyMatching":{"enabled":true,"minWordLength":5,"maxDistanceMedium":1,"maxDistanceLong":2},
		"score":{"flood10Sec":25,"flood60Sec":40,"suspiciousLink":20,"phoneSolicitation":25},
		"thresholds":{"log":30,"block":50,"mute5Minutes":70,"mute1Hour":90,"mute24Hours":120,"review":150}
	}`)
	var config Config
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	if config.FailurePolicy != FailOpen || config.Thresholds.TempBanSeconds == 0 || len(config.Duplicate.Levels) != 3 || len(config.Recipients.Levels) != 3 {
		t.Fatalf("defaults not applied: %#v", config)
	}
}

func TestNormalizeObfuscatedWords(t *testing.T) {
	tests := []string{"V.I.A.G.R.A", "v i a g r a", "v1agra", "vi@gr@", "v\u200biagra", "vìàgrà"}
	for _, input := range tests {
		got := NormalizeText(input)
		if got.CompactText != "viagra" {
			t.Fatalf("NormalizeText(%q).CompactText = %q, want viagra", input, got.CompactText)
		}
	}
}

func TestNormalizePreservesEmojiBoundary(t *testing.T) {
	got := NormalizeText("ciao🙂mondo")
	if got.NormalizedText != "ciao mondo" || got.CompactText != "ciaomondo" {
		t.Fatalf("unexpected normalization: %#v", got)
	}
}

func TestBannedTermMatchingAndFalsePositive(t *testing.T) {
	terms := []BannedTerm{{ID: "1", Term: "viagra", Severity: 60, Enabled: true, Fuzzy: true, Action: ActionBlock}}
	matched, _, _, fuzzy := matchBannedTerms(NormalizeText("V.I.A.G.R.A"), terms, DefaultConfig().FuzzyMatching)
	if len(matched) != 1 || fuzzy {
		t.Fatalf("expected compact exact match, got matched=%v fuzzy=%v", matched, fuzzy)
	}
	matched, _, _, _ = matchBannedTerms(NormalizeText("viaggio in treno"), terms, DefaultConfig().FuzzyMatching)
	if len(matched) != 0 {
		t.Fatalf("unexpected false positive: %v", matched)
	}
}

func TestFuzzyMatching(t *testing.T) {
	terms := []BannedTerm{{ID: "1", Term: "telegram", Severity: 40, Enabled: true, Fuzzy: true}}
	matched, _, _, fuzzy := matchBannedTerms(NormalizeText("scrivimi su telegran"), terms, DefaultConfig().FuzzyMatching)
	if len(matched) != 1 || !fuzzy {
		t.Fatalf("expected fuzzy match, got matched=%v fuzzy=%v", matched, fuzzy)
	}
}

func TestActionThresholds(t *testing.T) {
	thresholds := DefaultConfig().Thresholds
	tests := []struct {
		score int
		want  Action
	}{{20, ActionAllow}, {40, ActionAllowLog}, {60, ActionBlock}, {95, ActionMute}, {160, ActionTempBan}}
	for _, test := range tests {
		got, _ := actionForScore(test.score, thresholds)
		if got != test.want {
			t.Fatalf("actionForScore(%d) = %s, want %s", test.score, got, test.want)
		}
	}
}
