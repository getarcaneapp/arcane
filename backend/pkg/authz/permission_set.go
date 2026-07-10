package authz

import "strings"

// PermissionSet is the effective permission set for one caller in one request.
// Built once by the auth bridge and stashed in the request context.
//
// Global permissions apply to every environment and to org-level endpoints.
// PerEnv permissions apply only when the caller is acting on that specific
// environment. Sudo bypasses all checks (used for the agent token path).
type PermissionSet struct {
	Global map[string]struct{}
	PerEnv map[string]map[string]struct{}
	Sudo   bool
}

// Allows reports whether the caller may perform `perm`. For env-scoped
// permissions, envID is the target environment's ID. For org-level
// permissions, pass envID = "" — only Global permissions count.
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
		if _, ok := env[perm]; ok {
			return true
		}
	}
	return false
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
	return false
}

// IsGlobalAdmin reports whether the caller holds enough global permissions to
// be considered an administrator. True for sudo callers and for callers whose
// Global set contains every defined permission. Used by the backward-compat
// IsAdminFromContext helper and by last-admin guards.
//
// Implementation: first checks cardinality against TotalPermissionsCount() for a
// fast early exit, then walks AllPermissions() to confirm every known
// permission is present. This guards against a ps.Global that contains the right
// count but includes injected unknown permissions instead of all real ones.
func (ps *PermissionSet) IsGlobalAdmin() bool {
	if ps == nil {
		return false
	}
	if ps.Sudo {
		return true
	}
	if len(ps.Global) != TotalPermissionsCount() {
		return false
	}
	for _, permission := range AllPermissions() {
		if _, ok := ps.Global[permission]; !ok {
			return false
		}
	}
	return true
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
	if id == "" {
		return ""
	}
	return id
}
