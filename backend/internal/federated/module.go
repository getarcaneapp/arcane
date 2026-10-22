package federated

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

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
		resp, err := federatedCredentialService.ExchangeToken(c.Request().Context(), types.TokenExchangeRequest{
			GrantType:          form.Get("grant_type"),
			SubjectToken:       form.Get("subject_token"),
			SubjectTokenType:   form.Get("subject_token_type"),
			Audience:           form.Get("audience"),
			Scope:              form.Get("scope"),
			RequestedTokenType: form.Get("requested_token_type"),
		})
		if err != nil {
			return writeFederatedTokenExchangeError(c, err)
		}
		return c.JSON(http.StatusOK, resp)
	})
}

// RegisterFederatedCredentials registers federated credential management routes.
func RegisterFederatedCredentials(api huma.API, federatedCredentialService *FederatedCredentialService) {
	h := &FederatedCredentialHandler{
		federatedCredentialService: federatedCredentialService,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-federated-credentials",
		Method:      http.MethodGet,
		Path:        "/federated/credentials",
		Summary:     "List federated credentials",
		Description: "Get a paginated list of workload identity federation trust rules",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedList, h.ListFederatedCredentials)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-federated-credential",
		Method:      http.MethodPost,
		Path:        "/federated/credentials",
		Summary:     "Create a federated credential",
		Description: "Create a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedCreate, h.CreateFederatedCredential)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-federated-credential",
		Method:      http.MethodGet,
		Path:        "/federated/credentials/{id}",
		Summary:     "Get a federated credential",
		Description: "Get details of a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedRead, h.GetFederatedCredential)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-federated-credential",
		Method:      http.MethodPatch,
		Path:        "/federated/credentials/{id}",
		Summary:     "Update a federated credential",
		Description: "Update a workload identity federation trust rule",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedUpdate, h.UpdateFederatedCredential)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-federated-credential",
		Method:      http.MethodDelete,
		Path:        "/federated/credentials/{id}",
		Summary:     "Delete a federated credential",
		Description: "Delete a workload identity federation trust rule and its service user",
		Tags:        []string{"Federated Credentials"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermFederatedDelete, h.DeleteFederatedCredential)
}
