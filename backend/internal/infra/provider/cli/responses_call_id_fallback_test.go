package cli

import "testing"

func TestResolveCallIDPrefersCallID(t *testing.T) {
	got, synthesized := resolveCallID(map[string]any{"call_id": "call_real", "id": "fco_ignored"})
	if got != "call_real" || synthesized {
		t.Fatalf("got=%q synthesized=%v", got, synthesized)
	}
}

func TestResolveCallIDFallsBackToID(t *testing.T) {
	got, synthesized := resolveCallID(map[string]any{"id": "fco_codex_apps"})
	if got != "fco_codex_apps" || !synthesized {
		t.Fatalf("got=%q synthesized=%v", got, synthesized)
	}
}

func TestResolveCallIDSynthesizesWhenMissing(t *testing.T) {
	got, synthesized := resolveCallID(map[string]any{"type": "function_call_output"})
	if !synthesized || got == "" || got[:5] != "call_" {
		t.Fatalf("got=%q synthesized=%v", got, synthesized)
	}
}

func TestNormalizeFunctionCallOutputSynthesizesMissingCallID(t *testing.T) {
	c := newResponsesToolCompatibility()
	out, err := c.normalizeFunctionCallOutputInput(map[string]any{
		"type": "function_call_output",
		"id":   "fco_01a1be00_apps_open_page",
		"name": "codex_apps_open_page",
		"output": "{\"page_id\":null}",
	}, "input[4]")
	if err != nil {
		t.Fatal(err)
	}
	if out["call_id"] != "fco_01a1be00_apps_open_page" {
		t.Fatalf("call_id=%v", out["call_id"])
	}
	if !c.changed {
		t.Fatal("expected changed")
	}
}
