package gateway

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
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

// Upstream 5xx resubmits the same turn on the next account and must be
// reported as x-grok-transient-retry; Cloudflare 525/526 must not rotate.
func TestGatewayTransientRetryCountsUpstream5xxOnly(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		attempts int
		retries  []int
	}{
		{name: "502 resubmits with retry 1", status: http.StatusBadGateway, attempts: 2, retries: []int{0, 1}},
		{name: "429 rotates without transient retry", status: http.StatusTooManyRequests, attempts: 2, retries: []int{0, 0}},
		{name: "525 is fatal", status: 525, attempts: 1, retries: []int{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
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
			responseRepo := relational.NewResponseRepository(database)
			keyRepo := relational.NewClientKeyRepository(database)
			var ids []uint64
			for i, name := range []string{"first", "second"} {
				credential, _, err := accountRepo.UpsertByIdentity(ctx, account.Credential{Provider: account.ProviderBuild, Name: name, SourceKey: name, EncryptedAccessToken: name, ExpiresAt: time.Now().Add(time.Hour), Enabled: true, AuthStatus: account.AuthStatusActive, Priority: 200 - i*100, MaxConcurrent: 1})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, credential.ID)
			}
			if err := modelRepo.UpsertDiscovered(ctx, account.ProviderBuild, []string{"grok-test"}); err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				if err := modelRepo.ReplaceAccountCapabilities(ctx, id, []string{"grok-test"}, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			clientKey, err := keyRepo.Create(ctx, clientkey.Key{Name: "test-key", Prefix: "test-prefix", SecretHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", EncryptedSecret: "encrypted-key", Enabled: true, RPMLimit: 120, MaxConcurrent: 8})
			if err != nil {
				t.Fatal(err)
			}
			adapter := &failoverAdapter{firstID: ids[0], failureStatus: test.status, failureBody: `{"error":{"message":"upstream failure"}}`}
			registry := provider.NewRegistry(adapter)
			sticky := memory.NewStickyStore()
			accountService := accountapp.NewService(accountRepo, auditRepo, memory.NewDeviceSessionStore(), sticky, registry, testCipher(t), nil)
			selector := NewSelector(accountRepo, memory.NewConcurrencyLimiter(), sticky, registry, time.Hour, time.Second, time.Minute)
			service := NewService(modelRepo, auditRepo, accountService, clientkeyapp.NewService(nil, nil, nil, 60, 4, nil), registry, selector, responseRepo, 3)
			result, err := service.CreateResponse(ctx, Input{RequestID: "req-" + test.name, ClientKey: clientKey, PublicModel: "grok-test", Body: []byte(`{"model":"grok-test"}`)})
			if err == nil {
				_, _ = io.ReadAll(result.Body)
				result.Finalize(Usage{}, "resp-"+test.name, "")
				_ = result.Body.Close()
			}
			if len(adapter.attempts) != test.attempts {
				t.Fatalf("attempts = %#v, want %d (err=%v)", adapter.attempts, test.attempts, err)
			}
			got := adapter.transientRetries
			if len(got) != len(test.retries) {
				t.Fatalf("transient retries = %#v, want %#v", got, test.retries)
			}
			for i := range got {
				if got[i] != test.retries[i] {
					t.Fatalf("transient retries = %#v, want %#v", got, test.retries)
				}
			}
		})
	}
}
