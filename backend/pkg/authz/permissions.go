// Package authz defines the permission taxonomy and authorization primitives
// used across Arcane handlers. Permissions are strings of the form
// "<resource>:<action>" and are classified as either org-level (require a
// globally-scoped role) or env-scoped (resolved against the environment ID
// from the request path).
package authz

const (
	// Built-in role IDs. Stable across migrations so other code may reference them
	// safely. All six built-in roles are seeded by migration 054_add_rbac.
	BuiltInRoleAdmin         = "role_admin"
	BuiltInRoleEditor        = "role_editor"
	BuiltInRoleNoShellEditor = "role_no_shell_editor"
	BuiltInRoleDeployer      = "role_deployer"
	BuiltInRoleMonitor       = "role_monitor"
	BuiltInRoleViewer        = "role_viewer"
	// Org-level permissions (require a global-scope role assignment).

	PermUsersList                = "users:list"
	PermUsersRead                = "users:read"
	PermUsersCreate              = "users:create"
	PermUsersUpdate              = "users:update"
	PermUsersDelete              = "users:delete"
	PermRolesList                = "roles:list"
	PermRolesRead                = "roles:read"
	PermApiKeysList              = "apikeys:list"
	PermApiKeysRead              = "apikeys:read"
	PermApiKeysCreate            = "apikeys:create"
	PermApiKeysUpdate            = "apikeys:update"
	PermApiKeysDelete            = "apikeys:delete"
	PermFederatedList            = "federated:list"
	PermFederatedRead            = "federated:read"
	PermFederatedCreate          = "federated:create"
	PermFederatedUpdate          = "federated:update"
	PermFederatedDelete          = "federated:delete"
	PermSettingsRead             = "settings:read"
	PermSettingsWrite            = "settings:write"
	PermEnvironmentsList         = "environments:list"
	PermEnvironmentsRead         = "environments:read"
	PermEnvironmentsCreate       = "environments:create"
	PermEnvironmentsUpdate       = "environments:update"
	PermEnvironmentsDelete       = "environments:delete"
	PermEnvironmentsPair         = "environments:pair"
	PermEnvironmentsSync         = "environments:sync"
	PermRegistriesList           = "registries:list"
	PermRegistriesRead           = "registries:read"
	PermRegistriesCreate         = "registries:create"
	PermRegistriesUpdate         = "registries:update"
	PermRegistriesDelete         = "registries:delete"
	PermRegistriesTest           = "registries:test"
	PermRegistriesBrowse         = "registries:browse"
	PermRegistriesDeleteTags     = "registries:delete-tags"
	PermTemplatesList            = "templates:list"
	PermTemplatesRead            = "templates:read"
	PermTemplatesCreate          = "templates:create"
	PermTemplatesUpdate          = "templates:update"
	PermTemplatesDelete          = "templates:delete"
	PermVariablesRead            = "variables:read"
	PermVariablesCreate          = "variables:create"
	PermVariablesUpdate          = "variables:update"
	PermVariablesDelete          = "variables:delete"
	PermVariablesSync            = "variables:sync"
	PermGitReposList             = "git-repositories:list"
	PermGitReposRead             = "git-repositories:read"
	PermGitReposCreate           = "git-repositories:create"
	PermGitReposUpdate           = "git-repositories:update"
	PermGitReposDelete           = "git-repositories:delete"
	PermGitReposTest             = "git-repositories:test"
	PermGitReposSync             = "git-repositories:sync"
	PermS3DestinationsList       = "s3-destinations:list"
	PermS3DestinationsRead       = "s3-destinations:read"
	PermS3DestinationsCreate     = "s3-destinations:create"
	PermS3DestinationsUpdate     = "s3-destinations:update"
	PermS3DestinationsDelete     = "s3-destinations:delete"
	PermS3DestinationsTest       = "s3-destinations:test"
	PermS3DestinationsSync       = "s3-destinations:sync"
	PermSystemBackupsRead        = "system-backups:read"
	PermSystemBackupsManage      = "system-backups:manage"
	PermSystemBackupsRestore     = "system-backups:restore"
	PermSystemBackupsRecoveryKey = "system-backups:recovery-key"
	PermEventsRead               = "events:read"
	PermEventsDelete             = "events:delete"
	PermCustomizeManage          = "customize:manage"
	PermNotificationsManage      = "notifications:manage"
	PermDiagnosticsRead          = "diagnostics:read"
	// Env-scoped permissions (resolved against the {id} env ID in the path).

	PermContainersList          = "containers:list"
	PermContainersRead          = "containers:read"
	PermContainersLogs          = "containers:logs"
	PermContainersCreate        = "containers:create"
	PermContainersStart         = "containers:start"
	PermContainersStop          = "containers:stop"
	PermContainersRestart       = "containers:restart"
	PermContainersRedeploy      = "containers:redeploy"
	PermContainersEdit          = "containers:edit"
	PermContainersKill          = "containers:kill"
	PermContainersPause         = "containers:pause"
	PermContainersDelete        = "containers:delete"
	PermContainersExec          = "containers:exec"
	PermContainersAutoUpdate    = "containers:autoupdate"
	PermProjectsList            = "projects:list"
	PermProjectsRead            = "projects:read"
	PermProjectsLogs            = "projects:logs"
	PermProjectsCreate          = "projects:create"
	PermProjectsUpdate          = "projects:update"
	PermProjectsDeploy          = "projects:deploy"
	PermProjectsDown            = "projects:down"
	PermProjectsRestart         = "projects:restart"
	PermProjectsDelete          = "projects:delete"
	PermProjectsArchive         = "projects:archive"
	PermImagesList              = "images:list"
	PermImagesRead              = "images:read"
	PermImagesPull              = "images:pull"
	PermImagesPush              = "images:push"
	PermImagesBuild             = "images:build"
	PermImagesTag               = "images:tag"
	PermImagesCommit            = "images:commit"
	PermImagesPrune             = "images:prune"
	PermImagesDelete            = "images:delete"
	PermImagesUpload            = "images:upload"
	PermImagesPatch             = "images:patch"
	PermVolumesList             = "volumes:list"
	PermVolumesRead             = "volumes:read"
	PermVolumesCreate           = "volumes:create"
	PermVolumesRename           = "volumes:rename"
	PermVolumesDelete           = "volumes:delete"
	PermVolumesPrune            = "volumes:prune"
	PermVolumesUpload           = "volumes:upload"
	PermVolumesBackup           = "volumes:backup"
	PermNetworksList            = "networks:list"
	PermNetworksRead            = "networks:read"
	PermNetworksCreate          = "networks:create"
	PermNetworksDelete          = "networks:delete"
	PermNetworksPrune           = "networks:prune"
	PermNetworksConnect         = "networks:connect"
	PermNetworksDisconnect      = "networks:disconnect"
	PermSwarmRead               = "swarm:read"
	PermSwarmInit               = "swarm:init"
	PermSwarmJoin               = "swarm:join"
	PermSwarmLeave              = "swarm:leave"
	PermSwarmSpec               = "swarm:spec"
	PermSwarmNodes              = "swarm:nodes"
	PermSwarmServices           = "swarm:services"
	PermSwarmServicesLogs       = "swarm:services:logs"
	PermSwarmStacks             = "swarm:stacks"
	PermSwarmConfigs            = "swarm:configs"
	PermSwarmSecrets            = "swarm:secrets"
	PermSwarmUnlock             = "swarm:unlock"
	PermPodmanRead              = "podman:read"
	PermPodmanPods              = "podman:pods"
	PermPodmanQuadlets          = "podman:quadlets"
	PermPodmanQuadletsLifecycle = "podman:quadlets:lifecycle"
	PermPodmanKube              = "podman:kube"
	PermPodmanSecrets           = "podman:secrets"
	PermPodmanAutoUpdate        = "podman:autoupdate"
	PermGitOpsList              = "gitops:list"
	PermGitOpsRead              = "gitops:read"
	PermGitOpsCreate            = "gitops:create"
	PermGitOpsUpdate            = "gitops:update"
	PermGitOpsDelete            = "gitops:delete"
	PermGitOpsSync              = "gitops:sync"
	PermGitOpsLifecycle         = "gitops:lifecycle"
	PermGitOpsBackup            = "gitops:backup"
	PermWebhooksList            = "webhooks:list"
	PermWebhooksCreate          = "webhooks:create"
	PermWebhooksUpdate          = "webhooks:update"
	PermWebhooksDelete          = "webhooks:delete"
	PermJobsManage              = "jobs:manage"
	PermSystemRead              = "system:read"
	PermSystemPrune             = "system:prune"
	PermSystemUpgrade           = "system:upgrade"
	PermImageUpdatesRead        = "image-updates:read"
	PermImageUpdatesCheck       = "image-updates:check"
	PermVulnsRead               = "vulnerabilities:read"
	PermVulnsScan               = "vulnerabilities:scan"
	PermVulnsManage             = "vulnerabilities:manage"
	PermBuildWorkspacesManage   = "build-workspaces:manage"
	PermActivitiesRead          = "activities:read"
	PermActivitiesCancel        = "activities:cancel"
	PermActivitiesDelete        = "activities:delete"
)

