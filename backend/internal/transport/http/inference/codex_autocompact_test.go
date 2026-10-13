package inference

import "testing"

func TestCodexAutoCompactTokenLimit(t *testing.T) {
	cases := map[int]int{200000: 170000, 256000: 217600, 500000: 300000, 2000000: 300000}
	for window, want := range cases {
		got := codexAutoCompactTokenLimit(window)
		if got == nil || *got != want {
			t.Fatalf("window %d: got %v want %d", window, got, want)
		}
	}
	if codexAutoCompactTokenLimit(0) != nil {
		t.Fatal("unknown window must leave Codex default")
	}
}
