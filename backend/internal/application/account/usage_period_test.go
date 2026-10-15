package account

import (
	"testing"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
)

func TestResolveUsagePeriod(t *testing.T) {
	now := time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)
	billing := &accountdomain.Billing{UsagePeriodStart: "2026-10-11T13:37:00Z", UsagePeriodEnd: "2026-10-18T13:37:00Z"}
	got := resolveUsagePeriod(billing, nil, now)
	if got.Source != "billingUsagePeriod" || !got.Start.Equal(time.Date(2026, 10, 11, 13, 37, 0, 0, time.UTC)) {
		t.Fatalf("billing %+v", got)
	}
	// 快照过期：向前滚动一个周期。
	stale := &accountdomain.Billing{UsagePeriodStart: "2026-10-01T00:00:00Z", UsagePeriodEnd: "2026-10-08T00:00:00Z"}
	got = resolveUsagePeriod(stale, nil, now)
	if !got.Start.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)) || !got.End.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rolled %+v", got)
	}
	reset := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	short := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	windows := []accountdomain.QuotaWindow{{Mode: "fast", WindowSeconds: 7200, ResetAt: &short}, {Mode: "weekly", WindowSeconds: 7 * 86400, ResetAt: &reset}}
	got = resolveUsagePeriod(nil, windows, now)
	if got.Source != "quotaWindow:weekly" || !got.Start.Equal(reset.Add(-7*24*time.Hour)) {
		t.Fatalf("window %+v", got)
	}
	got = resolveUsagePeriod(nil, nil, now)
	if got.Source != "rolling7d" || !got.End.IsZero() || !got.Start.Equal(now.Add(-7*24*time.Hour)) {
		t.Fatalf("fallback %+v", got)
	}
}
