package web

import (
	"strings"
	"unicode"
)

// Placeholder web_search queries observed on degraded Grok Web accounts
// (chenyme/grok2api#1103). Real searches use topic-specific queries.
var degradedWebSearchPlaceholders = []string{
	"current information and recent sources",
	"best expert recommendations and evidence",
	"latest updates and authoritative references",
}

// IsDegradedWebSearchQuery reports whether a hosted web_search query is one of
// the known degraded-account placeholders.
func IsDegradedWebSearchQuery(query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return false
	}
	for _, placeholder := range degradedWebSearchPlaceholders {
		if normalized == placeholder {
			return true
		}
	}
	return false
}

// HostedSearchCallsDegraded reports whether any completed/in-progress hosted
// search used a degraded placeholder query.
func HostedSearchCallsDegraded(calls []hostedSearchCall) bool {
	for _, call := range calls {
		if call.Kind == "web_search" && IsDegradedWebSearchQuery(call.Query) {
			return true
		}
	}
	return false
}

var honeypotFakeLinkMarkers = []string{
	"huggingface.co/papers/trending",
	"arxiv.org/list/cs.AI/recent",
	"paperswithcode.com/latest",
}

// LooksLikeCastleHoneypot detects the Castle-missing honeypot: a burst of
// unrelated English dictionary words plus fixed fake academic links
// (chenyme/grok2api#1102). Prefer failing closed over forwarding gibberish.
func LooksLikeCastleHoneypot(text string, searchURLs []string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	fakeLinks := 0
	for _, raw := range searchURLs {
		lower := strings.ToLower(raw)
		for _, marker := range honeypotFakeLinkMarkers {
			if strings.Contains(lower, marker) {
				fakeLinks++
				break
			}
		}
	}
	words := strings.FieldsFunc(trimmed, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	if len(words) < 24 {
		return fakeLinks >= 2
	}
	shortEnglish := 0
	for _, word := range words {
		if len(word) < 2 || len(word) > 12 {
			continue
		}
		ascii := true
		for _, r := range word {
			if r > unicode.MaxASCII || !unicode.IsLetter(r) {
				ascii = false
				break
			}
		}
		if ascii {
			shortEnglish++
		}
	}
	ratio := float64(shortEnglish) / float64(len(words))
	if fakeLinks >= 2 && ratio >= 0.75 {
		return true
	}
	if fakeLinks >= 1 && ratio >= 0.9 && len(words) >= 40 {
		return true
	}
	// Pure word-salad with no punctuation-heavy prose and almost no long tokens.
	if fakeLinks == 0 && ratio >= 0.92 && len(words) >= 60 {
		long := 0
		for _, word := range words {
			if len(word) >= 10 {
				long++
			}
		}
		if float64(long)/float64(len(words)) < 0.05 {
			return true
		}
	}
	return false
}
