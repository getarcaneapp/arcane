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

var accessSurfaces = []AccessSurface{
	landingSurface("landing.customize", "/customize", "Customize", []string{
		"customize.category.templates",
		"customize.category.registries",
		"customize.category.variables",
		"customize.category.git-repositories",
	}, 40),
	landingSurface("landing.settings", "/settings", "Settings", []string{
		"settings.category.storage",
		"settings.category.docker",
		"settings.category.security",
		"settings.category.timeouts",
		"settings.category.activity",
		"settings.category.build",
		"settings.category.jobschedule",
	}, 120),
	landingSurface("landing.system", "/system", "System", []string{
		"settings.category.apikeys",
		"settings.category.authentication",
		"settings.category.notifications",
		"settings.category.roles",
		"settings.category.s3destinations",
		"settings.category.systembackups",
		"settings.category.users",
		"settings.category.webhooks",
		"settings.category.diagnostics",
	}, 125),

	routeSurface("route.projects", "/projects", "Projects", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 30),
	routeSurface("route.projects.new", "/projects/new", "Create project", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead, PermProjectsCreate}, 0),
	routeSurface("route.projects.detail", "/projects/{projectId}", "Project", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 0),
	routeSurface("route.environments", "/environments", "Environments", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 130),
	routeSurface("route.environments.detail", "/environments/{id}", "Environment", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 0),
	routeSurface("route.gitops", "/gitops", "GitOps Syncs", AccessScopeModeEnvPlusGlobal, []string{PermGitOpsList, PermGitOpsRead}, 0),
	routeSurface("route.containers", "/containers", "Containers", AccessScopeModeEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 20),
	routeSurface("route.containers.detail", "/containers/{containerId}", "Container", AccessScopeModeEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 0),
	routeSurface("route.images", "/images", "Images", AccessScopeModeEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 50),
	routeSurface("route.images.detail", "/images/{imageId}", "Image", AccessScopeModeEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 0),
	routeSurface("route.images.builds", "/images/builds", "Builds", AccessScopeModeEnvPlusGlobal, []string{PermImagesBuild}, 0),
	// The security page moved from /images/vulnerabilities; the surface ID stays
	// stable so saved role access scopes keep working.
	routeSurface("route.images.vulnerabilities", "/security", "Security", AccessScopeModeEnvPlusGlobal, []string{PermVulnsRead}, 55),
	routeSurface("route.updates", "/updates", "Image Updates", AccessScopeModeEnvPlusGlobal, []string{PermImageUpdatesRead}, 0),
	routeSurface("route.networks", "/networks", "Networks", AccessScopeModeEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 70),
	routeSurface("route.networks.detail", "/networks/{networkId}", "Network", AccessScopeModeEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 0),
	routeSurface("route.ports", "/networks/ports", "Ports", AccessScopeModeEnvPlusGlobal, []string{PermContainersList}, 0),
	routeSurface("route.networks.topology", "/networks/topology", "Network Topology", AccessScopeModeEnvPlusGlobal, []string{PermNetworksRead}, 0),
	routeSurface("route.volumes", "/volumes", "Volumes", AccessScopeModeEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 60),
	routeSurface("route.volumes.detail", "/volumes/{volumeName}", "Volume", AccessScopeModeEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 0),
	routeSurface("route.swarm", "/swarm", "Swarm", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.swarm.services", "/swarm/services", "Services", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 80),
	routeSurface("route.swarm.services.detail", "/swarm/services/{serviceId}", "Service", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.swarm.nodes", "/swarm/nodes", "Nodes", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.swarm.tasks", "/swarm/tasks", "Tasks", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.swarm.stacks", "/swarm/stacks", "Stacks", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 90),
	routeSurface("route.swarm.stacks.new", "/swarm/stacks/new", "Create stack", AccessScopeModeEnvPlusGlobal, []string{PermSwarmStacks}, 0),
	routeSurface("route.swarm.stacks.detail", "/swarm/stacks/{name}", "Stack", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.swarm.cluster", "/swarm/cluster", "Cluster", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 100),
	routeSurface("route.swarm.configs", "/swarm/configs", "Configs", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.swarm.secrets", "/swarm/secrets", "Secrets", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
	routeSurface("route.podman", "/podman/pods", "Podman", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead}, 0),
	routeSurface("route.podman.quadlets", "/podman/quadlets", "Quadlets", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead}, 0),
	allOfRouteSurface("route.podman.quadlets.new", "/podman/quadlets/new", "Install quadlet", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanQuadlets}),
	allOfRouteSurface("route.podman.kube", "/podman/kube", "Kube", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanKube}),
	routeSurface("route.podman.secrets", "/podman/secrets", "Podman secrets", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead}, 0),
	allOfRouteSurface("route.podman.autoupdate", "/podman/autoupdate", "Podman auto-update", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanAutoUpdate}),
	routeSurface("route.events", "/events", "Events", AccessScopeModeGlobalOnly, []string{PermEventsRead}, 110),
	routeSurface("route.activities", "", "Activities", AccessScopeModeAnyEffectiveScope, []string{PermActivitiesRead}, 0),
	allOfRouteSurface("route.oidc-role-mappings", "", "OIDC Role Mappings", AccessScopeModeGlobalOnly, AllPermissions()),
	routeSurface("route.customize.templates.create",
		"/customize/templates/create",
		"Create template",
		AccessScopeModeGlobalOnly,
		[]string{
			PermCustomizeManage,
			PermTemplatesList,
			PermTemplatesRead,
		},
		0),

	routeSurface("route.customize.templates.default",
		"/customize/templates/default",
		"Default template",
		AccessScopeModeGlobalOnly,
		[]string{
			PermCustomizeManage,
			PermTemplatesList,
			PermTemplatesRead,
		},
		0),

	routeSurface("route.customize.templates.detail", "/customize/templates/{id}", "Template", AccessScopeModeGlobalOnly, []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}, 0),
	allOfRouteSurface("route.customize.registries.browse", "/customize/registries/{id}", "Registry browser", AccessScopeModeGlobalOnly, []string{PermRegistriesRead, PermRegistriesBrowse}),

	settingsCategorySurface("apikeys", "/system/api-keys", "API Keys", AccessScopeModeGlobalOnly, []string{PermApiKeysList, PermApiKeysRead}),
	settingsCategorySurface("authentication", "/system/authentication", "Authentication", AccessScopeModeGlobalOnly, []string{PermSettingsRead}),
	settingsCategorySurface("notifications", "/system/notifications", "Notifications", AccessScopeModeGlobalOnly, []string{PermNotificationsManage}),
	settingsCategorySurface("roles", "/system/roles", "Roles", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}),
	settingsCategorySurface("s3destinations", "/system/backups/s3", "S3 Destinations", AccessScopeModeGlobalOnly, []string{PermS3DestinationsList, PermS3DestinationsRead}),
	globalAdminSettingsCategorySurface("systembackups", "/system/backups", "Backups"),
	routeSurface("route.settings.roles.new", "/system/roles/new", "Create role", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}, 0),
	routeSurface("route.settings.roles.detail", "/system/roles/{id}", "Role", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}, 0),
	settingsCategorySurface("users", "/system/users", "Users", AccessScopeModeGlobalOnly, []string{PermUsersList, PermUsersRead}),
	settingsCategorySurface("webhooks", "/system/webhooks", "Webhooks", AccessScopeModeEnvPlusGlobal, []string{PermWebhooksList}),
	settingsCategorySurface("diagnostics", "/system/diagnostics", "Diagnostics", AccessScopeModeGlobalOnly, []string{PermDiagnosticsRead}),
	settingsCategorySurface("storage", "/settings/storage", "Storage & Limits", AccessScopeModeEnvPlusGlobal, []string{PermSettingsRead}),
	settingsCategorySurface("docker", "/settings/docker", "Docker", AccessScopeModeEnvPlusGlobal, []string{PermSettingsRead}),
	settingsCategorySurface("security", "/settings/security", "Security", AccessScopeModeEnvPlusGlobal, []string{PermSettingsRead}),
	settingsCategorySurface("timeouts", "/settings/timeouts", "Timeouts", AccessScopeModeEnvPlusGlobal, []string{PermSettingsRead}),
	settingsCategorySurface("activity", "/settings/activity", "Activity", AccessScopeModeEnvPlusGlobal, []string{PermSettingsRead}),
	settingsCategorySurface("build", "/settings/builds", "Builds", AccessScopeModeEnvPlusGlobal, []string{PermSettingsRead}),
	settingsCategorySurface("jobschedule", "/settings/automations", "Automations", AccessScopeModeEnvPlusGlobal, []string{PermJobsManage}),

	customizeCategorySurface("templates", "/customize/templates", "Templates", []string{PermCustomizeManage, PermTemplatesList, PermTemplatesRead}),
	customizeCategorySurface("registries", "/customize/registries", "Container Registries", []string{PermCustomizeManage, PermRegistriesList, PermRegistriesRead}),
	customizeCategorySurface("variables", "/customize/variables", "Variables", []string{PermVariablesRead}),
	customizeCategorySurface("git-repositories", "/customize/git-repositories", "Git Repositories", []string{PermCustomizeManage, PermGitReposList, PermGitReposRead}),
}

