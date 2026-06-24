package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/webhook"
)

type webhookHandler struct {
	webhookService *services.WebhookService
}

// --- Input/Output types ---

type listWebhooksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type listWebhooksOutput struct {
	Body base.ApiResponse[[]webhook.Summary]
}

type createWebhookInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	Body          *webhook.CreateInput `required:"true"`
}

type createWebhookOutput struct {
	Body base.ApiResponse[webhook.Created]
}

type deleteWebhookInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	WebhookID     string `path:"webhookId" doc:"Webhook ID"`
}

type deleteWebhookOutput struct {
	Body base.ApiResponse[any]
}

type updateWebhookInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	WebhookID     string               `path:"webhookId" doc:"Webhook ID"`
	Body          *webhook.UpdateInput `required:"true"`
}

type updateWebhookOutput struct {
	Body base.ApiResponse[any]
}

// RegisterWebhooks registers the authenticated CRUD routes for webhook management.
func RegisterWebhooks(api huma.API, webhookService *services.WebhookService) {
	h := &webhookHandler{webhookService: webhookService}

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermWebhooksList, h.listWebhooksInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermWebhooksCreate, h.createWebhookInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermWebhooksUpdate, h.updateWebhookInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermWebhooksDelete, h.deleteWebhookInternal)
}

// ListWebhooks returns all webhooks for an environment (tokens are masked).
func (h *webhookHandler) listWebhooksInternal(ctx context.Context, input *listWebhooksInput) (*listWebhooksOutput, error) {
	if h.webhookService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	webhooks, err := h.webhookService.ListWebhookSummaries(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list webhooks")
	}

	return &listWebhooksOutput{
		Body: base.ApiResponse[[]webhook.Summary]{
			Success: true,
			Data:    webhooks,
		},
	}, nil
}

// CreateWebhook creates a new webhook and returns the raw token (shown once only).
func (h *webhookHandler) createWebhookInternal(ctx context.Context, input *createWebhookInput) (*createWebhookOutput, error) {
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
		input.Body.ActionType,
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
		if errors.Is(err, services.ErrWebhookInvalidAction) {
			return nil, huma.Error400BadRequest("invalid action type for target type")
		}
		return nil, huma.Error500InternalServerError("failed to create webhook")
	}

	return &createWebhookOutput{
		Body: base.ApiResponse[webhook.Created]{
			Success: true,
			Data: webhook.Created{
				ID:         wh.ID,
				Name:       wh.Name,
				Token:      rawToken,
				TargetType: wh.TargetType,
				ActionType: wh.ActionType,
				TargetID:   wh.TargetID,
				CreatedAt:  wh.CreatedAt,
			},
		},
	}, nil
}

// UpdateWebhook updates a webhook's enabled state.
func (h *webhookHandler) updateWebhookInternal(ctx context.Context, input *updateWebhookInput) (*updateWebhookOutput, error) {
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
	return &updateWebhookOutput{
		Body: base.ApiResponse[any]{Success: true},
	}, nil
}

// DeleteWebhook removes a webhook.
func (h *webhookHandler) deleteWebhookInternal(ctx context.Context, input *deleteWebhookInput) (*deleteWebhookOutput, error) {
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

	return &deleteWebhookOutput{
		Body: base.ApiResponse[any]{
			Success: true,
		},
	}, nil
}
