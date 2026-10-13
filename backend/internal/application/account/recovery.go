package account

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

// AutoReactivateConfig controls the automatic re-activation scheduler.
//
// Only accounts whose failure was classified as transient are probed. Admin
// disables, permanent reauth (bans, policy 403s, revoked refresh tokens) and
// refresh_permanent credentials are never touched: a revoked or banned
// credential cannot be honestly revived and needs a fresh login or import.
type AutoReactivateConfig struct {
	Enabled                bool
	Interval               time.Duration
	MaxAge                 time.Duration
	IncludeQualityDisabled bool
}

const (
	recoveryBatchLimit     = 20
	recoveryProbeTimeout   = 90 * time.Second
	recoveryQualityTimeout = 6 * time.Minute
	recoveryInitialDelay   = 2 * time.Minute
	// recoveryMaxAttempts is the attempt cap; with the 10m/30m/2h/6h/24h
	// backoff it is reached after roughly five days of failed probes.
	recoveryMaxAttempts = 9
	recoveryQualityHold = 15 * time.Minute
)

// Recheck outcomes returned to the admin API.
const (
	RecheckReactivated = "reactivated"
	RecheckHealthy     = "healthy"
	RecheckFailed      = "failed"
	RecheckPermanent   = "permanent"
	RecheckSkipped     = "skipped"
)

// RecheckResult describes one recovery probe.
type RecheckResult struct {
	AccountID   uint64     `json:"accountId,string"`
	Outcome     string     `json:"outcome"`
	Message     string     `json:"message"`
	Attempts    int        `json:"attempts"`
	NextCheckAt *time.Time `json:"nextCheckAt,omitempty"`
}

// QualityRecheckFunc runs one canary probe for a quality-disabled account.
type QualityRecheckFunc func(ctx context.Context, accountID uint64) (passed bool, detail string, err error)

// RecoveryProbeFunc performs one cheap authenticated upstream call.
type RecoveryProbeFunc func(ctx context.Context, credential accountdomain.Credential) error

func normalizeAutoReactivateConfig(value AutoReactivateConfig) AutoReactivateConfig {
	if value.Interval < time.Minute {
		value.Interval = 10 * time.Minute
	}
	if value.MaxAge <= 0 {
		value.MaxAge = 5 * 24 * time.Hour
	}
	return value
}

// SetAutoReactivateConfig sets the static scheduler policy.
func (s *Service) SetAutoReactivateConfig(value AutoReactivateConfig) {
	s.recoveryMu.Lock()
	s.autoReactivate = normalizeAutoReactivateConfig(value)
	s.recoveryMu.Unlock()
}

func (s *Service) autoReactivateConfig() AutoReactivateConfig {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return s.autoReactivate
}

// SetQualityRecheck wires the canary probe used before re-enabling auto_quality accounts.
func (s *Service) SetQualityRecheck(value QualityRecheckFunc) { s.qualityRecheck = value }

// SetRecoveryProbe overrides the auth probe (tests).
func (s *Service) SetRecoveryProbe(value RecoveryProbeFunc) { s.recoveryProbe = value }

func (s *Service) recoveryRepo() (repository.AccountRecoveryRepository, bool) {
	value, ok := s.accounts.(repository.AccountRecoveryRepository)
	return value, ok
}

// RunAccountRecovery is the background loop: every interval it probes due
// candidates one at a time with a random gap between probes.
func (s *Service) RunAccountRecovery(ctx context.Context) {
	cfg := s.autoReactivateConfig()
	if !cfg.Enabled {
		return
	}
	if _, ok := s.recoveryRepo(); !ok {
		return
	}
	timer := time.NewTimer(recoveryInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := s.runRecoveryRound(ctx, true); err != nil && ctx.Err() == nil {
			s.logger.Warn("account_recovery_round_failed", "error", err)
		}
		timer.Reset(s.autoReactivateConfig().Interval)
	}
}

func (s *Service) runRecoveryRound(ctx context.Context, jitter bool) error {
	repo, ok := s.recoveryRepo()
	if !ok {
		return nil
	}
	cfg := s.autoReactivateConfig()
	includeQuality := cfg.IncludeQualityDisabled && s.qualityRecheck != nil
	ids, err := repo.ListRecoveryCandidateIDs(ctx, s.now(), includeQuality, recoveryBatchLimit)
	if err != nil {
		return err
	}
	for index, id := range ids {
		if index > 0 && jitter {
			gap := 5*time.Second + time.Duration(rand.Int64N(int64(25*time.Second)))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(gap):
			}
		}
		if _, err := s.recheck(ctx, id, false); err != nil && ctx.Err() == nil {
			s.logger.Warn("account_recovery_probe_error", "account_id", id, "error", err)
		}
	}
	return nil
}

