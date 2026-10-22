package authz

import (
	"maps"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
)

// PermissionSet is the effective permission set for one caller in one request.
// Built once by the auth bridge and stashed in the request context.
//
// Global permissions apply to every environment and to org-level endpoints.
// PerEnv permissions apply only when the caller is acting on that specific
// environment. Scoped permissions are keyed by environment ID ("" for global)
// and apply only to resources matching one of their selectors. Sudo bypasses
// all checks (used for the agent token path).
type PermissionSet struct {
	Global    map[string]struct{}
	PerEnv    map[string]map[string]struct{}
	Scoped    map[string]map[string][]types.ResourceSelector
	SubjectID string
	Sudo      bool
}

// Allows reports whether the caller may perform `perm`. For env-scoped
// permissions, envID is the target environment's ID. For org-level
// permissions, pass envID = "" — only Global permissions count.
func (ps *PermissionSet) Allows(perm, envID string) bool {
	if ps == nil {
		return false
	}
	if ps.unrestricted(perm, envID) {
		return true
	}
	return len(ps.Scoped[""][perm]) > 0 || envID != "" && len(ps.Scoped[envID][perm]) > 0
}

func (ps *PermissionSet) unrestricted(perm, envID string) bool {
	if ps.Sudo {
		return true
	}
	if _, ok := ps.Global[PermAll]; ok {
		return true
	}
	if _, ok := ps.Global[perm]; ok {
		return true
	}
	if envID == "" {
		return false
	}
	_, all := ps.PerEnv[envID][PermAll]
	_, ok := ps.PerEnv[envID][perm]
	return all || ok
}

// AllowsAny reports whether the caller may perform perm in at least one
// effective scope. Global and sudo permissions satisfy the check immediately;
// otherwise every explicitly granted environment is considered.
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
	for envID := range ps.Scoped {
		if ps.Allows(perm, envID) {
			return true
		}
	}
	return false
}

// IsGlobalAdmin reports whether the caller is an administrator: sudo callers
// and callers holding the PermAll wildcard at global scope. Used by last-admin
// guards and admin-only checks.
func (ps *PermissionSet) IsGlobalAdmin() bool {
	if ps == nil {
		return false
	}
	_, all := ps.Global[PermAll]
	return ps.Sudo || all
}

// SudoPermissionSet returns a PermissionSet that allows every action. Used for
// the agent token path, which bypasses per-user permission resolution entirely.
func SudoPermissionSet() *PermissionSet {
	return &PermissionSet{Sudo: true}
}

// EnvironmentPermissionSet returns the effective permission set for a
// per-environment access token. It grants every environment-scoped permission
// only for envID and no global or sudo permissions.
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

// NewPermissionSet builds an empty PermissionSet ready for population.
func NewPermissionSet() *PermissionSet {
	return &PermissionSet{
		Global: make(map[string]struct{}),
		PerEnv: make(map[string]map[string]struct{}),
		Scoped: make(map[string]map[string][]types.ResourceSelector),
	}
}

// AddGlobal grants `perms` at global scope.
func (ps *PermissionSet) AddGlobal(perms ...string) {
	if ps.Global == nil {
		ps.Global = make(map[string]struct{})
	}
	for _, p := range perms {
		ps.Global[p] = struct{}{}
	}
}

// AddEnv grants `perms` scoped to envID.
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

func (ps *PermissionSet) AddScopedGlobal(sel types.ResourceSelector, perms ...string) {
	ps.AddScopedEnv("", sel, perms...)
}

func (ps *PermissionSet) AddScopedEnv(envID string, sel types.ResourceSelector, perms ...string) {
	if envID != "" {
		ps.AddEnv(envID)
	}
	if ps.Scoped == nil {
		ps.Scoped = make(map[string]map[string][]types.ResourceSelector)
	}
	env, ok := ps.Scoped[envID]
	if !ok {
		env = make(map[string][]types.ResourceSelector)
		ps.Scoped[envID] = env
	}
	for _, p := range perms {
		env[p] = append(env[p], sel)
	}
}

func (ps *PermissionSet) Scope(perm, envID string) ResourceScope {
	if ps == nil {
		return ResourceScope{}
	}
	if ps.unrestricted(perm, envID) {
		return ResourceScope{
			unrestricted: true,
			subjectID:    ps.SubjectID,
		}
	}
	selectors := slices.Clone(ps.Scoped[""][perm])
	if envID != "" {
		selectors = append(selectors, ps.Scoped[envID][perm]...)
	}
	return ResourceScope{
		subjectID: ps.SubjectID,
		selectors: selectors,
	}
}

func (ps *PermissionSet) PermissionsForEnv(envID string) (all, scoped []string) {
	if ps == nil {
		return nil, nil
	}
	if ps.Sudo {
		return []string{PermAll}, nil
	}
	set := make(map[string]struct{})
	maps.Copy(set, ps.Global)
	for _, id := range slices.Compact([]string{"", envID}) {
		if id != "" {
			maps.Copy(set, ps.PerEnv[id])
		}
		for p := range ps.Scoped[id] {
			set[p] = struct{}{}
		}
	}
	all = slices.Sorted(maps.Keys(set))
	scoped = slices.DeleteFunc(slices.Clone(all), func(p string) bool {
		return ps.unrestricted(p, envID)
	})
	return all, scoped
}

// EnvIDFromPath extracts the environment ID from a Huma operation path of the
// form /environments/{id}/... Returns "" for paths without an env segment.
// Tolerates a leading /api prefix for safety, though the Huma group already
// strips it.
func EnvIDFromPath(path string) string {
	path = strings.TrimPrefix(path, "/api")
	if !strings.HasPrefix(path, "/environments/") {
		return ""
	}
	rest := path[len("/environments/"):]
	before, _, ok := strings.Cut(rest, "/")
	if !ok {
		// Path is just /environments/<id> (no trailing segment) — this is
		// not an env-scoped operation, it's the env detail endpoint itself,
		// which is org-level.
		return ""
	}
	id := before
	return id
}
