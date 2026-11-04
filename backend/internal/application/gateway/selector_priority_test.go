package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
)

func priorityPlanOrder(t *testing.T, selector *Selector, values []account.RoutingCandidate) []uint64 {
	t.Helper()
	plan, err := selector.planCandidates(context.Background(), values, time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for candidate, ok := plan.Next(); ok; candidate, ok = plan.Next() {
		ids = append(ids, candidate.Credential.ID)
	}
	return ids
}

func upstreamWindow(remaining int) *account.QuotaWindow {
	return &account.QuotaWindow{Source: account.QuotaSourceUpstream, Remaining: remaining}
}

func TestNormalizePriorityMode(t *testing.T) {
	if NormalizePriorityMode("strict") != PriorityModeStrict || NormalizePriorityMode("") != PriorityModeSoft || NormalizePriorityMode("bogus") != PriorityModeSoft {
		t.Fatal("unexpected priority mode normalization")
	}
}

func TestStrictPriorityRanksPriorityBeforeQuotaSignals(t *testing.T) {
	limiter := &batchConcurrencyLimiter{values: map[string]int{}}
	selector := NewSelector(nil, limiter, nil, nil, time.Hour, time.Second, time.Minute)
	values := []account.RoutingCandidate{
		// 低优先级但额度已知可用：soft 模式下会排在前面。
		{Credential: account.Credential{ID: 1, Priority: 1, MaxConcurrent: 8}, QuotaWindow: upstreamWindow(10)},
		// 高优先级、额度未知。
		{Credential: account.Credential{ID: 2, Priority: 100, MaxConcurrent: 8}},
	}
	if got := priorityPlanOrder(t, selector, values); got[0] != 1 {
		t.Fatalf("soft mode order = %v, want quota-known account 1 first", got)
	}
	selector.UpdatePriorityMode(PriorityModeStrict, true)
	if got := priorityPlanOrder(t, selector, values); len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("strict mode order = %v, want [2 1]", got)
	}
}

func TestStrictPriorityWithinTierPrefersFewestInFlightThenLRU(t *testing.T) {
	limiter := &batchConcurrencyLimiter{values: map[string]int{"account:1": 2, "account:2": 0, "account:3": 0, "account:4": 0}}
	selector := NewSelector(nil, limiter, nil, nil, time.Hour, time.Second, time.Minute)
	selector.UpdatePriorityMode(PriorityModeStrict, true)
	now := time.Now().UTC()
	selector.lastSelectedAt[2] = now
	selector.lastSelectedAt[3] = now.Add(-time.Minute)
	values := []account.RoutingCandidate{
		{Credential: account.Credential{ID: 1, Priority: 50, MaxConcurrent: 8}},
		{Credential: account.Credential{ID: 2, Priority: 50, MaxConcurrent: 8}},
		{Credential: account.Credential{ID: 3, Priority: 50, MaxConcurrent: 8}},
		{Credential: account.Credential{ID: 4, Priority: 10, MaxConcurrent: 8}},
	}
	got := priorityPlanOrder(t, selector, values)
	want := []uint64{3, 2, 1, 4}
	for index := range want {
		if index >= len(got) || got[index] != want[index] {
			t.Fatalf("strict order = %v, want %v", got, want)
		}
	}
}

func TestStrictPriorityFallsBackToLowerTierWhenHigherSaturated(t *testing.T) {
	limiter := &batchConcurrencyLimiter{values: map[string]int{"account:1": 1, "account:2": 1, "account:3": 0}}
	selector := NewSelector(nil, limiter, nil, nil, time.Hour, time.Second, time.Minute)
	selector.UpdatePriorityMode(PriorityModeStrict, true)
	values := []account.RoutingCandidate{
		{Credential: account.Credential{ID: 1, Priority: 100, MaxConcurrent: 1}},
		{Credential: account.Credential{ID: 2, Priority: 100, MaxConcurrent: 1}},
		{Credential: account.Credential{ID: 3, Priority: 1, MaxConcurrent: 1}},
	}
	got := priorityPlanOrder(t, selector, values)
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("strict fallback order = %v, want [3]", got)
	}
}

func TestStrictPriorityExcludedHigherTierFallsThrough(t *testing.T) {
	limiter := &batchConcurrencyLimiter{values: map[string]int{}}
	selector := NewSelector(nil, limiter, nil, nil, time.Hour, time.Second, time.Minute)
	selector.UpdatePriorityMode(PriorityModeStrict, true)
	values := []account.RoutingCandidate{
		{Credential: account.Credential{ID: 1, Priority: 100, MaxConcurrent: 8}},
		{Credential: account.Credential{ID: 2, Priority: 50, MaxConcurrent: 8}},
		{Credential: account.Credential{ID: 3, Priority: 1, MaxConcurrent: 8}},
	}
	session := &selectionSession{selector: selector, values: values, normalCandidates: []int{0, 1, 2}}
	excluded := map[uint64]bool{1: true}
	if !session.hasHigherPriorityCandidate(excluded, 1) {
		t.Fatal("expected tier 50 to remain available above tier 1")
	}
	if session.hasHigherPriorityCandidate(excluded, 50) {
		t.Fatal("excluded tier-100 account must not count as available")
	}
	plan, err := selector.planCandidateIndexes(context.Background(), values, session.unexcludedNormalIndexes(excluded), time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := plan.Next()
	if !ok || first.Credential.ID != 2 {
		t.Fatalf("first after exclusion = %d, want 2", first.Credential.ID)
	}
}

func TestStrictPriorityOverridesStickyToggle(t *testing.T) {
	selector := NewSelector(nil, nil, nil, nil, time.Hour, time.Second, time.Minute)
	if selector.strictPriorityOverridesSticky() {
		t.Fatal("soft mode must not override sticky sessions")
	}
	selector.UpdatePriorityMode(PriorityModeStrict, true)
	if !selector.strictPriorityOverridesSticky() {
		t.Fatal("strict mode with override should yield sticky sessions")
	}
	selector.UpdatePriorityMode(PriorityModeStrict, false)
	if selector.strictPriorityOverridesSticky() {
		t.Fatal("override disabled should keep sticky sessions")
	}
}

func TestSegmentedCohortsStrictPriorityFirst(t *testing.T) {
	values := []account.RoutingCandidate{
		{Credential: account.Credential{ID: 1, Provider: account.ProviderBuild, Priority: 1}, QuotaWindow: upstreamWindow(5)},
		{Credential: account.Credential{ID: 2, Provider: account.ProviderBuild, Priority: 100}},
	}
	soft := segmentedCandidateCohorts(values, nil, time.Now().UTC(), nil, false, 0, 64, segmentedWindowsBeforeFullFallback)
	if len(soft) != 2 || values[soft[0].indexes[0]].Credential.ID != 1 {
		t.Fatalf("soft cohorts first = %#v", soft)
	}
	strict := segmentedCandidateCohorts(values, nil, time.Now().UTC(), nil, false, 0, 64, segmentedWindowsBeforeFullFallback, true)
	if len(strict) != 2 || values[strict[0].indexes[0]].Credential.ID != 2 {
		t.Fatalf("strict cohorts first = %#v", strict)
	}
}
