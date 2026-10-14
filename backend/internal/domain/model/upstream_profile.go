package model

import (
	"sort"
	"strings"
	"sync"
)

// UpstreamModelProfile carries the per-model metadata the Grok Build
// /v1/models catalog advertises. grok-build (xai-grok-shell remote/client.rs)
// drives its reasoning-effort menu, default effort, and context budget from
// exactly these fields instead of a compiled-in table, so the gateway mirrors
// the same source of truth once an account has synchronized its catalog.
type UpstreamModelProfile struct {
	// ReasoningEfforts is the ordered menu from `reasoning_efforts[].value`,
	// filtered to the grok-build wire vocabulary. Empty means the catalog did
	// not publish a menu and static capabilities apply.
	ReasoningEfforts []string
	// DefaultReasoningEffort is the menu entry flagged `default: true`, or the
	// top-level `reasoning_effort` when the menu carries no flag.
	DefaultReasoningEffort string
	// SupportsReasoningEffort mirrors `supports_reasoning_effort`.
	SupportsReasoningEffort bool
	// ContextWindow mirrors `context_window` (tokens); zero when absent.
	ContextWindow int
	// ContextWindows mirrors the selectable sizes grok-build reads from
	// `contextWindows` / `context_windows` / `_meta.contextWindows` (e.g.
	// [256000, 500000]); ContextWindow is only the default selection.
	ContextWindows []int
	// MaxCompletionTokens mirrors `max_completion_tokens`; zero when absent.
	MaxCompletionTokens int
	// SupportsBackendSearch mirrors `supports_backend_search`.
	SupportsBackendSearch bool
	// AutoCompactThresholdPercent mirrors `auto_compact_threshold_percent`
	// (1-100); zero when absent.
	AutoCompactThresholdPercent int
}

var (
	upstreamProfilesMu sync.RWMutex
	upstreamProfiles   = map[string]UpstreamModelProfile{}
)