// RecheckAccount runs one probe now (admin "立即重检"). Healthy active accounts
// get a read-only auth check with no state change.
func (s *Service) RecheckAccount(ctx context.Context, id uint64) (RecheckResult, error) {
	return s.recheck(ctx, id, true)
}

func (s *Service) recheck(ctx context.Context, id uint64, manual bool) (RecheckResult, error) {
	// One probe at a time across the scheduler and admin requests.
	s.recoveryProbeMu.Lock()
	defer s.recoveryProbeMu.Unlock()
	value, err := s.accounts.Get(ctx, id)
	if err != nil {
		return RecheckResult{}, mapRepositoryError(err)
	}
	result := RecheckResult{AccountID: id, Attempts: value.RecoveryAttempts, NextCheckAt: value.RecoveryNextCheckAt}
	source := accountdomain.EffectiveDisabledSource(value.Enabled, value.DisabledSource)
	switch {
	case value.Enabled && value.AuthStatus == accountdomain.AuthStatusActive:
		if !manual {
			result.Outcome, result.Message = RecheckSkipped, "账号已正常"
			return result, nil
		}
		probeCtx, cancel := context.WithTimeout(ctx, recoveryProbeTimeout)
		defer cancel()
		if err := s.readOnlyProbe(probeCtx, value); err != nil {
			result.Outcome, result.Message = RecheckFailed, "账号状态正常，但上游校验失败："+shortRecoveryError(err)
			return result, nil
		}
		result.Outcome, result.Message = RecheckHealthy, "账号正常，上游校验通过"
		return result, nil
	case source == accountdomain.DisabledSourceAdmin:
		result.Outcome, result.Message = RecheckSkipped, "管理员手动停用的账号不会自动恢复"
		return result, nil
	case value.AuthStatus == accountdomain.AuthStatusReauthRequired:
		if value.RefreshPermanent || value.ReauthKind == accountdomain.ReauthKindPermanent {
			result.Outcome, result.Message = RecheckPermanent, "凭据已永久失效或账号被封禁，需要重新登录或导入"
			return result, nil
		}
		if !value.Enabled {
			result.Outcome, result.Message = RecheckSkipped, "账号已停用，不自动恢复"
			return result, nil
		}
		return s.recheckReauth(ctx, value, result)
	case !value.Enabled && source == accountdomain.DisabledSourceAutoQuality:
		if s.qualityRecheck == nil {
			result.Outcome, result.Message = RecheckSkipped, "未配置质量重检"
			return result, nil
		}
		if !manual && !s.autoReactivateConfig().IncludeQualityDisabled {
			result.Outcome, result.Message = RecheckSkipped, "未开启质量停用账号的自动恢复"
			return result, nil
		}
		return s.recheckQuality(ctx, value, result)
	default:
		result.Outcome, result.Message = RecheckSkipped, "该状态不支持自动恢复"
		return result, nil
	}
}

