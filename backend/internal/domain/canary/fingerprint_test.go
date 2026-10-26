package canary

import "testing"

func TestJudgeFingerprint(t *testing.T) {
	policy := FingerprintPolicy{MinConfidence: 0.6, FamilyTolerant: true, SuspectAfter: 2}
	cases := []struct {
		top, fam string
		conf     float64
		valid    int
		status   string
		verdict  string
	}{
		{"grok-4.7", "grok", 0.9, 3, StatusPass, VerdictMatch},
		{"grok-4.5", "grok", 0.9, 3, StatusPass, VerdictFamily},
		{"gpt-4o", "gpt", 0.9, 3, StatusFail, VerdictMismatch},
		{"gpt-4o", "gpt", 0.3, 3, StatusSkipped, VerdictInconclusive},
		{"", "", 0, 0, StatusSkipped, VerdictInsufficient},
	}
	for _, c := range cases {
		r := FingerprintResult{Model: "grok-4.7", TopModel: c.top, TopFamily: c.fam, Confidence: c.conf, ValidOutputs: c.valid}
		JudgeFingerprint(&r, "grok", policy)
		if r.Status != c.status || r.Verdict != c.verdict {
			t.Fatalf("%+v -> %s/%s", c, r.Status, r.Verdict)
		}
	}
	strict := FingerprintResult{Model: "grok-4.7", TopModel: "grok-4.5", TopFamily: "grok", Confidence: 0.9, ValidOutputs: 3}
	JudgeFingerprint(&strict, "grok", FingerprintPolicy{MinConfidence: 0.6})
	if strict.Verdict != VerdictMismatch {
		t.Fatalf("family intolerant: %s", strict.Verdict)
	}
}

func TestFingerprintStreakAndPool(t *testing.T) {
	h := []FingerprintResult{{Status: StatusFail}, {Status: StatusSkipped}, {Status: StatusFail}, {Status: StatusPass}, {Status: StatusFail}}
	if got := ConsecutiveMismatches(h); got != 2 {
		t.Fatalf("streak %d", got)
	}
	pool := []FingerprintResult{{AccountID: 1, Model: "m", Status: StatusFail}, {AccountID: 2, Model: "m", Status: StatusPass}, {AccountID: 3, Model: "x", Status: StatusFail}}
	if !FingerprintPoolPasses(pool, 1, "m") {
		t.Fatal("pool should pass")
	}
	if FingerprintPoolPasses(pool, 2, "m") {
		t.Fatal("pool of failing account 1 should not pass")
	}
	if FingerprintPoolPasses(pool[:1], 1, "m") {
		t.Fatal("empty pool must not pass")
	}
}
