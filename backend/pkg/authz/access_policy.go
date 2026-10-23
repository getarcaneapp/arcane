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

	AccessScopeModeGlobalOnly        = "global-only"
	AccessScopeModeEnvPlusGlobal     = "env-plus-global"
	AccessScopeModeAnyEffectiveScope = "any-effective-scope"
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
		"settings.category.apikeys",
		"settings.category.authentication",
		"settings.category.notifications",
		"settings.category.roles",
		"settings.category.s3destinations",
		"settings.category.systembackups",
		"settings.category.users",
		"settings.category.webhooks",
		"settings.category.diagnostics",
	}, 120),

	routeSurfaceInternal("route.dashboard", "/dashboard", "Dashboard", AccessScopeModeEnvPlusGlobal, []string{PermDashboardRead}, 10),
	routeSurfaceInternal("route.projects", "/projects", "Projects", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 30),
	routeSurfaceInternal("route.projects.new", "/projects/new", "Create project", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead, PermProjectsCreate}, 0),
	routeSurfaceInternal("route.projects.detail", "/projects/{projectId}", "Project", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 0),
	routeSurfaceInternal("route.environments", "/environments", "Environments", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 130),
	routeSurfaceInternal("route.environments.detail", "/environments/{id}", "Environment", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 0),
	routeSurfaceInternal("route.gitops", "/gitops", "GitOps Syncs", AccessScopeModeEnvPlusGlobal, []string{PermGitOpsList, PermGitOpsRead}, 0),
	routeSurfaceInternal("route.containers", "/containers", "Containers", AccessScopeModeEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 20),
	routeSurfaceInternal("route.containers.detail", "/containers/{containerId}", "Container", AccessScopeModeEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 0),
	routeSurfaceInternal("route.images", "/images", "Images", AccessScopeModeEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 50),
	routeSurfaceInternal("route.images.detail", "/images/{imageId}", "Image", AccessScopeModeEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 0),
	routeSurfaceInternal("route.images.builds", "/images/builds", "Builds", AccessScopeModeEnvPlusGlobal, []string{PermImagesBuild}, 0),
	// The security page moved from /images/vulnerabilities; the surface ID stays
	// stable so saved role access scopes keep working.
	routeSurfaceInternal("route.images.vulnerabilities", "/security", "Security", AccessScopeModeEnvPlusGlobal, []string{PermVulnsRead}, 55),
	routeSurfaceInternal("route.updates", "/updates", "Image Updates", AccessScopeModeEnvPlusGlobal, []string{PermImageUpdatesRead}, 0),
	routeSurfaceInternal("route.networks", "/networks", "Networks", AccessScopeModeEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 70),
	routeSurfaceInternal("route.networks.detail", "/networks/{networkId}", "Network", AccessScopeModeEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 0),
	routeSurfaceInternal("route.ports", "/networks/ports", "Ports", AccessScopeModeEnvPlusGlobal, []string{PermContainersList}, 0),
	routeSurfaceInternal("route.networks.topology", "/networks/topology", "Network Topology", AccessScopeModeEnvPlusGlobal, []string{PermNetworksRead}, 0),
	routeSurfaceInternal("route.volumes", "/volumes", "Volumes", AccessScopeModeEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 60),
	routeSurfaceInternal("route.volumes.detail", "/volumes/{volumeName}", "Volume", AccessScopeModeEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 0),
	routeSurfaceInternal("route.swarm", "/swarm", "Swarm", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.services", "/swarm/services", "Services", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 80),
	routeSurfaceInternal("route.swarm.services.detail", "/swarm/services/{serviceId}", "Service", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.nodes", "/swarm/nodes", "Nodes", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.tasks", "/swarm/tasks", "Tasks", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.stacks", "/swarm/stacks", "Stacks", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 90),
	routeSurfaceInternal("route.swarm.stacks.new", "/swarm/stacks/new", "Create stack", AccessScopeModeEnvPlusGlobal, []string{PermSwarmStacks}, 0),
	routeSurfaceInternal("route.swarm.stacks.detail", "/swarm/stacks/{name}", "Stack", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.cluster", "/swarm/cluster", "Cluster", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 100),
	routeSurfaceInternal("route.swarm.configs", "/swarm/configs", "Configs", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.swarm.secrets", "/swarm/secrets", "Secrets", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurfaceInternal("route.podman", "/podman/pods", "Podman", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead}, 0),
	routeSurfaceInternal("route.podman.quadlets", "/podman/quadlets", "Quadlets", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead}, 0),
	allOfRouteSurfaceInternal("route.podman.quadlets.new", "/podman/quadlets/new", "Install quadlet", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanQuadlets}),
	allOfRouteSurfaceInternal("route.podman.kube", "/podman/kube", "Kube", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanKube}),
	routeSurfaceInternal("route.podman.secrets", "/podman/secrets", "Podman secrets", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead}, 0),
	allOfRouteSurfaceInternal("route.podman.autoupdate", "/podman/autoupdate", "Podman auto-update", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanAutoUpdate}),
	routeSurfaceInternal("route.events", "/events", "Events", AccessScopeModeGlobalOnly, []string{PermEventsRead}, 110),
	routeSurfaceInternal("route.activities", "", "Activities", AccessScopeModeAnyEffectiveScope, []string{PermActivitiesRead}, 0),
	allOfRouteSurfaceInternal("route.oidc-role-mappings", "", "OIDC Role Mappings", AccessScopeModeGlobalOnly, AllPermissions()),
	routeSurfaceInternal("route.customize.templates.create",
		"/customize/templates/create",
		"Create template",
		AccessScopeModeGlobalOnly,
		[]string{
			PermCustomizeManage,
			PermTemplatesList,
			PermTemplatesRead,
		},
		0),

	routeSurfaceInternal("route.customize.templates.default",
		"/customize/templates/default",
		"Default template",
		AccessScopeModeGlobalOnly,
		[]string{
			PermCustomizeManage,
			PermTemplatesList,
			PermTemplatesRead,
		},
		0),

	routeSurfaceInternal("route.customize.templates.detail", "/customize/templates/{id}", "Template", AccessScopeModeGlobalOnly, []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}, 0),
	allOfRouteSurfaceInternal("route.customize.registries.browse", "/customize/registries/{id}", "Registry browser", AccessScopeModeGlobalOnly, []string{PermRegistriesRead, PermRegistriesBrowse}),

	settingsCategorySurfaceInternal("apikeys", "/settings/api-keys", "API Keys", AccessScopeModeGlobalOnly, []string{PermApiKeysList, PermApiKeysRead}),
	settingsCategorySurfaceInternal("authentication", "/settings/authentication", "Authentication", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurfaceInternal("notifications", "/settings/notifications", "Notifications", AccessScopeModeGlobalOnly, []string{PermNotificationsManage}),
	settingsCategorySurfaceInternal("roles", "/settings/roles", "Roles", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}),
	settingsCategorySurfaceInternal("s3destinations", "/settings/backups/s3", "S3 Destinations", AccessScopeModeGlobalOnly, []string{PermS3DestinationsList, PermS3DestinationsRead}),
	globalAdminSettingsCategorySurfaceInternal("systembackups", "/settings/backups", "Backups"),
	routeSurfaceInternal("route.settings.roles.new", "/settings/roles/new", "Create role", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}, 0),
	routeSurfaceInternal("route.settings.roles.detail", "/settings/roles/{id}", "Role", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}, 0),
	settingsCategorySurfaceInternal("users", "/settings/users", "Users", AccessScopeModeGlobalOnly, []string{PermUsersList, PermUsersRead}),
	settingsCategorySurfaceInternal("webhooks", "/settings/webhooks", "Webhooks", AccessScopeModeEnvPlusGlobal, []string{PermWebhooksList}),
	settingsCategorySurfaceInternal("diagnostics", "/settings/diagnostics", "Diagnostics", AccessScopeModeGlobalOnly, []string{PermDiagnosticsRead}),

	customizeCategorySurfaceInternal("templates", "/customize/templates", "Templates", []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}),
	customizeCategorySurfaceInternal("registries", "/customize/registries", "Container Registries", []string{PermCustomizeManage, PermRegistriesList, PermRegistriesRead}),
	customizeCategorySurfaceInternal("variables", "/customize/variables", "Variables", []string{PermVariablesRead}),
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
func CanAccessSurface(ps *PermissionSet, surfaceID, envID string) bool {
	return canAccessSurfaceInternal(ps, surfaceID, envID, make(map[string]struct{}))
}

