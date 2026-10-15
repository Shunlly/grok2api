package gateway

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/chenyme/grok2api/backend/internal/domain/audit"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

// Gateway-side auto compaction modelled on grok-build
// (xai-grok-shell session/compaction.rs, helpers/full_replace_compaction.rs):
// when the estimated input exceeds thresholdPercent (85%) of the model context
// window, older history is replaced by a model-written summary that starts with
// the grok-build continuation preamble. System/developer items, the latest user
// message and the most recent turns (with their tool call/output pairs) are kept
// verbatim. Clients such as Codex resend the full original history every turn,
// so summaries are cached by a hash chain over the compacted prefix and reused
// incrementally instead of being regenerated on every request.

//go:embed auto_compact_prompt.txt
var autoCompactSummaryPrompt string

const (
	autoCompactPreamble          = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n"
	autoCompactAuditHeader       = "X-Grok2api-Context-Compacted"
	DefaultAutoCompactThreshold  = 85
	DefaultAutoCompactWindow     = 256_000
	DefaultAutoCompactReserve    = 32_768
	DefaultAutoCompactSummaryMax = 60_000
	DefaultAutoCompactKeepTurns  = 2
	DefaultAutoCompactKeepPct    = 25
	DefaultAutoCompactCache      = 256
	DefaultAutoCompactTimeout    = 4 * time.Minute
	autoCompactTranscriptItemMax = 8_000
	autoCompactTranscriptMaxB    = 1_800_000
	autoCompactImageTokens       = 1_500
)

// AutoCompactConfig is the runtime compaction policy.
type AutoCompactConfig struct {
	Enabled               bool
	ThresholdPercent      int
	KeepRecentTurns       int
	KeepRecentPercent     int
	SummaryMaxChars       int
	ReserveTokens         int
	CacheEntries          int
	SummaryTimeout        time.Duration
	FallbackContextWindow int
}

func (c AutoCompactConfig) normalized() AutoCompactConfig {
	if c.ThresholdPercent <= 0 || c.ThresholdPercent > 100 {
		c.ThresholdPercent = DefaultAutoCompactThreshold
	}
	if c.KeepRecentTurns <= 0 {
		c.KeepRecentTurns = DefaultAutoCompactKeepTurns
	}
	if c.KeepRecentPercent <= 0 || c.KeepRecentPercent >= c.ThresholdPercent {
		c.KeepRecentPercent = min(DefaultAutoCompactKeepPct, c.ThresholdPercent/2)
	}
	if c.SummaryMaxChars <= 0 {
		c.SummaryMaxChars = DefaultAutoCompactSummaryMax
	}
	if c.ReserveTokens <= 0 {
		c.ReserveTokens = DefaultAutoCompactReserve
	}
	if c.CacheEntries <= 0 {
		c.CacheEntries = DefaultAutoCompactCache
	}
	if c.SummaryTimeout <= 0 {
		c.SummaryTimeout = DefaultAutoCompactTimeout
	}
	if c.FallbackContextWindow <= 0 {
		c.FallbackContextWindow = DefaultAutoCompactWindow
	}
	return c
}

type autoCompactState struct {
	cfg   atomic.Pointer[AutoCompactConfig]
	cache *summaryCache
	once  sync.Once
}

// UpdateAutoCompact replaces the compaction policy.
func (s *Service) UpdateAutoCompact(cfg AutoCompactConfig) {
	cfg = cfg.normalized()
	s.autoCompact.cfg.Store(&cfg)
	s.autoCompactCache().resize(cfg.CacheEntries)
}

func (s *Service) autoCompactConfig() AutoCompactConfig {
	if cfg := s.autoCompact.cfg.Load(); cfg != nil {
		return *cfg
	}
	return AutoCompactConfig{Enabled: true}.normalized()
}

func (s *Service) autoCompactCache() *summaryCache {
	s.autoCompact.once.Do(func() { s.autoCompact.cache = newSummaryCache(DefaultAutoCompactCache) })
	return s.autoCompact.cache
}

