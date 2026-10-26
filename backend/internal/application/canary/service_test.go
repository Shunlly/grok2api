package canary

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gatewayapp "github.com/chenyme/grok2api/backend/internal/application/gateway"
	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

type fakeProber struct {
	mu        sync.Mutex
	reasoning map[uint64]int64
	fail      map[uint64]bool
}

func (f *fakeProber) ProbeAccountCanary(_ context.Context, in gatewayapp.CanaryProbeInput) (gatewayapp.CanaryProbeOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[in.AccountID] {
		return gatewayapp.CanaryProbeOutput{ReasoningText: "partial", ReasoningChars: 7, StreamBytes: 120, StreamChunks: 3, ErrorCode: gatewayapp.CanaryErrIdleTimeout}, gatewayapp.ErrCanaryTransport
	}
	answer := "Final answer: " + answerFor(in.Prompt)
	return gatewayapp.CanaryProbeOutput{Text: answer, VisibleChars: int64(len(answer)), ReasoningTokens: f.reasoning[in.AccountID], OutputTokens: 1000}, nil
}

type fakeAccounts struct{}

func (fakeAccounts) ListEnabled(context.Context, accountdomain.Provider) ([]accountdomain.Credential, error) {
	return []accountdomain.Credential{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}}, nil
}

type memStore struct {
	rows   []canary.Result
	rounds map[string]bool
}

func (m *memStore) Create(_ context.Context, v canary.Result) (canary.Result, error) {
	v.ID = uint64(len(m.rows) + 1)
	m.rows = append([]canary.Result{v}, m.rows...)
	m.rounds[v.RoundID] = true
	return v, nil
}
func (m *memStore) ListRecent(_ context.Context, id uint64, limit int) ([]canary.Result, error) {
	out := []canary.Result{}
	for _, row := range m.rows {
		if id == 0 || row.AccountID == id {
			out = append(out, row)
		}
	}
	return out, nil
}
func (m *memStore) CountRounds(context.Context) (int64, error)    { return 0, nil } // always prompt 0
func (m *memStore) DeleteBefore(context.Context, time.Time) error { return nil }

func TestSuspectOnlyAfterThreeConsecutiveFailsWhilePoolPasses(t *testing.T) {
	prober := &fakeProber{reasoning: map[uint64]int64{1: 2000, 2: 2100, 3: 10}, fail: map[uint64]bool{}}
	store := &memStore{rounds: map[string]bool{}}
	service := NewService(Config{Enabled: true}, 9, prober, fakeAccounts{}, store, nil)
	ctx := context.Background()
	for round := 1; round <= 3; round++ {
		results, err := service.RunRound(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.AccountID == 3 {
				if r.Status != canary.StatusFail || r.Suspect != (round == 3) {
					t.Fatalf("round %d account 3 = %#v", round, r)
				}
			} else if r.Status != canary.StatusPass || r.Suspect {
				t.Fatalf("round %d healthy = %#v", round, r)
			}
		}
		if round == 1 {
			// A transport error in between must not reset or count.
			prober.fail[3] = true
			results, _ := service.RunRound(ctx, "test")
			if results[2].Status != canary.StatusError || results[2].ConsecutiveFails != 1 {
				t.Fatalf("transport error = %#v", results[2])
			}
			prober.fail[3] = false
		}
	}
	summary, err := service.Summary(ctx, 5)
	if err != nil || len(summary.Accounts) != 3 || !summary.Accounts[2].Latest.Suspect || !summary.ObserveOnly {
		t.Fatalf("summary = %#v err=%v", summary, err)
	}
}

func TestTriggerRejectsConcurrentRound(t *testing.T) {
	service := NewService(Config{}, 9, &fakeProber{}, fakeAccounts{}, &memStore{rounds: map[string]bool{}}, nil)
	service.running.Lock()
	if _, err := service.Trigger(context.Background()); !errors.Is(err, ErrRoundRunning) {
		t.Fatalf("err = %v", err)
	}
}

// answerFor returns the expected answer of a generated canary prompt.
func answerFor(prompt string) string {
	for seq := int64(0); seq < 1000; seq++ {
		if p := canary.Generate(seq); p.Text == prompt {
			return p.Expected
		}
	}
	return "0"
}
