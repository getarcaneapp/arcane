package authz

import "slices"

type (
	// AccessSurface describes one frontend-visible surface whose reachability is
	// derived from backend-owned permission metadata. This is decision metadata for
	// UX gating; backend middleware and service checks remain authoritative.
	AccessSurface struct {
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
)

const (
	AccessSurfaceKindRoute            = "route"
	AccessSurfaceKindSettingsCategory = "settings-category"
	AccessSurfaceKindLanding          = "landing"
	AccessModePermissions             = "permissions"
	AccessModeAnyChild                = "any-child"
	AccessMatchModeAnyOf              = "any-of"
	AccessMatchModeAllOf              = "all-of"
	AccessScopeModeGlobalOnly         = "global-only"
	AccessScopeModeEnvPlusGlobal      = "env-plus-global"
	AccessScopeModeAnyEffectiveScope  = "any-effective-scope"
)

var (
	accessSurfaces = []AccessSurface{
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
		allOfRouteSurface("route.projects.new", "/projects/new", "Create project", AccessScopeModeEnvPlusGlobal, []string{PermProjectsCreate}),
		routeSurface("route.projects.detail", "/projects/{projectId}", "Project", AccessScopeModeEnvPlusGlobal, []string{PermProjectsList, PermProjectsRead}, 0),
		routeSurface("route.environments", "/environments", "Environments", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 130),
		routeSurface("route.environments.detail", "/environments/{id}", "Environment", AccessScopeModeGlobalOnly, []string{PermEnvironmentsList, PermEnvironmentsRead}, 0),
		routeSurface("route.gitops", "/gitops", "GitOps Syncs", AccessScopeModeEnvPlusGlobal, []string{PermGitOpsList, PermGitOpsRead}, 0),
		routeSurface("route.containers", "/containers", "Containers", AccessScopeModeEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 20),
		routeSurface("route.containers.new", "/containers/new", "Create container", AccessScopeModeEnvPlusGlobal, []string{PermContainersCreate}, 0),
		routeSurface("route.containers.detail", "/containers/{containerId}", "Container", AccessScopeModeEnvPlusGlobal, []string{PermContainersList, PermContainersRead}, 0),
		routeSurface("route.containers.edit", "/containers/{containerId}/edit", "Edit container", AccessScopeModeEnvPlusGlobal, []string{PermContainersUpdate}, 0),
		routeSurface("route.images", "/images", "Images", AccessScopeModeEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 50),
		routeSurface("route.images.detail", "/images/{imageId}", "Image", AccessScopeModeEnvPlusGlobal, []string{PermImagesList, PermImagesRead}, 0),
		routeSurface("route.images.builds", "/images/builds", "Builds", AccessScopeModeEnvPlusGlobal, []string{PermImagesBuild}, 0),
		// The security page moved from /images/vulnerabilities; the surface ID stays
		// stable so saved role access scopes keep working.
		routeSurface("route.images.vulnerabilities", "/security", "Security", AccessScopeModeEnvPlusGlobal, []string{PermVulnsList}, 55),
		routeSurface("route.updates", "/updates", "Image Updates", AccessScopeModeEnvPlusGlobal, []string{PermImageUpdatesList, PermImageUpdatesRead}, 0),
		routeSurface("route.networks", "/networks", "Networks", AccessScopeModeEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 70),
		routeSurface("route.networks.detail", "/networks/{networkId}", "Network", AccessScopeModeEnvPlusGlobal, []string{PermNetworksList, PermNetworksRead}, 0),
		routeSurface("route.ports", "/networks/ports", "Ports", AccessScopeModeEnvPlusGlobal, []string{PermContainersList}, 0),
		routeSurface("route.networks.topology", "/networks/topology", "Network Topology", AccessScopeModeEnvPlusGlobal, []string{PermNetworksRead}, 0),
		routeSurface("route.volumes", "/volumes", "Volumes", AccessScopeModeEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 60),
		routeSurface("route.volumes.detail", "/volumes/{volumeName}", "Volume", AccessScopeModeEnvPlusGlobal, []string{PermVolumesList, PermVolumesRead}, 0),
		routeSurface("route.swarm", "/swarm", "Swarm", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
		routeSurface("route.swarm.services", "/swarm/services", "Services", AccessScopeModeEnvPlusGlobal, []string{PermSwarmServicesList, PermSwarmServicesRead}, 80),
		routeSurface("route.swarm.services.detail", "/swarm/services/{serviceId}", "Service", AccessScopeModeEnvPlusGlobal, []string{PermSwarmServicesList, PermSwarmServicesRead}, 0),
		routeSurface("route.swarm.nodes", "/swarm/nodes", "Nodes", AccessScopeModeEnvPlusGlobal, []string{PermSwarmNodesList, PermSwarmNodesRead}, 0),
		routeSurface("route.swarm.tasks", "/swarm/tasks", "Tasks", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 0),
		routeSurface("route.swarm.stacks", "/swarm/stacks", "Stacks", AccessScopeModeEnvPlusGlobal, []string{PermSwarmStacksList, PermSwarmStacksRead}, 90),
		routeSurface("route.swarm.stacks.new", "/swarm/stacks/new", "Create stack", AccessScopeModeEnvPlusGlobal, []string{PermSwarmStacksCreate}, 0),
		routeSurface("route.swarm.stacks.detail", "/swarm/stacks/{name}", "Stack", AccessScopeModeEnvPlusGlobal, []string{PermSwarmStacksList, PermSwarmStacksRead}, 0),
		routeSurface("route.swarm.cluster", "/swarm/cluster", "Cluster", AccessScopeModeEnvPlusGlobal, []string{PermSwarmRead}, 100),
		routeSurface("route.swarm.configs", "/swarm/configs", "Configs", AccessScopeModeEnvPlusGlobal, []string{PermSwarmConfigsList, PermSwarmConfigsRead}, 0),
		routeSurface("route.swarm.secrets", "/swarm/secrets", "Secrets", AccessScopeModeEnvPlusGlobal, []string{PermSwarmSecretsList, PermSwarmSecretsRead}, 0),
		routeSurface("route.podman", "/podman/pods", "Podman", AccessScopeModeEnvPlusGlobal, []string{PermPodmanPodsList, PermPodmanPodsRead}, 0),
		routeSurface("route.podman.pods.detail", "/podman/pods/{id}", "Pod", AccessScopeModeEnvPlusGlobal, []string{PermPodmanPodsList, PermPodmanPodsRead}, 0),
		routeSurface("route.podman.quadlets", "/podman/quadlets", "Quadlets", AccessScopeModeEnvPlusGlobal, []string{PermPodmanQuadletsList, PermPodmanQuadletsRead}, 0),
		routeSurface("route.podman.quadlets.detail", "/podman/quadlets/{name}", "Quadlet", AccessScopeModeEnvPlusGlobal, []string{PermPodmanQuadletsList, PermPodmanQuadletsRead}, 0),
		allOfRouteSurface("route.podman.quadlets.new", "/podman/quadlets/new", "Install quadlet", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanQuadletsCreate}),
		allOfRouteSurface("route.podman.kube", "/podman/kube", "Kube", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanKubeCreate}),
		routeSurface("route.podman.secrets", "/podman/secrets", "Podman secrets", AccessScopeModeEnvPlusGlobal, []string{PermPodmanSecretsList, PermPodmanSecretsRead}, 0),
		allOfRouteSurface("route.podman.autoupdate", "/podman/autoupdate", "Podman auto-update", AccessScopeModeEnvPlusGlobal, []string{PermPodmanRead, PermPodmanAutoUpdate}),
		routeSurface("route.events", "/events", "Events", AccessScopeModeGlobalOnly, []string{PermEventsList}, 110),
		routeSurface("route.activities", "", "Activities", AccessScopeModeAnyEffectiveScope, []string{PermActivitiesList, PermActivitiesRead}, 0),
		allOfRouteSurface("route.oidc-role-mappings", "", "OIDC Role Mappings", AccessScopeModeGlobalOnly, AllPermissions()),
		routeSurface("route.projects.templates", "/projects/templates", "Templates", AccessScopeModeGlobalOnly, []string{PermTemplatesList, PermTemplatesRead}, 40),
		allOfRouteSurface("route.projects.templates.create", "/projects/templates/create", "Create template", AccessScopeModeGlobalOnly, []string{PermTemplatesCreate}),
		allOfRouteSurface("route.projects.templates.default", "/projects/templates/default", "Default template", AccessScopeModeGlobalOnly, []string{PermTemplatesUpdate}),
		routeSurface("route.projects.templates.detail", "/projects/templates/{id}", "Template", AccessScopeModeGlobalOnly, []string{PermTemplatesList, PermTemplatesRead}, 0),
		routeSurface("route.images.registries", "/images/registries", "Container Registries", AccessScopeModeGlobalOnly, []string{PermRegistriesList, PermRegistriesRead}, 41),
		allOfRouteSurface("route.images.registries.browse", "/images/registries/{id}", "Registry browser", AccessScopeModeGlobalOnly, []string{PermRegistriesRead, PermRegistriesBrowse}),
		routeSurface("route.projects.variables", "/projects/variables", "Variables", AccessScopeModeGlobalOnly, []string{PermVariablesList, PermVariablesRead}, 42),
		routeSurface("route.gitops.repositories", "/gitops/repositories", "Git Repositories", AccessScopeModeGlobalOnly, []string{PermGitReposList, PermGitReposRead}, 43),

		settingsCategorySurface("apikeys", "/system/api-keys", "API Keys", AccessScopeModeGlobalOnly, []string{PermApiKeysList, PermApiKeysRead}),
		allOfSettingsCategorySurface("authentication", "/system/authentication", "Authentication", AccessScopeModeGlobalOnly, AllPermissions()),
		settingsCategorySurface("notifications", "/system/notifications", "Notifications", AccessScopeModeGlobalOnly, []string{PermNotificationsList, PermNotificationsRead}),
		settingsCategorySurface("roles", "/system/roles", "Roles", AccessScopeModeGlobalOnly, []string{PermRolesList, PermRolesRead}),
		settingsCategorySurface("s3destinations", "/system/backups/s3", "S3 Destinations", AccessScopeModeGlobalOnly, []string{PermS3DestinationsList, PermS3DestinationsRead}),
		settingsCategorySurface("systembackups", "/system/backups", "Backups", AccessScopeModeGlobalOnly, []string{PermSystemBackupsList, PermSystemBackupsRead}),
		allOfRouteSurface("route.settings.roles.new", "/system/roles/new", "Create role", AccessScopeModeGlobalOnly, AllPermissions()),
		allOfRouteSurface("route.settings.roles.detail", "/system/roles/{id}", "Role", AccessScopeModeGlobalOnly, AllPermissions()),
		settingsCategorySurface("users", "/system/users", "Users", AccessScopeModeGlobalOnly, []string{PermUsersList, PermUsersRead}),
		settingsCategorySurface("webhooks", "/system/webhooks", "Webhooks", AccessScopeModeEnvPlusGlobal, []string{PermWebhooksList}),
		settingsCategorySurface("diagnostics", "/system/diagnostics", "Diagnostics", AccessScopeModeGlobalOnly, []string{PermDiagnosticsRead}),
		settingsCategorySurface("storage", "/settings/storage", "Storage & Limits", AccessScopeModeEnvPlusGlobal, []string{PermEnvironmentSettingsRead}),
		settingsCategorySurface("docker", "/settings/docker", "Docker", AccessScopeModeEnvPlusGlobal, []string{PermEnvironmentSettingsRead}),
		settingsCategorySurface("security", "/settings/security", "Security", AccessScopeModeEnvPlusGlobal, []string{PermEnvironmentSettingsRead}),
		settingsCategorySurface("timeouts", "/settings/timeouts", "Timeouts", AccessScopeModeEnvPlusGlobal, []string{PermEnvironmentSettingsRead}),
		settingsCategorySurface("activity", "/settings/activity", "Activity", AccessScopeModeEnvPlusGlobal, []string{PermEnvironmentSettingsRead}),
		settingsCategorySurface("build", "/settings/builds", "Builds", AccessScopeModeEnvPlusGlobal, []string{PermEnvironmentSettingsRead}),
		allOfSettingsCategorySurface("jobschedule", "/settings/automations", "Automations", AccessScopeModeEnvPlusGlobal, []string{PermJobsList, PermEnvironmentSettingsRead}),
	}

	accessSurfacesByID = buildAccessSurfaceIndex(accessSurfaces)
)

