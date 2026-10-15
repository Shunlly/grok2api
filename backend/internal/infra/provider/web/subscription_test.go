package web

import "testing"

func TestParsePlanSubscription(t *testing.T) {
	body := []byte(`{"dominantPlan":{"plan":"supergrok.heavy"},"subscriptions":[
	 {"status":"SUBSCRIPTION_STATUS_EXPIRED","tier":"SUBSCRIPTION_TIER_SUPER_GROK","billingPeriodEnd":"2027-01-01T00:00:00Z"},
	 {"billingInterval":"BILLING_INTERVAL_MONTHLY","billingPeriodEnd":"2026-12-09T11:20:28.012Z","billingSystem":"BILLING_SYSTEM_GOOGLE","cancelAtPeriodEnd":true,"status":"SUBSCRIPTION_STATUS_ACTIVE","tier":"SUBSCRIPTION_TIER_SUPER_GROK_PRO"}]}`)
	value, err := parsePlanSubscription(body)
	if err != nil {
		t.Fatal(err)
	}
	if value.Plan != "supergrok.heavy" || value.Status != "SUBSCRIPTION_STATUS_ACTIVE" || !value.CancelAtPeriodEnd || value.Count != 2 {
		t.Fatalf("unexpected %+v", value)
	}
	if value.PeriodEnd == nil || value.PeriodEnd.Format("2006-01-02T15:04:05") != "2026-12-09T11:20:28" {
		t.Fatalf("period end %v", value.PeriodEnd)
	}
	if value.AutoRenews() {
		t.Fatal("cancelAtPeriodEnd must not auto-renew")
	}
}

func TestParsePlanSubscriptionFreeAndMissingDate(t *testing.T) {
	value, err := parsePlanSubscription([]byte(`{"subscriptions":[]}`))
	if err != nil || value.Count != 0 || value.PeriodEnd != nil {
		t.Fatalf("free %+v %v", value, err)
	}
	value, err = parsePlanSubscription([]byte(`{"subscriptions":[{"status":"SUBSCRIPTION_STATUS_ACTIVE"}]}`))
	if err != nil || value.PeriodEnd != nil {
		t.Fatalf("missing date must stay nil %+v", value)
	}
}
