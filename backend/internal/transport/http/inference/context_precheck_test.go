package inference

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"

	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

func codeLikeFiller(chars int) string {
	words := strings.Fields("func return var value error config handler request response stream token account retry buffer context window parser module")
	r := rand.New(rand.NewSource(7))
	var b strings.Builder
	for b.Len() < chars {
		b.WriteString(words[r.Intn(len(words))])
		b.WriteByte(' ')
	}
	return b.String()
}

func TestResponsesContextPrecheck(t *testing.T) {
	model := "precheck-test-model-256k"
	modeldomain.RegisterUpstreamModelProfile(model, modeldomain.UpstreamModelProfile{ContextWindow: 256000})

	// Production regression: a ~1.26M-char body (~250k real tokens) was
	// estimated at 315k by the old chars/4 rule and wrongly rejected.
	real250k, _ := json.Marshal(map[string]any{"model": model, "input": codeLikeFiller(1_262_000)})
	if msg, over := responsesContextOverflow(model, real250k); over {
		t.Fatalf("~250k-token request must pass: %s", msg)
	}
	// ~360k real tokens (accepted upstream historically) must also pass.
	real360k, _ := json.Marshal(map[string]any{"model": model, "input": codeLikeFiller(1_800_000)})
	if _, over := responsesContextOverflow(model, real360k); over {
		t.Fatal("~360k-token request must pass")
	}
	// Far beyond any plausible limit (>384k lower bound) is rejected.
	huge, _ := json.Marshal(map[string]any{"model": model, "input": []any{map[string]any{"role": "user", "content": codeLikeFiller(2_500_000)}}})
	message, over := responsesContextOverflow(model, huge)
	if !over || !strings.Contains(message, "compact") {
		t.Fatalf("2.5M chars must be rejected with compact guidance, got %v %q", over, message)
	}
	media, _ := json.Marshal(map[string]any{"model": model, "input": []any{
		map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + strings.Repeat("A", 3_000_000)},
		map[string]any{"type": "reasoning", "encrypted_content": strings.Repeat("B", 3_000_000)},
	}})
	if _, over := responsesContextOverflow(model, media); over {
		t.Fatal("binary/encrypted payloads must not count toward the estimate")
	}
	if _, over := responsesContextOverflow("unknown-model-without-profile", huge); over {
		t.Fatal("models without any known context window are never prechecked")
	}
}

func TestResponsesContextPrecheckUsesLargerStaticWindow(t *testing.T) {
	// grok-4.7: catalog may say 256k but the static table says 500k; the larger wins.
	body, _ := json.Marshal(map[string]any{"model": "grok-4.7", "input": codeLikeFiller(3_000_000)})
	if _, over := responsesContextOverflow("grok-4.7", body); over {
		t.Fatal("3M chars (~500k lower bound) is within 1.5x of the 500k static window")
	}
}
