package federated

import (
	"context"
	"net/http"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	federatedtypes "github.com/getarcaneapp/arcane/types/v2/federated"
	"github.com/labstack/echo/v5"
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
	Body federatedtypes.CreateFederatedCredential
}

type GetFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
}

type UpdateFederatedCredentialInput struct {
	ID   string `path:"id" doc:"Federated credential ID"`
	Body federatedtypes.UpdateFederatedCredential
}

type DeleteFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
}

// RegisterFederatedTokenExchange registers the public RFC 8693 token exchange
// endpoint. It intentionally uses Echo because the standard requires form
// encoding while the rest of Arcane's Huma API is JSON-first.
func RegisterFederatedTokenExchange(g *echo.Group, federatedCredentialService *FederatedCredentialService) {
	g.POST("/auth/federated/token", func(c *echo.Context) error {
		if federatedCredentialService == nil {
			return c.JSON(http.StatusInternalServerError, federatedTokenExchangeError{
				Error:            "server_error",
				ErrorDescription: "service not available",
			})
		}
		if err := c.Request().ParseForm(); err != nil {
			return c.JSON(http.StatusBadRequest, federatedTokenExchangeError{
				Error:            "invalid_request",
				ErrorDescription: "invalid token exchange request",
			})
		}

		form := c.Request().Form
		resp, err := federatedCredentialService.ExchangeToken(c.Request().Context(), federatedtypes.TokenExchangeRequest{
			GrantType:          form.Get("grant_type"),
			SubjectToken:       form.Get("subject_token"),
			SubjectTokenType:   form.Get("subject_token_type"),
			Audience:           form.Get("audience"),
			Scope:              form.Get("scope"),
			RequestedTokenType: form.Get("requested_token_type"),
		})
		if err != nil {
			return writeFederatedTokenExchangeErrorInternal(c, err)
		}
		return c.JSON(http.StatusOK, resp)
	})
}

// RegisterFederatedCredentials registers federated credential management routes.
func RegisterFederatedCredentials(api huma.API, federatedCredentialService *FederatedCredentialService) {
	h := &FederatedCredentialHandler{
		federatedCredentialService: federatedCredentialService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-federated-credentials",
		Method:      http.MethodGet,
		Path:        "/federated-credentials",
		Summary:     "List federated credentials",
		Description: "Get a paginated list of workload identity federation trust rules",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermFederatedList),
	}, h.ListFederatedCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "create-federated-credential",
		Method:      http.MethodPost,
		Path:        "/federated-credentials",
		Summary:     "Create a federated credential",
		Description: "Create a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.CreateFederatedCredential)

	huma.Register(api, huma.Operation{
		OperationID: "get-federated-credential",
		Method:      http.MethodGet,
		Path:        "/federated-credentials/{id}",
		Summary:     "Get a federated credential",
		Description: "Get details of a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermFederatedRead),
	}, h.GetFederatedCredential)

	huma.Register(api, huma.Operation{
		OperationID: "update-federated-credential",
		Method:      http.MethodPut,
		Path:        "/federated-credentials/{id}",
		Summary:     "Update a federated credential",
		Description: "Update a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.UpdateFederatedCredential)

	huma.Register(api, huma.Operation{
		OperationID: "delete-federated-credential",
		Method:      http.MethodDelete,
		Path:        "/federated-credentials/{id}",
		Summary:     "Delete a federated credential",
		Description: "Delete a workload identity federation trust rule and its service user",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireGlobalAdmin(api),
	}, h.DeleteFederatedCredential)
}

func (h *FederatedCredentialHandler) ListFederatedCredentials(ctx context.Context, input *ListFederatedCredentialsInput) (*handlerutil.Page[federatedtypes.FederatedCredential], error) {
	credentials, paginationResp, err := h.federatedCredentialService.List(ctx, handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search))
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list federated credentials")
	}

	return &handlerutil.Page[federatedtypes.FederatedCredential]{
		Body: base.Paginated[federatedtypes.FederatedCredential]{
			Success:    true,
			Data:       credentials,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *FederatedCredentialHandler) CreateFederatedCredential(ctx context.Context, input *CreateFederatedCredentialInput) (*handlerutil.Out[federatedtypes.FederatedCredential], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	credential, err := h.federatedCredentialService.Create(ctx, user.ID, input.Body)
	if err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &handlerutil.Out[federatedtypes.FederatedCredential]{
		Body: base.ApiResponse[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) GetFederatedCredential(ctx context.Context, input *GetFederatedCredentialInput) (*handlerutil.Out[federatedtypes.FederatedCredential], error) {
	credential, err := h.federatedCredentialService.Get(ctx, input.ID)
	if err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &handlerutil.Out[federatedtypes.FederatedCredential]{
		Body: base.ApiResponse[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) UpdateFederatedCredential(ctx context.Context, input *UpdateFederatedCredentialInput) (*handlerutil.Out[federatedtypes.FederatedCredential], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	credential, err := h.federatedCredentialService.Update(ctx, user.ID, input.ID, input.Body)
	if err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &handlerutil.Out[federatedtypes.FederatedCredential]{
		Body: base.ApiResponse[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) DeleteFederatedCredential(ctx context.Context, input *DeleteFederatedCredentialInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.federatedCredentialService.Delete(ctx, input.ID); err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Federated credential deleted successfully",
			},
		},
	}, nil
}

func writeFederatedTokenExchangeErrorInternal(c *echo.Context, err error) error {
	var code string
	description := "token exchange rejected"

	switch {
	case errors.Is(err, common.ErrFederatedCredentialInvalidRequest):
		code = "invalid_request"
		description = "invalid token exchange request"
	case errors.Is(err, common.ErrFederatedCredentialInvalidGrant),
		errors.Is(err, common.ErrFederatedCredentialNotFound):
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

func federatedCredentialManagementErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrFederatedCredentialNotFound):
		return huma.Error404NotFound("federated credential not found")
	case errors.Is(err, common.ErrFederatedCredentialInvalid),
		errors.Is(err, common.ErrFederatedCredentialInvalidRequest):
		return huma.Error400BadRequest("invalid federated credential")
	case errors.Is(err, common.ErrFederatedCredentialPermissionEscalation):
		return huma.Error403Forbidden("permission denied")
	default:
		return huma.Error500InternalServerError("federated credential operation failed")
	}
}
