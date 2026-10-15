package account

import (
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/repository"
)

func TestBuildAccountModelsReasons(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	synced := now.Add(-time.Hour)
	account := accountResponse{ID: 7, Provider: "grok_build", Enabled: true, AuthStatus: "active",
		ModelQuotaBlocks: []modelQuotaBlockResponse{{Model: "blocked", Reason: "quota", CooldownUntil: now.Add(time.Hour)}, {Model: "a", Reason: "old", CooldownUntil: now.Add(-time.Hour)}}}
	snapshot := repository.AccountModelSnapshot{
		Capabilities:  []string{"a", "blocked", "bound", "extra"},
		LastSuccessAt: &synced,
		Routes: []repository.AccountModelRoute{
			{PublicID: "A", UpstreamModel: "a", Enabled: true},
			{PublicID: "Off", UpstreamModel: "a", Enabled: false},
			{PublicID: "Missing", UpstreamModel: "missing", Enabled: true},
			{PublicID: "Blocked", UpstreamModel: "blocked", Enabled: true},
			{PublicID: "Bound", UpstreamModel: "bound", Enabled: true, HasBindings: true},
		},
	}
	result := buildAccountModels(account, snapshot, now)
	if len(result.Usable) != 1 || result.Usable[0].ID != "A" {
		t.Fatalf("usable = %+v", result.Usable)
	}
	reasons := map[string]string{}
	for _, item := range result.Unavailable {
		reasons[item.ID] = item.Reason
	}
	want := map[string]string{"Off": "routeDisabled", "Missing": "notSupported", "Blocked": "modelQuotaBlocked", "Bound": "notBound"}
	for id, reason := range want {
		if reasons[id] != reason {
			t.Fatalf("%s reason = %q want %q", id, reasons[id], reason)
		}
	}
	if len(result.All) != 4 || len(result.All[0].PublicIDs) != 1 || len(result.All[3].PublicIDs) != 0 {
		t.Fatalf("all = %+v", result.All)
	}

	account.Enabled = false
	account.NoQuota = true
	result = buildAccountModels(account, repository.AccountModelSnapshot{Routes: snapshot.Routes[:1]}, now)
	if result.CapabilityKnown || result.Source != "unsynced" || len(result.Usable) != 0 || result.Unavailable[0].Reason != "accountDisabled" || len(result.AccountBlockers) != 2 {
		t.Fatalf("blocked account = %+v", result)
	}
}

func TestBuildAccountModelsAllowlistReason(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	synced := now.Add(-time.Hour)
	account := accountResponse{ID: 7, Provider: "grok_build", Enabled: true, AuthStatus: "active", AllowedModels: []string{"a"}}
	snapshot := repository.AccountModelSnapshot{
		Capabilities: []string{"a", "b"}, LastSuccessAt: &synced,
		Routes: []repository.AccountModelRoute{{PublicID: "A", UpstreamModel: "a", Enabled: true}, {PublicID: "B", UpstreamModel: "b", Enabled: true}},
	}
	result := buildAccountModels(account, snapshot, now)
	if len(result.Usable) != 1 || result.Usable[0].ID != "A" || len(result.Unavailable) != 1 || result.Unavailable[0].Reason != "notAllowed" {
		t.Fatalf("result = %+v", result)
	}
}
