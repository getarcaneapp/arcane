package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type testEnvironmentTokenResolver struct {
	env *models.Environment
}

func (r testEnvironmentTokenResolver) ResolveEnvironmentByAccessToken(_ context.Context, token string) (*models.Environment, error) {
	if r.env != nil && r.env.AccessToken != nil && *r.env.AccessToken == token {
		return r.env, nil
	}
	return nil, ErrInvalidEnvironmentAccessTokenForTest
}

var ErrInvalidEnvironmentAccessTokenForTest = context.Canceled

func TestAuthMiddleware_ManagerAuthAcceptsEnvironmentAccessTokenViaAPIKey(t *testing.T) {
	token := "env-access-token"
	router := echo.New()
	router.Use(
		NewAuthMiddleware(nil, &config.Config{}).
			WithEnvironmentAccessTokenResolver(testEnvironmentTokenResolver{
				env: &models.Environment{
					BaseModel:   models.BaseModel{ID: "env-self"},
					Name:        "Self Target",
					AccessToken: &token,
				},
			}).
			Add(),
	)
	router.GET("/secure", func(c echo.Context) error {
		currentUser := c.Get("currentUser")
		require.NotNil(t, currentUser)

		user, ok := currentUser.(*models.User)
		require.True(t, ok)
		require.Equal(t, "environment:env-self", user.ID)
		require.Equal(t, "Self Target", user.Username)
		require.Equal(t, "environment_access_token", c.Get("authMethod"))

		return c.JSON(http.StatusOK, map[string]any{"userId": user.ID})
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("X-API-Key", token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "environment:env-self")
}
