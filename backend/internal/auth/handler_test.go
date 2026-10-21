package auth

import (
	"bytes"
	"context"
	"crypto/mldsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
)

type secureInput struct{}

type secureOutput struct {
	Body struct {
		UserID string `json:"userId"`
	} `json:"body"`
}

type testEnvironmentAccessResolver struct {
	env *environment.Environment
}

func TestNewHumaMiddleware_AcceptsEnvironmentAccessTokenViaAPIKey(t *testing.T) {
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
	api.UseMiddleware(NewHumaMiddleware(api, &AuthService{}, nil, nil, testEnvironmentAccessResolver{
		env: &environment.Environment{
			ID:          "env-self",
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
		localUser, ok := user.CurrentUserFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "environment:env-self", localUser.ID)
		require.Equal(t, "Self Target", localUser.Username)

		ps, ok := middleware.PermissionsFromContext(ctx)
		require.True(t, ok)
		require.True(t, ps.Allows(authz.PermContainersStart, "env-self"))
		require.False(t, ps.Allows(authz.PermContainersStart, "env-other"))
		require.False(t, ps.Allows(authz.PermUsersList, ""))
		require.False(t, ps.IsGlobalAdmin())

		resp := &secureOutput{}
		resp.Body.UserID = localUser.ID
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/secure", http.NoBody)
	req.Header.Set("X-API-Key", token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "environment:env-self")
}

func TestNewHumaMiddleware_UsesBearerWhenLoopbackProxySendsEnvironmentAccessToken(t *testing.T) {
	db := setupAuthMiddlewareTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	sessionSvc := session.NewSessionService(db)

	signingKey := newTestSigningKey()
	authSvc := NewAuthService(userSvc, nil, nil, sessionSvc, nil, nil, nil, &config.Config{JWTRefreshExpiry: 24 * time.Hour}).WithSigningKey(signingKey)
	bearerToken := mintHumaMiddlewareTestToken(t, userSvc, sessionSvc, signingKey, "u-loopback")

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
	api.UseMiddleware(NewHumaMiddleware(api, authSvc, nil, staticPermissionResolver{ps: ps}, testEnvironmentAccessResolver{
		env: &environment.Environment{
			ID:          "remote-env",
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
		Middlewares: middleware.RequirePermission(api, authz.PermContainersStart),
	}, func(ctx context.Context, _ *struct {
		ID  string `path:"id"`
		CID string `path:"cid"`
	},
	) (*secureOutput, error) {
		localUser, ok := user.CurrentUserFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "u-loopback", localUser.ID)

		resp := &secureOutput{}
		resp.Body.UserID = localUser.ID
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/0/containers/c/start", http.NoBody)
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
func TestNewHumaMiddleware_RejectsApiKeyOnBearerOnlyOperation(t *testing.T) {
	router := echo.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
		"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}

	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewHumaMiddleware(api, &AuthService{}, nil, nil, nil, &config.Config{}))

	huma.Register(api, huma.Operation{
		OperationID: "bearer-only",
		Method:      http.MethodPost,
		Path:        "/bearer-only",
		Security:    []map[string][]string{{"BearerAuth": {}}},
	}, func(ctx context.Context, _ *secureInput) (*secureOutput, error) {
		require.FailNow(t, "handler must not be reached with API key auth")
		return &secureOutput{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/bearer-only", http.NoBody)
	req.Header.Set("X-API-Key", "arc_whatever-valid-or-not-never-consulted")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
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
			require.Equal(t, testCase.expected, parseSecurityRequirements(api, testOperationProvider{
				operation: &huma.Operation{Security: testCase.security},
			}))
		})
	}
}

func TestNewHumaMiddleware_OpportunisticAuthOnPublicRoute(t *testing.T) {
	db := setupAuthMiddlewareTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	sessionSvc := session.NewSessionService(db)

	signingKey := newTestSigningKey()
	cfg := &config.Config{JWTRefreshExpiry: 24 * time.Hour}
	authSvc := NewAuthService(userSvc, nil, nil, sessionSvc, nil, nil, nil, cfg).WithSigningKey(signingKey)

	_, err := userSvc.CreateUser(t.Context(), &user.User{
		ID:       "u-logout",
		Username: "logouttest",
	})
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, _, err := sessionSvc.CreateSession(t.Context(), "u-logout", exp, types.SessionMeta{})
	require.NoError(t, err)

	claims := map[string]any{
		"jti":      "u-logout",
		"sub":      "access",
		"iat":      time.Now().Unix(),
		"exp":      exp.Unix(),
		"sid":      localSession.ID,
		"user_id":  "u-logout",
		"username": "logouttest",
		"roles":    []string{"user"},
	}
	token := signJWXToken(t, signingKey, claims)

	router := echo.New()
	apiGroup := router.Group("/api")
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
	}
	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewHumaMiddleware(api, authSvc, nil, nil, nil, &config.Config{}))

	var sawSessionID string
	huma.Register(api, huma.Operation{
		OperationID: "public-with-session",
		Method:      http.MethodPost,
		Path:        "/public",
		Security:    []map[string][]string{},
	}, func(ctx context.Context, _ *secureInput) (*secureOutput, error) {
		if sid, ok := middleware.GetCurrentSessionIDFromContext(ctx); ok {
			sawSessionID = sid
		}
		return &secureOutput{}, nil
	})

	t.Run("populates session ID when valid token presented", func(t *testing.T) {
		sawSessionID = ""
		req := httptest.NewRequest(http.MethodPost, "/api/public", http.NoBody)
		req.AddCookie(&http.Cookie{Name: cookie.InsecureTokenCookieName, Value: token})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, localSession.ID, sawSessionID)
	})

	t.Run("succeeds with no token", func(t *testing.T) {
		sawSessionID = ""
		req := httptest.NewRequest(http.MethodPost, "/api/public", http.NoBody)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, sawSessionID)
	})

	t.Run("succeeds with invalid token (does not block)", func(t *testing.T) {
		sawSessionID = ""
		req := httptest.NewRequest(http.MethodPost, "/api/public", http.NoBody)
		req.Header.Set("Authorization", "Bearer not-a-valid-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, sawSessionID)
	})
}

// After a self-update the app version changes and old access tokens fail the version
// check. That must be RECOVERABLE (the refresh path rotates the token), so the
// middleware returns a refreshable 401 and must NOT clear the auth cookies — otherwise
// the user is logged out on every update.
func TestNewHumaMiddleware_VersionMismatchIsRecoverable(t *testing.T) {
	db := setupAuthMiddlewareTestDB(t)
	userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
	sessionSvc := session.NewSessionService(db)

	signingKey := newTestSigningKey()
	browserSigningKey := bytes.Repeat([]byte{0x24}, browserSessionSigningKeySize)
	cfg := &config.Config{JWTRefreshExpiry: 24 * time.Hour}
	authSvc := NewAuthService(userSvc, nil, nil, sessionSvc, nil, nil, nil, cfg).WithSigningKey(signingKey)
	authSvc.browserSigningKey = browserSigningKey

	_, err := userSvc.CreateUser(t.Context(), &user.User{
		ID:       "u-ver",
		Username: "vertest",
	})
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, _, err := sessionSvc.CreateSession(t.Context(), "u-ver", exp, types.SessionMeta{})
	require.NoError(t, err)

	// An empty appVersion omits the claim, which passes the version check (no pin).
	mintToken := func(appVersion string) string {
		builder := jwt.NewBuilder().
			JwtID("browser-"+appVersion).
			Subject(browserTokenSubject).
			IssuedAt(time.Now()).
			Expiration(exp).
			Claim(claimSessionID, localSession.ID).
			Claim(claimUserID, "u-ver")
		if appVersion != "" {
			builder.Claim(claimAppVersion, appVersion)
		}
		token, buildErr := builder.Build()
		require.NoError(t, buildErr)
		signed, buildErr := jwt.Sign(token, jwt.WithKey(jwa.HS512(), browserSigningKey))
		require.NoError(t, buildErr)
		return string(signed)
	}

	router := echo.New()
	apiGroup := router.Group("/api")
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
	}
	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewHumaMiddleware(api, authSvc, nil, nil, nil, &config.Config{}))

	huma.Register(api, huma.Operation{
		OperationID: "protected",
		Method:      http.MethodGet,
		Path:        "/protected",
		Security:    []map[string][]string{{"BearerAuth": {}}},
	}, func(_ context.Context, _ *secureInput) (*secureOutput, error) {
		return &secureOutput{}, nil
	})

	t.Run("version mismatch returns a recoverable 401 without clearing cookies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/protected", http.NoBody)
		req.AddCookie(&http.Cookie{Name: cookie.InsecureTokenCookieName, Value: mintToken("v0.0.0-stale")})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Body.String(), "Application has been updated")
		// The frontend recovers via refresh; clearing the cookies here would log the
		// user out on every self-update.
		require.Empty(t, rec.Header().Values("Set-Cookie"))
	})

	t.Run("token without a version pin still authenticates", func(t *testing.T) {
		token := mintToken("")
		req := httptest.NewRequest(http.MethodGet, "/api/protected", http.NoBody)
		req.AddCookie(&http.Cookie{Name: cookie.InsecureTokenCookieName, Value: token})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		req = httptest.NewRequest(http.MethodGet, "/api/protected", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestNewHumaMiddleware_AgentAuthAppliesForwardedIconCatalog(t *testing.T) {
	const agentToken = "agent-token"
	router := echo.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}

	api := humaecho.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(NewHumaMiddleware(api, &AuthService{}, nil, nil, nil, &config.Config{
		AgentMode:  true,
		AgentToken: agentToken,
	}))

	huma.Register(api, huma.Operation{
		OperationID: "secure-agent-icon-catalog",
		Method:      http.MethodGet,
		Path:        "/secure-agent-icon-catalog",
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
	}, func(ctx context.Context, _ *secureInput) (*secureOutput, error) {
		localUser, ok := user.CurrentUserFromContext(ctx)
		require.True(t, ok)
		require.NotNil(t, localUser.Preferences.IconCatalog)
		require.Equal(t, "dashboard-icons", *localUser.Preferences.IconCatalog)

		resp := &secureOutput{}
		resp.Body.UserID = localUser.ID
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/secure-agent-icon-catalog", http.NoBody)
	req.Header.Set(middleware.HeaderAgentToken, agentToken)
	req.Header.Set(middleware.HeaderIconCatalog, "dashboard-icons")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func (r testEnvironmentAccessResolver) ResolveEnvironmentByAccessToken(_ context.Context, token string) (*environment.Environment, error) {
	if r.env != nil && r.env.AccessToken != nil && *r.env.AccessToken == token {
		return r.env, nil
	}
	return nil, context.Canceled
}

func setupAuthMiddlewareTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}, &user.User{}, &session.UserSession{}))
	return &database.DB{DB: db}
}

func mintHumaMiddlewareTestToken(t *testing.T, userSvc *user.UserService, sessionSvc *session.SessionService, signingKey *mldsa.PrivateKey, userID string) string {
	t.Helper()

	_, err := userSvc.CreateUser(t.Context(), &user.User{
		ID:       userID,
		Username: userID,
	})
	require.NoError(t, err)

	exp := time.Now().Add(5 * time.Minute)
	localSession, _, err := sessionSvc.CreateSession(t.Context(), userID, exp, types.SessionMeta{})
	require.NoError(t, err)

	claims := map[string]any{
		"jti":      userID,
		"sub":      "access",
		"iat":      time.Now().Unix(),
		"exp":      exp.Unix(),
		"sid":      localSession.ID,
		"user_id":  userID,
		"username": userID,
	}
	return signJWXToken(t, signingKey, claims)
}

type staticPermissionResolver struct {
	ps *authz.PermissionSet
}

func (r staticPermissionResolver) ResolvePermissions(_ context.Context, _ string) (*authz.PermissionSet, error) {
	return r.ps, nil
}

func (r staticPermissionResolver) ResolveApiKeyPermissions(_ context.Context, _ string) (*authz.PermissionSet, error) {
	return r.ps, nil
}

type testOperationProvider struct {
	operation *huma.Operation
}

func (p testOperationProvider) Operation() *huma.Operation {
	return p.operation
}

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
	user *user.User
	key  *apikey.ApiKey
}

