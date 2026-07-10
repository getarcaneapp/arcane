package authz

const (
	AccessSurfaceKindRoute             = "route"
	AccessSurfaceKindSettingsCategory  = "settings-category"
	AccessSurfaceKindCustomizeCategory = "customize-category"
	AccessSurfaceKindLanding           = "landing"

	AccessModePermissions = "permissions"
	AccessModeAnyChild    = "any-child"

	AccessMatchModeAnyOf = "any-of"
	AccessMatchModeAllOf = "all-of"

	AccessScopeModeGlobalOnly            = "global-only"
	AccessScopeModeSelectedEnvPlusGlobal = "selected-env-plus-global"
	AccessScopeModeAnyEffectiveScope     = "any-effective-scope"
)

// AccessSurface describes one frontend-visible surface whose reachability is
// derived from backend-owned permission metadata. This is decision metadata for
// UX gating; backend middleware and service checks remain authoritative.
type AccessSurface struct {
	ID            string
	Kind          string
	URL           string
	Label         string
	AccessMode    string
	MatchMode     string
	ScopeMode     string
	Permissions   []string
	Children      []string
	FallbackOrder int
}

var accessSurfacesInternal = []AccessSurface{
	landingSurfaceInternal("landing.customize", "/customize", "Customize", []string{
		"customize.category.templates",
		"customize.category.registries",
		"customize.category.variables",
		"customize.category.git-repositories",
	}, 40),
	landingSurfaceInternal("landing.settings", "/settings", "Settings", []string{
		"settings.category.activity",
		"settings.category.apikeys",
		"settings.category.appearance",
		"settings.category.authentication",
		"settings.category.build",
		"settings.category.jobschedule",
		"settings.category.notifications",
		"settings.category.roles",
		"settings.category.timeouts",
		"settings.category.users",
		"settings.category.webhooks",
		"settings.category.diagnostics",
	}, 120),

	routeSurfaceInternal("route.dashboard", "/dashboard", "Dashboard", AccessScopeModeSelectedEnvPlusGlobal, []string{PermDashboardRead}, 10),
	routeSurfaceInternal("route.projects", "/projects", "Projects", AccessScopeModeSelectedEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 30),
	routeSurfaceInternal("route.projects.new", "/projects/new", "Create project", AccessScopeModeSelectedEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead, PermProjectsCreate}, 0),
	routeSurfaceInternal("route.projects.detail", "/projects/{projectId}", "Project", AccessScopeModeSelectedEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 0),
	routeSurfaceInternal("route.environments", "/environments", "Environments", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 130),
	routeSurfaceInternal("route.environments.detail", "/environments/{id}", "Environment", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 0),
	routeSurfaceInternal("route.environments.gitops", "/environments/{id}/gitops", "GitOps Syncs", AccessScopeModeSelectedEnvPlusGlobal, []string{PermGitOpsList, PermGitOpsRead}, 0),
	routeSurfaceInternal("route.containers", "/containers", "Containers", AccessScopeModeSelectedEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 20),
	routeSurfaceInternal("route.containers.detail", "/containers/{containerId}", "Container", AccessScopeModeSelectedEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 0),
	routeSurfaceInternal("route.images", "/images", "Images", AccessScopeModeSelectedEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 50),
	routeSurfaceInternal("route.images.detail", "/images/{imageId}", "Image", AccessScopeModeSelectedEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 0),
	routeSurfaceInternal("route.images.builds", "/images/builds", "Builds", AccessScopeModeSelectedEnvPlusGlobal, []string{PermImagesBuild}, 0),
	routeSurfaceInternal("route.images.vulnerabilities", "/images/vulnerabilities", "Vulnerabilities", AccessScopeModeSelectedEnvPlusGlobal, []string{PermVulnsRead}, 0),
	routeSurfaceInternal("route.updates", "/updates", "Image Updates", AccessScopeModeSelectedEnvPlusGlobal, []string{PermImageUpdatesRead}, 0),
	routeSurfaceInternal("route.networks", "/networks", "Networks", AccessScopeModeSelectedEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 70),
	routeSurfaceInternal("route.networks.detail", "/networks/{networkId}", "Network", AccessScopeModeSelectedEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 0),
	routeSurfaceInternal("route.ports", "/ports", "Ports", AccessScopeModeSelectedEnvPlusGlobal, []string{PermContainersList}, 0),
	routeSurfaceInternal("route.networks.topology", "/networks/topology", "Network Topology", AccessScopeModeSelectedEnvPlusGlobal, []string{PermNetworksRead}, 0),
	routeSurfaceInternal("route.volumes", "/volumes", "Volumes", AccessScopeModeSelectedEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 60),
	routeSurfaceInternal("route.volumes.detail", "/volumes/{volumeName}", "Volume", AccessScopeModeSelectedEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 0),
	routeSurfaceInternal("route.swarm", "/swarm", "Swarm", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.services", "/swarm/services", "Services", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmServices}, 80),
	routeSurfaceInternal("route.swarm.services.detail", "/swarm/services/{serviceId}", "Service", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmServices}, 0),
	routeSurfaceInternal("route.swarm.nodes", "/swarm/nodes", "Nodes", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmNodes}, 0),
	routeSurfaceInternal("route.swarm.tasks", "/swarm/tasks", "Tasks", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.stacks", "/swarm/stacks", "Stacks", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmStacks}, 90),
	routeSurfaceInternal("route.swarm.stacks.new", "/swarm/stacks/new", "Create stack", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmStacks}, 0),
	routeSurfaceInternal("route.swarm.stacks.detail", "/swarm/stacks/{name}", "Stack", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmStacks}, 0),
	routeSurfaceInternal("route.swarm.cluster", "/swarm/cluster", "Cluster", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmRead}, 100),
	routeSurfaceInternal("route.swarm.configs", "/swarm/configs", "Configs", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmConfigs}, 0),
	routeSurfaceInternal("route.swarm.secrets", "/swarm/secrets", "Secrets", AccessScopeModeSelectedEnvPlusGlobal, []string{PermSwarmSecrets}, 0),
	routeSurfaceInternal("route.events", "/events", "Events", AccessScopeModeGlobalOnly, []string{PermEventsRead}, 110),
	routeSurfaceInternal("route.customize.templates.create", "/customize/templates/create", "Create template", AccessScopeModeGlobalOnly, []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}, 0),
	routeSurfaceInternal("route.customize.templates.default", "/customize/templates/default", "Default template", AccessScopeModeGlobalOnly, []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}, 0),
	routeSurfaceInternal("route.customize.templates.detail", "/customize/templates/{id}", "Template", AccessScopeModeGlobalOnly, []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}, 0),

	settingsCategorySurfaceInternal("activity", "/settings/activity", "Activity", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurfaceInternal("apikeys", "/settings/api-keys", "API Keys", AccessScopeModeGlobalOnly, []string{PermApiKeysList, PermApiKeysRead}),
	settingsCategorySurfaceInternal("appearance", "/settings/appearance", "Appearance", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurfaceInternal("authentication", "/settings/authentication", "Authentication", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurfaceInternal("build", "/settings/builds", "Builds", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurfaceInternal("jobschedule", "", "Job Schedule", AccessScopeModeSelectedEnvPlusGlobal, []string{PermJobsManage}),
	settingsCategorySurfaceInternal("notifications", "/settings/notifications", "Notifications", AccessScopeModeSelectedEnvPlusGlobal, []string{PermNotificationsManage}),
	settingsCategorySurfaceInternal("roles", "/settings/roles", "Roles", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}),
	routeSurfaceInternal("route.settings.roles.new", "/settings/roles/new", "Create role", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}, 0),
	routeSurfaceInternal("route.settings.roles.detail", "/settings/roles/{id}", "Role", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}, 0),
	settingsCategorySurfaceInternal("timeouts", "/settings/timeouts", "Timeouts", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurfaceInternal("users", "/settings/users", "Users", AccessScopeModeGlobalOnly, []string{PermUsersList, PermUsersRead}),
	settingsCategorySurfaceInternal("webhooks", "/settings/webhooks", "Webhooks", AccessScopeModeSelectedEnvPlusGlobal, []string{PermWebhooksList}),
	settingsCategorySurfaceInternal("diagnostics", "/settings/diagnostics", "Diagnostics", AccessScopeModeGlobalOnly, []string{PermDiagnosticsRead}),

	customizeCategorySurfaceInternal("templates", "/customize/templates", "Templates", []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}),
	customizeCategorySurfaceInternal("registries", "/customize/registries", "Container Registries", []string{PermCustomizeManage, PermRegistriesList, PermRegistriesRead}),
	customizeCategorySurfaceInternal("variables", "/customize/variables", "Variables", []string{PermCustomizeManage, PermTemplatesRead}),
	customizeCategorySurfaceInternal("git-repositories", "/customize/git-repositories", "Git Repositories", []string{PermCustomizeManage, PermGitReposList, PermGitReposRead}),
}

