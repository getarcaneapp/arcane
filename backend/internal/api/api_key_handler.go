package api

import (
	"errors"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/apikey"
)

type ApiKeyHandler struct {
	apiKeyService *services.ApiKeyService
}

func NewApiKeyHandler(group *gin.RouterGroup, apiKeyService *services.ApiKeyService, authMiddleware *middleware.AuthMiddleware) {
	handler := &ApiKeyHandler{apiKeyService: apiKeyService}

	apiGroup := group.Group("/api-keys")
	apiGroup.Use(authMiddleware.WithAdminRequired().Add())
	{
		apiGroup.GET("", handler.ListApiKeys)
		apiGroup.POST("", handler.CreateApiKey)
		apiGroup.GET("/:id", handler.GetApiKey)
		apiGroup.PUT("/:id", handler.UpdateApiKey)
		apiGroup.DELETE("/:id", handler.DeleteApiKey)
	}
}

// ListApiKeys godoc
//
//	@Summary		List API keys
//	@Description	Get a paginated list of API keys
//	@Tags			API Keys
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			search	query		string	false	"Search query for filtering by name or description"
//	@Param			sort	query		string	false	"Column to sort by"
//	@Param			order	query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Param			start	query		int		false	"Start index for pagination"	default(0)
//	@Param			limit	query		int		false	"Number of items per page"		default(20)
//	@Success		200		{object}	base.Paginated[apikey.ApiKey]
//	@Failure		500		{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/api-keys [get]
func (h *ApiKeyHandler) ListApiKeys(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	apiKeys, paginationResp, err := h.apiKeyService.ListApiKeys(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ApiKeyListError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       apiKeys,
		"pagination": paginationResp,
	})
}

// CreateApiKey godoc
//
//	@Summary		Create an API key
//	@Description	Create a new API key for programmatic access
//	@Tags			API Keys
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		apikey.Create	true	"API key creation request"
//	@Success		201		{object}	base.ApiResponse[apikey.ApiKeyCreatedDto]
//	@Failure		400		{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		401		{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500		{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/api-keys [post]
func (h *ApiKeyHandler) CreateApiKey(c *gin.Context) {
	user, ok := middleware.RequireAuthentication(c)
	if !ok {
		return
	}

	var req apikey.Create
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	apiKey, err := h.apiKeyService.CreateApiKey(c.Request.Context(), user.ID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ApiKeyCreationError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    apiKey,
	})
}

// GetApiKey godoc
//
//	@Summary		Get an API key
//	@Description	Get details of a specific API key by ID
//	@Tags			API Keys
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"API key ID"
//	@Success		200	{object}	base.ApiResponse[apikey.ApiKey]
//	@Failure		404	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/api-keys/{id} [get]
func (h *ApiKeyHandler) GetApiKey(c *gin.Context) {
	id := c.Param("id")

	apiKey, err := h.apiKeyService.GetApiKey(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ApiKeyNotFoundError{}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    apiKey,
	})
}

// UpdateApiKey godoc
//
//	@Summary		Update an API key
//	@Description	Update an existing API key's details
//	@Tags			API Keys
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"API key ID"
//	@Param			request	body		apikey.Update	true	"API key update request"
//	@Success		200		{object}	base.ApiResponse[apikey.ApiKey]
//	@Failure		400		{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		404		{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500		{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/api-keys/{id} [put]
func (h *ApiKeyHandler) UpdateApiKey(c *gin.Context) {
	id := c.Param("id")

	var req apikey.Update
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	apiKey, err := h.apiKeyService.UpdateApiKey(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, services.ErrApiKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"data":    gin.H{"error": (&common.ApiKeyNotFoundError{}).Error()},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ApiKeyUpdateError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    apiKey,
	})
}

// DeleteApiKey godoc
//
//	@Summary		Delete an API key
//	@Description	Delete an API key by ID
//	@Tags			API Keys
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"API key ID"
//	@Success		200	{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		404	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/api-keys/{id} [delete]
func (h *ApiKeyHandler) DeleteApiKey(c *gin.Context) {
	id := c.Param("id")

	if err := h.apiKeyService.DeleteApiKey(c.Request.Context(), id); err != nil {
		if errors.Is(err, services.ErrApiKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"data":    gin.H{"error": (&common.ApiKeyNotFoundError{}).Error()},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.ApiKeyDeletionError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "API key deleted successfully"},
	})
}
