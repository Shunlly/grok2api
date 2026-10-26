// Package canary exposes the observe-only account canary prober to admins.
package canary

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	canaryapp "github.com/chenyme/grok2api/backend/internal/application/canary"
	"github.com/chenyme/grok2api/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *canaryapp.Service }

func NewHandler(service *canaryapp.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Register(router *gin.RouterGroup) {
	if h.service == nil {
		return
	}
	router.GET("/quality/canary", h.summary)
	router.POST("/quality/canary/run", h.run)
	router.GET("/quality/canary/results/:id", h.resultDetail)
	router.GET("/quality/account-flags", h.accountFlags)
	router.GET("/quality/canary/fingerprint", h.fingerprintSummary)
	router.POST("/quality/canary/fingerprint/run", h.fingerprintRun)
	router.GET("/quality/canary/fingerprint/results/:id", h.fingerprintDetail)
}

func (h *Handler) fingerprintSummary(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("history"))
	value, err := h.service.FingerprintSummary(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "canary_fingerprint_summary_failed", err.Error())
		return
	}
	response.Success(c, http.StatusOK, value)
}

type fingerprintRunRequest struct {
	AccountIDs []string `json:"accountIds"`
}

// fingerprintRun starts a manual fingerprint round (works even when the
// scheduled probe is disabled). Empty accountIds samples accounts.
func (h *Handler) fingerprintRun(c *gin.Context) {
	var body fingerprintRunRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			response.Error(c, http.StatusBadRequest, "invalidRequest", "无效的请求")
			return
		}
	}
	ids := make([]uint64, 0, len(body.AccountIDs))
	for _, raw := range body.AccountIDs {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			response.Error(c, http.StatusBadRequest, "invalidRequest", "无效的账号 ID")
			return
		}
		ids = append(ids, id)
	}
	if len(ids) > 20 {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "一次最多 20 个账号")
		return
	}
	roundID, err := h.service.TriggerFingerprint(c.Request.Context(), ids, "manual")
	if errors.Is(err, canaryapp.ErrFingerprintRunning) {
		response.Error(c, http.StatusConflict, "canary_fingerprint_running", err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "canary_fingerprint_trigger_failed", err.Error())
		return
	}
	response.Success(c, http.StatusAccepted, gin.H{"roundId": roundID})
}

func (h *Handler) fingerprintDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "无效的结果 ID")
		return
	}
	value, err := h.service.FingerprintDetail(c.Request.Context(), id)
	if errors.Is(err, canaryapp.ErrResultNotFound) {
		response.Error(c, http.StatusNotFound, "canary_result_not_found", err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "canary_result_failed", err.Error())
		return
	}
	response.Success(c, http.StatusOK, value)
}

func (h *Handler) summary(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("history"))
	value, err := h.service.Summary(c.Request.Context(), limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "canary_summary_failed", err.Error())
		return
	}
	response.Success(c, http.StatusOK, value)
}

func (h *Handler) run(c *gin.Context) {
	roundID, err := h.service.Trigger(c.Request.Context())
	if errors.Is(err, canaryapp.ErrRoundRunning) {
		response.Error(c, http.StatusConflict, "canary_round_running", err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "canary_trigger_failed", err.Error())
		return
	}
	response.Success(c, http.StatusAccepted, gin.H{"roundId": roundID})
}

// accountFlags lists accounts with real quality hits (quality_degraded audits
// or failed canary probes) in the last 7 days; healthy accounts are omitted.
func (h *Handler) accountFlags(c *gin.Context) {
	flags, err := h.service.AccountFlags(c.Request.Context(), 7*24*time.Hour)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "account_flags_failed", err.Error())
		return
	}
	response.Success(c, http.StatusOK, gin.H{"flags": flags})
}

// resultDetail returns one probe with its stored artifact (prompt, answer,
// reasoning, usage, check detail). artifact is null for older results.
func (h *Handler) resultDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "无效的结果 ID")
		return
	}
	value, err := h.service.ResultDetail(c.Request.Context(), id)
	if errors.Is(err, canaryapp.ErrResultNotFound) {
		response.Error(c, http.StatusNotFound, "canary_result_not_found", err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "canary_result_failed", err.Error())
		return
	}
	response.Success(c, http.StatusOK, value)
}