var (
	orgLevelPermissions = buildOrgLevelPermissions()

	// allPermissionsSet is the canonical exact-set lookup of every defined
	// permission, built once from AllPermissions(). Anchors IsKnownPermission and
	// IsEnvScoped to exact membership so PermissionSet.IsGlobalAdmin can rely on
	// "ps.Global ⊆ allPermissionsSet" as a true invariant.
	allPermissionsSet = func() map[string]struct{} {
		all := AllPermissions()
		m := make(map[string]struct{}, len(all))
		for _, p := range all {
			m[p] = struct{}{}
		}
		return m
	}()

	// totalPermissionsCount caches len(allPermissionsSet) so callers on the auth
	// hot path (notably PermissionSet.IsGlobalAdmin) don't re-allocate and walk a
	// ~100-element slice on every authenticated request.
	totalPermissionsCount = len(allPermissionsSet)
)

func buildOrgLevelPermissions() map[string]struct{} {
	out := make(map[string]struct{})
	for _, resource := range permissionCatalog {
		if resource.Scope != PermissionScopeGlobal {
			continue
		}
		for _, action := range resource.Actions {
			out[action.Permission] = struct{}{}
		}
	}
	return out
}

// IsOrgLevel reports whether the given permission requires a globally-scoped
// role assignment (and applies only to org-level endpoints).
func IsOrgLevel(perm string) bool {
	_, ok := orgLevelPermissions[perm]
	return ok
}

