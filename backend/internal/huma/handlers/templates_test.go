package handlers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	humamiddleware "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFetchTemplateRegistryRequiresAuthentication(t *testing.T) {
	router := newTemplateFetchTestRouter(t, http.DefaultClient)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/fetch?url=http://example.com/registry.json", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "Unauthorized")
}

func TestFetchTemplateRegistryAuthenticatedSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Registry","description":"Demo","version":"1.0.0","author":"Arcane","templates":[]}`))
	}))
	defer server.Close()

	client, registryURL := makeTemplateFetchRemote(t, server)
	router := newTemplateFetchTestRouter(t, client)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/fetch?url="+url.QueryEscape(registryURL), nil)
	req.Header.Set("Authorization", "Bearer "+makeTemplateFetchToken(t, "test-secret", "user-1", "alice"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":true`)
	require.Contains(t, rec.Body.String(), `"name":"Registry"`)
}

func TestFetchTemplateRegistryAuthenticatedUnsafeTargetIsSanitized(t *testing.T) {
	router := newTemplateFetchTestRouter(t, http.DefaultClient)

	req := httptest.NewRequest(http.MethodGet, "/api/templates/fetch?url="+url.QueryEscape("http://127.0.0.1:8080/registry.json"), nil)
	req.Header.Set("Authorization", "Bearer "+makeTemplateFetchToken(t, "test-secret", "user-1", "alice"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Failed to fetch registry")
	require.NotContains(t, rec.Body.String(), "127.0.0.1")
	require.NotContains(t, rec.Body.String(), "connection refused")
	require.NotContains(t, rec.Body.String(), "Invalid JSON response")
}

func TestTemplateReadEndpointsRequireAuthentication(t *testing.T) {
	router := newTemplateFetchTestRouter(t, http.DefaultClient)

	testCases := []struct {
		name string
		path string
	}{
		{name: "list templates", path: "/api/templates"},
		{name: "list all templates", path: "/api/templates/all"},
		{name: "get template", path: "/api/templates/test-template"},
		{name: "get template content", path: "/api/templates/test-template/content"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Contains(t, rec.Body.String(), "Unauthorized")
		})
	}
}

func TestHealthEndpointRemainsPublic(t *testing.T) {
	router := newTemplateFetchTestRouter(t, http.DefaultClient)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "UP")
}

func newTemplateFetchTestRouter(t *testing.T, httpClient *http.Client) *gin.Engine {
	t.Helper()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalDir))
	})

	db, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))

	databaseDB := &database.DB{DB: db}
	userService := services.NewUserService(databaseDB)
	_, err = userService.CreateUser(context.Background(), &models.User{
		BaseModel: models.BaseModel{ID: "user-1"},
		Username:  "alice",
		Roles:     models.StringSlice{"admin"},
	})
	require.NoError(t, err)

	authService := services.NewAuthService(userService, nil, nil, "test-secret", &config.Config{})
	templateService := services.NewTemplateService(context.Background(), nil, httpClient, nil)

	router := gin.New()
	apiGroup := router.Group("/api")

	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {
			Type:   "http",
			Scheme: "bearer",
		},
		"ApiKeyAuth": {
			Type: "apiKey",
			In:   "header",
			Name: "X-API-Key",
		},
	}
	humaConfig.Security = []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}

	api := humagin.NewWithGroup(router, apiGroup, humaConfig)
	api.UseMiddleware(humamiddleware.NewAuthBridge(api, authService, nil, nil, &config.Config{}))
	RegisterHealth(api)
	RegisterTemplates(api, templateService, nil)

	return router
}

func makeTemplateFetchToken(t *testing.T, secret string, userID, username string) string {
	t.Helper()

	claims := services.UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        userID,
			Subject:   "access",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
		UserID:     userID,
		Username:   username,
		Roles:      []string{"admin"},
		AppVersion: config.Version,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	return signed
}

func makeTemplateFetchRemote(t *testing.T, server *httptest.Server) (*http.Client, string) {
	t.Helper()

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	listenerAddr := server.Listener.Addr().String()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{}
		return dialer.DialContext(ctx, network, listenerAddr)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	return client, "http://93.184.216.34:" + parsedURL.Port() + "/registry.json"
}
