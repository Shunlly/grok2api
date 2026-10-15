package relational

import (
	"context"

	"github.com/chenyme/grok2api/backend/internal/repository"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshAccountUsageStatsCoversCurrentPeriod(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	// 2026-10-15 00:30 北京时间 = 2026-10-14 16:30 UTC
	now := time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC)
	yesterday := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC) // 10-14 23:00 UTC+8
	today := time.Date(2026, 10, 14, 16, 30, 0, 0, time.UTC)
	id := func(v uint64) *uint64 { return &v }
	row := func(req string, status int, at time.Time, in, out, cost, est int64) requestAuditModel {
		return requestAuditModel{RequestID: req, ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", Operation: "responses", UsageSource: "upstream", StatusCode: status, AccountID: id(1), CreatedAt: at,
			InputTokens: in, OutputTokens: out, TotalTokens: in + out, CostInUSDTicks: cost, EstimatedCostInUSDTicks: est}
	}
	insert := func(rows ...requestAuditModel) {
		for i := range rows {
			if err := database.db.WithContext(ctx).Create(&rows[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	insert(
		row("old1", 200, yesterday, 100, 10, 1000, 0),
		row("quality_canary_1", 200, today, 999, 999, 999, 0),
		row("today1", 200, today, 50, 5, 0, 300),
		row("fail1", 502, today, 0, 0, 0, 0),
	)
	repo := NewAuditRepository(database)
	periodStart := time.Date(2026, 10, 14, 15, 30, 0, 0, time.UTC) // 周期从 old1 之后开始
	period := repository.UsagePeriod{Start: periodStart, End: periodStart.Add(7 * 24 * time.Hour), Source: "billingUsagePeriod"}
	got, err := repo.RefreshAccountUsageStats(ctx, 1, period, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalRequests != 2 || got.TotalFailedRequests != 1 || got.TotalTokens != 55 || got.TotalCostUSDTicks != 300 || got.TotalEstimatedTicks != 300 {
		t.Fatalf("period totals %+v", got)
	}
	if got.PeriodStart == nil || !got.PeriodStart.Equal(periodStart) || got.PeriodEnd == nil || got.PeriodSource != "billingUsagePeriod" {
		t.Fatalf("period meta %+v", got)
	}
	if got.TodayDate != "2026-10-15" || got.TodayRequests != 2 || got.TodayTokens != 55 || got.TodayCostUSDTicks != 300 {
		t.Fatalf("today %+v", got)
	}
	if got.TodayStart == nil || !got.TodayStart.Equal(time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("today start should be midnight %+v", got.TodayStart)
	}
	// 重复刷新幂等；更早的周期包含 old1。
	insert(row("today2", 200, today.Add(time.Minute), 10, 1, 7, 0))
	wide := repository.UsagePeriod{Start: now.Add(-7 * 24 * time.Hour), Source: "rolling7d"}
	for range 2 {
		got, err = repo.RefreshAccountUsageStats(ctx, 1, wide, now.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.TotalRequests != 4 || got.TotalTokens != 176 || got.TotalCostUSDTicks != 1307 || got.PeriodEnd != nil {
		t.Fatalf("wide %+v", got)
	}
	// 周期滚动：新周期内没有请求时归零。
	rolled := repository.UsagePeriod{Start: now.Add(time.Hour), Source: "billingUsagePeriod"}
	got, err = repo.RefreshAccountUsageStats(ctx, 1, rolled, now.Add(2*time.Hour))
	if err != nil || got.TotalRequests != 0 || got.TotalCostUSDTicks != 0 {
		t.Fatalf("rolled %+v %v", got, err)
	}
	stats, err := repo.AccountUsageStatsByAccounts(ctx, []uint64{1, 2})
	if err != nil || len(stats) != 1 || stats[1].TotalRequests != 0 || stats[1].PeriodStart == nil {
		t.Fatalf("lookup %+v %v", stats, err)
	}
}

func TestRefreshAccountUsageStatsTodayClampedToPeriod(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	id := uint64(1)
	mk := func(req string, at time.Time, cost int64) {
		m := requestAuditModel{RequestID: req, ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", Operation: "responses", UsageSource: "upstream", StatusCode: 200, AccountID: &id, CreatedAt: at, InputTokens: 10, TotalTokens: 10, CostInUSDTicks: cost}
		if err := database.db.WithContext(ctx).Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 北京时间 10-15：01:00 一条（上周期），周期 02:05 重置，03:00 一条（本周期）。
	mk("before_reset", time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC), 1000)
	mk("after_reset", time.Date(2026, 10, 14, 19, 0, 0, 0, time.UTC), 7)
	repo := NewAuditRepository(database)
	periodStart := time.Date(2026, 10, 14, 18, 5, 0, 0, time.UTC)
	now := time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)
	got, err := repo.RefreshAccountUsageStats(ctx, 1, repository.UsagePeriod{Start: periodStart, Source: "billingUsagePeriod"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.TodayRequests != 1 || got.TodayCostUSDTicks != 7 || got.TotalCostUSDTicks != 7 || got.TodayStart == nil || !got.TodayStart.Equal(periodStart) {
		t.Fatalf("clamped today %+v", got)
	}
	// 周期早于今日零点：今日从零点起算。
	got, err = repo.RefreshAccountUsageStats(ctx, 1, repository.UsagePeriod{Start: now.Add(-72 * time.Hour), Source: "rolling7d"}, now)
	if err != nil {
		t.Fatal(err)
	}
	midnight := time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)
	if got.TodayRequests != 2 || got.TodayCostUSDTicks != 1007 || !got.TodayStart.Equal(midnight) {
		t.Fatalf("midnight today %+v", got)
	}
	if got.TodayCostUSDTicks > got.TotalCostUSDTicks {
		t.Fatalf("today exceeds period %+v", got)
	}
}