// IsEnvScoped reports whether the given permission is resolved against an
// environment ID. Returns false for permissions not in AllPermissions().
func IsEnvScoped(perm string) bool {
	if _, ok := allPermissionsSet[perm]; !ok {
		return false
	}
	return !IsOrgLevel(perm)
}

// TotalPermissionsCount returns the number of distinct permission constants
// the package defines. Computed once at init; cheap to call repeatedly.
func TotalPermissionsCount() int { return totalPermissionsCount }

// AllPermissions returns every recognized permission constant. Used to seed
// the Admin built-in role and to validate role definitions.
func AllPermissions() []string {
	var count int
	for _, resource := range permissionCatalog {
		count += len(resource.Actions)
	}
	out := make([]string, 0, count)
	for _, resource := range permissionCatalog {
		for _, action := range resource.Actions {
			out = append(out, action.Permission)
		}
	}
	return out
}

// IsKnownPermission reports whether perm matches any defined permission
// constant. Used to reject role definitions referencing unknown permissions.
func IsKnownPermission(perm string) bool {
	_, ok := allPermissionsSet[perm]
	return ok
}

// BuiltInEditorPermissions returns the permission set for the Editor built-in
// role: read+write on Docker resources and read on most org-level resources.
// Excludes user/role/key management and settings writes.
func BuiltInEditorPermissions() []string {
	return []string{
		// Read on org-level
		PermUsersList, PermUsersRead,
		PermRolesList, PermRolesRead,
		PermApiKeysList, PermApiKeysRead,
		PermFederatedList, PermFederatedRead,
		PermSettingsRead,
		PermEnvironmentsList, PermEnvironmentsRead, PermEnvironmentsSync,
		PermRegistriesList, PermRegistriesRead, PermRegistriesBrowse,
		PermTemplatesList, PermTemplatesRead, PermTemplatesCreate, PermTemplatesUpdate, PermTemplatesDelete,
		PermVariablesRead, PermVariablesCreate, PermVariablesUpdate, PermVariablesDelete, PermVariablesSync,
		PermGitReposList, PermGitReposRead,
		// Editor can run volume backups, so it needs to pick a destination.
		PermS3DestinationsList, PermS3DestinationsRead,
		PermEventsRead,
		// Full env-scoped Docker management
		PermContainersList,
		PermContainersRead,
		PermContainersLogs,
		PermContainersCreate,
		PermContainersStart,
		PermContainersStop,
		PermContainersRestart,
		PermContainersRedeploy,
		PermContainersEdit,
		PermContainersKill,
		PermContainersPause,
		PermContainersDelete,
		PermContainersExec,
		PermContainersAutoUpdate,

		PermProjectsList, PermProjectsRead, PermProjectsLogs, PermProjectsCreate, PermProjectsUpdate, PermProjectsDeploy, PermProjectsDown, PermProjectsRestart, PermProjectsDelete, PermProjectsArchive,
		PermImagesList, PermImagesRead, PermImagesPull, PermImagesPush, PermImagesBuild, PermImagesTag, PermImagesCommit, PermImagesPrune, PermImagesDelete, PermImagesUpload, PermImagesPatch,
		PermVolumesList, PermVolumesRead, PermVolumesCreate, PermVolumesRename, PermVolumesDelete, PermVolumesPrune, PermVolumesUpload, PermVolumesBackup,
		PermNetworksList, PermNetworksRead, PermNetworksCreate, PermNetworksDelete, PermNetworksPrune, PermNetworksConnect, PermNetworksDisconnect,
		PermSwarmRead, PermSwarmSpec, PermSwarmNodes, PermSwarmServices, PermSwarmServicesLogs, PermSwarmStacks, PermSwarmConfigs, PermSwarmSecrets,
		PermPodmanRead, PermPodmanPods, PermPodmanQuadlets, PermPodmanQuadletsLifecycle, PermPodmanKube, PermPodmanSecrets, PermPodmanAutoUpdate,
		PermGitOpsList, PermGitOpsRead, PermGitOpsCreate, PermGitOpsUpdate, PermGitOpsDelete, PermGitOpsSync, PermGitOpsBackup,
		PermWebhooksList, PermWebhooksCreate, PermWebhooksUpdate, PermWebhooksDelete,
		PermJobsManage, PermNotificationsManage,
		PermSystemRead, PermSystemPrune,
		PermImageUpdatesRead, PermImageUpdatesCheck,
		PermVulnsRead, PermVulnsScan, PermVulnsManage,
		PermBuildWorkspacesManage,
		PermActivitiesRead, PermActivitiesCancel, PermActivitiesDelete,
	}
}

