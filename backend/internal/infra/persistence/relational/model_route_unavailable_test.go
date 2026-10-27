package relational

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

func TestHasEnabledPublicRouteSurvivesAccountReauthorization(t *testing.T) {
	ctx := context.Background()
	database := openTestDatabase(t)
	models := NewModelRepository(database)
	accounts := NewAccountRepository(database)
	build, _, err := accounts.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderBuild, Name: "build", SourceKey: "reauth-build",
		EncryptedAccessToken: testEncryptedToken, Enabled: true, AuthStatus: account.AuthStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := models.UpsertDiscovered(ctx, account.ProviderBuild, []string{"grok-reauth"}); err != nil {
		t.Fatal(err)
	}
	if err := models.ReplaceAccountCapabilities(ctx, build.ID, []string{"grok-reauth"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	build.AuthStatus = account.AuthStatusReauthRequired
	if _, err := accounts.Update(ctx, build); err != nil {
		t.Fatal(err)
	}
	if _, err := models.GetByPublicIDCandidates(ctx, "grok-reauth"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("route should be hidden from routing while reauthorizing, err = %v", err)
	}
	if exists, err := models.HasEnabledPublicRoute(ctx, "grok-reauth"); err != nil || !exists {
		t.Fatalf("enabled route must still exist: %v %v", exists, err)
	}
	if exists, err := models.HasEnabledPublicRoute(ctx, "grok-missing"); err != nil || exists {
		t.Fatalf("missing route = %v %v", exists, err)
	}
}
