package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

type staticPermissionResolverInternal struct {
	ps *authz.PermissionSet
}

func (r staticPermissionResolverInternal) ResolvePermissions(_ context.Context, _ *models.User) (*authz.PermissionSet, error) {
	return r.ps, nil
}

func (r staticPermissionResolverInternal) ResolveApiKeyPermissions(_ context.Context, _ string) (*authz.PermissionSet, error) {
	return r.ps, nil
}

func mintAuthBridgeTestTokenInternal(t *testing.T, userSvc *services.UserService, sessionSvc *services.SessionService, jwtSecret string, userID string) string {
	t.Helper()

	_, err := userSvc.CreateUser(context.Background(), &models.User{
		BaseModel: models.BaseModel{ID: userID},
		Username:  userID,
	})
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _, err := sessionSvc.CreateSession(context.Background(), userID, exp, auth.SessionMeta{})
	require.NoError(t, err)

	claims := jwt.MapClaims{
		"jti":      userID,
		"sub":      "access",
		"iat":      time.Now().Unix(),
		"exp":      exp.Unix(),
		"sid":      session.ID,
		"user_id":  userID,
		"username": userID,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
	require.NoError(t, err)
	return token
}

func TestNewAuthBridge_AcceptsEnvironmentAccessTokenViaAPIKey(t *testing.T) {
	token := "env-access-token"
	router := echo.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"ApiKeyAuth": {
			Type: "apiKey",
			In:   "header",
			Name: "X-API-Key",
		},
	}

	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewAuthBridge(api, &services.AuthService{}, nil, nil, testEnvironmentAccessResolver{
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

		ps, ok := PermissionsFromContext(ctx)
		require.True(t, ok)
		require.True(t, ps.Allows(authz.PermContainersStart, "env-self"))
		require.False(t, ps.Allows(authz.PermContainersStart, "env-other"))
		require.False(t, ps.Allows(authz.PermUsersList, ""))
		require.False(t, ps.IsGlobalAdmin())

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

func TestNewAuthBridge_UsesBearerWhenLoopbackProxySendsEnvironmentAccessToken(t *testing.T) {
	db := setupAuthMiddlewareTestDBInternal(t)
	userSvc := services.NewUserService(db)
	sessionSvc := services.NewSessionService(db)

	jwtSecret := "test-secret-please-do-not-use-in-prod"
	authSvc := services.NewAuthService(userSvc, nil, nil, sessionSvc, nil, jwtSecret, &config.Config{JWTRefreshExpiry: 24 * time.Hour})
	bearerToken := mintAuthBridgeTestTokenInternal(t, userSvc, sessionSvc, jwtSecret, "u-loopback")

	ps := authz.NewPermissionSet()
	ps.AddEnv("0", authz.PermContainersStart)

	envToken := "remote-env-access-token"
	router := echo.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
		"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}

	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewAuthBridge(api, authSvc, nil, staticPermissionResolverInternal{ps: ps}, testEnvironmentAccessResolver{
		env: &models.Environment{
			BaseModel:   models.BaseModel{ID: "remote-env"},
			Name:        "Remote Env",
			AccessToken: &envToken,
		},
	}, &config.Config{}))

	huma.Register(api, huma.Operation{
		OperationID: "loopback-start",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{cid}/start",
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: RequirePermission(api, authz.PermContainersStart),
	}, func(ctx context.Context, _ *struct {
		ID  string `path:"id"`
		CID string `path:"cid"`
	}) (*secureOutput, error) {
		user, ok := GetCurrentUserFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "u-loopback", user.ID)

		resp := &secureOutput{}
		resp.Body.UserID = user.ID
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/0/containers/c/start", nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("X-API-Key", envToken)
	req.Header.Set("X-Arcane-Agent-Token", envToken)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "u-loopback")
}

// A valid API key presented to a BearerAuth-only operation must be rejected:
// the bridge only attempts API-key auth when the operation declares ApiKeyAuth.
// This is the gate that makes personal-key create/delete session-only.
func TestNewAuthBridge_RejectsApiKeyOnBearerOnlyOperation(t *testing.T) {
	router := echo.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
		"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}

	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewAuthBridge(api, &services.AuthService{}, nil, nil, nil, &config.Config{}))

	huma.Register(api, huma.Operation{
		OperationID: "bearer-only",
		Method:      http.MethodPost,
		Path:        "/bearer-only",
		Security:    []map[string][]string{{"BearerAuth": {}}},
	}, func(ctx context.Context, _ *secureInput) (*secureOutput, error) {
		t.Fatal("handler must not be reached with API key auth")
		return &secureOutput{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/bearer-only", nil)
	req.Header.Set("X-API-Key", "arc_whatever-valid-or-not-never-consulted")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

type testOperationProvider struct {
	operation *huma.Operation
}

func (p testOperationProvider) Operation() *huma.Operation {
	return p.operation
}

func TestParseSecurityRequirements(t *testing.T) {
	router := echo.New()
	apiGroup := router.Group("/api")
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Security = []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}
	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)

	testCases := []struct {
		name     string
		security []map[string][]string
		expected securityRequirements
	}{
		{
			name:     "nil operation security inherits top-level auth",
			security: nil,
			expected: securityRequirements{
				isRequired: true,
				bearerAuth: true,
				apiKeyAuth: true,
			},
		},
		{
			name:     "explicit empty security stays public",
			security: []map[string][]string{},
			expected: securityRequirements{},
		},
		{
			name: "explicit dual auth stays protected",
			security: []map[string][]string{
				{"BearerAuth": {}},
				{"ApiKeyAuth": {}},
			},
			expected: securityRequirements{
				isRequired: true,
				bearerAuth: true,
				apiKeyAuth: true,
			},
		},
		{
			name: "explicit api key auth stays api-key-only",
			security: []map[string][]string{
				{"ApiKeyAuth": {}},
			},
			expected: securityRequirements{
				isRequired: true,
				apiKeyAuth: true,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, parseSecurityRequirementsInternal(api, testOperationProvider{
				operation: &huma.Operation{Security: testCase.security},
			}))
		})
	}
}

func setupAuthMiddlewareTestDBInternal(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}, &models.User{}, &models.UserSession{}))
	return &database.DB{DB: db}
}

