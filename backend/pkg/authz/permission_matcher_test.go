package authz

import (
	"slices"
	"testing"
)

func TestPermissionMatcherLookupExactAndWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")
	m.Add("GET", "/containers/{containerId}", "containers:read")

	if permissions, ok := m.Lookup("POST", "/containers/abc123/start"); !ok || !slices.Equal(permissions, []string{"containers:start"}) {
		t.Fatalf("expected containers:start, got %v ok=%v", permissions, ok)
	}
	if permissions, ok := m.Lookup("GET", "/containers/abc123"); !ok || !slices.Equal(permissions, []string{"containers:read"}) {
		t.Fatalf("expected containers:read, got %v ok=%v", permissions, ok)
	}
}

func TestPermissionMatcherStaticBeatsWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	// Order intentionally puts the wildcard route first to prove specificity,
	// not registration order, decides the winner.
	m.Add("GET", "/containers/{containerId}", "containers:read")
	m.Add("GET", "/containers/counts", "containers:list")

	if permissions, ok := m.Lookup("GET", "/containers/counts"); !ok || !slices.Equal(permissions, []string{"containers:list"}) {
		t.Fatalf("expected static route to win with containers:list, got %v ok=%v", permissions, ok)
	}
	if permissions, ok := m.Lookup("GET", "/containers/xyz"); !ok || !slices.Equal(permissions, []string{"containers:read"}) {
		t.Fatalf("expected wildcard route containers:read, got %v ok=%v", permissions, ok)
	}
}

func TestPermissionMatcherMethodAndLengthMismatch(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")

	if _, ok := m.Lookup("DELETE", "/containers/abc/start"); ok {
		t.Fatal("expected no match for wrong method")
	}
	if _, ok := m.Lookup("POST", "/containers/abc/start/extra"); ok {
		t.Fatal("expected no match for longer path")
	}
	if _, ok := m.Lookup("POST", "/containers/abc"); ok {
		t.Fatal("expected no match for shorter path")
	}
}

func TestPermissionMatcherNormalizesEchoParamsAndStripsQuery(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/volumes/:volumeName/browse", "volumes:browse")

	if permissions, ok := m.Lookup("GET", "/volumes/data/browse?path=/etc"); !ok || !slices.Equal(permissions, []string{"volumes:browse"}) {
		t.Fatalf("expected volumes:browse with echo param + query string, got %v ok=%v", permissions, ok)
	}
}

func TestPermissionMatcherReturnsEveryAcceptedPermission(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/operations", "projects:list", "containers:list", "image-updates:read")

	permissions, ok := m.Lookup("GET", "/operations")
	if !ok {
		t.Fatal("expected operations route to be found")
	}
	if !slices.Equal(permissions, []string{"projects:list", "containers:list", "image-updates:read"}) {
		t.Fatalf("unexpected accepted permissions: %v", permissions)
	}
}

func TestPermissionMatcherPublicRoute(t *testing.T) {
	m := NewPermissionMatcher()
	m.AddPublic("GET", "/settings/public")

	permissions, ok := m.Lookup("GET", "/settings/public")
	if !ok {
		t.Fatal("expected public route to be found")
	}
	if len(permissions) != 0 {
		t.Fatalf("expected no permissions for public route, got %v", permissions)
	}
}

func TestPermissionMatcherUnmappedReturnsNotFound(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/containers", "containers:list")

	if _, ok := m.Lookup("GET", "/images"); ok {
		t.Fatal("expected unmapped path to return not found")
	}
}
