package gateway

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
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

func TestRequestTooLargeErrorMessage(t *testing.T) {
	err := &RequestTooLargeError{Size: 5452595, Limit: 3 << 20}
	msg := err.Error()
	for _, want := range []string{"请求过大（5.2 MB > 3 MB）", "/compact", "Request too large (5.2 MB > 3 MB)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
}

func TestCheckUpstreamRequestSizeDisabledAndBoundary(t *testing.T) {
	s := &Service{}
	if s.checkUpstreamRequestSize(100<<20) != nil {
		t.Fatal("guard must be disabled at 0")
	}
	s.UpdateMaxUpstreamRequestBytes(3 << 20)
	if s.checkUpstreamRequestSize(3<<20) != nil {
		t.Fatal("exact limit must pass")
	}
	if s.checkUpstreamRequestSize(3<<20+1) == nil {
		t.Fatal("limit+1 must be rejected")
	}
}

func TestIsSlowLargeRequestFailure(t *testing.T) {
	old := time.Now().Add(-2 * slowLargeRequestThreshold)
	if !isSlowLargeRequestFailure(true, old) || isSlowLargeRequestFailure(false, old) || isSlowLargeRequestFailure(true, time.Now()) {
		t.Fatal("unexpected slow large classification")
	}
}

func TestGatewayRejectsOversizedRequestBeforeUpstream(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accountRepo := relational.NewAccountRepository(database)
	modelRepo := relational.NewModelRepository(database)
	auditRepo := relational.NewAuditRepository(database)
	keyRepo := relational.NewClientKeyRepository(database)
	first, _, err := accountRepo.UpsertByIdentity(ctx, account.Credential{Provider: account.ProviderBuild, Name: "first", SourceKey: "first", EncryptedAccessToken: "one", ExpiresAt: time.Now().Add(time.Hour), Enabled: true, AuthStatus: account.AuthStatusActive, Priority: 200, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := modelRepo.UpsertDiscovered(ctx, account.ProviderBuild, []string{"grok-test"}); err != nil {
		t.Fatal(err)
	}
	if err := modelRepo.ReplaceAccountCapabilities(ctx, first.ID, []string{"grok-test"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	clientKey, err := keyRepo.Create(ctx, clientkey.Key{Name: "test-key", Prefix: "test-prefix", SecretHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", EncryptedSecret: "encrypted-key", Enabled: true, RPMLimit: 120, MaxConcurrent: 8})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &failoverAdapter{firstID: 0}
	registry := provider.NewRegistry(adapter)
	sticky := memory.NewStickyStore()
	accountService := accountapp.NewService(accountRepo, auditRepo, memory.NewDeviceSessionStore(), sticky, registry, testCipher(t), nil)
	selector := NewSelector(accountRepo, memory.NewConcurrencyLimiter(), sticky, registry, time.Hour, time.Second, time.Minute)
	service := NewService(modelRepo, auditRepo, accountService, clientkeyapp.NewService(nil, nil, nil, 60, 4, nil), registry, selector, nil, 3)
	service.UpdateMaxUpstreamRequestBytes(1024)

	body := append([]byte(`{"model":"grok-test","input":"`), bytes.Repeat([]byte("x"), 4096)...)
	body = append(body, []byte(`"}`)...)
	started := time.Now()
	_, err = service.CreateResponse(ctx, Input{RequestID: "req-big", ClientKey: clientKey, PublicModel: "grok-test", Body: body})
	var tooLarge *RequestTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 1024 {
		t.Fatalf("err = %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("guard must fail fast")
	}
	if len(adapter.attempts) != 0 {
		t.Fatalf("upstream attempts = %v", adapter.attempts)
	}
	logs, total, err := auditRepo.List(ctx, 0, 10)
	if err != nil || total != 1 || logs[0].ErrorCode != RequestTooLargeCode || logs[0].StatusCode != http.StatusRequestEntityTooLarge || logs[0].AccountID != nil {
		t.Fatalf("audit = %#v, %d, %v", logs, total, err)
	}
	stored, err := accountRepo.Get(ctx, first.ID)
	if err != nil || stored.AuthStatus != account.AuthStatusActive {
		t.Fatalf("account = %#v, err = %v", stored, err)
	}

	// Small requests still reach the upstream.
	result, err := service.CreateResponse(ctx, Input{RequestID: "req-small", ClientKey: clientKey, PublicModel: "grok-test", Body: []byte(`{"model":"grok-test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	result.Finalize(Usage{}, "", "")
	_ = result.Body.Close()
	if len(adapter.attempts) != 1 {
		t.Fatalf("small attempts = %v", adapter.attempts)
	}
}
