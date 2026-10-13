package relational

import (
	"context"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/repository"
	"gorm.io/gorm"
)

var _ repository.AccountRecoveryRepository = (*AccountRepository)(nil)

// ListRecoveryCandidateIDs never returns admin-disabled, permanent or
// refresh_permanent accounts: those are excluded in SQL, and the scheduler
// re-checks the same rules on the loaded row before probing.
func (r *AccountRepository) ListRecoveryCandidateIDs(ctx context.Context, now time.Time, includeQuality bool, limit int) ([]uint64, error) {
	if limit < 1 {
		limit = 20
	}
	now = now.UTC()
	query := r.db.db.WithContext(ctx).Model(&accountModel{}).Select("id").
		Where("(recovery_next_check_at IS NULL OR recovery_next_check_at <= ?)", now).
		Where("NOT EXISTS (SELECT 1 FROM account_credentials credential WHERE credential.account_id = provider_accounts.id AND credential.refresh_permanent = ?)", true)
	transient := r.db.db.Where("enabled = ? AND auth_status = ? AND reauth_kind = ?", true, string(account.AuthStatusReauthRequired), string(account.ReauthKindTransient))
	if includeQuality {
		quality := r.db.db.Where("enabled = ? AND auth_status = ? AND disabled_source = ?", false, string(account.AuthStatusActive), string(account.DisabledSourceAutoQuality))
		query = query.Where(transient.Or(quality))
	} else {
		query = query.Where(transient)
	}
	var ids []uint64
	err := query.Order("recovery_next_check_at ASC, id ASC").Limit(limit).Pluck("id", &ids).Error
	return ids, mapError(err)
}

func (r *AccountRepository) SaveRecoveryState(ctx context.Context, id uint64, state repository.AccountRecoveryState) error {
	values := map[string]any{
		"reauth_kind": string(state.ReauthKind), "recovery_attempts": max(0, state.Attempts),
		"recovery_next_check_at": utcPointer(state.NextCheckAt), "recovery_last_check_at": utcPointer(state.LastCheckAt),
		"recovery_last_error": truncateRecoveryText(state.LastError),
	}
	result := r.db.db.WithContext(ctx).Model(&accountModel{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return mapError(result.Error)
	}
	if result.RowsAffected == 0 {
		return mapError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *AccountRepository) ReactivateAccount(ctx context.Context, id uint64, enable bool) error {
	var provider string
	err := r.db.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row accountModel
		if err := tx.Select("id", "provider").First(&row, id).Error; err != nil {
			return err
		}
		provider = row.Provider
		values := clearedRecoveryColumns()
		values["auth_status"] = string(account.AuthStatusActive)
		values["reauth_marked_at"] = nil
		values["last_error"] = ""
		values["failure_count"] = 0
		values["cooldown_until"] = nil
		if enable {
			values["enabled"] = true
			values["disabled_source"] = ""
			values["disabled_reason"] = ""
		}
		return tx.Model(&accountModel{}).Where("id = ?", id).Updates(values).Error
	})
	if err != nil {
		return mapError(err)
	}
	r.notifyInvalidation(ctx, repository.InvalidationEvent{Kind: repository.InvalidationAccountStateChanged, Provider: account.Provider(provider), AccountID: id})
	return nil
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