func TestNewAuthBridge_OpportunisticAuthOnPublicRoute(t *testing.T) {
	db := setupAuthMiddlewareTestDBInternal(t)
	userSvc := services.NewUserService(db)
	sessionSvc := services.NewSessionService(db)

	jwtSecret := "test-secret-please-do-not-use-in-prod"
	cfg := &config.Config{JWTRefreshExpiry: 24 * time.Hour}
	authSvc := services.NewAuthService(userSvc, nil, nil, sessionSvc, nil, jwtSecret, cfg)

	_, err := userSvc.CreateUser(context.Background(), &models.User{
		BaseModel: models.BaseModel{ID: "u-logout"},
		Username:  "logouttest",
	})
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _, err := sessionSvc.CreateSession(context.Background(), "u-logout", exp, auth.SessionMeta{})
	require.NoError(t, err)

	claims := jwt.MapClaims{
		"jti":      "u-logout",
		"sub":      "access",
		"iat":      time.Now().Unix(),
		"exp":      exp.Unix(),
		"sid":      session.ID,
		"user_id":  "u-logout",
		"username": "logouttest",
		"roles":    []string{"user"},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
	require.NoError(t, err)

	router := echo.New()
	apiGroup := router.Group("/api")
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
	}
	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewAuthBridge(api, authSvc, nil, nil, nil, &config.Config{}))

	var sawSessionID string
	huma.Register(api, huma.Operation{
		OperationID: "public-with-session",
		Method:      http.MethodPost,
		Path:        "/public",
		Security:    []map[string][]string{},
	}, func(ctx context.Context, _ *secureInput) (*secureOutput, error) {
		if sid, ok := GetCurrentSessionIDFromContext(ctx); ok {
			sawSessionID = sid
		}
		return &secureOutput{}, nil
	})

	t.Run("populates session ID when valid token presented", func(t *testing.T) {
		sawSessionID = ""
		req := httptest.NewRequest(http.MethodPost, "/api/public", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, session.ID, sawSessionID)
	})

	t.Run("succeeds with no token", func(t *testing.T) {
		sawSessionID = ""
		req := httptest.NewRequest(http.MethodPost, "/api/public", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "", sawSessionID)
	})

	t.Run("succeeds with invalid token (does not block)", func(t *testing.T) {
		sawSessionID = ""
		req := httptest.NewRequest(http.MethodPost, "/api/public", nil)
		req.Header.Set("Authorization", "Bearer not-a-valid-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "", sawSessionID)
	})
}

// After a self-update the app version changes and old access tokens fail the version
// check. That must be RECOVERABLE (the refresh path rotates the token), so the
// middleware returns a refreshable 401 and must NOT clear the auth cookies — otherwise
// the user is logged out on every update.
func TestNewAuthBridge_VersionMismatchIsRecoverable(t *testing.T) {
	db := setupAuthMiddlewareTestDBInternal(t)
	userSvc := services.NewUserService(db)
	sessionSvc := services.NewSessionService(db)

	jwtSecret := "test-secret-please-do-not-use-in-prod"
	cfg := &config.Config{JWTRefreshExpiry: 24 * time.Hour}
	authSvc := services.NewAuthService(userSvc, nil, nil, sessionSvc, nil, jwtSecret, cfg)

	_, err := userSvc.CreateUser(context.Background(), &models.User{
		BaseModel: models.BaseModel{ID: "u-ver"},
		Username:  "vertest",
	})
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	session, _, err := sessionSvc.CreateSession(context.Background(), "u-ver", exp, auth.SessionMeta{})
	require.NoError(t, err)

	// An empty appVersion omits the claim, which passes the version check (no pin).
	mintToken := func(appVersion string) string {
		claims := jwt.MapClaims{
			"jti":      "u-ver",
			"sub":      "access",
			"iat":      time.Now().Unix(),
			"exp":      exp.Unix(),
			"sid":      session.ID,
			"user_id":  "u-ver",
			"username": "vertest",
		}
		if appVersion != "" {
			claims["app_version"] = appVersion
		}
		token, signErr := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
		require.NoError(t, signErr)
		return token
	}

	router := echo.New()
	apiGroup := router.Group("/api")
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
	}
	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewAuthBridge(api, authSvc, nil, nil, nil, &config.Config{}))

	huma.Register(api, huma.Operation{
		OperationID: "protected",
		Method:      http.MethodGet,
		Path:        "/protected",
		Security:    []map[string][]string{{"BearerAuth": {}}},
	}, func(_ context.Context, _ *secureInput) (*secureOutput, error) {
		return &secureOutput{}, nil
	})

	t.Run("version mismatch returns a recoverable 401 without clearing cookies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
		req.Header.Set("Authorization", "Bearer "+mintToken("v0.0.0-stale"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Body.String(), "Application has been updated")
		// The frontend recovers via refresh; clearing the cookies here would log the
		// user out on every self-update.
		require.Empty(t, rec.Header().Values("Set-Cookie"))
	})

	t.Run("token without a version pin still authenticates", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
		req.Header.Set("Authorization", "Bearer "+mintToken(""))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}
