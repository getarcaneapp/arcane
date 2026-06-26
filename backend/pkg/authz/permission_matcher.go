package authz

import "strings"

// PermissionMatcher maps an HTTP method and environment-relative resource path
// to the permission required to perform it. It mirrors the per-operation
// RequirePermission checks enforced for the local environment so the remote
// environment proxy can enforce the same permission before forwarding a request
// to an agent (which itself runs with a sudo permission set and performs no
// authorization).
//
// Paths registered and looked up are the resource suffix AFTER the
// /environments/{id} prefix, e.g. "/containers/{containerId}/start" for the
// REST API or "/ws/containers/{containerId}/terminal" for WebSocket streams.
//
// The matcher is populated once during startup and is read-only afterwards, so
// concurrent Lookup calls during request serving need no synchronization.
type PermissionMatcher struct {
	routes []permRoute
}

type permRoute struct {
	method   string
	segments []string // literal segment, or "*" for a path parameter
	perm     string
}

// NewPermissionMatcher returns an empty matcher.
func NewPermissionMatcher() *PermissionMatcher {
	return &PermissionMatcher{}
}

// Add registers the permission required for method + pathTemplate. pathTemplate
// is the resource suffix after /environments/{id}. Path parameters may use
// either Huma's "{name}" or Echo's ":name" syntax; both are treated as
// single-segment wildcards.
func (m *PermissionMatcher) Add(method, pathTemplate, perm string) {
	m.routes = append(m.routes, permRoute{
		method:   strings.ToUpper(method),
		segments: templateSegmentsInternal(pathTemplate),
		perm:     perm,
	})
}

// AddPublic registers method + pathTemplate as a proxied route that requires no
// permission (an intentionally public endpoint). Lookup reports it as found
// with an empty permission string, which the proxy treats as "allow".
func (m *PermissionMatcher) AddPublic(method, pathTemplate string) {
	m.Add(method, pathTemplate, "")
}

// Lookup returns the permission required for method + suffixPath, where
// suffixPath is the resource path after /environments/{id} (for example
// "/containers/abc123/start"). When multiple templates match, the most specific
// one wins — a route with more literal (non-wildcard) segments is preferred, so
// static routes such as "/containers/counts" take precedence over
// parameterized routes such as "/containers/{containerId}".
//
// The boolean result reports whether any route matched. Callers should treat a
// false result as "deny" for proxied resource paths.
func (m *PermissionMatcher) Lookup(method, suffixPath string) (string, bool) {
	if i := strings.IndexByte(suffixPath, '?'); i >= 0 {
		suffixPath = suffixPath[:i]
	}
	reqSegs := pathSegmentsInternal(suffixPath)
	method = strings.ToUpper(method)

	bestScore := -1
	bestPerm := ""
	found := false
	for i := range m.routes {
		r := m.routes[i]
		if r.method != method || len(r.segments) != len(reqSegs) {
			continue
		}
		score := 0
		match := true
		for j, seg := range r.segments {
			if seg == "*" {
				continue
			}
			if seg != reqSegs[j] {
				match = false
				break
			}
			score++
		}
		if match && score > bestScore {
			bestScore = score
			bestPerm = r.perm
			found = true
		}
	}
	return bestPerm, found
}

// pathSegmentsInternal splits a path into its non-empty segments, ignoring
// leading and trailing slashes.
func pathSegmentsInternal(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// templateSegmentsInternal splits a path template into segments, normalizing
// any path parameter ("{name}" or ":name") to the "*" wildcard.
func templateSegmentsInternal(t string) []string {
	segs := pathSegmentsInternal(t)
	for i, s := range segs {
		if isParamSegmentInternal(s) {
			segs[i] = "*"
		}
	}
	return segs
}

func isParamSegmentInternal(s string) bool {
	if strings.HasPrefix(s, ":") {
		return true
	}
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}
