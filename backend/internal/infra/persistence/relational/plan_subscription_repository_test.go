package relational

import (
	"context"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
)

func TestPlanSubscriptionRoundTrip(t *testing.T) {
	ctx := context.Background()
	database := openTestDatabase(t)
	repo := NewAccountRepository(database)
	credential := createEgressOperationsProviderAccount(t, ctx, repo, account.ProviderWeb, "plan-web")
	end := time.Date(2026, 12, 9, 11, 20, 28, 0, time.UTC)
	value := account.PlanSubscription{AccountID: credential.ID, Plan: "supergrok.heavy", Status: "SUBSCRIPTION_STATUS_ACTIVE", PeriodEnd: &end, CancelAtPeriodEnd: true, Count: 1, SyncedAt: time.Now()}
	if err := repo.SavePlanSubscription(ctx, value); err != nil {
		t.Fatal(err)
	}
	value.CancelAtPeriodEnd = false
	if err := repo.SavePlanSubscription(ctx, value); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetPlanSubscriptions(ctx, []uint64{credential.ID, 999})
	if err != nil {
		t.Fatal(err)
	}
	plan, ok := got[credential.ID]
	if !ok || len(got) != 1 || plan.CancelAtPeriodEnd || plan.PeriodEnd == nil || !plan.PeriodEnd.Equal(end) || plan.Plan != "supergrok.heavy" {
		t.Fatalf("unexpected %+v", got)
	}
}
