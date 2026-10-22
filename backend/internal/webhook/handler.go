package webhook

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
)

type WebhookHandler struct {
	webhookService *WebhookService
}

type ListWebhooksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type CreateWebhookInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	types.Create[types.Webhook]
}

type DeleteWebhookInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	WebhookID     string `path:"webhookId" doc:"Webhook ID"`
}

type UpdateWebhookInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	WebhookID     string `path:"webhookId" doc:"Webhook ID"`
	types.Update[types.Webhook]
}

// ListWebhooks returns all webhooks for an environment (tokens are masked).
func (h *WebhookHandler) ListWebhooks(ctx context.Context, input *ListWebhooksInput) (*handlerutil.Out[[]types.Webhook], error) {
	webhooks, err := h.webhookService.ListWebhookSummaries(ctx, input.EnvironmentID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list webhooks")
	}

	return &handlerutil.Out[[]types.Webhook]{
		Body: types.ApiResponse[[]types.Webhook]{
			Success: true,
			Data:    webhooks,
		},
	}, nil
}

// CreateWebhook creates a new webhook and returns the raw token (shown once only).
func (h *WebhookHandler) CreateWebhook(ctx context.Context, input *CreateWebhookInput) (*handlerutil.Out[types.WebhookCreated], error) {
	actor := types.Actor{}
	if currentUser, exists := userctx.CurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	callerPerms, _ := middleware.PermissionsFromContext(ctx)
	wh, rawToken, err := h.webhookService.CreateWebhook(
		ctx,
		input.Body.Name,
		input.Body.TargetType,
		input.Body.ActionType,
		input.Body.TargetID,
		input.EnvironmentID,
		actor,
		callerPerms,
	)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[types.WebhookCreated]{
		Body: types.ApiResponse[types.WebhookCreated]{
			Success: true,
			Data: types.WebhookCreated{
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
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid webhook update body")
	}

	actor := types.Actor{}
	if currentUser, exists := userctx.CurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	callerPerms, _ := middleware.PermissionsFromContext(ctx)
	wh, err := h.webhookService.UpdateWebhook(ctx, input.WebhookID, input.EnvironmentID, input.Body, submitted, actor, callerPerms)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	_ = wh // updated record available if needed in future
	return &handlerutil.Out[any]{
		Body: types.ApiResponse[any]{Success: true},
	}, nil
}

// DeleteWebhook removes a webhook.
func (h *WebhookHandler) DeleteWebhook(ctx context.Context, input *DeleteWebhookInput) (*handlerutil.Out[any], error) {
	actor := types.Actor{}
	if currentUser, exists := userctx.CurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}

	if err := h.webhookService.DeleteWebhook(ctx, input.WebhookID, input.EnvironmentID, actor); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return &handlerutil.Out[any]{
		Body: types.ApiResponse[any]{
			Success: true,
		},
	}, nil
}
