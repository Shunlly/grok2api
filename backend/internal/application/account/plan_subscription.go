package account

import (
	"context"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
)

// PlanSource tells the HTTP layer where View.Plan came from.
const (
	PlanSourceWebSubscriptions = "webSubscriptions"
	PlanSourceLinkedWeb        = "linkedWebSubscriptions"
)

type planSubscriptionStore interface {
	SavePlanSubscription(ctx context.Context, value accountdomain.PlanSubscription) error
	GetPlanSubscriptions(ctx context.Context, accountIDs []uint64) (map[uint64]accountdomain.PlanSubscription, error)
}

type planSubscriptionAdapter interface {
	SyncPlanSubscription(ctx context.Context, credential accountdomain.Credential) (accountdomain.PlanSubscription, error)
}

// syncPlanSubscription refreshes the Web subscription snapshot after a full
// quota sync. It is best effort: a failure keeps the last snapshot and never
// fails the quota sync.
func (s *Service) syncPlanSubscription(ctx context.Context, value accountdomain.Credential, adapter any) {
	if value.Provider != accountdomain.ProviderWeb {
		return
	}
	store, ok := s.accounts.(planSubscriptionStore)
	if !ok {
		return
	}
	syncer, ok := adapter.(planSubscriptionAdapter)
	if !ok {
		return
	}
	plan, err := syncer.SyncPlanSubscription(ctx, value)
	if err != nil {
		s.logger.Warn("web_plan_subscription_sync_failed", "account_id", value.ID, "error", err)
		return
	}
	plan.AccountID = value.ID
	if err := store.SavePlanSubscription(ctx, plan); err != nil {
		s.logger.Warn("web_plan_subscription_save_failed", "account_id", value.ID, "error", err)
	}
}

// attachPlanSubscriptions fills View.Plan: Web accounts use their own
// snapshot; Build accounts use the snapshot of a linked Web account (same
// xAI user), because the Build API exposes no subscription expiry.
func (s *Service) attachPlanSubscriptions(ctx context.Context, views []View) {
	store, ok := s.accounts.(planSubscriptionStore)
	if !ok || len(views) == 0 {
		return
	}
	ids := make([]uint64, 0, len(views)*2)
	for _, view := range views {
		c := view.Credential
		switch c.Provider {
		case accountdomain.ProviderWeb:
			ids = append(ids, c.ID)
		case accountdomain.ProviderBuild:
			for _, linked := range c.LinkedAccounts {
				if linked.Provider == accountdomain.ProviderWeb {
					ids = append(ids, linked.ID)
				}
			}
			if c.LinkedProvider == accountdomain.ProviderWeb && c.LinkedAccountID != 0 {
				ids = append(ids, c.LinkedAccountID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	plans, err := store.GetPlanSubscriptions(ctx, ids)
	if err != nil {
		s.logger.Warn("plan_subscription_load_failed", "error", err)
		return
	}
	for index := range views {
		c := views[index].Credential
		switch c.Provider {
		case accountdomain.ProviderWeb:
			if plan, ok := plans[c.ID]; ok {
				views[index].Plan, views[index].PlanSource = &plan, PlanSourceWebSubscriptions
			}
		case accountdomain.ProviderBuild:
			candidates := []uint64{}
			if c.LinkedProvider == accountdomain.ProviderWeb && c.LinkedAccountID != 0 {
				candidates = append(candidates, c.LinkedAccountID)
			}
			for _, linked := range c.LinkedAccounts {
				if linked.Provider == accountdomain.ProviderWeb {
					candidates = append(candidates, linked.ID)
				}
			}
			for _, id := range candidates {
				if plan, ok := plans[id]; ok {
					views[index].Plan, views[index].PlanSource = &plan, PlanSourceLinkedWeb
					break
				}
			}
		}
	}
}
