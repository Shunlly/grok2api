package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
)

func newAffinityFixture(t *testing.T, count int) (*Selector, *memory.StickyStore, []account.Credential) {
	t.Helper()
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "affinity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := relational.NewAccountRepository(database)
	creds := make([]account.Credential, 0, count)
	for i := 0; i < count; i++ {
		name := string(rune('a' + i))
		cred, _, err := accounts.UpsertByIdentity(ctx, account.Credential{
			Provider: account.ProviderBuild, Name: name, SourceKey: name, EncryptedAccessToken: "encrypted",
			Enabled: true, AuthStatus: account.AuthStatusActive, Priority: 100 - i, MaxConcurrent: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		creds = append(creds, cred)
	}
	sticky := memory.NewStickyStore()
	selector := NewSelector(accounts, memory.NewConcurrencyLimiter(), sticky, nil, time.Hour, time.Second, time.Minute, 0)
	return selector, sticky, creds
}

func acquireSticky(t *testing.T, selector *Selector) *accountLease {
	t.Helper()
	lease, err := selector.Acquire(context.Background(), account.ProviderBuild, 0, "model", "", "codex-session", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func acquireStickySession(t *testing.T, selector *Selector) *accountLease {
	t.Helper()
	session, err := selector.beginSelectionSession(context.Background(), account.ProviderBuild, 0, "model", "", "codex-session", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := session.Acquire(context.Background(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func assertBinding(t *testing.T, sticky *memory.StickyStore, want uint64) {
	t.Helper()
	got, ok, err := sticky.Get(context.Background(), stickySessionKey("codex-session"), time.Now().UTC())
	if err != nil || !ok || got != want {
		t.Fatalf("sticky binding = %d ok=%v err=%v, want %d", got, ok, err, want)
	}
}

func TestAffinityStaysStickyUnderCap(t *testing.T) {
	selector, sticky, creds := newAffinityFixture(t, 2)
	selector.UpdateAffinity(AffinityConfig{BusyCap: 3, BusyGap: 0})
	var leases []*accountLease
	for i := 0; i < 3; i++ {
		lease := acquireSticky(t, selector)
		if lease.Credential.ID != creds[0].ID {
			t.Fatalf("request %d went to %d, want sticky %d", i, lease.Credential.ID, creds[0].ID)
		}
		leases = append(leases, lease)
	}
	assertBinding(t, sticky, creds[0].ID)
	for _, lease := range leases {
		lease.Release()
	}
}

func TestAffinitySpillsToLeastLoadedAtCapAndKeepsBinding(t *testing.T) {
	for _, viaSession := range []bool{false, true} {
		selector, sticky, creds := newAffinityFixture(t, 3)
		selector.UpdateAffinity(AffinityConfig{BusyCap: 3, BusyGap: 0})
		acquire := acquireSticky
		if viaSession {
			acquire = acquireStickySession
		}
		var held []*accountLease
		for i := 0; i < 3; i++ {
			held = append(held, acquire(t, selector))
		}
		// Load account b with one request so c is the least loaded alternative.
		busy, err := selector.claimAccountSlot(context.Background(), creds[1])
		if err != nil || busy == nil {
			t.Fatalf("claim b: %v", err)
		}
		spill := acquire(t, selector)
		if spill.Credential.ID != creds[2].ID {
			t.Fatalf("session=%v spill went to %d, want least loaded %d", viaSession, spill.Credential.ID, creds[2].ID)
		}
		assertBinding(t, sticky, creds[0].ID)
		spill.Release()
		busy.Release()
		for _, lease := range held {
			lease.Release()
		}
		back := acquire(t, selector)
		if back.Credential.ID != creds[0].ID {
			t.Fatalf("session=%v after load drop went to %d, want sticky %d", viaSession, back.Credential.ID, creds[0].ID)
		}
		back.Release()
	}
}

func TestAffinityGapSpills(t *testing.T) {
	selector, sticky, creds := newAffinityFixture(t, 2)
	selector.UpdateAffinity(AffinityConfig{BusyCap: 0, BusyGap: 2})
	first, second := acquireSticky(t, selector), acquireSticky(t, selector)
	if second.Credential.ID != creds[0].ID {
		t.Fatalf("gap 1 should stay sticky, got %d", second.Credential.ID)
	}
	third := acquireSticky(t, selector)
	if third.Credential.ID != creds[1].ID {
		t.Fatalf("gap 2 should spill, got %d", third.Credential.ID)
	}
	assertBinding(t, sticky, creds[0].ID)
	first.Release()
	second.Release()
	third.Release()
}

func TestAffinityAllBusyStaysSticky(t *testing.T) {
	selector, sticky, creds := newAffinityFixture(t, 2)
	selector.UpdateAffinity(AffinityConfig{BusyCap: 2, BusyGap: 1})
	var held []*accountLease
	for i := 0; i < 2; i++ {
		held = append(held, acquireSticky(t, selector))
		other, err := selector.claimAccountSlot(context.Background(), creds[1])
		if err != nil || other == nil {
			t.Fatalf("claim other: %v", err)
		}
		held = append(held, other)
	}
	lease := acquireSticky(t, selector)
	if lease.Credential.ID != creds[0].ID {
		t.Fatalf("all busy should stay sticky, got %d", lease.Credential.ID)
	}
	assertBinding(t, sticky, creds[0].ID)
	lease.Release()
	for _, value := range held {
		value.Release()
	}
}

func TestAffinityDisabled(t *testing.T) {
	selector, _, creds := newAffinityFixture(t, 2)
	selector.UpdateAffinity(AffinityConfig{})
	var held []*accountLease
	for i := 0; i < 5; i++ {
		lease := acquireSticky(t, selector)
		if lease.Credential.ID != creds[0].ID {
			t.Fatalf("disabled affinity spilled to %d", lease.Credential.ID)
		}
		held = append(held, lease)
	}
	for _, lease := range held {
		lease.Release()
	}
}
