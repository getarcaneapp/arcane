package webhook

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"net/http"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/webhook"
)

type WebhookHandler struct {
	webhookService *WebhookService
}

// --- Input/Output types ---

type ListWebhooksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type CreateWebhookInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	Body          *webhook.CreateInput `required:"true"`
}

type DeleteWebhookInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	WebhookID     string `path:"webhookId" doc:"Webhook ID"`
}

type UpdateWebhookInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	WebhookID     string               `path:"webhookId" doc:"Webhook ID"`
	Body          *webhook.UpdateInput `required:"true"`
}

// RegisterWebhooks registers the authenticated CRUD routes for webhook management.
func RegisterWebhooks(api huma.API, webhookService *WebhookService) {
	h := &WebhookHandler{webhookService: webhookService}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-webhooks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/webhooks",
		Summary:     "List webhooks",
		Description: "List all webhooks configured for this environment",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksList, h.ListWebhooks)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-webhook",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/webhooks",
		Summary:     "Create webhook",
		Description: "Create a webhook that triggers a container or stack update. The token is only returned once.",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksCreate, h.CreateWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-webhook",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/webhooks/{webhookId}",
		Summary:     "Update webhook",
		Description: "Update a webhook's enabled state",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksUpdate, h.UpdateWebhook)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-webhook",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/webhooks/{webhookId}",
		Summary:     "Delete webhook",
		Description: "Delete a webhook by ID",
		Tags:        []string{"Webhooks"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermWebhooksDelete, h.DeleteWebhook)
}

// ListWebhooks returns all webhooks for an environment (tokens are masked).
func (h *WebhookHandler) ListWebhooks(ctx context.Context, input *ListWebhooksInput) (*handlerutil.Out[[]webhook.Summary], error) {
	webhooks, err := h.webhookService.ListWebhookSummaries(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list webhooks")
	}

	return &handlerutil.Out[[]webhook.Summary]{
		Body: base.ApiResponse[[]webhook.Summary]{
			Success: true,
			Data:    webhooks,
		},
	}, nil
}

// CreateWebhook creates a new webhook and returns the raw token (shown once only).
func (h *WebhookHandler) CreateWebhook(ctx context.Context, input *CreateWebhookInput) (*handlerutil.Out[webhook.Created], error) {
	if input.Body == nil {
		return nil, huma.Error400BadRequest("request body is required")
	}

	actor := common.User{}
	if currentUser, exists := common.CurrentUserFromContext(ctx); exists && currentUser != nil {
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
		if errors.Is(err, ErrWebhookInvalidType) {
			return nil, huma.Error400BadRequest("invalid target type, must be 'container', 'project', 'updater', or 'gitops'")
		}
		if errors.Is(err, ErrWebhookMissingTarget) {
			return nil, huma.Error400BadRequest("target ID is required for container, project, and gitops webhook types")
		}
		if errors.Is(err, ErrWebhookInvalidAction) {
			return nil, huma.Error400BadRequest("invalid action type for target type")
		}
		return nil, huma.Error500InternalServerError("failed to create webhook")
	}

	return &handlerutil.Out[webhook.Created]{
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
func (h *WebhookHandler) UpdateWebhook(ctx context.Context, input *UpdateWebhookInput) (*handlerutil.Out[any], error) {
	if input.Body == nil {
		return nil, huma.Error400BadRequest("request body is required")
	}

	actor := common.User{}
	if currentUser, exists := common.CurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	wh, err := h.webhookService.UpdateWebhook(ctx, input.WebhookID, input.EnvironmentID, input.Body.Enabled, actor)
	if err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			return nil, huma.Error404NotFound("webhook not found")
		}
		return nil, huma.Error500InternalServerError("failed to update webhook")
	}

	_ = wh // updated record available if needed in future
	return &handlerutil.Out[any]{
		Body: base.ApiResponse[any]{Success: true},
	}, nil
}

// DeleteWebhook removes a webhook.
func (h *WebhookHandler) DeleteWebhook(ctx context.Context, input *DeleteWebhookInput) (*handlerutil.Out[any], error) {
	actor := common.User{}
	if currentUser, exists := common.CurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	if err := h.webhookService.DeleteWebhook(ctx, input.WebhookID, input.EnvironmentID, actor); err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			return nil, huma.Error404NotFound("webhook not found")
		}
		return nil, huma.Error500InternalServerError("failed to delete webhook")
	}

	return &handlerutil.Out[any]{
		Body: base.ApiResponse[any]{
			Success: true,
		},
	}, nil
}
