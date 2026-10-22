// Package authz defines the permission taxonomy and authorization primitives
// used across Arcane handlers. Permissions are strings of the form
// "<resource>:<action>" and are classified as either org-level (require a
// globally-scoped role) or env-scoped (resolved against the environment ID
// from the request path).
package authz

import "slices"

const (
	PermAll = "*"

	// Built-in role IDs. Stable so other code and foreign keys may reference
	// them; their definitions come from BuiltInRoles().

	BuiltInRoleAdmin         = "role_admin"
	BuiltInRoleEditor        = "role_editor"
	BuiltInRoleNoShellEditor = "role_no_shell_editor"
	BuiltInRoleDeployer      = "role_deployer"
	BuiltInRoleMonitor       = "role_monitor"
	BuiltInRoleViewer        = "role_viewer"

	// Org-level permissions (require a global-scope role assignment).

	PermUsersList   = "users:list"
	PermUsersRead   = "users:read"
	PermUsersCreate = "users:create"
	PermUsersUpdate = "users:update"
	PermUsersDelete = "users:delete"

	// Role, binding, group, and OIDC mapping permissions are delegable: holders
	// may only grant permissions, scopes, and selectors they hold themselves
	// (see CanDefineRole and CanBind).
	PermRolesList   = "roles:list"
	PermRolesRead   = "roles:read"
	PermRolesCreate = "roles:create"
	PermRolesUpdate = "roles:update"
	PermRolesDelete = "roles:delete"

	PermBindingsList   = "bindings:list"
	PermBindingsCreate = "bindings:create"
	PermBindingsDelete = "bindings:delete"

	PermGroupsList    = "groups:list"
	PermGroupsRead    = "groups:read"
	PermGroupsCreate  = "groups:create"
	PermGroupsUpdate  = "groups:update"
	PermGroupsDelete  = "groups:delete"
	PermGroupsMembers = "groups:members"

	PermOidcMappingsList   = "oidcmappings:list"
	PermOidcMappingsCreate = "oidcmappings:create"
	PermOidcMappingsUpdate = "oidcmappings:update"
	PermOidcMappingsDelete = "oidcmappings:delete"

	PermAuditRead   = "audit:read"
	PermAuditExport = "audit:export"

	PermApiKeysList   = "apikeys:list"
	PermApiKeysRead   = "apikeys:read"
	PermApiKeysCreate = "apikeys:create"
	PermApiKeysUpdate = "apikeys:update"
	PermApiKeysDelete = "apikeys:delete"

	PermFederatedList   = "federated:list"
	PermFederatedRead   = "federated:read"
	PermFederatedCreate = "federated:create"
	PermFederatedUpdate = "federated:update"
	PermFederatedDelete = "federated:delete"

	PermSettingsRead  = "settings:read"
	PermSettingsWrite = "settings:write"

	PermEnvironmentsList   = "environments:list"
	PermEnvironmentsRead   = "environments:read"
	PermEnvironmentsCreate = "environments:create"
	PermEnvironmentsUpdate = "environments:update"
	PermEnvironmentsDelete = "environments:delete"
	PermEnvironmentsPair   = "environments:pair"
	PermEnvironmentsSync   = "environments:sync"

	PermRegistriesList       = "registries:list"
	PermRegistriesRead       = "registries:read"
	PermRegistriesCreate     = "registries:create"
	PermRegistriesUpdate     = "registries:update"
	PermRegistriesDelete     = "registries:delete"
	PermRegistriesTest       = "registries:test"
	PermRegistriesBrowse     = "registries:browse"
	PermRegistriesDeleteTags = "registries:delete-tags"

	PermTemplatesList   = "templates:list"
	PermTemplatesRead   = "templates:read"
	PermTemplatesCreate = "templates:create"
	PermTemplatesUpdate = "templates:update"
	PermTemplatesDelete = "templates:delete"

	PermVariablesRead   = "variables:read"
	PermVariablesCreate = "variables:create"
	PermVariablesUpdate = "variables:update"
	PermVariablesDelete = "variables:delete"
	PermVariablesSync   = "variables:sync"

	PermGitReposList   = "git-repositories:list"
	PermGitReposRead   = "git-repositories:read"
	PermGitReposCreate = "git-repositories:create"
	PermGitReposUpdate = "git-repositories:update"
	PermGitReposDelete = "git-repositories:delete"
	PermGitReposTest   = "git-repositories:test"
	PermGitReposSync   = "git-repositories:sync"

	// PermS3DestinationsList and its siblings gate S3 backup destinations,
	// which hold credentials and are manager-owned, so they are granted
	// separately from general settings, like container registries.
	PermS3DestinationsList   = "s3-destinations:list"
	PermS3DestinationsRead   = "s3-destinations:read"
	PermS3DestinationsCreate = "s3-destinations:create"
	PermS3DestinationsUpdate = "s3-destinations:update"
	PermS3DestinationsDelete = "s3-destinations:delete"
	PermS3DestinationsTest   = "s3-destinations:test"
	PermS3DestinationsSync   = "s3-destinations:sync"

	// PermSystemBackupsRead and siblings gate Arcane's own system backups.
	// Every route additionally requires a global admin; the dedicated grants
	// exist so admin-owned scoped API keys can be limited to this surface
	// instead of carrying settings:read/write. Restore is separate because it
	// replaces the whole instance.
	PermSystemBackupsRead    = "system-backups:read"
	PermSystemBackupsManage  = "system-backups:manage"
	PermSystemBackupsRestore = "system-backups:restore"
	// PermSystemBackupsRecoveryKey is separate from manage: regenerating the
	// key invalidates access to the existing repositories.
	PermSystemBackupsRecoveryKey = "system-backups:recovery-key"

	PermEventsRead          = "events:read"
	PermEventsDelete        = "events:delete"
	PermCustomizeManage     = "customize:manage"
	PermNotificationsManage = "notifications:manage"

	// PermDiagnosticsRead gates the admin-only runtime diagnostics surface
	// (runtime/memory/GC stats, WebSocket metrics, pprof profiles, and the live
	// backend log tail). Global-scoped; seeded only into the Admin role.
	PermDiagnosticsRead = "diagnostics:read"

	// Env-scoped permissions (resolved against the {id} env ID in the path).

	PermContainersList       = "containers:list"
	PermContainersRead       = "containers:read"
	PermContainersLogs       = "containers:logs"
	PermContainersCreate     = "containers:create"
	PermContainersStart      = "containers:start"
	PermContainersStop       = "containers:stop"
	PermContainersRestart    = "containers:restart"
	PermContainersRedeploy   = "containers:redeploy"
	PermContainersEdit       = "containers:edit"
	PermContainersKill       = "containers:kill"
	PermContainersPause      = "containers:pause"
	PermContainersDelete     = "containers:delete"
	PermContainersExec       = "containers:exec"
	PermContainersAutoUpdate = "containers:autoupdate"

	PermProjectsList    = "projects:list"
	PermProjectsRead    = "projects:read"
	PermProjectsLogs    = "projects:logs"
	PermProjectsCreate  = "projects:create"
	PermProjectsUpdate  = "projects:update"
	PermProjectsDeploy  = "projects:deploy"
	PermProjectsDown    = "projects:down"
	PermProjectsRestart = "projects:restart"
	PermProjectsDelete  = "projects:delete"
	PermProjectsArchive = "projects:archive"

	PermImagesList   = "images:list"
	PermImagesRead   = "images:read"
	PermImagesPull   = "images:pull"
	PermImagesBuild  = "images:build"
	PermImagesTag    = "images:tag"
	PermImagesCommit = "images:commit"
	PermImagesPrune  = "images:prune"
	PermImagesDelete = "images:delete"
	PermImagesUpload = "images:upload"
	PermImagesPatch  = "images:patch"

	PermVolumesList   = "volumes:list"
	PermVolumesRead   = "volumes:read"
	PermVolumesCreate = "volumes:create"
	PermVolumesRename = "volumes:rename"
	PermVolumesDelete = "volumes:delete"
	PermVolumesPrune  = "volumes:prune"
	PermVolumesUpload = "volumes:upload"
	PermVolumesBackup = "volumes:backup"

	PermNetworksList       = "networks:list"
	PermNetworksRead       = "networks:read"
	PermNetworksCreate     = "networks:create"
	PermNetworksDelete     = "networks:delete"
	PermNetworksPrune      = "networks:prune"
	PermNetworksConnect    = "networks:connect"
	PermNetworksDisconnect = "networks:disconnect"

	PermSwarmRead         = "swarm:read"
	PermSwarmInit         = "swarm:init"
	PermSwarmJoin         = "swarm:join"
	PermSwarmLeave        = "swarm:leave"
	PermSwarmSpec         = "swarm:spec"
	PermSwarmNodes        = "swarm:nodes"
	PermSwarmServices     = "swarm:services"
	PermSwarmServicesLogs = "swarm:services:logs"
	PermSwarmStacks       = "swarm:stacks"
	PermSwarmConfigs      = "swarm:configs"
	PermSwarmSecrets      = "swarm:secrets"
	PermSwarmUnlock       = "swarm:unlock"

	PermPodmanRead              = "podman:read"
	PermPodmanPods              = "podman:pods"
	PermPodmanQuadlets          = "podman:quadlets"
	PermPodmanQuadletsLifecycle = "podman:quadlets:lifecycle"
	PermPodmanKube              = "podman:kube"
	PermPodmanSecrets           = "podman:secrets"
	PermPodmanAutoUpdate        = "podman:autoupdate"

	PermGitOpsList   = "gitops:list"
	PermGitOpsRead   = "gitops:read"
	PermGitOpsCreate = "gitops:create"
	PermGitOpsUpdate = "gitops:update"
	PermGitOpsDelete = "gitops:delete"
	PermGitOpsSync   = "gitops:sync"
	// PermGitOpsLifecycle gates configuring a sync's pre-deploy lifecycle hook
	// (script + runner image + env + host bind mounts + network mode), which is
	// effectively arbitrary code execution on the host. It is intentionally
	// separate from gitops:create / gitops:update so that holding those broader
	// permissions (e.g. the built-in Editor role) does not by itself grant the
	// ability to author hooks; seeded only into the Admin built-in role.
	PermGitOpsLifecycle = "gitops:lifecycle"
	// PermGitOpsBackup gates the "Back up to Git" feature. Backup-only routes
	// (preview, history, resolve) require it alone; the shared sync routes
	// require it in addition to the matching gitops:* permission when the sync
	// is a backup. Backups push project files (optionally the .env) to a
	// repository, so holding the broader gitops:* permissions alone is not enough.
	PermGitOpsBackup = "gitops:backup"

	PermWebhooksList   = "webhooks:list"
	PermWebhooksCreate = "webhooks:create"
	PermWebhooksUpdate = "webhooks:update"
	PermWebhooksDelete = "webhooks:delete"

	PermJobsManage    = "jobs:manage"
	PermDashboardRead = "dashboard:read"

	PermSystemRead    = "system:read"
	PermSystemPrune   = "system:prune"
	PermSystemUpgrade = "system:upgrade"

	PermImageUpdatesRead  = "image-updates:read"
	PermImageUpdatesCheck = "image-updates:check"

	PermVulnsRead   = "vulnerabilities:read"
	PermVulnsScan   = "vulnerabilities:scan"
	PermVulnsManage = "vulnerabilities:manage"

	PermBuildWorkspacesManage = "build-workspaces:manage"

	PermActivitiesRead   = "activities:read"
	PermActivitiesCancel = "activities:cancel"
	PermActivitiesDelete = "activities:delete"
)

