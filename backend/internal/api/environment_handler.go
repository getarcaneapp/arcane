package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/dto"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/gin-gonic/gin"
)

const LOCAL_DOCKER_ENVIRONMENT_ID = "0"

type EnvironmentHandler struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	cfg                *config.Config
	httpClient         *http.Client
}

func NewEnvironmentHandler(
	group *gin.RouterGroup,
	environmentService *services.EnvironmentService,
	settingsService *services.SettingsService,
	authMiddleware *middleware.AuthMiddleware,
	cfg *config.Config,
) {
	h := &EnvironmentHandler{
		environmentService: environmentService,
		settingsService:    settingsService,
		cfg:                cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	apiGroup := group.Group("/environments")
	apiGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		apiGroup.GET("", h.ListEnvironments)
		apiGroup.POST("", h.CreateEnvironment)
		apiGroup.GET("/tags", h.GetAllTags)
		apiGroup.GET("/filters", h.ListFilters)
		apiGroup.POST("/filters", h.CreateFilter)
		apiGroup.GET("/filters/default", h.GetDefaultFilter)
		apiGroup.DELETE("/filters/default", h.ClearFilterDefault)
		apiGroup.GET("/filters/:filterId", h.GetFilter)
		apiGroup.PUT("/filters/:filterId", h.UpdateFilter)
		apiGroup.DELETE("/filters/:filterId", h.DeleteFilter)
		apiGroup.POST("/filters/:filterId/default", h.SetFilterDefault)
		apiGroup.GET("/:id", h.GetEnvironment)
		apiGroup.PUT("/:id", h.UpdateEnvironment)
		apiGroup.DELETE("/:id", h.DeleteEnvironment)
		apiGroup.POST("/:id/test", h.TestConnection)
		apiGroup.POST("/:id/heartbeat", h.UpdateHeartbeat)
		apiGroup.POST("/:id/agent/pair", h.PairAgent)
		apiGroup.POST("/:id/sync-registries", h.SyncRegistries)
	}
}

func (h *EnvironmentHandler) PairAgent(c *gin.Context) {
	if c.Param("id") != LOCAL_DOCKER_ENVIRONMENT_ID {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": "Not found"}})
		return
	}
	type pairReq struct {
		Rotate *bool `json:"rotate,omitempty"`
	}
	var req pairReq
	_ = c.ShouldBindJSON(&req)

	if h.cfg.AgentToken == "" || (req.Rotate != nil && *req.Rotate) {
		h.cfg.AgentToken = utils.GenerateRandomString(48)
	}

	// Persist token on the agent so it survives restarts
	if err := h.settingsService.SetStringSetting(c.Request.Context(), "agentToken", h.cfg.AgentToken); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.AgentTokenPersistenceError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"token": h.cfg.AgentToken,
		},
	})
}

// Create
func (h *EnvironmentHandler) CreateEnvironment(c *gin.Context) {
	var req dto.CreateEnvironmentDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	env := &models.Environment{
		ApiUrl:  req.ApiUrl,
		Enabled: true,
	}
	if req.Name != nil {
		env.Name = *req.Name
	}
	if req.Enabled != nil {
		env.Enabled = *req.Enabled
	}
	if req.Tags != nil {
		env.Tags = req.Tags
	}

	if (req.AccessToken == nil || *req.AccessToken == "") && req.BootstrapToken != nil && *req.BootstrapToken != "" {
		token, err := h.environmentService.PairAgentWithBootstrap(c.Request.Context(), req.ApiUrl, *req.BootstrapToken)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "Failed to pair with agent",
				"apiUrl", req.ApiUrl,
				"error", err.Error())

			c.JSON(http.StatusBadGateway, gin.H{
				"success": false,
				"data": gin.H{
					"error": (&common.AgentPairingError{Err: err}).Error(),
					"hint":  "Ensure the agent is running and the bootstrap token matches AGENT_BOOTSTRAP_TOKEN",
				},
			})
			return
		}
		env.AccessToken = &token
	} else if req.AccessToken != nil && *req.AccessToken != "" {
		env.AccessToken = req.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(c.Request.Context(), env)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentCreationError{Err: err}).Error()}})
		return
	}

	// Sync registries to the new environment in the background
	if created.AccessToken != nil && *created.AccessToken != "" {
		go func() {
			ctx := context.Background()
			if err := h.environmentService.SyncRegistriesToEnvironment(ctx, created.ID); err != nil {
				slog.WarnContext(ctx, "Failed to sync registries to new environment",
					"environmentID", created.ID,
					"environmentName", created.Name,
					"error", err.Error())
			} else {
				slog.InfoContext(ctx, "Successfully synced registries to new environment",
					"environmentID", created.ID,
					"environmentName", created.Name)
			}
		}()
	}

	out, mapErr := dto.MapOne[*models.Environment, dto.EnvironmentDto](created)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentMappingError{Err: mapErr}).Error()}})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"success": true, "data": out})
}

