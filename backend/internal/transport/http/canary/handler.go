// Package canary exposes the observe-only account canary prober to admins.
package canary

import (
	"errors"
	"net/http"
	"strconv"

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