// orgLevelPermissions is derived from the authz permission catalog. Env-scoped
// permissions are everything in the catalog that is not in this set.
var orgLevelPermissions = buildOrgLevelPermissionsInternal()

func buildOrgLevelPermissionsInternal() map[string]struct{} {
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

// allPermissionsSet is the canonical exact-set lookup of every defined
// permission, built once from AllPermissions(). Anchors IsKnownPermission and
// IsEnvScoped to exact membership.
var allPermissionsSet = func() map[string]struct{} {
	all := AllPermissions()
	m := make(map[string]struct{}, len(all))
	for _, p := range all {
		m[p] = struct{}{}
	}
	return m
}()

// totalPermissionsCount caches len(allPermissionsSet) so callers don't
// re-allocate and walk the full permission list.
var totalPermissionsCount = len(allPermissionsSet)

// TotalPermissionsCount returns the number of distinct permission constants
// the package defines. Computed once at init; cheap to call repeatedly.
func TotalPermissionsCount() int { return totalPermissionsCount }

// AllPermissions returns every recognized permission constant. Used to
// validate role definitions. PermAll is not included.
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

type RoleDefinition struct {
	ID          string
	Name        string
	Description string
	Permissions []string
	BuiltIn     bool
}

var builtInEditorPermissions = []string{
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
	PermImagesList, PermImagesRead, PermImagesPull, PermImagesBuild, PermImagesTag, PermImagesCommit, PermImagesPrune, PermImagesDelete, PermImagesUpload, PermImagesPatch,
	PermVolumesList, PermVolumesRead, PermVolumesCreate, PermVolumesRename, PermVolumesDelete, PermVolumesPrune, PermVolumesUpload, PermVolumesBackup,
	PermNetworksList, PermNetworksRead, PermNetworksCreate, PermNetworksDelete, PermNetworksPrune, PermNetworksConnect, PermNetworksDisconnect,
	PermSwarmRead, PermSwarmSpec, PermSwarmNodes, PermSwarmServices, PermSwarmServicesLogs, PermSwarmStacks, PermSwarmConfigs, PermSwarmSecrets,
	PermPodmanRead, PermPodmanPods, PermPodmanQuadlets, PermPodmanQuadletsLifecycle, PermPodmanKube, PermPodmanSecrets, PermPodmanAutoUpdate,
	PermGitOpsList, PermGitOpsRead, PermGitOpsCreate, PermGitOpsUpdate, PermGitOpsDelete, PermGitOpsSync, PermGitOpsBackup,
	PermWebhooksList, PermWebhooksCreate, PermWebhooksUpdate, PermWebhooksDelete,
	PermJobsManage, PermNotificationsManage, PermDashboardRead,
	PermSystemRead, PermSystemPrune,
	PermImageUpdatesRead, PermImageUpdatesCheck,
	PermVulnsRead, PermVulnsScan, PermVulnsManage,
	PermBuildWorkspacesManage,
	PermActivitiesRead, PermActivitiesCancel, PermActivitiesDelete,
}

var builtInDeployerPermissions = []string{
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
	PermDashboardRead,
	PermSystemRead,
	PermImageUpdatesRead, PermImageUpdatesCheck,
	PermVulnsRead,
	PermActivitiesRead, PermActivitiesCancel,
}

var builtInViewerPermissions = []string{
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
	PermDashboardRead,
	PermSystemRead,
	PermImageUpdatesRead,
	PermVulnsRead,
	PermActivitiesRead,
}

var builtInMonitorPermissions = []string{
	PermEnvironmentsList, PermEnvironmentsRead,
	PermEventsRead,
	PermContainersList, PermContainersRead, PermContainersLogs,
	PermProjectsList, PermProjectsRead, PermProjectsLogs,
	PermImagesList, PermImagesRead,
	PermVolumesList, PermVolumesRead,
	PermNetworksList, PermNetworksRead,
	PermSwarmRead, PermSwarmServicesLogs,
	PermPodmanRead,
	PermDashboardRead,
	PermSystemRead,
	PermImageUpdatesRead,
	PermVulnsRead,
	PermActivitiesRead,
}

func BuiltInRoles() []RoleDefinition {
	return []RoleDefinition{
		{
			ID:          BuiltInRoleAdmin,
			Name:        "Admin",
			Description: "Full administrative access",
			Permissions: []string{PermAll},
			BuiltIn:     true,
		},
		{
			ID:          BuiltInRoleEditor,
			Name:        "Editor",
			Description: "Read and write on Docker resources",
			Permissions: slices.Clone(builtInEditorPermissions),
			BuiltIn:     true,
		},
		{
			ID:          BuiltInRoleNoShellEditor,
			Name:        "No-Shell Editor",
			Description: "Editor without interactive container shell access",
			Permissions: slices.DeleteFunc(slices.Clone(builtInEditorPermissions), func(p string) bool {
				return p == PermContainersExec
			}),
			BuiltIn: true,
		},
		{
			ID:          BuiltInRoleDeployer,
			Name:        "Deployer",
			Description: "Deploy and lifecycle containers and projects",
			Permissions: slices.Clone(builtInDeployerPermissions),
			BuiltIn:     true,
		},
		{
			ID:          BuiltInRoleMonitor,
			Name:        "Monitor",
			Description: "Observability-only access: logs, dashboards, events",
			Permissions: slices.Clone(builtInMonitorPermissions),
			BuiltIn:     true,
		},
		{
			ID:          BuiltInRoleViewer,
			Name:        "Viewer",
			Description: "Read-only access to all resources",
			Permissions: slices.Clone(builtInViewerPermissions),
			BuiltIn:     true,
		},
	}
}

func BuiltInRole(id string) (RoleDefinition, bool) {
	for _, role := range BuiltInRoles() {
		if role.ID == id {
			return role, true
		}
	}
	return RoleDefinition{}, false
}
