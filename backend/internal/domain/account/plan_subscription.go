package account

import "time"

// PlanSubscription is the upstream consumer subscription (SuperGrok / Heavy)
// snapshot. Only Grok Web exposes it (GET /rest/subscriptions); Build accounts
// borrow it from their linked Web account because the Build API (/user,
// /billing) only returns the tier and the weekly usage period.
//
// PeriodEnd is the subscription's current billing period end, i.e. the date the
// plan expires unless it renews. It is NOT the weekly usage window.
type PlanSubscription struct {
	AccountID         uint64
	Plan              string // dominantPlan.plan, e.g. supergrok.heavy
	Tier              string // SUBSCRIPTION_TIER_*
	Status            string // SUBSCRIPTION_STATUS_*
	BillingInterval   string // BILLING_INTERVAL_*
	BillingSystem     string
	PeriodEnd         *time.Time
	CancelAtPeriodEnd bool
	// Count is the number of subscriptions returned upstream; 0 means the
	// account has no subscription (free).
	Count    int
	SyncedAt time.Time
}

// AutoRenews reports whether the plan renews at PeriodEnd.
func (p PlanSubscription) AutoRenews() bool {
	return p.Count > 0 && !p.CancelAtPeriodEnd && p.Status == "SUBSCRIPTION_STATUS_ACTIVE"
}
