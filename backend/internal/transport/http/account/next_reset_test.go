package account

import (
	"testing"
	"time"
)

func TestNextQuotaReset(t *testing.T) {
	now := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	later := now.Add(48 * time.Hour)
	sooner := now.Add(6 * time.Hour)
	past := now.Add(-time.Hour)

	if got, src := nextQuotaReset(accountResponse{}, now); got != nil || src != "" {
		t.Fatalf("unknown = %v %q", got, src)
	}
	v := accountResponse{
		Billing:          &billingResponse{UsagePeriodEnd: later.Format(time.RFC3339Nano)},
		QuotaWindows:     []quotaWindowResponse{{Mode: "weekly", WindowSeconds: 604800, ResetAt: &sooner}, {Mode: "img", WindowSeconds: 0, ResetAt: &now}, {Mode: "stale", WindowSeconds: 60, ResetAt: &past}},
		ModelQuotaBlocks: []modelQuotaBlockResponse{{Model: "m", CooldownUntil: past}},
	}
	if got, src := nextQuotaReset(v, now); got == nil || !got.Equal(sooner) || src != "quotaWindow:weekly" {
		t.Fatalf("earliest = %v %q", got, src)
	}
	v.Quota = quotaResponse{Status: "waitingReset", PeriodEnd: later.Format(time.RFC3339Nano), NextProbeAt: &sooner}
	if got, src := nextQuotaReset(v, now); got == nil || !got.Equal(later) || src != "quotaRecoveryPeriodEnd" {
		t.Fatalf("recovery = %v %q", got, src)
	}
	v.Quota.PeriodEnd = ""
	if got, src := nextQuotaReset(v, now); got == nil || !got.Equal(sooner) || src != "quotaRecoveryProbe" {
		t.Fatalf("probe = %v %q", got, src)
	}
}
