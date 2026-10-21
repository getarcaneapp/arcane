package authz

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
)

func TestPermissionMatcherLookupExactAndWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")
	m.Add("GET", "/containers/{containerId}", "containers:read")
	{

		perm, ok := m.Lookup("POST", "/containers/abc123/start", nil).Get()
		require.False(t, !ok || perm != "containers:start",
			"expected containers:start, got %q ok=%v", perm, ok)
	}
	{

		perm, ok := m.Lookup("GET", "/containers/abc123", nil).Get()
		require.False(t, !ok || perm != "containers:read",
			"expected containers:read, got %q ok=%v", perm, ok)
	}
}

func TestPermissionMatcherStaticBeatsWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	// Order intentionally puts the wildcard route first to prove specificity,
	// not registration order, decides the winner.
	m.Add("GET", "/containers/{containerId}", "containers:read")
	m.Add("GET", "/containers/counts", "containers:list")
	{

		perm, ok := m.Lookup("GET", "/containers/counts", nil).Get()
		require.False(t, !ok || perm != "containers:list",
			"expected static route to win with containers:list, got %q ok=%v", perm, ok)
	}
	{

		perm, ok := m.Lookup("GET", "/containers/xyz", nil).Get()
		require.False(t, !ok || perm != "containers:read",
			"expected wildcard route containers:read, got %q ok=%v", perm, ok)
	}
}

func TestPermissionMatcherMethodAndLengthMismatch(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")
	{

		_, ok := m.Lookup("DELETE", "/containers/abc/start", nil).Get()
		require.False(t, ok,
			"expected no match for wrong method")
	}
	{

		_, ok := m.Lookup("POST", "/containers/abc/start/extra", nil).Get()
		require.False(t, ok,
			"expected no match for longer path")
	}
	{

		_, ok := m.Lookup("POST", "/containers/abc", nil).Get()
		require.False(t, ok,
			"expected no match for shorter path")
	}
}

func TestPermissionMatcherNormalizesEchoParamsAndIgnoresQuery(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/volumes/:volumeName/workspace", "volumes:read")
	{

		perm, ok := m.Lookup("GET", "/volumes/data/workspace", url.Values{"path": {"/etc"}}).Get()
		require.False(t, !ok || perm != "volumes:read",
			"expected volumes:read with echo param + query string, got %q ok=%v", perm, ok)
	}
}

func TestPermissionMatcherPublicRoute(t *testing.T) {
	m := NewPermissionMatcher()
	m.AddPublic("GET", "/settings/public")

	perm, ok := m.Lookup("GET", "/settings/public", nil).Get()

	require.True(t, ok,
		"expected public route to be found")

	require.Empty(t, perm,
		"expected empty permission for public route, got %q", perm)
}

func TestPermissionMatcherUnmappedReturnsNotFound(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/containers", "containers:list")
	{

		_, ok := m.Lookup("GET", "/images", nil).Get()
		require.False(t, ok,
			"expected unmapped path to return not found")
	}
}

func TestPermissionMatcherCollectsEnvironmentAggregation(t *testing.T) {
	_, api := humatest.New(t)
	handler := func(context.Context, *struct {
		ID string `path:"id"`
	},
	) (*struct{}, error) {
		return &struct{}{}, nil
	}
	aggregate := map[string]any{MetaRequiredPermission: "containers:list", MetaEnvironmentAggregate: true}
	huma.Register(api, huma.Operation{OperationID: "list", Method: http.MethodGet, Path: "/environments/{id}/containers", Metadata: aggregate}, handler)
	huma.Register(api, huma.Operation{OperationID: "create", Method: http.MethodPost, Path: "/environments/{id}/projects", Metadata: aggregate}, handler)
	huma.Register(api, huma.Operation{
		OperationID: "images",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images",
		Metadata:    map[string]any{MetaRequiredPermission: "images:list"},
	}, handler)

	m := NewPermissionMatcher()
	m.CollectFromHumaAPI(api)

	require.True(t, m.SupportsEnvironmentAggregation(http.MethodGet, "/containers"))
	require.False(t, m.SupportsEnvironmentAggregation(http.MethodPost, "/projects"), "only GET operations aggregate")
	require.False(t, m.SupportsEnvironmentAggregation(http.MethodGet, "/images"), "operations must opt in")
	require.False(t, m.SupportsEnvironmentAggregation(http.MethodGet, "/containers/abc"), "suffix must match exactly")
	perm, ok := m.Lookup(http.MethodGet, "/containers", nil).Get()
	require.True(t, ok)
	require.Equal(t, "containers:list", perm)
}

func TestPermissionMatcherActionPermissions(t *testing.T) {
	perms := map[string]string{
		"start": "containers:start",
		"kill":  "containers:kill",
	}

	t.Run("manual", func(t *testing.T) {
		m := NewPermissionMatcher()
		m.AddActions("POST", "/containers/{containerId}", "action", perms)
		m.Add("POST", "/containers/counts", "containers:list")

		perm, ok := m.Lookup("POST", "/containers/abc", url.Values{"action": {"kill"}}).Get()
		require.True(t, ok)
		require.Equal(t, "containers:kill", perm)

		_, ok = m.Lookup("POST", "/containers/abc", url.Values{"action": {"explode"}}).Get()
		require.False(t, ok, "unknown action must deny")

		_, ok = m.Lookup("POST", "/containers/abc", nil).Get()
		require.False(t, ok, "missing action must deny")

		perm, ok = m.Lookup("POST", "/containers/counts", nil).Get()
		require.True(t, ok)
		require.Equal(t, "containers:list", perm)
	})

	t.Run("collected from huma", func(t *testing.T) {
		_, api := humatest.New(t)
		huma.Register(api, huma.Operation{
			OperationID: "container-action",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/containers/{containerId}",
			Metadata: map[string]any{
				MetaActionParam:       "action",
				MetaActionPermissions: perms,
			},
		}, func(context.Context, *struct {
			ID          string `path:"id"`
			ContainerID string `path:"containerId"`
		},
		) (*struct{}, error) {
			return &struct{}{}, nil
		})

		m := NewPermissionMatcher()
		m.CollectFromHumaAPI(api)

		perm, ok := m.Lookup(http.MethodPost, "/containers/abc", url.Values{"action": {"start"}}).Get()
		require.True(t, ok)
		require.Equal(t, "containers:start", perm)

		_, ok = m.Lookup(http.MethodPost, "/containers/abc", url.Values{"action": {"remove"}}).Get()
		require.False(t, ok)
	})
}
