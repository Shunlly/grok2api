package account

import (
	"context"
	"fmt"
	"time"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

// AutoSyncTarget is one account eligible for the scheduled quota sync.
type AutoSyncTarget struct {
	ID      uint64
	Billing bool
}

// AutoSyncTargets lists enabled, active (not reauth) accounts whose provider
// exposes upstream quota (billing or remote windows). Local-window providers
// have nothing to fetch and are skipped.
func (s *Service) AutoSyncTargets(ctx context.Context) ([]AutoSyncTarget, error) {
	if s.providers == nil {
		return nil, fmt.Errorf("Provider 注册表未初始化")
	}
	targets := make([]AutoSyncTarget, 0)
	for _, providerValue := range s.providers.Providers() {
		kind, ok := s.providers.QuotaKind(providerValue)
		if !ok || (kind != provider.QuotaBilling && kind != provider.QuotaRemoteWindow) {
			continue
		}
		ids, err := s.accounts.ListEnabledAccountIDs(ctx, providerValue, false)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			targets = append(targets, AutoSyncTarget{ID: id, Billing: kind == provider.QuotaBilling})
		}
	}
	return targets, nil
}

// AutoSyncAccount refreshes one target through the same path as the manual
// "sync all" buttons (which also refreshes account_usage_stats).
func (s *Service) AutoSyncAccount(ctx context.Context, target AutoSyncTarget) error {
	if target.Billing {
		_, err := s.RefreshBilling(ctx, target.ID)
		return err
	}
	_, err := s.RefreshQuota(ctx, target.ID)
	return err
}

// NextQuotaResets returns, per account, the earliest known upcoming-or-past
// boundary (raw, not rolled): Build billing usage_period_end and every quota
// window reset_at. Accounts without any known boundary are omitted.
func (s *Service) NextQuotaResets(ctx context.Context, ids []uint64) (map[uint64]time.Time, error) {
	result := make(map[uint64]time.Time, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	windows, err := s.accounts.GetQuotaWindows(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		var candidates []time.Time
		if billing, err := s.accounts.GetBilling(ctx, id); err == nil {
			if end, ok := parseRFC3339Loose(billing.UsagePeriodEnd); ok {
				candidates = append(candidates, end)
			}
		}
		for _, w := range windows[id] {
			if w.ResetAt != nil && !w.ResetAt.IsZero() {
				candidates = append(candidates, w.ResetAt.UTC())
			}
		}
		if t, ok := earliestReset(candidates); ok {
			result[id] = t
		}
	}
	return result, nil
}

func earliestReset(values []time.Time) (time.Time, bool) {
	var best time.Time
	for _, v := range values {
		if best.IsZero() || v.Before(best) {
			best = v
		}
	}
	return best, !best.IsZero()
}

// AutoSyncSupported reports whether the scheduled quota sync covers accounts
// of this provider (billing or remote-window quota).
func (s *Service) AutoSyncSupported(providerValue string) bool {
	if s == nil || s.providers == nil {
		return false
	}
	for _, p := range s.providers.Providers() {
		if string(p) != providerValue {
			continue
		}
		kind, ok := s.providers.QuotaKind(p)
		return ok && (kind == provider.QuotaBilling || kind == provider.QuotaRemoteWindow)
	}
	return false
}
