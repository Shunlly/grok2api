package account

import (
	"context"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

// RefreshContextWindowCap folds the enabled Build accounts' context window
// settings into the process-wide cap used by /v1/models and the precheck.
// /v1/models is not per-account, so the cap is the largest window any enabled
// Build account allows; any account on auto leaves it uncapped.
func (s *Service) RefreshContextWindowCap(ctx context.Context) {
	if s == nil || s.accounts == nil {
		return
	}
	accounts, err := s.accounts.ListEnabled(ctx, accountdomain.ProviderBuild)
	if err != nil {
		return
	}
	settings := make([]int, 0, len(accounts))
	for _, value := range accounts {
		settings = append(settings, value.ContextWindow)
	}
	modeldomain.SetBuildContextWindowCap(modeldomain.ResolveContextWindowCap(settings))
}

// RunContextWindowCapRefresh keeps the cap current as accounts are enabled,
// disabled or recovered by background paths.
func (s *Service) RunContextWindowCapRefresh(ctx context.Context) {
	s.RefreshContextWindowCap(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RefreshContextWindowCap(ctx)
		}
	}
}
