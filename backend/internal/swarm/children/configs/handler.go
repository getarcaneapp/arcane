package configs

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/swarm"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the swarm config endpoints.
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

type ListSwarmConfigsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
}

type CreateSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarm.ConfigCreateRequest
}

type DeleteSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
}

// ListConfigs lists swarm configs in the current environment.
//
// It delegates to the swarm service and normalizes nil config slices to empty
// arrays in the response body.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose configs should be listed.
//
// Returns the current swarm configs.
// Returns a mapped HTTP error when config enumeration fails.
func (h *Handler) ListConfigs(ctx context.Context, input *ListSwarmConfigsInput) (*handlerutil.Out[[]swarm.ConfigSummary], error) {
	items, err := h.service.ListConfigs(ctx)
	if err != nil {
		return nil, h.mapError(err, "Failed to list swarm configs")
	}
	if items == nil {
		items = []swarm.ConfigSummary{}
	}

	return &handlerutil.Out[[]swarm.ConfigSummary]{Body: base.ApiResponse[[]swarm.ConfigSummary]{Success: true, Data: items}}, nil
}

// GetConfig returns details for a single swarm config.
//
// It delegates to the swarm service and maps missing configs to
// `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and swarm config to inspect.
//
// Returns the config summary when the config exists.
// Returns `404 Not Found` when the config does not exist or another mapped HTTP
// error when inspection fails.
func (h *Handler) GetConfig(ctx context.Context, input *GetSwarmConfigInput) (*handlerutil.Out[swarm.ConfigSummary], error) {
	cfg, err := h.service.GetConfig(ctx, input.ConfigID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm config not found")
		}
		return nil, h.mapError(err, "Failed to inspect swarm config")
	}

	return &handlerutil.Out[swarm.ConfigSummary]{Body: base.ApiResponse[swarm.ConfigSummary]{Success: true, Data: *cfg}}, nil
}

// CreateConfig creates a new swarm config.
//
// It requires admin privileges, delegates the creation request to the swarm
// service, and records an audit event containing the created config ID and name.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the config specification.
//
// Returns the created config summary.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when validation or creation fails.
func (h *Handler) CreateConfig(ctx context.Context, input *CreateSwarmConfigInput) (*handlerutil.Out[swarm.ConfigSummary], error) {
	cfg, err := h.service.CreateConfig(ctx, input.Body)
	if err != nil {
		return nil, h.mapError(err, "Failed to create swarm config")
	}

	h.audit(ctx, input.EnvironmentID, "config.create", "swarm_config", cfg.ID, cfg.Spec.Name, map[string]any{"configId": cfg.ID, "name": cfg.Spec.Name})

	return &handlerutil.Out[swarm.ConfigSummary]{Body: base.ApiResponse[swarm.ConfigSummary]{Success: true, Data: *cfg}}, nil
}

// DeleteConfig removes a swarm config.
//
// It requires admin privileges, delegates removal to the swarm service, maps
// missing configs to `404 Not Found`, and records an audit event on success.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the config to remove.
//
// Returns a confirmation response when the config is removed.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the config does not exist, or another mapped HTTP error when removal fails.
func (h *Handler) DeleteConfig(ctx context.Context, input *DeleteSwarmConfigInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.RemoveConfig(ctx, input.ConfigID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm config not found")
		}
		return nil, h.mapError(err, "Failed to remove swarm config")
	}

	h.audit(ctx, input.EnvironmentID, "config.delete", "swarm_config", input.ConfigID, "", map[string]any{"configId": input.ConfigID})

	return handlerutil.MessageOutput("Swarm config removed successfully", ""), nil
}