func (h *EnvironmentHandler) ListEnvironments(c *gin.Context) {
	params := pagination.ExtractListModifiersQueryParams(c)

	envs, paginationResp, err := h.environmentService.ListEnvironmentsPaginated(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentListError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"data":       envs,
		"pagination": paginationResp,
	})
}

// GetAllTags returns all unique tags used across environments
func (h *EnvironmentHandler) GetAllTags(c *gin.Context) {
	tags, err := h.environmentService.GetAllTags(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": err.Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tags,
	})
}

// Get by ID
func (h *EnvironmentHandler) GetEnvironment(c *gin.Context) {
	environmentID := c.Param("id")

	environment, err := h.environmentService.GetEnvironmentByID(c.Request.Context(), environmentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentNotFoundError{}).Error()}})
		return
	}

	out, mapErr := dto.MapOne[*models.Environment, dto.EnvironmentDto](environment)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentMappingError{Err: mapErr}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    out,
	})
}

// Update
func (h *EnvironmentHandler) UpdateEnvironment(c *gin.Context) {
	environmentID := c.Param("id")
	isLocalEnv := environmentID == LOCAL_DOCKER_ENVIRONMENT_ID

	var req dto.UpdateEnvironmentDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	updates := h.buildUpdateMapInternal(&req, isLocalEnv)

	pairingSucceeded, err := h.handleEnvironmentPairingInternal(c.Request.Context(), environmentID, &req, updates, isLocalEnv)
	if err != nil {
		c.JSON(err.statusCode, gin.H{"success": false, "data": gin.H{"error": err.message}})
		return
	}

	updated, updateErr := h.environmentService.UpdateEnvironment(c.Request.Context(), environmentID, updates)
	if updateErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentUpdateError{Err: updateErr}).Error()}})
		return
	}

	h.triggerPostUpdateTasksInternal(environmentID, updated, pairingSucceeded, &req)

	out, mapErr := dto.MapOne[*models.Environment, dto.EnvironmentDto](updated)
	if mapErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentMappingError{Err: mapErr}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

func (h *EnvironmentHandler) buildUpdateMapInternal(req *dto.UpdateEnvironmentDto, isLocalEnv bool) map[string]any {
	updates := map[string]any{}

	// For local environment, only allow name and tags
	if !isLocalEnv {
		if req.ApiUrl != nil {
			updates["api_url"] = *req.ApiUrl
		}
		if req.Enabled != nil {
			updates["enabled"] = *req.Enabled
		}
	}

	if req.Name != nil {
		updates["name"] = *req.Name
	}

	if req.Tags != nil {
		updates["tags"] = req.Tags
	}

	return updates
}

type updateError struct {
	statusCode int
	message    string
}

func (h *EnvironmentHandler) handleEnvironmentPairingInternal(ctx context.Context, environmentID string, req *dto.UpdateEnvironmentDto, updates map[string]any, isLocalEnv bool) (bool, *updateError) {
	pairingSucceeded := false

	// Local environment cannot be paired or have access token updated
	if isLocalEnv {
		return pairingSucceeded, nil
	}

	// If caller asked to pair (bootstrapToken present) and no accessToken provided in the request,
	// resolve apiUrl (current or updated) and let the service pair and persist the token.
	if (req.AccessToken == nil) && req.BootstrapToken != nil && *req.BootstrapToken != "" {
		current, err := h.environmentService.GetEnvironmentByID(ctx, environmentID)
		if err != nil || current == nil {
			return false, &updateError{
				statusCode: http.StatusNotFound,
				message:    "Environment not found",
			}
		}

		apiUrl := current.ApiUrl
		if req.ApiUrl != nil && *req.ApiUrl != "" {
			apiUrl = *req.ApiUrl
		}

		if _, err := h.environmentService.PairAndPersistAgentToken(ctx, environmentID, apiUrl, *req.BootstrapToken); err != nil {
			return false, &updateError{
				statusCode: http.StatusBadGateway,
				message:    "Agent pairing failed: " + err.Error(),
			}
		}
		pairingSucceeded = true
	} else if req.AccessToken != nil {
		updates["access_token"] = *req.AccessToken
	}

	return pairingSucceeded, nil
}

