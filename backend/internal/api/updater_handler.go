package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/services"
)

type UpdaterHandler struct {
	updaterService *services.UpdaterService
}

func NewUpdaterHandler(group *gin.RouterGroup, updaterService *services.UpdaterService, authMiddleware *middleware.AuthMiddleware) {
	handler := &UpdaterHandler{updaterService: updaterService}

	apiGroup := group.Group("/environments/:id/updater")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.POST("/run", handler.Run)
		apiGroup.GET("/history", handler.History)
		apiGroup.GET("/status", handler.Status)
	}
}

func (h *UpdaterHandler) Run(c *gin.Context) {
	var req dto.UpdaterRunRequest
	_ = c.ShouldBindJSON(&req)

	out, err := h.updaterService.ApplyPending(c.Request.Context(), req.DryRun)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.UpdaterRunError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

func (h *UpdaterHandler) Status(c *gin.Context) {
	status := h.updaterService.GetStatus()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

func (h *UpdaterHandler) History(c *gin.Context) {
	limit := 50
	if ls := c.Query("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil && v > 0 {
			limit = v
		}
	}

	history, err := h.updaterService.GetHistory(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.UpdaterHistoryError{Err: err}).Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": history})
}
