package canary

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gatewayapp "github.com/chenyme/grok2api/backend/internal/application/gateway"
	"github.com/chenyme/grok2api/backend/internal/domain/canary"
	"github.com/chenyme/grok2api/backend/internal/thirdparty/lmfpd"
)

type fpSamples map[string][]lmfpd.Output

func loadSamples(t *testing.T) fpSamples {
	t.Helper()
	raw, err := os.ReadFile("../../thirdparty/lmfpd/testdata/samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	out := fpSamples{}
	for _, key := range []string{"grok-4.7", "gpt"} {
		var v []lmfpd.Output
		if err := json.Unmarshal(all[key], &v); err != nil {
			t.Fatal(err)
		}
		out[key] = v
	}
	return out
}

// fpProber answers challenge i (encoded in the prompt) with a reference sample.
type fpProber struct {
	mu      sync.Mutex
	samples fpSamples
	as      map[uint64]string // account -> sample key; "" = too few numbers
	fail    map[uint64]bool
	calls   []gatewayapp.CanaryProbeInput
}

func (p *fpProber) ProbeAccountCanary(_ context.Context, in gatewayapp.CanaryProbeInput) (gatewayapp.CanaryProbeOutput, error) {
	p.mu.Lock()
	p.calls = append(p.calls, in)
	p.mu.Unlock()
	if p.fail[in.AccountID] {
		return gatewayapp.CanaryProbeOutput{RequestID: "rq"}, gatewayapp.ErrCanaryTransport
	}
	key := p.as[in.AccountID]
	if key == "" {
		return gatewayapp.CanaryProbeOutput{Text: "12 7 99", RequestID: "rq", DurationMS: 5}, nil
	}
	i := int(in.Prompt[len(in.Prompt)-1] - '0')
	return gatewayapp.CanaryProbeOutput{Text: p.samples[key][i].Text, RequestID: "rq", OutputTokens: 900, DurationMS: 1000 + int64(i)}, nil
}

type memFPStore struct {
	memStore
	mu  sync.Mutex
	fps []canary.FingerprintResult
}

func (m *memFPStore) CreateFingerprint(_ context.Context, v canary.FingerprintResult) (canary.FingerprintResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.ID = uint64(len(m.fps) + 1)
	m.fps = append([]canary.FingerprintResult{v}, m.fps...)
	return v, nil
}
func (m *memFPStore) ListFingerprints(_ context.Context, id uint64, _ int) ([]canary.FingerprintResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []canary.FingerprintResult{}
	for _, v := range m.fps {
		if id == 0 || v.AccountID == id {
			v.RawOutputs, v.Prompts = nil, nil
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *memFPStore) GetFingerprint(_ context.Context, id uint64) (canary.FingerprintResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.fps {
		if v.ID == id {
			return v, nil
		}
	}
	return canary.FingerprintResult{}, ErrResultNotFound
}
func (m *memFPStore) CountFingerprintRounds(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rounds := map[string]bool{}
	for _, v := range m.fps {
		rounds[v.RoundID] = true
	}
	return int64(len(rounds)), nil
}
func (m *memFPStore) DeleteFingerprintsBefore(context.Context, time.Time) error { return nil }

func newFPService(t *testing.T, prober *fpProber, cfg FingerprintConfig) (*Service, *memFPStore) {
	t.Helper()
	detector, err := lmfpd.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	samples := prober.samples
	source := func() ([]lmfpd.Challenge, error) {
		out := make([]lmfpd.Challenge, 3)
		for i := range out {
			out[i] = lmfpd.Challenge{ID: "c", ExpectedCount: samples["grok-4.7"][i].ExpectedCount, Prompt: "pick numbers #" + string(rune('0'+i))}
		}
		return out, nil
	}
	store := &memFPStore{memStore: memStore{rounds: map[string]bool{}}}
	service := NewService(Config{Fingerprint: cfg}, 9, prober, fakeAccounts{}, store, nil)
	service.SetFingerprintAnalyzer(detector, source)
	return service, store
}

func TestFingerprintRoundJudgesAndFlags(t *testing.T) {
	samples := loadSamples(t)
	prober := &fpProber{samples: samples, as: map[uint64]string{1: "grok-4.7", 2: "grok-4.7", 3: "gpt"}, fail: map[uint64]bool{}}
	service, _ := newFPService(t, prober, FingerprintConfig{Models: []string{"grok-4.7"}, FamilyTolerant: true, SuspectAfter: 2, SampleAccounts: 3})
	ctx := context.Background()
	for round := 1; round <= 2; round++ {
		results, err := service.RunFingerprintRound(ctx, nil, "manual")
		if err != nil || len(results) != 3 {
			t.Fatalf("round %d: %v %d", round, err, len(results))
		}
		for _, r := range results {
			switch r.AccountID {
			case 1, 2:
				if r.Status != canary.StatusPass || r.Verdict != canary.VerdictMatch || r.TopModel != "grok-4.7" || r.Suspect || r.ValidOutputs != 3 || len(r.Candidates) != 3 {
					t.Fatalf("round %d healthy %#v", round, r)
				}
			case 3:
				if r.Status != canary.StatusFail || r.Verdict != canary.VerdictMismatch || r.Suspect != (round == 2) || r.ConsecutiveMismatch != round {
					t.Fatalf("round %d substituted %#v", round, r)
				}
			}
			if len(r.RawOutputs) != 3 || r.DurationMS != 3003 {
				t.Fatalf("raw/latency %#v", r)
			}
		}
	}
	for _, call := range prober.calls {
		if call.ReasoningEffort != "low" || call.Model != "grok-4.7" {
			t.Fatalf("call %#v", call)
		}
	}
	flags, err := service.AccountFlags(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 1 || flags[0].AccountID != 3 || !flags[0].SuspectSubstituted || flags[0].FingerprintTop == "" {
		t.Fatalf("flags %#v", flags)
	}
	summary, err := service.FingerprintSummary(ctx, 5)
	if err != nil || len(summary.Accounts) != 3 || !summary.ObserveOnly || summary.Enabled || len(summary.Accounts[2].History) != 2 {
		t.Fatalf("summary %v %#v", err, summary)
	}
	detail, err := service.FingerprintDetail(ctx, summary.Accounts[0].Latest.ID)
	if err != nil || len(detail.RawOutputs) != 3 || !strings.HasPrefix(detail.Prompts[0], "pick") {
		t.Fatalf("detail %v %#v", err, detail)
	}
}

func TestFingerprintInsufficientAndErrorsDoNotFail(t *testing.T) {
	samples := loadSamples(t)
	prober := &fpProber{samples: samples, as: map[uint64]string{1: "", 2: "grok-4.7"}, fail: map[uint64]bool{3: true}}
	service, _ := newFPService(t, prober, FingerprintConfig{Models: []string{"grok-4.5"}, FamilyTolerant: true})
	results, err := service.RunFingerprintRound(context.Background(), []uint64{1, 2, 3}, "manual")
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint64]canary.FingerprintResult{}
	for _, r := range results {
		got[r.AccountID] = r
	}
	if got[1].Status != canary.StatusSkipped || got[1].Verdict != canary.VerdictInsufficient {
		t.Fatalf("insufficient %#v", got[1])
	}
	if got[2].Status != canary.StatusPass || got[2].Verdict != canary.VerdictFamily {
		t.Fatalf("family %#v", got[2])
	}
	if got[3].Status != canary.StatusError || got[3].Suspect {
		t.Fatalf("error %#v", got[3])
	}
}

func TestFingerprintSamplingRotates(t *testing.T) {
	samples := loadSamples(t)
	prober := &fpProber{samples: samples, as: map[uint64]string{1: "grok-4.7", 2: "grok-4.7", 3: "grok-4.7"}, fail: map[uint64]bool{}}
	service, _ := newFPService(t, prober, FingerprintConfig{Models: []string{"grok-4.7"}, SampleAccounts: 1})
	var order []uint64
	for i := 0; i < 4; i++ {
		results, err := service.RunFingerprintRound(context.Background(), nil, "schedule")
		if err != nil || len(results) != 1 {
			t.Fatal(err)
		}
		order = append(order, results[0].AccountID)
	}
	if order[0] != 1 || order[1] != 2 || order[2] != 3 || order[3] != 1 {
		t.Fatalf("rotation %v", order)
	}
}

func TestAutoFingerprintAfterReasoningFailures(t *testing.T) {
	samples := loadSamples(t)
	prober := &fpProber{samples: samples, as: map[uint64]string{3: "gpt"}, fail: map[uint64]bool{}}
	service, store := newFPService(t, prober, FingerprintConfig{Models: []string{"grok-4.7"}, AutoAfterFails: 2})
	failing := []canary.Result{{AccountID: 3, PromptKind: canary.KindReasoning, Status: canary.StatusFail, ConsecutiveFails: 2}}
	service.maybeAutoFingerprint(context.Background(), "schedule", failing) // disabled + scheduled: no probe
	service.maybeAutoFingerprint(context.Background(), "manual", []canary.Result{{AccountID: 3, PromptKind: canary.KindReasoning, Status: canary.StatusFail, ConsecutiveFails: 1}})
	time.Sleep(50 * time.Millisecond)
	prober.mu.Lock()
	calls := len(prober.calls)
	prober.mu.Unlock()
	if calls != 0 {
		t.Fatalf("unexpected auto probe: %d", len(prober.calls))
	}
	service.maybeAutoFingerprint(context.Background(), "manual", failing)
	for i := 0; i < 200; i++ {
		if n, _ := store.CountFingerprintRounds(context.Background()); n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	service.fp.running.Lock() // wait for the background round to finish
	service.fp.running.Unlock()
	list, _ := store.ListFingerprints(context.Background(), 3, 10)
	if len(list) != 1 || list[0].Trigger != "auto" {
		t.Fatalf("auto probe %#v", list)
	}
}
