package relational

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

func TestFingerprintRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "fp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewCanaryRepository(database)
	now := time.Now().UTC()
	stored, err := repo.CreateFingerprint(ctx, canary.FingerprintResult{
		RoundID: "r1", Trigger: "manual", AccountID: 7, Model: "grok-4.7", Status: canary.StatusPass, Verdict: canary.VerdictMatch,
		TopModel: "grok-4.7", TopFamily: "grok", Confidence: 0.91,
		Candidates:  []canary.FingerprintCandidate{{Model: "grok-4.7", Family: "grok", Score: 1.2, Probability: 0.91}},
		ValidCounts: []int{300, 299, 0}, ValidOutputs: 2, Prompts: []string{"p1"}, RawOutputs: []string{"1 2 3"}, CreatedAt: now,
	})
	if err != nil || stored.ID == 0 {
		t.Fatal(err)
	}
	if _, err := repo.CreateFingerprint(ctx, canary.FingerprintResult{RoundID: "r2", Trigger: "auto", AccountID: 8, Model: "grok-4.5", Status: canary.StatusSkipped, CreatedAt: now.Add(-40 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListFingerprints(ctx, 0, 10)
	if err != nil || len(list) != 2 || list[0].ID != stored.ID {
		t.Fatalf("list %v %#v", err, list)
	}
	if list[0].RawOutputs != nil || len(list[0].Candidates) != 1 || list[0].ValidCounts[1] != 299 {
		t.Fatalf("list row %#v", list[0])
	}
	got, err := repo.GetFingerprint(ctx, stored.ID)
	if err != nil || len(got.RawOutputs) != 1 || got.RawOutputs[0] != "1 2 3" || got.Prompts[0] != "p1" {
		t.Fatalf("get %v %#v", err, got)
	}
	if n, _ := repo.CountFingerprintRounds(ctx); n != 2 {
		t.Fatalf("rounds %d", n)
	}
	if err := repo.DeleteFingerprintsBefore(ctx, now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.ListFingerprints(ctx, 8, 10); len(list) != 0 {
		t.Fatal("prune failed")
	}
}
