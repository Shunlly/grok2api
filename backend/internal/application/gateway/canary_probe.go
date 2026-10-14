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
}

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
	startedAt := time.Now()
	result, err := s.CreateChatCompletion(ctx, Input{
		RequestID: requestID, ClientKey: key, PublicModel: publicModel, Body: body,
		Streaming: true, Operation: audit.OperationChat, ForcedAccountID: input.AccountID, CanaryProbe: true,
	})
	if err != nil {
		return CanaryProbeOutput{RequestID: requestID}, fmt.Errorf("%w: %v", ErrCanaryTransport, err)
	}
	defer result.Body.Close()
	usage := Usage{}
	responseID := ""
	errorCode := ""
	defer func() { result.Finalize(usage, responseID, errorCode) }()
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		errorCode = "canary_upstream_error"
		raw, _ := io.ReadAll(io.LimitReader(result.Body, 4<<10))
		return CanaryProbeOutput{RequestID: requestID}, fmt.Errorf("%w: 上游返回 %d: %s", ErrCanaryTransport, result.StatusCode, strings.TrimSpace(string(raw)))
	}
	var firstAt time.Time
	var visible strings.Builder
	var reasoningChars int64
	var reasoningText strings.Builder
	terminal := false
	total := 0
	scanner := bufio.NewScanner(result.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		total += len(line) + 1
		if total > qualityProbeMaxStreamBytes {
			errorCode = "canary_response_too_large"
			return CanaryProbeOutput{RequestID: requestID}, fmt.Errorf("%w: 响应过大", ErrCanaryTransport)
		}
		if line == ": grok2api-reasoning-start" {
			if firstAt.IsZero() {
				firstAt = time.Now()
				if result.MarkFirstToken != nil {
					result.MarkFirstToken()
				}
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
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
		if responseID == "" {
			responseID = event.ID
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
			if qualityProbeHasGeneratedDelta(delta.Content, delta.Reasoning, delta.ReasoningContent, delta.ThinkingContent) && firstAt.IsZero() {
				firstAt = time.Now()
				if result.MarkFirstToken != nil {
					result.MarkFirstToken()
				}
			}
			reasoningChars += int64(utf8.RuneCountInString(delta.Reasoning + delta.ReasoningContent + delta.ThinkingContent))
			if reasoningText.Len() < canaryMaxReasoningCapture {
				reasoningText.WriteString(delta.Reasoning + delta.ReasoningContent + delta.ThinkingContent)
			}
			visible.WriteString(delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		errorCode = "canary_stream_interrupted"
		return CanaryProbeOutput{RequestID: requestID}, fmt.Errorf("%w: %v", ErrCanaryTransport, err)
	}
	if !terminal {
		errorCode = "canary_stream_incomplete"
		return CanaryProbeOutput{RequestID: requestID}, fmt.Errorf("%w: 流未正常结束", ErrCanaryTransport)
	}
	out := CanaryProbeOutput{
		RequestID: requestID, Text: visible.String(), OutputTokens: usage.OutputTokens,
		ReasoningTokens: usage.ReasoningTokens, ReasoningChars: reasoningChars, ReasoningText: reasoningText.String(),
		InputTokens: usage.InputTokens, TotalTokens: usage.TotalTokens, ResponseModel: usage.ResponseModel,
		DurationMS: time.Since(startedAt).Milliseconds(),
	}
	out.VisibleChars = int64(utf8.RuneCountInString(out.Text))
	if !firstAt.IsZero() {
		out.FirstTokenMS = firstAt.Sub(startedAt).Milliseconds()
	}
	return out, nil
}
