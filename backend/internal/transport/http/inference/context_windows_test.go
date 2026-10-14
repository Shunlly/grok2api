package inference

import (
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

func TestCodexCatalogUsesLargestSelectableUpstreamWindow(t *testing.T) {
	modeldomain.ResetUpstreamProfiles()
	t.Cleanup(modeldomain.ResetUpstreamProfiles)
	for _, slug := range []string{"grok-4.7", "grok-4.7-build-fast"} {
		modeldomain.RegisterUpstreamModelProfile(slug, modeldomain.UpstreamModelProfile{
			ContextWindow: 256000, ContextWindows: []int{256000, 500000}, AutoCompactThresholdPercent: 80,
		})
	}
	modeldomain.RegisterUpstreamModelProfile("grok-composer-2.5-fast", modeldomain.UpstreamModelProfile{ContextWindow: 200000})
	items := []modelListItem{
		{ID: "grok-4.7", Provider: account.ProviderBuild, Capability: modeldomain.CapabilityResponses},
		{ID: "grok-4.7-build-fast", Provider: account.ProviderBuild, Capability: modeldomain.CapabilityResponses},
		{ID: "grok-composer-2.5-fast", Provider: account.ProviderBuild, Capability: modeldomain.CapabilityResponses},
	}
	models := newCodexModelCatalog(items).Models
	for _, m := range models[:2] {
		if m.ContextWindow != 500000 || m.MaxContextWindow != 500000 || m.AutoCompactTokenLimit == nil || *m.AutoCompactTokenLimit != 300000 {
			t.Fatalf("%s window=%d/%d compact=%v", m.Slug, m.ContextWindow, m.MaxContextWindow, m.AutoCompactTokenLimit)
		}
	}
	if models[2].ContextWindow != 200000 {
		t.Fatalf("composer window = %d", models[2].ContextWindow)
	}
}

func TestBuildFastStaticFallbackIs500k(t *testing.T) {
	modeldomain.ResetUpstreamProfiles()
	capability, _ := lookupGrokCapability(account.ProviderBuild, "grok-4.7-build-fast")
	if capability.contextWindow != 500000 {
		t.Fatalf("fallback = %d", capability.contextWindow)
	}
}
