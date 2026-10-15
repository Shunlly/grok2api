package account

import (
	"context"
	"net/http"
	"time"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/repository"
	"github.com/chenyme/grok2api/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

// AccountModelLookup 读取单账号的模型能力快照与同 Provider 的公开路由。
type AccountModelLookup interface {
	AccountModelSnapshot(ctx context.Context, accountID uint64, provider string) (repository.AccountModelSnapshot, error)
}

// WithModels 设置账号模型视图的数据源；nil 时 /accounts/:id/models 返回 503。
func (h *Handler) WithModels(lookup AccountModelLookup) *Handler {
	if lookup != nil {
		h.models = lookup
	}
	return h
}

type accountModelItem struct {
	ID            string     `json:"id"`
	UpstreamModel string     `json:"upstreamModel"`
	Capability    string     `json:"capability,omitempty"`
	ContextWindow int        `json:"contextWindow,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	Detail        string     `json:"detail,omitempty"`
	Until         *time.Time `json:"until,omitempty"`
}

type accountUpstreamModel struct {
	ID            string   `json:"id"`
	ContextWindow int      `json:"contextWindow,omitempty"`
	PublicIDs     []string `json:"publicIds"`
}

type accountModelsResponse struct {
	AccountID       uint64                 `json:"accountId"`
	Provider        string                 `json:"provider"`
	Usable          []accountModelItem     `json:"usable"`
	Unavailable     []accountModelItem     `json:"unavailable"`
	All             []accountUpstreamModel `json:"all"`
	AccountBlockers []string               `json:"accountBlockers"`
	CapabilityKnown bool                   `json:"capabilityKnown"`
	FetchedAt       *time.Time             `json:"fetchedAt,omitempty"`
	LastAttemptAt   *time.Time             `json:"lastAttemptAt,omitempty"`
	LastError       string                 `json:"lastError,omitempty"`
	Source          string                 `json:"source"`
	ModelSyncFailed bool                   `json:"modelSyncFailed,omitempty"`
}

func (h *Handler) accountModels(c *gin.Context) {
	h.writeAccountModels(c, false)
}

func (h *Handler) refreshAccountModels(c *gin.Context) {
	h.writeAccountModels(c, true)
}

func (h *Handler) writeAccountModels(c *gin.Context, refresh bool) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if h.models == nil {
		response.Error(c, http.StatusServiceUnavailable, "accountModelsUnavailable", "账号模型视图未初始化")
		return
	}
	syncFailed := false
	if refresh {
		synchronizer, ok := h.sync.(accountModelSynchronizer)
		if !ok {
			response.Error(c, http.StatusServiceUnavailable, "accountModelSyncUnavailable", "模型同步器未初始化")
			return
		}
		syncFailed = synchronizer.SyncModels(c.Request.Context(), id) != nil
	}
	value, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		h.writeServiceError(c, "accountGetFailed", err, http.StatusInternalServerError, "读取账号失败")
		return
	}
	snapshot, err := h.models.AccountModelSnapshot(c.Request.Context(), id, string(value.Credential.Provider))
	if err != nil {
		h.writeServiceError(c, "accountModelsFailed", err, http.StatusInternalServerError, "读取账号模型失败")
		return
	}
	result := buildAccountModels(newAccountResponse(value), snapshot, time.Now().UTC())
	result.ModelSyncFailed = syncFailed
	response.Success(c, http.StatusOK, result)
}

// accountBlockers 列出让该账号整体暂不参与调度的原因（与具体模型无关）。
func accountBlockers(account accountResponse, now time.Time) []string {
	blockers := []string{}
	if !account.Enabled {
		blockers = append(blockers, "accountDisabled")
	}
	if account.AuthStatus != "" && account.AuthStatus != string(accountdomain.AuthStatusActive) {
		blockers = append(blockers, "reauthRequired")
	}
	if account.CooldownUntil != nil && now.Before(*account.CooldownUntil) {
		blockers = append(blockers, "accountCooldown")
	}
	if account.NoQuota {
		blockers = append(blockers, "noQuota")
	} else {
		switch account.Quota.Status {
		case "waitingReset":
			blockers = append(blockers, "quotaWaitingReset")
		case "probing":
			blockers = append(blockers, "quotaProbing")
		}
	}
	return blockers
}

func buildAccountModels(account accountResponse, snapshot repository.AccountModelSnapshot, now time.Time) accountModelsResponse {
	result := accountModelsResponse{
		AccountID: account.ID, Provider: account.Provider, Usable: []accountModelItem{}, Unavailable: []accountModelItem{},
		All: []accountUpstreamModel{}, AccountBlockers: accountBlockers(account, now),
		CapabilityKnown: snapshot.LastSuccessAt != nil, FetchedAt: snapshot.LastSuccessAt, LastAttemptAt: snapshot.LastAttemptAt,
		LastError: snapshot.LastError, Source: "accountModelCapabilities",
	}
	if !result.CapabilityKnown {
		result.Source = "unsynced"
	}
	supported := make(map[string]bool, len(snapshot.Capabilities))
	for _, model := range snapshot.Capabilities {
		supported[model] = true
	}
	blocks := map[string]modelQuotaBlockResponse{}
	for _, block := range account.ModelQuotaBlocks {
		if block.CooldownUntil.After(now) {
			blocks[block.Model] = block
		}
	}
	publicIDs := map[string][]string{}
	for _, route := range snapshot.Routes {
		item := accountModelItem{ID: route.PublicID, UpstreamModel: route.UpstreamModel, Capability: route.Capability, ContextWindow: upstreamContextWindow(account, route.UpstreamModel)}
		if route.Enabled {
			publicIDs[route.UpstreamModel] = append(publicIDs[route.UpstreamModel], route.PublicID)
		}
		switch {
		case !route.Enabled:
			item.Reason = "routeDisabled"
		case result.CapabilityKnown && !supported[route.UpstreamModel]:
			item.Reason = "notSupported"
		case route.HasBindings && !route.Bound:
			item.Reason = "notBound"
		case !allowsModel(account.AllowedModels, route.UpstreamModel):
			item.Reason = "notAllowed"
		default:
			if block, blocked := blocks[route.UpstreamModel]; blocked {
				until := block.CooldownUntil
				item.Reason, item.Detail, item.Until = "modelQuotaBlocked", block.Reason, &until
			} else if len(result.AccountBlockers) > 0 {
				item.Reason = result.AccountBlockers[0]
			}
		}
		if item.Reason == "" {
			result.Usable = append(result.Usable, item)
		} else {
			result.Unavailable = append(result.Unavailable, item)
		}
	}
	for _, model := range snapshot.Capabilities {
		ids := publicIDs[model]
		if ids == nil {
			ids = []string{}
		}
		result.All = append(result.All, accountUpstreamModel{ID: model, ContextWindow: upstreamContextWindow(account, model), PublicIDs: ids})
	}
	return result
}

// upstreamContextWindow 返回 Build 模型的有效上下文（账号手动设置优先）；其他 Provider 不提供。
func upstreamContextWindow(account accountResponse, model string) int {
	if account.Provider != string(accountdomain.ProviderBuild) {
		return 0
	}
	profile, ok := modeldomain.UpstreamProfile(model)
	if !ok {
		return 0
	}
	if account.ContextWindow > 0 {
		return account.ContextWindow
	}
	return profile.EffectiveContextWindow()
}

func allowedModelsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func allowsModel(allowed []string, upstreamModel string) bool {
	return accountdomain.Credential{AllowedModels: allowed}.AllowsModel(upstreamModel)
}
