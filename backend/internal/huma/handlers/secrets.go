package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	secrettypes "github.com/getarcaneapp/arcane/types/secret"
)

// SecretHandler provides Huma-based secrets management endpoints.
type SecretHandler struct {
	secretService *services.SecretService
}

// SecretPaginatedResponse is the paginated response for secrets.
type SecretPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []secrettypes.Secret    `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListSecretsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSecretsOutput struct {
	Body SecretPaginatedResponse
}

type GetSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type GetSecretOutput struct {
	Body base.ApiResponse[secrettypes.Secret]
}

type GetSecretContentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type GetSecretContentOutput struct {
	Body base.ApiResponse[secrettypes.SecretWithContent]
}

type CreateSecretInput struct {
	EnvironmentID string             `path:"id" doc:"Environment ID"`
	Body          secrettypes.Create `doc:"Secret creation data"`
}

type CreateSecretOutput struct {
	Body base.ApiResponse[secrettypes.Secret]
}

type UpdateSecretInput struct {
	EnvironmentID string             `path:"id" doc:"Environment ID"`
	SecretID      string             `path:"secretId" doc:"Secret ID"`
	Body          secrettypes.Update `doc:"Secret update data"`
}

type UpdateSecretOutput struct {
	Body base.ApiResponse[secrettypes.Secret]
}

type DeleteSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type DeleteSecretOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// RegisterSecrets registers secret management routes using Huma.
func RegisterSecrets(api huma.API, secretService *services.SecretService) {
	h := &SecretHandler{secretService: secretService}

	huma.Register(api, huma.Operation{
		OperationID: "list-secrets",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/secrets",
		Summary:     "List secrets",
		Description: "Get a paginated list of secrets",
		Tags:        []string{"Secrets"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListSecrets)

	huma.Register(api, huma.Operation{
		OperationID: "get-secret",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/secrets/{secretId}",
		Summary:     "Get secret metadata",
		Description: "Get a secret's metadata without content",
		Tags:        []string{"Secrets"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetSecret)

	huma.Register(api, huma.Operation{
		OperationID: "get-secret-content",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/secrets/{secretId}/content",
		Summary:     "Get secret content",
		Description: "Get a secret with decrypted content",
		Tags:        []string{"Secrets"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetSecretContent)

	huma.Register(api, huma.Operation{
		OperationID: "create-secret",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/secrets",
		Summary:     "Create secret",
		Description: "Create a new secret",
		Tags:        []string{"Secrets"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.CreateSecret)

	huma.Register(api, huma.Operation{
		OperationID: "update-secret",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/secrets/{secretId}",
		Summary:     "Update secret",
		Description: "Update an existing secret",
		Tags:        []string{"Secrets"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateSecret)

	huma.Register(api, huma.Operation{
		OperationID: "delete-secret",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/secrets/{secretId}",
		Summary:     "Delete secret",
		Description: "Delete a secret",
		Tags:        []string{"Secrets"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.DeleteSecret)
}

// ListSecrets returns a paginated list of secrets.
func (h *SecretHandler) ListSecrets(ctx context.Context, input *ListSecretsInput) (*ListSecretsOutput, error) {
	if h.secretService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(0, input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.secretService.ListSecretsPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.SecretListError{Err: err}).Error())
	}

	if items == nil {
		items = []secrettypes.Secret{}
	}

	return &ListSecretsOutput{
		Body: SecretPaginatedResponse{
			Success: true,
			Data:    items,
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

// GetSecret returns secret metadata.
func (h *SecretHandler) GetSecret(ctx context.Context, input *GetSecretInput) (*GetSecretOutput, error) {
	if h.secretService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	secret, err := h.secretService.GetSecretByID(ctx, input.EnvironmentID, input.SecretID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.SecretRetrievalError{Err: err}).Error())
	}

	composePath := ""
	if path, err := h.secretService.ComposeSecretPath(ctx, secret.Name); err == nil {
		composePath = path
	}

	out := secrettypes.Secret{
		ID:            secret.ID,
		Name:          secret.Name,
		EnvironmentID: secret.EnvironmentID,
		Description:   secret.Description,
		CreatedAt:     secret.CreatedAt,
		UpdatedAt:     secret.UpdatedAt,
		ComposePath:   composePath,
	}

	return &GetSecretOutput{
		Body: base.ApiResponse[secrettypes.Secret]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetSecretContent returns secret metadata with decrypted content.
func (h *SecretHandler) GetSecretContent(ctx context.Context, input *GetSecretContentInput) (*GetSecretContentOutput, error) {
	if h.secretService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	secret, err := h.secretService.GetSecretWithContent(ctx, input.EnvironmentID, input.SecretID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.SecretContentError{Err: err}).Error())
	}

	return &GetSecretContentOutput{
		Body: base.ApiResponse[secrettypes.SecretWithContent]{
			Success: true,
			Data:    *secret,
		},
	}, nil
}

// CreateSecret creates a new secret.
func (h *SecretHandler) CreateSecret(ctx context.Context, input *CreateSecretInput) (*CreateSecretOutput, error) {
	if h.secretService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	secret, err := h.secretService.CreateSecret(ctx, input.EnvironmentID, input.Body, *user)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.SecretCreationError{Err: err}).Error())
	}

	return &CreateSecretOutput{
		Body: base.ApiResponse[secrettypes.Secret]{
			Success: true,
			Data:    *secret,
		},
	}, nil
}

// UpdateSecret updates an existing secret.
func (h *SecretHandler) UpdateSecret(ctx context.Context, input *UpdateSecretInput) (*UpdateSecretOutput, error) {
	if h.secretService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	secret, err := h.secretService.UpdateSecret(ctx, input.EnvironmentID, input.SecretID, input.Body, *user)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.SecretUpdateError{Err: err}).Error())
	}

	return &UpdateSecretOutput{
		Body: base.ApiResponse[secrettypes.Secret]{
			Success: true,
			Data:    *secret,
		},
	}, nil
}

// DeleteSecret deletes a secret.
func (h *SecretHandler) DeleteSecret(ctx context.Context, input *DeleteSecretInput) (*DeleteSecretOutput, error) {
	if h.secretService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.secretService.DeleteSecret(ctx, input.EnvironmentID, input.SecretID, *user); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.SecretDeletionError{Err: err}).Error())
	}

	return &DeleteSecretOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Secret deleted successfully",
			},
		},
	}, nil
}
