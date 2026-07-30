package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

func TestRequirePermission_RejectsCallerMissingPermission(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		// Caller has containers:list but NOT containers:start.
		ps := authz.NewPermissionSet()
		ps.AddEnv("env-1", authz.PermContainersList)
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserPermissions, ps)))
	})

	huma.Register(api, huma.Operation{
		OperationID: "guarded-start",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{cid}/start",
		Middlewares: RequirePermission(api, authz.PermContainersStart),
	}, func(_ context.Context, _ *struct {
		ID  string `path:"id"`
		CID string `path:"cid"`
	}) (*struct{}, error) {
		t.Fatal("handler must not run when permission is missing")
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-1/containers/c/start", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "permission denied: containers:start")
}

func TestRequirePermission_AllowsCallerWithPermissionOnEnv(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		ps := authz.NewPermissionSet()
		ps.AddEnv("env-1", authz.PermContainersStart)
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserPermissions, ps)))
	})

	type out struct {
		Body struct {
			Success bool `json:"success"`
		}
	}
	handlerRan := false
	huma.Register(api, huma.Operation{
		OperationID: "guarded-start-allow",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{cid}/start",
		Middlewares: RequirePermission(api, authz.PermContainersStart),
	}, func(_ context.Context, _ *struct {
		ID  string `path:"id"`
		CID string `path:"cid"`
	}) (*out, error) {
		handlerRan = true
		return &out{Body: struct {
			Success bool `json:"success"`
		}{Success: true}}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-1/containers/c/start", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, handlerRan)
}

func TestRequirePermission_EnvScopedDoesNotLeakAcrossEnvs(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		ps := authz.NewPermissionSet()
		// Permission scoped to env-1; request will target env-2.
		ps.AddEnv("env-1", authz.PermContainersStart)
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserPermissions, ps)))
	})

	huma.Register(api, huma.Operation{
		OperationID: "guarded-start-other-env",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/containers/{cid}/start",
		Middlewares: RequirePermission(api, authz.PermContainersStart),
	}, func(_ context.Context, _ *struct {
		ID  string `path:"id"`
		CID string `path:"cid"`
	}) (*struct{}, error) {
		t.Fatal("handler must not run when permission is scoped to a different env")
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-2/containers/c/start", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequirePermissionEnvironmentGrantCannotManageGlobalNotificationsInternal(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		ps := authz.NewPermissionSet()
		ps.AddEnv("env-1", authz.PermNotificationsManage)
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserPermissions, ps)))
	})

	huma.Register(api, huma.Operation{
		OperationID: "guarded-global-notifications",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/settings",
		Middlewares: RequirePermission(api, authz.PermNotificationsManage),
	}, func(_ context.Context, _ *struct {
		ID string `path:"id"`
	}) (*struct{}, error) {
		t.Fatal("handler must not run for an environment-scoped grant on global notification settings")
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environments/env-1/notifications/settings", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequirePermission_SudoCallerAllowed(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserPermissions, authz.SudoPermissionSet())))
	})

	handlerRan := false
	huma.Register(api, huma.Operation{
		OperationID: "guarded-sudo",
		Method:      http.MethodDelete,
		Path:        "/users/{userId}",
		Middlewares: RequirePermission(api, authz.PermUsersDelete),
	}, func(_ context.Context, _ *struct {
		UserID string `path:"userId"`
	}) (*struct{}, error) {
		handlerRan = true
		return &struct{}{}, nil
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/users/u-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, handlerRan)
}

func TestRequireSudoRejectsHumanAdminAndAllowsAgent(t *testing.T) {
	tests := []struct {
		name       string
		permission *authz.PermissionSet
		wantStatus int
		wantRan    bool
	}{
		{name: "human admin", permission: func() *authz.PermissionSet {
			ps := authz.NewPermissionSet()
			ps.AddGlobal(authz.AllPermissions()...)
			return ps
		}(), wantStatus: http.StatusForbidden},
		{name: "agent sudo", permission: authz.SudoPermissionSet(), wantStatus: http.StatusNoContent, wantRan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := echo.New()
			api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))
			api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
				next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserPermissions, tt.permission)))
			})

			handlerRan := false
			huma.Register(api, huma.Operation{
				OperationID: "materialized-variables-" + tt.name,
				Method:      http.MethodGet,
				Path:        "/environments/{id}/templates/variables",
				Middlewares: RequireSudo(api),
			}, func(_ context.Context, _ *struct {
				ID string `path:"id"`
			}) (*struct{}, error) {
				handlerRan = true
				return &struct{}{}, nil
			})

			req := httptest.NewRequest(http.MethodGet, "/api/environments/0/templates/variables", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantRan, handlerRan)
		})
	}
}
