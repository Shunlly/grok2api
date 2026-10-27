package gateway

import (
	"context"
	"errors"
	"testing"

	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

type unavailableRouteResolver struct {
	aliasRouteResolver
	enabled map[string]bool
}

func (r *unavailableRouteResolver) GetByPublicIDCandidates(context.Context, string) ([]modeldomain.Route, error) {
	return nil, repository.ErrNotFound
}

func (r *unavailableRouteResolver) HasEnabledPublicRoute(_ context.Context, publicID string) (bool, error) {
	return r.enabled[publicID], nil
}

func TestModelLookupErrorDistinguishesUnavailableAccountsFromUnknownModel(t *testing.T) {
	service := &Service{models: &unavailableRouteResolver{enabled: map[string]bool{"grok-4.7-build-fast": true, "grok-4.5": true}}}
	ctx := context.Background()
	err := service.modelLookupError(ctx, "grok-4.7-build-fast")
	if !errors.Is(err, ErrModelAccountsUnavailable) || !errors.Is(err, ErrNoAvailableAccount) || errors.Is(err, ErrModelNotFound) {
		t.Fatalf("enabled route without accounts = %v", err)
	}
	if err := service.modelLookupError(ctx, "grok-4.5-low"); !errors.Is(err, ErrModelAccountsUnavailable) {
		t.Fatalf("effort alias of enabled route = %v", err)
	}
	if err := service.modelLookupError(ctx, "missing-model"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("unknown model = %v", err)
	}
	plain := &Service{models: &aliasRouteResolver{}}
	if err := plain.modelLookupError(ctx, "x"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("resolver without checker = %v", err)
	}
}

func TestCreateResponseReportsUnavailableAccountsForEnabledRoute(t *testing.T) {
	service := &Service{models: &unavailableRouteResolver{enabled: map[string]bool{"grok-4.7-build-fast": true}}}
	_, err := service.CreateResponse(context.Background(), Input{PublicModel: "grok-4.7-build-fast"})
	if !errors.Is(err, ErrModelAccountsUnavailable) {
		t.Fatalf("CreateResponse error = %v", err)
	}
}
