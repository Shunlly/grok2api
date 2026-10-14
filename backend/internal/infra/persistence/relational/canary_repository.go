package relational

import (
	"context"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

type canaryProbeModel struct {
	ID               uint64    `gorm:"primaryKey;autoIncrement"`
	RoundID          string    `gorm:"size:64;not null"`
	Trigger          string    `gorm:"size:16;not null"`
	AccountID        uint64    `gorm:"not null"`
	AccountName      string    `gorm:"size:255;not null;default:''"`
	Model            string    `gorm:"size:128;not null"`
	PromptID         string    `gorm:"size:64;not null"`
	PromptKind       string    `gorm:"size:16;not null"`
	Status           string    `gorm:"size:16;not null"`
	Reasons          string    `gorm:"size:512;not null;default:''"`
	Score            float64   `gorm:"not null;default:0"`
	Correct          bool      `gorm:"not null;default:false"`
	ReasoningTokens  int64     `gorm:"not null;default:0"`
	OutputTokens     int64     `gorm:"not null;default:0"`
	VisibleChars     int64     `gorm:"not null;default:0"`
	PoolMedianReason float64   `gorm:"not null;default:0"`
	FirstTokenMS     int64     `gorm:"not null;default:0"`
	DurationMS       int64     `gorm:"not null;default:0"`
	ConsecutiveFails int       `gorm:"not null;default:0"`
	Suspect          bool      `gorm:"not null;default:false"`
	Error            string    `gorm:"size:512;not null;default:''"`
	RequestID        string    `gorm:"size:64;not null;default:''"`
	CreatedAt        time.Time `gorm:"not null"`
}

func (canaryProbeModel) TableName() string { return "canary_probe_results" }

// CanaryRepository stores observe-only account canary probe results.
type CanaryRepository struct{ db *Database }

func NewCanaryRepository(db *Database) *CanaryRepository { return &CanaryRepository{db: db} }

func (r *CanaryRepository) Create(ctx context.Context, value canary.Result) (canary.Result, error) {
	row := canaryProbeModel(value)
	row.ID = 0
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if err := r.db.db.WithContext(ctx).Create(&row).Error; err != nil {
		return canary.Result{}, err
	}
	return canary.Result(row), nil
}

// ListRecent returns newest-first results, optionally for one account.
func (r *CanaryRepository) ListRecent(ctx context.Context, accountID uint64, limit int) ([]canary.Result, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.db.db.WithContext(ctx).Model(&canaryProbeModel{}).Order("created_at DESC, id DESC").Limit(limit)
	if accountID != 0 {
		query = query.Where("account_id = ?", accountID)
	}
	var rows []canaryProbeModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]canary.Result, 0, len(rows))
	for _, row := range rows {
		out = append(out, canary.Result(row))
	}
	return out, nil
}

// CountRounds returns how many distinct rounds were stored (rotation seed).
func (r *CanaryRepository) CountRounds(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.db.WithContext(ctx).Model(&canaryProbeModel{}).Distinct("round_id").Count(&count).Error
	return count, err
}

// DeleteBefore prunes old history.
func (r *CanaryRepository) DeleteBefore(ctx context.Context, cutoff time.Time) error {
	if err := r.db.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&canaryProbeArtifactModel{}).Error; err != nil {
		return err
	}
	return r.db.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&canaryProbeModel{}).Error
}

