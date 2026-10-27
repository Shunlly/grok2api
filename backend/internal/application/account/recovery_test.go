package account

import (
	"context"
	"errors"
	"testing"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

func newRecoveryTestService(t *testing.T, now time.Time) (*Service, *relational.AccountRepository, *[]uint64) {
	t.Helper()
	service, repo := newAutoCleanTestService(t, now)
	probed := []uint64{}
	service.SetRecoveryProbe(func(_ context.Context, value accountdomain.Credential) error {
		probed = append(probed, value.ID)
		return nil
	})
	return service, repo, &probed
}

func recoveryAccount(t *testing.T, repo *relational.AccountRepository, name string) accountdomain.Credential {
	t.Helper()
	return mustUpsert(t, repo, accountdomain.Credential{
		Provider: accountdomain.ProviderBuild, Name: name, SourceKey: name, EncryptedAccessToken: "x",
		Enabled: true, AuthStatus: accountdomain.AuthStatusActive,
	})
}

func mustGet(t *testing.T, repo *relational.AccountRepository, id uint64) accountdomain.Credential {
	t.Helper()
	value, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestClassifyReauthReason(t *testing.T) {
	cases := map[string]accountdomain.ReauthKind{
		"grok_build account is blocked":                                    accountdomain.ReauthKindPermanent,
		"grok_build upstream error code x matched the invalidation policy": accountdomain.ReauthKindPermanent,
		"OAuth refresh failed: invalid_grant":                              accountdomain.ReauthKindPermanent,
		permanentRefreshExpiredReason:                                      accountdomain.ReauthKindPermanent,
		"grok_build credential rejected":                                   accountdomain.ReauthKindTransient,
		"grok_build chat endpoint access denied":                           accountdomain.ReauthKindTransient,
	}
	for reason, want := range cases {
		if got := accountdomain.ClassifyReauthReason(reason, false); got != want {
			t.Fatalf("%q => %q, want %q", reason, got, want)
		}
	}
	if accountdomain.ClassifyReauthReason("credential rejected", true) != accountdomain.ReauthKindPermanent {
		t.Fatal("refresh_permanent must be permanent")
	}
}

func TestRecoveryBackoffSchedule(t *testing.T) {
	want := []time.Duration{90 * time.Second, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for attempts, expected := range want {
		if got := accountdomain.RecoveryBackoff(attempts); got != expected {
			t.Fatalf("attempt %d => %s, want %s", attempts, got, expected)
		}
	}
}

func TestMarkReauthRequiredTagsKind(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, _ := newRecoveryTestService(t, now)
	transient := recoveryAccount(t, repo, "transient")
	banned := recoveryAccount(t, repo, "banned")
	ctx := context.Background()
	if err := service.MarkReauthRequired(ctx, transient.ID, "grok_build credential rejected"); err != nil {
		t.Fatal(err)
	}
	if err := service.MarkReauthRequired(ctx, banned.ID, "grok_build account is blocked"); err != nil {
		t.Fatal(err)
	}
	got := mustGet(t, repo, transient.ID)
	if got.ReauthKind != accountdomain.ReauthKindTransient || got.RecoveryNextCheckAt == nil || !got.RecoveryNextCheckAt.Equal(now.Add(90*time.Second)) {
		t.Fatalf("transient = %+v", got)
	}
	got = mustGet(t, repo, banned.ID)
	if got.ReauthKind != accountdomain.ReauthKindPermanent || got.RecoveryNextCheckAt != nil {
		t.Fatalf("banned = %+v", got)
	}
	// Re-marking a transient account with a ban upgrades it to permanent.
	if err := service.MarkReauthRequired(ctx, transient.ID, "grok_build account is blocked"); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, repo, transient.ID); got.ReauthKind != accountdomain.ReauthKindPermanent {
		t.Fatalf("upgrade = %+v", got)
	}
}

func TestRecoveryRoundReactivatesTransientOnly(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, probed := newRecoveryTestService(t, now)
	ctx := context.Background()
	transient := recoveryAccount(t, repo, "transient")
	banned := recoveryAccount(t, repo, "banned")
	adminOff := recoveryAccount(t, repo, "admin-off")
	notDue := recoveryAccount(t, repo, "not-due")
	healthy := recoveryAccount(t, repo, "healthy")
	for _, id := range []uint64{transient.ID, adminOff.ID} {
		if err := service.MarkReauthRequired(ctx, id, "credential rejected"); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.MarkReauthRequired(ctx, banned.ID, "account is blocked"); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := service.BatchUpdate(ctx, accountdomain.ProviderBuild, []uint64{adminOff.ID}, UpdateInput{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(10 * time.Minute) }
	if err := service.MarkReauthRequired(ctx, notDue.ID, "credential rejected"); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(11 * time.Minute) }
	if err := service.runRecoveryRound(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(*probed) != 1 || (*probed)[0] != transient.ID {
		t.Fatalf("probed = %v, want only %d", *probed, transient.ID)
	}
	got := mustGet(t, repo, transient.ID)
	if got.AuthStatus != accountdomain.AuthStatusActive || got.LastError != "" || got.ReauthKind != "" || got.RecoveryNextCheckAt != nil {
		t.Fatalf("reactivated = %+v", got)
	}
	if got := mustGet(t, repo, adminOff.ID); got.Enabled || got.DisabledSource != accountdomain.DisabledSourceAdmin || got.AuthStatus != accountdomain.AuthStatusReauthRequired {
		t.Fatalf("admin-disabled touched: %+v", got)
	}
	if got := mustGet(t, repo, banned.ID); got.AuthStatus != accountdomain.AuthStatusReauthRequired {
		t.Fatalf("banned touched: %+v", got)
	}
	if got := mustGet(t, repo, healthy.ID); got.AuthStatus != accountdomain.AuthStatusActive || got.RecoveryAttempts != 0 {
		t.Fatalf("healthy changed: %+v", got)
	}
	// Manual recheck refuses admin disables and permanent accounts.
	for id, want := range map[uint64]string{adminOff.ID: RecheckSkipped, banned.ID: RecheckPermanent} {
		result, err := service.RecheckAccount(ctx, id)
		if err != nil || result.Outcome != want {
			t.Fatalf("recheck %d = %+v, %v; want %s", id, result, err, want)
		}
	}
	if len(*probed) != 1 {
		t.Fatalf("manual recheck probed protected accounts: %v", *probed)
	}
}

func TestRecoveryFailureBacksOffAndGivesUp(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, _ := newRecoveryTestService(t, now)
	ctx := context.Background()
	value := recoveryAccount(t, repo, "flaky")
	if err := service.MarkReauthRequired(ctx, value.ID, "credential rejected"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service.SetRecoveryProbe(func(context.Context, accountdomain.Credential) error {
		calls++
		return errors.New("upstream credential unauthorized")
	})
	clock := now.Add(10 * time.Minute)
	for attempt := 1; attempt <= recoveryMaxAttempts; attempt++ {
		service.now = func() time.Time { return clock }
		result, err := service.RecheckAccount(ctx, value.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := mustGet(t, repo, value.ID)
		if attempt < recoveryMaxAttempts {
			if result.Outcome != RecheckFailed || got.RecoveryAttempts != attempt || got.RecoveryNextCheckAt == nil {
				t.Fatalf("attempt %d result=%+v state=%+v", attempt, result, got)
			}
			delay := got.RecoveryNextCheckAt.Sub(clock)
			base := accountdomain.RecoveryBackoff(attempt)
			if delay < base || delay >= base+time.Minute {
				t.Fatalf("attempt %d delay %s, want %s+jitter", attempt, delay, base)
			}
			clock = *got.RecoveryNextCheckAt
		} else if result.Outcome != RecheckPermanent || got.ReauthKind != accountdomain.ReauthKindPermanent {
			t.Fatalf("final attempt result=%+v state=%+v", result, got)
		}
	}
	// A permanent account is no longer listed.
	ids, err := repo.ListRecoveryCandidateIDs(ctx, clock.Add(48*time.Hour), true, 10)
	if err != nil || len(ids) != 0 {
		t.Fatalf("candidates after give-up = %v, %v", ids, err)
	}
}

func TestRecoveryProbeBlockedErrorIsPermanent(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, _ := newRecoveryTestService(t, now)
	ctx := context.Background()
	value := recoveryAccount(t, repo, "later-banned")
	if err := service.MarkReauthRequired(ctx, value.ID, "credential rejected"); err != nil {
		t.Fatal(err)
	}
	service.SetRecoveryProbe(func(context.Context, accountdomain.Credential) error { return errors.New("account is blocked") })
	result, err := service.RecheckAccount(ctx, value.ID)
	if err != nil || result.Outcome != RecheckPermanent {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestRecoveryMaxAgeGivesUp(t *testing.T) {
	// The repository stamps reauth_marked_at with the wall clock, so anchor to it.
	now := time.Now().UTC()
	service, repo, probed := newRecoveryTestService(t, now)
	ctx := context.Background()
	value := recoveryAccount(t, repo, "old")
	if err := service.MarkReauthRequired(ctx, value.ID, "credential rejected"); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(6 * 24 * time.Hour) }
	result, err := service.RecheckAccount(ctx, value.ID)
	if err != nil || result.Outcome != RecheckPermanent || len(*probed) != 0 {
		t.Fatalf("result = %+v, %v, probed=%v", result, err, *probed)
	}
}

func TestManualRecheckHealthyHasNoSideEffects(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, probed := newRecoveryTestService(t, now)
	value := recoveryAccount(t, repo, "healthy")
	before := mustGet(t, repo, value.ID)
	result, err := service.RecheckAccount(context.Background(), value.ID)
	if err != nil || (result.Outcome != RecheckHealthy && result.Outcome != RecheckFailed) {
		t.Fatalf("result = %+v, %v", result, err)
	}
	after := mustGet(t, repo, value.ID)
	if len(*probed) != 0 || after.AuthStatus != before.AuthStatus || after.Enabled != before.Enabled || after.LastError != before.LastError || after.RecoveryAttempts != 0 {
		t.Fatalf("healthy recheck changed state: before=%+v after=%+v", before, after)
	}
}

func TestDisabledSourceTagging(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, _ := newRecoveryTestService(t, now)
	ctx := context.Background()
	admin := recoveryAccount(t, repo, "admin")
	quality := recoveryAccount(t, repo, "quality")
	disabled := false
	adminValue := mustGet(t, repo, admin.ID)
	adminValue.Enabled = false
	if _, err := repo.Update(ctx, adminValue); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, repo, admin.ID); got.DisabledSource != accountdomain.DisabledSourceAdmin {
		t.Fatalf("admin source = %q", got.DisabledSource)
	}
	if _, err := repo.UpdateMany(ctx, accountdomain.ProviderBuild, []uint64{quality.ID}, repository.AccountUpdates{Enabled: &disabled, DisabledSource: accountdomain.DisabledSourceAutoQuality, DisabledReason: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, repo, quality.ID); got.DisabledSource != accountdomain.DisabledSourceAutoQuality || got.DisabledReason != "x" {
		t.Fatalf("quality source = %+v", got)
	}
	enabled := true
	if _, err := service.BatchUpdate(ctx, accountdomain.ProviderBuild, []uint64{quality.ID}, UpdateInput{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, repo, quality.ID); got.DisabledSource != "" || got.DisabledReason != "" {
		t.Fatalf("re-enabled source = %+v", got)
	}
}

func TestQualityRecoveryRequiresOptInAndCanaryPass(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, _ := newRecoveryTestService(t, now)
	ctx := context.Background()
	value := recoveryAccount(t, repo, "quality")
	disabled := false
	if _, err := repo.UpdateMany(ctx, accountdomain.ProviderBuild, []uint64{value.ID}, repository.AccountUpdates{Enabled: &disabled, DisabledSource: accountdomain.DisabledSourceAutoQuality}); err != nil {
		t.Fatal(err)
	}
	pass := false
	calls := 0
	service.SetQualityRecheck(func(context.Context, uint64) (bool, string, error) {
		calls++
		return pass, "思考 token 过少", nil
	})
	if err := service.runRecoveryRound(ctx, false); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("quality recheck ran without opt-in")
	}
	service.SetAutoReactivateConfig(AutoReactivateConfig{Enabled: true, IncludeQualityDisabled: true})
	if err := service.runRecoveryRound(ctx, false); err != nil {
		t.Fatal(err)
	}
	got := mustGet(t, repo, value.ID)
	if calls != 1 || got.Enabled || got.DisabledSource != accountdomain.DisabledSourceAutoQuality || got.RecoveryAttempts != 1 || got.RecoveryNextCheckAt == nil {
		t.Fatalf("failed quality probe state = %+v calls=%d", got, calls)
	}
	pass = true
	service.now = func() time.Time { return got.RecoveryNextCheckAt.Add(time.Second) }
	if err := service.runRecoveryRound(ctx, false); err != nil {
		t.Fatal(err)
	}
	got = mustGet(t, repo, value.ID)
	if calls != 2 || !got.Enabled || got.DisabledSource != "" || got.CooldownUntil != nil || got.RecoveryAttempts != 0 {
		t.Fatalf("passed quality probe state = %+v", got)
	}
}

func TestFastRecoveryTickSkipsQualityCandidates(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	service, repo, probed := newRecoveryTestService(t, now)
	ctx := context.Background()
	transient := recoveryAccount(t, repo, "transient-fast")
	if err := service.MarkReauthRequired(ctx, transient.ID, "credential rejected"); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := service.runRecoveryRoundScoped(ctx, false, false); err != nil {
		t.Fatal(err)
	}
	if len(*probed) != 1 || (*probed)[0] != transient.ID {
		t.Fatalf("fast tick should probe the due transient account within 2 minutes, probed = %v", *probed)
	}
}
