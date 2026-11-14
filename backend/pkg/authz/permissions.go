package authz

import "slices"

const (
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
	PermSettingsUpdate           = "settings:update"
	PermEnvironmentsList         = "environments:list"
	PermEnvironmentsRead         = "environments:read"
	PermEnvironmentsCreate       = "environments:create"
	PermEnvironmentsUpdate       = "environments:update"
	PermEnvironmentsDelete       = "environments:delete"
	PermEnvironmentsPair         = "environments:pair"
	PermEnvironmentsSync         = "environments:sync"
	PermEnvironmentsRestart      = "environments:restart"
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
	PermVariablesList            = "variables:list"
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
	PermSystemBackupsList        = "system-backups:list"
	PermSystemBackupsRead        = "system-backups:read"
	PermSystemBackupsCreate      = "system-backups:create"
	PermSystemBackupsUpdate      = "system-backups:update"
	PermSystemBackupsDelete      = "system-backups:delete"
	PermSystemBackupsUpload      = "system-backups:upload"
	PermSystemBackupsRestore     = "system-backups:restore"
	PermSystemBackupsRecoveryKey = "system-backups:recovery-key"
	PermEventsList               = "events:list"
	PermEventsDelete             = "events:delete"
	PermNotificationsList        = "notifications:list"
	PermNotificationsRead        = "notifications:read"
	PermNotificationsUpdate      = "notifications:update"
	PermNotificationsDelete      = "notifications:delete"
	PermNotificationsTest        = "notifications:test"
	PermDiagnosticsRead          = "diagnostics:read"
	PermDiagnosticsProfile       = "diagnostics:profile"
	// Env-scoped permissions (resolved against the {id} env ID in the path).

	PermContainersList            = "containers:list"
	PermContainersRead            = "containers:read"
	PermContainersLogs            = "containers:logs"
	PermContainersCreate          = "containers:create"
	PermContainersStart           = "containers:start"
	PermContainersStop            = "containers:stop"
	PermContainersRestart         = "containers:restart"
	PermContainersRedeploy        = "containers:redeploy"
	PermContainersUpdate          = "containers:update"
	PermContainersKill            = "containers:kill"
	PermContainersPause           = "containers:pause"
	PermContainersDelete          = "containers:delete"
	PermContainersExec            = "containers:exec"
	PermContainersAutoUpdate      = "containers:autoupdate"
	PermProjectsList              = "projects:list"
	PermProjectsRead              = "projects:read"
	PermProjectsLogs              = "projects:logs"
	PermProjectsCreate            = "projects:create"
	PermProjectsUpdate            = "projects:update"
	PermProjectsDeploy            = "projects:deploy"
	PermProjectsDown              = "projects:down"
	PermProjectsRestart           = "projects:restart"
	PermProjectsDelete            = "projects:delete"
	PermProjectsArchive           = "projects:archive"
	PermImagesList                = "images:list"
	PermImagesRead                = "images:read"
	PermImagesPull                = "images:pull"
	PermImagesPush                = "images:push"
	PermImagesBuild               = "images:build"
	PermImagesTag                 = "images:tag"
	PermImagesCommit              = "images:commit"
	PermImagesPrune               = "images:prune"
	PermImagesDelete              = "images:delete"
	PermImagesUpload              = "images:upload"
	PermImagesPatch               = "images:patch"
	PermVolumesList               = "volumes:list"
	PermVolumesRead               = "volumes:read"
	PermVolumesCreate             = "volumes:create"
	PermVolumesRename             = "volumes:rename"
	PermVolumesDelete             = "volumes:delete"
	PermVolumesPrune              = "volumes:prune"
	PermVolumesUpload             = "volumes:upload"
	PermVolumesBackup             = "volumes:backup"
	PermNetworksList              = "networks:list"
	PermNetworksRead              = "networks:read"
	PermNetworksCreate            = "networks:create"
	PermNetworksDelete            = "networks:delete"
	PermNetworksPrune             = "networks:prune"
	PermNetworksConnect           = "networks:connect"
	PermNetworksDisconnect        = "networks:disconnect"
	PermSwarmRead                 = "swarm:read"
	PermSwarmUpdate               = "swarm:update"
	PermSwarmInit                 = "swarm:init"
	PermSwarmJoin                 = "swarm:join"
	PermSwarmLeave                = "swarm:leave"
	PermSwarmUnlock               = "swarm:unlock"
	PermSwarmServicesList         = "swarm-services:list"
	PermSwarmServicesRead         = "swarm-services:read"
	PermSwarmServicesCreate       = "swarm-services:create"
	PermSwarmServicesUpdate       = "swarm-services:update"
	PermSwarmServicesDelete       = "swarm-services:delete"
	PermSwarmServicesLogs         = "swarm-services:logs"
	PermSwarmStacksList           = "swarm-stacks:list"
	PermSwarmStacksRead           = "swarm-stacks:read"
	PermSwarmStacksCreate         = "swarm-stacks:create"
	PermSwarmStacksUpdate         = "swarm-stacks:update"
	PermSwarmStacksDelete         = "swarm-stacks:delete"
	PermSwarmNodesList            = "swarm-nodes:list"
	PermSwarmNodesRead            = "swarm-nodes:read"
	PermSwarmNodesUpdate          = "swarm-nodes:update"
	PermSwarmNodesDelete          = "swarm-nodes:delete"
	PermSwarmNodesPromote         = "swarm-nodes:promote"
	PermSwarmNodesDemote          = "swarm-nodes:demote"
	PermSwarmConfigsList          = "swarm-configs:list"
	PermSwarmConfigsRead          = "swarm-configs:read"
	PermSwarmConfigsCreate        = "swarm-configs:create"
	PermSwarmConfigsUpdate        = "swarm-configs:update"
	PermSwarmConfigsDelete        = "swarm-configs:delete"
	PermSwarmSecretsList          = "swarm-secrets:list"
	PermSwarmSecretsRead          = "swarm-secrets:read"
	PermSwarmSecretsCreate        = "swarm-secrets:create"
	PermSwarmSecretsUpdate        = "swarm-secrets:update"
	PermSwarmSecretsDelete        = "swarm-secrets:delete"
	PermPodmanRead                = "podman:read"
	PermPodmanAutoUpdate          = "podman:autoupdate"
	PermPodmanPodsList            = "podman-pods:list"
	PermPodmanPodsRead            = "podman-pods:read"
	PermPodmanPodsCreate          = "podman-pods:create"
	PermPodmanPodsDelete          = "podman-pods:delete"
	PermPodmanPodsPrune           = "podman-pods:prune"
	PermPodmanPodsStart           = "podman-pods:start"
	PermPodmanPodsStop            = "podman-pods:stop"
	PermPodmanPodsRestart         = "podman-pods:restart"
	PermPodmanPodsPause           = "podman-pods:pause"
	PermPodmanPodsKill            = "podman-pods:kill"
	PermPodmanQuadletsList        = "podman-quadlets:list"
	PermPodmanQuadletsRead        = "podman-quadlets:read"
	PermPodmanQuadletsCreate      = "podman-quadlets:create"
	PermPodmanQuadletsUpdate      = "podman-quadlets:update"
	PermPodmanQuadletsDelete      = "podman-quadlets:delete"
	PermPodmanQuadletsStart       = "podman-quadlets:start"
	PermPodmanQuadletsStop        = "podman-quadlets:stop"
	PermPodmanQuadletsRestart     = "podman-quadlets:restart"
	PermPodmanSecretsList         = "podman-secrets:list"
	PermPodmanSecretsRead         = "podman-secrets:read"
	PermPodmanSecretsCreate       = "podman-secrets:create"
	PermPodmanSecretsUpdate       = "podman-secrets:update"
	PermPodmanSecretsDelete       = "podman-secrets:delete"
	PermPodmanKubeRead            = "podman-kube:read"
	PermPodmanKubeCreate          = "podman-kube:create"
	PermPodmanKubeDelete          = "podman-kube:delete"
	PermGitOpsList                = "gitops:list"
	PermGitOpsRead                = "gitops:read"
	PermGitOpsCreate              = "gitops:create"
	PermGitOpsUpdate              = "gitops:update"
	PermGitOpsDelete              = "gitops:delete"
	PermGitOpsSync                = "gitops:sync"
	PermGitOpsLifecycle           = "gitops:lifecycle"
	PermGitOpsBackup              = "gitops:backup"
	PermGitOpsBidirectional       = "gitops:bidirectional"
	PermWebhooksList              = "webhooks:list"
	PermWebhooksCreate            = "webhooks:create"
	PermWebhooksUpdate            = "webhooks:update"
	PermWebhooksDelete            = "webhooks:delete"
	PermJobsList                  = "jobs:list"
	PermJobsRead                  = "jobs:read"
	PermJobsUpdate                = "jobs:update"
	PermJobsRun                   = "jobs:run"
	PermJobsRestart               = "jobs:restart"
	PermJobsRetry                 = "jobs:retry"
	PermJobsCancel                = "jobs:cancel"
	PermJobsAck                   = "jobs:ack"
	PermSystemRead                = "system:read"
	PermSystemPrune               = "system:prune"
	PermSystemUpgrade             = "system:upgrade"
	PermImageUpdatesList          = "image-updates:list"
	PermImageUpdatesRead          = "image-updates:read"
	PermImageUpdatesCheck         = "image-updates:check"
	PermImageUpdatesApply         = "image-updates:apply"
	PermVulnsList                 = "vulnerabilities:list"
	PermVulnsRead                 = "vulnerabilities:read"
	PermVulnsScan                 = "vulnerabilities:scan"
	PermVulnsUpdate               = "vulnerabilities:update"
	PermBuildWorkspacesList       = "build-workspaces:list"
	PermBuildWorkspacesRead       = "build-workspaces:read"
	PermBuildWorkspacesCreate     = "build-workspaces:create"
	PermBuildWorkspacesDelete     = "build-workspaces:delete"
	PermActivitiesList            = "activities:list"
	PermActivitiesRead            = "activities:read"
	PermActivitiesCancel          = "activities:cancel"
	PermActivitiesDelete          = "activities:delete"
	PermEnvironmentSettingsRead   = "environment-settings:read"
	PermEnvironmentSettingsUpdate = "environment-settings:update"
)