func (h *EnvironmentHandler) triggerPostUpdateTasksInternal(environmentID string, updated *models.Environment, pairingSucceeded bool, req *dto.UpdateEnvironmentDto) {
	// Trigger health check after update to verify new configuration
	// This runs in background and doesn't block the response
	if updated.Enabled {
		go func() {
			ctx := context.Background()
			status, err := h.environmentService.TestConnection(ctx, environmentID, nil)
			if err != nil {
				slog.WarnContext(ctx, "Failed to test connection after environment update", "environment_id", environmentID, "environment_name", updated.Name, "status", status, "error", err)
			} else {
				slog.InfoContext(ctx, "Environment health check completed after update", "environment_id", environmentID, "environment_name", updated.Name, "status", status)
			}
		}()
	}

	// Sync registries if pairing succeeded or if access token was updated
	if pairingSucceeded || (req.AccessToken != nil && *req.AccessToken != "") {
		go func() {
			ctx := context.Background()
			if err := h.environmentService.SyncRegistriesToEnvironment(ctx, environmentID); err != nil {
				slog.WarnContext(ctx, "Failed to sync registries after environment update",
					"environmentID", environmentID,
					"environmentName", updated.Name,
					"error", err.Error())
			} else {
				slog.InfoContext(ctx, "Successfully synced registries after environment update",
					"environmentID", environmentID,
					"environmentName", updated.Name)
			}
		}()
	}
}

// Delete
func (h *EnvironmentHandler) DeleteEnvironment(c *gin.Context) {
	environmentID := c.Param("id")

	// Prevent deletion of local environment
	if environmentID == LOCAL_DOCKER_ENVIRONMENT_ID {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.LocalEnvironmentDeletionError{}).Error()}})
		return
	}

	err := h.environmentService.DeleteEnvironment(c.Request.Context(), environmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.EnvironmentDeletionError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"message": "Environment deleted successfully"},
	})
}

// TestConnection
func (h *EnvironmentHandler) TestConnection(c *gin.Context) {
	environmentID := c.Param("id")

	// Allow optional apiUrl in request body to test without saving
	var req struct {
		ApiUrl *string `json:"apiUrl"`
	}
	_ = c.ShouldBindJSON(&req)

	status, err := h.environmentService.TestConnection(c.Request.Context(), environmentID, req.ApiUrl)
	resp := dto.TestConnectionDto{Status: status}
	if err != nil {
		msg := err.Error()
		resp.Message = &msg
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"data":    resp,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

func (h *EnvironmentHandler) UpdateHeartbeat(c *gin.Context) {
	environmentID := c.Param("id")

	err := h.environmentService.UpdateEnvironmentHeartbeat(c.Request.Context(), environmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   (&common.HeartbeatUpdateError{Err: err}).Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Heartbeat updated successfully",
	})
}

func (h *EnvironmentHandler) SyncRegistries(c *gin.Context) {
	environmentID := c.Param("id")

	err := h.environmentService.SyncRegistriesToEnvironment(c.Request.Context(), environmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
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

func (h *EnvironmentHandler) ListFilters(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	filters, err := h.environmentService.ListFilters(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterListError{Err: err}).Error()}})
		return
	}

	out := make([]dto.EnvironmentFilterDto, len(filters))
	for i, f := range filters {
		out[i] = toFilterDto(&f)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

func (h *EnvironmentHandler) GetFilter(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	filterID := c.Param("filterId")
	filter, err := h.environmentService.GetFilter(c.Request.Context(), filterID, userID)
	if err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.FilterNotFoundError{}).Error()}})
			return
		}
		if errors.Is(err, services.ErrFilterForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "data": gin.H{"error": (&common.FilterForbiddenError{}).Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": err.Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": toFilterDto(filter)})
}

