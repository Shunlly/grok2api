package canary

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

type artifactMemStore struct {
	memStore
	artifacts map[uint64]canary.Artifact
}

func (m *artifactMemStore) CreateArtifact(_ context.Context, v canary.Artifact) error {
	m.artifacts[v.ResultID] = v
	return nil
}

func (m *artifactMemStore) GetResult(_ context.Context, id uint64) (canary.Result, *canary.Artifact, error) {
	for _, row := range m.rows {
		if row.ID == id {
			if a, ok := m.artifacts[id]; ok {
				return row, &a, nil
			}
			return row, nil, nil
		}
	}
	return canary.Result{}, nil, context.Canceled
}

func (m *artifactMemStore) DeleteArtifactsBefore(context.Context, time.Time) error { return nil }

func TestRoundStoresArtifactAndDetail(t *testing.T) {
	prober := &fakeProber{reasoning: map[uint64]int64{1: 2000, 2: 2100, 3: 2050}, fail: map[uint64]bool{3: true}}
	store := &artifactMemStore{memStore: memStore{rounds: map[string]bool{}}, artifacts: map[uint64]canary.Artifact{}}
	service := NewService(Config{Enabled: true}, 9, prober, fakeAccounts{}, store, nil)
	results, err := service.RunRound(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.ResultDetail(context.Background(), results[0].ID)
	if err != nil || detail.Artifact == nil {
		t.Fatalf("detail = %#v err=%v", detail, err)
	}
	a := detail.Artifact
	if a.Response != "Final answer: 401" || !strings.Contains(a.Prompt, "1000") || a.Expected != "401" || !strings.Contains(a.CheckDetail, "401") || a.ReasoningEffort != "high" {
		t.Fatalf("artifact = %#v", a)
	}
	failed, err := service.ResultDetail(context.Background(), results[2].ID)
	if err != nil || failed.Artifact == nil || !strings.HasPrefix(failed.Artifact.CheckDetail, "探测失败") ||
		failed.Artifact.ErrorCode != "canary_stream_idle_timeout" || failed.Artifact.StreamBytes != 120 || failed.Artifact.StreamChunks != 3 || failed.Artifact.Reasoning != "partial" {
		t.Fatalf("failed detail = %#v err=%v", failed, err)
	}
	// Results recorded before artifacts existed have no artifact.
	delete(store.artifacts, results[1].ID)
	old, err := service.ResultDetail(context.Background(), results[1].ID)
	if err != nil || old.Artifact != nil {
		t.Fatalf("old detail = %#v err=%v", old, err)
	}
	if _, err := service.ResultDetail(context.Background(), 999); err != ErrResultNotFound {
		t.Fatalf("missing err = %v", err)
	}
}

func TestTruncateBytesKeepsRuneBoundary(t *testing.T) {
	value, cut := canary.TruncateBytes("ab中文", 4)
	if value != "ab" || !cut {
		t.Fatalf("got %q %v", value, cut)
	}
	if value, cut := canary.TruncateBytes("abc", 10); value != "abc" || cut {
		t.Fatalf("got %q %v", value, cut)
	}
}

func TestIdleTimeoutDefaultsAndCap(t *testing.T) {
	s := NewService(Config{}, 1, &fakeProber{}, fakeAccounts{}, &memStore{rounds: map[string]bool{}}, nil)
	if s.cfg.IdleTimeout != 75*time.Second {
		t.Fatalf("idle = %v", s.cfg.IdleTimeout)
	}
	s = NewService(Config{ProbeTimeout: time.Minute, IdleTimeout: 2 * time.Minute}, 1, &fakeProber{}, fakeAccounts{}, &memStore{rounds: map[string]bool{}}, nil)
	if s.cfg.IdleTimeout != time.Minute {
		t.Fatalf("idle cap = %v", s.cfg.IdleTimeout)
	}
}
