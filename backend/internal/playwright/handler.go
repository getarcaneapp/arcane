//go:build playwright

package playwright

import (
	"net/http"

	"github.com/getarcaneapp/arcane/backend/v2/internal/federated"
	"github.com/labstack/echo/v5"
)

type playwrightHandlerInternal struct {
	service   *PlaywrightService
	federated *federated.FederatedCredentialService
}

type createTestAPIKeysRequestInternal struct {
	Count int `json:"count"`
}

type createTestFederatedCredentialRequestInternal struct {
	IssuerURL       string   `json:"issuerUrl"`
	Audiences       []string `json:"audiences"`
	Subject         string   `json:"subject"`
	RoleID          string   `json:"roleId"`
	TokenTTLSeconds int      `json:"tokenTtlSeconds"`
}

// SetupRoutes registers endpoints used by the Playwright end-to-end suite.
func SetupRoutes(api *echo.Group, service *PlaywrightService, federatedService *federated.FederatedCredentialService) {
	group := api.Group("/playwright")
	handler := &playwrightHandlerInternal{service: service, federated: federatedService}

	group.POST("/create-test-api-keys", handler.createTestAPIKeysInternal)
	group.POST("/delete-test-api-keys", handler.deleteTestAPIKeysInternal)
	group.POST("/create-test-federated-credential", handler.createTestFederatedCredentialInternal)
}

func (h *playwrightHandlerInternal) createTestAPIKeysInternal(c *echo.Context) error {
	var req createTestAPIKeysRequestInternal
	if err := c.Bind(&req); err != nil || req.Count <= 0 {
		req.Count = 2
	}

	apiKeys, err := h.service.CreateTestApiKeys(c.Request().Context(), req.Count)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]any{"apiKeys": apiKeys})
}

func (h *playwrightHandlerInternal) deleteTestAPIKeysInternal(c *echo.Context) error {
	if err := h.service.DeleteAllTestApiKeys(c.Request().Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *playwrightHandlerInternal) createTestFederatedCredentialInternal(c *echo.Context) error {
	var req createTestFederatedCredentialRequestInternal
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
