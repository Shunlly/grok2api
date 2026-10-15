package account

import (
	"testing"
	"time"

	quotasyncapp "github.com/chenyme/grok2api/backend/internal/application/quotasync"
)

func TestComputeQuotaSyncHint(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	hourly := now.Add(time.Hour)
	synced := now.Add(-10 * time.Minute)
	base := quotaSyncInputs{supported: true, hasStatus: true, now: now, resetDelay: 90 * time.Second,
		status: quotasyncapp.Status{Enabled: true, NextRunAt: &hourly}}

	t.Run("hourly", func(t *testing.T) {
		item := accountResponse{ID: 1, Enabled: true, AuthStatus: "active", Billing: &billingResponse{SyncedAt: synced}}
		computeQuotaSyncHint(&item, base)
		if item.NextQuotaSyncReason != "hourly" || !item.NextQuotaSyncAt.Equal(hourly) || !item.LastQuotaSyncAt.Equal(synced) {
			t.Fatalf("got %+v", item)
		}
	})
	t.Run("reset before hourly from nextResetAt", func(t *testing.T) {
		reset := now.Add(20 * time.Minute)
		item := accountResponse{ID: 1, Enabled: true, AuthStatus: "active", NextResetAt: &reset}
		computeQuotaSyncHint(&item, base)
		if item.NextQuotaSyncReason != "reset" || !item.NextQuotaSyncAt.Equal(reset.Add(90*time.Second)) {
			t.Fatalf("got %+v", item)
		}
	})
	t.Run("reset beyond hourly still exposed", func(t *testing.T) {
		reset := now.Add(5 * time.Hour)
		item := accountResponse{ID: 1, Enabled: true, AuthStatus: "active", NextResetAt: &reset}
		computeQuotaSyncHint(&item, base)
		if item.NextQuotaSyncReason != "hourly" || item.ResetQuotaSyncAt == nil || !item.ResetQuotaSyncAt.Equal(reset.Add(90*time.Second)) {
			t.Fatalf("got %+v", item)
		}
	})
	t.Run("scheduler pending wins", func(t *testing.T) {
		pending := now.Add(3 * time.Minute)
		in := base
		in.pending = map[uint64]time.Time{1: pending}
		item := accountResponse{ID: 1, Enabled: true, AuthStatus: "active"}
		computeQuotaSyncHint(&item, in)
		if item.NextQuotaSyncReason != "reset" || !item.NextQuotaSyncAt.Equal(pending) {
			t.Fatalf("got %+v", item)
		}
	})
	for name, tc := range map[string]struct {
		item accountResponse
		in   quotaSyncInputs
		want string
	}{
		"auto off":    {accountResponse{Enabled: true}, quotaSyncInputs{hasStatus: true, supported: true}, "autoSyncDisabled"},
		"account off": {accountResponse{Enabled: false}, base, "accountDisabled"},
		"reauth":      {accountResponse{Enabled: true, AuthStatus: "reauthRequired"}, base, "reauthRequired"},
	} {
		t.Run(name, func(t *testing.T) {
			computeQuotaSyncHint(&tc.item, tc.in)
			if tc.item.NextQuotaSyncReason != tc.want || tc.item.NextQuotaSyncAt != nil {
				t.Fatalf("got %+v", tc.item)
			}
		})
	}
}
