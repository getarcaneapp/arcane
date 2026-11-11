package authz

import (
	"maps"
	"slices"
	"strings"
)

type (
	// Built once by the auth bridge and stashed in the request context.
	PermissionSet struct {
		Global map[string]struct{}
		PerEnv map[string]map[string]struct{}
		Sudo   bool
	}

	ForwardedPermissions struct {
		Global      []string `json:"global"`
		Environment []string `json:"environment"`
		GlobalAdmin bool     `json:"globalAdmin,omitzero"`
	}
)

func (ps *PermissionSet) Allows(perm, envID string) bool {
	if ps == nil {
		return false
	}
	if ps.Sudo {
		return true
	}
	if _, ok := ps.Global[perm]; ok {
		return true
	}
	if envID == "" {
		return false
	}
	if env, ok := ps.PerEnv[envID]; ok {
		if _, localOk := env[perm]; localOk {
			return true
		}
	}
	return false
}

func (ps *PermissionSet) AllowsAny(perm string) bool {
	if ps == nil {
		return false
	}
	if ps.Allows(perm, "") {
		return true
	}
	for envID := range ps.PerEnv {
		if ps.Allows(perm, envID) {
			return true
		}
	}
	return false
}

// AccessibleEnvironmentIDs returns nil when the caller may list every environment,
// otherwise the sorted IDs it holds at least one permission on.
func (ps *PermissionSet) AccessibleEnvironmentIDs() []string {
	switch {
	case ps == nil:
		return []string{}
	case ps.Allows(PermEnvironmentsList, ""):
		return nil
	default:
		// Never nil: callers treat nil as unrestricted, so a user without grants must get an empty list.
		ids := slices.AppendSeq(make([]string, 0, len(ps.PerEnv)), maps.Keys(ps.PerEnv))
		slices.Sort(ids)
		return ids
	}
}

func (ps *PermissionSet) IsGlobalAdmin() bool {
	if ps == nil {
		return false
	}
	if ps.Sudo {
		return true
	}
	for _, permission := range AllPermissions() {
		if _, ok := ps.Global[permission]; !ok {
			return false
		}
	}
	return true
}

func SudoPermissionSet() *PermissionSet {
	return &PermissionSet{Sudo: true}
}

func EnvironmentPermissionSet(envID string) *PermissionSet {
	ps := NewPermissionSet()
	if envID == "" {
		return ps
	}
	for _, p := range AllPermissions() {
		if IsEnvScoped(p) {
			ps.AddEnv(envID, p)
		}
	}
	return ps
}

func (ps *PermissionSet) ForwardedPermissions(envID string) ForwardedPermissions {
	if ps == nil {
		return ForwardedPermissions{}
	}
	return ForwardedPermissions{
		Global: slices.DeleteFunc(slices.Clone(orderedPermissions), func(perm string) bool {
			_, global := ps.Global[perm]
			return !global
		}),
		Environment: slices.DeleteFunc(slices.Clone(orderedPermissions), func(perm string) bool {
			_, global := ps.Global[perm]
			_, granted := ps.PerEnv[envID][perm]
			return global || !granted || !IsEnvScoped(perm)
		}),
	}
}

func (f ForwardedPermissions) PermissionSet(envID string) *PermissionSet {
	ps := NewPermissionSet()
	global := f.Global
	if f.GlobalAdmin {
		global = AllPermissions()
	}
	for _, perm := range global {
		if IsKnownPermission(perm) {
			ps.AddGlobal(perm)
		}
	}
	for _, perm := range f.Environment {
		if IsEnvScoped(perm) {
			ps.AddEnv(envID, perm)
		}
	}
	return ps
}

func NewPermissionSet() *PermissionSet {
	return &PermissionSet{
		Global: make(map[string]struct{}),
		PerEnv: make(map[string]map[string]struct{}),
	}
}

func (ps *PermissionSet) AddGlobal(perms ...string) {
	if ps.Global == nil {
		ps.Global = make(map[string]struct{})
	}
	for _, p := range perms {
		ps.Global[p] = struct{}{}
	}
}

func (ps *PermissionSet) AddEnv(envID string, perms ...string) {
	if envID == "" {
		ps.AddGlobal(perms...)
		return
	}
	if ps.PerEnv == nil {
		ps.PerEnv = make(map[string]map[string]struct{})
	}
	env, ok := ps.PerEnv[envID]
	if !ok {
		env = make(map[string]struct{})
		ps.PerEnv[envID] = env
	}
	for _, p := range perms {
		env[p] = struct{}{}
	}
}

func EnvIDFromPath(path string) string {
	path = strings.TrimPrefix(path, "/api")
	if !strings.HasPrefix(path, "/agents/") {
		return ""
	}
	rest := path[len("/agents/"):]
	before, _, ok := strings.Cut(rest, "/")
	if !ok {
		// Path is just /agents/<id> (no trailing segment) — this is
		// not an env-scoped operation, it's the env detail endpoint itself,
		// which is org-level.
		return ""
	}
	id := before
	return id
}
