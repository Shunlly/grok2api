package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

func TestSelectorRespectsAccountModelAllowlist(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "selector-allowlist.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := relational.NewAccountRepository(database)
	models := relational.NewModelRepository(database)
	restricted, _, err := accounts.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderBuild, Name: "restricted", SourceKey: "restricted", EncryptedAccessToken: "encrypted", AuthStatus: account.AuthStatusActive,
		Priority: 900, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	open, _, err := accounts.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderBuild, Name: "open", SourceKey: "open", EncryptedAccessToken: "encrypted", AuthStatus: account.AuthStatusActive,
		Priority: 100, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []uint64{restricted.ID, open.ID} {
		if err := accounts.SaveBilling(ctx, account.Billing{AccountID: id, MonthlyLimit: 100, SyncedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := models.ReplaceAccountCapabilities(ctx, id, []string{"grok-a", "grok-b"}, now); err != nil {
			t.Fatal(err)
		}
	}
	restricted.AllowedModels = []string{"grok-a"}
	if restricted, err = accounts.Update(ctx, restricted); err != nil {
		t.Fatal(err)
	}
	if len(restricted.AllowedModels) != 1 || restricted.AllowedModels[0] != "grok-a" {
		t.Fatalf("allowlist not persisted: %#v", restricted.AllowedModels)
	}
	selector := NewSelector(accounts, memory.NewConcurrencyLimiter(), memory.NewStickyStore(), nil, time.Hour, time.Second, time.Minute)
	accounts.SetInvalidationObserver(func(_ context.Context, event repository.InvalidationEvent) { selector.ApplyInvalidation(event) })

	lease, err := selector.Acquire(ctx, account.ProviderBuild, 0, "grok-b", "", "", map[uint64]bool{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Credential.ID != open.ID {
		t.Fatalf("grok-b selected %d, want open account %d", lease.Credential.ID, open.ID)
	}
	lease.Release()

	lease, err = selector.Acquire(ctx, account.ProviderBuild, 0, "grok-a", "", "", map[uint64]bool{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Credential.ID != restricted.ID {
		t.Fatalf("grok-a selected %d, want higher-priority restricted account %d", lease.Credential.ID, restricted.ID)
	}
	lease.Release()

	// Excluding the only unrestricted account leaves no account for grok-b.
	_, err = selector.Acquire(ctx, account.ProviderBuild, 0, "grok-b", "", "", map[uint64]bool{open.ID: true}, true)
	var unavailable *SelectionUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != SelectionUnsupportedModel {
		t.Fatalf("err = %v, want unsupported model", err)
	}

	// Clearing the allowlist restores allow-all.
	restricted.AllowedModels = nil
	if _, err := accounts.Update(ctx, restricted); err != nil {
		t.Fatal(err)
	}
	lease, err = selector.Acquire(ctx, account.ProviderBuild, 0, "grok-b", "", "", map[uint64]bool{open.ID: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Credential.ID != restricted.ID {
		t.Fatalf("after clearing, grok-b selected %d, want %d", lease.Credential.ID, restricted.ID)
	}
	lease.Release()
}

func TestCredentialAllowsModel(t *testing.T) {
	if !(account.Credential{}).AllowsModel("x") {
		t.Fatal("empty allowlist must allow all")
	}
	value := account.Credential{AllowedModels: []string{"grok-a"}}
	if !value.AllowsModel("grok-a") || value.AllowsModel("grok-b") {
		t.Fatal("allowlist mismatch")
	}
}