func (v testApiKeyValidator) ValidateApiKeyWithID(_ context.Context, rawKey string) (*user.User, *apikey.ApiKey, error) {
	if rawKey == "valid-key" {
		return v.user, v.key, nil
	}
	return nil, nil, context.Canceled
}

// testPermissionResolver returns distinguishable sets so tests can tell which
// resolution path the middleware took: user roles vs per-key grants.
type testPermissionResolver struct{}

func (testPermissionResolver) ResolvePermissions(_ context.Context, _ string) (*authz.PermissionSet, error) {
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
	owner := &user.User{ID: userID, Username: "owner"}

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
						user: owner,
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

			req := httptest.NewRequest(http.MethodGet, "/secure", http.NoBody)
			req.Header.Set("X-API-Key", "valid-key")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}

	t.Run("browser cookie uses owner role permissions", func(t *testing.T) {
		ctx := t.Context()
		db := setupAuthMiddlewareTestDB(t)
		userSvc := user.NewUserService(db, nil, session.RevokeAllUserSessionsExceptInDB)
		sessionSvc := session.NewSessionService(db)
		settingsSvc, err := newSettingsServiceForAuthTest(t, ctx, db)
		require.NoError(t, err)
		authSvc := newTestAuthService()
		authSvc.userService = userSvc
		authSvc.settingsService = settingsSvc
		authSvc.sessionService = sessionSvc

		browserUser := &user.User{ID: "browser-owner", Username: "browser-owner"}
		_, err = userSvc.CreateUser(ctx, browserUser)
		require.NoError(t, err)
		expiresAt := time.Now().Add(time.Hour)
		browserSession, refreshJTI, err := sessionSvc.CreateSession(ctx, browserUser.ID, expiresAt, types.SessionMeta{})
		require.NoError(t, err)
		tokenPair, err := authSvc.buildTokenPair(ctx, browserUser, browserSession, refreshJTI)
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

		req := httptest.NewRequest(http.MethodGet, "/secure", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+tokenPair.BrowserToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)

		req = httptest.NewRequest(http.MethodGet, "/secure", http.NoBody)
		req.AddCookie(&http.Cookie{Name: cookie.InsecureTokenCookieName, Value: tokenPair.BrowserToken})
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestAuthMiddleware_ManagerAuthEnvironmentAccessToken(t *testing.T) {
	token := "env-access-token"
	cases := []struct {
		name          string
		header        string
		adminRequired bool
		wantStatus    int
	}{
		{name: "agent token ordinary route", header: "X-Arcane-Agent-Token", wantStatus: http.StatusOK},
		{name: "API key ordinary route", header: "X-API-Key", wantStatus: http.StatusOK},
		{name: "agent token admin route", header: "X-Arcane-Agent-Token", adminRequired: true, wantStatus: http.StatusForbidden},
		{name: "API key admin route", header: "X-API-Key", adminRequired: true, wantStatus: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := echo.New()
			authMiddleware := NewAuthMiddleware(nil, &config.Config{}).
				WithEnvironmentAccessTokenResolver(testEnvironmentTokenResolver{
					env: &environment.Environment{
						ID:          "env-self",
						Name:        "Self Target",
						AccessToken: &token,
					},
				})
			if tc.adminRequired {
				authMiddleware = authMiddleware.WithAdminRequired()
			}
			router.Use(authMiddleware.Add())
			called := false
			router.GET("/secure", func(c *echo.Context) error {
				called = true
				currentUser := c.Get("currentUser")
				require.NotNil(t, currentUser)

				authenticatedUser, ok := currentUser.(*user.User)
				require.True(t, ok)
				require.Equal(t, "environment:env-self", authenticatedUser.ID)
				require.Equal(t, "Self Target", authenticatedUser.Username)
				require.Equal(t, "environment_access_token", c.Get("authMethod"))

				ps, ok := c.Get("userPermissions").(*authz.PermissionSet)
				require.True(t, ok)
				require.True(t, ps.Allows(authz.PermContainersStart, "env-self"))
				require.False(t, ps.Allows(authz.PermContainersStart, "env-other"))
				require.False(t, ps.Allows(authz.PermUsersList, ""))
				require.False(t, ps.IsGlobalAdmin())

				return c.JSON(http.StatusOK, map[string]any{"userId": authenticatedUser.ID})
			})

			req := httptest.NewRequest(http.MethodGet, "/secure", http.NoBody)
			req.Header.Set(tc.header, token)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, !tc.adminRequired, called)
			if tc.adminRequired {
				require.JSONEq(t, `{"statusCode":0,"code":"FORBIDDEN","message":"You don't have permission to access this resource"}`, rec.Body.String())
			} else {
				require.Contains(t, rec.Body.String(), "environment:env-self")
			}
		})
	}
}
