package relational

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshAccountUsageStatsAccumulatesAcrossPurge(t *testing.T) {
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
	got, err := repo.RefreshAccountUsageStats(ctx, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalRequests != 3 || got.TotalFailedRequests != 1 || got.TotalTokens != 165 || got.TotalCostUSDTicks != 1300 || got.TotalUpstreamCostTicks != 1000 || got.TotalEstimatedTicks != 300 {
		t.Fatalf("totals %+v", got)
	}
	if got.TodayDate != "2026-10-15" || got.TodayRequests != 2 || got.TodayTokens != 55 || got.TodayCostUSDTicks != 300 {
		t.Fatalf("today %+v", got)
	}
	// 审计清理后累计值不回落，新请求继续累加；重复刷新不会重复计数。
	if err := database.db.WithContext(ctx).Exec("DELETE FROM request_audits WHERE request_id = 'old1'").Error; err != nil {
		t.Fatal(err)
	}
	insert(row("today2", 200, today.Add(time.Minute), 10, 1, 7, 0))
	for range 2 {
		got, err = repo.RefreshAccountUsageStats(ctx, 1, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.TotalRequests != 4 || got.TotalTokens != 176 || got.TotalCostUSDTicks != 1307 || got.TodayRequests != 3 {
		t.Fatalf("after purge %+v", got)
	}
	stats, err := repo.AccountUsageStatsByAccounts(ctx, []uint64{1, 2})
	if err != nil || len(stats) != 1 || stats[1].TotalRequests != 4 || stats[1].FirstAuditAt == nil {
		t.Fatalf("lookup %+v %v", stats, err)
	}
}