// BuiltInDeployerPermissions returns the permission set for the Deployer
// built-in role: container/project lifecycle and read-only on everything else.
// Cannot create or delete resources; cannot manage settings/users/roles/keys.
func BuiltInDeployerPermissions() []string {
	return []string{
		PermEnvironmentsList, PermEnvironmentsRead,
		PermRegistriesList, PermRegistriesRead,
		PermTemplatesList, PermTemplatesRead,
		PermVariablesRead,
		PermEventsRead,
		PermContainersList,
		PermContainersRead,
		PermContainersLogs,
		PermContainersStart,
		PermContainersStop,
		PermContainersRestart,
		PermContainersRedeploy,
		PermContainersEdit,
		PermContainersKill,
		PermContainersPause,

		PermProjectsList, PermProjectsRead, PermProjectsLogs, PermProjectsDeploy, PermProjectsDown, PermProjectsRestart,
		PermImagesList, PermImagesRead, PermImagesPull, PermImagesTag, PermImagesCommit,
		PermVolumesList, PermVolumesRead,
		PermNetworksList, PermNetworksRead,
		PermSwarmRead, PermSwarmServicesLogs,
		PermPodmanRead, PermPodmanQuadletsLifecycle, PermPodmanAutoUpdate,
		PermGitOpsList, PermGitOpsRead, PermGitOpsSync,
		PermSystemRead,
		PermImageUpdatesRead, PermImageUpdatesCheck,
		PermVulnsRead,
		PermActivitiesRead, PermActivitiesCancel,
	}
}

