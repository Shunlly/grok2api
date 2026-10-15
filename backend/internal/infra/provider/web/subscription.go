package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	domainegress "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

// SyncPlanSubscription reads GET /rest/subscriptions, the endpoint grok.com uses
// for its subscription settings page. It returns the plan's current billing
// period end (plan expiry), status and whether it is set to cancel at period end.
func (a *Adapter) SyncPlanSubscription(ctx context.Context, credential account.Credential) (account.PlanSubscription, error) {
	cfg := a.config()
	token, err := a.cipher.Decrypt(credential.EncryptedAccessToken)
	if err != nil {
		return account.PlanSubscription{}, err
	}
	lease, err := a.egress.AcquireCredential(ctx, domainegress.ScopeWeb, credential)
	if err != nil {
		return account.PlanSubscription{}, err
	}
	defer lease.Release()
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.QuotaTimeoutSeconds)*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, cfg.BaseURL+"/rest/subscriptions", nil)
	if err != nil {
		return account.PlanSubscription{}, err
	}
	request.Header = buildHeaders(token, lease, "")
	applyAppHeaders(request.Header, cfg.BaseURL, cfg.BaseURL+"/")
	a.applySignedStatsig(requestCtx, request, token, lease)
	response, err := lease.DoDeferredForbidden(request)
	if err != nil {
		return account.PlanSubscription{}, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()
	if err != nil {
		return account.PlanSubscription{}, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		return account.PlanSubscription{}, provider.ErrUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return account.PlanSubscription{}, fmt.Errorf("Grok Web 订阅接口返回 %d", response.StatusCode)
	}
	value, err := parsePlanSubscription(body)
	if err != nil {
		return account.PlanSubscription{}, err
	}
	value.AccountID = credential.ID
	value.SyncedAt = time.Now().UTC()
	return value, nil
}

type subscriptionsPayload struct {
	DominantPlan *struct {
		Plan string `json:"plan"`
	} `json:"dominantPlan"`
	Subscriptions []struct {
		Status            string `json:"status"`
		Tier              string `json:"tier"`
		BillingInterval   string `json:"billingInterval"`
		BillingSystem     string `json:"billingSystem"`
		BillingPeriodEnd  string `json:"billingPeriodEnd"`
		CancelAtPeriodEnd bool   `json:"cancelAtPeriodEnd"`
		Google            *struct {
			ExpiryTime string `json:"expiryTime"`
		} `json:"google"`
	} `json:"subscriptions"`
}

// parsePlanSubscription picks the active subscription with the latest period
// end. No date is invented: when upstream omits billingPeriodEnd (and the store
// expiryTime) PeriodEnd stays nil.
func parsePlanSubscription(body []byte) (account.PlanSubscription, error) {
	var payload subscriptionsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return account.PlanSubscription{}, fmt.Errorf("解析 Grok Web 订阅: %w", err)
	}
	result := account.PlanSubscription{Count: len(payload.Subscriptions)}
	if payload.DominantPlan != nil {
		result.Plan = strings.TrimSpace(payload.DominantPlan.Plan)
	}
	bestActive := false
	for _, item := range payload.Subscriptions {
		raw := strings.TrimSpace(item.BillingPeriodEnd)
		if raw == "" && item.Google != nil {
			raw = strings.TrimSpace(item.Google.ExpiryTime)
		}
		var end *time.Time
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			utc := parsed.UTC()
			end = &utc
		}
		active := item.Status == "SUBSCRIPTION_STATUS_ACTIVE"
		better := result.Status == "" ||
			(active && !bestActive) ||
			(active == bestActive && end != nil && (result.PeriodEnd == nil || end.After(*result.PeriodEnd)))
		if !better {
			continue
		}
		bestActive = active
		result.Status, result.Tier = strings.TrimSpace(item.Status), strings.TrimSpace(item.Tier)
		result.BillingInterval, result.BillingSystem = strings.TrimSpace(item.BillingInterval), strings.TrimSpace(item.BillingSystem)
		result.PeriodEnd, result.CancelAtPeriodEnd = end, item.CancelAtPeriodEnd
	}
	return result, nil
}
