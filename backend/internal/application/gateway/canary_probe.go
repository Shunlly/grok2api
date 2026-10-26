package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chenyme/grok2api/backend/internal/domain/audit"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
)

// CanaryProbeInput is one observe-only canary request pinned to an account.
type CanaryProbeInput struct {
	ClientKeyID     uint64
	AccountID       uint64
	Model           string
	Prompt          string
	ReasoningEffort string
	MaxOutputTokens int
	// IdleTimeout bounds the wait for the first generated token and any later
	// silent gap between stream chunks. Zero uses DefaultCanaryIdleTimeout.
	IdleTimeout time.Duration
	// MaxVisibleChars, when positive, stops reading once that much visible
	// text has arrived and returns it as a successful (truncated) output.
	// Guards against runaway outputs when upstream ignores MaxOutputTokens.
	MaxVisibleChars int
}

// DefaultCanaryIdleTimeout is the first-token / inter-chunk silence budget.
const DefaultCanaryIdleTimeout = 75 * time.Second

// Canary transport error codes.
const (
	CanaryErrIdleTimeout = "canary_stream_idle_timeout"
	CanaryErrInterrupted = "canary_stream_interrupted"
	CanaryErrIncomplete  = "canary_stream_incomplete"
)

// CanaryProbeOutput carries the raw measurements; scoring lives in domain/canary.
type CanaryProbeOutput struct {
	RequestID       string
	Text            string
	VisibleChars    int64
	OutputTokens    int64
	ReasoningTokens int64
	ReasoningChars  int64
	ReasoningText   string
	InputTokens     int64
	TotalTokens     int64
	ResponseModel   string
	FirstTokenMS    int64
	DurationMS      int64
	// StreamBytes / StreamChunks count what arrived (also on failures).
	StreamBytes  int64
	StreamChunks int64
	// ErrorCode is the transport error label when the probe failed.
	ErrorCode string
}

// canaryMaxReasoningCapture bounds the reasoning text kept for the artifact.
const canaryMaxReasoningCapture = 128 << 10

// ErrCanaryTransport marks failures that must not count against an account.
var ErrCanaryTransport = errors.New("canary transport error")

// ProbeAccountCanary sends one streaming chat request pinned to accountID on
// its own egress. It never changes routing state beyond the normal per-request
// accounting the gateway already performs.
func (s *Service) ProbeAccountCanary(ctx context.Context, input CanaryProbeInput) (CanaryProbeOutput, error) {
	key, err := s.clientKeys.Get(ctx, input.ClientKeyID)
	if err != nil {
		return CanaryProbeOutput{}, fmt.Errorf("读取金丝雀探测身份: %w", err)
	}
	publicModel, ok := qualityProbeBuildPublicModel(input.Model)
	if !ok {
		return CanaryProbeOutput{}, fmt.Errorf("金丝雀探测模型必须属于 Grok Build: %s", input.Model)
	}
	idPart, err := security.NewOpaqueToken(12)
	if err != nil {
		return CanaryProbeOutput{}, err
	}
	// The quality_ prefix keeps probes out of the degrade statistics.
	requestID := "quality_canary_" + idPart
	payload := map[string]any{
		"model":          input.Model,
		"messages":       []map[string]string{{"role": "user", "content": input.Prompt}},
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
	}
	if input.MaxOutputTokens > 0 {
		payload["max_tokens"] = input.MaxOutputTokens
	}
	if effort := strings.TrimSpace(input.ReasoningEffort); effort != "" {
		payload["reasoning_effort"] = effort
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CanaryProbeOutput{}, err
	}
	idle := input.IdleTimeout
	if idle <= 0 {
		idle = DefaultCanaryIdleTimeout
	}
	probeCtx, cancelProbe := context.WithCancel(ctx)
	defer cancelProbe()
	watchdog := newCanaryWatchdog(idle, cancelProbe)
	defer watchdog.stop()
	startedAt := time.Now()
	result, err := s.CreateChatCompletion(probeCtx, Input{
		RequestID: requestID, ClientKey: key, PublicModel: publicModel, Body: body,
		Streaming: true, Operation: audit.OperationChat, ForcedAccountID: input.AccountID, CanaryProbe: true,
	})
	if err != nil {
		out := CanaryProbeOutput{RequestID: requestID, DurationMS: time.Since(startedAt).Milliseconds()}
		if watchdog.fired() || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out.ErrorCode = CanaryErrIdleTimeout
			return out, fmt.Errorf("%w: %s: 等待上游响应超时: %v", ErrCanaryTransport, CanaryErrIdleTimeout, err)
		}
		return out, fmt.Errorf("%w: %v", ErrCanaryTransport, err)
	}
	defer result.Body.Close()
	usage := Usage{}
	responseID := ""
	errorCode := ""
	defer func() { result.Finalize(usage, responseID, errorCode) }()
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		errorCode = "canary_upstream_error"
		raw, _ := io.ReadAll(io.LimitReader(result.Body, 4<<10))
		return CanaryProbeOutput{RequestID: requestID, ErrorCode: errorCode, DurationMS: time.Since(startedAt).Milliseconds(), Text: strings.TrimSpace(string(raw))},
			fmt.Errorf("%w: 上游返回 %d: %s", ErrCanaryTransport, result.StatusCode, strings.TrimSpace(string(raw)))
	}
	out, readErr := readCanaryStreamLimited(ctx, result.Body, startedAt, watchdog, &usage, &responseID, result.MarkFirstToken, input.MaxVisibleChars)
	out.RequestID = requestID
	errorCode = out.ErrorCode
	return out, readErr
}

// canaryWatchdog cancels the probe when no progress happens within the budget.
type canaryWatchdog struct {
	timer   *time.Timer
	budget  time.Duration
	mu      sync.Mutex
	didFire bool
}

