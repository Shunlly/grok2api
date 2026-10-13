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
	return r.db.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&canaryProbeModel{}).Error
}