func (h *EnvironmentHandler) GetDefaultFilter(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	filter, err := h.environmentService.GetDefaultFilter(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterListError{Err: err}).Error()}})
		return
	}

	if filter == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": toFilterDto(filter)})
}

func (h *EnvironmentHandler) CreateFilter(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	var req dto.CreateEnvironmentFilterDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()}})
		return
	}

	filter := &models.EnvironmentFilter{
		UserID:       userID,
		Name:         req.Name,
		IsDefault:    req.IsDefault,
		SelectedTags: req.SelectedTags,
		ExcludedTags: req.ExcludedTags,
		TagMode:      models.EnvironmentFilterTagMode(defaultString(req.TagMode, string(models.TagModeAny))),
		StatusFilter: models.EnvironmentFilterStatusFilter(defaultString(req.StatusFilter, string(models.StatusFilterAll))),
		GroupBy:      models.EnvironmentFilterGroupBy(defaultString(req.GroupBy, string(models.GroupByNone))),
	}

	created, err := h.environmentService.CreateFilter(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterCreationError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"success": true, "data": toFilterDto(created)})
}

func (h *EnvironmentHandler) UpdateFilter(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	filterID := c.Param("filterId")

	var req dto.UpdateEnvironmentFilterDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()}})
		return
	}

	updates := buildFilterUpdates(&req)
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "data": gin.H{"error": (&common.InvalidRequestFormatError{}).Error()}})
		return
	}

	updated, err := h.environmentService.UpdateFilter(c.Request.Context(), filterID, userID, updates)
	if err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.FilterNotFoundError{}).Error()}})
			return
		}
		if errors.Is(err, services.ErrFilterForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "data": gin.H{"error": (&common.FilterForbiddenError{}).Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterUpdateError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": toFilterDto(updated)})
}

func (h *EnvironmentHandler) DeleteFilter(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	filterID := c.Param("filterId")
	if err := h.environmentService.DeleteFilter(c.Request.Context(), filterID, userID); err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.FilterNotFoundError{}).Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterDeleteError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
}

func (h *EnvironmentHandler) SetFilterDefault(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	filterID := c.Param("filterId")
	if err := h.environmentService.SetFilterDefault(c.Request.Context(), filterID, userID); err != nil {
		if errors.Is(err, services.ErrFilterNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "data": gin.H{"error": (&common.FilterNotFoundError{}).Error()}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterUpdateError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
}

func (h *EnvironmentHandler) ClearFilterDefault(c *gin.Context) {
	userID, ok := middleware.GetCurrentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "data": gin.H{"error": (&common.NotAuthenticatedError{}).Error()}})
		return
	}

	if err := h.environmentService.ClearFilterDefault(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "data": gin.H{"error": (&common.FilterUpdateError{Err: err}).Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
}

func toFilterDto(f *models.EnvironmentFilter) dto.EnvironmentFilterDto {
	var d dto.EnvironmentFilterDto
	_ = dto.MapStruct(f, &d)

	// Format timestamps as RFC3339
	d.CreatedAt = f.CreatedAt.Format(time.RFC3339)
	if f.UpdatedAt != nil {
		d.UpdatedAt = f.UpdatedAt.Format(time.RFC3339)
	}

	// Ensure slices are not nil for JSON serialization
	if d.SelectedTags == nil {
		d.SelectedTags = []string{}
	}
	if d.ExcludedTags == nil {
		d.ExcludedTags = []string{}
	}
	return d
}

func buildFilterUpdates(req *dto.UpdateEnvironmentFilterDto) map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.IsDefault != nil {
		updates["is_default"] = *req.IsDefault
	}
	if req.SelectedTags != nil {
		updates["selected_tags"] = models.StringSlice(req.SelectedTags)
	}
	if req.ExcludedTags != nil {
		updates["excluded_tags"] = models.StringSlice(req.ExcludedTags)
	}
	if req.TagMode != nil {
		updates["tag_mode"] = models.EnvironmentFilterTagMode(*req.TagMode)
	}
	if req.StatusFilter != nil {
		updates["status_filter"] = models.EnvironmentFilterStatusFilter(*req.StatusFilter)
	}
	if req.GroupBy != nil {
		updates["group_by"] = models.EnvironmentFilterGroupBy(*req.GroupBy)
	}
	return updates
}

func defaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}
