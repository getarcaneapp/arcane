package api

import (
	"encoding/json"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/env"
	"go.getarcane.app/types/template"
)

type TemplateHandler struct {
	templateService *services.TemplateService
}

func NewTemplateHandler(group *gin.RouterGroup, templateService *services.TemplateService, authMiddleware *middleware.AuthMiddleware) {
	handler := &TemplateHandler{templateService: templateService}

	apiGroup := group.Group("/templates")

	apiGroup.GET("/fetch", handler.FetchRegistry)

	apiGroup.GET("", authMiddleware.WithAdminNotRequired().WithSuccessOptional().Add(), handler.GetAllTemplatesPaginated)
	apiGroup.GET("/all", authMiddleware.WithAdminNotRequired().WithSuccessOptional().Add(), handler.GetAllTemplates)
	apiGroup.GET("/:id", authMiddleware.WithAdminNotRequired().WithSuccessOptional().Add(), handler.GetTemplate)
	apiGroup.GET("/:id/content", authMiddleware.WithAdminNotRequired().WithSuccessOptional().Add(), handler.GetTemplateContent)

	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.POST("", handler.CreateTemplate)
		apiGroup.PUT("/:id", handler.UpdateTemplate)
		apiGroup.DELETE("/:id", handler.DeleteTemplate)
		apiGroup.POST("/:id/download", handler.DownloadTemplate)
		apiGroup.GET("/default", handler.GetDefaultTemplates)
		apiGroup.POST("/default", handler.SaveDefaultTemplates)
		apiGroup.GET("/registries", handler.GetRegistries)
		apiGroup.POST("/registries", handler.CreateRegistry)
		apiGroup.PUT("/registries/:id", handler.UpdateRegistry)
		apiGroup.DELETE("/registries/:id", handler.DeleteRegistry)
		apiGroup.GET("/variables", handler.GetGlobalVariables)
		apiGroup.PUT("/variables", handler.UpdateGlobalVariables)
	}
}

// GetAllTemplatesPaginated godoc
//
//	@Summary		List templates (paginated)
//	@Description	Get a paginated list of compose templates
//	@Tags			Templates
//	@Param			pagination[page]	query		int		false	"Page number for pagination"	default(1)
//	@Param			pagination[limit]	query		int		false	"Number of items per page"		default(20)
//	@Param			sort[column]		query		string	false	"Column to sort by"
//	@Param			sort[direction]		query		string	false	"Sort direction (asc or desc)"	default("asc")
//	@Success		200					{object}	base.Paginated[template.Template]
//	@Router			/api/templates [get]
func (h *TemplateHandler) GetAllTemplatesPaginated(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	if params.Limit == 0 {
		params.Limit = 20
	}

	templates, paginationResp, err := h.templateService.GetAllTemplatesPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateListError{Err: err}).Error()},
		})
		return
	}

	pagination.ApplyFilterResultsHeaders(&c.Writer, pagination.FilterResult[template.Template]{
		Items:          templates,
		TotalCount:     paginationResp.TotalItems,
		TotalAvailable: paginationResp.GrandTotalItems,
	})

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       templates,
		"pagination": paginationResp,
	})
}

// GetAllTemplates godoc
//
//	@Summary		List all templates
//	@Description	Get all compose templates without pagination
//	@Tags			Templates
//	@Success		200	{array}	template.Template
//	@Router			/api/templates/all [get]
func (h *TemplateHandler) GetAllTemplates(c *gin.Context) {
	templates, err := h.templateService.GetAllTemplates(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateListError{Err: err}).Error()},
		})
		return
	}

	var out []template.Template
	if mapped, mapErr := mapper.MapSlice[models.ComposeTemplate, template.Template](templates); mapErr == nil {
		out = mapped
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// GetTemplate godoc
//
//	@Summary		Get a template
//	@Description	Get a compose template by ID
//	@Tags			Templates
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	base.ApiResponse[template.Template]
//	@Router			/api/templates/{id} [get]
func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateIDRequiredError{}).Error()},
		})
		return
	}

	tmpl, err := h.templateService.GetTemplate(c.Request.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		var msg string
		if err.Error() == "template not found" {
			status = http.StatusNotFound
			msg = (&common.TemplateNotFoundError{}).Error()
		} else {
			msg = (&common.TemplateRetrievalError{Err: err}).Error()
		}
		c.JSON(status, gin.H{
			"success": false,
			"data":    gin.H{"error": msg},
		})
		return
	}

	var out template.Template
	if mapErr := mapper.MapStruct(tmpl, &out); mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// GetTemplateContent godoc
//
//	@Summary		Get template content
//	@Description	Get the compose content for a template with parsed data
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	base.ApiResponse[template.Content]
//	@Failure		400	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/{id}/content [get]
func (h *TemplateHandler) GetTemplateContent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateIDRequiredError{}).Error()},
		})
		return
	}

	contentData, err := h.templateService.GetTemplateContentWithParsedData(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateContentError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    contentData,
	})
}

