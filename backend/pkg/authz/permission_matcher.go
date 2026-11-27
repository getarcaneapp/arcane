package authz

import (
	"net/url"
	"strings"

	"github.com/samber/mo"

	kit "kit.getarcane.app"
)

type (
	PermissionMatcher struct {
		aggregates map[string]struct{}
		routes     []permRoute
	}

	permRoute struct {
		actionPerms map[string]string
		method      string
		perm        string
		actionParam string
		segments    []string // literal segment, or "*" for a path parameter
	}
)

func NewPermissionMatcher() *PermissionMatcher {
	return &PermissionMatcher{
		aggregates: make(map[string]struct{}),
	}
}

func (m *PermissionMatcher) Add(method, pathTemplate, perm string) {
	m.routes = append(m.routes, permRoute{
		method:   strings.ToUpper(method),
		segments: templateSegments(pathTemplate),
		perm:     perm,
	})
}

func (m *PermissionMatcher) AddActions(method, pathTemplate, param string, perms map[string]string) {
	m.routes = append(m.routes, permRoute{
		method:      strings.ToUpper(method),
		segments:    templateSegments(pathTemplate),
		actionParam: param,
		actionPerms: perms,
	})
}

func (m *PermissionMatcher) Lookup(method, suffixPath string, query url.Values) mo.Option[string] {
	method = strings.ToUpper(method)

	best, found := m.best(method, pathSegments(suffixPath))
	if !found {
		return mo.None[string]()
	}
	if best.actionParam == "" {
		return mo.Some(best.perm)
	}
	perm, ok := best.actionPerms[query.Get(best.actionParam)]
	if !ok {
		return mo.None[string]()
	}
	return mo.Some(perm)
}

// ActionParam reports the query parameter that selects the permission of the
// best matching route, so callers can tell "this route takes an action, and the
// value is unknown" apart from "this route is unmapped".
func (m *PermissionMatcher) ActionParam(method, suffixPath string) string {
	best, found := m.best(strings.ToUpper(method), pathSegments(suffixPath))
	if !found {
		return ""
	}
	return best.actionParam
}

func (m *PermissionMatcher) best(method string, reqSegs []string) (permRoute, bool) {
	bestScore := -1
	var best permRoute
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
			best = r
			found = true
		}
	}
	return best, found
}

func (m *PermissionMatcher) SupportsEnvironmentAggregation(method, suffix string) bool {
	_, ok := m.aggregates[strings.ToUpper(method)+" "+suffix]
	return ok
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func templateSegments(t string) []string {
	segs := pathSegments(t)
	for i, s := range segs {
		if strings.HasPrefix(s, ":") || (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) {
			segs[i] = "*"
		}
	}
	return segs
}

func WorkspaceRequiredPermissions[T any](conditions PermissionConditions, changes []T, operation func(T) string) ([]string, bool) {
	required := make([]string, 0, len(changes))
	for _, change := range changes {
		perms := conditions.Required("fileChanges.operation", operation(change))
		if len(perms) == 0 {
			return nil, false
		}
		required = append(required, perms...)
	}
	return kit.Unique(required), true
}
