package authz

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

type (
	ResourceKey struct {
		Type string
		ID   string
	}

	// Built once by the auth bridge and stashed in the request context.
	PermissionSet struct {
		Global      map[string]struct{}
		PerEnv      map[string]map[string]struct{}
		PerResource map[string]map[ResourceKey]map[string]struct{}
		Sudo        bool
	}

	ForwardedResourcePermissions struct {
		Type        string   `json:"type"`
		ID          string   `json:"id"`
		Permissions []string `json:"permissions"`
	}

	ForwardedPermissions struct {
		Global      []string                       `json:"global"`
		Environment []string                       `json:"environment"`
		Resources   []ForwardedResourcePermissions `json:"resources,omitempty"`
		GlobalAdmin bool                           `json:"globalAdmin,omitzero"`
	}
)

const (
	ResourceTypeProject   = "project"
	ResourceTypeContainer = "container"
	ResourceTypeVolume    = "volume"
	ResourceTypeNetwork   = "network"
	ResourceTypeImage     = "image"
	ResourceTypeGitOps    = "gitops"
)

func IsKnownResourceType(resourceType string) bool {
	switch resourceType {
	case ResourceTypeProject, ResourceTypeContainer, ResourceTypeVolume, ResourceTypeNetwork, ResourceTypeImage, ResourceTypeGitOps:
		return true
	default:
		return false
	}
}

// ResourceTypesForPermission returns the resource types whose resource-scoped grants
// can authorize route-level entry for perm. Collection-wide mutations (create, prune)
// and org-level permissions return nil and require an environment-wide or global grant.
func ResourceTypesForPermission(perm string) []string {
	switch perm {
	case PermProjectsList, PermProjectsRead, PermProjectsLogs, PermProjectsUpdate,
		PermProjectsDeploy, PermProjectsJobsRun, PermProjectsBackup, PermProjectsRestore,
		PermProjectsDown, PermProjectsRestart, PermProjectsDelete, PermProjectsArchive, PermProjectsExport:
		return []string{ResourceTypeProject}
	case PermContainersList, PermContainersRead, PermContainersLogs,
		PermContainersStart, PermContainersStop, PermContainersRestart, PermContainersRedeploy,
		PermContainersUpdate, PermContainersKill, PermContainersPause, PermContainersDelete,
		PermContainersExec, PermContainersAutoUpdate:
		return []string{ResourceTypeContainer, ResourceTypeProject}
	case PermVolumesList, PermVolumesRead, PermVolumesRename, PermVolumesDelete, PermVolumesUpload, PermVolumesBackup:
		return []string{ResourceTypeVolume, ResourceTypeProject}
	case PermNetworksList, PermNetworksRead, PermNetworksDelete, PermNetworksConnect, PermNetworksDisconnect:
		return []string{ResourceTypeNetwork, ResourceTypeProject}
	case PermImagesList, PermImagesRead:
		return []string{ResourceTypeImage, ResourceTypeProject}
	case PermImagesPush, PermImagesTag, PermImagesCommit, PermImagesDelete, PermImagesPatch:
		return []string{ResourceTypeImage}
	case PermGitOpsList, PermGitOpsRead, PermGitOpsUpdate, PermGitOpsDelete,
		PermGitOpsSync, PermGitOpsBackup, PermGitOpsBidirectional, PermGitOpsLifecycle:
		return []string{ResourceTypeGitOps, ResourceTypeProject}
	default:
		return nil
	}
}

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

func (ps *PermissionSet) AllowsResource(perm, envID, resourceType, resourceID string) bool {
	if ps.Allows(perm, envID) {
		return true
	}
	if ps == nil || envID == "" || resourceID == "" || !slices.Contains(ResourceTypesForPermission(perm), resourceType) {
		return false
	}
	if envResources, ok := ps.PerResource[envID]; ok {
		if perms, found := envResources[ResourceKey{Type: resourceType, ID: resourceID}]; found {
			_, allowed := perms[perm]
			return allowed
		}
	}
	return false
}

func (ps *PermissionSet) AllowsAnyResource(perm, envID string, resourceTypes ...string) bool {
	if ps.Allows(perm, envID) {
		return true
	}
	if ps == nil || envID == "" || len(resourceTypes) == 0 {
		return false
	}
	envResources, ok := ps.PerResource[envID]
	if !ok {
		return false
	}
	for key, perms := range envResources {
		if !slices.Contains(resourceTypes, key.Type) {
			continue
		}
		if _, allowed := perms[perm]; allowed {
			return true
		}
	}
	return false
}