// AccessSurfaces returns a defensive copy of every backend-owned access
// surface in stable evaluation order.
func AccessSurfaces() []AccessSurface {
	out := make([]AccessSurface, len(accessSurfaces))
	for i, surface := range accessSurfaces {
		surface.Permissions = slices.Clone(surface.Permissions)
		surface.Children = slices.Clone(surface.Children)
		out[i] = surface
	}
	return out
}

// CanAccessSurface evaluates backend-owned UI reachability metadata for the
// caller. It is for advisory UX only; middleware and handlers still enforce
// permissions on actual API requests.
func CanAccessSurface(ps *PermissionSet, surfaceID, envID string) bool {
	if ps == nil {
		return false
	}
	visiting := make(map[string]struct{})
	var canAccess func(id string) bool
	canAccess = func(id string) bool {
		if _, ok := visiting[id]; ok {
			return false
		}
		surface, ok := accessSurfacesByID[id]
		if !ok {
			return false
		}

		switch surface.AccessMode {
		case AccessModeAnyChild:
			visiting[id] = struct{}{}
			defer delete(visiting, id)
			return slices.ContainsFunc(surface.Children, canAccess)
		case AccessModePermissions:
			return allowsSurfacePermissions(ps, surface, envID)
		default:
			return false
		}
	}
	return canAccess(surfaceID)
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

func allOfSettingsCategorySurface(categoryID, url, label, scopeMode string, permissions []string) AccessSurface {
	surface := settingsCategorySurface(categoryID, url, label, scopeMode, permissions)
	surface.MatchMode = AccessMatchModeAllOf
	return surface
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
