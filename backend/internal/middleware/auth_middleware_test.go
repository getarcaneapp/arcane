package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
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

type testApiKeyValidator struct {
	user *models.User
	key  *models.ApiKey
}

func (v testApiKeyValidator) ValidateApiKeyWithID(_ context.Context, rawKey string) (*models.User, *models.ApiKey, error) {
	if rawKey == "valid-key" {
		return v.user, v.key, nil
	}
	return nil, nil, context.Canceled
}

// testPermissionResolver returns distinguishable sets so tests can tell which
// resolution path the middleware took: user roles vs per-key grants.
type testPermissionResolver struct{}

func (testPermissionResolver) ResolvePermissions(_ context.Context, _ *models.User) (*authz.PermissionSet, error) {
	ps := authz.NewPermissionSet()
	ps.AddGlobal("containers:list")
	return ps, nil
}

func (testPermissionResolver) ResolveApiKeyPermissions(_ context.Context, _ string) (*authz.PermissionSet, error) {
	ps := authz.NewPermissionSet()
	ps.AddGlobal("images:list")
	return ps, nil
}

func TestAuthMiddleware_ManagerAuthResolvesPermissionsByKeyKind(t *testing.T) {
	userID := "key-owner"
	user := &models.User{BaseModel: models.BaseModel{ID: userID}, Username: "owner"}

	cases := []struct {
		name        string
		kind        string
		wantAllowed string
		wantDenied  string
	}{
		{name: "personal key inherits owner role permissions", kind: models.ApiKeyKindPersonal, wantAllowed: "containers:list", wantDenied: "images:list"},
		{name: "scoped key limited to its own grants", kind: models.ApiKeyKindScoped, wantAllowed: "images:list", wantDenied: "containers:list"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := echo.New()
			router.Use(
				NewAuthMiddleware(nil, &config.Config{}).
					WithApiKeyValidator(testApiKeyValidator{
						user: user,
						key:  &models.ApiKey{BaseModel: models.BaseModel{ID: "key-1"}, Kind: tc.kind, UserID: &userID},
					}).
					WithPermissionResolver(testPermissionResolver{}).
					Add(),
			)
			router.GET("/secure", func(c echo.Context) error {
				ps, ok := c.Get("userPermissions").(*authz.PermissionSet)
				require.True(t, ok)
				require.True(t, ps.Allows(tc.wantAllowed, ""))
				require.False(t, ps.Allows(tc.wantDenied, ""))
				return c.JSON(http.StatusOK, map[string]any{"ok": true})
			})

			req := httptest.NewRequest(http.MethodGet, "/secure", nil)
			req.Header.Set("X-API-Key", "valid-key")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

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
