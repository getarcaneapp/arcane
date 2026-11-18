package source

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	SecretSourceHandler struct {
		service *Service
		sync    func(ctx context.Context) []types.EnvironmentSyncStatus
	}

	ListSecretSourcesInput struct{}

	ListSecretSourcePresetsInput struct{}

	SecretSourceIDInput struct {
		ID string `path:"id" doc:"Secret source ID"`
	}

	CreateSecretSourceInput struct {
		types.Create[types.SecretSourceInput]
	}

	UpdateSecretSourceInput struct {
		ID string `path:"id" doc:"Secret source ID"`
		types.Update[types.SecretSourceInput]
	}

	TestSecretSourceInput struct {
		types.Create[types.SecretSourceTestInput]
	}

	ValidateSecretSourceInput struct {
		Body types.SecretSourceValidateInput
	}
)

func NewHandler(service *Service, sync func(ctx context.Context) []types.EnvironmentSyncStatus) *SecretSourceHandler {
	return &SecretSourceHandler{
		service: service,
		sync:    sync,
	}
}

func (h *SecretSourceHandler) ListSecretSources(ctx context.Context, _ *ListSecretSourcesInput) (*handlerutil.Out[[]types.SecretSource], error) {
	sources, err := h.service.List(ctx)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[[]types.SecretSource]{
		Body: types.ApiResponse[[]types.SecretSource]{
			Success: true,
			Data:    sources,
		},
	}, nil
}

func (h *SecretSourceHandler) GetSecretSource(ctx context.Context, input *SecretSourceIDInput) (*handlerutil.Out[types.SecretSource], error) {
	source, err := h.service.Get(ctx, input.ID)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSource]{
		Body: types.ApiResponse[types.SecretSource]{
			Success: true,
			Data:    source,
		},
	}, nil
}

func (h *SecretSourceHandler) CreateSecretSource(ctx context.Context, input *CreateSecretSourceInput) (*handlerutil.Out[types.SecretSourceMutationResponse], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}
	source, err := h.service.Create(ctx, input.Body, submitted)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSourceMutationResponse]{
		Body: types.ApiResponse[types.SecretSourceMutationResponse]{
			Success: true,
			Data: types.SecretSourceMutationResponse{
				Source:      &source,
				SyncResults: h.sync(ctx),
			},
		},
	}, nil
}

func (h *SecretSourceHandler) UpdateSecretSource(ctx context.Context, input *UpdateSecretSourceInput) (*handlerutil.Out[types.SecretSourceMutationResponse], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}
	source, err := h.service.Update(ctx, input.ID, input.Body, submitted)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSourceMutationResponse]{
		Body: types.ApiResponse[types.SecretSourceMutationResponse]{
			Success: true,
			Data: types.SecretSourceMutationResponse{
				Source:      &source,
				SyncResults: h.sync(ctx),
			},
		},
	}, nil
}

func (h *SecretSourceHandler) DeleteSecretSource(ctx context.Context, input *SecretSourceIDInput) (*handlerutil.Out[types.SecretSourceMutationResponse], error) {
	if err := h.service.Delete(ctx, input.ID); err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSourceMutationResponse]{
		Body: types.ApiResponse[types.SecretSourceMutationResponse]{
			Success: true,
			Data: types.SecretSourceMutationResponse{
				SyncResults: h.sync(ctx),
			},
		},
	}, nil
}

func (h *SecretSourceHandler) RefreshSecretSource(ctx context.Context, input *SecretSourceIDInput) (*handlerutil.Out[types.SecretSourceMutationResponse], error) {
	source, err := h.service.Refresh(ctx, input.ID)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSourceMutationResponse]{
		Body: types.ApiResponse[types.SecretSourceMutationResponse]{
			Success: true,
			Data: types.SecretSourceMutationResponse{
				Source:      &source,
				SyncResults: h.sync(ctx),
			},
		},
	}, nil
}

func (h *SecretSourceHandler) TestSecretSource(ctx context.Context, input *TestSecretSourceInput) (*handlerutil.Out[types.SecretSourceTestResult], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}
	permissions, _ := middleware.PermissionsFromContext(ctx)
	result, err := h.service.Test(ctx, TestRequest{
		Permissions: permissions,
		Submitted:   submitted,
		Input:       input.Body,
	})
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSourceTestResult]{
		Body: types.ApiResponse[types.SecretSourceTestResult]{
			Success: true,
			Data:    result,
		},
	}, nil
}

func (h *SecretSourceHandler) ValidateDefinition(_ context.Context, input *ValidateSecretSourceInput) (*handlerutil.Out[types.SecretSourceDefinition], error) {
	definition, err := h.service.ValidateDefinition(input.Body.Definition)
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[types.SecretSourceDefinition]{
		Body: types.ApiResponse[types.SecretSourceDefinition]{
			Success: true,
			Data:    definition,
		},
	}, nil
}

func (h *SecretSourceHandler) ListPresets(_ context.Context, _ *ListSecretSourcePresetsInput) (*handlerutil.Out[[]types.SecretSourcePreset], error) {
	presets, err := h.service.Presets()
	if err != nil {
		return nil, handlerutil.APIError(err)
	}
	return &handlerutil.Out[[]types.SecretSourcePreset]{
		Body: types.ApiResponse[[]types.SecretSourcePreset]{
			Success: true,
			Data:    presets,
		},
	}, nil
}