// NormalizeUpstreamProfile validates and canonicalizes a catalog profile:
// effort values are lower-cased, de-duplicated, restricted to known tiers,
// and the default is dropped when the menu does not contain it.
func NormalizeUpstreamProfile(profile UpstreamModelProfile) UpstreamModelProfile {
	efforts := make([]string, 0, len(profile.ReasoningEfforts))
	seen := make(map[string]struct{}, len(profile.ReasoningEfforts))
	for _, raw := range profile.ReasoningEfforts {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !IsKnownReasoningEffort(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		efforts = append(efforts, value)
	}
	profile.ReasoningEfforts = efforts
	profile.DefaultReasoningEffort = strings.ToLower(strings.TrimSpace(profile.DefaultReasoningEffort))
	if _, exists := seen[profile.DefaultReasoningEffort]; !exists {
		profile.DefaultReasoningEffort = ""
	}
	if len(efforts) > 0 {
		profile.SupportsReasoningEffort = true
	}
	if profile.ContextWindow < 0 {
		profile.ContextWindow = 0
	}
	windows := make([]int, 0, len(profile.ContextWindows))
	for _, window := range profile.ContextWindows {
		if window > 0 {
			windows = append(windows, window)
		}
	}
	profile.ContextWindows = windows
	if profile.MaxCompletionTokens < 0 {
		profile.MaxCompletionTokens = 0
	}
	if profile.AutoCompactThresholdPercent < 0 || profile.AutoCompactThresholdPercent > 100 {
		profile.AutoCompactThresholdPercent = 0
	}
	return profile
}

// RegisterUpstreamModelProfile stores the catalog profile for an upstream model
// slug (without provider prefix). Later registrations replace earlier ones so a
// catalog refresh always wins.
func RegisterUpstreamModelProfile(upstreamModel string, profile UpstreamModelProfile) {
	slug := strings.ToLower(externalModelSlug(upstreamModel))
	if slug == "" {
		return
	}
	normalized := NormalizeUpstreamProfile(profile)
	upstreamProfilesMu.Lock()
	upstreamProfiles[slug] = normalized
	upstreamProfilesMu.Unlock()
}

// UpstreamProfile returns the registered catalog profile for a model slug.
func UpstreamProfile(upstreamModel string) (UpstreamModelProfile, bool) {
	slug := strings.ToLower(externalModelSlug(upstreamModel))
	if slug == "" {
		return UpstreamModelProfile{}, false
	}
	upstreamProfilesMu.RLock()
	profile, ok := upstreamProfiles[slug]
	upstreamProfilesMu.RUnlock()
	if !ok {
		return UpstreamModelProfile{}, false
	}
	profile.ReasoningEfforts = append([]string(nil), profile.ReasoningEfforts...)
	profile.ContextWindows = append([]int(nil), profile.ContextWindows...)
	return profile, true
}

// UpstreamProfileSlugs lists registered slugs in stable order (for admin/debug views).
func UpstreamProfileSlugs() []string {
	upstreamProfilesMu.RLock()
	slugs := make([]string, 0, len(upstreamProfiles))
	for slug := range upstreamProfiles {
		slugs = append(slugs, slug)
	}
	upstreamProfilesMu.RUnlock()
	sort.Strings(slugs)
	return slugs
}

// ResetUpstreamProfiles clears every registered profile. Tests use it to
// isolate catalog state between cases.
func ResetUpstreamProfiles() {
	upstreamProfilesMu.Lock()
	upstreamProfiles = map[string]UpstreamModelProfile{}
	upstreamProfilesMu.Unlock()
}

// MaxContextWindow is the largest window the catalog lets a client select:
// the default `context_window` or any listed `contextWindows` size. The window
// selection is a client-side budget in grok-build (no request field carries
// it), so the gateway advertises the largest selectable size.
func (p UpstreamModelProfile) MaxContextWindow() int {
	window := p.ContextWindow
	for _, candidate := range p.ContextWindows {
		if candidate > window {
			window = candidate
		}
	}
	return window
}

// buildContextWindowCap is the largest context window the enabled Build
// accounts allow (admin per-account setting). 0 means at least one account is
// on auto, so the catalog maximum applies.
var buildContextWindowCap struct {
	sync.RWMutex
	value int
}

// SetBuildContextWindowCap publishes the effective Build window cap (0 = no cap).
func SetBuildContextWindowCap(value int) {
	buildContextWindowCap.Lock()
	buildContextWindowCap.value = max(value, 0)
	buildContextWindowCap.Unlock()
}

// BuildContextWindowCap returns the published cap (0 = auto / uncapped).
func BuildContextWindowCap() int {
	buildContextWindowCap.RLock()
	defer buildContextWindowCap.RUnlock()
	return buildContextWindowCap.value
}

// ResolveContextWindowCap folds per-account settings (0 = auto) into one cap:
// any auto account (or no accounts) means uncapped, otherwise the largest setting.
func ResolveContextWindowCap(settings []int) int {
	if len(settings) == 0 {
		return 0
	}
	result := 0
	for _, setting := range settings {
		if setting <= 0 {
			return 0
		}
		result = max(result, setting)
	}
	return result
}

// SelectableContextWindows lists the distinct windows a client may choose, ascending.
func (p UpstreamModelProfile) SelectableContextWindows() []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(p.ContextWindows)+1)
	for _, window := range append([]int{p.ContextWindow}, p.ContextWindows...) {
		if window <= 0 {
			continue
		}
		if _, ok := seen[window]; ok {
			continue
		}
		seen[window] = struct{}{}
		result = append(result, window)
	}
	sort.Ints(result)
	return result
}

// ApplyContextWindowCap returns window limited by capValue (0 = no cap),
// preferring the largest catalog-selectable size that fits.
func (p UpstreamModelProfile) ApplyContextWindowCap(window, capValue int) int {
	if capValue <= 0 || window <= capValue {
		return window
	}
	best := 0
	for _, candidate := range p.SelectableContextWindows() {
		if candidate <= capValue {
			best = candidate
		}
	}
	if best > 0 {
		return best
	}
	return capValue
}

// EffectiveContextWindow is the catalog maximum limited by the Build account cap.
func (p UpstreamModelProfile) EffectiveContextWindow() int {
	return p.ApplyContextWindowCap(p.MaxContextWindow(), BuildContextWindowCap())
}
