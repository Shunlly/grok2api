package relational

import (
	"context"
	"errors"
	"time"

	"github.com/chenyme/grok2api/backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// accountUsageStatsModel 保存账号真实请求用量快照，只在额度/账单同步时刷新。
// 不设外键：与 request_audits.account_id 一致，账号删除后残留行只是不再被读取。
type accountUsageStatsModel struct {
	AccountID              uint64 `gorm:"primaryKey"`
	TotalRequests          int64  `gorm:"not null;default:0"`
	TotalFailedRequests    int64  `gorm:"not null;default:0"`
	TotalInputTokens       int64  `gorm:"not null;default:0"`
	TotalCachedInputTokens int64  `gorm:"not null;default:0"`
	TotalOutputTokens      int64  `gorm:"not null;default:0"`
	TotalReasoningTokens   int64  `gorm:"not null;default:0"`
	TotalTokens            int64  `gorm:"not null;default:0"`
	TotalCostUSDTicks      int64  `gorm:"column:total_cost_usd_ticks;not null;default:0"`
	TotalUpstreamCostTicks int64  `gorm:"not null;default:0"`
	TotalEstimatedTicks    int64  `gorm:"not null;default:0"`
	FirstAuditAt           *time.Time
	LastAuditID            uint64        `gorm:"not null;default:0"`
	TodayDate              string        `gorm:"size:10;not null;default:''"`
	TodayRequests          int64         `gorm:"not null;default:0"`
	TodayFailedRequests    int64         `gorm:"not null;default:0"`
	TodayInputTokens       int64         `gorm:"not null;default:0"`
	TodayCachedInputTokens int64         `gorm:"not null;default:0"`
	TodayOutputTokens      int64         `gorm:"not null;default:0"`
	TodayTokens            int64         `gorm:"not null;default:0"`
	TodayCostUSDTicks      int64         `gorm:"column:today_cost_usd_ticks;not null;default:0"`
	ComputedAt             time.Time     `gorm:"not null"`
}

func (accountUsageStatsModel) TableName() string { return "account_usage_stats" }

// usageStatsZone 是"今日"的自然日时区，与管理端显示时区（北京时间）一致。
var usageStatsZone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("UTC+8", 8*3600)
}()

const usageStatsAggregate = "COUNT(*) AS requests, " +
	"COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0) AS failed, " +
	"COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(cached_input_tokens), 0) AS cached_input_tokens, " +
	"COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens, " +
	"COALESCE(SUM(total_tokens), 0) AS tokens, " +
	"COALESCE(SUM(CASE WHEN cost_in_usd_ticks > 0 THEN cost_in_usd_ticks ELSE estimated_cost_in_usd_ticks END), 0) AS cost, " +
	"COALESCE(SUM(cost_in_usd_ticks), 0) AS upstream_cost, " +
	"COALESCE(SUM(CASE WHEN cost_in_usd_ticks > 0 THEN 0 ELSE estimated_cost_in_usd_ticks END), 0) AS estimated_cost, " +
	"COALESCE(MAX(id), 0) AS max_id"

type usageAggregateRow struct {
	Requests          int64
	Failed            int64
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
	Tokens            int64
	Cost              int64
	UpstreamCost      int64
	EstimatedCost     int64
	MaxID             uint64
}

func (r *AuditRepository) aggregateUsage(tx *gorm.DB, accountID uint64, scope func(*gorm.DB) *gorm.DB) (usageAggregateRow, error) {
	var row usageAggregateRow
	query := tx.Model(&requestAuditModel{}).Select(usageStatsAggregate).
		Where("account_id = ?", accountID).Where(dashboardExcludeProbePredicate)
	err := scope(query).Scan(&row).Error
	return row, err
}

