package gateway

import (
	"bytes"
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
	"github.com/chenyme/grok2api/backend/internal/repository"
)

type always401Adapter struct {
	attempts  atomic.Int64
	refreshes atomic.Int64
}

func (a *always401Adapter) Provider() account.Provider { return account.ProviderBuild }
func (a *always401Adapter) Definition() provider.Definition {
	return testConversationDefinition(account.ProviderBuild)
}
func (a *always401Adapter) ForwardResponse(context.Context, provider.ResponseResourceRequest) (*provider.Response, error) {
	a.attempts.Add(1)
	return &provider.Response{
		StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized", Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"error":{"code":"unauthorized","message":"access token rejected"}}`)),
	}, nil
}
func (a *always401Adapter) RefreshCredential(context.Context, account.Credential) (provider.RefreshedCredential, error) {
	a.refreshes.Add(1)
	return provider.RefreshedCredential{EncryptedAccessToken: "access-new", EncryptedRefreshToken: "refresh-new", ExpiresAt: time.Now().Add(6 * time.Hour)}, nil
}

type scoped401Fixture struct {
	service  *Service
	adapter  *always401Adapter
	accounts *relational.AccountRepository
	ids      []uint64
	key      clientkey.Key
	verifies atomic.Int64
}

func newScoped401Fixture(t *testing.T, count int, verifyStatus int) *scoped401Fixture {
	t.Helper()
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "scoped401.db"))
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
	if err := modelRepo.UpsertDiscovered(ctx, account.ProviderBuild, []string{"grok-scoped"}); err != nil {
		t.Fatal(err)
	}
	fixture := &scoped401Fixture{adapter: &always401Adapter{}, accounts: accountRepo}
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("acct-%d", index)
		value, _, err := accountRepo.UpsertByIdentity(ctx, account.Credential{
			Provider: account.ProviderBuild, Name: name, SourceKey: name,
			EncryptedAccessToken: "access-old", EncryptedRefreshToken: "refresh-old", ExpiresAt: time.Now().Add(time.Hour),
			Enabled: true, AuthStatus: account.AuthStatusActive, Priority: 100, MaxConcurrent: 4,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := modelRepo.ReplaceAccountCapabilities(ctx, value.ID, []string{"grok-scoped"}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		fixture.ids = append(fixture.ids, value.ID)
	}
	fixture.key, err = keyRepo.Create(ctx, clientkey.Key{Name: "k", Prefix: "scoped", SecretHash: strings.Repeat("c", 64), EncryptedSecret: "e", Enabled: true, RPMLimit: 120, MaxConcurrent: 8})
	if err != nil {
		t.Fatal(err)
	}
	registry := provider.NewRegistry(fixture.adapter)
	sticky := memory.NewStickyStore()
	accountService := accountapp.NewService(accountRepo, auditRepo, memory.NewDeviceSessionStore(), sticky, registry, testCipher(t), nil)
	selector := NewSelector(accountRepo, memory.NewConcurrencyLimiter(), sticky, registry, time.Hour, time.Second, time.Minute)
	fixture.service = NewService(modelRepo, auditRepo, accountService, clientkeyapp.NewService(nil, nil, nil, 60, 4, nil), registry, selector, nil, 3)
	fixture.service.verifyCredential = func(context.Context, account.Credential) (int, error) {
		fixture.verifies.Add(1)
		return verifyStatus, nil
	}
	return fixture
}

func (f *scoped401Fixture) run(t *testing.T, body []byte) error {
	t.Helper()
	_, err := f.service.CreateResponse(context.Background(), Input{RequestID: "req-scoped", ClientKey: f.key, PublicModel: "grok-scoped", Body: body})
	if err == nil {
		t.Fatal("expected failure")
	}
	return err
}

func (f *scoped401Fixture) reauthCount(t *testing.T) int {
	t.Helper()
	marked := 0
	for _, id := range f.ids {
		value, err := f.accounts.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if value.AuthStatus == account.AuthStatusReauthRequired {
			marked++
		}
	}
	return marked
}

func smallBody() []byte { return []byte(`{"model":"grok-scoped","input":"hello"}`) }

func TestAll401AcrossEveryTriedAccountMarksNone(t *testing.T) {
	f := newScoped401Fixture(t, 3, http.StatusUnauthorized)
	_ = f.run(t, smallBody())
	if f.verifies.Load() < 2 {
		t.Fatalf("verifies = %d, want each tried account verified", f.verifies.Load())
	}
	if got := f.reauthCount(t); got != 0 {
		t.Fatalf("reauth marked = %d, want 0 when every tried account 401s", got)
	}
}

func TestSingleAccountConfirmed401IsMarked(t *testing.T) {
	f := newScoped401Fixture(t, 1, http.StatusUnauthorized)
	_ = f.run(t, smallBody())
	if got := f.reauthCount(t); got != 1 {
		t.Fatalf("reauth marked = %d, want 1", got)
	}
}

func TestVerifiedAccountKeeps401RequestScoped(t *testing.T) {
	f := newScoped401Fixture(t, 3, http.StatusOK)
	err := f.run(t, smallBody())
	var failure *UpstreamFailure
	if !errors.As(err, &failure) || failure.HTTPStatus != http.StatusGatewayTimeout || failure.Code != "upstream_request_scoped_unauthorized" {
		t.Fatalf("err = %v", err)
	}
	if f.adapter.attempts.Load() != 2 || f.verifies.Load() != 1 {
		t.Fatalf("attempts=%d verifies=%d, want one account (primary+refresh retry) and no switch", f.adapter.attempts.Load(), f.verifies.Load())
	}
	if got := f.reauthCount(t); got != 0 {
		t.Fatalf("reauth marked = %d", got)
	}
}

func TestLargeBody401NeverMarksOrSwitches(t *testing.T) {
	f := newScoped401Fixture(t, 3, http.StatusUnauthorized)
	body := append([]byte(`{"model":"grok-scoped","input":"`), bytes.Repeat([]byte("x"), 600<<10)...)
	body = append(body, []byte(`"}`)...)
	err := f.run(t, body)
	var failure *UpstreamFailure
	if !errors.As(err, &failure) || failure.HTTPStatus != http.StatusRequestEntityTooLarge || failure.Code != RequestTooLargeCode {
		t.Fatalf("err = %v", err)
	}
	if f.adapter.attempts.Load() != 2 || f.adapter.refreshes.Load() != 1 || f.verifies.Load() != 0 {
		t.Fatalf("attempts=%d refreshes=%d verifies=%d", f.adapter.attempts.Load(), f.adapter.refreshes.Load(), f.verifies.Load())
	}
	if got := f.reauthCount(t); got != 0 {
		t.Fatalf("reauth marked = %d", got)
	}
}

func TestSlow401SkipsRefreshReauthAndFailover(t *testing.T) {
	f := newScoped401Fixture(t, 3, http.StatusUnauthorized)
	f.service.UpdateRequestScoped401(RequestScoped401Config{MinBytes: 512 << 10, MinElapsed: time.Nanosecond})
	err := f.run(t, smallBody())
	var failure *UpstreamFailure
	if !errors.As(err, &failure) || failure.HTTPStatus != http.StatusGatewayTimeout {
		t.Fatalf("err = %v", err)
	}
	if f.adapter.attempts.Load() != 1 || f.adapter.refreshes.Load() != 0 {
		t.Fatalf("attempts=%d refreshes=%d", f.adapter.attempts.Load(), f.adapter.refreshes.Load())
	}
	if got := f.reauthCount(t); got != 0 {
		t.Fatalf("reauth marked = %d", got)
	}
}

func TestShouldSkipPendingReauth(t *testing.T) {
	if !shouldSkipPendingReauth(3, 3) || shouldSkipPendingReauth(1, 1) || shouldSkipPendingReauth(1, 3) {
		t.Fatal("unexpected skip decision")
	}
}

var _ = repository.ErrNotFound
