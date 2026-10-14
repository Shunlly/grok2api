package relational

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

func TestCanaryArtifactRoundTripAndPrune(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "artifact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewCanaryRepository(database)
	now := time.Now().UTC()
	withArtifact, err := repo.Create(ctx, canary.Result{RoundID: "r1", Trigger: "manual", AccountID: 1, Model: "grok-4.7", PromptID: "svg-shapes", PromptKind: canary.KindSVG, Status: canary.StatusPass, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("<svg>", 50000)
	if err := repo.CreateArtifact(ctx, canary.Artifact{ResultID: withArtifact.ID, Prompt: "p", Response: big, InputTokens: 12, CheckDetail: "ok", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	legacy, err := repo.Create(ctx, canary.Result{RoundID: "r0", Trigger: "schedule", AccountID: 1, Model: "grok-4.7", PromptID: "x", PromptKind: canary.KindReasoning, Status: canary.StatusPass, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	result, artifact, err := repo.GetResult(ctx, withArtifact.ID)
	if err != nil || artifact == nil || artifact.Response != big || artifact.InputTokens != 12 || result.PromptID != "svg-shapes" {
		t.Fatalf("got %#v %v err=%v", result, artifact != nil, err)
	}
	if _, artifact, err := repo.GetResult(ctx, legacy.ID); err != nil || artifact != nil {
		t.Fatalf("legacy artifact = %v err=%v", artifact, err)
	}
	if _, _, err := repo.GetResult(ctx, 9999); err == nil {
		t.Fatal("missing result should error")
	}
	if err := repo.DeleteArtifactsBefore(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, artifact, _ := repo.GetResult(ctx, withArtifact.ID); artifact != nil {
		t.Fatal("artifact not pruned")
	}
}
