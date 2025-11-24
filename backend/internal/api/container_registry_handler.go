package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
	registry "github.com/ofkm/arcane-backend/internal/utils/registry"
)

type ContainerRegistryHandler struct {
	registryService *services.ContainerRegistryService
}

func NewContainerRegistryHandler(group *gin.RouterGroup, registryService *services.ContainerRegistryService, authMiddleware *middleware.AuthMiddleware) {
	handler := &ContainerRegistryHandler{registryService: registryService}

	apiGroup := group.Group("/container-registries")

	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("", handler.GetRegistries)
		apiGroup.POST("", handler.CreateRegistry)
		apiGroup.POST("/sync", handler.SyncRegistries)
		apiGroup.GET("/:id", handler.GetRegistry)
		apiGroup.PUT("/:id", handler.UpdateRegistry)
		apiGroup.DELETE("/:id", handler.DeleteRegistry)
		apiGroup.POST("/:id/test", handler.TestRegistry)
	}
}

func (h *ContainerRegistryHandler) GetRegistries(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryListError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       registries,
		"pagination": paginationResp,
	})
}

func (h *ContainerRegistryHandler) GetRegistry(c *gin.Context) {
	id := c.Param("id")

	registry, err := h.registryService.GetRegistryByID(c.Request.Context(), id)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryRetrievalError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := dto.MapOne[*models.ContainerRegistry, dto.ContainerRegistryDto](registry)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *ContainerRegistryHandler) CreateRegistry(c *gin.Context) {
	var req models.CreateContainerRegistryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	registry, err := h.registryService.CreateRegistry(c.Request.Context(), req)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryCreationError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := dto.MapOne[*models.ContainerRegistry, dto.ContainerRegistryDto](registry)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *ContainerRegistryHandler) UpdateRegistry(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateContainerRegistryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	registry, err := h.registryService.UpdateRegistry(c.Request.Context(), id, req)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryUpdateError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := dto.MapOne[*models.ContainerRegistry, dto.ContainerRegistryDto](registry)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

func (h *ContainerRegistryHandler) DeleteRegistry(c *gin.Context) {
	id := c.Param("id")

	if err := h.registryService.DeleteRegistry(c.Request.Context(), id); err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryDeletionError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Container registry deleted successfully"},
	})
}

func (h *ContainerRegistryHandler) TestRegistry(c *gin.Context) {
	id := c.Param("id")

	registry, err := h.registryService.GetRegistryByID(c.Request.Context(), id)
	if err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryRetrievalError{Err: err}).Error()},
		})
		return
	}

	decryptedToken, err := utils.Decrypt(registry.Token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TokenDecryptionError{Err: err}).Error()},
		})
		return
	}

	testResult, err := h.performRegistryTest(c.Request.Context(), registry, decryptedToken)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"data":    gin.H{"message": (&common.RegistryTestError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    testResult,
	})
}

func (h *ContainerRegistryHandler) SyncRegistries(c *gin.Context) {
	var req dto.SyncRegistriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if err := h.registryService.SyncRegistries(c.Request.Context(), req.Registries); err != nil {
		apiErr := models.ToAPIError(err)
		c.JSON(apiErr.HTTPStatus(), gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistrySyncError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Registries synced successfully"},
	})
}

func (h *ContainerRegistryHandler) performRegistryTest(ctx context.Context, registryModel *models.ContainerRegistry, decryptedToken string) (map[string]interface{}, error) {
	var creds *registry.Credentials
	if registryModel.Username != "" && decryptedToken != "" {
		creds = &registry.Credentials{
			Username: registryModel.Username,
			Token:    decryptedToken,
		}
	}

	testResult, err := registry.TestRegistryConnection(ctx, registryModel.URL, creds)
	if err != nil {
		return nil, err
	}

	if !testResult.AuthSuccess {
		if len(testResult.Errors) > 0 {
			return nil, fmt.Errorf("%s", testResult.Errors[0])
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	return map[string]interface{}{
		"message": "Authentication succeeded",
	}, nil
}
