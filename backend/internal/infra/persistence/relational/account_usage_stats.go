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
	LastAuditID            uint64    `gorm:"not null;default:0"`
	TodayDate              string    `gorm:"size:10;not null;default:''"`
	TodayRequests          int64     `gorm:"not null;default:0"`
	TodayFailedRequests    int64     `gorm:"not null;default:0"`
	TodayInputTokens       int64     `gorm:"not null;default:0"`
	TodayCachedInputTokens int64     `gorm:"not null;default:0"`
	TodayOutputTokens      int64     `gorm:"not null;default:0"`
	TodayTokens            int64     `gorm:"not null;default:0"`
	TodayCostUSDTicks      int64     `gorm:"column:today_cost_usd_ticks;not null;default:0"`
	ComputedAt             time.Time `gorm:"not null"`
	// PeriodStart/PeriodEnd/PeriodSource：Total* 覆盖的额度周期（v3.4.4 起 Total* 只统计当前周期）。
	PeriodStart  *time.Time
	PeriodEnd    *time.Time
	PeriodSource string `gorm:"size:64;not null;default:''"`
	// TodayStart：Today* 的起算时间 = max(北京时间当日零点, PeriodStart)，保证今日不超过本周期（v3.4.7）。
	TodayStart *time.Time
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

// RefreshAccountUsageStats 按账号当前额度周期 [period.Start, now] 从审计表重算周期用量，并重算今日用量（起点 = max(北京时间零点, period.Start)）。
// 周期滚动后数值自然从 0 开始；旧版本的累计水位字段保留但不再参与计算。
func (r *AuditRepository) RefreshAccountUsageStats(ctx context.Context, accountID uint64, period repository.UsagePeriod, now time.Time) (repository.AccountUsageStats, error) {
	if accountID == 0 {
		return repository.AccountUsageStats{}, errors.New("账号 ID 无效")
	}
	if period.Start.IsZero() || period.Start.After(now) {
		return repository.AccountUsageStats{}, errors.New("用量周期无效")
	}
	tx := r.db.db.WithContext(ctx)
	periodStart := period.Start.UTC()
	nowUTC := now.UTC()
	total, err := r.aggregateUsage(tx, accountID, func(q *gorm.DB) *gorm.DB {
		return q.Where("created_at >= ? AND created_at <= ?", periodStart, nowUTC)
	})
	if err != nil {
		return repository.AccountUsageStats{}, err
	}
	local := now.In(usageStatsZone)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, usageStatsZone).UTC()
	// 今日只统计本周期内的部分：周期在今天重置时，从重置时间起算。
	if periodStart.After(dayStart) {
		dayStart = periodStart
	}
	today, err := r.aggregateUsage(tx, accountID, func(q *gorm.DB) *gorm.DB {
		return q.Where("created_at >= ? AND created_at <= ?", dayStart, nowUTC)
	})
	if err != nil {
		return repository.AccountUsageStats{}, err
	}
	next := accountUsageStatsModel{AccountID: accountID,
		TotalRequests: total.Requests, TotalFailedRequests: total.Failed, TotalInputTokens: total.InputTokens,
		TotalCachedInputTokens: total.CachedInputTokens, TotalOutputTokens: total.OutputTokens, TotalReasoningTokens: total.ReasoningTokens,
		TotalTokens: total.Tokens, TotalCostUSDTicks: total.Cost, TotalUpstreamCostTicks: total.UpstreamCost, TotalEstimatedTicks: total.EstimatedCost,
		LastAuditID: total.MaxID, TodayDate: local.Format("2006-01-02"), TodayRequests: today.Requests, TodayFailedRequests: today.Failed,
		TodayInputTokens: today.InputTokens, TodayCachedInputTokens: today.CachedInputTokens, TodayOutputTokens: today.OutputTokens,
		TodayTokens: today.Tokens, TodayCostUSDTicks: today.Cost, ComputedAt: nowUTC,
		PeriodStart: &periodStart, PeriodSource: period.Source, TodayStart: &dayStart,
	}
	if !period.End.IsZero() {
		end := period.End.UTC()
		next.PeriodEnd = &end
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}}, UpdateAll: true}).Create(&next).Error; err != nil {
		return repository.AccountUsageStats{}, err
	}
	return usageStatsFromModel(next), nil
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
		PeriodStart: m.PeriodStart, PeriodEnd: m.PeriodEnd, PeriodSource: m.PeriodSource, TodayStart: m.TodayStart,
	}
}
