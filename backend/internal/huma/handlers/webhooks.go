package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/webhook"
	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	webhookService *services.WebhookService
}

// --- Input/Output types ---

type ListWebhooksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ListWebhooksOutput struct {
	Body base.ApiResponse[[]webhook.Summary]
}

type CreateWebhookInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	Body          *webhook.CreateInput `required:"true"`
}

type CreateWebhookOutput struct {
	Body base.ApiResponse[webhook.Created]
}

type DeleteWebhookInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	WebhookID     string `path:"webhookId" doc:"Webhook ID"`
}

type DeleteWebhookOutput struct {
	Body base.ApiResponse[any]
}

type UpdateWebhookInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	WebhookID     string               `path:"webhookId" doc:"Webhook ID"`
	Body          *webhook.UpdateInput `required:"true"`
}

type UpdateWebhookOutput struct {
	Body base.ApiResponse[any]
}

// RegisterWebhooks registers the authenticated CRUD routes for webhook management.
func RegisterWebhooks(api huma.API, webhookService *services.WebhookService) {
	h := &WebhookHandler{webhookService: webhookService}

	huma.Register(api, huma.Operation{
		OperationID: "list-webhooks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/webhooks",
		Summary:     "List webhooks",
		Description: "List all webhooks configured for this environment",
		Tags:        []string{"Webhooks"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListWebhooks)

	huma.Register(api, huma.Operation{
		OperationID: "create-webhook",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/webhooks",
		Summary:     "Create webhook",
		Description: "Create a webhook that triggers a container or stack update. The token is only returned once.",
		Tags:        []string{"Webhooks"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateWebhook)

	huma.Register(api, huma.Operation{
		OperationID: "update-webhook",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/webhooks/{webhookId}",
		Summary:     "Update webhook",
		Description: "Update a webhook's enabled state",
		Tags:        []string{"Webhooks"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateWebhook)

	huma.Register(api, huma.Operation{
		OperationID: "delete-webhook",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/webhooks/{webhookId}",
		Summary:     "Delete webhook",
		Description: "Delete a webhook by ID",
		Tags:        []string{"Webhooks"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteWebhook)
}

// RegisterWebhookTrigger registers the public (unauthenticated) trigger endpoint on the
// raw Gin router, bypassing the Huma auth middleware. The token in the URL is the sole
// authentication mechanism.
//
// Security note: tokens appear in server access logs and browser history. Ensure access
// logs are appropriately protected, and consider rate-limiting this endpoint at the
// reverse-proxy level.
func RegisterWebhookTrigger(router *gin.RouterGroup, webhookService *services.WebhookService) {
	router.POST("/webhooks/trigger/:token", func(c *gin.Context) {
		if webhookService == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "service not available"})
			return
		}

		token := c.Param("token")
		result, err := webhookService.TriggerByToken(c.Request.Context(), token)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, services.ErrWebhookNotFound) || errors.Is(err, services.ErrWebhookInvalid) {
				status = http.StatusNotFound
			} else if errors.Is(err, services.ErrWebhookDisabled) {
				status = http.StatusForbidden
			}
			msg := err.Error()
			if status == http.StatusInternalServerError {
				msg = "internal server error"
			}
			c.JSON(status, gin.H{"success": false, "error": msg})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
	})
}

// ListWebhooks returns all webhooks for an environment (tokens are masked).
func (h *WebhookHandler) ListWebhooks(ctx context.Context, input *ListWebhooksInput) (*ListWebhooksOutput, error) {
	if h.webhookService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	webhooks, err := h.webhookService.ListWebhooks(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list webhooks")
	}

	summaries := make([]webhook.Summary, len(webhooks))
	for i, wh := range webhooks {
		summaries[i] = webhook.Summary{
			ID:              wh.ID,
			Name:            wh.Name,
			TokenPrefix:     wh.TokenPrefix,
			TargetType:      wh.TargetType,
			TargetID:        wh.TargetID,
			EnvironmentID:   wh.EnvironmentID,
			Enabled:         wh.Enabled,
			LastTriggeredAt: wh.LastTriggeredAt,
			CreatedAt:       wh.CreatedAt,
		}
	}

	return &ListWebhooksOutput{
		Body: base.ApiResponse[[]webhook.Summary]{
			Success: true,
			Data:    summaries,
		},
	}, nil
}

// CreateWebhook creates a new webhook and returns the raw token (shown once only).
func (h *WebhookHandler) CreateWebhook(ctx context.Context, input *CreateWebhookInput) (*CreateWebhookOutput, error) {
	if h.webhookService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.Body == nil {
		return nil, huma.Error400BadRequest("request body is required")
	}

	actor := models.User{}
	if currentUser, exists := humamw.GetCurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	wh, rawToken, err := h.webhookService.CreateWebhook(
		ctx,
		input.Body.Name,
		input.Body.TargetType,
		input.Body.TargetID,
		input.EnvironmentID,
		actor,
	)
	if err != nil {
		if errors.Is(err, services.ErrWebhookInvalidType) {
			return nil, huma.Error400BadRequest("invalid target type, must be 'container', 'project', 'updater', or 'gitops'")
		}
		if errors.Is(err, services.ErrWebhookMissingTarget) {
			return nil, huma.Error400BadRequest("target ID is required for container, project, and gitops webhook types")
		}
		return nil, huma.Error500InternalServerError("failed to create webhook")
	}

	return &CreateWebhookOutput{
		Body: base.ApiResponse[webhook.Created]{
			Success: true,
			Data: webhook.Created{
				ID:         wh.ID,
				Name:       wh.Name,
				Token:      rawToken,
				TargetType: wh.TargetType,
				TargetID:   wh.TargetID,
				CreatedAt:  wh.CreatedAt,
			},
		},
	}, nil
}

// UpdateWebhook updates a webhook's enabled state.
func (h *WebhookHandler) UpdateWebhook(ctx context.Context, input *UpdateWebhookInput) (*UpdateWebhookOutput, error) {
	if h.webhookService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.Body == nil {
		return nil, huma.Error400BadRequest("request body is required")
	}

	actor := models.User{}
	if currentUser, exists := humamw.GetCurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	wh, err := h.webhookService.UpdateWebhook(ctx, input.WebhookID, input.EnvironmentID, input.Body.Enabled, actor)
	if err != nil {
		if errors.Is(err, services.ErrWebhookNotFound) {
			return nil, huma.Error404NotFound("webhook not found")
		}
		return nil, huma.Error500InternalServerError("failed to update webhook")
	}

	_ = wh // updated record available if needed in future
	return &UpdateWebhookOutput{
		Body: base.ApiResponse[any]{Success: true},
	}, nil
}

// DeleteWebhook removes a webhook.
func (h *WebhookHandler) DeleteWebhook(ctx context.Context, input *DeleteWebhookInput) (*DeleteWebhookOutput, error) {
	if h.webhookService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	actor := models.User{}
	if currentUser, exists := humamw.GetCurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	if err := h.webhookService.DeleteWebhook(ctx, input.WebhookID, input.EnvironmentID, actor); err != nil {
		if errors.Is(err, services.ErrWebhookNotFound) {
			return nil, huma.Error404NotFound("webhook not found")
		}
		return nil, huma.Error500InternalServerError("failed to delete webhook")
	}

	return &DeleteWebhookOutput{
		Body: base.ApiResponse[any]{
			Success: true,
		},
	}, nil
}
