package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
)

type VersionHandler struct {
	version *services.VersionService
}

func NewVersionHandler(api *gin.RouterGroup, version *services.VersionService) *VersionHandler {
	h := &VersionHandler{version: version}
	api.GET("/version", h.Get)
	api.GET("/app-version", h.GetAppVersion)
	return h
}

// Get godoc
//
//	@Summary		Get version information
//	@Description	Get application version information and check for updates
//	@Tags			Version
//	@Param			current	query		string	false	"Current version to compare against"
//	@Success		200		{object}	version.Info
//	@Router			/api/version [get]
func (h *VersionHandler) Get(c *gin.Context) {
	current := strings.TrimSpace(c.Query("current"))

	info, err := h.version.GetVersionInformation(c.Request.Context(), current)
	if err != nil {
		slog.Warn("version information fetch error", "error", err)
	}
	c.JSON(http.StatusOK, info)
}

// GetAppVersion godoc
//
//	@Summary		Get app version
//	@Description	Get the current application version
//	@Tags			Version
//	@Success		200	{object}	version.Info
//	@Router			/api/app-version [get]
func (h *VersionHandler) GetAppVersion(c *gin.Context) {
	info := h.version.GetAppVersionInfo(c.Request.Context())
	c.JSON(http.StatusOK, info)
}