// RefreshAccountUsageStats 增量累加水位之后的审计记录，并重算今日（北京时间）用量。
func (r *AuditRepository) RefreshAccountUsageStats(ctx context.Context, accountID uint64, now time.Time) (repository.AccountUsageStats, error) {
	if accountID == 0 {
		return repository.AccountUsageStats{}, errors.New("账号 ID 无效")
	}
	var saved accountUsageStatsModel
	err := r.db.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current accountUsageStatsModel
		err := tx.Where("account_id = ?", accountID).Take(&current).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !exists {
			current = accountUsageStatsModel{AccountID: accountID}
		}
		watermark := current.LastAuditID
		delta, err := r.aggregateUsage(tx, accountID, func(q *gorm.DB) *gorm.DB { return q.Where("id > ?", watermark) })
		if err != nil {
			return err
		}
		next := current
		if delta.Requests > 0 {
			next.TotalRequests += delta.Requests
			next.TotalFailedRequests += delta.Failed
			next.TotalInputTokens += delta.InputTokens
			next.TotalCachedInputTokens += delta.CachedInputTokens
			next.TotalOutputTokens += delta.OutputTokens
			next.TotalReasoningTokens += delta.ReasoningTokens
			next.TotalTokens += delta.Tokens
			next.TotalCostUSDTicks += delta.Cost
			next.TotalUpstreamCostTicks += delta.UpstreamCost
			next.TotalEstimatedTicks += delta.EstimatedCost
			if delta.MaxID > next.LastAuditID {
				next.LastAuditID = delta.MaxID
			}
			if next.FirstAuditAt == nil {
				var first []struct{ CreatedAt time.Time }
				if err := tx.Model(&requestAuditModel{}).Select("created_at").
					Where("account_id = ? AND id > ?", accountID, watermark).Where(dashboardExcludeProbePredicate).
					Order("id ASC").Limit(1).Scan(&first).Error; err != nil {
					return err
				}
				if len(first) > 0 && !first[0].CreatedAt.IsZero() {
					at := first[0].CreatedAt.UTC()
					next.FirstAuditAt = &at
				}
			}
		}
		local := now.In(usageStatsZone)
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, usageStatsZone).UTC()
		today, err := r.aggregateUsage(tx, accountID, func(q *gorm.DB) *gorm.DB {
			return q.Where("created_at >= ? AND id <= ?", dayStart, next.LastAuditID)
		})
		if err != nil {
			return err
		}
		next.TodayDate = local.Format("2006-01-02")
		next.TodayRequests, next.TodayFailedRequests = today.Requests, today.Failed
		next.TodayInputTokens, next.TodayCachedInputTokens = today.InputTokens, today.CachedInputTokens
		next.TodayOutputTokens, next.TodayTokens, next.TodayCostUSDTicks = today.OutputTokens, today.Tokens, today.Cost
		next.ComputedAt = now.UTC()
		if !exists {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				// 并发刷新已先写入，保留对方结果。
				return tx.Where("account_id = ?", accountID).Take(&saved).Error
			}
		} else {
			// 以旧水位做乐观锁，防止并发刷新重复累加同一批审计。
			result := tx.Model(&accountUsageStatsModel{}).
				Where("account_id = ? AND last_audit_id = ?", accountID, watermark).
				Select("*").Omit("account_id").Updates(&next)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return tx.Where("account_id = ?", accountID).Take(&saved).Error
			}
		}
		saved = next
		return nil
	})
	if err != nil {
		return repository.AccountUsageStats{}, err
	}
	return usageStatsFromModel(saved), nil
}

// AccountUsageStatsByAccounts 读取已保存的用量快照；未刷新过的账号不出现在结果中。
func (r *AuditRepository) AccountUsageStatsByAccounts(ctx context.Context, accountIDs []uint64) (map[uint64]repository.AccountUsageStats, error) {
	result := make(map[uint64]repository.AccountUsageStats, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	var rows []accountUsageStatsModel
	for start := 0; start < len(accountIDs); start += auditLookupBatchSize {
		end := min(start+auditLookupBatchSize, len(accountIDs))
		var batch []accountUsageStatsModel
		if err := r.db.db.WithContext(ctx).Where("account_id IN ?", accountIDs[start:end]).Find(&batch).Error; err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
	}
	for _, row := range rows {
		result[row.AccountID] = usageStatsFromModel(row)
	}
	return result, nil
}

func usageStatsFromModel(m accountUsageStatsModel) repository.AccountUsageStats {
	return repository.AccountUsageStats{
		AccountID: m.AccountID, TotalRequests: m.TotalRequests, TotalFailedRequests: m.TotalFailedRequests,
		TotalInputTokens: m.TotalInputTokens, TotalCachedInputTokens: m.TotalCachedInputTokens, TotalOutputTokens: m.TotalOutputTokens,
		TotalReasoningTokens: m.TotalReasoningTokens, TotalTokens: m.TotalTokens, TotalCostUSDTicks: m.TotalCostUSDTicks,
		TotalUpstreamCostTicks: m.TotalUpstreamCostTicks, TotalEstimatedTicks: m.TotalEstimatedTicks, FirstAuditAt: m.FirstAuditAt,
		LastAuditID: m.LastAuditID, TodayDate: m.TodayDate, TodayRequests: m.TodayRequests, TodayFailedRequests: m.TodayFailedRequests,
		TodayInputTokens: m.TodayInputTokens, TodayCachedInputTokens: m.TodayCachedInputTokens, TodayOutputTokens: m.TodayOutputTokens,
		TodayTokens: m.TodayTokens, TodayCostUSDTicks: m.TodayCostUSDTicks, ComputedAt: m.ComputedAt.UTC(),
	}
}
