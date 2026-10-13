package inference

import (
	"encoding/json"
	"strings"
	"testing"

	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

func TestResponsesContextPrecheck(t *testing.T) {
	model := "precheck-test-model-1000"
	modeldomain.RegisterUpstreamModelProfile(model, modeldomain.UpstreamModelProfile{ContextWindow: 1000})

	small, _ := json.Marshal(map[string]any{"model": model, "input": strings.Repeat("a", 3000)})
	if _, over := responsesContextOverflow(model, small); over {
		t.Fatal("3000 chars (~750 tokens lower bound) must not be rejected for a 1000-token window")
	}
	big, _ := json.Marshal(map[string]any{"model": model, "input": []any{map[string]any{"role": "user", "content": strings.Repeat("hello ", 1000)}}})
	message, over := responsesContextOverflow(model, big)
	if !over || !strings.Contains(message, "compact") {
		t.Fatalf("6000 chars must be rejected with compact guidance, got %v %q", over, message)
	}
	// Images and encrypted reasoning must not inflate the estimate.
	media, _ := json.Marshal(map[string]any{"model": model, "input": []any{
		map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + strings.Repeat("A", 100000)},
		map[string]any{"type": "reasoning", "encrypted_content": strings.Repeat("B", 100000)},
	}})
	if _, over := responsesContextOverflow(model, media); over {
		t.Fatal("binary/encrypted payloads must not count toward the estimate")
	}
	if _, over := responsesContextOverflow("unknown-model-without-profile", big); over {
		t.Fatal("models without a catalog context window are never prechecked")
	}
}
