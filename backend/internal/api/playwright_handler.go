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
}

// SkipOnboardingHandler godoc
// @Summary Skip onboarding (Playwright)
// @Description Skip the onboarding process (for testing only)
// @Tags Playwright
// @Success 204
// @Router /api/playwright/skip-onboarding [post]
func (ph *PlaywrightHandler) SkipOnboardingHandler(c *gin.Context) {
	if err := ph.PlaywrightService.SkipOnboarding(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.SkipOnboardingError{Err: err}).Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// ResetOnboardingHandler godoc
// @Summary Reset onboarding (Playwright)
// @Description Reset the onboarding process (for testing only)
// @Tags Playwright
// @Success 204
// @Router /api/playwright/reset-onboarding [post]
func (ph *PlaywrightHandler) ResetOnboardingHandler(c *gin.Context) {
	if err := ph.PlaywrightService.ResetOnboarding(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.ResetOnboardingError{Err: err}).Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