// CreateTemplate godoc
//
//	@Summary		Create a template
//	@Description	Create a new compose template
//	@Tags			Templates
//	@Accept			json
//	@Produce		json
//	@Param			template	body		object	true	"Template creation data"
//	@Success		201			{object}	base.ApiResponse[template.Template]
//	@Router			/api/templates [post]
func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Content     string `json:"content" binding:"required"`
		EnvContent  string `json:"envContent"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	tmpl := &models.ComposeTemplate{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		IsCustom:    true,
		IsRemote:    false,
	}
	if req.EnvContent != "" {
		tmpl.EnvContent = &req.EnvContent
	}

	if err := h.templateService.CreateTemplate(c.Request.Context(), tmpl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateCreationError{Err: err}).Error()},
		})
		return
	}

	var out template.Template
	if mapErr := mapper.MapStruct(tmpl, &out); mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateMappingError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    out,
	})
}

// UpdateTemplate godoc
//
//	@Summary		Update a template
//	@Description	Update an existing compose template
//	@Tags			Templates
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string	true	"Template ID"
//	@Param			template	body		object	true	"Template update data"
//	@Success		200			{object}	base.ApiResponse[template.Template]
//	@Router			/api/templates/{id} [put]
func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateIDRequiredError{}).Error()},
		})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Content     string `json:"content" binding:"required"`
		EnvContent  string `json:"envContent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	updates := &models.ComposeTemplate{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
	}
	if req.EnvContent != "" {
		updates.EnvContent = &req.EnvContent
	} else {
		updates.EnvContent = nil
	}

	if err := h.templateService.UpdateTemplate(c.Request.Context(), id, updates); err != nil {
		status := http.StatusInternalServerError
		var msg string
		if err.Error() == "template not found" {
			status = http.StatusNotFound
			msg = (&common.TemplateNotFoundError{}).Error()
		} else {
			msg = (&common.TemplateUpdateError{Err: err}).Error()
		}
		c.JSON(status, gin.H{
			"success": false,
			"data":    gin.H{"error": msg},
		})
		return
	}

	updated, err := h.templateService.GetTemplate(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{"message": "Template updated successfully"},
		})
		return
	}

	var out template.Template
	if mapErr := mapper.MapStruct(updated, &out); mapErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{"message": "Template updated successfully"},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// DeleteTemplate godoc
//
//	@Summary		Delete a template
//	@Description	Delete a compose template
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		404	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/{id} [delete]
func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.TemplateIDRequiredError{}).Error()},
		})
		return
	}

	if err := h.templateService.DeleteTemplate(c.Request.Context(), id); err != nil {
		status := http.StatusInternalServerError
		var msg string
		if err.Error() == "template not found" {
			status = http.StatusNotFound
			msg = (&common.TemplateNotFoundError{}).Error()
		} else {
			msg = (&common.TemplateDeletionError{Err: err}).Error()
		}
		c.JSON(status, gin.H{
			"success": false,
			"data":    gin.H{"error": msg},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Template deleted successfully"},
	})
}

// GetDefaultTemplates godoc
//
//	@Summary		Get default templates
//	@Description	Get the default compose and env templates
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Success		200	{object}	base.ApiResponse[template.DefaultTemplates]
//	@Router			/api/templates/default [get]
func (h *TemplateHandler) GetDefaultTemplates(c *gin.Context) {
	composeTemplate := h.templateService.GetComposeTemplate()
	envTemplate := h.templateService.GetEnvTemplate()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"composeTemplate": composeTemplate,
			"envTemplate":     envTemplate,
		},
	})
}

// SaveDefaultTemplates godoc
//
//	@Summary		Save default templates
//	@Description	Save the default compose and env templates
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			templates	body		template.SaveDefault		true	"Default templates data"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/default [post]
func (h *TemplateHandler) SaveDefaultTemplates(c *gin.Context) {
	var req struct {
		ComposeContent string `json:"composeContent" binding:"required"`
		EnvContent     string `json:"envContent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if err := h.templateService.SaveComposeTemplate(req.ComposeContent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.DefaultTemplateSaveError{Err: err}).Error()},
		})
		return
	}

	if err := h.templateService.SaveEnvTemplate(req.EnvContent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.DefaultTemplateSaveError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Default templates saved successfully"},
	})
}

// GetRegistries godoc
//
//	@Summary		List template registries
//	@Description	Get all template registries
//	@Tags			Templates
//	@Success		200	{array}	template.Registry
//	@Router			/api/templates/registries [get]
func (h *TemplateHandler) GetRegistries(c *gin.Context) {
	registries, err := h.templateService.GetRegistries(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryFetchError{Err: err}).Error()},
		})
		return
	}

	out, mapErr := mapper.MapSlice[models.TemplateRegistry, template.Registry](registries)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryFetchError{Err: mapErr}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// CreateRegistry godoc
//
//	@Summary		Create a template registry
//	@Description	Create a new template registry
//	@Tags			Templates
//	@Accept			json
//	@Produce		json
//	@Param			registry	body		object	true	"Registry creation data"
//	@Success		201			{object}	base.ApiResponse[template.Registry]
//	@Router			/api/templates/registries [post]
func (h *TemplateHandler) CreateRegistry(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		URL         string `json:"url" binding:"required"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	registry := &models.TemplateRegistry{
		Name:        req.Name,
		URL:         req.URL,
		Description: req.Description,
		Enabled:     req.Enabled,
	}
	if err := h.templateService.CreateRegistry(c.Request.Context(), registry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryCreationError{Err: err}).Error()},
		})
		return
	}

	var out template.Registry
	if mapErr := mapper.MapStruct(registry, &out); mapErr != nil {
		c.JSON(http.StatusCreated, gin.H{
			"success": true,
			"data":    gin.H{"message": "Registry created"},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    out,
	})
}

