//go:build playwright

package api

import (
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/services"
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
}

type CreateTestApiKeysRequest struct {
	Count int `json:"count"`
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
