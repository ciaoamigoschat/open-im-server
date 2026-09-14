package moderation

import (
	"net"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	domainPattern       = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s]+|(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|it|me|io|co|info|biz)(?:/[^\s]*)?`)
	phonePattern        = regexp.MustCompile(`(?i)(?:\+?39[\s.-]?)?(?:3\d{2})[\s.-]?\d{3}[\s.-]?\d{3,4}`)
	solicitationPattern = regexp.MustCompile(`(?i)\b(?:scrivimi|contattami|chiamami|whats\s*app|telegram|wa\.me|t\.me)\b`)
)

func matchBannedTerms(text NormalizedText, terms []BannedTerm, fuzzy FuzzyConfig) ([]string, int, Action, bool) {
	matched := make([]string, 0)
	score := 0
	strongest := ActionAllow
	fuzzyMatched := false
	tokens := strings.Fields(text.NormalizedText)

	for _, term := range terms {
		if !term.Enabled || strings.TrimSpace(term.Term) == "" {
			continue
		}
		normalizedTerm := NormalizeText(term.Term)
		exact := containsTokenSequence(text.NormalizedText, normalizedTerm.NormalizedText)
		contains := exact || (!term.ExactOnly && strings.Contains(text.CompactText, normalizedTerm.CompactText))
		isFuzzy := false
		if !contains && fuzzy.Enabled && term.Fuzzy && utf8.RuneCountInString(normalizedTerm.CompactText) >= fuzzy.MinWordLength {
			isFuzzy = fuzzyTokenMatch(tokens, normalizedTerm.CompactText, fuzzy)
		}
		if !contains && !isFuzzy {
			continue
		}
		matched = appendUnique(matched, term.Term)
		score += term.Severity
		strongest = strongerAction(strongest, term.Action)
		fuzzyMatched = fuzzyMatched || isFuzzy
	}
	return matched, score, strongest, fuzzyMatched
}

func containsTokenSequence(text, term string) bool {
	if term == "" {
		return false
	}
	return strings.Contains(" "+text+" ", " "+term+" ")
}

func fuzzyTokenMatch(tokens []string, term string, cfg FuzzyConfig) bool {
	length := utf8.RuneCountInString(term)
	maxDistance := cfg.MaxDistanceLong
	if length <= 7 {
		maxDistance = cfg.MaxDistanceMedium
	}
	for _, token := range tokens {
		if difference := utf8.RuneCountInString(token) - length; difference > maxDistance || difference < -maxDistance {
			continue
		}
		if levenshtein(token, term, maxDistance) <= maxDistance {
			return true
		}
	}
	return false
}

func levenshtein(a, b string, limit int) int {
	ar, br := []rune(a), []rune(b)
	previous := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ra := range ar {
		current := make([]int, len(br)+1)
		current[0] = i + 1
		rowMin := current[0]
		for j, rb := range br {
			cost := 0
			if ra != rb {
				cost = 1
			}
			current[j+1] = min3(current[j]+1, previous[j+1]+1, previous[j]+cost)
			if current[j+1] < rowMin {
				rowMin = current[j+1]
			}
		}
		if rowMin > limit {
			return limit + 1
		}
		previous = current
	}
	return previous[len(br)]
}

func extractDomains(text string) []string {
	matches := domainPattern.FindAllString(text, -1)
	domains := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := strings.TrimRight(strings.TrimSpace(match), ".,;:!?)]}\"")
		candidate = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(candidate), "https://"), "http://")
		candidate = strings.TrimPrefix(candidate, "www.")
		host := strings.Split(candidate, "/")[0]
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		if host != "" {
			domains = appendUnique(domains, host)
		}
	}
	return domains
}

func matchBannedDomains(found []string, banned []BannedDomain) ([]string, int, Action) {
	matched := make([]string, 0)
	score := 0
	action := ActionAllow
	for _, candidate := range found {
		for _, item := range banned {
			domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(item.Domain)), "www.")
			if !item.Enabled || domain == "" || (candidate != domain && !strings.HasSuffix(candidate, "."+domain)) {
				continue
			}
			matched = appendUnique(matched, domain)
			score += item.Severity
			action = strongerAction(action, item.Action)
		}
	}
	return matched, score, action
}

func hasPhoneSolicitation(text string) bool {
	return phonePattern.MatchString(text) && solicitationPattern.MatchString(text)
}

func strongerAction(a, b Action) Action {
	weight := map[Action]int{ActionAllow: 0, ActionAllowLog: 1, ActionReview: 2, ActionBlock: 3, ActionMute: 4, ActionTempBan: 5}
	if weight[b] > weight[a] {
		return b
	}
	return a
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func min3(a, b, c int) int {
	if a < b && a < c {
		return a
	}
	if b < c {
		return b
	}
	return c
}
