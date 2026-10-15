package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
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
	slimVideoPlaceholder      = "[视频已省略 / video omitted to save context]"
	slimAudioPlaceholder      = "[音频已省略 / audio omitted to save context]"
	slimFilePlaceholderFmt    = "[文件已省略: %s / file omitted to save context]"
	slimDataPlaceholderFmt    = "[内嵌数据已省略: %s / inline data omitted to save context]"
	slimDataURIMinBytes       = 1 << 10
	DefaultSlimKeepImageTurns = 2
	DefaultSlimKeepMediaTurns = 2
	DefaultSlimKeepToolTurns  = 2
	slimAuditHeader           = "X-Grok2api-Request-Slimming"
)

// RequestSlimmingConfig is the runtime slimming policy.
type RequestSlimmingConfig struct {
	Enabled bool
	// KeepMediaTurns keeps images/video/audio/files/inline base64 only in the
	// most recent N user turns. KeepImageTurns is the legacy alias.
	KeepMediaTurns      int
	KeepImageTurns      int
	KeepToolOutputTurns int
}

func (c RequestSlimmingConfig) mediaTurns() int {
	if c.KeepMediaTurns > 0 {
		return c.KeepMediaTurns
	}
	if c.KeepImageTurns > 0 {
		return c.KeepImageTurns
	}
	return DefaultSlimKeepMediaTurns
}

type requestSlimmingState struct {
	v atomic.Pointer[RequestSlimmingConfig]
}

// UpdateRequestSlimming replaces the slimming policy.
func (s *Service) UpdateRequestSlimming(cfg RequestSlimmingConfig) {
	cfg.KeepMediaTurns = cfg.mediaTurns()
	cfg.KeepImageTurns = cfg.KeepMediaTurns
	if cfg.KeepToolOutputTurns <= 0 {
		cfg.KeepToolOutputTurns = DefaultSlimKeepToolTurns
	}
	s.requestSlimming.v.Store(&cfg)
}

func (s *Service) requestSlimmingConfig() RequestSlimmingConfig {
	if cfg := s.requestSlimming.v.Load(); cfg != nil {
		return *cfg
	}
	return RequestSlimmingConfig{Enabled: true, KeepMediaTurns: DefaultSlimKeepMediaTurns, KeepImageTurns: DefaultSlimKeepImageTurns, KeepToolOutputTurns: DefaultSlimKeepToolTurns}
}

type slimStats struct {
	OriginalBytes   int
	SlimmedBytes    int
	ImagesRemoved   int
	VideosRemoved   int
	AudiosRemoved   int
	FilesRemoved    int
	DataURIsRemoved int
	OutputsTrimmed  int
	BytesTrimmed    int
}

func (st slimStats) mediaRemoved() int {
	return st.ImagesRemoved + st.VideosRemoved + st.AudiosRemoved + st.FilesRemoved + st.DataURIsRemoved
}

func (st slimStats) changed() bool { return st.mediaRemoved() > 0 || st.OutputsTrimmed > 0 }

func (st *slimStats) add(o slimStats) {
	st.ImagesRemoved += o.ImagesRemoved
	st.VideosRemoved += o.VideosRemoved
	st.AudiosRemoved += o.AudiosRemoved
	st.FilesRemoved += o.FilesRemoved
	st.DataURIsRemoved += o.DataURIsRemoved
	st.OutputsTrimmed += o.OutputsTrimmed
	st.BytesTrimmed += o.BytesTrimmed
}

