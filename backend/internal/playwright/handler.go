//go:build playwright

package playwright

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/getarcaneapp/arcane/backend/v3/internal/federated"
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

// SetupRoutes registers endpoints used by the Playwright end-to-end suite.
func SetupRoutes(api *echo.Group, service *PlaywrightService, federatedService *federated.FederatedCredentialService) {
	group := api.Group("/playwright")
	handler := &playwrightHandler{service: service, federated: federatedService}

	group.POST("/gitops", handler.createTestGitOpsProject)
	group.POST("/apikeys", handler.createTestAPIKeys)
	group.DELETE("/apikeys", handler.deleteTestAPIKeys)
	group.POST("/federated", handler.createTestFederatedCredential)
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
