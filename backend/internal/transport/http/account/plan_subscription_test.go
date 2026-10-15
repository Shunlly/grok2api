package account

import (
	"testing"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
)

func TestApplyPlanSubscription(t *testing.T) {
	end := time.Date(2026, 12, 9, 11, 20, 28, 0, time.UTC)
	var result accountResponse
	applyPlanSubscription(&result, &accountdomain.PlanSubscription{Plan: "supergrok.heavy", Status: "SUBSCRIPTION_STATUS_ACTIVE", PeriodEnd: &end, CancelAtPeriodEnd: true, Count: 1, SyncedAt: end}, "linkedWebSubscriptions")
	if result.PlanExpiresAt == nil || !result.PlanExpiresAt.Equal(end) || result.PlanAutoRenew || !result.PlanCancelAtEnd || result.PlanExpiresSource != "linkedWebSubscriptions" {
		t.Fatalf("unexpected %+v", result)
	}
	var free accountResponse
	applyPlanSubscription(&free, &accountdomain.PlanSubscription{Count: 0, SyncedAt: end}, "webSubscriptions")
	if !free.PlanSubscriptionNo || free.PlanExpiresAt != nil {
		t.Fatalf("free %+v", free)
	}
	var none accountResponse
	applyPlanSubscription(&none, nil, "")
	if none.PlanExpiresAt != nil || none.PlanSyncedAt != nil {
		t.Fatal("nil plan must leave fields empty")
	}
}