var (
	orgLevelPermissions = buildOrgLevelPermissions()

	allPermissionsSet = func() map[string]struct{} {
		all := AllPermissions()
		m := make(map[string]struct{}, len(all))
		for _, p := range all {
			m[p] = struct{}{}
		}
		return m
	}()

	totalPermissionsCount = len(allPermissionsSet)

	orderedPermissions = AllPermissions()

	permissionRequires = func() map[string][]string {
		out := make(map[string][]string)
		for _, resource := range permissionCatalog {
			for _, action := range resource.Actions {
				out[action.Permission] = action.Requires
			}
		}
		return out
	}()
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

func IsOrgLevel(perm string) bool {
	_, ok := orgLevelPermissions[perm]
	return ok
}

func IsEnvScoped(perm string) bool {
	if _, ok := allPermissionsSet[perm]; !ok {
		return false
	}
	return !IsOrgLevel(perm)
}

func TotalPermissionsCount() int { return totalPermissionsCount }

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

func IsKnownPermission(perm string) bool {
	_, ok := allPermissionsSet[perm]
	return ok
}

func ExpandPermissions(perms []string) []string {
	selected := make(map[string]struct{}, len(perms))
	pending := slices.Clone(perms)
	for len(pending) > 0 {
		perm := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := selected[perm]; ok {
			continue
		}
		selected[perm] = struct{}{}
		pending = append(pending, permissionRequires[perm]...)
	}
	out := make([]string, 0, len(selected))
	for _, perm := range orderedPermissions {
		if _, ok := selected[perm]; ok {
			out = append(out, perm)
		}
	}
	for _, perm := range perms {
		if _, ok := selected[perm]; ok && !IsKnownPermission(perm) {
			out = append(out, perm)
			delete(selected, perm)
		}
	}
	return out
}

func BuiltInEditorPermissions() []string {
	return []string{
		PermUsersList, PermUsersRead,
		PermRolesList, PermRolesRead,
		PermApiKeysList, PermApiKeysRead,
		PermFederatedList, PermFederatedRead,
		PermSettingsRead,
		PermEnvironmentsList, PermEnvironmentsRead, PermEnvironmentsSync,
		PermRegistriesList, PermRegistriesRead, PermRegistriesBrowse,
		PermTemplatesList, PermTemplatesRead, PermTemplatesCreate, PermTemplatesUpdate, PermTemplatesDelete,
		PermVariablesList, PermVariablesRead, PermVariablesCreate, PermVariablesUpdate, PermVariablesDelete, PermVariablesSync,
		PermGitReposList, PermGitReposRead,
		// Editor can run volume backups, so it needs to pick a destination.
		PermS3DestinationsList, PermS3DestinationsRead,
		PermEventsList,
		PermContainersList,
		PermContainersRead,
		PermContainersLogs,
		PermContainersCreate,
		PermContainersStart,
		PermContainersStop,
		PermContainersRestart,
		PermContainersRedeploy,
		PermContainersUpdate,
		PermContainersKill,
		PermContainersPause,
		PermContainersDelete,
		PermContainersExec,
		PermContainersAutoUpdate,

		PermProjectsList, PermProjectsRead, PermProjectsLogs, PermProjectsCreate, PermProjectsUpdate, PermProjectsDeploy, PermProjectsDown, PermProjectsRestart, PermProjectsDelete, PermProjectsArchive,
		PermImagesList, PermImagesRead, PermImagesPull, PermImagesPush, PermImagesBuild, PermImagesTag, PermImagesCommit, PermImagesPrune, PermImagesDelete, PermImagesUpload, PermImagesPatch,
		PermVolumesList, PermVolumesRead, PermVolumesCreate, PermVolumesRename, PermVolumesDelete, PermVolumesPrune, PermVolumesUpload, PermVolumesBackup,
		PermNetworksList, PermNetworksRead, PermNetworksCreate, PermNetworksDelete, PermNetworksPrune, PermNetworksConnect, PermNetworksDisconnect,
		PermSwarmRead, PermSwarmUpdate,
		PermSwarmServicesList, PermSwarmServicesRead, PermSwarmServicesCreate, PermSwarmServicesUpdate, PermSwarmServicesDelete, PermSwarmServicesLogs,
		PermSwarmStacksList, PermSwarmStacksRead, PermSwarmStacksCreate, PermSwarmStacksUpdate, PermSwarmStacksDelete,
		PermSwarmNodesList, PermSwarmNodesRead, PermSwarmNodesUpdate, PermSwarmNodesDelete, PermSwarmNodesPromote, PermSwarmNodesDemote,
		PermSwarmConfigsList, PermSwarmConfigsRead, PermSwarmConfigsCreate, PermSwarmConfigsUpdate, PermSwarmConfigsDelete,
		PermSwarmSecretsList, PermSwarmSecretsRead, PermSwarmSecretsCreate, PermSwarmSecretsUpdate, PermSwarmSecretsDelete,
		PermPodmanRead, PermPodmanAutoUpdate,
		PermPodmanPodsList, PermPodmanPodsRead, PermPodmanPodsCreate, PermPodmanPodsDelete, PermPodmanPodsPrune,
		PermPodmanPodsStart, PermPodmanPodsStop, PermPodmanPodsRestart, PermPodmanPodsPause, PermPodmanPodsKill,
		PermPodmanQuadletsList, PermPodmanQuadletsRead, PermPodmanQuadletsCreate, PermPodmanQuadletsUpdate, PermPodmanQuadletsDelete,
		PermPodmanQuadletsStart, PermPodmanQuadletsStop, PermPodmanQuadletsRestart,
		PermPodmanSecretsList, PermPodmanSecretsRead, PermPodmanSecretsCreate, PermPodmanSecretsUpdate, PermPodmanSecretsDelete,
		PermPodmanKubeRead, PermPodmanKubeCreate, PermPodmanKubeDelete,
		PermGitOpsList, PermGitOpsRead, PermGitOpsCreate, PermGitOpsUpdate, PermGitOpsDelete, PermGitOpsSync, PermGitOpsBackup, PermGitOpsBidirectional,
		PermWebhooksList, PermWebhooksCreate, PermWebhooksUpdate, PermWebhooksDelete,
		PermJobsList, PermJobsRead, PermJobsUpdate, PermJobsRun, PermJobsRestart, PermJobsRetry, PermJobsCancel, PermJobsAck,
		PermNotificationsList, PermNotificationsRead, PermNotificationsUpdate, PermNotificationsDelete, PermNotificationsTest,
		PermSystemRead, PermSystemPrune,
		PermImageUpdatesList, PermImageUpdatesRead, PermImageUpdatesCheck, PermImageUpdatesApply,
		PermVulnsList, PermVulnsRead, PermVulnsScan, PermVulnsUpdate,
		PermBuildWorkspacesList, PermBuildWorkspacesRead, PermBuildWorkspacesCreate, PermBuildWorkspacesDelete,
		PermActivitiesList, PermActivitiesRead, PermActivitiesCancel, PermActivitiesDelete,
		PermEnvironmentSettingsRead,
	}
}

func BuiltInDeployerPermissions() []string {
	return []string{
		PermEnvironmentsList, PermEnvironmentsRead,
		PermRegistriesList, PermRegistriesRead,
		PermTemplatesList, PermTemplatesRead,
		PermVariablesList, PermVariablesRead,
		PermEventsList,
		PermContainersList,
		PermContainersRead,
		PermContainersLogs,
		PermContainersStart,
		PermContainersStop,
		PermContainersRestart,
		PermContainersRedeploy,
		PermContainersKill,
		PermContainersPause,
		PermContainersAutoUpdate,

		PermProjectsList, PermProjectsRead, PermProjectsLogs, PermProjectsDeploy, PermProjectsDown, PermProjectsRestart,
		PermImagesList, PermImagesRead, PermImagesPull, PermImagesTag, PermImagesCommit,
		PermVolumesList, PermVolumesRead,
		PermNetworksList, PermNetworksRead,
		PermSwarmRead,
		PermSwarmServicesList, PermSwarmServicesRead, PermSwarmServicesLogs,
		PermSwarmStacksList, PermSwarmStacksRead,
		PermSwarmNodesList, PermSwarmNodesRead,
		PermSwarmConfigsList, PermSwarmConfigsRead,
		PermSwarmSecretsList, PermSwarmSecretsRead,
		PermPodmanRead, PermPodmanAutoUpdate,
		PermPodmanPodsList, PermPodmanPodsRead,
		PermPodmanQuadletsList, PermPodmanQuadletsRead, PermPodmanQuadletsStart, PermPodmanQuadletsStop, PermPodmanQuadletsRestart,
		PermPodmanSecretsList, PermPodmanSecretsRead,
		PermGitOpsList, PermGitOpsRead, PermGitOpsSync,
		PermSystemRead,
		PermImageUpdatesList, PermImageUpdatesRead, PermImageUpdatesCheck, PermImageUpdatesApply,
		PermVulnsList, PermVulnsRead,
		PermActivitiesList, PermActivitiesRead, PermActivitiesCancel,
	}
}

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
		PermVariablesList, PermVariablesRead,
		PermGitReposList, PermGitReposRead,
		PermS3DestinationsList, PermS3DestinationsRead,
		PermEventsList,
		PermContainersList, PermContainersRead, PermContainersLogs,
		PermProjectsList, PermProjectsRead, PermProjectsLogs,
		PermImagesList, PermImagesRead,
		PermVolumesList, PermVolumesRead,
		PermNetworksList, PermNetworksRead,
		PermSwarmRead,
		PermSwarmServicesList, PermSwarmServicesRead, PermSwarmServicesLogs,
		PermSwarmStacksList, PermSwarmStacksRead,
		PermSwarmNodesList, PermSwarmNodesRead,
		PermSwarmConfigsList, PermSwarmConfigsRead,
		PermSwarmSecretsList, PermSwarmSecretsRead,
		PermPodmanRead,
		PermPodmanPodsList, PermPodmanPodsRead,
		PermPodmanQuadletsList, PermPodmanQuadletsRead,
		PermPodmanSecretsList, PermPodmanSecretsRead,
		PermGitOpsList, PermGitOpsRead,
		PermWebhooksList,
		PermSystemRead,
		PermImageUpdatesList, PermImageUpdatesRead,
		PermVulnsList, PermVulnsRead,
		PermActivitiesList, PermActivitiesRead,
		PermEnvironmentSettingsRead,
	}
}

