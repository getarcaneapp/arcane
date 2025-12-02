//go:build playwright

package api

import (
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
)

type PlaywrightHandler struct {
	PlaywrightService *services.PlaywrightService
}

func NewPlaywrightHandler(playwrightService *services.PlaywrightService) *PlaywrightHandler {
	return &PlaywrightHandler{PlaywrightService: playwrightService}
}

func SetupPlaywrightRoutes(api *gin.RouterGroup, playwrightService *services.PlaywrightService) {
	playwright := api.Group("/playwright")

	playwrightHandler := NewPlaywrightHandler(playwrightService)

	playwright.POST("/skip-onboarding", playwrightHandler.SkipOnboardingHandler)
	playwright.POST("/reset-onboarding", playwrightHandler.ResetOnboardingHandler)
	playwright.POST("/create-test-api-keys", playwrightHandler.CreateTestApiKeysHandler)
	playwright.POST("/delete-test-api-keys", playwrightHandler.DeleteTestApiKeysHandler)
}

func (ph *PlaywrightHandler) SkipOnboardingHandler(c *gin.Context) {
	if err := ph.PlaywrightService.SkipOnboarding(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.SkipOnboardingError{Err: err}).Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

func (ph *PlaywrightHandler) ResetOnboardingHandler(c *gin.Context) {
	if err := ph.PlaywrightService.ResetOnboarding(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.ResetOnboardingError{Err: err}).Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

type CreateTestApiKeysRequest struct {
	Count int `json:"count"`
}

func (ph *PlaywrightHandler) CreateTestApiKeysHandler(c *gin.Context) {
	var req CreateTestApiKeysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Default to 2 if no count provided
		req.Count = 2
	}

	if req.Count <= 0 {
		req.Count = 2
	}

	apiKeys, err := ph.PlaywrightService.CreateTestApiKeys(c.Request.Context(), req.Count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"apiKeys": apiKeys})
}

func (ph *PlaywrightHandler) DeleteTestApiKeysHandler(c *gin.Context) {
	if err := ph.PlaywrightService.DeleteAllTestApiKeys(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
