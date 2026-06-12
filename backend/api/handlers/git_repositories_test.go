package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

// TestGitRepositoryHandlers_PermissionGating verifies that the mutating
// git-repository operations are gated by their RequirePermission middleware:
// a caller without the required permission gets 403 before the handler runs.
//
// We exercise the full Huma operation stack (registration → middleware → handler)
// so the same path that runs in production is what's under test.
func TestGitRepositoryHandlers_PermissionGating(t *testing.T) {
	tests := []struct {
		name string
		// register installs one operation on the API with its real middleware.
		register func(api huma.API)
		method   string
		path     string
	}{
		{
			name: "create repository",
			register: func(api huma.API) {
				huma.Register(api, huma.Operation{
					OperationID: "test-create-git-repo",
					Method:      http.MethodPost,
					Path:        "/customize/git-repositories",
					Middlewares: humamw.RequirePermission(api, authz.PermGitReposCreate),
				}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
					t.Fatal("handler must not run when permission is missing")
					return nil, nil
				})
			},
			method: http.MethodPost,
			path:   "/api/customize/git-repositories",
		},
		{
			name: "update repository",
			register: func(api huma.API) {
				huma.Register(api, huma.Operation{
					OperationID: "test-update-git-repo",
					Method:      http.MethodPut,
					Path:        "/customize/git-repositories/{id}",
					Middlewares: humamw.RequirePermission(api, authz.PermGitReposUpdate),
				}, func(_ context.Context, _ *struct {
					ID string `path:"id"`
				}) (*struct{}, error) {
					t.Fatal("handler must not run when permission is missing")
					return nil, nil
				})
			},
			method: http.MethodPut,
			path:   "/api/customize/git-repositories/repo-1",
		},
		{
			name: "delete repository",
			register: func(api huma.API) {
				huma.Register(api, huma.Operation{
					OperationID: "test-delete-git-repo",
					Method:      http.MethodDelete,
					Path:        "/customize/git-repositories/{id}",
					Middlewares: humamw.RequirePermission(api, authz.PermGitReposDelete),
				}, func(_ context.Context, _ *struct {
					ID string `path:"id"`
				}) (*struct{}, error) {
					t.Fatal("handler must not run when permission is missing")
					return nil, nil
				})
			},
			method: http.MethodDelete,
			path:   "/api/customize/git-repositories/repo-1",
		},
		{
			name: "test repository",
			register: func(api huma.API) {
				huma.Register(api, huma.Operation{
					OperationID: "test-test-git-repo",
					Method:      http.MethodPost,
					Path:        "/customize/git-repositories/{id}/test",
					Middlewares: humamw.RequirePermission(api, authz.PermGitReposTest),
				}, func(_ context.Context, _ *struct {
					ID string `path:"id"`
				}) (*struct{}, error) {
					t.Fatal("handler must not run when permission is missing")
					return nil, nil
				})
			},
			method: http.MethodPost,
			path:   "/api/customize/git-repositories/repo-1/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := echo.New()
			api := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

			// Attach an empty (deny-all) permission set, simulating an
			// authenticated caller who lacks git-repository permissions.
			api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
				next(huma.WithContext(ctx, context.WithValue(ctx.Context(), humamw.ContextKeyUserPermissions, authz.NewPermissionSet())))
			})

			tt.register(api)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Contains(t, rec.Body.String(), "permission denied")
		})
	}
}

// Compile-time assertion that GitRepositoryService is the type the production
// handler expects; keeps this test honest if the field is ever renamed.
var _ = (*GitRepositoryHandler)(&GitRepositoryHandler{repoService: &services.GitRepositoryService{}})
