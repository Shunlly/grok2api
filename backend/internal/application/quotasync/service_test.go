package quotasync

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	accountapp "github.com/chenyme/grok2api/backend/internal/application/account"
)

type fakeAccounts struct {
	mu      sync.Mutex
	targets []accountapp.AutoSyncTarget
	resets  map[uint64]time.Time
	synced  []uint64
	fail    map[uint64]bool
}

func (f *fakeAccounts) AutoSyncTargets(context.Context) ([]accountapp.AutoSyncTarget, error) {
	return append([]accountapp.AutoSyncTarget(nil), f.targets...), nil
}
func (f *fakeAccounts) AutoSyncAccount(_ context.Context, t accountapp.AutoSyncTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.synced = append(f.synced, t.ID)
	if f.fail[t.ID] {
		return errors.New("boom")
	}
	return nil
}
func (f *fakeAccounts) NextQuotaResets(context.Context, []uint64) (map[uint64]time.Time, error) {
	out := map[uint64]time.Time{}
	for k, v := range f.resets {
		out[k] = v
	}
	return out, nil
}

func newTestService(f *fakeAccounts, now time.Time) *Service {
	s := NewService(Config{Interval: time.Hour, Concurrency: 2}, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }
	return s
}

func TestRunOnceCountsFailuresWithoutStopping(t *testing.T) {
	f := &fakeAccounts{targets: []accountapp.AutoSyncTarget{{ID: 1, Billing: true}, {ID: 2}, {ID: 3}}, fail: map[uint64]bool{2: true}}
	s := newTestService(f, time.Now())
	st := s.RunOnce(context.Background(), "scheduled", nil)
	if st.LastTotal != 3 || st.LastSucceeded != 2 || st.LastFailed != 1 || len(f.synced) != 3 {
		t.Fatalf("unexpected status %+v synced=%v", st, f.synced)
	}
}

func TestPlanSchedulesResetSyncBeforeNextFullRun(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	reset := now.Add(20 * time.Minute)
	f := &fakeAccounts{targets: []accountapp.AutoSyncTarget{{ID: 1}, {ID: 2}}, resets: map[uint64]time.Time{1: reset, 2: now.Add(5 * time.Hour)}}
	s := newTestService(f, now)
	s.RunOnce(context.Background(), "scheduled", nil)
	wake, ids := s.plan(now.Add(time.Hour))
	if !wake.Equal(reset.Add(90*time.Second)) || len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("wake=%v ids=%v", wake, ids)
	}
	// After the reset sync the same boundary is not scheduled again.
	s.RunOnce(context.Background(), "period_reset", ids)
	if f.synced[len(f.synced)-1] != 1 || len(f.synced) != 3 {
		t.Fatalf("reset sync should only touch account 1: %v", f.synced)
	}
	wake, ids = s.plan(now.Add(time.Hour))
	if !wake.Equal(now.Add(time.Hour)) || ids != nil {
		t.Fatalf("expected regular run, wake=%v ids=%v", wake, ids)
	}
}

func TestPastBoundariesRightAfterSyncAreNotRescheduled(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	f := &fakeAccounts{targets: []accountapp.AutoSyncTarget{{ID: 1}}, resets: map[uint64]time.Time{1: now.Add(-time.Minute)}}
	s := newTestService(f, now)
	s.RunOnce(context.Background(), "scheduled", nil)
	if wake, ids := s.plan(now.Add(time.Hour)); ids != nil || !wake.Equal(now.Add(time.Hour)) {
		t.Fatalf("wake=%v ids=%v", wake, ids)
	}
}

func TestRunDisabledReturnsImmediately(t *testing.T) {
	s := NewService(Config{Interval: 0}, &fakeAccounts{}, nil)
	if err := s.Run(context.Background()); err != nil || s.Status().Enabled {
		t.Fatal("disabled scheduler should be a no-op")
	}
}
