package auth

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"

	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	usersvc "github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	authtypes "github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type testEnvironmentTokenResolver struct {
	env *environment.Environment
}

func (r testEnvironmentTokenResolver) ResolveEnvironmentByAccessToken(_ context.Context, token string) (*environment.Environment, error) {
	if r.env != nil && r.env.AccessToken != nil && *r.env.AccessToken == token {
		return r.env, nil
	}
	return nil, ErrInvalidEnvironmentAccessTokenForTest
}

var ErrInvalidEnvironmentAccessTokenForTest = context.Canceled

type testApiKeyValidator struct {
	user *common.User
	key  *apikey.ApiKey
}

func (v testApiKeyValidator) ValidateApiKeyWithID(_ context.Context, rawKey string) (*common.User, *apikey.ApiKey, error) {
	if rawKey == "valid-key" {
		return v.user, v.key, nil
	}
	return nil, nil, context.Canceled
}

// testPermissionResolver returns distinguishable sets so tests can tell which
// resolution path the middleware took: user roles vs per-key grants.
type testPermissionResolver struct{}

func (testPermissionResolver) ResolvePermissions(_ context.Context, _ *common.User) (*authz.PermissionSet, error) {
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
	user := &common.User{ID: userID, Username: "owner"}

	cases := []struct {
		name        string
		kind        string
		wantAllowed string
		wantDenied  string
	}{
		{name: "personal key inherits owner role permissions", kind: apikey.ApiKeyKindPersonal, wantAllowed: "containers:list", wantDenied: "images:list"},
		{name: "scoped key limited to its own grants", kind: apikey.ApiKeyKindScoped, wantAllowed: "images:list", wantDenied: "containers:list"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := echo.New()
			router.Use(
				NewAuthMiddleware(nil, &config.Config{}).
					WithApiKeyValidator(testApiKeyValidator{
						user: user,
						key:  &apikey.ApiKey{ID: "key-1", Kind: tc.kind, UserID: &userID},
					}).
					WithPermissionResolver(testPermissionResolver{}).
					Add(),
			)
			router.GET("/secure", func(c *echo.Context) error {
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

	t.Run("browser cookie uses owner role permissions", func(t *testing.T) {
		ctx := context.Background()
		db := setupAuthMiddlewareTestDBInternal(t)
		userSvc := usersvc.NewUserService(db)
		sessionSvc := session.NewSessionService(db)
		settingsSvc, err := newSettingsServiceForAuthTestInternal(t, ctx, db)
		require.NoError(t, err)
		authSvc := newTestAuthService()
		authSvc.userService = userSvc
		authSvc.settingsService = settingsSvc
		authSvc.sessionService = sessionSvc

		browserUser := &common.User{ID: "browser-owner", Username: "browser-owner"}
		_, err = userSvc.CreateUser(ctx, browserUser)
		require.NoError(t, err)
		expiresAt := time.Now().Add(time.Hour)
		browserSession, refreshJTI, err := sessionSvc.CreateSession(ctx, browserUser.ID, expiresAt, authtypes.SessionMeta{})
		require.NoError(t, err)
		tokenPair, err := authSvc.buildTokenPairInternal(ctx, browserUser, browserSession, refreshJTI)
		require.NoError(t, err)

		router := echo.New()
		router.Use(
			NewAuthMiddleware(authSvc, &config.Config{}).
				WithPermissionResolver(testPermissionResolver{}).
				Add(),
		)
		router.GET("/secure", func(c *echo.Context) error {
			ps, ok := c.Get("userPermissions").(*authz.PermissionSet)
			require.True(t, ok)
			require.True(t, ps.Allows("containers:list", ""))
			return c.NoContent(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/secure", nil)
		req.Header.Set("Authorization", "Bearer "+tokenPair.BrowserToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)

		req = httptest.NewRequest(http.MethodGet, "/secure", nil)
		req.AddCookie(&http.Cookie{Name: cookie.InsecureTokenCookieName, Value: tokenPair.BrowserToken})
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestAuthMiddleware_ManagerAuthAcceptsEnvironmentAccessTokenViaAPIKey(t *testing.T) {
	token := "env-access-token"
	router := echo.New()
	router.Use(
		NewAuthMiddleware(nil, &config.Config{}).
			WithEnvironmentAccessTokenResolver(testEnvironmentTokenResolver{
				env: &environment.Environment{
					ID:          "env-self",
					Name:        "Self Target",
					AccessToken: &token,
				},
			}).
			Add(),
	)
	router.GET("/secure", func(c *echo.Context) error {
		currentUser := c.Get("currentUser")
		require.NotNil(t, currentUser)

		user, ok := currentUser.(*common.User)
		require.True(t, ok)
		require.Equal(t, "environment:env-self", user.ID)
		require.Equal(t, "Self Target", user.Username)
		require.Equal(t, "environment_access_token", c.Get("authMethod"))

		ps, ok := c.Get("userPermissions").(*authz.PermissionSet)
		require.True(t, ok)
		require.True(t, ps.Allows(authz.PermContainersStart, "env-self"))
		require.False(t, ps.Allows(authz.PermContainersStart, "env-other"))
		require.False(t, ps.Allows(authz.PermUsersList, ""))
		require.False(t, ps.IsGlobalAdmin())

		return c.JSON(http.StatusOK, map[string]any{"userId": user.ID})
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("X-API-Key", token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "environment:env-self")
}