func newCanaryWatchdog(budget time.Duration, cancel context.CancelFunc) *canaryWatchdog {
	w := &canaryWatchdog{budget: budget}
	w.timer = time.AfterFunc(budget, func() {
		w.mu.Lock()
		w.didFire = true
		w.mu.Unlock()
		cancel()
	})
	return w
}

func (w *canaryWatchdog) reset() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.didFire {
		w.timer.Reset(w.budget)
	}
}

func (w *canaryWatchdog) stop() {
	if w != nil {
		w.timer.Stop()
	}
}

func (w *canaryWatchdog) fired() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.didFire
}

// readCanaryStream parses the SSE body. parentCtx is the caller's overall
// deadline (not the watchdog-cancelled child). Partial output is returned on
// failure so it can be stored as an artifact.
func readCanaryStream(parentCtx context.Context, body io.Reader, startedAt time.Time, watchdog *canaryWatchdog, usage *Usage, responseID *string, markFirst func()) (CanaryProbeOutput, error) {
	return readCanaryStreamLimited(parentCtx, body, startedAt, watchdog, usage, responseID, markFirst, 0)
}

// CanaryErrVisibleCapped marks an output cut at MaxVisibleChars.
const CanaryErrVisibleCapped = "canary_visible_capped"

func readCanaryStreamLimited(parentCtx context.Context, body io.Reader, startedAt time.Time, watchdog *canaryWatchdog, usage *Usage, responseID *string, markFirst func(), maxVisible int) (CanaryProbeOutput, error) {
	var firstAt time.Time
	var visible strings.Builder
	var reasoningChars int64
	var reasoningText strings.Builder
	var chunks int64
	terminal := false
	total := 0
	build := func(code string) CanaryProbeOutput {
		out := CanaryProbeOutput{
			Text: visible.String(), OutputTokens: usage.OutputTokens,
			ReasoningTokens: usage.ReasoningTokens, ReasoningChars: reasoningChars, ReasoningText: reasoningText.String(),
			InputTokens: usage.InputTokens, TotalTokens: usage.TotalTokens, ResponseModel: usage.ResponseModel,
			DurationMS: time.Since(startedAt).Milliseconds(), StreamBytes: int64(total), StreamChunks: chunks, ErrorCode: code,
		}
		out.VisibleChars = int64(utf8.RuneCountInString(out.Text))
		if !firstAt.IsZero() {
			out.FirstTokenMS = firstAt.Sub(startedAt).Milliseconds()
		}
		return out
	}
	markFirstToken := func() {
		if firstAt.IsZero() {
			firstAt = time.Now()
			if markFirst != nil {
				markFirst()
			}
		}
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		total += len(line) + 1
		if total > qualityProbeMaxStreamBytes {
			return build("canary_response_too_large"), fmt.Errorf("%w: 响应过大", ErrCanaryTransport)
		}
		if line == ": grok2api-reasoning-start" {
			markFirstToken()
			watchdog.reset()
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			// Comments/keepalives do not count as progress.
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			terminal = true
			break
		}
		var event qualityProbeChatEvent
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		chunks++
		if *responseID == "" {
			*responseID = event.ID
		}
		if event.Usage != nil {
			usage.Reported = true
			usage.InputTokens = event.Usage.PromptTokens
			usage.OutputTokens = event.Usage.CompletionTokens
			usage.ReasoningTokens = event.Usage.CompletionTokensDetails.ReasoningTokens
			usage.TotalTokens = event.Usage.TotalTokens
			usage.ResponseModel = event.Model
		}
		for _, choice := range event.Choices {
			delta := choice.Delta
			if qualityProbeHasGeneratedDelta(delta.Content, delta.Reasoning, delta.ReasoningContent, delta.ThinkingContent) {
				markFirstToken()
			}
			reasoningChars += int64(utf8.RuneCountInString(delta.Reasoning + delta.ReasoningContent + delta.ThinkingContent))
			if reasoningText.Len() < canaryMaxReasoningCapture {
				reasoningText.WriteString(delta.Reasoning + delta.ReasoningContent + delta.ThinkingContent)
			}
			visible.WriteString(delta.Content)
		}
		if maxVisible > 0 && visible.Len() >= maxVisible {
			return build(CanaryErrVisibleCapped), nil
		}
		// Before the first generated token only real tokens count as progress;
		// afterwards any data chunk resets the idle budget.
		if !firstAt.IsZero() {
			watchdog.reset()
		}
	}
	if err := scanner.Err(); err != nil {
		idleCut := watchdog.fired() || (firstAt.IsZero() && errors.Is(parentCtx.Err(), context.DeadlineExceeded))
		if idleCut {
			out := build(CanaryErrIdleTimeout)
			return out, fmt.Errorf("%w: %s: 流空闲超时（已收 %d 字节/%d 块/%d 输出 token，首字=%t）: %v",
				ErrCanaryTransport, CanaryErrIdleTimeout, out.StreamBytes, out.StreamChunks, out.OutputTokens, !firstAt.IsZero(), err)
		}
		out := build(CanaryErrInterrupted)
		return out, fmt.Errorf("%w: %s（已收 %d 字节/%d 块）: %v", ErrCanaryTransport, CanaryErrInterrupted, out.StreamBytes, out.StreamChunks, err)
	}
	if !terminal {
		if watchdog.fired() {
			out := build(CanaryErrIdleTimeout)
			return out, fmt.Errorf("%w: %s: 流空闲超时（已收 %d 字节/%d 块）", ErrCanaryTransport, CanaryErrIdleTimeout, out.StreamBytes, out.StreamChunks)
		}
		return build(CanaryErrIncomplete), fmt.Errorf("%w: 流未正常结束", ErrCanaryTransport)
	}
	return build(""), nil
}
