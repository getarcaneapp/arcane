package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/services"
)

type ImageUpdateHandler struct {
	imageUpdateService *services.ImageUpdateService
}

func NewImageUpdateHandler(group *gin.RouterGroup, imageUpdateService *services.ImageUpdateService, authMiddleware *middleware.AuthMiddleware) {
	handler := &ImageUpdateHandler{imageUpdateService: imageUpdateService}

	apiGroup := group.Group("/environments/:id/image-updates")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("/check", handler.CheckImageUpdate)
		apiGroup.GET("/check/:imageId", handler.CheckImageUpdateByID)
		apiGroup.POST("/check/:imageId", handler.CheckImageUpdateByID)
		apiGroup.POST("/check-batch", handler.CheckMultipleImages)
		apiGroup.POST("/check-all", handler.CheckAllImages)
		apiGroup.GET("/summary", handler.GetUpdateSummary)
	}
}

func (h *ImageUpdateHandler) CheckImageUpdate(c *gin.Context) {
	imageRef := c.Query("imageRef")
	if imageRef == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ImageRefRequiredError{}).Error(),
		})
		return
	}

	result, err := h.imageUpdateService.CheckImageUpdate(c.Request.Context(), imageRef)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ImageUpdateCheckError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func (h *ImageUpdateHandler) CheckImageUpdateByID(c *gin.Context) {
	imageID := c.Param("imageId")
	if imageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ImageIDRequiredError{}).Error(),
		})
		return
	}

	result, err := h.imageUpdateService.CheckImageUpdateByID(c.Request.Context(), imageID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.ImageUpdateCheckError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func (h *ImageUpdateHandler) CheckMultipleImages(c *gin.Context) {
	var req dto.BatchImageUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if len(req.ImageRefs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   (&common.ImageRefListRequiredError{}).Error(),
		})
		return
	}

	results, err := h.imageUpdateService.CheckMultipleImages(c.Request.Context(), req.ImageRefs, req.Credentials)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.BatchImageUpdateCheckError{Err: err}).Error(),
		})
		return
	}

	response := dto.BatchImageUpdateResponse(results)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

func (h *ImageUpdateHandler) CheckAllImages(c *gin.Context) {
	var req dto.BatchImageUpdateRequest
	_ = c.ShouldBindJSON(&req)

	results, err := h.imageUpdateService.CheckAllImages(c.Request.Context(), 0, req.Credentials)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.AllImageUpdateCheckError{Err: err}).Error(),
		})
		return
	}

	response := dto.BatchImageUpdateResponse(results)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

func (h *ImageUpdateHandler) GetUpdateSummary(c *gin.Context) {
	summary, err := h.imageUpdateService.GetUpdateSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.UpdateSummaryError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}
