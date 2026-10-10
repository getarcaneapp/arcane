package secrets

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/swarm"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the swarm secret endpoints.
type Handler struct {
	service  *Service
	audit    func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any)
	mapError func(err error, fallback string) error
}

func NewHandler(
	service *Service,
	audit func(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any),
	mapError func(err error, fallback string) error,
) *Handler {
	return &Handler{service: service, audit: audit, mapError: mapError}
}

type ListSwarmSecretsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type CreateSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.SecretCreateRequest
}

type DeleteSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

// ListSecrets lists swarm secrets in the current environment.
//
// It delegates to the swarm service and normalizes nil secret slices to empty
// arrays in the response body.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose secrets should be listed.
//
// Returns the current swarm secrets.
// Returns a mapped HTTP error when secret enumeration fails.
func (h *Handler) ListSecrets(ctx context.Context, input *ListSwarmSecretsInput) (*handlerutil.Out[[]swarm.SecretSummary], error) {
	items, err := h.service.ListSecrets(ctx)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm secrets")
	}
	if items == nil {
		items = []swarm.SecretSummary{}
	}

	return &handlerutil.Out[[]swarm.SecretSummary]{Body: base.ApiResponse[[]swarm.SecretSummary]{Success: true, Data: items}}, nil
}

// GetSecret returns details for a single swarm secret.
//
// It delegates to the swarm service and maps missing secrets to
// `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and secret to inspect.
//
// Returns the secret summary when the secret exists.
// Returns `404 Not Found` when the secret does not exist or another mapped HTTP
// error when inspection fails.
func (h *Handler) GetSecret(ctx context.Context, input *GetSwarmSecretInput) (*handlerutil.Out[swarm.SecretSummary], error) {
	secret, err := h.service.GetSecret(ctx, input.SecretID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm secret not found")
		}
		return nil, h.mapError(err, "Failed to inspect swarm secret")
	}

	return &handlerutil.Out[swarm.SecretSummary]{Body: base.ApiResponse[swarm.SecretSummary]{Success: true, Data: *secret}}, nil
}

// CreateSecret creates a new swarm secret.
//
// It requires admin privileges, delegates the creation request to the swarm
// service, and records an audit event containing the created secret ID and name.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the secret specification.
//
// Returns the created secret summary.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when validation or creation fails.
func (h *Handler) CreateSecret(ctx context.Context, input *CreateSwarmSecretInput) (*handlerutil.Out[swarm.SecretSummary], error) {
	secret, err := h.service.CreateSecret(ctx, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to create swarm secret")
	}

	h.audit(ctx, input.EnvironmentID, "secret.create", "swarm_secret", secret.ID, secret.Spec.Name, map[string]any{"secretId": secret.ID, "name": secret.Spec.Name})

	return &handlerutil.Out[swarm.SecretSummary]{Body: base.ApiResponse[swarm.SecretSummary]{Success: true, Data: *secret}}, nil
}

// DeleteSecret removes a swarm secret.
//
// It requires admin privileges, delegates removal to the swarm service, maps
// missing secrets to `404 Not Found`, and records an audit event on success.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the secret to remove.
//
// Returns a confirmation response when the secret is removed.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the secret does not exist, or another mapped HTTP error when removal fails.
func (h *Handler) DeleteSecret(ctx context.Context, input *DeleteSwarmSecretInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.RemoveSecret(ctx, input.SecretID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm secret not found")
		}
		return nil, h.mapError(err, "Failed to remove swarm secret")
	}

	h.audit(ctx, input.EnvironmentID, "secret.delete", "swarm_secret", input.SecretID, "", map[string]any{"secretId": input.SecretID})

	return handlerutil.MessageOutput("Swarm secret removed successfully", ""), nil
}
