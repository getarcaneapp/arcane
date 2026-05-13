package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/auth"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
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
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
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
	authSvc := services.NewAuthService(userSvc, nil, nil, sessionSvc, jwtSecret, cfg)

	_, err := userSvc.CreateUser(context.Background(), &models.User{
		BaseModel: models.BaseModel{ID: "u-logout"},
		Username:  "logouttest",
		Roles:     models.StringSlice{"user"},
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
	api.UseMiddleware(NewAuthBridge(api, authSvc, nil, nil, &config.Config{}))

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
