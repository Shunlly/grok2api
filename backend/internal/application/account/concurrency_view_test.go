package account

import (
	"context"
	"testing"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

func TestConcurrencyOverviewCountsActiveAccountLeases(t *testing.T) {
	ctx := context.Background()
	service, repo := newAutoCleanTestService(t, time.Now())
	build := mustUpsert(t, repo, accountdomain.Credential{Provider: accountdomain.ProviderBuild, Name: "b", SourceKey: "b", EncryptedAccessToken: "x", Enabled: true})
	web := mustUpsert(t, repo, accountdomain.Credential{Provider: accountdomain.ProviderWeb, Name: "w", SourceKey: "w", EncryptedAccessToken: "x", Enabled: true})
	idle := mustUpsert(t, repo, accountdomain.Credential{Provider: accountdomain.ProviderBuild, Name: "i", SourceKey: "i", EncryptedAccessToken: "x", Enabled: true})

	empty, err := service.ConcurrencyOverview(ctx)
	if err != nil || empty.Total != 0 {
		t.Fatalf("without limiter: %+v %v", empty, err)
	}

	limiter := memory.NewConcurrencyLimiter()
	service.SetConcurrencyLimiter(limiter)
	var releases []func()
	for _, id := range []uint64{build.ID, build.ID, web.ID} {
		release, ok, err := limiter.Acquire(ctx, repository.AccountConcurrencyKey(id), 10)
		if err != nil || !ok {
			t.Fatalf("acquire: %v %v", ok, err)
		}
		releases = append(releases, release)
	}

	current, err := service.CurrentConcurrency(ctx, []uint64{build.ID, web.ID, idle.ID})
	if err != nil {
		t.Fatal(err)
	}
	if current[build.ID] != 2 || current[web.ID] != 1 || current[idle.ID] != 0 {
		t.Fatalf("unexpected per-account: %v", current)
	}
	overview, err := service.ConcurrencyOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Total != 3 || overview.ActiveAccounts != 2 {
		t.Fatalf("unexpected overview: %+v", overview)
	}

	for _, release := range releases {
		release()
	}
	overview, _ = service.ConcurrencyOverview(ctx)
	if overview.Total != 0 || overview.ActiveAccounts != 0 {
		t.Fatalf("after release: %+v", overview)
	}
}