// UpdateRegistry godoc
//
//	@Summary		Update a template registry
//	@Description	Update an existing template registry
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string						true	"Registry ID"
//	@Param			registry	body		template.UpdateRegistry		true	"Registry update data"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		404			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/registries/{id} [put]
func (h *TemplateHandler) UpdateRegistry(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryIDRequiredError{}).Error()},
		})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		URL         string `json:"url" binding:"required"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	updates := &models.TemplateRegistry{
		Name:        req.Name,
		URL:         req.URL,
		Description: req.Description,
		Enabled:     req.Enabled,
	}
	if err := h.templateService.UpdateRegistry(c.Request.Context(), id, updates); err != nil {
		status := http.StatusInternalServerError
		var msg string
		if err.Error() == "registry not found" {
			status = http.StatusNotFound
			msg = (&common.RegistryNotFoundError{}).Error()
		} else {
			msg = (&common.RegistryUpdateError{Err: err}).Error()
		}
		c.JSON(status, gin.H{
			"success": false,
			"data":    gin.H{"error": msg},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Registry updated successfully"},
	})
}

// DeleteRegistry godoc
//
//	@Summary		Delete a template registry
//	@Description	Delete a template registry
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"Registry ID"
//	@Success		200	{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		404	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/registries/{id} [delete]
func (h *TemplateHandler) DeleteRegistry(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.RegistryIDRequiredError{}).Error()},
		})
		return
	}

	if err := h.templateService.DeleteRegistry(c.Request.Context(), id); err != nil {
		status := http.StatusInternalServerError
		var msg string
		if err.Error() == "registry not found" {
			status = http.StatusNotFound
			msg = (&common.RegistryNotFoundError{}).Error()
		} else {
			msg = (&common.RegistryDeletionError{Err: err}).Error()
		}
		c.JSON(status, gin.H{
			"success": false,
			"data":    gin.H{"error": msg},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Registry deleted successfully"},
	})
}

// FetchRegistry godoc
//
//	@Summary		Fetch remote registry
//	@Description	Fetch templates from a remote registry URL
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			url	query		string	true	"Registry URL"
//	@Success		200	{object}	map[string]any
//	@Failure		400	{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		502	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/fetch [get]
func (h *TemplateHandler) FetchRegistry(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.QueryParameterRequiredError{}).Error()},
		})
		return
	}

	body, err := h.templateService.FetchRaw(c.Request.Context(), url)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "data": gin.H{"error": (&common.RegistryFetchError{Err: err}).Error()}})
		return
	}

	var registry interface{}
	if err := json.Unmarshal(body, &registry); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "data": gin.H{"error": (&common.InvalidJSONResponseError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    registry,
	})
}

// DownloadTemplate godoc
//
//	@Summary		Download a template
//	@Description	Download a remote template to local storage
//	@Tags			Templates
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	base.ApiResponse[template.Template]
//	@Router			/api/templates/{id}/download [post]
func (h *TemplateHandler) DownloadTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.TemplateIDRequiredError{}).Error()}})
		return
	}

	tmpl, err := h.templateService.GetTemplate(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.TemplateNotFoundError{}).Error()}})
		return
	}
	if !tmpl.IsRemote {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.TemplateAlreadyLocalError{}).Error()}})
		return
	}

	localTemplate, err := h.templateService.DownloadTemplate(c.Request.Context(), tmpl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.TemplateDownloadError{Err: err}).Error()}})
		return
	}

	var out template.Template
	if mapErr := mapper.MapStruct(localTemplate, &out); mapErr != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "Template downloaded successfully"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// GetGlobalVariables godoc
//
//	@Summary		Get global variables
//	@Description	Get global template variables
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Success		200	{object}	base.ApiResponse[[]env.Variable]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/variables [get]
func (h *TemplateHandler) GetGlobalVariables(c *gin.Context) {
	vars, err := h.templateService.GetGlobalVariables(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.GlobalVariablesRetrievalError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    vars,
	})
}

// UpdateGlobalVariables godoc
//
//	@Summary		Update global variables
//	@Description	Update global template variables
//	@Tags			Templates
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		json
//	@Param			variables	body		env.Summary		true	"Variables update data"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/templates/variables [put]
func (h *TemplateHandler) UpdateGlobalVariables(c *gin.Context) {
	var req env.Summary
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if err := h.templateService.UpdateGlobalVariables(c.Request.Context(), req.Variables); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.GlobalVariablesUpdateError{Err: err}).Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"message": "Global variables updated successfully",
		},
	})
}
