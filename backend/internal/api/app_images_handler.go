package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/services"
	apphttp "github.com/ofkm/arcane-backend/internal/utils/http"
)

type ApplicationImagesHandler struct {
	appImagesService *services.ApplicationImagesService
}

func NewApplicationImagesHandler(group *gin.RouterGroup, appImagesService *services.ApplicationImagesService) {
	appImageHandler := &ApplicationImagesHandler{appImagesService: appImagesService}

	group.GET("/app-images/logo", appImageHandler.getLogo)
	group.GET("/app-images/favicon", appImageHandler.getFavicon)
	group.GET("/app-images/profile", appImageHandler.getDefaultProfile)
}

func (c *ApplicationImagesHandler) getLogo(ctx *gin.Context) {
	name := "logo"

	if fullParam := ctx.Query("full"); fullParam != "" {
		if full, err := strconv.ParseBool(fullParam); err == nil && full {
			name = "logo-full"
		}
	}

	c.getImage(ctx, name)
}

func (c *ApplicationImagesHandler) getFavicon(ctx *gin.Context) {
	c.getImage(ctx, "favicon")
}

func (c *ApplicationImagesHandler) getDefaultProfile(ctx *gin.Context) {
	c.getImage(ctx, "profile")
}

func (c *ApplicationImagesHandler) getImage(ctx *gin.Context, name string) {
	imageData, mimeType, err := c.appImagesService.GetImage(name)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.ImageRetrievalError{Err: err}).Error()}})
		return
	}

	ctx.Header("Content-Type", mimeType)
	apphttp.SetCacheControlHeader(ctx, 15*time.Minute, 24*time.Hour)
	ctx.Data(200, mimeType, imageData)
}
