package account

import (
	"net/http"
	"sort"

	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

type contextWindowModel struct {
	Model                       string `json:"model"`
	ContextWindow               int    `json:"contextWindow"`
	ContextWindows              []int  `json:"contextWindows"`
	SelectableContextWindows    []int  `json:"selectableContextWindows"`
	MaxContextWindow            int    `json:"maxContextWindow"`
	EffectiveContextWindow      int    `json:"effectiveContextWindow"`
	AutoCompactThresholdPercent int    `json:"autoCompactThresholdPercent"`
	MaxCompletionTokens         int    `json:"maxCompletionTokens"`
}

// contextWindows exposes the cached upstream Build catalog context fields
// (read-only) so the account dialog can offer the selectable sizes.
func (h *Handler) contextWindows(c *gin.Context) {
	slugs := modeldomain.UpstreamProfileSlugs()
	models := make([]contextWindowModel, 0, len(slugs))
	options := map[int]struct{}{}
	for _, slug := range slugs {
		profile, ok := modeldomain.UpstreamProfile(slug)
		if !ok {
			continue
		}
		selectable := profile.SelectableContextWindows()
		for _, window := range selectable {
			options[window] = struct{}{}
		}
		windows := profile.ContextWindows
		if windows == nil {
			windows = []int{}
		}
		models = append(models, contextWindowModel{
			Model: slug, ContextWindow: profile.ContextWindow, ContextWindows: windows, SelectableContextWindows: selectable,
			MaxContextWindow: profile.MaxContextWindow(), EffectiveContextWindow: profile.EffectiveContextWindow(),
			AutoCompactThresholdPercent: profile.AutoCompactThresholdPercent, MaxCompletionTokens: profile.MaxCompletionTokens,
		})
	}
	sizes := make([]int, 0, len(options))
	for window := range options {
		sizes = append(sizes, window)
	}
	sort.Ints(sizes)
	response.Success(c, http.StatusOK, gin.H{"models": models, "options": sizes, "cap": modeldomain.BuildContextWindowCap()})
}
