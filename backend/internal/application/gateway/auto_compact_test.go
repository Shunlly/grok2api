package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func bigResponsesBody(turns int, outputBytes int) []byte {
	items := []any{map[string]any{"type": "message", "role": "developer", "content": "dev rules"}}
	for i := 0; i < turns; i++ {
		items = append(items,
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "question " + string(rune('A'+i%26))}}},
			map[string]any{"type": "function_call", "call_id": "c" + string(rune('a'+i%26)) + string(rune('0'+i/26)), "name": "read_file", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": "c" + string(rune('a'+i%26)) + string(rune('0'+i/26)), "output": strings.Repeat("x y ", outputBytes/4)},
			map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "answer"}}},
		)
	}
	body, _ := json.Marshal(map[string]any{"model": "m", "instructions": "sys", "input": items})
	return body
}

func checkPairs(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var top struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatal(err)
	}
	calls := map[string]bool{}
	for _, it := range top.Input {
		if it["type"] == "function_call" {
			calls[it["call_id"].(string)] = true
		}
		if it["type"] == "function_call_output" && !calls[it["call_id"].(string)] {
			t.Fatalf("orphan output %v", it["call_id"])
		}
	}
	return top.Input
}

func TestAutoCompactSummarizesAndCaches(t *testing.T) {
	body := bigResponsesBody(20, 20000) // ~100k tokens
	cache := newSummaryCache(16)
	var calls atomic.Int32
	summarize := func(ctx context.Context, transcript string) (string, error) {
		calls.Add(1)
		if !strings.Contains(transcript, "question A") {
			t.Fatalf("transcript missing history")
		}
		return "<summary>1. Primary Request: test</summary>", nil
	}
	cfg := AutoCompactConfig{Enabled: true}
	out, st, ok := autoCompactBody(context.Background(), body, "m", 100000, cfg, cache, summarize)
	if !ok || st.Fallback || st.AfterTokens >= st.Threshold || st.BeforeTokens <= st.Threshold {
		t.Fatalf("unexpected stats %+v ok=%v", st, ok)
	}
	items := checkPairs(t, out)
	if items[0]["role"] != "developer" {
		t.Fatalf("system item not kept first: %v", items[0])
	}
	if !strings.Contains(string(out), autoCompactPreamble[:40]) || !strings.Contains(string(out), "Primary Request: test") {
		t.Fatal("summary missing")
	}
	if !strings.Contains(string(out), "question T") {
		t.Fatal("latest user turn missing")
	}
	// Codex resends the full history plus a new turn: summary is reused.
	var top map[string]any
	_ = json.Unmarshal(body, &top)
	top["input"] = append(top["input"].([]any), map[string]any{"type": "message", "role": "user", "content": "next"})
	body2, _ := json.Marshal(top)
	_, st2, ok2 := autoCompactBody(context.Background(), body2, "m", 100000, cfg, cache, summarize)
	if !ok2 || !st2.CacheHit || calls.Load() != 1 {
		t.Fatalf("expected cache hit, calls=%d st=%+v", calls.Load(), st2)
	}
}

func TestAutoCompactBelowThresholdUntouched(t *testing.T) {
	body := bigResponsesBody(2, 100)
	_, _, ok := autoCompactBody(context.Background(), body, "m", 256000, AutoCompactConfig{Enabled: true}, newSummaryCache(4), nil)
	if ok {
		t.Fatal("should not compact")
	}
}

func TestAutoCompactFallbackOnSummaryError(t *testing.T) {
	body := bigResponsesBody(20, 20000)
	out, st, ok := autoCompactBody(context.Background(), body, "m", 100000, AutoCompactConfig{Enabled: true}, newSummaryCache(4), func(context.Context, string) (string, error) {
		return "", errors.New("boom")
	})
	if !ok || !st.Fallback || !strings.Contains(string(out), "automatic summarization failed") {
		t.Fatalf("fallback not applied: %+v", st)
	}
	checkPairs(t, out)
}

func TestAutoCompactSingleLongTurnCutsInside(t *testing.T) {
	items := []any{map[string]any{"role": "user", "content": "do the task"}}
	for i := 0; i < 40; i++ {
		id := "t" + strings.Repeat("i", i+1)
		items = append(items,
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "exec_command", "arguments": "{}"}}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": strings.Repeat("ab ", 4000)},
		)
	}
	body, _ := json.Marshal(map[string]any{"model": "m", "messages": items})
	out, st, ok := autoCompactBody(context.Background(), body, "m", 100000, AutoCompactConfig{Enabled: true}, newSummaryCache(4), func(context.Context, string) (string, error) { return "sum", nil })
	if !ok || st.AfterTokens > st.Threshold {
		t.Fatalf("not compacted: %+v", st)
	}
	var top struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.Unmarshal(out, &top)
	if top.Messages[1]["content"] != "do the task" {
		t.Fatalf("pinned user message missing: %v", top.Messages[1])
	}
	if top.Messages[2]["role"] == "tool" {
		t.Fatal("cut starts on orphan tool message")
	}
}

func TestSlimmingCapsLatestToolOutputs(t *testing.T) {
	items := []any{
		map[string]any{"type": "message", "role": "user", "content": "hi"},
		map[string]any{"type": "function_call", "call_id": "s", "name": "exec_command", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "s", "output": strings.Repeat("中", 30000)},
		map[string]any{"type": "function_call", "call_id": "r", "name": "read_file", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "r", "output": strings.Repeat("a", 50000)},
	}
	body, _ := json.Marshal(map[string]any{"input": items})
	out, st := slimRequestBody(body, RequestSlimmingConfig{Enabled: true, KeepToolOutputTurns: 2})
	if st.OutputsTrimmed != 2 {
		t.Fatalf("trimmed=%d", st.OutputsTrimmed)
	}
	var top struct {
		Input []map[string]any `json:"input"`
	}
	_ = json.Unmarshal(out, &top)
	shell := top.Input[2]["output"].(string)
	if n := len([]rune(shell)); n > 20200 || !strings.Contains(shell, "output truncated") {
		t.Fatalf("shell output len %d", n)
	}
	if n := len(top.Input[4]["output"].(string)); n > 40200 {
		t.Fatalf("general output len %d", n)
	}
}
