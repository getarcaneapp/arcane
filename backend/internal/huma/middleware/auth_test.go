package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type secureInput struct{}

type secureOutput struct {
	Body struct {
		UserID string `json:"userId"`
	} `json:"body"`
}

type testEnvironmentAccessResolver struct {
	env *models.Environment
}

func (r testEnvironmentAccessResolver) ResolveEnvironmentByAccessToken(_ context.Context, token string) (*models.Environment, error) {
	if r.env != nil && r.env.AccessToken != nil && *r.env.AccessToken == token {
		return r.env, nil
	}
	return nil, context.Canceled
}

func TestNewAuthBridge_AcceptsEnvironmentAccessTokenViaAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	token := "env-access-token"
	router := gin.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"ApiKeyAuth": {
			Type: "apiKey",
			In:   "header",
			Name: "X-API-Key",
		},
	}

	api := humagin.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewAuthBridge(api, &services.AuthService{}, nil, testEnvironmentAccessResolver{
		env: &models.Environment{
			BaseModel:   models.BaseModel{ID: "env-self"},
			Name:        "Self Target",
			AccessToken: &token,
		},
	}, &config.Config{}))

	huma.Register(api, huma.Operation{
		OperationID: "secure",
		Method:      http.MethodGet,
		Path:        "/secure",
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, func(ctx context.Context, _ *secureInput) (*secureOutput, error) {
		user, ok := GetCurrentUserFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "environment:env-self", user.ID)
		require.Equal(t, "Self Target", user.Username)

		resp := &secureOutput{}
		resp.Body.UserID = user.ID
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	req.Header.Set("X-API-Key", token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "environment:env-self")
}
