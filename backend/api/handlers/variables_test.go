package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/labstack/echo/v5"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVariableRoutesRequireTheirExactPermissions(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		permission string
	}{
		{name: "list", method: http.MethodGet, path: "/api/variables", permission: authz.PermVariablesRead},
		{name: "create", method: http.MethodPost, path: "/api/variables", body: `{"key":"API_URL","value":"https://example.test","allEnvironments":true}`, permission: authz.PermVariablesCreate},
		{name: "update", method: http.MethodPut, path: "/api/variables/variable-1", body: `{}`, permission: authz.PermVariablesUpdate},
		{name: "delete", method: http.MethodDelete, path: "/api/variables/variable-1", permission: authz.PermVariablesDelete},
		{name: "sync", method: http.MethodPost, path: "/api/variables/sync", permission: authz.PermVariablesSync},
		{name: "sync status", method: http.MethodGet, path: "/api/variables/sync-status", permission: authz.PermVariablesRead},
	}

	allVariablePermissions := []string{
		authz.PermVariablesRead,
		authz.PermVariablesCreate,
		authz.PermVariablesUpdate,
		authz.PermVariablesDelete,
		authz.PermVariablesSync,
	}

	for _, tt := range tests {
		t.Run(tt.name+" rejects other grants", func(t *testing.T) {
			permissions := authz.NewPermissionSet()
			permissions.AddGlobal(authz.PermTemplatesRead, authz.PermTemplatesCreate, authz.PermTemplatesUpdate, authz.PermTemplatesDelete)
			for _, permission := range allVariablePermissions {
				if permission != tt.permission {
					permissions.AddGlobal(permission)
				}
			}

			router, api := newPermissionGatingRouterInternal(t, permissions)
			RegisterVariables(api, nil, nil)
			response := performVariableRequestInternal(router, tt.method, tt.path, tt.body)

			require.Equal(t, http.StatusForbidden, response.Code)
			require.Contains(t, response.Body.String(), "permission denied: "+tt.permission)
		})

		t.Run(tt.name+" allows exact grant", func(t *testing.T) {
			permissions := authz.NewPermissionSet()
			permissions.AddGlobal(tt.permission)

			router, api := newPermissionGatingRouterInternal(t, permissions)
			variableService, environmentService := setupVariableHandlerServicesInternal(t)
			RegisterVariables(api, variableService, environmentService)
			response := performVariableRequestInternal(router, tt.method, tt.path, tt.body)

			require.NotEqual(t, http.StatusForbidden, response.Code)
			require.NotContains(t, response.Body.String(), "permission denied")
		})
	}
}

func TestMaterializedVariableRoutesRequireSudo(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "get", method: http.MethodGet},
		{name: "put", method: http.MethodPut, body: `{"variables":[{"key":"TOKEN","value":"plaintext"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name+" rejects human admin", func(t *testing.T) {
			permissions := authz.NewPermissionSet()
			permissions.AddGlobal(authz.AllPermissions()...)
			router, api := newPermissionGatingRouterInternal(t, permissions)
			RegisterMaterializedVariables(api, nil, nil)

			response := performVariableRequestInternal(router, tt.method, "/api/environments/0/templates/variables", tt.body)

			require.Equal(t, http.StatusForbidden, response.Code)
			require.Contains(t, response.Body.String(), "agent authentication required")
		})

		t.Run(tt.name+" allows agent", func(t *testing.T) {
			router, api := newPermissionGatingRouterInternal(t, authz.SudoPermissionSet())
			variableService, environmentService := setupVariableHandlerServicesInternal(t)
			RegisterMaterializedVariables(api, variableService, environmentService)

			response := performVariableRequestInternal(router, tt.method, "/api/environments/0/templates/variables", tt.body)

			require.Equal(t, http.StatusOK, response.Code)
		})
	}
}

func setupVariableHandlerServicesInternal(t *testing.T) (*services.VariableService, *services.EnvironmentService) {
	t.Helper()

	databaseConnection, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "variables.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, databaseConnection.AutoMigrate(
		&models.GlobalVariable{},
		&models.Environment{},
		&models.KVEntry{},
		&models.SettingVariable{},
	))
	databaseDB := &database.DB{DB: databaseConnection}
	settingsService, err := services.NewSettingsService(context.Background(), databaseDB)
	require.NoError(t, err)
	require.NoError(t, settingsService.UpdateSetting(context.Background(), "projectsDirectory", t.TempDir()))
	environmentService := services.NewEnvironmentService(databaseDB, nil, nil, nil, settingsService, nil)
	variableService := services.NewVariableService(databaseDB, environmentService, settingsService, services.NewKVService(databaseDB))
	return variableService, environmentService
}

func performVariableRequestInternal(router *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
