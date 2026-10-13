package canary

import "testing"

func TestCheckAnswer(t *testing.T) {
	p := Prompts[0]
	if ok, _ := CheckAnswer(p, "work...\nFinal answer: 401"); !ok {
		t.Fatal("expected correct")
	}
	if ok, reason := CheckAnswer(p, "Final answer: 400"); ok || reason == "" {
		t.Fatal("expected wrong answer")
	}
	if ok, _ := CheckAnswer(p, "401"); ok {
		t.Fatal("missing final answer line must fail")
	}
	if ok, _ := CheckAnswer(p, "**Final Answer:** 1,401"); ok {
		t.Fatal("1401 is wrong")
	}
}

func TestCheckSVG(t *testing.T) {
	good := `<html><body><svg viewBox="0 0 200 200"><circle r="10" fill="red"><animateTransform attributeName="transform" type="rotate"/></circle><rect fill="blue"/><polygon fill="green"/></svg></body></html>`
	if ok, reason := CheckSVG(good); !ok {
		t.Fatal(reason)
	}
	if ok, _ := CheckSVG(`<svg><circle/></svg>`); ok {
		t.Fatal("incomplete svg must fail")
	}
	if ok, _ := CheckSVG(`<svg><circle/><rect/><polygon/>`); ok {
		t.Fatal("unterminated svg must fail")
	}
}

func TestScoreAndSuspect(t *testing.T) {
	healthy := Result{ReasoningTokens: 900, VisibleChars: 40}
	Score(&healthy, Prompts[0], true, "", 1000)
	if healthy.Status != StatusPass || healthy.Score < 90 {
		t.Fatalf("healthy = %#v", healthy)
	}
	lazy := Result{ReasoningTokens: 100, VisibleChars: 40}
	Score(&lazy, Prompts[0], true, "", 1000)
	if lazy.Status != StatusFail {
		t.Fatalf("10%% of pool reasoning must fail: %#v", lazy)
	}
	history := []Result{{Status: StatusFail}, {Status: StatusError}, {Status: StatusFail}, {Status: StatusSkipped}, {Status: StatusFail}, {Status: StatusPass}, {Status: StatusFail}}
	if got := ConsecutiveFails(history); got != 3 {
		t.Fatalf("consecutive = %d", got)
	}
	round := []Result{{AccountID: 1, Status: StatusFail}, {AccountID: 2, Status: StatusPass}, {AccountID: 3, Status: StatusError}}
	if !PoolPasses(round, 1) {
		t.Fatal("pool passes")
	}
	if PoolPasses([]Result{{AccountID: 1, Status: StatusFail}, {AccountID: 2, Status: StatusFail}}, 1) {
		t.Fatal("pool fails too: not suspect")
	}
	if PoolPasses([]Result{{AccountID: 1, Status: StatusFail}}, 1) {
		t.Fatal("single account cannot be compared")
	}
	if Median([]int64{0, 5, 1, 3}) != 3 || Median([]int64{4, 2}) != 3 {
		t.Fatal("median")
	}
}
