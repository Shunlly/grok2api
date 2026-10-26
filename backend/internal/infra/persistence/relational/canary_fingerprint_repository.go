package relational

import (
	"context"
	"encoding/json"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/canary"
)

type canaryFingerprintModel struct {
	ID                  uint64    `gorm:"primaryKey;autoIncrement"`
	RoundID             string    `gorm:"size:64;not null;index:idx_canary_fp_round"`
	Trigger             string    `gorm:"size:16;not null"`
	AccountID           uint64    `gorm:"not null;index:idx_canary_fp_account"`
	AccountName         string    `gorm:"size:255;not null;default:''"`
	Model               string    `gorm:"size:128;not null"`
	Status              string    `gorm:"size:16;not null"`
	Verdict             string    `gorm:"size:16;not null;default:''"`
	TopModel            string    `gorm:"size:128;not null;default:''"`
	TopFamily           string    `gorm:"size:64;not null;default:''"`
	Confidence          float64   `gorm:"not null;default:0"`
	Candidates          string    `gorm:"type:text;not null"`
	ValidCounts         string    `gorm:"size:64;not null;default:''"`
	ValidOutputs        int       `gorm:"not null;default:0"`
	OutputTokens        int64     `gorm:"not null;default:0"`
	DurationMS          int64     `gorm:"not null;default:0"`
	ConsecutiveMismatch int       `gorm:"not null;default:0"`
	Suspect             bool      `gorm:"not null;default:false"`
	Reasons             string    `gorm:"size:512;not null;default:''"`
	Error               string    `gorm:"size:512;not null;default:''"`
	RequestIDs          string    `gorm:"size:255;not null;default:''"`
	Prompts             string    `gorm:"type:text;not null"`
	RawOutputs          string    `gorm:"type:text;not null"`
	CreatedAt           time.Time `gorm:"not null;index:idx_canary_fp_created"`
}

func (canaryFingerprintModel) TableName() string { return "canary_fingerprint_results" }

func encodeJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

func fingerprintToRow(v canary.FingerprintResult) canaryFingerprintModel {
	return canaryFingerprintModel{
		ID: v.ID, RoundID: v.RoundID, Trigger: v.Trigger, AccountID: v.AccountID, AccountName: v.AccountName,
		Model: v.Model, Status: v.Status, Verdict: v.Verdict, TopModel: v.TopModel, TopFamily: v.TopFamily,
		Confidence: v.Confidence, Candidates: encodeJSON(v.Candidates), ValidCounts: encodeJSON(v.ValidCounts),
		ValidOutputs: v.ValidOutputs, OutputTokens: v.OutputTokens, DurationMS: v.DurationMS,
		ConsecutiveMismatch: v.ConsecutiveMismatch, Suspect: v.Suspect, Reasons: v.Reasons, Error: v.Error,
		RequestIDs: v.RequestIDs, Prompts: encodeJSON(v.Prompts), RawOutputs: encodeJSON(v.RawOutputs), CreatedAt: v.CreatedAt,
	}
}

func fingerprintFromRow(row canaryFingerprintModel, withRaw bool) canary.FingerprintResult {
	v := canary.FingerprintResult{
		ID: row.ID, RoundID: row.RoundID, Trigger: row.Trigger, AccountID: row.AccountID, AccountName: row.AccountName,
		Model: row.Model, Status: row.Status, Verdict: row.Verdict, TopModel: row.TopModel, TopFamily: row.TopFamily,
		Confidence: row.Confidence, ValidOutputs: row.ValidOutputs, OutputTokens: row.OutputTokens, DurationMS: row.DurationMS,
		ConsecutiveMismatch: row.ConsecutiveMismatch, Suspect: row.Suspect, Reasons: row.Reasons, Error: row.Error,
		RequestIDs: row.RequestIDs, CreatedAt: row.CreatedAt,
	}
	_ = json.Unmarshal([]byte(row.Candidates), &v.Candidates)
	_ = json.Unmarshal([]byte(row.ValidCounts), &v.ValidCounts)
	if v.Candidates == nil {
		v.Candidates = []canary.FingerprintCandidate{}
	}
	if v.ValidCounts == nil {
		v.ValidCounts = []int{}
	}
	if withRaw {
		_ = json.Unmarshal([]byte(row.Prompts), &v.Prompts)
		_ = json.Unmarshal([]byte(row.RawOutputs), &v.RawOutputs)
	}
	return v
}

// CreateFingerprint stores one fingerprint probe.
func (r *CanaryRepository) CreateFingerprint(ctx context.Context, value canary.FingerprintResult) (canary.FingerprintResult, error) {
	row := fingerprintToRow(value)
	row.ID = 0
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if err := r.db.db.WithContext(ctx).Create(&row).Error; err != nil {
		return canary.FingerprintResult{}, err
	}
	return fingerprintFromRow(row, true), nil
}

// ListFingerprints returns newest-first results without raw outputs.
func (r *CanaryRepository) ListFingerprints(ctx context.Context, accountID uint64, limit int) ([]canary.FingerprintResult, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.db.db.WithContext(ctx).Model(&canaryFingerprintModel{}).Omit("prompts", "raw_outputs").Order("created_at DESC, id DESC").Limit(limit)
	if accountID != 0 {
		query = query.Where("account_id = ?", accountID)
	}
	var rows []canaryFingerprintModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]canary.FingerprintResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, fingerprintFromRow(row, false))
	}
	return out, nil
}

// GetFingerprint returns one result including prompts and raw outputs.
func (r *CanaryRepository) GetFingerprint(ctx context.Context, id uint64) (canary.FingerprintResult, error) {
	var row canaryFingerprintModel
	if err := r.db.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return canary.FingerprintResult{}, err
	}
	return fingerprintFromRow(row, true), nil
}

// CountFingerprintRounds returns how many distinct fingerprint rounds were stored.
func (r *CanaryRepository) CountFingerprintRounds(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.db.WithContext(ctx).Model(&canaryFingerprintModel{}).Distinct("round_id").Count(&count).Error
	return count, err
}

// DeleteFingerprintsBefore prunes old fingerprint history.
func (r *CanaryRepository) DeleteFingerprintsBefore(ctx context.Context, cutoff time.Time) error {
	return r.db.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&canaryFingerprintModel{}).Error
}
