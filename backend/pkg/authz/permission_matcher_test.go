package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissionMatcherLookupExactAndWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")
	m.Add("GET", "/containers/{containerId}", "containers:read")
	{

		perm, ok := m.Lookup("POST", "/containers/abc123/start").Get()
		require.False(t, !ok || perm != "containers:start",
			"expected containers:start, got %q ok=%v", perm, ok)
	}
	{

		perm, ok := m.Lookup("GET", "/containers/abc123").Get()
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

		perm, ok := m.Lookup("GET", "/containers/counts").Get()
		require.False(t, !ok || perm != "containers:list",
			"expected static route to win with containers:list, got %q ok=%v", perm, ok)
	}
	{

		perm, ok := m.Lookup("GET", "/containers/xyz").Get()
		require.False(t, !ok || perm != "containers:read",
			"expected wildcard route containers:read, got %q ok=%v", perm, ok)
	}

}

func TestPermissionMatcherMethodAndLengthMismatch(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")
	{

		_, ok := m.Lookup("DELETE", "/containers/abc/start").Get()
		require.False(t, ok,
			"expected no match for wrong method")
	}
	{

		_, ok := m.Lookup("POST", "/containers/abc/start/extra").Get()
		require.False(t, ok,
			"expected no match for longer path")
	}
	{

		_, ok := m.Lookup("POST", "/containers/abc").Get()
		require.False(t, ok,
			"expected no match for shorter path")
	}

}

func TestPermissionMatcherNormalizesEchoParamsAndStripsQuery(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/volumes/:volumeName/browse", "volumes:browse")
	{

		perm, ok := m.Lookup("GET", "/volumes/data/browse?path=/etc").Get()
		require.False(t, !ok || perm != "volumes:browse",
			"expected volumes:browse with echo param + query string, got %q ok=%v", perm, ok)
	}

}

func TestPermissionMatcherPublicRoute(t *testing.T) {
	m := NewPermissionMatcher()
	m.AddPublic("GET", "/settings/public")

	perm, ok := m.Lookup("GET", "/settings/public").Get()

	require.True(t, ok,
		"expected public route to be found")

	require.Empty(t, perm,
		"expected empty permission for public route, got %q", perm)

}

func TestPermissionMatcherUnmappedReturnsNotFound(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/containers", "containers:list")
	{

		_, ok := m.Lookup("GET", "/images").Get()
		require.False(t, ok,
			"expected unmapped path to return not found")
	}

}
