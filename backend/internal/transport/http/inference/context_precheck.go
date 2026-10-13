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
const precheckCharsPerToken = 4

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
	profile, ok := modeldomain.UpstreamProfile(model)
	if !ok || profile.ContextWindow <= 0 {
		return "", false
	}
	estimate := estimateResponsesInputTokensLowerBound(body)
	if estimate <= profile.ContextWindow {
		return "", false
	}
	return fmt.Sprintf("输入上下文约 %d tokens（保守估计），已超过模型 %s 的上下文窗口 %d tokens。请压缩上下文（如 Codex 的 /compact）或开启新会话后重试。", estimate, model, profile.ContextWindow), true
}