// AccountQualityFlags aggregates real quality hits per account since the
// cutoff: quality_degraded (missing thinking) audits and non-passing canary
// probes. Speed classes are intentionally not included.
func (r *CanaryRepository) AccountQualityFlags(ctx context.Context, since time.Time) ([]canary.AccountFlag, error) {
	type degradedRow struct {
		AccountID uint64
		Hits      int64
		Last      string
	}
	var degraded []degradedRow
	if err := r.db.db.WithContext(ctx).Table("request_audits").
		Select("account_id, COUNT(*) AS hits, MAX(created_at) AS last").
		Where("account_id IS NOT NULL AND error_code = ? AND created_at >= ?", "quality_degraded", since).
		Group("account_id").Scan(&degraded).Error; err != nil {
		return nil, err
	}
	var failed []canaryProbeModel
	if err := r.db.db.WithContext(ctx).Model(&canaryProbeModel{}).
		Where("status IN ? AND created_at >= ?", []string{canary.StatusFail, canary.StatusError}, since).
		Order("created_at DESC, id DESC").Find(&failed).Error; err != nil {
		return nil, err
	}
	byID := map[uint64]*canary.AccountFlag{}
	order := []uint64{}
	get := func(id uint64) *canary.AccountFlag {
		if flag, ok := byID[id]; ok {
			return flag
		}
		byID[id] = &canary.AccountFlag{AccountID: id}
		order = append(order, id)
		return byID[id]
	}
	for _, row := range degraded {
		flag := get(row.AccountID)
		flag.DegradedHits = row.Hits
		if parsed, ok := parseDBTime(row.Last); ok {
			flag.LastDegradedAt = &parsed
		}
	}
	for _, row := range failed {
		flag := get(row.AccountID)
		flag.CanaryFailures++
		if flag.LastCanaryFailAt == nil {
			at := row.CreatedAt.UTC()
			flag.LastCanaryFailAt = &at
			flag.LastCanaryStatus = row.Status
			flag.LastCanaryReason = row.Reasons
			if flag.LastCanaryReason == "" {
				flag.LastCanaryReason = row.Error
			}
		}
	}
	out := make([]canary.AccountFlag, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func parseDBTime(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999-07:00", time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

type canaryProbeArtifactModel struct {
	ResultID           uint64    `gorm:"primaryKey;autoIncrement:false"`
	Prompt             string    `gorm:"type:text;not null"`
	Expected           string    `gorm:"size:64;not null;default:''"`
	Response           string    `gorm:"type:text;not null"`
	ResponseTruncated  bool      `gorm:"not null;default:false"`
	Reasoning          string    `gorm:"type:text;not null"`
	ReasoningTruncated bool      `gorm:"not null;default:false"`
	ReasoningChars     int64     `gorm:"not null;default:0"`
	InputTokens        int64     `gorm:"not null;default:0"`
	OutputTokens       int64     `gorm:"not null;default:0"`
	ReasoningTokens    int64     `gorm:"not null;default:0"`
	TotalTokens        int64     `gorm:"not null;default:0"`
	ResponseModel      string    `gorm:"size:128;not null;default:''"`
	ReasoningEffort    string    `gorm:"size:16;not null;default:''"`
	MaxOutputTokens    int       `gorm:"not null;default:0"`
	FirstTokenMS       int64     `gorm:"not null;default:0"`
	DurationMS         int64     `gorm:"not null;default:0"`
	CheckDetail        string    `gorm:"size:1024;not null;default:''"`
	CreatedAt          time.Time `gorm:"not null;index:idx_canary_artifact_created"`
}

func (canaryProbeArtifactModel) TableName() string { return "canary_probe_artifacts" }

// CreateArtifact stores the probe output (prompt, answer, reasoning, usage).
func (r *CanaryRepository) CreateArtifact(ctx context.Context, value canary.Artifact) error {
	row := canaryProbeArtifactModel(value)
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	return r.db.db.WithContext(ctx).Create(&row).Error
}

// GetResult returns one probe result and its artifact (nil when not recorded).
func (r *CanaryRepository) GetResult(ctx context.Context, id uint64) (canary.Result, *canary.Artifact, error) {
	var row canaryProbeModel
	if err := r.db.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return canary.Result{}, nil, err
	}
	var artifact canaryProbeArtifactModel
	err := r.db.db.WithContext(ctx).Where("result_id = ?", id).Limit(1).Find(&artifact).Error
	if err != nil {
		return canary.Result(row), nil, err
	}
	if artifact.ResultID == 0 {
		return canary.Result(row), nil, nil
	}
	value := canary.Artifact(artifact)
	return canary.Result(row), &value, nil
}

// DeleteArtifactsBefore prunes stored artifacts (audit retention).
func (r *CanaryRepository) DeleteArtifactsBefore(ctx context.Context, cutoff time.Time) error {
	return r.db.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&canaryProbeArtifactModel{}).Error
}