func (s *Service) recheckReauth(ctx context.Context, value accountdomain.Credential, result RecheckResult) (RecheckResult, error) {
	repo, ok := s.recoveryRepo()
	if !ok {
		return result, ErrUnsupported
	}
	now := s.now()
	cfg := s.autoReactivateConfig()
	kind := value.ReauthKind
	if kind == accountdomain.ReauthKindNone {
		kind = accountdomain.ClassifyReauthReason(value.LastError, value.RefreshPermanent)
	}
	if kind == accountdomain.ReauthKindPermanent || (value.ReauthMarkedAt != nil && now.Sub(*value.ReauthMarkedAt) > cfg.MaxAge) || value.RecoveryAttempts >= recoveryMaxAttempts {
		return s.giveUpReauth(ctx, repo, value, result, "超过自动恢复期限或判定为永久失效")
	}
	probe := s.recoveryProbe
	if probe == nil {
		probe = s.defaultRecoveryProbe
	}
	probeCtx, cancel := context.WithTimeout(ctx, recoveryProbeTimeout)
	probeErr := probe(probeCtx, value)
	cancel()
	if probeErr == nil {
		if err := repo.ReactivateAccount(ctx, value.ID, false); err != nil {
			return result, err
		}
		s.logAccountStateChange(value, string(accountdomain.AuthStatusReauthRequired), "active", "自动重检通过")
		if s.sticky != nil {
			_ = s.sticky.DeleteByAccount(ctx, value.ID)
		}
		result.Outcome, result.Message, result.Attempts, result.NextCheckAt = RecheckReactivated, "重检通过，账号已恢复", 0, nil
		return result, nil
	}
	// The probe itself may have re-marked the account; honour a permanent verdict.
	latest, err := s.accounts.Get(ctx, value.ID)
	if err == nil {
		if latest.AuthStatus == accountdomain.AuthStatusActive && latest.Enabled {
			// A token refresh succeeded but the verification call failed transiently:
			// put the account back into reauth so it keeps being rechecked.
			if markErr := s.MarkReauthRequired(ctx, value.ID, value.LastError); markErr != nil {
				return result, markErr
			}
			latest, _ = s.accounts.Get(ctx, value.ID)
		}
		value.RefreshPermanent = latest.RefreshPermanent
		if latest.ReauthKind == accountdomain.ReauthKindPermanent {
			kind = accountdomain.ReauthKindPermanent
		}
	}
	if value.RefreshPermanent || kind == accountdomain.ReauthKindPermanent || accountdomain.ClassifyReauthReason(probeErr.Error(), false) == accountdomain.ReauthKindPermanent {
		return s.giveUpReauth(ctx, repo, value, result, shortRecoveryError(probeErr))
	}
	attempts := value.RecoveryAttempts + 1
	if attempts >= recoveryMaxAttempts {
		return s.giveUpReauth(ctx, repo, value, result, shortRecoveryError(probeErr))
	}
	next := now.Add(accountdomain.RecoveryBackoff(attempts) + recoveryJitter())
	state := repository.AccountRecoveryState{ReauthKind: accountdomain.ReauthKindTransient, Attempts: attempts, NextCheckAt: &next, LastCheckAt: &now, LastError: shortRecoveryError(probeErr)}
	if err := repo.SaveRecoveryState(ctx, value.ID, state); err != nil {
		return result, err
	}
	s.logger.Info("account_recovery_probe_failed", "account_id", value.ID, "provider", value.Provider, "attempts", attempts, "next_check_at", next, "error", state.LastError)
	result.Outcome, result.Message, result.Attempts, result.NextCheckAt = RecheckFailed, "重检未通过："+state.LastError, attempts, &next
	return result, nil
}

func (s *Service) giveUpReauth(ctx context.Context, repo repository.AccountRecoveryRepository, value accountdomain.Credential, result RecheckResult, reason string) (RecheckResult, error) {
	now := s.now()
	state := repository.AccountRecoveryState{ReauthKind: accountdomain.ReauthKindPermanent, Attempts: value.RecoveryAttempts, LastCheckAt: &now, LastError: reason}
	if err := repo.SaveRecoveryState(ctx, value.ID, state); err != nil {
		return result, err
	}
	s.logAccountStateChange(value, "reauthRequired", "reauthRequired(permanent)", reason)
	result.Outcome, result.Message, result.NextCheckAt = RecheckPermanent, "已停止自动重检："+reason, nil
	return result, nil
}

