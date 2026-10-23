package account

import (
	"context"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

// ConcurrencyOverview 是账号推理租约的实时快照。
type ConcurrencyOverview struct {
	Total          int `json:"total"`
	ActiveAccounts int `json:"activeAccounts"`
	Accounts       map[uint64]int
}

// CurrentConcurrency 批量读取指定账号当前占用的推理租约数，缺失项视为零。
func (s *Service) CurrentConcurrency(ctx context.Context, ids []uint64) (map[uint64]int, error) {
	result := make(map[uint64]int)
	if len(ids) == 0 || s.concurrency == nil {
		return result, nil
	}
	keys := make([]string, len(ids))
	for index, id := range ids {
		keys[index] = repository.AccountConcurrencyKey(id)
	}
	var values map[string]int
	if reader, ok := s.concurrency.(repository.ConcurrencySnapshotReader); ok {
		current, err := reader.CurrentMany(ctx, keys)
		if err != nil {
			return nil, err
		}
		values = current
	} else {
		values = make(map[string]int, len(keys))
		for _, key := range keys {
			current, err := s.concurrency.Current(ctx, key)
			if err != nil {
				return nil, err
			}
			values[key] = current
		}
	}
	for index, id := range ids {
		if value := values[keys[index]]; value > 0 {
			result[id] = value
		}
	}
	return result, nil
}

// ConcurrencyOverview 汇总所有启用账号当前的推理并发。
func (s *Service) ConcurrencyOverview(ctx context.Context) (ConcurrencyOverview, error) {
	overview := ConcurrencyOverview{Accounts: map[uint64]int{}}
	if s.concurrency == nil {
		return overview, nil
	}
	seen := make(map[uint64]struct{})
	ids := make([]uint64, 0)
	for _, providerValue := range []accountdomain.Provider{accountdomain.ProviderBuild, accountdomain.ProviderWeb, accountdomain.ProviderConsole} {
		providerIDs, err := s.accounts.ListEnabledAccountIDs(ctx, providerValue, false)
		if err != nil {
			return overview, err
		}
		for _, id := range providerIDs {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	current, err := s.CurrentConcurrency(ctx, ids)
	if err != nil {
		return overview, err
	}
	overview.Accounts = current
	for _, value := range current {
		overview.Total += value
		overview.ActiveAccounts++
	}
	return overview, nil
}
