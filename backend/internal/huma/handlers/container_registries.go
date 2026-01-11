package handlers

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/registry"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/containerregistry"
)

// ContainerRegistryHandler handles container registry management endpoints.
type ContainerRegistryHandler struct {
	registryService *services.ContainerRegistryService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// ContainerRegistryPaginatedResponse is the paginated response for container registries.
type ContainerRegistryPaginatedResponse struct {
	Success    bool                                  `json:"success"`
	Data       []containerregistry.ContainerRegistry `json:"data"`
	Pagination base.PaginationResponse               `json:"pagination"`
}

type ListContainerRegistriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ListContainerRegistriesOutput struct {
	Body ContainerRegistryPaginatedResponse
}

type CreateContainerRegistryInput struct {
	Body models.CreateContainerRegistryRequest
}

type CreateContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type GetContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type GetContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type UpdateContainerRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body models.UpdateContainerRegistryRequest
}

type UpdateContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type DeleteContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type DeleteContainerRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type TestContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type TestContainerRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type SyncContainerRegistriesInput struct {
	Body containerregistry.SyncRequest
}

type SyncContainerRegistriesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterContainerRegistries registers all container registry endpoints.
func RegisterContainerRegistries(api huma.API, registryService *services.ContainerRegistryService) {
	h := &ContainerRegistryHandler{registryService: registryService}

	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistries",
		Method:      "GET",
		Path:        "/container-registries",
		Summary:     "List container registries",
		Description: "Get a paginated list of container registries",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "createContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries",
		Summary:     "Create a container registry",
		Description: "Create a new container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "syncContainerRegistries",
		Method:      "POST",
		Path:        "/container-registries/sync",
		Summary:     "Sync container registries",
		Description: "Sync container registries from a remote source",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.SyncRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistry",
		Method:      "GET",
		Path:        "/container-registries/{id}",
		Summary:     "Get a container registry",
		Description: "Get a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "updateContainerRegistry",
		Method:      "PUT",
		Path:        "/container-registries/{id}",
		Summary:     "Update a container registry",
		Description: "Update an existing container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "deleteContainerRegistry",
		Method:      "DELETE",
		Path:        "/container-registries/{id}",
		Summary:     "Delete a container registry",
		Description: "Delete a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "testContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries/{id}/test",
		Summary:     "Test a container registry",
		Description: "Test connectivity and authentication to a container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.TestRegistry)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRegistries returns a paginated list of container registries.
func (h *ContainerRegistryHandler) ListRegistries(ctx context.Context, input *ListContainerRegistriesInput) (*ListContainerRegistriesOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(0, input.Start, input.Limit, input.Sort, input.Order, input.Search)

	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryListError{Err: err}).Error())
	}

	return &ListContainerRegistriesOutput{
		Body: ContainerRegistryPaginatedResponse{
			Success: true,
			Data:    registries,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// CreateRegistry creates a new container registry.
func (h *ContainerRegistryHandler) CreateRegistry(ctx context.Context, input *CreateContainerRegistryInput) (*CreateContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	reg, err := h.registryService.CreateRegistry(ctx, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryCreationError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &CreateContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetRegistry returns a container registry by ID.
func (h *ContainerRegistryHandler) GetRegistry(ctx context.Context, input *GetContainerRegistryInput) (*GetContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryRetrievalError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &GetContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRegistry updates a container registry.
func (h *ContainerRegistryHandler) UpdateRegistry(ctx context.Context, input *UpdateContainerRegistryInput) (*UpdateContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	reg, err := h.registryService.UpdateRegistry(ctx, input.ID, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryUpdateError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &UpdateContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteRegistry deletes a container registry.
func (h *ContainerRegistryHandler) DeleteRegistry(ctx context.Context, input *DeleteContainerRegistryInput) (*DeleteContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.registryService.DeleteRegistry(ctx, input.ID); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryDeletionError{Err: err}).Error())
	}

	return &DeleteContainerRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Container registry deleted successfully",
			},
		},
	}, nil
}

// TestRegistry tests connectivity to a container registry.
func (h *ContainerRegistryHandler) TestRegistry(ctx context.Context, input *TestContainerRegistryInput) (*TestContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryRetrievalError{Err: err}).Error())
	}

	decryptedToken, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TokenDecryptionError{Err: err}).Error())
	}

	testResult, testErr := h.performRegistryTest(ctx, reg, decryptedToken)
	if testErr != nil {
		return nil, huma.Error400BadRequest((&common.RegistryTestError{Err: testErr}).Error())
	}

	return &TestContainerRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: testResult["message"].(string),
			},
		},
	}, nil
}

// SyncRegistries syncs container registries from a remote source.
func (h *ContainerRegistryHandler) SyncRegistries(ctx context.Context, input *SyncContainerRegistriesInput) (*SyncContainerRegistriesOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.registryService.SyncRegistries(ctx, input.Body.Registries); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistrySyncError{Err: err}).Error())
	}

	return &SyncContainerRegistriesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registries synced successfully",
			},
		},
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

func (h *ContainerRegistryHandler) performRegistryTest(ctx context.Context, registryModel *models.ContainerRegistry, decryptedToken string) (map[string]interface{}, error) {
	var creds *registry.Credentials
	if registryModel.Username != "" && decryptedToken != "" {
		creds = &registry.Credentials{
			Username: registryModel.Username,
			Token:    decryptedToken,
		}
	}

	testResult, err := registry.TestRegistryConnection(ctx, registryModel.URL, creds)
	if err != nil {
		return nil, err
	}

	if !testResult.AuthSuccess {
		if len(testResult.Errors) > 0 {
			return nil, fmt.Errorf("%s", testResult.Errors[0])
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	return map[string]interface{}{
		"message": "Authentication succeeded",
	}, nil
}
