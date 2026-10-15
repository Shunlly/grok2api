package repository

import "time"

// AccountUsageStats 是账号真实请求用量快照（排除探针 / 考题 / 内部 Client Key）。
// Total* 以审计 ID 水位增量累加，审计按保留期清理后累计值不会回落；
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
}