var accessSurfacesByID = buildAccessSurfaceIndex(accessSurfaces)

// AccessSurfaces returns a defensive copy of every backend-owned access
// surface in stable evaluation order.
func AccessSurfaces() []AccessSurface {
	out := make([]AccessSurface, len(accessSurfaces))
	for i := range accessSurfaces {
		out[i] = copyAccessSurface(accessSurfaces[i])
	}
	return out
}

// CanAccessSurface evaluates backend-owned UI reachability metadata for the
// caller. It is for advisory UX only; middleware and handlers still enforce
// permissions on actual API requests.
func CanAccessSurface(ps *PermissionSet, surfaceID, envID string) bool {
	return canAccessSurface(ps, surfaceID, envID, make(map[string]struct{}))
}

func canAccessSurface(ps *PermissionSet, surfaceID, envID string, visiting map[string]struct{}) bool {
	if ps == nil {
		return false
	}
	if _, ok := visiting[surfaceID]; ok {
		return false
	}
	surface, ok := accessSurfacesByID[surfaceID]
	if !ok {
		return false
	}

	switch surface.AccessMode {
	case AccessModeAnyChild:
		visiting[surfaceID] = struct{}{}
		defer delete(visiting, surfaceID)
		for _, childID := range surface.Children {
			if canAccessSurface(ps, childID, envID, visiting) {
				return true
			}
		}
		return false
	case AccessModePermissions:
		return allowsSurfacePermissions(ps, surface, envID)
	default:
		return false
	}
}

