package authz

import (
	"net/url"
	"strings"

	"github.com/samber/mo"

	"kit.getarcane.app"

	"getarcane.dev/types/v3"
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
	return &PermissionMatcher{}
}

func (m *PermissionMatcher) Add(method, pathTemplate, perm string) {
	m.routes = append(m.routes, permRoute{
		method:   strings.ToUpper(method),
		segments: templateSegments(pathTemplate),
		perm:     perm,
	})
}

func (m *PermissionMatcher) AddPublic(method, pathTemplate string) {
	m.Add(method, pathTemplate, "")
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
	reqSegs := pathSegments(suffixPath)
	method = strings.ToUpper(method)

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

func VolumeWorkspaceRequiredPermissions(changes []types.VolumeWorkspaceFileChange) ([]string, bool) {
	required := make([]string, 0, 3)
	for _, change := range changes {
		perms := VolumeWorkspaceConditions.Required("fileChanges.operation", change.Operation)
		if len(perms) == 0 {
			return nil, false
		}
		required = append(required, perms...)
	}
	return kit.Unique(required), true
}
