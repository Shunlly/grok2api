package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// Request slimming drops images and trims huge tool outputs from older turns
// before the body is forwarded to xAI. Coding clients such as Codex resend the
// full history (including every screenshot) on each request, which grows the
// body until xAI times out. Only items outside the most recent turns are
// touched, and the rewrite is a pure function of the body so repeated requests
// produce byte-identical prefixes (prompt-cache friendly).

const (
	slimImagePlaceholder      = "[图片已省略 / image omitted to save context]"
	slimToolOutputThreshold   = 16 << 10
	slimToolOutputHead        = 6 << 10
	slimToolOutputTail        = 4 << 10
	DefaultSlimKeepImageTurns = 2
	DefaultSlimKeepToolTurns  = 2
	slimAuditHeader           = "X-Grok2api-Request-Slimming"
)

// RequestSlimmingConfig is the runtime slimming policy.
type RequestSlimmingConfig struct {
	Enabled             bool
	KeepImageTurns      int
	KeepToolOutputTurns int
}

type requestSlimmingState struct {
	v atomic.Pointer[RequestSlimmingConfig]
}

// UpdateRequestSlimming replaces the slimming policy.
func (s *Service) UpdateRequestSlimming(cfg RequestSlimmingConfig) {
	if cfg.KeepImageTurns <= 0 {
		cfg.KeepImageTurns = DefaultSlimKeepImageTurns
	}
	if cfg.KeepToolOutputTurns <= 0 {
		cfg.KeepToolOutputTurns = DefaultSlimKeepToolTurns
	}
	s.requestSlimming.v.Store(&cfg)
}

func (s *Service) requestSlimmingConfig() RequestSlimmingConfig {
	if cfg := s.requestSlimming.v.Load(); cfg != nil {
		return *cfg
	}
	return RequestSlimmingConfig{Enabled: true, KeepImageTurns: DefaultSlimKeepImageTurns, KeepToolOutputTurns: DefaultSlimKeepToolTurns}
}

type slimStats struct {
	OriginalBytes  int
	SlimmedBytes   int
	ImagesRemoved  int
	OutputsTrimmed int
	BytesTrimmed   int
}

func (st slimStats) changed() bool { return st.ImagesRemoved > 0 || st.OutputsTrimmed > 0 }

func (st slimStats) auditValue() string {
	return fmt.Sprintf("original=%d slimmed=%d images_removed=%d outputs_trimmed=%d", st.OriginalBytes, st.SlimmedBytes, st.ImagesRemoved, st.OutputsTrimmed)
}

// slimRequestBody returns the slimmed body (or the original slice untouched
// when nothing qualifies or the body is not a recognised JSON shape).
func slimRequestBody(body []byte, cfg RequestSlimmingConfig) ([]byte, slimStats) {
	stats := slimStats{OriginalBytes: len(body), SlimmedBytes: len(body)}
	if !cfg.Enabled || len(body) == 0 {
		return body, stats
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return body, stats
	}
	field := ""
	for _, name := range []string{"input", "messages"} {
		if raw, ok := top[name]; ok && len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '[' {
			field = name
			break
		}
	}
	if field == "" {
		return body, stats
	}
	var items []json.RawMessage
	if err := json.Unmarshal(top[field], &items); err != nil {
		return body, stats
	}
	turnOf := assignTurns(items)
	totalTurns := 0
	if len(turnOf) > 0 {
		totalTurns = turnOf[len(turnOf)-1]
	}
	keepImg, keepTool := cfg.KeepImageTurns, cfg.KeepToolOutputTurns
	for i, raw := range items {
		age := totalTurns - turnOf[i] // 0 = latest turn
		dropImages := age >= keepImg
		trimTools := age >= keepTool && age >= 1
		if !dropImages && !trimTools {
			continue
		}
		if !dropImages && !bytes.Contains(raw, []byte("output")) && !bytes.Contains(raw, []byte("tool")) {
			continue
		}
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			continue
		}
		obj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		local := slimStats{}
		changed := false
		if dropImages {
			changed = stripImages(obj, &local) || changed
		}
		if trimTools {
			changed = trimToolOutputs(obj, &local) || changed
		}
		if !changed {
			continue
		}
		encoded, err := marshalNoEscape(obj)
		if err != nil {
			continue
		}
		items[i] = encoded
		stats.ImagesRemoved += local.ImagesRemoved
		stats.OutputsTrimmed += local.OutputsTrimmed
		stats.BytesTrimmed += local.BytesTrimmed
	}
	if !stats.changed() {
		return body, stats
	}
	newItems, err := marshalNoEscape(items)
	if err != nil {
		return body, slimStats{OriginalBytes: len(body), SlimmedBytes: len(body)}
	}
	top[field] = newItems
	out, err := marshalNoEscape(top)
	if err != nil || !json.Valid(out) {
		return body, slimStats{OriginalBytes: len(body), SlimmedBytes: len(body)}
	}
	stats.SlimmedBytes = len(out)
	return out, stats
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// assignTurns numbers items by user turn: a turn starts at a genuine user
// message (not an Anthropic user message made only of tool_result blocks).
// Items before the first user message get turn 0.
func assignTurns(items []json.RawMessage) []int {
	turns := make([]int, len(items))
	current := 0
	for i, raw := range items {
		if isUserTurnStart(raw) {
			current++
		}
		turns[i] = current
	}
	return turns
}

