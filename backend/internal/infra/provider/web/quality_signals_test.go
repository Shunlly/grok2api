package web

import "testing"

func TestIsDegradedWebSearchQuery(t *testing.T) {
	for _, query := range []string{
		"current information and recent sources",
		"Best Expert Recommendations and Evidence",
		"latest updates and authoritative references",
	} {
		if !IsDegradedWebSearchQuery(query) {
			t.Fatalf("expected degraded placeholder %q", query)
		}
	}
	if IsDegradedWebSearchQuery("Rust 1.90 release notes") {
		t.Fatal("topic query must not be treated as placeholder")
	}
}

func TestLooksLikeCastleHoneypot(t *testing.T) {
	salad := stringsJoin(60, "alpha bravo charlie delta echo foxtrot golf hotel india")
	urls := []string{
		"https://huggingface.co/papers/trending",
		"https://arxiv.org/list/cs.AI/recent",
		"https://paperswithcode.com/latest",
	}
	if !LooksLikeCastleHoneypot(salad, urls) {
		t.Fatal("expected honeypot detection for word salad + fake links")
	}
	if LooksLikeCastleHoneypot("Rust 1.90 ships new edition polish and stabilizes several APIs.", []string{"https://blog.rust-lang.org/2025/01/01/Rust-1.90.0.html"}) {
		t.Fatal("real answer must not look like a honeypot")
	}
}

func stringsJoin(n int, phrase string) string {
	out := phrase
	for i := 1; i < n; i++ {
		out += " " + phrase
	}
	return out
}