func BuiltInMonitorPermissions() []string {
	return []string{
		PermEnvironmentsList, PermEnvironmentsRead,
		PermEventsList,
		PermContainersList, PermContainersRead, PermContainersLogs,
		PermProjectsList, PermProjectsRead, PermProjectsLogs,
		PermImagesList, PermImagesRead,
		PermVolumesList, PermVolumesRead,
		PermNetworksList, PermNetworksRead,
		PermSwarmRead,
		PermSwarmServicesList, PermSwarmServicesRead, PermSwarmServicesLogs,
		PermSwarmStacksList, PermSwarmStacksRead,
		PermSwarmNodesList, PermSwarmNodesRead,
		PermSwarmConfigsList, PermSwarmConfigsRead,
		PermSwarmSecretsList, PermSwarmSecretsRead,
		PermPodmanRead,
		PermPodmanPodsList, PermPodmanPodsRead,
		PermPodmanQuadletsList, PermPodmanQuadletsRead,
		PermPodmanSecretsList, PermPodmanSecretsRead,
		PermSystemRead,
		PermImageUpdatesList, PermImageUpdatesRead,
		PermVulnsList, PermVulnsRead,
		PermActivitiesList, PermActivitiesRead,
	}
}

func BuiltInNoShellEditorPermissions() []string {
	return slices.DeleteFunc(BuiltInEditorPermissions(), func(p string) bool {
		return p == PermContainersExec
	})
}
