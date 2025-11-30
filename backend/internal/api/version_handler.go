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

func (h *VersionHandler) Get(c *gin.Context) {
	current := strings.TrimSpace(c.Query("current"))

	info, err := h.version.GetVersionInformation(c.Request.Context(), current)
	if err != nil {
		slog.Warn("version information fetch error", "error", err)
	}
	c.JSON(http.StatusOK, info)
}

func (h *VersionHandler) GetAppVersion(c *gin.Context) {
	info := h.version.GetAppVersionInfo(c.Request.Context())
	c.JSON(http.StatusOK, info)
}
