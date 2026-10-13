package relational

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	auditdomain "github.com/chenyme/grok2api/backend/internal/domain/audit"
	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

func TestAccountQualityFlagsOnlyRealHits(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "flags.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	audits := NewAuditRepository(database)
	now := time.Now().UTC()
	first := int64(100)
	degradedID, fastID, canaryID := uint64(1), uint64(2), uint64(3)
	if err := audits.CreateBatch(ctx, []auditdomain.Record{
		{RequestID: "deg", ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", AccountID: &degradedID, StatusCode: 200, Streaming: true, ErrorCode: auditdomain.ErrorQualityDegraded, CreatedAt: now.Add(-time.Hour)},
		{RequestID: "old", ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", AccountID: &degradedID, StatusCode: 200, Streaming: true, ErrorCode: auditdomain.ErrorQualityDegraded, CreatedAt: now.Add(-10 * 24 * time.Hour)},
		{RequestID: "fast", ClientKeyID: 1, ModelRouteID: 1, Provider: "grok_build", AccountID: &fastID, StatusCode: 200, Streaming: true, OutputTokens: 5000, FirstTokenMS: &first, DurationMS: 1100, CreatedAt: now.Add(-time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewCanaryRepository(database)
	for _, value := range []canary.Result{
		{RoundID: "r", Trigger: "schedule", AccountID: canaryID, Model: "m", PromptID: "p", PromptKind: "svg", Status: canary.StatusFail, Reasons: "wrong", CreatedAt: now.Add(-2 * time.Hour)},
		{RoundID: "r", Trigger: "schedule", AccountID: fastID, Model: "m", PromptID: "p", PromptKind: "svg", Status: canary.StatusPass, CreatedAt: now.Add(-2 * time.Hour)},
	} {
		if _, err := repo.Create(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	flags, err := repo.AccountQualityFlags(ctx, now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint64]canary.AccountFlag{}
	for _, flag := range flags {
		got[flag.AccountID] = flag
	}
	if len(got) != 2 || got[1].DegradedHits != 1 || got[1].LastDegradedAt == nil || got[3].CanaryFailures != 1 || got[3].LastCanaryReason != "wrong" {
		t.Fatalf("flags = %#v", flags)
	}
	if _, ok := got[2]; ok {
		t.Fatalf("speed-only / passing account must not be flagged: %#v", got[2])
	}
}
