//go:build playwright

package playwright

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"

	"getarcane.dev/arcane/v3/internal/federated"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
)

type (
	playwrightHandler struct {
		service   *PlaywrightService
		federated *federated.FederatedCredentialService
	}

	createTestAPIKeysRequest struct {
		Count int `json:"count"`
	}

	createTestFederatedCredentialRequest struct {
		IssuerURL       string   `json:"issuerUrl"`
		Subject         string   `json:"subject"`
		RoleID          string   `json:"roleId"`
		Audiences       []string `json:"audiences"`
		TokenTTLSeconds int      `json:"tokenTtlSeconds"`
	}
)

func RegisterRoutes(api huma.API, service *PlaywrightService, federatedService *federated.FederatedCredentialService) {
	handler := &playwrightHandler{
		service:   service,
		federated: federatedService,
	}
	huma.Register(api, huma.Operation{
		OperationID: "create-playwright-gitops-project",
		Method:      http.MethodPost,
		Path:        "/playwright/gitops",
		Tags:        []string{"Playwright"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(handler.createTestGitOpsProject))
	huma.Register(api, huma.Operation{
		OperationID: "create-playwright-api-keys",
		Method:      http.MethodPost,
		Path:        "/playwright/apikeys",
		Tags:        []string{"Playwright"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(handler.createTestAPIKeys))
	huma.Register(api, huma.Operation{
		OperationID: "delete-playwright-api-keys",
		Method:      http.MethodDelete,
		Path:        "/playwright/apikeys",
		Tags:        []string{"Playwright"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(handler.deleteTestAPIKeys))
	huma.Register(api, huma.Operation{
		OperationID: "create-playwright-federated-credential",
		Method:      http.MethodPost,
		Path:        "/playwright/federated",
		Tags:        []string{"Playwright"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessPublic},
	}, middleware.EchoHandler(handler.createTestFederatedCredential))
}

func (h *playwrightHandler) createTestAPIKeys(c *echo.Context) error {
	var req createTestAPIKeysRequest
	if err := c.Bind(&req); err != nil || req.Count <= 0 {
		req.Count = 2
	}

	apiKeys, err := h.service.CreateTestApiKeys(c.Request().Context(), req.Count)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]any{"apiKeys": apiKeys})
}

func (h *playwrightHandler) deleteTestAPIKeys(c *echo.Context) error {
	if err := h.service.DeleteAllTestApiKeys(c.Request().Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *playwrightHandler) createTestFederatedCredential(c *echo.Context) error {
	var req createTestFederatedCredentialRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid request body"})
	}

	credentialID, err := h.federated.CreatePlaywrightCredential(
		c.Request().Context(),
		req.IssuerURL,
		req.Audiences,
		req.Subject,
		req.RoleID,
		req.TokenTTLSeconds,
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]any{"credential": map[string]string{"id": credentialID}})
}

func (h *playwrightHandler) createTestGitOpsProject(c *echo.Context) error {
	if err := h.service.CreateTestGitOpsProject(c.Request().Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