var accessSurfacesByIDInternal = buildAccessSurfaceIndexInternal(accessSurfacesInternal)

// AccessSurfaces returns a defensive copy of every backend-owned access
// surface in stable evaluation order.
func AccessSurfaces() []AccessSurface {
	out := make([]AccessSurface, len(accessSurfacesInternal))
	for i := range accessSurfacesInternal {
		out[i] = copyAccessSurfaceInternal(accessSurfacesInternal[i])
	}
	return out
}

// CanAccessSurface evaluates backend-owned UI reachability metadata for the
// caller. It is for advisory UX only; middleware and handlers still enforce
// permissions on actual API requests.
func CanAccessSurface(ps *PermissionSet, surfaceID, selectedEnvID string) bool {
	return canAccessSurfaceInternal(ps, surfaceID, selectedEnvID, make(map[string]struct{}))
}

// CanAccessSettingsCategory reports whether a settings category is reachable
// for the selected environment.
func CanAccessSettingsCategory(ps *PermissionSet, categoryID, selectedEnvID string) bool {
	return CanAccessSurface(ps, "settings.category."+categoryID, selectedEnvID)
}

// CanAccessCustomizeCategory reports whether a customize category is reachable.
func CanAccessCustomizeCategory(ps *PermissionSet, categoryID, selectedEnvID string) bool {
	return CanAccessSurface(ps, "customize.category."+categoryID, selectedEnvID)
}