// AllowsRoute checks whether the caller may enter a route gated by perm on envID.
// For resource-targetable permissions, a matching resource-scoped grant passes route
// entry so the handler/service can enforce exact per-resource filtering and checks.
func (ps *PermissionSet) AllowsRoute(perm, envID string) bool {
	if ps.Allows(perm, envID) {
		return true
	}
	if envID == "" {
		return false
	}
	rts := ResourceTypesForPermission(perm)
	if len(rts) == 0 {
		return false
	}
	return ps.AllowsAnyResource(perm, envID, rts...)
}

// ResourceIDs returns nil when the caller has unrestricted env/global access for perm,
// otherwise the sorted resource IDs of resourceType granted perm on envID.
func (ps *PermissionSet) ResourceIDs(perm, envID, resourceType string) []string {
	if ps.Allows(perm, envID) {
		return nil
	}
	ids := make([]string, 0)
	if ps == nil || envID == "" || resourceType == "" {
		return ids
	}
	for key, perms := range ps.PerResource[envID] {
		if key.Type != resourceType {
			continue
		}
		if _, ok := perms[perm]; ok {
			ids = append(ids, key.ID)
		}
	}
	slices.Sort(ids)
	return ids
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
	if rts := ResourceTypesForPermission(perm); len(rts) > 0 {
		for envID := range ps.PerResource {
			if ps.AllowsAnyResource(perm, envID, rts...) {
				return true
			}
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
		seen := make(map[string]struct{}, len(ps.PerEnv)+len(ps.PerResource))
		for envID := range ps.PerEnv {
			seen[envID] = struct{}{}
		}
		for envID := range ps.PerResource {
			seen[envID] = struct{}{}
		}
		ids := slices.AppendSeq(make([]string, 0, len(seen)), maps.Keys(seen))
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
	var resources []ForwardedResourcePermissions
	if envResources := ps.PerResource[envID]; len(envResources) > 0 {
		keys := slices.Collect(maps.Keys(envResources))
		slices.SortFunc(keys, func(a, b ResourceKey) int {
			if c := cmp.Compare(a.Type, b.Type); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})
		for _, key := range keys {
			granted := envResources[key]
			perms := slices.DeleteFunc(slices.Clone(orderedPermissions), func(perm string) bool {
				_, global := ps.Global[perm]
				_, envWide := ps.PerEnv[envID][perm]
				_, local := granted[perm]
				return global || envWide || !local || !IsEnvScoped(perm)
			})
			if len(perms) == 0 {
				continue
			}
			resources = append(resources, ForwardedResourcePermissions{
				Type:        key.Type,
				ID:          key.ID,
				Permissions: perms,
			})
		}
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
		Resources: resources,
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
	for _, resource := range f.Resources {
		if !IsKnownResourceType(resource.Type) || resource.ID == "" {
			continue
		}
		for _, perm := range resource.Permissions {
			if IsEnvScoped(perm) {
				ps.AddResource(envID, resource.Type, resource.ID, perm)
			}
		}
	}
	return ps
}

func NewPermissionSet() *PermissionSet {
	return &PermissionSet{
		Global:      make(map[string]struct{}),
		PerEnv:      make(map[string]map[string]struct{}),
		PerResource: make(map[string]map[ResourceKey]map[string]struct{}),
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

func (ps *PermissionSet) AddResource(envID, resourceType, resourceID string, perms ...string) {
	if envID == "" || resourceType == "" || resourceID == "" {
		return
	}
	if ps.PerResource == nil {
		ps.PerResource = make(map[string]map[ResourceKey]map[string]struct{})
	}
	envResources, ok := ps.PerResource[envID]
	if !ok {
		envResources = make(map[ResourceKey]map[string]struct{})
		ps.PerResource[envID] = envResources
	}
	key := ResourceKey{Type: resourceType, ID: resourceID}
	resPerms, ok := envResources[key]
	if !ok {
		resPerms = make(map[string]struct{})
		envResources[key] = resPerms
	}
	for _, p := range perms {
		if IsEnvScoped(p) {
			resPerms[p] = struct{}{}
		}
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
	return before
}
