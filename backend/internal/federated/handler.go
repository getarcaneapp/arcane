package federated

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// FederatedCredentialHandler provides Huma-based federated credential
// management endpoints.
type FederatedCredentialHandler struct {
	federatedCredentialService *FederatedCredentialService
}

type federatedTokenExchangeError struct {
	Error            string `json:"error"`                       //nolint:tagliatelle // RFC 6749 wire shape is snake_case.
	ErrorDescription string `json:"error_description,omitempty"` //nolint:tagliatelle // RFC 6749 wire shape is snake_case.
}

type ListFederatedCredentialsInput struct {
	Search string `query:"search" doc:"Search query for filtering by name, issuer, or subject"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type CreateFederatedCredentialInput struct {
	types.Create[types.FederatedCredential]
}

type GetFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
}

type UpdateFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
	types.Update[types.FederatedCredential]
}

type DeleteFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
}

func (h *FederatedCredentialHandler) ListFederatedCredentials(ctx context.Context, input *ListFederatedCredentialsInput) (*handlerutil.Page[types.FederatedCredential], error) {
	credentials, paginationResp, err := h.federatedCredentialService.List(ctx, handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search))
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list federated credentials")
	}

	return &handlerutil.Page[types.FederatedCredential]{
		Body: types.Paginated[types.FederatedCredential]{
			Success:    true,
			Data:       credentials,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *FederatedCredentialHandler) CreateFederatedCredential(ctx context.Context, input *CreateFederatedCredentialInput) (*handlerutil.Out[types.FederatedCredential], error) {
	caller, _ := authz.PermissionsFromContext(ctx)
	credential, err := h.federatedCredentialService.Create(ctx, caller, input.Body)
	if err != nil {
		return nil, federatedCredentialManagementError(err)
	}

	return &handlerutil.Out[types.FederatedCredential]{
		Body: types.ApiResponse[types.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) GetFederatedCredential(ctx context.Context, input *GetFederatedCredentialInput) (*handlerutil.Out[types.FederatedCredential], error) {
	credential, err := h.federatedCredentialService.Get(ctx, input.ID)
	if err != nil {
		return nil, federatedCredentialManagementError(err)
	}

	return &handlerutil.Out[types.FederatedCredential]{
		Body: types.ApiResponse[types.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) UpdateFederatedCredential(ctx context.Context, input *UpdateFederatedCredentialInput) (*handlerutil.Out[types.FederatedCredential], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}

	caller, _ := authz.PermissionsFromContext(ctx)
	credential, err := h.federatedCredentialService.Update(ctx, caller, input.ID, input.Body, submitted)
	if err != nil {
		return nil, federatedCredentialManagementError(err)
	}

	return &handlerutil.Out[types.FederatedCredential]{
		Body: types.ApiResponse[types.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) DeleteFederatedCredential(ctx context.Context, input *DeleteFederatedCredentialInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.federatedCredentialService.Delete(ctx, input.ID); err != nil {
		return nil, federatedCredentialManagementError(err)
	}

	return handlerutil.MessageOutput("Federated credential deleted successfully", ""), nil
}

func writeFederatedTokenExchangeError(c *echo.Context, err error) error {
	var code string
	description := "token exchange rejected"

	switch {
	case errors.Is(err, common.ErrFederatedCredentialInvalidRequest):
		code = "invalid_request"
		description = "invalid token exchange request"
	case errors.Is(err, common.ErrFederatedCredentialInvalidGrant), errors.Is(err, common.ErrFederatedCredentialNotFound):
		code = "invalid_grant"
	case errors.Is(err, common.ErrFederatedCredentialInvalid):
		code = "invalid_request"
	default:
		code = "server_error"
		description = "token exchange failed"
	}

	return c.JSON(http.StatusBadRequest, federatedTokenExchangeError{
		Error:            code,
		ErrorDescription: description,
	})
}

func federatedCredentialManagementError(err error) error {
	switch {
	case errors.Is(err, common.ErrFederatedCredentialNotFound):
		return huma.Error404NotFound("federated credential not found")
	case errors.Is(err, common.ErrFederatedCredentialInvalid), errors.Is(err, common.ErrFederatedCredentialInvalidRequest):
		return huma.Error400BadRequest("invalid federated credential")
	case errors.Is(err, common.ErrFederatedCredentialPermissionEscalation):
		return huma.Error403Forbidden("permission denied")
	default:
		return huma.Error500InternalServerError("federated credential operation failed")
	}
}
