package dashboard

import "slices"

// Resources 表示 Dashboard 所需的资源总量与可用量。
type Resources struct {
	ActiveAccounts   int64
	TotalAccounts    int64
	BuildAccounts    int64
	WebAccounts      int64
	ConsoleAccounts  int64
	EnabledModels    int64
	TotalModels      int64
	ActiveClientKeys int64
	TotalClientKeys  int64
}

// Usage 表示指定时间窗口内的请求聚合。
type Usage struct {
	Requests           int64
	SuccessfulRequests int64
	FailedRequests     int64
	InputTokens        int64
	CachedInputTokens  int64
	OutputTokens       int64
	ReasoningTokens    int64
	Tokens             int64
	BilledCostUSDTicks int64
	FirstTokenSamples  int64
	FirstTokenTotalMS  int64
	ThroughputSamples  int64
	ThroughputTokens   int64
	GenerationTotalMS  int64
	// FirstTokenMedianMS / FirstTokenP90MS 只统计真实业务请求（排除质量探针与定时考题）。
	FirstTokenMedianMS int64              `gorm:"-"`
	FirstTokenP90MS    int64              `gorm:"-"`
	FirstTokenBuckets  []FirstTokenBucket `gorm:"-"`
}

// FirstTokenBucket 按输入 token 规模分组的首字耗时统计。
type FirstTokenBucket struct {
	Key      string
	MinInput int64
	MaxInput int64 // 0 表示无上限
	Samples  int64
	MedianMS int64
}

// FirstTokenInputBuckets 定义首字统计的输入规模分组（左闭右开）。
var FirstTokenInputBuckets = []FirstTokenBucket{
	{Key: "lt20k", MinInput: 0, MaxInput: 20_000},
	{Key: "20k-60k", MinInput: 20_000, MaxInput: 60_000},
	{Key: "60k-120k", MinInput: 60_000, MaxInput: 120_000},
	{Key: "120k-200k", MinInput: 120_000, MaxInput: 200_000},
	{Key: "gt200k", MinInput: 200_000},
}

// FirstTokenSample 是单次请求的首字耗时与输入规模。
type FirstTokenSample struct {
	FirstTokenMS int64
	InputTokens  int64
}

// PercentileMS 使用最近秩法计算已排序样本的百分位（p 取 0-100）。
func PercentileMS(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(p/100*float64(len(sorted))+0.999999999) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// ApplyFirstTokenSamples 用真实请求样本填充首字统计（平均、中位数、p90 与分组）。
func (u *Usage) ApplyFirstTokenSamples(samples []FirstTokenSample) {
	all := make([]int64, 0, len(samples))
	grouped := make([][]int64, len(FirstTokenInputBuckets))
	var total int64
	for _, sample := range samples {
		all = append(all, sample.FirstTokenMS)
		total += sample.FirstTokenMS
		for index, bucket := range FirstTokenInputBuckets {
			if sample.InputTokens >= bucket.MinInput && (bucket.MaxInput == 0 || sample.InputTokens < bucket.MaxInput) {
				grouped[index] = append(grouped[index], sample.FirstTokenMS)
				break
			}
		}
	}
	sortInt64(all)
	u.FirstTokenSamples = int64(len(all))
	u.FirstTokenTotalMS = total
	u.FirstTokenMedianMS = PercentileMS(all, 50)
	u.FirstTokenP90MS = PercentileMS(all, 90)
	u.FirstTokenBuckets = make([]FirstTokenBucket, 0, len(FirstTokenInputBuckets))
	for index, bucket := range FirstTokenInputBuckets {
		values := grouped[index]
		sortInt64(values)
		bucket.Samples = int64(len(values))
		bucket.MedianMS = PercentileMS(values, 50)
		u.FirstTokenBuckets = append(u.FirstTokenBuckets, bucket)
	}
}

func sortInt64(values []int64) { slices.Sort(values) }

// Bucket 表示一个固定时间桶内的请求和 token 数量。
type Bucket struct {
	Index              int
	Requests           int64
	InputTokens        int64
	CachedInputTokens  int64
	OutputTokens       int64
	ReasoningTokens    int64
	Tokens             int64
	BilledCostUSDTicks int64
}

// ModelUsage 表示指定时间范围内按公开模型聚合的调用量。
type ModelUsage struct {
	Model              string
	Requests           int64
	InputTokens        int64
	CachedInputTokens  int64
	OutputTokens       int64
	ReasoningTokens    int64
	Tokens             int64
	BilledCostUSDTicks int64
}

// ActivityBucket 表示活动热力图中的单日请求量。
type ActivityBucket struct {
	Index    int
	Requests int64
}

// ProviderUsage 表示指定时间范围内单个上游渠道的调用量。
type ProviderUsage struct {
	Provider           string
	Requests           int64
	SuccessfulRequests int64
	Tokens             int64
}

// Aggregate 表示持久化层返回的 Dashboard 聚合快照。
type Aggregate struct {
	Resources       Resources
	Usage           Usage
	Buckets         []Bucket
	ActivityBuckets []ActivityBucket
	TopModels       []ModelUsage
	Providers       []ProviderUsage
}
