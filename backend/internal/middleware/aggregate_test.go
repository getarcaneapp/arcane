package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

type scopeTestInput struct {
	ID string `path:"id"`
}

func newScopeTestRouter(t *testing.T, limit int) (*echo.Echo, *[]*httpx.EnvironmentScope) {
	t.Helper()
	router := echo.New()
	group := router.Group("/api")
	matcher := authz.NewPermissionMatcher()
	group.Use(NewEnvironmentScopeMiddleware(func(_ context.Context, id string) (base.EnvironmentRef, error) {
		return base.EnvironmentRef{ID: id, Name: id}, nil
	}, matcher, func(context.Context) int { return limit }))
	api := humaecho.NewWithGroup(router, group, huma.DefaultConfig("test", "1.0.0"))

	handled := []*httpx.EnvironmentScope{}
	handler := func(ctx context.Context, _ *scopeTestInput) (*struct{}, error) {
		scope, _ := httpx.EnvironmentScopeFromContext(ctx)
		handled = append(handled, scope)
		return &struct{}{}, nil
	}
	aggregate := map[string]any{authz.MetaEnvironmentAggregate: true}
	huma.Register(api, huma.Operation{OperationID: "list-containers", Method: http.MethodGet, Path: "/environments/{id}/containers", Metadata: aggregate}, handler)
	huma.Register(api, huma.Operation{OperationID: "create-container", Method: http.MethodPost, Path: "/environments/{id}/containers", Metadata: aggregate}, handler)
	huma.Register(api, huma.Operation{OperationID: "list-images", Method: http.MethodGet, Path: "/environments/{id}/images"}, handler)
	matcher.CollectFromHumaAPI(api)
	return router, &handled
}

func serveScopeTestRequest(router *echo.Echo, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(method, target, http.NoBody))
	return recorder
}

func TestEnvironmentScopeMiddlewarePassesSingleEnvironmentThrough(t *testing.T) {
	router, handled := newScopeTestRouter(t, 64)

	recorder := serveScopeTestRequest(router, http.MethodGet, "/api/environments/env-1/containers")

	require.Less(t, recorder.Code, http.StatusBadRequest)
	require.Equal(t, []*httpx.EnvironmentScope{nil}, *handled)
}

func TestEnvironmentScopeMiddlewareAttachesDeduplicatedScope(t *testing.T) {
	router, handled := newScopeTestRouter(t, 64)

	recorder := serveScopeTestRequest(router, http.MethodGet, "/api/environments/0,env-a,0,env-b/containers?search=web")

	require.Less(t, recorder.Code, http.StatusBadRequest)
	require.Len(t, *handled, 1)
	scope := (*handled)[0]
	require.NotNil(t, scope)
	require.Equal(t, []string{"0", "env-a", "env-b"}, scope.IDs)
	require.Same(t, router, scope.Handler)
	require.NotNil(t, scope.Resolve)
	require.Equal(t, "/api/environments/0,env-a,0,env-b/containers", scope.Request.URL.Path)
	_, captured := httpx.EnvironmentScopeFromContext(scope.Request.Context())
	require.False(t, captured, "the captured request must not carry the scope")
}

func TestEnvironmentScopeMiddlewareRejectsInvalidScopes(t *testing.T) {
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("env-%d", i)
	}
	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "unsupported route", method: http.MethodGet, target: "/api/environments/a,b/images"},
		{name: "non-GET method", method: http.MethodPost, target: "/api/environments/a,b/containers"},
		{name: "empty component", method: http.MethodGet, target: "/api/environments/a,,b/containers"},
		{name: "too many environments", method: http.MethodGet, target: "/api/environments/" + strings.Join(tooMany, ",") + "/containers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, handled := newScopeTestRouter(t, 64)

			recorder := serveScopeTestRequest(router, tt.method, tt.target)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"error"`)
			require.Empty(t, *handled)
		})
	}
}

func TestEnvironmentScopeMiddlewareEnforcesConfiguredLimit(t *testing.T) {
	router, handled := newScopeTestRouter(t, 3)

	rejected := serveScopeTestRequest(router, http.MethodGet, "/api/environments/a,b,c,d/containers")
	accepted := serveScopeTestRequest(router, http.MethodGet, "/api/environments/a,b,c,a/containers")

	require.Equal(t, http.StatusBadRequest, rejected.Code)
	require.Contains(t, rejected.Body.String(), "limited to 3 environments")
	require.Less(t, accepted.Code, http.StatusBadRequest)
	require.Len(t, *handled, 1)
	require.Equal(t, []string{"a", "b", "c"}, (*handled)[0].IDs)
}
