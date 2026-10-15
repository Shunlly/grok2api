package relational

import (
	"context"
	"time"

	"gorm.io/gorm/clause"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
)

type planSubscriptionModel struct {
	AccountID         uint64 `gorm:"primaryKey;autoIncrement:false"`
	Plan              string `gorm:"size:100;not null;default:''"`
	Tier              string `gorm:"size:100;not null;default:''"`
	Status            string `gorm:"size:100;not null;default:''"`
	BillingInterval   string `gorm:"size:100;not null;default:''"`
	BillingSystem     string `gorm:"size:100;not null;default:''"`
	PeriodEnd         *time.Time
	CancelAtPeriodEnd bool         `gorm:"not null;default:false"`
	Count             int          `gorm:"not null;default:0"`
	SyncedAt          time.Time    `gorm:"not null"`
	Account           accountModel `gorm:"foreignKey:AccountID;references:ID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE"`
}

func (planSubscriptionModel) TableName() string { return "account_plan_subscriptions" }

// SavePlanSubscription upserts the latest upstream subscription snapshot.
func (r *AccountRepository) SavePlanSubscription(ctx context.Context, value account.PlanSubscription) error {
	row := planSubscriptionModel{
		AccountID: value.AccountID, Plan: truncate(value.Plan, 100), Tier: truncate(value.Tier, 100), Status: truncate(value.Status, 100),
		BillingInterval: truncate(value.BillingInterval, 100), BillingSystem: truncate(value.BillingSystem, 100),
		PeriodEnd: value.PeriodEnd, CancelAtPeriodEnd: value.CancelAtPeriodEnd, Count: value.Count, SyncedAt: value.SyncedAt.UTC(),
	}
	return r.db.db.WithContext(ctx).Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
}

// GetPlanSubscriptions loads subscription snapshots keyed by account id.
func (r *AccountRepository) GetPlanSubscriptions(ctx context.Context, accountIDs []uint64) (map[uint64]account.PlanSubscription, error) {
	result := make(map[uint64]account.PlanSubscription, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	var rows []planSubscriptionModel
	if err := r.db.db.WithContext(ctx).Where("account_id IN ?", accountIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.AccountID] = account.PlanSubscription{
			AccountID: row.AccountID, Plan: row.Plan, Tier: row.Tier, Status: row.Status, BillingInterval: row.BillingInterval,
			BillingSystem: row.BillingSystem, PeriodEnd: row.PeriodEnd, CancelAtPeriodEnd: row.CancelAtPeriodEnd, Count: row.Count, SyncedAt: row.SyncedAt,
		}
	}
	return result, nil
}
