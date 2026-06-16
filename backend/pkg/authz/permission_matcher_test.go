package authz

import "testing"

func TestPermissionMatcherLookupExactAndWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("POST", "/containers/{containerId}/start", "containers:start")
	m.Add("GET", "/containers/{containerId}", "containers:read")

	if perm, ok := m.Lookup("POST", "/containers/abc123/start"); !ok || perm != "containers:start" {
		t.Fatalf("expected containers:start, got %q ok=%v", perm, ok)
	}
	if perm, ok := m.Lookup("GET", "/containers/abc123"); !ok || perm != "containers:read" {
		t.Fatalf("expected containers:read, got %q ok=%v", perm, ok)
	}
}

func TestPermissionMatcherStaticBeatsWildcard(t *testing.T) {
	m := NewPermissionMatcher()
	// Order intentionally puts the wildcard route first to prove specificity,
	// not registration order, decides the winner.
	m.Add("GET", "/containers/{containerId}", "containers:read")
	m.Add("GET", "/containers/counts", "containers:list")

	if perm, ok := m.Lookup("GET", "/containers/counts"); !ok || perm != "containers:list" {
		t.Fatalf("expected static route to win with containers:list, got %q ok=%v", perm, ok)
	}
	if perm, ok := m.Lookup("GET", "/containers/xyz"); !ok || perm != "containers:read" {
		t.Fatalf("expected wildcard route containers:read, got %q ok=%v", perm, ok)
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

	if perm, ok := m.Lookup("GET", "/volumes/data/browse?path=/etc"); !ok || perm != "volumes:browse" {
		t.Fatalf("expected volumes:browse with echo param + query string, got %q ok=%v", perm, ok)
	}
}

func TestPermissionMatcherPublicRoute(t *testing.T) {
	m := NewPermissionMatcher()
	m.AddPublic("GET", "/settings/public")

	perm, ok := m.Lookup("GET", "/settings/public")
	if !ok {
		t.Fatal("expected public route to be found")
	}
	if perm != "" {
		t.Fatalf("expected empty permission for public route, got %q", perm)
	}
}

func TestPermissionMatcherUnmappedReturnsNotFound(t *testing.T) {
	m := NewPermissionMatcher()
	m.Add("GET", "/containers", "containers:list")

	if _, ok := m.Lookup("GET", "/images"); ok {
		t.Fatal("expected unmapped path to return not found")
	}
}