// contextWindowForModel resolves the context window from the upstream model
// catalog (grok-build's default `context_window`), capped by the admin Build
// window setting, falling back to 256k like grok-build.
func contextWindowForModel(upstreamModel string, fallback int) int {
	window := 0
	if profile, ok := modeldomain.UpstreamProfile(upstreamModel); ok {
		window = profile.ContextWindow
		if window <= 0 {
			window = profile.MaxContextWindow()
		}
	}
	if capValue := modeldomain.BuildContextWindowCap(); capValue > 0 && (window <= 0 || capValue < window) {
		window = capValue
	}
	if window <= 0 {
		window = fallback
	}
	return window
}

// ---- token estimation ----

var base64ish = regexp.MustCompile(`^[A-Za-z0-9+/=_\-\s]+$`)

// estimateTokens approximates input tokens (~4 bytes per token for text; inline
// media counts as a fixed image cost instead of its base64 length).
func estimateTokens(raw []byte) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return (len(raw) + 3) / 4
	}
	return estimateNodeTokens(v)
}

func estimateNodeTokens(node any) int {
	switch n := node.(type) {
	case map[string]any:
		total := 0
		for k, v := range n {
			total += 1 + len(k)/8
			total += estimateNodeTokens(v)
		}
		return total
	case []any:
		total := 0
		for _, el := range n {
			total += estimateNodeTokens(el)
		}
		return total
	case string:
		if len(n) >= 1024 && (strings.HasPrefix(n, "data:") && strings.Contains(n[:min(len(n), 128)], ";base64,") || base64ish.MatchString(n[:min(len(n), 2048)]) && !strings.ContainsAny(n[:min(len(n), 2048)], " \n")) {
			return autoCompactImageTokens
		}
		return (len(n) + 3) / 4
	default:
		return 1
	}
}

// ---- item classification ----

type compactItem struct {
	raw    json.RawMessage
	tokens int
	system bool
	user   bool // genuine user turn start
	output bool // tool output that must follow its call
}

func classifyCompactItem(raw json.RawMessage) compactItem {
	it := compactItem{raw: raw, tokens: estimateTokens(raw)}
	var head struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(raw, &head)
	switch head.Type {
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output", "mcp_call_output", "tool_search_output", "computer_call_output":
		it.output = true
	}
	if head.Type == "" || head.Type == "message" {
		switch head.Role {
		case "system", "developer":
			it.system = true
		case "tool":
			it.output = true
		case "user":
			if isUserTurnStart(raw) {
				it.user = true
			} else {
				it.output = true // Anthropic tool_result-only user message
			}
		}
	}
	return it
}

// ---- compaction plan ----

type compactPlan struct {
	field       string
	top         map[string]json.RawMessage
	items       []compactItem
	convIdx     []int // indices of non-system items
	chain       []string
	totalTokens int
}

func parseCompactPlan(body []byte) (*compactPlan, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return nil, false
	}
	field := ""
	for _, name := range []string{"input", "messages"} {
		if raw, ok := top[name]; ok && len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '[' {
			field = name
			break
		}
	}
	if field == "" {
		return nil, false
	}
	var raws []json.RawMessage
	if json.Unmarshal(top[field], &raws) != nil || len(raws) < 2 {
		return nil, false
	}
	p := &compactPlan{field: field, top: top}
	for _, raw := range raws {
		p.items = append(p.items, classifyCompactItem(raw))
	}
	// Everything except the conversation items (instructions, tools, system) counts too.
	p.totalTokens = estimateTokens(body)
	h := sha256.New()
	for i, it := range p.items {
		if it.system {
			continue
		}
		p.convIdx = append(p.convIdx, i)
		h.Write(bytes.TrimSpace(it.raw))
		h.Write([]byte{0})
		p.chain = append(p.chain, hex.EncodeToString(h.Sum(nil)))
	}
	return p, true
}

