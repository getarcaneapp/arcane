// Package huma provides Huma API setup and configuration for OpenAPI generation.
// This package allows gradual migration of handlers from Gin to Huma while
// maintaining the existing Gin router.
package huma

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/huma/handlers"
	"github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
)

// Services holds all service dependencies needed by Huma handlers.
type Services struct {
	User   *services.UserService
	Auth   *services.AuthService
	Oidc   *services.OidcService
	ApiKey *services.ApiKeyService
	// Add more services here as handlers are migrated
}

// SetupAPI creates and configures the Huma API alongside the existing Gin router.
// This allows gradual migration of handlers from Gin to Huma.
func SetupAPI(router *gin.Engine, apiGroup *gin.RouterGroup, cfg *config.Config, svc *Services) huma.API {
	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"

	// Configure servers for OpenAPI spec
	if cfg.AppUrl != "" {
		humaConfig.Servers = []*huma.Server{
			{URL: cfg.AppUrl + "/api"},
		}
	} else {
		humaConfig.Servers = []*huma.Server{
			{URL: "/api"},
		}
	}

	// Configure security schemes
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "JWT Bearer token authentication",
		},
		"ApiKeyAuth": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-API-Key",
			Description: "API Key authentication",
		},
	}

	// Create Huma API wrapping the Gin router group
	api := humagin.NewWithGroup(router, apiGroup, humaConfig)

	// Add authentication middleware
	api.UseMiddleware(middleware.NewAuthBridge(svc.Auth, cfg))

	// Register all Huma handlers
	registerHandlers(api, svc)

	return api
}

// SetupAPIForSpec creates a Huma API instance for OpenAPI spec generation only.
// No services are required - this is purely for schema generation.
func SetupAPIForSpec() huma.API {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("Arcane API", config.Version)
	humaConfig.Info.Description = "Modern Docker Management, Designed for Everyone"
	humaConfig.Servers = []*huma.Server{
		{URL: "/api"},
	}
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "JWT Bearer token authentication",
		},
		"ApiKeyAuth": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-API-Key",
			Description: "API Key authentication",
		},
	}

	api := humagin.NewWithGroup(router, apiGroup, humaConfig)

	// Register handlers with nil services (just for schema)
	registerHandlers(api, nil)

	return api
}

// registerHandlers registers all Huma-based API handlers.
// Add new handlers here as they are migrated from Gin.
func registerHandlers(api huma.API, svc *Services) {
	var userSvc *services.UserService
	var authSvc *services.AuthService
	var oidcSvc *services.OidcService
	var apiKeySvc *services.ApiKeyService

	if svc != nil {
		userSvc = svc.User
		authSvc = svc.Auth
		oidcSvc = svc.Oidc
		apiKeySvc = svc.ApiKey
	}

	// Auth handlers
	handlers.RegisterAuth(api, userSvc, authSvc, oidcSvc)

	// API Key handlers
	handlers.RegisterApiKeys(api, apiKeySvc)

	// Add more handler registrations here as they are migrated:
	// handlers.RegisterContainers(api, svc.Docker, svc.Container)
	// handlers.RegisterImages(api, svc.Docker, svc.Image)
	// etc.
}
