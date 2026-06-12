//go:build playwright

package api

import (
	"net/http"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/labstack/echo/v4"
)

type PlaywrightHandler struct {
	PlaywrightService *services.PlaywrightService
}

func NewPlaywrightHandler(playwrightService *services.PlaywrightService) *PlaywrightHandler {
	return &PlaywrightHandler{PlaywrightService: playwrightService}
}

func SetupPlaywrightRoutes(api *echo.Group, playwrightService *services.PlaywrightService) {
	playwright := api.Group("/playwright")

	playwrightHandler := NewPlaywrightHandler(playwrightService)

	playwright.POST("/create-test-api-keys", playwrightHandler.CreateTestApiKeysHandler)
	playwright.POST("/delete-test-api-keys", playwrightHandler.DeleteTestApiKeysHandler)
	playwright.POST("/create-test-federated-credential", playwrightHandler.CreateTestFederatedCredentialHandler)
}

type CreateTestApiKeysRequest struct {
	Count int `json:"count"`
}

type CreateTestFederatedCredentialRequest struct {
	IssuerURL       string   `json:"issuerUrl"`
	Audiences       []string `json:"audiences"`
	Subject         string   `json:"subject"`
	RoleID          string   `json:"roleId"`
	TokenTTLSeconds int      `json:"tokenTtlSeconds"`
}

func (ph *PlaywrightHandler) CreateTestApiKeysHandler(c echo.Context) error {
	var req CreateTestApiKeysRequest
	if err := c.Bind(&req); err != nil {
		req.Count = 2
	}

	if req.Count <= 0 {
		req.Count = 2
	}

	apiKeys, err := ph.PlaywrightService.CreateTestApiKeys(c.Request().Context(), req.Count)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]any{"apiKeys": apiKeys})
}

func (ph *PlaywrightHandler) DeleteTestApiKeysHandler(c echo.Context) error {
	if err := ph.PlaywrightService.DeleteAllTestApiKeys(c.Request().Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (ph *PlaywrightHandler) CreateTestFederatedCredentialHandler(c echo.Context) error {
	var req CreateTestFederatedCredentialRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid request body"})
	}

	credentialID, err := ph.PlaywrightService.CreateTestFederatedCredential(
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