// chainKey identifies the first k conversation items (k >= 1).
func (p *compactPlan) chainKey(model string, k int) string {
	return model + "|" + p.field + "|" + p.chain[k-1]
}

func (p *compactPlan) conv(k int) compactItem { return p.items[p.convIdx[k]] }

func (p *compactPlan) suffixTokens(c int) int {
	total := 0
	for k := c; k < len(p.convIdx); k++ {
		total += p.conv(k).tokens
	}
	return total
}

// lastUserConv returns the conversation index of the latest genuine user message.
func (p *compactPlan) lastUserConv() int {
	for k := len(p.convIdx) - 1; k >= 0; k-- {
		if p.conv(k).user {
			return k
		}
	}
	return -1
}

// chooseCut picks the first kept conversation index (> after). A valid cut never
// starts on a tool output, so every kept output still follows its call.
func (p *compactPlan) chooseCut(after, keepTurns, budget int) int {
	n := len(p.convIdx)
	turnStart := -1
	seen := 0
	for k := n - 1; k >= 0; k-- {
		if p.conv(k).user {
			seen++
			turnStart = k
			if seen >= keepTurns {
				break
			}
		}
	}
	start := after + 1
	if turnStart > start {
		start = turnStart
	}
	last := -1
	for c := start; c < n; c++ {
		if p.conv(c).output {
			continue
		}
		last = c
		if p.suffixTokens(c) <= budget {
			return c
		}
	}
	if last < 0 {
		// No boundary after the preferred start: fall back to the latest valid one.
		for c := n - 1; c > after; c-- {
			if !p.conv(c).output {
				return c
			}
		}
	}
	return last
}

func (p *compactPlan) summaryItem(text string) json.RawMessage {
	var v any
	if p.field == "input" {
		v = map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}
	} else {
		v = map[string]any{"role": "user", "content": text}
	}
	out, _ := marshalNoEscape(v)
	return out
}

// assemble builds: system items before the cut, summary, pinned latest user
// message (when it lies before the cut), then every item from the cut onward.
func (p *compactPlan) assemble(cut int, summary string) ([]byte, error) {
	cutPos := p.convIdx[cut]
	lastUser := p.lastUserConv()
	out := make([]json.RawMessage, 0, len(p.items))
	for i := 0; i < cutPos; i++ {
		if p.items[i].system {
			out = append(out, p.items[i].raw)
		}
	}
	out = append(out, p.summaryItem(autoCompactPreamble+summary))
	if lastUser >= 0 && lastUser < cut {
		out = append(out, p.conv(lastUser).raw)
	}
	for i := cutPos; i < len(p.items); i++ {
		out = append(out, p.items[i].raw)
	}
	encoded, err := marshalNoEscape(out)
	if err != nil {
		return nil, err
	}
	top := make(map[string]json.RawMessage, len(p.top))
	for k, v := range p.top {
		top[k] = v
	}
	top[p.field] = encoded
	body, err := marshalNoEscape(top)
	if err != nil || !json.Valid(body) {
		return nil, errors.New("compacted body invalid")
	}
	return body, nil
}

// ---- transcript rendering for the summarizer ----

func renderTranscript(p *compactPlan, from, to int, previous string) string {
	var b strings.Builder
	if previous != "" {
		b.WriteString("<conversation_summary>\n")
		b.WriteString(previous)
		b.WriteString("\n</conversation_summary>\n\n")
	}
	for k := from; k < to; k++ {
		b.WriteString(renderItem(p.conv(k).raw))
		b.WriteString("\n\n")
	}
	s := b.String()
	if len(s) > autoCompactTranscriptMaxB {
		head := autoCompactTranscriptMaxB * 3 / 10
		tail := autoCompactTranscriptMaxB - head
		s = clipUTF8(s[:head]) + "\n\n[... 中间部分对话过长已省略 / middle of transcript omitted ...]\n\n" + clipUTF8Tail(s[len(s)-tail:])
	}
	return s
}