func (s *Service) recheckQuality(ctx context.Context, value accountdomain.Credential, result RecheckResult) (RecheckResult, error) {
	repo, ok := s.recoveryRepo()
	if !ok {
		return result, ErrUnsupported
	}
	if value.RecoveryAttempts >= recoveryMaxAttempts {
		result.Outcome, result.Message = RecheckPermanent, "质量重检次数已用完，请手动处理"
		return result, nil
	}
	now := s.now()
	// Hold the account in cooldown while it is enabled for the probe so normal
	// traffic is not routed to it; the pinned recovery probe bypasses this hold.
	hold := now.Add(recoveryQualityHold)
	if err := s.accounts.UpdateHealth(ctx, value.ID, value.Provider, value.FailureCount, &hold, value.LastError, false); err != nil {
		return result, mapRepositoryError(err)
	}
	enabled := true
	if _, err := s.accounts.UpdateMany(ctx, value.Provider, []uint64{value.ID}, repository.AccountUpdates{Enabled: &enabled}); err != nil {
		return result, mapRepositoryError(err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, recoveryQualityTimeout)
	passed, detail, probeErr := s.qualityRecheck(probeCtx, value.ID)
	cancel()
	if probeErr == nil && passed {
		if err := repo.ReactivateAccount(ctx, value.ID, true); err != nil {
			return result, err
		}
		s.logAccountStateChange(value, "disabled(auto_quality)", "active", "金丝雀重检通过")
		result.Outcome, result.Message, result.Attempts, result.NextCheckAt = RecheckReactivated, "金丝雀重检通过，账号已恢复", 0, nil
		return result, nil
	}
	if detail == "" && probeErr != nil {
		detail = shortRecoveryError(probeErr)
	}
	disabled := false
	if _, err := s.accounts.UpdateMany(ctx, value.Provider, []uint64{value.ID}, repository.AccountUpdates{Enabled: &disabled, DisabledSource: accountdomain.DisabledSourceAutoQuality, DisabledReason: value.DisabledReason}); err != nil {
		return result, mapRepositoryError(err)
	}
	attempts := value.RecoveryAttempts + 1
	var next *time.Time
	if attempts < recoveryMaxAttempts {
		value := now.Add(accountdomain.RecoveryBackoff(attempts) + recoveryJitter())
		next = &value
	}
	if err := repo.SaveRecoveryState(ctx, value.ID, repository.AccountRecoveryState{Attempts: attempts, NextCheckAt: next, LastCheckAt: &now, LastError: detail}); err != nil {
		return result, err
	}
	s.logger.Info("account_recovery_quality_failed", "account_id", value.ID, "attempts", attempts, "next_check_at", next, "detail", detail)
	result.Outcome, result.Message, result.Attempts, result.NextCheckAt = RecheckFailed, "金丝雀重检未通过："+detail, attempts, next
	return result, nil
}

// defaultRecoveryProbe refreshes OAuth credentials when supported and then
// makes one cheap authenticated call (billing, or quota for SSO providers).
func (s *Service) defaultRecoveryProbe(ctx context.Context, value accountdomain.Credential) error {
	if s.providers != nil && s.providers.SupportsCredentialRefresh(value.Provider) && value.EncryptedRefreshToken != "" {
		refreshed, err := s.ensureCredential(ctx, value, ensureCredentialOptions{force: true, bypassCooldown: true})
		if err != nil {
			return err
		}
		value = refreshed
	}
	return s.readOnlyProbe(ctx, value)
}

// readOnlyProbe makes an authenticated read without forcing a token refresh.
func (s *Service) readOnlyProbe(ctx context.Context, value accountdomain.Credential) error {
	if s.providers == nil {
		return ErrUnsupported
	}
	if adapter, ok := s.providers.Billing(value.Provider); ok {
		credential, err := s.EnsureCredential(ctx, value, false)
		if err != nil {
			return err
		}
		_, err = adapter.GetBilling(ctx, credential)
		return err
	}
	_, err := s.RefreshQuota(ctx, value.ID)
	return err
}

// applyReauthClassification keeps the scheduler's bookkeeping when an account
// that is already reauthRequired is re-marked, unless the new cause is permanent.
func applyReauthClassification(value *accountdomain.Credential, previous accountdomain.Credential, reason string, now time.Time) {
	kind := accountdomain.ClassifyReauthReason(reason, value.RefreshPermanent)
	if previous.AuthStatus == accountdomain.AuthStatusReauthRequired && previous.ReauthKind != accountdomain.ReauthKindNone {
		if previous.ReauthKind == accountdomain.ReauthKindPermanent || kind == accountdomain.ReauthKindPermanent {
			value.ReauthKind = accountdomain.ReauthKindPermanent
			value.RecoveryNextCheckAt = nil
		}
		return
	}
	value.ReauthKind = kind
	value.RecoveryAttempts = 0
	value.RecoveryLastCheckAt = nil
	value.RecoveryLastError = ""
	value.RecoveryNextCheckAt = nil
	if kind == accountdomain.ReauthKindTransient {
		next := now.Add(accountdomain.RecoveryBackoff(0))
		value.RecoveryNextCheckAt = &next
	}
}

func (s *Service) logAccountStateChange(value accountdomain.Credential, from, to, reason string) {
	if s.logger == nil {
		return
	}
	s.logger.Warn("account_state_changed", "account_id", value.ID, "provider", value.Provider, "name", value.Name,
		"from", from, "to", to, "reauth_kind", value.ReauthKind, "reason", shortRecoveryText(reason))
}

func recoveryJitter() time.Duration { return time.Duration(rand.Int64N(int64(time.Minute))) }

func shortRecoveryError(err error) string {
	if err == nil {
		return ""
	}
	return shortRecoveryText(err.Error())
}

func shortRecoveryText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}
