package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
)

type ImageHandler struct {
	imageService       *services.ImageService
	imageUpdateService *services.ImageUpdateService
	dockerService      *services.DockerClientService
	settingsService    *services.SettingsService
}

func NewImageHandler(group *gin.RouterGroup, dockerService *services.DockerClientService, imageService *services.ImageService, imageUpdateService *services.ImageUpdateService, settingsService *services.SettingsService, authMiddleware *middleware.AuthMiddleware) {
	handler := &ImageHandler{dockerService: dockerService, imageService: imageService, imageUpdateService: imageUpdateService, settingsService: settingsService}

	apiGroup := group.Group("/environments/:id/images")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("/counts", handler.GetImageUsageCounts)
		apiGroup.GET("", handler.List)
		apiGroup.GET("/:imageId", handler.GetByID)
		apiGroup.DELETE("/:imageId", handler.Remove)
		apiGroup.POST("/pull", handler.Pull)
		apiGroup.POST("/prune", handler.Prune)
		apiGroup.POST("/upload", handler.Upload)
	}
}

func (h *ImageHandler) List(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	if params.Limit == 0 {
		params.Limit = 20
	}

	images, paginationResp, err := h.imageService.ListImagesPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ImageListError{Err: err}).Error()},
		})
		return
	}

	pagination.ApplyFilterResultsHeaders(&c.Writer, pagination.FilterResult[dto.ImageSummaryDto]{
		Items:          images,
		TotalCount:     paginationResp.TotalItems,
		TotalAvailable: paginationResp.GrandTotalItems,
	})

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       images,
		"pagination": paginationResp,
	})
}

func (h *ImageHandler) GetByID(c *gin.Context) {
	id := c.Param("imageId")

	img, err := h.imageService.GetImageByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.ImageNotFoundError{Err: err}).Error()}})
		return
	}

	out := dto.NewImageDetailSummaryDto(img)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *ImageHandler) Remove(c *gin.Context) {
	id := c.Param("imageId")
	force := c.Query("force") == "true"

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}
	if err := h.imageService.RemoveImage(c.Request.Context(), id, force, *currentUser); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ImageRemovalError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Image removed successfully"},
	})
}

func (h *ImageHandler) Pull(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.ImagePullDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}
	c.Writer.Header().Set("Content-Type", "application/x-json-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	if err := h.imageService.PullImage(ctx, req.ImageName, c.Writer, *currentUser, req.Credentials); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ImagePullError{ImageName: req.ImageName, Err: err}).Error()},
		})
		return
	}

	slog.InfoContext(ctx, "Image pull stream completed",
		slog.String("imageName", req.ImageName))
}

func (h *ImageHandler) Prune(c *gin.Context) {
	dangling := c.Query("dangling") == "true"

	var req struct {
		Dangling *bool               `json:"dangling"`
		Filters  map[string][]string `json:"filters"`
	}
	if err := c.ShouldBindJSON(&req); err == nil {
		if req.Dangling != nil {
			dangling = *req.Dangling
		} else if vals, ok := req.Filters["dangling"]; ok {
			for _, v := range vals {
				if v == "true" || v == "1" {
					dangling = true
					break
				}
			}
		}
	} else if c.Request.ContentLength > 0 {
		// Only return error if body was provided but invalid
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	slog.DebugContext(c.Request.Context(), "Image prune request", slog.Bool("dangling_only", dangling))

	report, err := h.imageService.PruneImages(c.Request.Context(), dangling)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ImagePruneError{Err: err}).Error()},
		})
		return
	}

	out := dto.NewImagePruneReportDto(*report)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

func (h *ImageHandler) GetImageUsageCounts(c *gin.Context) {
	ctx := c.Request.Context()

	var (
		inuse, unused, total int
		totalSize            int64
		errs                 []error
	)

	_, iu, un, tot, err := h.dockerService.GetAllImages(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("get images: %w", err))
	} else {
		inuse, unused, total = iu, un, tot
	}

	sz, err := h.imageService.GetTotalImageSize(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("get total image size: %w", err))
	} else {
		totalSize = sz
	}

	if len(errs) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ImageUsageCountsError{Err: errors.Join(errs...)}).Error()},
		})
		return
	}

	out := dto.ImageUsageCountsDto{
		Inuse:     inuse,
		Unused:    unused,
		Total:     total,
		TotalSize: totalSize,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *ImageHandler) Upload(c *gin.Context) {
	ctx := context.Background()

	currentUser, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	// Stream the uploaded file directly to the Docker daemon to avoid buffering large files in memory
	mr, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidMultipartFormError{Err: err}).Error()},
		})
		return
	}

	var (
		part     *multipart.Part
		fileName string
		found    bool
	)

	for {
		p, err := mr.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.FileUploadReadError{Err: err}).Error()}})
			return
		}
		// Only consider file parts (have a filename)
		if p.FileName() != "" {
			part = p
			fileName = p.FileName()
			found = true
			break
		}
		// Discard and continue non-file parts
		_ = p.Close()
	}

	if !found || part == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.NoFileUploadedError{}).Error()}})
		return
	}
	defer part.Close()

	maxSizeMB := h.settingsService.GetIntSetting(ctx, "maxImageUploadSize", 500)
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	// Validate that the file is a Docker image tar archive
	// We allow .tar, .tar.gz, .tgz, .tar.xz
	lowerName := strings.ToLower(fileName)
	if !strings.HasSuffix(lowerName, ".tar") && !strings.HasSuffix(lowerName, ".tar.gz") && !strings.HasSuffix(lowerName, ".tgz") && !strings.HasSuffix(lowerName, ".tar.xz") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.InvalidFileFormatError{}).Error()}})
		return
	}

	result, err := h.imageService.LoadImageFromReader(ctx, part, fileName, *currentUser, maxSizeBytes)
	if err != nil {
		// Check if it's a size limit error
		if strings.Contains(err.Error(), "exceeds maximum allowed size") {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "data": gin.H{"error": err.Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.ImageLoadError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