func clipUTF8(s string) string {
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func clipUTF8Tail(s string) string {
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

func clipMiddle(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	head := limit * 6 / 10
	tail := limit - head
	return clipUTF8(s[:head]) + fmt.Sprintf("\n...[%d bytes omitted]...\n", len(s)-head-tail) + clipUTF8Tail(s[len(s)-tail:])
}

func renderItem(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return clipMiddle(string(raw), autoCompactTranscriptItemMax)
	}
	t, _ := m["type"].(string)
	role, _ := m["role"].(string)
	switch {
	case t == "function_call" || t == "custom_tool_call":
		name, _ := m["name"].(string)
		args := stringify(firstNonNil(m["arguments"], m["input"]))
		return fmt.Sprintf("[assistant tool call %s]\n%s", name, clipMiddle(args, autoCompactTranscriptItemMax))
	case t == "local_shell_call":
		return "[assistant shell call]\n" + clipMiddle(stringify(m["action"]), autoCompactTranscriptItemMax)
	case strings.HasSuffix(t, "_output") || role == "tool":
		return "[tool output]\n" + clipMiddle(contentText(firstNonNil(m["output"], m["content"])), autoCompactTranscriptItemMax)
	case t == "reasoning":
		return "[assistant reasoning]\n" + clipMiddle(contentText(m["summary"]), autoCompactTranscriptItemMax/2)
	}
	if role == "" {
		role = t
	}
	text := contentText(m["content"])
	if calls, ok := m["tool_calls"].([]any); ok {
		for _, c := range calls {
			cm := asMap(c)
			fn := asMap(cm["function"])
			text += fmt.Sprintf("\n[tool call %v] %s", fn["name"], clipMiddle(stringify(fn["arguments"]), autoCompactTranscriptItemMax))
		}
	}
	return fmt.Sprintf("[%s]\n%s", role, clipMiddle(text, autoCompactTranscriptItemMax*4))
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	out, _ := marshalNoEscape(v)
	return string(out)
}

func contentText(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case []any:
		parts := make([]string, 0, len(n))
		for _, el := range n {
			m := asMap(el)
			if m == nil {
				parts = append(parts, stringify(el))
				continue
			}
			t, _ := m["type"].(string)
			switch {
			case strings.Contains(t, "image"):
				parts = append(parts, "[image]")
			case strings.Contains(t, "video"), strings.Contains(t, "audio"), strings.Contains(t, "file"), t == "document":
				parts = append(parts, "["+t+"]")
			case t == "tool_use":
				parts = append(parts, fmt.Sprintf("[tool call %v] %s", m["name"], stringify(m["input"])))
			case t == "tool_result":
				parts = append(parts, "[tool output] "+contentText(m["content"]))
			default:
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	}
	return stringify(v)
}

// ---- summary cache (LRU + single flight) ----

type summaryCache struct {
	mu       sync.Mutex
	limit    int
	order    *list.List
	entries  map[string]*list.Element
	inflight map[string]*summaryCall
}

type summaryEntry struct{ key, summary string }

type summaryCall struct {
	done    chan struct{}
	summary string
	err     error
}

func newSummaryCache(limit int) *summaryCache {
	return &summaryCache{limit: limit, order: list.New(), entries: map[string]*list.Element{}, inflight: map[string]*summaryCall{}}
}

func (c *summaryCache) resize(limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	c.evictLocked()
}

func (c *summaryCache) evictLocked() {
	for c.order.Len() > c.limit {
		el := c.order.Back()
		c.order.Remove(el)
		delete(c.entries, el.Value.(*summaryEntry).key)
	}
}

func (c *summaryCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*summaryEntry).summary, true
	}
	return "", false
}

func (c *summaryCache) put(key, summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		el.Value.(*summaryEntry).summary = summary
		c.order.MoveToFront(el)
		return
	}
	c.entries[key] = c.order.PushFront(&summaryEntry{key: key, summary: summary})
	c.evictLocked()
}

