package relational

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardFirstTokenStatsExcludeProbesAndBucketByInput(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "ttft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	ms := func(v int64) *int64 { return &v }
	row := func(id string, input, ttft int64) requestAuditModel {
		return requestAuditModel{RequestID: id, ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", Operation: "responses", UsageSource: "upstream", StatusCode: 200, Streaming: true, InputTokens: input, OutputTokens: 10, FirstTokenMS: ms(ttft), DurationMS: ttft + 1000, CreatedAt: now.Add(-time.Hour)}
	}
	audits := []requestAuditModel{
		row("a", 1_000, 1_000), row("b", 5_000, 3_000),
		row("c", 80_000, 18_000), row("d", 150_000, 29_000), row("e", 250_000, 37_000),
		row("quality_probe1", 50, 500), row("quality_canary_x", 50, 400),
	}
	for index := range audits {
		if err := database.db.WithContext(ctx).Create(&audits[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := NewDashboardRepository(database).Snapshot(ctx, testDashboardWindow(testDashboardBoundaries(now.Add(-24*time.Hour), 2*time.Hour, 12)), now)
	if err != nil {
		t.Fatal(err)
	}
	u := snapshot.Usage
	if u.FirstTokenSamples != 5 || u.FirstTokenTotalMS != 88_000 || u.FirstTokenMedianMS != 18_000 || u.FirstTokenP90MS != 37_000 {
		t.Fatalf("ttft = samples %d total %d median %d p90 %d", u.FirstTokenSamples, u.FirstTokenTotalMS, u.FirstTokenMedianMS, u.FirstTokenP90MS)
	}
	want := map[string][2]int64{"lt20k": {2, 1_000}, "20k-60k": {0, 0}, "60k-120k": {1, 18_000}, "120k-200k": {1, 29_000}, "gt200k": {1, 37_000}}
	if len(u.FirstTokenBuckets) != 5 {
		t.Fatalf("buckets = %#v", u.FirstTokenBuckets)
	}
	for _, b := range u.FirstTokenBuckets {
		if w := want[b.Key]; b.Samples != w[0] || b.MedianMS != w[1] {
			t.Fatalf("bucket %s = %#v want %v", b.Key, b, w)
		}
	}
}
