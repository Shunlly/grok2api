package inference

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

// Local context-length precheck for /v1/responses.
//
// The estimate is deliberately a LOWER bound so a healthy request is never
// rejected locally: only text-bearing string values are counted (keys, JSON
// punctuation, data: URLs / base64 media and encrypted reasoning blobs are
// skipped), and runes are divided by 4 (English averages ~4 chars/token, CJK
// ~1 char/token). We only reject when that lower bound already exceeds the
// model's catalog context window; anything else is left for upstream to judge.
const (
	// 6 chars/token is below every realistic tokenizer ratio for code/English
	// (observed ~4-5) so the estimate stays a lower bound. CJK text tokenizes
	// at ~1 char/token, which only makes the estimate lower still.
	precheckCharsPerToken = 6
	// Only reject when the lower bound exceeds the largest known window by
	// 50%: upstream has accepted ~360k-token requests while the Build catalog
	// advertises 256k for grok-4.7, so the catalog value is not a hard limit.
	precheckWindowSlackPercent = 150
	// Small bodies can never be over any window; skip parsing them entirely.
	precheckMinBodyBytes = 1 << 20
)

var precheckSkippedKeys = map[string]bool{
	"encrypted_content": true, "image_url": true, "file_data": true, "data": true,
	"signature": true, "id": true, "call_id": true, "previous_response_id": true,
}

func estimateResponsesInputTokensLowerBound(body []byte) int {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return 0
	}
	runes := 0
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch typed := v.(type) {
		case map[string]any:
			for k, child := range typed {
				if precheckSkippedKeys[k] {
					continue
				}
				walk(k, child)
			}
		case []any:
			for _, child := range typed {
				walk(key, child)
			}
		case string:
			if strings.HasPrefix(typed, "data:") {
				return
			}
			runes += utf8.RuneCountInString(typed)
		}
	}
	walk("", value)
	return runes / precheckCharsPerToken
}

// responsesContextOverflow returns a client-facing message when the request is
// certainly over the model's advertised context window.
func responsesContextOverflow(model string, body []byte) (string, bool) {
	if len(body) < precheckMinBodyBytes {
		return "", false
	}
	window := 0
	if profile, ok := modeldomain.UpstreamProfile(model); ok {
		window = profile.ContextWindow
	}
	if capability, known := grokCapabilities[model]; known && capability.contextWindow > window {
		window = capability.contextWindow
	}
	if window <= 0 {
		return "", false
	}
	limit := window * precheckWindowSlackPercent / 100
	estimate := estimateResponsesInputTokensLowerBound(body)
	if estimate <= limit {
		return "", false
	}
	return fmt.Sprintf("输入上下文至少约 %d tokens（保守下限估计），远超模型 %s 的上下文窗口 %d tokens。请压缩上下文（如 Codex 的 /compact）或开启新会话后重试。", estimate, model, window), true
}
