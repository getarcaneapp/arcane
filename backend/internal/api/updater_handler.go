package api

import (
	"net/http"
	"strconv"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/updater"
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

// Run godoc
//
//	@Summary		Run updater
//	@Description	Apply pending container updates
//	@Tags			Updater
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Environment ID"
//	@Param			request	body		updater.Options		false	"Updater run options"
//	@Success		200		{object}	base.ApiResponse[updater.Result]
//	@Failure		500		{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/updater/run [post]
func (h *UpdaterHandler) Run(c *gin.Context) {
	var req updater.Options
	_ = c.ShouldBindJSON(&req)

	out, err := h.updaterService.ApplyPending(c.Request.Context(), req.DryRun)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": (&common.UpdaterRunError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// Status godoc
//
//	@Summary		Get updater status
//	@Description	Get the current status of the updater
//	@Tags			Updater
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[updater.Status]
//	@Router			/api/environments/{id}/updater/status [get]
func (h *UpdaterHandler) Status(c *gin.Context) {
	status := h.updaterService.GetStatus()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

// History godoc
//
//	@Summary		Get updater history
//	@Description	Get the history of update operations
//	@Tags			Updater
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id		path		string	true	"Environment ID"
//	@Param			limit	query		int		false	"Number of history entries to return"	default(50)
//	@Success		200		{object}	base.ApiResponse[[]updater.HistoryEntry]
//	@Failure		500		{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/updater/history [get]
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
