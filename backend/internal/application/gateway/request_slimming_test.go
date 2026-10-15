package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var slimCfg = RequestSlimmingConfig{Enabled: true, KeepImageTurns: 2, KeepToolOutputTurns: 2}

func fakeImage(n int) string { return "data:image/png;base64," + strings.Repeat("A", n) }

// codexLikePayload builds a Responses API body resembling a long Codex session.
func codexLikePayload(turns, imgBytes, toolBytes int) []byte {
	items := []any{map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "<permissions>"}}}}
	for t := 0; t < turns; t++ {
		items = append(items,
			map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": fmt.Sprintf("turn %d <b>", t)},
				map[string]any{"type": "input_image", "image_url": fakeImage(imgBytes)},
			}},
			map[string]any{"type": "reasoning", "id": fmt.Sprintf("rs_%d", t), "summary": []any{}, "encrypted_content": "gAAAA" + strings.Repeat("x", 200)},
			map[string]any{"type": "function_call", "call_id": fmt.Sprintf("call_%d", t), "name": "shell", "arguments": "{\"cmd\":\"ls\"}"},
			map[string]any{"type": "function_call_output", "call_id": fmt.Sprintf("call_%d", t), "output": strings.Repeat("é", toolBytes/2)},
			map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "done"}}},
		)
	}
	body, _ := json.Marshal(map[string]any{"model": "grok-4.7", "stream": true, "input": items, "prompt_cache_key": "abc"})
	return body
}

func TestSlimReplayLargeCodexPayload(t *testing.T) {
	body := codexLikePayload(30, 170<<10, 40<<10)
	if len(body) < 5<<20 {
		t.Fatalf("payload too small: %d", len(body))
	}
	out, st := slimRequestBody(body, slimCfg)
	if !json.Valid(out) {
		t.Fatal("invalid json")
	}
	if st.SlimmedBytes >= 3<<20 || st.ImagesRemoved != 28 || st.OutputsTrimmed != 28 {
		t.Fatalf("unexpected stats %+v", st)
	}
	var orig, slim struct {
		Input []map[string]any `json:"input"`
	}
	_ = json.Unmarshal(body, &orig)
	_ = json.Unmarshal(out, &slim)
	if len(orig.Input) != len(slim.Input) {
		t.Fatal("item count changed")
	}
	for i := range orig.Input {
		if orig.Input[i]["type"] != slim.Input[i]["type"] || orig.Input[i]["call_id"] != slim.Input[i]["call_id"] {
			t.Fatalf("item %d order/call_id changed", i)
		}
		if orig.Input[i]["type"] == "reasoning" && orig.Input[i]["encrypted_content"] != slim.Input[i]["encrypted_content"] {
			t.Fatal("reasoning changed")
		}
	}
	// last two turns keep images and outputs untouched
	n := len(slim.Input)
	for _, i := range []int{n - 5, n - 10} {
		content := slim.Input[i]["content"].([]any)
		if content[1].(map[string]any)["type"] != "input_image" {
			t.Fatalf("recent image removed at %d", i)
		}
	}
	if slim.Input[n-2]["output"] != orig.Input[n-2]["output"] || slim.Input[n-7]["output"] != orig.Input[n-7]["output"] {
		t.Fatal("recent tool output trimmed")
	}
	old := slim.Input[1]["content"].([]any)[1].(map[string]any)
	if old["type"] != "input_text" || old["text"] != slimImagePlaceholder {
		t.Fatalf("old image not replaced: %v", old)
	}
	if !strings.Contains(slim.Input[4]["output"].(string), "字节已省略") {
		t.Fatal("old output not trimmed")
	}
	if !json.Valid([]byte(fmt.Sprintf("%q", slim.Input[4]["output"]))) {
		t.Fatal("bad utf8")
	}
	if !bytes.Contains(out, []byte("<b>")) {
		t.Fatal("html escaped")
	}
	// deterministic & prefix stable when a new turn is appended later
	out2, _ := slimRequestBody(body, slimCfg)
	if !bytes.Equal(out, out2) {
		t.Fatal("not deterministic")
	}
	// idempotent
	out3, st3 := slimRequestBody(out, slimCfg)
	if st3.changed() || !bytes.Equal(out3, out) {
		t.Fatal("not idempotent")
	}
}

func TestSlimNoopForSmallAndDisabled(t *testing.T) {
	body := codexLikePayload(2, 100, 100)
	out, st := slimRequestBody(body, slimCfg)
	if st.changed() || !bytes.Equal(out, body) {
		t.Fatal("small request should be untouched")
	}
	big := codexLikePayload(5, 1000, 100)
	out, st = slimRequestBody(big, RequestSlimmingConfig{Enabled: false})
	if st.changed() || !bytes.Equal(out, big) {
		t.Fatal("disabled should be untouched")
	}
	if out, _ := slimRequestBody([]byte("not json"), slimCfg); string(out) != "not json" {
		t.Fatal("bad json changed")
	}
}

func TestSlimChatAndAnthropic(t *testing.T) {
	chat := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:x"}}]},{"role":"tool","tool_call_id":"c1","content":"` + strings.Repeat("a", 20000) + `"},{"role":"user","content":"b"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:y"}}]}]}`)
	out, st := slimRequestBody(chat, slimCfg)
	if st.ImagesRemoved != 1 || st.OutputsTrimmed != 1 || !bytes.Contains(out, []byte("data:y")) || bytes.Contains(out, []byte("data:x")) {
		t.Fatalf("chat %+v %s", st, out[:200])
	}
	anth := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"OLD"}}]},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"x","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"base64","data":"TR"}}]}]},{"role":"user","content":"q2"},{"role":"user","content":"q3"}]}`)
	out, st = slimRequestBody(anth, slimCfg)
	if st.ImagesRemoved != 2 || bytes.Contains(out, []byte("OLD")) || bytes.Contains(out, []byte("\"TR\"")) || !bytes.Contains(out, []byte(`"tool_use_id":"t1"`)) {
		t.Fatalf("anthropic %+v %s", st, out)
	}
	// tool_result-only user message does not start a turn: keep its image when recent
	anth2 := []byte(`{"messages":[{"role":"user","content":"q1"},{"role":"user","content":[{"type":"image","source":{"data":"KEEP"}}]},{"role":"assistant","content":"a"},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"r"}]}]}`)
	out, _ = slimRequestBody(anth2, slimCfg)
	if !bytes.Contains(out, []byte("KEEP")) {
		t.Fatal("recent image dropped")
	}
}