// CanAccessSettingsCategory reports whether a settings category is reachable
// for the selected environment.
func CanAccessSettingsCategory(ps *PermissionSet, categoryID, envID string) bool {
	return CanAccessSurface(ps, "settings.category."+categoryID, envID)
}

// CanAccessCustomizeCategory reports whether a customize category is reachable.
func CanAccessCustomizeCategory(ps *PermissionSet, categoryID, envID string) bool {
	return CanAccessSurface(ps, "customize.category."+categoryID, envID)
}

func canAccessSurfaceInternal(ps *PermissionSet, surfaceID, envID string, visiting map[string]struct{}) bool {
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
			if canAccessSurfaceInternal(ps, childID, envID, visiting) {
				return true
			}
		}
		return false
	case AccessModePermissions:
		return allowsSurfacePermissionsInternal(ps, surface, envID)
	default:
		return false
	}
}

func allowsSurfacePermissionsInternal(ps *PermissionSet, surface AccessSurface, envID string) bool {
	if len(surface.Permissions) == 0 {
		return false
	}

	switch surface.MatchMode {
	case AccessMatchModeAllOf:
		for _, perm := range surface.Permissions {
			if !allowsPermissionForScopeModeInternal(ps, perm, surface.ScopeMode, envID) {
				return false
			}
		}
		return true
	case AccessMatchModeAnyOf:
		for _, perm := range surface.Permissions {
			if allowsPermissionForScopeModeInternal(ps, perm, surface.ScopeMode, envID) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func allowsPermissionForScopeModeInternal(ps *PermissionSet, perm, scopeMode, envID string) bool {
	switch scopeMode {
	case AccessScopeModeGlobalOnly:
		return ps.Allows(perm, "")
	case AccessScopeModeEnvPlusGlobal:
		return ps.Allows(perm, envID)
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

func allOfRouteSurfaceInternal(id, url, label, scopeMode string, permissions []string) AccessSurface {
	surface := routeSurfaceInternal(id, url, label, scopeMode, permissions, 0)
	surface.MatchMode = AccessMatchModeAllOf
	return surface
}

// globalAdminSettingsCategorySurfaceInternal approximates the global-admin
// gate its routes enforce: only admins hold every permission.
func globalAdminSettingsCategorySurfaceInternal(categoryID, url, label string) AccessSurface {
	surface := settingsCategorySurfaceInternal(categoryID, url, label, AccessScopeModeGlobalOnly, AllPermissions())
	surface.MatchMode = AccessMatchModeAllOf
	return surface
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
		ScopeMode:     AccessScopeModeEnvPlusGlobal,
		Children:      append([]string(nil), children...),
		FallbackOrder: fallbackOrder,
	}
}
