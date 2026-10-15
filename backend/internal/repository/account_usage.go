package repository

import "time"

// AccountUsageStats 是账号真实请求用量快照（排除探针 / 考题 / 内部 Client Key）。
// Total* 是当前额度周期 [PeriodStart, ComputedAt] 内的用量，每次刷新从审计表重算，周期滚动后归零；
// Today* 每次刷新按 Asia/Shanghai 自然日从审计表重算。
type AccountUsageStats struct {
	AccountID uint64

	TotalRequests          int64
	TotalFailedRequests    int64
	TotalInputTokens       int64
	TotalCachedInputTokens int64
	TotalOutputTokens      int64
	TotalReasoningTokens   int64
	TotalTokens            int64
	TotalCostUSDTicks      int64
	TotalUpstreamCostTicks int64
	TotalEstimatedTicks    int64
	FirstAuditAt           *time.Time
	LastAuditID            uint64

	TodayDate              string
	TodayRequests          int64
	TodayFailedRequests    int64
	TodayInputTokens       int64
	TodayCachedInputTokens int64
	TodayOutputTokens      int64
	TodayTokens            int64
	TodayCostUSDTicks      int64

	ComputedAt time.Time

	PeriodStart  *time.Time
	PeriodEnd    *time.Time
	PeriodSource string
}

// UsagePeriod 是账号当前额度周期。Source 取值：billingUsagePeriod / quotaWindow:<mode> / rolling7d。
type UsagePeriod struct {
	Start  time.Time
	End    time.Time
	Source string
}
