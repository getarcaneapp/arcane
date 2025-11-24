package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/config"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/services"
	"github.com/ofkm/arcane-backend/internal/utils"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
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

	// For local environment, only allow name
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
