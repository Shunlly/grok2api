package relational

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLastRealUseByAccountsExcludesProbesAndFailures(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "lastuse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	id := func(v uint64) *uint64 { return &v }
	row := func(req string, account uint64, status int, at time.Time) requestAuditModel {
		return requestAuditModel{RequestID: req, ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", Operation: "responses", UsageSource: "upstream", StatusCode: status, AccountID: id(account), CreatedAt: at}
	}
	audits := []requestAuditModel{
		row("real1", 1, 200, base),
		row("quality_canary_x", 1, 200, base.Add(time.Hour)),
		row("fail", 1, 502, base.Add(2*time.Hour)),
		row("quality_probe", 2, 200, base),
	}
	for index := range audits {
		if err := database.db.WithContext(ctx).Create(&audits[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewAuditRepository(database).LastRealUseByAccounts(ctx, []uint64{1, 2, 3, 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[1].Equal(base) {
		t.Fatalf("got %v", got)
	}
}
