package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	accountapp "github.com/chenyme/grok2api/backend/internal/application/account"
	clientkeyapp "github.com/chenyme/grok2api/backend/internal/application/clientkey"
	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/clientkey"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
)

type rlBuildAdapter struct {
	calls   atomic.Int64
	respond func(call int64) *provider.Response
}

func (a *rlBuildAdapter) Provider() account.Provider { return account.ProviderBuild }
func (a *rlBuildAdapter) Definition() provider.Definition {
	return testConversationDefinition(account.ProviderBuild)
}
func (a *rlBuildAdapter) ForwardResponse(context.Context, provider.ResponseResourceRequest) (*provider.Response, error) {
	return a.respond(a.calls.Add(1)), nil
}
func (a *rlBuildAdapter) RefreshCredential(context.Context, account.Credential) (provider.RefreshedCredential, error) {
	return provider.RefreshedCredential{EncryptedAccessToken: "a", EncryptedRefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type rlFixture struct {
	service  *Service
	accounts *relational.AccountRepository
	ids      []uint64
	key      clientkey.Key
}

func newRLFixture(t *testing.T, count, maxConcurrent int, adapter *rlBuildAdapter) *rlFixture {
	t.Helper()
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accountRepo := relational.NewAccountRepository(database)
	modelRepo := relational.NewModelRepository(database)
	auditRepo := relational.NewAuditRepository(database)
	keyRepo := relational.NewClientKeyRepository(database)
	if err := modelRepo.UpsertDiscovered(ctx, account.ProviderBuild, []string{"grok-rl"}); err != nil {
		t.Fatal(err)
	}
	f := &rlFixture{accounts: accountRepo}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("rl-%d", i)
		value, _, err := accountRepo.UpsertByIdentity(ctx, account.Credential{
			Provider: account.ProviderBuild, Name: name, SourceKey: name,
			EncryptedAccessToken: "a", EncryptedRefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour),
			Enabled: true, AuthStatus: account.AuthStatusActive, Priority: 100, MaxConcurrent: maxConcurrent,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := modelRepo.ReplaceAccountCapabilities(ctx, value.ID, []string{"grok-rl"}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		f.ids = append(f.ids, value.ID)
	}
	f.key, err = keyRepo.Create(ctx, clientkey.Key{Name: "k", Prefix: "rl", SecretHash: strings.Repeat("d", 64), EncryptedSecret: "e", Enabled: true, RPMLimit: 600, MaxConcurrent: 16})
	if err != nil {
		t.Fatal(err)
	}
	registry := provider.NewRegistry(adapter)
	sticky := memory.NewStickyStore()
	accountService := accountapp.NewService(accountRepo, auditRepo, memory.NewDeviceSessionStore(), sticky, registry, testCipher(t), nil)
	selector := NewSelector(accountRepo, memory.NewConcurrencyLimiter(), sticky, registry, time.Hour, time.Second, time.Minute)
	f.service = NewService(modelRepo, auditRepo, accountService, clientkeyapp.NewService(nil, nil, nil, 60, 4, nil), registry, selector, nil, 3)
	f.service.verifyCredential = func(context.Context, account.Credential) (int, error) { return http.StatusOK, nil }
	return f
}

func (f *rlFixture) run(ctx context.Context) (*Result, error) {
	return f.service.CreateResponse(ctx, Input{RequestID: "req-rl", ClientKey: f.key, PublicModel: "grok-rl", Body: []byte(`{"model":"grok-rl","input":"hi"}`)})
}

func rateLimitedResponse(retryAfter, body string) *provider.Response {
	header := make(http.Header)
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	header.Set("X-Grok-Req-Id", "req-123")
	header.Set("X-Api-Key", "secret-value")
	return &provider.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func TestBuildRateLimitCoolsAllAccountsAndReturns429WithRetryAfter(t *testing.T) {
	adapter := &rlBuildAdapter{respond: func(int64) *provider.Response {
		return rateLimitedResponse("7", `{"error":{"code":"rate_limited","message":"slow down"}}`)
	}}
	f := newRLFixture(t, 3, 4, adapter)
	f.service.UpdateQueue(QueueConfig{MaxWait: 0})
	_, err := f.run(context.Background())
	var failure *UpstreamFailure
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want UpstreamFailure", err)
	}
	if failure.HTTPStatus != http.StatusTooManyRequests || failure.Code != "upstream_rate_limited" || failure.RetryAfter != 7*time.Second {
		t.Fatalf("failure = %+v", failure)
	}
	if got := adapter.calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want one per account", got)
	}
	for _, id := range f.ids {
		value, err := f.accounts.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if value.AuthStatus == account.AuthStatusReauthRequired {
			t.Fatalf("account %d marked reauth on 429", id)
		}
		if value.FailureCount != 0 {
			t.Fatalf("account %d failure count = %d, want soft cooldown", id, value.FailureCount)
		}
		if value.CooldownUntil == nil || time.Until(*value.CooldownUntil) > 8*time.Second || time.Until(*value.CooldownUntil) < 5*time.Second {
			t.Fatalf("account %d cooldown = %v, want ~7s", id, value.CooldownUntil)
		}
	}
}

func TestBuildConcurrencyLimitCodeOnNon429IsRateLimit(t *testing.T) {
	failure := newHTTPUpstreamFailure(http.StatusServiceUnavailable, []byte(`{"error":{"code":"concurrency_limit"}}`), 1, "a")
	if !isBuildRateLimitFailure(http.StatusServiceUnavailable, failure, []byte(`{"error":{"code":"concurrency_limit"}}`)) {
		t.Fatal("concurrency_limit must be a rate-limit signal")
	}
	body := []byte(`{"error":{"type":"global_rate_limit"}}`)
	if !isBuildRateLimitFailure(http.StatusServiceUnavailable, newHTTPUpstreamFailure(503, body, 1, "a"), body) {
		t.Fatal("global_rate_limit must be a rate-limit signal")
	}
	if isBuildRateLimitFailure(http.StatusServiceUnavailable, newHTTPUpstreamFailure(503, []byte(`{}`), 1, "a"), []byte(`{}`)) {
		t.Fatal("plain 503 is not a rate limit")
	}
}

func TestBuildUsageExhaustedIsQuotaNotRateLimit(t *testing.T) {
	for _, code := range []string{"usage_pool_exhausted", "usage_limit_reached"} {
		body := []byte(`{"error":{"code":"` + code + `","message":"x"}}`)
		failure := newHTTPUpstreamFailure(http.StatusTooManyRequests, body, 1, "a")
		if !failure.QuotaExhausted {
			t.Fatalf("%s: QuotaExhausted = false", code)
		}
		if isBuildRateLimitFailure(http.StatusTooManyRequests, failure, body) {
			t.Fatalf("%s must use the quota path", code)
		}
	}
}

func TestRateLimitCooldownClamp(t *testing.T) {
	c := RateLimitConfig{MinCooldown: 5 * time.Second, MaxCooldown: 120 * time.Second, DefaultCooldown: 10 * time.Second}
	cases := map[time.Duration]time.Duration{0: 10 * time.Second, time.Second: 5 * time.Second, 30 * time.Second: 30 * time.Second, time.Hour: 120 * time.Second}
	for in, want := range cases {
		if got := c.cooldown(in); got != want {
			t.Fatalf("cooldown(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestQueueAdmission(t *testing.T) {
	if _, ok := queueAdmission(&SelectionUnavailableError{Reason: SelectionSaturated}, nil, time.Second); !ok {
		t.Fatal("saturated should queue")
	}
	if hint, ok := queueAdmission(&SelectionUnavailableError{Reason: SelectionCooling, RetryAfter: 2 * time.Second}, nil, 30*time.Second); !ok || hint != 2*time.Second {
		t.Fatal("short cooling should queue with hint")
	}
	if _, ok := queueAdmission(&SelectionUnavailableError{Reason: SelectionCooling, RetryAfter: time.Minute}, nil, 30*time.Second); ok {
		t.Fatal("long cooling must not queue")
	}
	if _, ok := queueAdmission(&SelectionUnavailableError{Reason: SelectionCooling, RetryAfter: time.Second}, &UpstreamFailure{}, 30*time.Second); ok {
		t.Fatal("cooling after own failures must not queue")
	}
	if _, ok := queueAdmission(&SelectionUnavailableError{Reason: SelectionNoAccounts}, nil, time.Second); ok {
		t.Fatal("no accounts must not queue")
	}
	if _, ok := queueAdmission(&SelectionUnavailableError{Reason: SelectionSaturated}, nil, 0); ok {
		t.Fatal("expired deadline must not queue")
	}
}

func TestAwaitQueueWakeCancelAndFull(t *testing.T) {
	selector := NewSelector(nil, nil, nil, nil, time.Hour, time.Second, time.Minute)
	s := &Service{selector: selector}
	done := make(chan bool, 1)
	go func() {
		ok, _ := s.awaitQueue(context.Background(), time.Now().Add(5*time.Second), 0, 10)
		done <- ok
	}()
	time.Sleep(50 * time.Millisecond)
	if QueuedRequests() != 1 {
		t.Fatalf("queued = %d, want 1", QueuedRequests())
	}
	selector.announceLeaseReturn()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("wake should retry")
		}
	case <-time.After(time.Second):
		t.Fatal("lease release did not wake queue")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.awaitQueue(ctx, time.Now().Add(time.Second), 0, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	queuedRequests.Add(1)
	ok, err := s.awaitQueue(context.Background(), time.Now().Add(time.Second), 0, 1)
	queuedRequests.Add(-1)
	if ok || err != nil {
		t.Fatal("full queue must reject immediately")
	}
	if QueuedRequests() != 0 {
		t.Fatalf("queued leak = %d", QueuedRequests())
	}
}

func TestQueueTimeoutReturns503WithRetryAfter(t *testing.T) {
	release := make(chan struct{})
	adapter := &rlBuildAdapter{}
	adapter.respond = func(int64) *provider.Response {
		<-release
		return rateLimitedResponse("1", `{"error":{"code":"rate_limited"}}`)
	}
	f := newRLFixture(t, 1, 1, adapter)
	f.service.UpdateQueue(QueueConfig{MaxWait: 300 * time.Millisecond, MaxLength: 8})
	first := make(chan error, 1)
	go func() { _, err := f.run(context.Background()); first <- err }()
	for adapter.calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	started := time.Now()
	_, err := f.run(context.Background())
	if elapsed := time.Since(started); elapsed < 250*time.Millisecond {
		t.Fatalf("second request returned after %v, want queued ~300ms", elapsed)
	}
	var selectionErr *SelectionUnavailableError
	if !errors.As(err, &selectionErr) || selectionErr.Reason != SelectionSaturated || selectionErr.RetryAfter != queueTimeoutRetryAfter || selectionErr.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("err = %#v, want saturated 503 with retry-after", err)
	}
	close(release)
	<-first
}

func TestQueuedRequestProceedsAfterLeaseRelease(t *testing.T) {
	release := make(chan struct{})
	adapter := &rlBuildAdapter{}
	adapter.respond = func(call int64) *provider.Response {
		if call == 1 {
			<-release
		}
		return rateLimitedResponse("1", `{"error":{"code":"rate_limited"}}`)
	}
	f := newRLFixture(t, 1, 1, adapter)
	f.service.UpdateQueue(QueueConfig{MaxWait: 5 * time.Second, MaxLength: 8})
	first := make(chan error, 1)
	go func() { _, err := f.run(context.Background()); first <- err }()
	for adapter.calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	second := make(chan error, 1)
	go func() { _, err := f.run(context.Background()); second <- err }()
	time.Sleep(100 * time.Millisecond)
	if adapter.calls.Load() != 1 {
		t.Fatalf("calls = %d, second request should wait", adapter.calls.Load())
	}
	close(release)
	<-first
	select {
	case err := <-second:
		// The first request cooled the only account; the queued request must
		// still reach a terminal 429/cooling answer rather than a 503 saturation.
		var selectionErr *SelectionUnavailableError
		if errors.As(err, &selectionErr) && selectionErr.Reason == SelectionSaturated {
			t.Fatalf("queued request still saturated: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queued request never woke")
	}
}

func TestUpstreamHeaderLogRedactsSecrets(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "3")
	header.Set("X-Grok-Req-Id", "abc")
	header.Set("X-Ratelimit-Remaining", "0")
	header.Set("X-Auth-Token", "s3cret")
	header.Set("Set-Cookie", "sid=1")
	header.Set("Content-Type", "application/json")
	got := upstreamHeaderLog(header)
	if got["retry-after"] != "3" || got["x-grok-req-id"] != "abc" || got["x-ratelimit-remaining"] != "0" {
		t.Fatalf("got %v", got)
	}
	if got["x-auth-token"] != "[REDACTED]" {
		t.Fatalf("token not redacted: %v", got)
	}
	if _, ok := got["set-cookie"]; ok {
		t.Fatal("cookie must be dropped")
	}
	if _, ok := got["content-type"]; ok {
		t.Fatal("non x-* header must be skipped")
	}
	audit := sanitizeDiagnosticHeaders(header)
	if v := audit["X-Grok-Req-Id"]; len(v) != 1 || v[0] != "abc" {
		t.Fatalf("audit headers = %v", audit)
	}
	if v := audit["X-Auth-Token"]; len(v) != 1 || v[0] != "[REDACTED]" {
		t.Fatalf("audit token = %v", audit)
	}
	if _, ok := audit["Set-Cookie"]; ok {
		t.Fatal("audit must drop cookies")
	}
}