// BuiltInViewerPermissions returns the permission set for the Viewer built-in
// role: read-only access across every resource.
func BuiltInViewerPermissions() []string {
	return []string{
		PermUsersList, PermUsersRead,
		PermRolesList, PermRolesRead,
		PermApiKeysList, PermApiKeysRead,
		PermFederatedList, PermFederatedRead,
		PermSettingsRead,
		PermEnvironmentsList, PermEnvironmentsRead,
		PermRegistriesList, PermRegistriesRead,
		PermTemplatesList, PermTemplatesRead,
		PermVariablesRead,
		PermGitReposList, PermGitReposRead,
		PermS3DestinationsList, PermS3DestinationsRead,
		PermEventsRead,
		PermContainersList, PermContainersRead, PermContainersLogs,
		PermProjectsList, PermProjectsRead, PermProjectsLogs,
		PermImagesList, PermImagesRead,
		PermVolumesList, PermVolumesRead,
		PermNetworksList, PermNetworksRead,
		PermSwarmRead, PermSwarmServicesLogs,
		PermPodmanRead,
		PermGitOpsList, PermGitOpsRead,
		PermWebhooksList,
		PermSystemRead,
		PermImageUpdatesRead,
		PermVulnsRead,
		PermActivitiesRead,
	}
}

// BuiltInMonitorPermissions returns the permission set for the Monitor
// built-in role: observability-only access — read Docker resources, view logs,
// dashboards, and events. No mutations, no exec, no user/role/settings access.
func BuiltInMonitorPermissions() []string {
	return []string{
		PermEnvironmentsList, PermEnvironmentsRead,
		PermEventsRead,
		PermContainersList, PermContainersRead, PermContainersLogs,
		PermProjectsList, PermProjectsRead, PermProjectsLogs,
		PermImagesList, PermImagesRead,
		PermVolumesList, PermVolumesRead,
		PermNetworksList, PermNetworksRead,
		PermSwarmRead, PermSwarmServicesLogs,
		PermPodmanRead,
		PermSystemRead,
		PermImageUpdatesRead,
		PermVulnsRead,
		PermActivitiesRead,
	}
}

// BuiltInNoShellEditorPermissions returns the Editor permission set minus
// PermContainersExec. For teams that want full Docker management but no
// interactive shell access into running containers.
func BuiltInNoShellEditorPermissions() []string {
	src := BuiltInEditorPermissions()
	out := make([]string, 0, len(src))
	for _, p := range src {
		if p != PermContainersExec {
			out = append(out, p)
		}
	}
	return out
}