func allowsSurfacePermissions(ps *PermissionSet, surface AccessSurface, envID string) bool {
	if len(surface.Permissions) == 0 {
		return false
	}

	switch surface.MatchMode {
	case AccessMatchModeAllOf:
		for _, perm := range surface.Permissions {
			if !allowsPermissionForScopeMode(ps, perm, surface.ScopeMode, envID) {
				return false
			}
		}
		return true
	case AccessMatchModeAnyOf:
		for _, perm := range surface.Permissions {
			if allowsPermissionForScopeMode(ps, perm, surface.ScopeMode, envID) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func allowsPermissionForScopeMode(ps *PermissionSet, perm, scopeMode, envID string) bool {
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

func buildAccessSurfaceIndex(surfaces []AccessSurface) map[string]AccessSurface {
	out := make(map[string]AccessSurface, len(surfaces))
	for _, surface := range surfaces {
		out[surface.ID] = surface
	}
	return out
}

func copyAccessSurface(surface AccessSurface) AccessSurface {
	surface.Permissions = append([]string(nil), surface.Permissions...)
	surface.Children = append([]string(nil), surface.Children...)
	return surface
}

func routeSurface(id, url, label, scopeMode string, permissions []string, fallbackOrder int) AccessSurface {
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

func allOfRouteSurface(id, url, label, scopeMode string, permissions []string) AccessSurface {
	surface := routeSurface(id, url, label, scopeMode, permissions, 0)
	surface.MatchMode = AccessMatchModeAllOf
	return surface
}

// globalAdminSettingsCategorySurface approximates the global-admin
// gate its routes enforce: only admins hold every permission.
func globalAdminSettingsCategorySurface(categoryID, url, label string) AccessSurface {
	surface := settingsCategorySurface(categoryID, url, label, AccessScopeModeGlobalOnly, AllPermissions())
	surface.MatchMode = AccessMatchModeAllOf
	return surface
}

func settingsCategorySurface(categoryID, url, label, scopeMode string, permissions []string) AccessSurface {
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

func customizeCategorySurface(categoryID, url, label string, permissions []string) AccessSurface {
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

func landingSurface(id, url, label string, children []string, fallbackOrder int) AccessSurface {
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
