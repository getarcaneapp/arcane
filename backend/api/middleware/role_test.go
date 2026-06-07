package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestRequireAdmin_RejectsNonAdmin(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserIsAdmin, false)))
	})

	huma.Register(api, huma.Operation{
		OperationID: "guarded",
		Method:      http.MethodGet,
		Path:        "/guarded",
		Middlewares: RequireAdmin(api),
	}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
		t.Fatal("handler must not run for non-admin")
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/guarded", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "admin access required")
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	router := echo.New()
	api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), ContextKeyUserIsAdmin, true)))
	})

	type guardedAdminOutput struct {
		Body struct {
			Success bool `json:"success"`
		}
	}

	handlerRan := false
	huma.Register(api, huma.Operation{
		OperationID: "guarded-admin",
		Method:      http.MethodGet,
		Path:        "/guarded-admin",
		Middlewares: RequireAdmin(api),
	}, func(_ context.Context, _ *struct{}) (*guardedAdminOutput, error) {
		handlerRan = true
		return &guardedAdminOutput{
			Body: struct {
				Success bool `json:"success"`
			}{Success: true},
		}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/guarded-admin", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, handlerRan)
}
