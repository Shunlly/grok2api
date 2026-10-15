package relational

import (
	"context"
	"sort"

	"github.com/chenyme/grok2api/backend/internal/repository"
)

// AccountModelSnapshot 读取账号已同步的上游模型能力、同步状态以及同 Provider 的公开模型路由。
func (r *ModelRepository) AccountModelSnapshot(ctx context.Context, accountID uint64, provider string) (repository.AccountModelSnapshot, error) {
	result := repository.AccountModelSnapshot{Capabilities: []string{}, Routes: []repository.AccountModelRoute{}}
	db := r.db.db.WithContext(ctx)
	var capabilities []accountModelCapabilityModel
	if err := db.Where("account_id = ?", accountID).Order("upstream_model ASC").Find(&capabilities).Error; err != nil {
		return result, err
	}
	for _, row := range capabilities {
		result.Capabilities = append(result.Capabilities, row.UpstreamModel)
	}
	var states []accountModelSyncStateModel
	if err := db.Where("account_id = ?", accountID).Limit(1).Find(&states).Error; err != nil {
		return result, err
	}
	if len(states) > 0 {
		attempt := states[0].LastAttemptAt.UTC()
		result.LastAttemptAt = &attempt
		if states[0].LastSuccessAt != nil {
			success := states[0].LastSuccessAt.UTC()
			result.LastSuccessAt = &success
		}
		result.LastError = states[0].LastError
	}
	var routes []modelRouteModel
	if err := db.Where("provider = ?", provider).Order("public_id ASC").Find(&routes).Error; err != nil {
		return result, err
	}
	ids := make([]uint64, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, route.ID)
	}
	hasBindings := map[uint64]bool{}
	bound := map[uint64]bool{}
	if len(ids) > 0 {
		var bindings []modelRouteAccountModel
		if err := db.Where("model_route_id IN ?", ids).Find(&bindings).Error; err != nil {
			return result, err
		}
		for _, binding := range bindings {
			hasBindings[binding.ModelRouteID] = true
			if binding.AccountID == accountID {
				bound[binding.ModelRouteID] = true
			}
		}
	}
	for _, route := range routes {
		result.Routes = append(result.Routes, repository.AccountModelRoute{
			ID: route.ID, PublicID: route.PublicID, UpstreamModel: route.UpstreamModel, Capability: route.Capability,
			Enabled: route.Enabled, HasBindings: hasBindings[route.ID], Bound: bound[route.ID],
		})
	}
	sort.SliceStable(result.Routes, func(i, j int) bool { return result.Routes[i].PublicID < result.Routes[j].PublicID })
	return result, nil
}
