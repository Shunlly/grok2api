// Package quotasync runs the scheduled background quota / billing / usage
// stats sync. It reuses the account service's manual sync path, staggers
// upstream requests with jitter and only logs failures: account health is
// changed exclusively by the existing sync logic.
package quotasync

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	accountapp "github.com/chenyme/grok2api/backend/internal/application/account"
)

// Accounts is the subset of the account service used by the scheduler.
type Accounts interface {
	AutoSyncTargets(context.Context) ([]accountapp.AutoSyncTarget, error)
	AutoSyncAccount(context.Context, accountapp.AutoSyncTarget) error
	NextQuotaResets(context.Context, []uint64) (map[uint64]time.Time, error)
}

type Config struct {
	// Interval between full syncs; 0 disables the scheduler.
	Interval     time.Duration
	InitialDelay time.Duration
	// ResetDelay is how long after a known reset boundary the extra sync runs.
	ResetDelay time.Duration
	// Concurrency bounds parallel upstream calls (default 2).
	Concurrency int
	// MaxJitter is the random pause before each account request.
	MaxJitter time.Duration
	// AccountTimeout bounds one account's sync.
	AccountTimeout time.Duration
}

// Status is exposed to the admin UI.
type Status struct {
	Enabled         bool       `json:"enabled"`
	IntervalSeconds int64      `json:"intervalSeconds"`
	Running         bool       `json:"running"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
	LastFinishedAt  *time.Time `json:"lastFinishedAt,omitempty"`
	LastReason      string     `json:"lastReason,omitempty"`
	LastTotal       int        `json:"lastTotal"`
	LastSucceeded   int        `json:"lastSucceeded"`
	LastFailed      int        `json:"lastFailed"`
	NextRunAt       *time.Time `json:"nextRunAt,omitempty"`
	NextResetSyncAt *time.Time `json:"nextResetSyncAt,omitempty"`
}

type Service struct {
	cfg      Config
	accounts Accounts
	logger   *slog.Logger
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error

	mu      sync.Mutex
	status  Status
	resets  map[uint64]time.Time // account -> raw reset boundary still pending
	handled map[uint64]time.Time // account -> boundary already re-synced
}

func NewService(cfg Config, accounts Accounts, logger *slog.Logger) *Service {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.ResetDelay <= 0 {
		cfg.ResetDelay = 90 * time.Second
	}
	if cfg.AccountTimeout <= 0 {
		cfg.AccountTimeout = 2 * time.Minute
	}
	if cfg.MaxJitter < 0 {
		cfg.MaxJitter = 0
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg: cfg, accounts: accounts, logger: logger, now: time.Now, sleep: sleepCtx,
		status:  Status{Enabled: cfg.Interval > 0, IntervalSeconds: int64(cfg.Interval / time.Second)},
		resets:  map[uint64]time.Time{},
		handled: map[uint64]time.Time{},
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Run blocks until ctx ends. It returns immediately when disabled.
func (s *Service) Run(ctx context.Context) error {
	if s == nil || s.cfg.Interval <= 0 || s.accounts == nil {
		return nil
	}
	nextFull := s.now().Add(s.cfg.InitialDelay)
	s.logger.Info("quota_auto_sync_started", "interval", s.cfg.Interval.String(), "first_run_at", nextFull.Format(time.RFC3339))
	for {
		wake, resetDue := s.plan(nextFull)
		if err := s.sleep(ctx, wake.Sub(s.now())); err != nil {
			return nil
		}
		now := s.now()
		if !now.Before(nextFull) {
			s.RunOnce(ctx, "scheduled", nil)
			nextFull = s.now().Add(s.cfg.Interval)
			continue
		}
		if len(resetDue) > 0 {
			s.RunOnce(ctx, "period_reset", resetDue)
		}
	}
}

// plan picks the next wake-up time and the accounts whose reset sync is due
// at that moment (nil when the wake-up is the regular full sync).
func (s *Service) plan(nextFull time.Time) (time.Time, []uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	earliest := time.Time{}
	for id, at := range s.resets {
		if done, ok := s.handled[id]; ok && done.Equal(at) {
			continue
		}
		due := at.Add(s.cfg.ResetDelay)
		if !due.After(now) && at.Before(now.Add(-24*time.Hour)) {
			// Stale boundary upstream never advanced; the hourly sync covers it.
			continue
		}
		if earliest.IsZero() || due.Before(earliest) {
			earliest = due
		}
	}
	nf := nextFull
	s.status.NextRunAt = &nf
	s.status.NextResetSyncAt = nil
	if earliest.IsZero() || !earliest.Before(nextFull) {
		return nextFull, nil
	}
	if earliest.Before(now) {
		earliest = now
	}
	e := earliest
	s.status.NextResetSyncAt = &e
	var ids []uint64
	for id, at := range s.resets {
		if done, ok := s.handled[id]; ok && done.Equal(at) {
			continue
		}
		if !at.Add(s.cfg.ResetDelay).After(earliest) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return earliest, ids
}

// RunOnce syncs all targets (only == nil) or just the listed account ids.
func (s *Service) RunOnce(ctx context.Context, reason string, only []uint64) Status {
	started := s.now()
	s.mu.Lock()
	s.status.Running = true
	s.status.LastRunAt = &started
	s.status.LastReason = reason
	if only != nil {
		for _, id := range only {
			s.handled[id] = s.resets[id]
		}
	}
	s.mu.Unlock()

	targets, err := s.accounts.AutoSyncTargets(ctx)
	if err != nil {
		s.logger.Warn("quota_auto_sync_list_failed", "reason", reason, "error", err)
	}
	if only != nil {
		wanted := make(map[uint64]bool, len(only))
		for _, id := range only {
			wanted[id] = true
		}
		filtered := targets[:0]
		for _, t := range targets {
			if wanted[t.ID] {
				filtered = append(filtered, t)
			}
		}
		targets = filtered
	}
	succeeded, failed := s.syncTargets(ctx, reason, targets)
	s.refreshResets(ctx)

	finished := s.now()
	s.mu.Lock()
	s.status.Running = false
	s.status.LastFinishedAt = &finished
	s.status.LastTotal = len(targets)
	s.status.LastSucceeded = succeeded
	s.status.LastFailed = failed
	out := s.status
	s.mu.Unlock()
	s.logger.Info("quota_auto_sync_finished", "reason", reason, "total", len(targets), "succeeded", succeeded, "failed", failed, "elapsed", finished.Sub(started).Round(time.Millisecond).String())
	return out
}

func (s *Service) syncTargets(ctx context.Context, reason string, targets []accountapp.AutoSyncTarget) (int, int) {
	var mu sync.Mutex
	succeeded, failed := 0, 0
	sem := make(chan struct{}, s.cfg.Concurrency)
	var wg sync.WaitGroup
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(target accountapp.AutoSyncTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			if s.cfg.MaxJitter > 0 {
				if err := s.sleep(ctx, time.Duration(rand.Int64N(int64(s.cfg.MaxJitter)))); err != nil {
					return
				}
			}
			workCtx, cancel := context.WithTimeout(ctx, s.cfg.AccountTimeout)
			err := s.accounts.AutoSyncAccount(workCtx, target)
			cancel()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				s.logger.Warn("quota_auto_sync_account_failed", "reason", reason, "account_id", target.ID, "error", err)
				return
			}
			succeeded++
		}(target)
	}
	wg.Wait()
	return succeeded, failed
}

func (s *Service) refreshResets(ctx context.Context) {
	targets, err := s.accounts.AutoSyncTargets(ctx)
	if err != nil {
		return
	}
	ids := make([]uint64, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.ID)
	}
	resets, err := s.accounts.NextQuotaResets(ctx, ids)
	if err != nil {
		s.logger.Warn("quota_auto_sync_resets_failed", "error", err)
		return
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resets = resets
	for id, at := range resets {
		// Boundaries already in the past right after a sync were just covered.
		if !at.After(now) {
			s.handled[id] = at
		}
	}
	for id := range s.handled {
		if _, ok := resets[id]; !ok {
			delete(s.handled, id)
		}
	}
}

// ResetDelay is the pause after a reset boundary before the catch-up sync.
func (s *Service) ResetDelay() time.Duration {
	if s == nil {
		return 0
	}
	return s.cfg.ResetDelay
}

// PendingResetSyncs returns, per account, when its reset catch-up sync is
// scheduled (boundary + ResetDelay), including ones after the next hourly
// run. Already handled or stale boundaries are omitted.
func (s *Service) PendingResetSyncs() map[uint64]time.Time {
	out := map[uint64]time.Time{}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for id, at := range s.resets {
		if done, ok := s.handled[id]; ok && done.Equal(at) {
			continue
		}
		due := at.Add(s.cfg.ResetDelay)
		if !due.After(now) && at.Before(now.Add(-24*time.Hour)) {
			continue
		}
		if due.Before(now) {
			due = now
		}
		out[id] = due
	}
	return out
}
