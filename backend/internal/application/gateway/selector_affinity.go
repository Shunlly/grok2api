package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
)

// Default affinity spill-over thresholds (gateway.affinity).
const (
	DefaultAffinityBusyCap = 3
	DefaultAffinityBusyGap = 2
	// affinitySpillScanLimit bounds how many alternative candidates are read per
	// sticky hit so very large pools do not pay a full concurrency scan.
	affinitySpillScanLimit = 64
)

// AffinityConfig controls when a sticky (session-affinity) request borrows a
// less loaded account. 0 disables the corresponding rule.
type AffinityConfig struct {
	BusyCap int
	BusyGap int
}

// UpdateAffinity hot-updates the spill-over thresholds; negatives become 0.
func (s *Selector) UpdateAffinity(value AffinityConfig) {
	s.configMu.Lock()
	s.affinityBusyCap = max(0, value.BusyCap)
	s.affinityBusyGap = max(0, value.BusyGap)
	s.configMu.Unlock()
}

func (s *Selector) affinityConfig() (int, int) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.affinityBusyCap, s.affinityBusyGap
}

type affinityAlternative struct {
	candidate account.RoutingCandidate
	inFlight  int
}

// affinitySpillTargets decides whether the sticky account is busy enough to
// lend this request elsewhere. It returns alternatives ordered by in-flight
// count (least loaded first), or nil to stay on the sticky account. Rules:
//   - cap: sticky in-flight >= busyCap and some alternative is below busyCap;
//   - gap: sticky in-flight - least alternative >= busyGap (alternative must
//     also be below busyCap when the cap is enabled).
//
// When every alternative is at/over the cap the request stays sticky.
func (s *Selector) affinitySpillTargets(ctx context.Context, values []account.RoutingCandidate, indexes []int, stickyID uint64) ([]affinityAlternative, int) {
	busyCap, busyGap := s.affinityConfig()
	if (busyCap <= 0 && busyGap <= 0) || s.concurrency == nil {
		return nil, 0
	}
	// Cheap pre-check on the sticky account alone: when it is below both
	// thresholds no rule can fire, so the common path skips the pool scan.
	stickyNow, err := s.concurrency.Current(ctx, accountConcurrencyKey(stickyID))
	if err != nil {
		return nil, 0
	}
	if (busyCap <= 0 || stickyNow < busyCap) && (busyGap <= 0 || stickyNow < busyGap) {
		return nil, stickyNow
	}
	alternatives := make([]account.RoutingCandidate, 0, min(len(indexes), affinitySpillScanLimit))
	for _, index := range indexes {
		candidate := values[index]
		if candidate.Credential.ID == stickyID {
			continue
		}
		alternatives = append(alternatives, candidate)
		if len(alternatives) >= affinitySpillScanLimit {
			break
		}
	}
	if len(alternatives) == 0 {
		return nil, 0
	}
	keys := make([]string, 0, len(alternatives)+1)
	keys = append(keys, accountConcurrencyKey(stickyID))
	for _, candidate := range alternatives {
		keys = append(keys, accountConcurrencyKey(candidate.Credential.ID))
	}
	// Fresh read (not the short-lived plan snapshot): the decision is about
	// leases this very session just opened.
	current, err := s.readConcurrency(ctx, keys)
	if err != nil {
		return nil, 0
	}
	stickyInFlight := current[keys[0]]
	result := make([]affinityAlternative, 0, len(alternatives))
	for index, candidate := range alternatives {
		inFlight := current[keys[index+1]]
		limit := candidate.Credential.MaxConcurrent
		if limit <= 0 {
			limit = account.DefaultMaxConcurrent
		}
		if inFlight >= limit || (busyCap > 0 && inFlight >= busyCap) {
			continue
		}
		result = append(result, affinityAlternative{candidate: candidate, inFlight: inFlight})
	}
	if len(result) == 0 {
		return nil, stickyInFlight
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].inFlight < result[j].inFlight })
	least := result[0].inFlight
	overCap := busyCap > 0 && stickyInFlight >= busyCap
	overGap := busyGap > 0 && stickyInFlight-least >= busyGap
	if !overCap && !overGap {
		return nil, stickyInFlight
	}
	return result, stickyInFlight
}

func (s *Selector) readConcurrency(ctx context.Context, keys []string) (map[string]int, error) {
	if reader, ok := s.concurrency.(interface {
		CurrentMany(context.Context, []string) (map[string]int, error)
	}); ok {
		return reader.CurrentMany(ctx, keys)
	}
	values := make(map[string]int, len(keys))
	for _, key := range keys {
		value, err := s.concurrency.Current(ctx, key)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

// tryAffinitySpill claims a slot on the least loaded alternative without
// touching the sticky binding. It returns nil when no alternative could be
// claimed, so the caller falls back to the sticky account.
func (s *Selector) tryAffinitySpill(ctx context.Context, values []account.RoutingCandidate, indexes []int, stickyID uint64, quotaMode string, materialFailures *credentialMaterialFailureTracker) (*accountLease, error) {
	targets, stickyInFlight := s.affinitySpillTargets(ctx, values, indexes, stickyID)
	for _, target := range targets {
		lease, err := s.claimAccountSlotTracked(ctx, target.candidate.Credential, materialFailures)
		if err != nil {
			if errors.Is(err, errRoutingCredentialStale) {
				continue
			}
			return nil, err
		}
		if lease == nil {
			continue
		}
		lease.Billing = target.candidate.Billing
		lease.QuotaMode = effectiveQuotaMode(target.candidate, quotaMode)
		busyCap, busyGap := s.affinityConfig()
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Info("affinity_spillover", slog.Uint64("sticky_account_id", stickyID), slog.Int("sticky_in_flight", stickyInFlight),
			slog.Uint64("account_id", target.candidate.Credential.ID), slog.Int("account_in_flight", target.inFlight),
			slog.Int("busy_cap", busyCap), slog.Int("busy_gap", busyGap))
		return lease, nil
	}
	return nil, nil
}