// do runs fn once per key across concurrent callers (Codex retries resend the same body).
func (c *summaryCache) do(key string, fn func() (string, error)) (string, error) {
	c.mu.Lock()
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.summary, call.err
	}
	call := &summaryCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()
	call.summary, call.err = fn()
	if call.err == nil {
		c.put(key, call.summary)
	}
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
	close(call.done)
	return call.summary, call.err
}

// ---- orchestration ----

type compactStats struct {
	Window, Threshold         int
	BeforeTokens, AfterTokens int
	BeforeBytes, AfterBytes   int
	ItemsCompacted            int
	CacheHit, Fallback        bool
	SummaryChars              int
	SummaryMS                 int64
	SummaryError              string
}

func (st compactStats) auditValue() string {
	return fmt.Sprintf("window=%d threshold=%d before_tokens=%d after_tokens=%d before_bytes=%d after_bytes=%d items_compacted=%d cache_hit=%t fallback=%t summary_chars=%d summary_ms=%d",
		st.Window, st.Threshold, st.BeforeTokens, st.AfterTokens, st.BeforeBytes, st.AfterBytes, st.ItemsCompacted, st.CacheHit, st.Fallback, st.SummaryChars, st.SummaryMS)
}

type summarizeFunc func(ctx context.Context, transcript string) (string, error)

// autoCompactBody returns the compacted body, or ok=false when no compaction applies.
func autoCompactBody(ctx context.Context, body []byte, model string, window int, cfg AutoCompactConfig, cache *summaryCache, summarize summarizeFunc) ([]byte, compactStats, bool) {
	cfg = cfg.normalized()
	st := compactStats{Window: window, Threshold: window * cfg.ThresholdPercent / 100, BeforeBytes: len(body)}
	if !cfg.Enabled || window <= 0 {
		return body, st, false
	}
	p, ok := parseCompactPlan(body)
	if !ok {
		return body, st, false
	}
	st.BeforeTokens = p.totalTokens
	if p.totalTokens <= st.Threshold || len(p.convIdx) < 2 {
		return body, st, false
	}
	// 1. Reuse the longest cached summary prefix.
	base, previous := 0, ""
	for k := len(p.convIdx) - 1; k >= 1; k-- {
		if summary, hit := cache.get(p.chainKey(model, k)); hit {
			if p.conv(k).output {
				continue
			}
			base, previous = k, summary
			break
		}
	}
	if base > 0 {
		if out, err := p.assemble(base, previous); err == nil {
			if tokens := estimateTokens(out); tokens <= st.Threshold {
				st.CacheHit, st.AfterTokens, st.AfterBytes, st.ItemsCompacted, st.SummaryChars = true, tokens, len(out), base, len(previous)
				return out, st, true
			}
		}
	}
	// 2. Summarize [base, cut) (carrying the previous summary forward).
	budget := window * cfg.KeepRecentPercent / 100
	cut := p.chooseCut(base, cfg.KeepRecentTurns, budget)
	if cut <= base || cut <= 0 {
		return body, st, false
	}
	transcript := renderTranscript(p, base, cut, previous)
	started := time.Now()
	summary, err := cache.do(p.chainKey(model, cut), func() (string, error) {
		sctx, cancel := context.WithTimeout(ctx, cfg.SummaryTimeout)
		defer cancel()
		text, err := summarize(sctx, transcript)
		if err != nil {
			return "", err
		}
		text = cleanSummary(text)
		if strings.TrimSpace(text) == "" {
			return "", errors.New("empty summary")
		}
		if len(text) > cfg.SummaryMaxChars {
			text = clipUTF8(text[:cfg.SummaryMaxChars]) + "\n[summary truncated]"
		}
		return text, nil
	})
	st.SummaryMS = time.Since(started).Milliseconds()
	if err != nil {
		// Deterministic fallback: drop the oldest items with a placeholder.
		st.Fallback, st.SummaryError = true, err.Error()
		summary = fmt.Sprintf("[早期 %d 条对话记录因超出上下文已省略，自动摘要失败 / %d earlier conversation items were omitted to fit the context window; automatic summarization failed]", cut, cut)
		if previous != "" {
			summary = previous + "\n\n" + summary
		}
	}
	out, aerr := p.assemble(cut, summary)
	if aerr != nil {
		return body, st, false
	}
	st.AfterTokens, st.AfterBytes, st.ItemsCompacted, st.SummaryChars = estimateTokens(out), len(out), cut, len(summary)
	return out, st, true
}

