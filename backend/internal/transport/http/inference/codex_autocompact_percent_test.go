package inference

import "testing"

func TestCodexAutoCompactTokenLimitPercentUsesUpstreamPercent(t *testing.T) {
	if got := *codexAutoCompactTokenLimitPercent(256000, 80); got != 204800 {
		t.Fatalf("80%% of 256k = %d", got)
	}
	if got := *codexAutoCompactTokenLimitPercent(256000, 0); got != 217600 {
		t.Fatalf("default 85%% of 256k = %d", got)
	}
	if got := *codexAutoCompactTokenLimitPercent(500000, 80); got != 300000 {
		t.Fatalf("cap = %d", got)
	}
}