func canAccessSurfaceInternal(ps *PermissionSet, surfaceID, selectedEnvID string, visiting map[string]struct{}) bool {
	if ps == nil {
		return false
	}
	if _, ok := visiting[surfaceID]; ok {
		return false
	}
	surface, ok := accessSurfacesByIDInternal[surfaceID]
	if !ok {
		return false
	}

	switch surface.AccessMode {
	case AccessModeAnyChild:
		visiting[surfaceID] = struct{}{}
		defer delete(visiting, surfaceID)
		for _, childID := range surface.Children {
			if canAccessSurfaceInternal(ps, childID, selectedEnvID, visiting) {
				return true
			}
		}
		return false
	case AccessModePermissions:
		return allowsSurfacePermissionsInternal(ps, surface, selectedEnvID)
	default:
		return false
	}
}

func allowsSurfacePermissionsInternal(ps *PermissionSet, surface AccessSurface, selectedEnvID string) bool {
	if len(surface.Permissions) == 0 {
		return false
	}

	switch surface.MatchMode {
	case AccessMatchModeAllOf:
		for _, perm := range surface.Permissions {
			if !allowsPermissionForScopeModeInternal(ps, perm, surface.ScopeMode, selectedEnvID) {
				return false
			}
		}
		return true
	case AccessMatchModeAnyOf:
		for _, perm := range surface.Permissions {
			if allowsPermissionForScopeModeInternal(ps, perm, surface.ScopeMode, selectedEnvID) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func allowsPermissionForScopeModeInternal(ps *PermissionSet, perm, scopeMode, selectedEnvID string) bool {
	switch scopeMode {
	case AccessScopeModeGlobalOnly:
		return ps.Allows(perm, "")
	case AccessScopeModeSelectedEnvPlusGlobal:
		return ps.Allows(perm, selectedEnvID)
	case AccessScopeModeAnyEffectiveScope:
		return ps.AllowsAny(perm)
	default:
		return false
	}
}

func buildAccessSurfaceIndexInternal(surfaces []AccessSurface) map[string]AccessSurface {
	out := make(map[string]AccessSurface, len(surfaces))
	for _, surface := range surfaces {
		out[surface.ID] = surface
	}
	return out
}

func copyAccessSurfaceInternal(surface AccessSurface) AccessSurface {
	surface.Permissions = append([]string(nil), surface.Permissions...)
	surface.Children = append([]string(nil), surface.Children...)
	return surface
}

func routeSurfaceInternal(id, url, label, scopeMode string, permissions []string, fallbackOrder int) AccessSurface {
	return AccessSurface{
		ID:            id,
		Kind:          AccessSurfaceKindRoute,
		URL:           url,
		Label:         label,
		AccessMode:    AccessModePermissions,
		MatchMode:     AccessMatchModeAnyOf,
		ScopeMode:     scopeMode,
		Permissions:   append([]string(nil), permissions...),
		FallbackOrder: fallbackOrder,
	}
}

func settingsCategorySurfaceInternal(categoryID, url, label, scopeMode string, permissions []string) AccessSurface {
	return AccessSurface{
		ID:          "settings.category." + categoryID,
		Kind:        AccessSurfaceKindSettingsCategory,
		URL:         url,
		Label:       label,
		AccessMode:  AccessModePermissions,
		MatchMode:   AccessMatchModeAnyOf,
		ScopeMode:   scopeMode,
		Permissions: append([]string(nil), permissions...),
	}
}

func customizeCategorySurfaceInternal(categoryID, url, label string, permissions []string) AccessSurface {
	return AccessSurface{
		ID:          "customize.category." + categoryID,
		Kind:        AccessSurfaceKindCustomizeCategory,
		URL:         url,
		Label:       label,
		AccessMode:  AccessModePermissions,
		MatchMode:   AccessMatchModeAnyOf,
		ScopeMode:   AccessScopeModeGlobalOnly,
		Permissions: append([]string(nil), permissions...),
	}
}

func landingSurfaceInternal(id, url, label string, children []string, fallbackOrder int) AccessSurface {
	return AccessSurface{
		ID:            id,
		Kind:          AccessSurfaceKindLanding,
		URL:           url,
		Label:         label,
		AccessMode:    AccessModeAnyChild,
		MatchMode:     AccessMatchModeAnyOf,
		ScopeMode:     AccessScopeModeSelectedEnvPlusGlobal,
		Children:      append([]string(nil), children...),
		FallbackOrder: fallbackOrder,
	}
}