func isUserTurnStart(raw json.RawMessage) bool {
	var head struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Role != "user" {
		return false
	}
	if head.Type != "" && head.Type != "message" {
		return false
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(head.Content, &blocks) == nil && len(blocks) > 0 {
		for _, b := range blocks {
			if b.Type != "tool_result" {
				return true
			}
		}
		return false
	}
	return true
}

// stripImages recursively replaces image parts with a text placeholder.
func stripImages(node any, st *slimStats) bool {
	changed := false
	switch n := node.(type) {
	case map[string]any:
		for _, key := range sortedKeys(n) {
			child := n[key]
			if arr, ok := child.([]any); ok {
				for idx, el := range arr {
					if repl, ok := imageReplacement(el); ok {
						arr[idx] = repl
						st.ImagesRemoved++
						changed = true
						continue
					}
					changed = stripImages(el, st) || changed
				}
			} else if m, ok := child.(map[string]any); ok {
				changed = stripImages(m, st) || changed
			}
		}
	case []any:
		for _, el := range n {
			changed = stripImages(el, st) || changed
		}
	}
	return changed
}

func imageReplacement(el any) (any, bool) {
	m, ok := el.(map[string]any)
	if !ok {
		return nil, false
	}
	t, _ := m["type"].(string)
	switch t {
	case "input_image":
		return map[string]any{"type": "input_text", "text": slimImagePlaceholder}, true
	case "image_url":
		return map[string]any{"type": "text", "text": slimImagePlaceholder}, true
	case "image":
		if _, ok := m["source"]; ok {
			return map[string]any{"type": "text", "text": slimImagePlaceholder}, true
		}
	}
	return nil, false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// deterministic order
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// trimToolOutputs shortens oversized tool results in place.
func trimToolOutputs(obj map[string]any, st *slimStats) bool {
	t, _ := obj["type"].(string)
	role, _ := obj["role"].(string)
	switch {
	case t == "function_call_output" || t == "custom_tool_call_output" || t == "local_shell_call_output":
		return trimField(obj, "output", st)
	case role == "tool":
		return trimField(obj, "content", st)
	case role == "user":
		changed := false
		if blocks, ok := obj["content"].([]any); ok {
			for _, b := range blocks {
				if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_result" {
					changed = trimField(bm, "content", st) || changed
				}
			}
		}
		return changed
	}
	return false
}

func trimField(obj map[string]any, key string, st *slimStats) bool {
	switch v := obj[key].(type) {
	case string:
		if s, ok := trimText(v, st); ok {
			obj[key] = s
			return true
		}
	case []any:
		changed := false
		for _, el := range v {
			if m, ok := el.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					if s, ok := trimText(text, st); ok {
						m["text"] = s
						changed = true
					}
				}
			}
		}
		return changed
	}
	return false
}

func trimText(s string, st *slimStats) (string, bool) {
	if len(s) <= slimToolOutputThreshold {
		return s, false
	}
	headEnd := slimToolOutputHead
	for headEnd > 0 && !utf8.RuneStart(s[headEnd]) {
		headEnd--
	}
	tailStart := len(s) - slimToolOutputTail
	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}
	omitted := tailStart - headEnd
	var b strings.Builder
	b.WriteString(s[:headEnd])
	fmt.Fprintf(&b, "\n...[中间 %d 字节已省略 / %d bytes omitted to save context]...\n", omitted, omitted)
	b.WriteString(s[tailStart:])
	st.OutputsTrimmed++
	st.BytesTrimmed += omitted
	return b.String(), true
}

func withSlimmingAuditHeader(headers map[string][]string, st slimStats) map[string][]string {
	out := make(map[string][]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out[slimAuditHeader] = []string{st.auditValue()}
	return out
}