var summaryTagRe = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)

func cleanSummary(text string) string {
	if m := summaryTagRe.FindStringSubmatch(text); len(m) == 2 {
		text = m[1]
	}
	return strings.TrimSpace(text)
}

// summarizeViaUpstream asks the same model, through the normal gateway path
// (same client key, account selection and audits), for a grok-build style summary.
func (s *Service) summarizeViaUpstream(parent Input, reserveTokens int) summarizeFunc {
	return func(ctx context.Context, transcript string) (string, error) {
		payload := map[string]any{
			"model": parent.PublicModel,
			"messages": []map[string]string{
				{"role": "system", "content": "You are a helpful AI assistant tasked with summarizing conversations."},
				{"role": "user", "content": "<conversation_transcript>\n" + transcript + "\n</conversation_transcript>\n\n" + autoCompactSummaryPrompt},
			},
			"stream":         true,
			"stream_options": map[string]bool{"include_usage": true},
			"max_tokens":     reserveTokens,
		}
		body, err := marshalNoEscape(payload)
		if err != nil {
			return "", err
		}
		result, err := s.CreateChatCompletion(ctx, Input{
			RequestID: parent.RequestID + "_autocompact", ClientKey: parent.ClientKey, PublicModel: parent.PublicModel, Body: body,
			Streaming: true, Method: http.MethodPost, Path: "/v1/chat/completions", Headers: map[string][]string{"X-Grok2api-Internal": {"auto-compact"}},
			auditOperation: audit.OperationCompaction, skipQualityHold: true, skipAutoCompact: true,
		})
		if err != nil {
			return "", err
		}
		defer result.Body.Close()
		usage, responseID, errorCode := Usage{}, "", ""
		defer func() {
			if result.Finalize != nil {
				result.Finalize(usage, responseID, errorCode)
			}
		}()
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			errorCode = "auto_compact_upstream_error"
			raw, _ := io.ReadAll(io.LimitReader(result.Body, 2<<10))
			return "", fmt.Errorf("summary upstream %d: %s", result.StatusCode, strings.TrimSpace(string(raw)))
		}
		out, readErr := readCanaryStream(ctx, result.Body, time.Now(), nil, &usage, &responseID, result.MarkFirstToken)
		errorCode = out.ErrorCode
		if readErr != nil {
			return "", readErr
		}
		return out.Text, nil
	}
}

// maybeAutoCompact applies compaction to input.Body when eligible.
func (s *Service) maybeAutoCompact(ctx context.Context, input *Input, upstreamModel string) (compactStats, bool) {
	cfg := s.autoCompactConfig()
	if !cfg.Enabled || input.skipAutoCompact || input.CanaryProbe || input.ForcedAccountID != 0 || input.PreviousResponseID != "" {
		return compactStats{}, false
	}
	if input.Operation == audit.OperationCompaction || input.auditOperation == audit.OperationCompaction {
		return compactStats{}, false
	}
	window := contextWindowForModel(upstreamModel, cfg.FallbackContextWindow)
	out, st, ok := autoCompactBody(ctx, input.Body, upstreamModel, window, cfg, s.autoCompactCache(), s.summarizeViaUpstream(*input, cfg.ReserveTokens))
	if ok {
		input.Body = out
	}
	return st, ok
}

func withCompactionAuditHeader(headers map[string][]string, st compactStats) map[string][]string {
	out := make(map[string][]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out[autoCompactAuditHeader] = []string{st.auditValue()}
	return out
}
