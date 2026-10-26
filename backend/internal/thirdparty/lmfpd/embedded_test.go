package lmfpd

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLoadEmbedded(t *testing.T) {
	d, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"grok-4.5", "grok-4.6", "grok-4.7"} {
		if d.ModelFamily(m) != "grok" {
			t.Fatalf("%s family %q", m, d.ModelFamily(m))
		}
	}
	if d.HasModel("nope") {
		t.Fatal("unexpected model")
	}
	c, err := GenerateChallenges(3)
	if err != nil || len(c) != 3 {
		t.Fatal(err)
	}
	a, err := d.Analyze([]Output{{Text: "1 2 3", ExpectedCount: 300}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Results) != 0 {
		t.Fatalf("too few numbers should not score: %+v", a.Decision)
	}
}

func TestEmbeddedRanksReferenceSamples(t *testing.T) {
	raw, err := os.ReadFile("testdata/samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var samples map[string]json.RawMessage
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatal(err)
	}
	d, _ := LoadEmbedded()
	for _, key := range []string{"grok-4.7", "gpt"} {
		var outs []Output
		if err := json.Unmarshal(samples[key], &outs); err != nil {
			t.Fatal(err)
		}
		a, err := d.Analyze(outs, false)
		if err != nil || len(a.Results) == 0 {
			t.Fatalf("%s: %v %+v", key, err, a)
		}
		t.Logf("%s -> %s fam=%s p=%v", key, a.Prediction, a.FamilyPrediction, deref(a.Probability))
		if key == "grok-4.7" && a.FamilyPrediction != "grok" {
			t.Fatalf("grok sample predicted %s", a.FamilyPrediction)
		}
		if key == "gpt" && a.FamilyPrediction == "grok" {
			t.Fatal("gpt sample predicted grok")
		}
	}
}

func deref(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}