func (st slimStats) auditValue() string {
	return fmt.Sprintf("original=%d slimmed=%d images_removed=%d videos_removed=%d audios_removed=%d files_removed=%d data_uris_removed=%d outputs_trimmed=%d", st.OriginalBytes, st.SlimmedBytes, st.ImagesRemoved, st.VideosRemoved, st.AudiosRemoved, st.FilesRemoved, st.DataURIsRemoved, st.OutputsTrimmed)
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
	keepImg, keepTool := cfg.mediaTurns(), cfg.KeepToolOutputTurns
	textType := "text"
	if field == "input" {
		textType = "input_text"
	}
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
			changed = stripMedia(obj, textType, &local) || changed
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
		stats.add(local)
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

// stripMedia recursively replaces multimodal parts (image, video, audio,
// inline files/documents) with a text placeholder and scrubs inline base64
// data URIs from any remaining string (e.g. tool outputs).
func stripMedia(node any, textType string, st *slimStats) bool {
	changed := false
	switch n := node.(type) {
	case map[string]any:
		for _, key := range sortedKeys(n) {
			if key == "encrypted_content" {
				continue
			}
			switch child := n[key].(type) {
			case []any:
				for idx, el := range child {
					if repl, ok := mediaReplacement(el, textType, st); ok {
						child[idx] = repl
						changed = true
						continue
					}
					if str, ok := el.(string); ok {
						if ns, ok := scrubDataURIs(str, st); ok {
							child[idx] = ns
							changed = true
						}
						continue
					}
					changed = stripMedia(el, textType, st) || changed
				}
			case map[string]any:
				changed = stripMedia(child, textType, st) || changed
			case string:
				if ns, ok := scrubDataURIs(child, st); ok {
					n[key] = ns
					changed = true
				}
			}
		}
	case []any:
		for _, el := range n {
			changed = stripMedia(el, textType, st) || changed
		}
	}
	return changed
}

var slimDataURIRe = regexp.MustCompile(`data:([A-Za-z0-9.+-]+/[A-Za-z0-9.+-]+)?((?:;[A-Za-z0-9._=-]+)*);base64,[A-Za-z0-9+/=_\-\\]+`)

func scrubDataURIs(s string, st *slimStats) (string, bool) {
	if len(s) < slimDataURIMinBytes || !strings.Contains(s, ";base64,") {
		return s, false
	}
	n := 0
	out := slimDataURIRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) < slimDataURIMinBytes {
			return m
		}
		n++
		mime := "application/octet-stream"
		if sub := slimDataURIRe.FindStringSubmatch(m); len(sub) > 1 && sub[1] != "" {
			mime = sub[1]
		}
		return fmt.Sprintf(slimDataPlaceholderFmt, mime)
	})
	if n == 0 {
		return s, false
	}
	st.DataURIsRemoved += n
	return out, true
}

func mediaKindFromMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	}
	return ""
}

// hasInlineData reports whether a node carries inline (base64 / data URI)
// payload rather than a small URL or file_id reference.
func hasInlineData(node any) bool {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if s, ok := v.(string); ok {
				if k == "file_data" || (k == "data" && len(s) >= 256) || (strings.HasPrefix(s, "data:") && strings.Contains(s, ";base64,")) {
					return true
				}
				if t, _ := n["type"].(string); k == "type" && (t == "base64" || t == "text" || t == "content") {
					return true
				}
				continue
			}
			if hasInlineData(v) {
				return true
			}
		}
	case []any:
		for _, el := range n {
			if hasInlineData(el) {
				return true
			}
		}
	}
	return false
}

func fileName(m map[string]any) string {
	for _, src := range []map[string]any{m, asMap(m["file"]), asMap(m["source"])} {
		if src == nil {
			continue
		}
		for _, k := range []string{"filename", "title", "name"} {
			if v, ok := src[k].(string); ok && v != "" {
				return v
			}
		}
	}
	return "file"
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func mediaReplacement(el any, textType string, st *slimStats) (any, bool) {
	m, ok := el.(map[string]any)
	if !ok {
		return nil, false
	}
	t, _ := m["type"].(string)
	kind := ""
	switch t {
	case "input_image", "image_url":
		kind = "image"
	case "image":
		if _, ok := m["source"]; ok {
			kind = "image"
		} else if hasInlineData(m) || m["image_url"] != nil || m["url"] != nil {
			kind = "image"
		}
	case "input_video", "video_url", "video":
		kind = "video"
	case "input_audio", "audio_url", "audio":
		kind = "audio"
	case "input_file", "file", "document":
		if hasInlineData(m) {
			kind = "file"
			if src := asMap(m["source"]); src != nil {
				if mt, _ := src["media_type"].(string); mediaKindFromMime(mt) != "" {
					kind = mediaKindFromMime(mt)
				}
			}
		}
	}
	text := ""
	switch kind {
	case "image":
		st.ImagesRemoved++
		text = slimImagePlaceholder
	case "video":
		st.VideosRemoved++
		text = slimVideoPlaceholder
	case "audio":
		st.AudiosRemoved++
		text = slimAudioPlaceholder
	case "file":
		st.FilesRemoved++
		text = fmt.Sprintf(slimFilePlaceholderFmt, fileName(m))
	default:
		return nil, false
	}
	return map[string]any{"type": textType, "text": text}, true
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
