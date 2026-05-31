package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	federatedtypes "github.com/getarcaneapp/arcane/types/federated"
	"github.com/labstack/echo/v4"
)

// FederatedCredentialHandler provides Huma-based federated credential
// management endpoints.
type FederatedCredentialHandler struct {
	federatedCredentialService *services.FederatedCredentialService
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

type ListFederatedCredentialsOutput struct {
	Body base.Paginated[federatedtypes.FederatedCredential]
}

type CreateFederatedCredentialInput struct {
	Body federatedtypes.CreateFederatedCredential
}

type CreateFederatedCredentialOutput struct {
	Body base.ApiResponse[federatedtypes.FederatedCredential]
}

type GetFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
}

type GetFederatedCredentialOutput struct {
	Body base.ApiResponse[federatedtypes.FederatedCredential]
}

type UpdateFederatedCredentialInput struct {
	ID   string `path:"id" doc:"Federated credential ID"`
	Body federatedtypes.UpdateFederatedCredential
}

type UpdateFederatedCredentialOutput struct {
	Body base.ApiResponse[federatedtypes.FederatedCredential]
}

type DeleteFederatedCredentialInput struct {
	ID string `path:"id" doc:"Federated credential ID"`
}

type DeleteFederatedCredentialOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// RegisterFederatedTokenExchange registers the public RFC 8693 token exchange
// endpoint. It intentionally uses Echo because the standard requires form
// encoding while the rest of Arcane's Huma API is JSON-first.
func RegisterFederatedTokenExchange(g *echo.Group, federatedCredentialService *services.FederatedCredentialService) {
	g.POST("/auth/federated/token", func(c echo.Context) error {
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
func RegisterFederatedCredentials(api huma.API, federatedCredentialService *services.FederatedCredentialService) {
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
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermFederatedList),
	}, h.ListFederatedCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "create-federated-credential",
		Method:      http.MethodPost,
		Path:        "/federated-credentials",
		Summary:     "Create a federated credential",
		Description: "Create a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.CreateFederatedCredential)

	huma.Register(api, huma.Operation{
		OperationID: "get-federated-credential",
		Method:      http.MethodGet,
		Path:        "/federated-credentials/{id}",
		Summary:     "Get a federated credential",
		Description: "Get details of a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermFederatedRead),
	}, h.GetFederatedCredential)

	huma.Register(api, huma.Operation{
		OperationID: "update-federated-credential",
		Method:      http.MethodPut,
		Path:        "/federated-credentials/{id}",
		Summary:     "Update a federated credential",
		Description: "Update a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.UpdateFederatedCredential)

	huma.Register(api, huma.Operation{
		OperationID: "delete-federated-credential",
		Method:      http.MethodDelete,
		Path:        "/federated-credentials/{id}",
		Summary:     "Delete a federated credential",
		Description: "Delete a workload identity federation trust rule and its service user",
		Tags:        []string{"Federated Credentials"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequireGlobalAdmin(api),
	}, h.DeleteFederatedCredential)
}

func (h *FederatedCredentialHandler) ListFederatedCredentials(ctx context.Context, input *ListFederatedCredentialsInput) (*ListFederatedCredentialsOutput, error) {
	if h.federatedCredentialService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	credentials, paginationResp, err := h.federatedCredentialService.List(ctx, pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: input.Search},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		Params: pagination.Params{
			Start: input.Start,
			Limit: input.Limit,
		},
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list federated credentials")
	}

	return &ListFederatedCredentialsOutput{
		Body: base.Paginated[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    credentials,
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

func (h *FederatedCredentialHandler) CreateFederatedCredential(ctx context.Context, input *CreateFederatedCredentialInput) (*CreateFederatedCredentialOutput, error) {
	if h.federatedCredentialService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	credential, err := h.federatedCredentialService.Create(ctx, user.ID, input.Body)
	if err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &CreateFederatedCredentialOutput{
		Body: base.ApiResponse[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) GetFederatedCredential(ctx context.Context, input *GetFederatedCredentialInput) (*GetFederatedCredentialOutput, error) {
	if h.federatedCredentialService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	credential, err := h.federatedCredentialService.Get(ctx, input.ID)
	if err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &GetFederatedCredentialOutput{
		Body: base.ApiResponse[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) UpdateFederatedCredential(ctx context.Context, input *UpdateFederatedCredentialInput) (*UpdateFederatedCredentialOutput, error) {
	if h.federatedCredentialService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	credential, err := h.federatedCredentialService.Update(ctx, user.ID, input.ID, input.Body)
	if err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &UpdateFederatedCredentialOutput{
		Body: base.ApiResponse[federatedtypes.FederatedCredential]{
			Success: true,
			Data:    *credential,
		},
	}, nil
}

func (h *FederatedCredentialHandler) DeleteFederatedCredential(ctx context.Context, input *DeleteFederatedCredentialInput) (*DeleteFederatedCredentialOutput, error) {
	if h.federatedCredentialService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.federatedCredentialService.Delete(ctx, input.ID); err != nil {
		return nil, federatedCredentialManagementErrorInternal(err)
	}

	return &DeleteFederatedCredentialOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Federated credential deleted successfully",
			},
		},
	}, nil
}

func writeFederatedTokenExchangeErrorInternal(c echo.Context, err error) error {
	var code string
	description := "token exchange rejected"

	switch {
	case common.IsErrorFederatedCredentialInvalidRequest(err):
		code = "invalid_request"
		description = "invalid token exchange request"
	case common.IsErrorFederatedCredentialInvalidGrant(err),
		common.IsErrorFederatedCredentialNotFound(err):
		code = "invalid_grant"
	case common.IsErrorFederatedCredentialInvalid(err):
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
	case common.IsErrorFederatedCredentialNotFound(err):
		return huma.Error404NotFound("federated credential not found")
	case common.IsErrorFederatedCredentialInvalid(err),
		common.IsErrorFederatedCredentialInvalidRequest(err):
		return huma.Error400BadRequest("invalid federated credential")
	case common.IsErrorFederatedCredentialPermissionEscalation(err):
		return huma.Error403Forbidden("permission denied")
	default:
		return huma.Error500InternalServerError("federated credential operation failed")
	}
}
