package authz

import (
	"slices"

	"github.com/getarcaneapp/arcane/types/v3"
)

const (
	PermissionScopeGlobal = "global"
	PermissionScopeEnv    = "env"
)

// PermissionCatalogResource describes a resource group in the permission
// catalog. It is the authz-owned source for permission ordering, scope, and
// display metadata used by API manifests and validation helpers.
type PermissionCatalogResource struct {
	Key     string
	Label   string
	Scope   string
	Actions []PermissionCatalogAction
}

// PermissionCatalogAction describes one recognized permission.
type PermissionCatalogAction struct {
	Key         string
	Permission  string
	Label       string
	Description string
	Selectors   []types.SelectorKind
}

var (
	selectorsAll = []types.SelectorKind{
		types.SelectorKindLabels,
		types.SelectorKindProjects,
		types.SelectorKindOwner,
		types.SelectorKindNames,
	}
	selectorsProject = []types.SelectorKind{
		types.SelectorKindProjects,
		types.SelectorKindOwner,
		types.SelectorKindNames,
	}
	selectorsImage = []types.SelectorKind{
		types.SelectorKindLabels,
		types.SelectorKindNames,
	}
	selectorsSwarmService = []types.SelectorKind{
		types.SelectorKindLabels,
		types.SelectorKindProjects,
		types.SelectorKindNames,
	}
	selectorsSwarmStack = []types.SelectorKind{
		types.SelectorKindProjects,
		types.SelectorKindNames,
	}
)

var permissionCatalog = []PermissionCatalogResource{
	{"users", "Users", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermUsersList, "List", "", nil},
		{"read", PermUsersRead, "Read", "", nil},
		{"create", PermUsersCreate, "Create", "", nil},
		{"update", PermUsersUpdate, "Update", "", nil},
		{"delete", PermUsersDelete, "Delete", "", nil},
	}},
	{"roles", "Roles", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermRolesList, "List", "", nil},
		{"read", PermRolesRead, "Read", "", nil},
		{"create", PermRolesCreate, "Create", "", nil},
		{"update", PermRolesUpdate, "Update", "", nil},
		{"delete", PermRolesDelete, "Delete", "", nil},
	}},
	{"bindings", "Role Bindings", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermBindingsList, "List", "", nil},
		{"create", PermBindingsCreate, "Create", "", nil},
		{"delete", PermBindingsDelete, "Delete", "", nil},
	}},
	{"groups", "Groups", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermGroupsList, "List", "", nil},
		{"read", PermGroupsRead, "Read", "", nil},
		{"create", PermGroupsCreate, "Create", "", nil},
		{"update", PermGroupsUpdate, "Update", "", nil},
		{"delete", PermGroupsDelete, "Delete", "", nil},
		{"members", PermGroupsMembers, "Manage members", "", nil},
	}},
	{"oidcmappings", "OIDC Group Mappings", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermOidcMappingsList, "List", "", nil},
		{"create", PermOidcMappingsCreate, "Create", "", nil},
		{"update", PermOidcMappingsUpdate, "Update", "", nil},
		{"delete", PermOidcMappingsDelete, "Delete", "", nil},
	}},
	{"apikeys", "API Keys", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermApiKeysList, "List", "", nil},
		{"read", PermApiKeysRead, "Read", "", nil},
		{"create", PermApiKeysCreate, "Create", "", nil},
		{"update", PermApiKeysUpdate, "Update", "", nil},
		{"delete", PermApiKeysDelete, "Delete", "", nil},
	}},
	{"federated", "Federated Credentials", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermFederatedList, "List", "", nil},
		{"read", PermFederatedRead, "Read", "", nil},
		{"create", PermFederatedCreate, "Create", "", nil},
		{"update", PermFederatedUpdate, "Update", "", nil},
		{"delete", PermFederatedDelete, "Delete", "", nil},
	}},
	{"settings", "Settings", PermissionScopeGlobal, []PermissionCatalogAction{
		{"read", PermSettingsRead, "Read", "", nil},
		{"write", PermSettingsWrite, "Write", "", nil},
	}},
	{"environments", "Environments", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermEnvironmentsList, "List", "", nil},
		{"read", PermEnvironmentsRead, "Read", "", nil},
		{"create", PermEnvironmentsCreate, "Create", "", nil},
		{"update", PermEnvironmentsUpdate, "Update", "", nil},
		{"delete", PermEnvironmentsDelete, "Delete", "", nil},
		{"pair", PermEnvironmentsPair, "Pair agent", "", nil},
		{"sync", PermEnvironmentsSync, "Sync heartbeat", "", nil},
	}},
	{"registries", "Container Registries", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermRegistriesList, "List", "", nil},
		{"read", PermRegistriesRead, "Read", "", nil},
		{"create", PermRegistriesCreate, "Create", "", nil},
		{"update", PermRegistriesUpdate, "Update", "", nil},
		{"delete", PermRegistriesDelete, "Delete", "", nil},
		{"test", PermRegistriesTest, "Test", "", nil},
		{"browse", PermRegistriesBrowse, "Browse", "Browse repositories and tags stored in the registry (the pages also require Read)", nil},
		{"delete-tags", PermRegistriesDeleteTags, "Delete tags", "Delete image manifests from the registry", nil},
	}},
	{"templates", "Templates", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermTemplatesList, "List", "", nil},
		{"read", PermTemplatesRead, "Read", "", nil},
		{"create", PermTemplatesCreate, "Create", "", nil},
		{"update", PermTemplatesUpdate, "Update", "", nil},
		{"delete", PermTemplatesDelete, "Delete", "", nil},
	}},
	{"variables", "Variables", PermissionScopeGlobal, []PermissionCatalogAction{
		{"read", PermVariablesRead, "Read", "", nil},
		{"create", PermVariablesCreate, "Create", "", nil},
		{"update", PermVariablesUpdate, "Update", "", nil},
		{"delete", PermVariablesDelete, "Delete", "", nil},
		{"sync", PermVariablesSync, "Sync", "", nil},
	}},
	{"git-repositories", "Git Repositories", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermGitReposList, "List", "", nil},
		{"read", PermGitReposRead, "Read", "", nil},
		{"create", PermGitReposCreate, "Create", "", nil},
		{"update", PermGitReposUpdate, "Update", "", nil},
		{"delete", PermGitReposDelete, "Delete", "", nil},
		{"test", PermGitReposTest, "Test", "", nil},
		{"sync", PermGitReposSync, "Sync", "", nil},
	}},
	{"s3-destinations", "S3 Destinations", PermissionScopeGlobal, []PermissionCatalogAction{
		{"list", PermS3DestinationsList, "List", "", nil},
		{"read", PermS3DestinationsRead, "Read", "", nil},
		{"create", PermS3DestinationsCreate, "Create", "", nil},
		{"update", PermS3DestinationsUpdate, "Update", "", nil},
		{"delete", PermS3DestinationsDelete, "Delete", "", nil},
		{"test", PermS3DestinationsTest, "Test", "", nil},
		{"sync", PermS3DestinationsSync, "Sync", "", nil},
	}},
	{"system-backups", "System Backups", PermissionScopeGlobal, []PermissionCatalogAction{
		{"read", PermSystemBackupsRead, "Read", "", nil},
		{"manage", PermSystemBackupsManage, "Manage", "", nil},
		{"restore", PermSystemBackupsRestore, "Restore", "", nil},
		{"recovery-key", PermSystemBackupsRecoveryKey, "Recovery Key", "", nil},
	}},
	{"events", "Events", PermissionScopeGlobal, []PermissionCatalogAction{
		{"read", PermEventsRead, "Read", "", nil},
		{"delete", PermEventsDelete, "Delete", "", nil},
	}},
	{"customize", "Customize", PermissionScopeGlobal, []PermissionCatalogAction{
		{"manage", PermCustomizeManage, "Manage", "", nil},
	}},
	{"diagnostics", "Diagnostics", PermissionScopeGlobal, []PermissionCatalogAction{
		{"read", PermDiagnosticsRead, "View", "View runtime diagnostics, pprof profiles, and backend logs", nil},
	}},
	{"audit", "Audit Log", PermissionScopeGlobal, []PermissionCatalogAction{
		{"read", PermAuditRead, "Read", "", nil},
		{"export", PermAuditExport, "Export", "", nil},
	}},
	{"containers", "Containers", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermContainersList, "List", "", selectorsAll},
		{"read", PermContainersRead, "Read", "", selectorsAll},
		{"logs", PermContainersLogs, "View logs", "", selectorsAll},
		{"create", PermContainersCreate, "Create", "", selectorsAll},
		{"start", PermContainersStart, "Start", "", selectorsAll},
		{"stop", PermContainersStop, "Stop", "", selectorsAll},
		{"restart", PermContainersRestart, "Restart", "", selectorsAll},
		{"redeploy", PermContainersRedeploy, "Redeploy", "", selectorsAll},
		{"edit", PermContainersEdit, "Edit (recreate with changes)", "", selectorsAll},
		{"kill", PermContainersKill, "Kill (send signal)", "", selectorsAll},
		{"pause", PermContainersPause, "Pause / unpause", "", selectorsAll},
		{"delete", PermContainersDelete, "Delete", "", selectorsAll},
		{"exec", PermContainersExec, "Exec / terminal", "", selectorsAll},
		{"autoupdate", PermContainersAutoUpdate, "Auto-update", "", selectorsAll},
	}},
	{"projects", "Projects", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermProjectsList, "List", "", selectorsProject},
		{"read", PermProjectsRead, "Read", "", selectorsProject},
		{"logs", PermProjectsLogs, "View logs", "", selectorsProject},
		{"create", PermProjectsCreate, "Create", "", selectorsProject},
		{"update", PermProjectsUpdate, "Update", "", selectorsProject},
		{"deploy", PermProjectsDeploy, "Deploy", "", selectorsProject},
		{"down", PermProjectsDown, "Bring down", "", selectorsProject},
		{"restart", PermProjectsRestart, "Restart", "", selectorsProject},
		{"delete", PermProjectsDelete, "Delete", "", selectorsProject},
		{"archive", PermProjectsArchive, "Archive / unarchive", "", selectorsProject},
	}},
	{"images", "Images", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermImagesList, "List", "", selectorsImage},
		{"read", PermImagesRead, "Read", "", selectorsImage},
		{"pull", PermImagesPull, "Pull", "", selectorsImage},
		{"build", PermImagesBuild, "Build", "", selectorsImage},
		{"tag", PermImagesTag, "Tag", "", selectorsImage},
		{"commit", PermImagesCommit, "Commit container", "", selectorsImage},
		{"prune", PermImagesPrune, "Prune", "", nil},
		{"delete", PermImagesDelete, "Delete", "", selectorsImage},
		{"upload", PermImagesUpload, "Upload", "", selectorsImage},
		{"patch", PermImagesPatch, "Patch", "", selectorsImage},
	}},
	{"volumes", "Volumes", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermVolumesList, "List", "", selectorsAll},
		{"read", PermVolumesRead, "Read", "", selectorsAll},
		{"create", PermVolumesCreate, "Create", "", selectorsAll},
		{"rename", PermVolumesRename, "Rename", "", selectorsAll},
		{"delete", PermVolumesDelete, "Delete", "", selectorsAll},
		{"prune", PermVolumesPrune, "Prune", "", nil},
		{"upload", PermVolumesUpload, "Upload", "", selectorsAll},
		{"backup", PermVolumesBackup, "Backup / restore", "", selectorsAll},
	}},
	{"networks", "Networks", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermNetworksList, "List", "", selectorsAll},
		{"read", PermNetworksRead, "Read", "", selectorsAll},
		{"create", PermNetworksCreate, "Create", "", selectorsAll},
		{"delete", PermNetworksDelete, "Delete", "", selectorsAll},
		{"prune", PermNetworksPrune, "Prune", "", nil},
		{"connect", PermNetworksConnect, "Connect container", "", selectorsAll},
		{"disconnect", PermNetworksDisconnect, "Disconnect container", "", selectorsAll},
	}},
	{"swarm", "Swarm", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermSwarmRead, "Read", "", nil},
		{"init", PermSwarmInit, "Initialize", "", nil},
		{"join", PermSwarmJoin, "Join", "", nil},
		{"leave", PermSwarmLeave, "Leave", "", nil},
		{"spec", PermSwarmSpec, "Update spec", "", nil},
		{"nodes", PermSwarmNodes, "Manage nodes", "", nil},
		{"services", PermSwarmServices, "Manage services", "", selectorsSwarmService},
		{"services:logs", PermSwarmServicesLogs, "View service logs", "", selectorsSwarmService},
		{"stacks", PermSwarmStacks, "Manage stacks", "", selectorsSwarmStack},
		{"configs", PermSwarmConfigs, "Manage configs", "", nil},
		{"secrets", PermSwarmSecrets, "Manage secrets", "", nil},
		{"unlock", PermSwarmUnlock, "Unlock / join tokens", "", nil},
	}},
	{"podman", "Podman", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermPodmanRead, "Read", "", nil},
		{"pods", PermPodmanPods, "Manage pods", "", nil},
		{"quadlets", PermPodmanQuadlets, "Manage quadlets", "", nil},
		{"quadlets:lifecycle", PermPodmanQuadletsLifecycle, "Start / stop quadlets", "", nil},
		{"kube", PermPodmanKube, "Play / generate kube", "", nil},
		{"secrets", PermPodmanSecrets, "Manage secrets", "", nil},
		{"autoupdate", PermPodmanAutoUpdate, "Run auto-update", "", nil},
	}},
	{"gitops", "GitOps Syncs", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermGitOpsList, "List", "", nil},
		{"read", PermGitOpsRead, "Read", "", nil},
		{"create", PermGitOpsCreate, "Create", "", nil},
		{"update", PermGitOpsUpdate, "Update", "", nil},
		{"delete", PermGitOpsDelete, "Delete", "", nil},
		{"sync", PermGitOpsSync, "Trigger sync", "", nil},
		{
			"lifecycle",
			PermGitOpsLifecycle,
			"Configure pre-deploy hooks",
			"Create or modify a sync's pre-deploy lifecycle hook, which runs an arbitrary container with host bind mounts and env on every sync",
			nil,
		},

		{
			"backup",
			PermGitOpsBackup,
			"Back up to Git",
			"View a project's Git backup preview and history, and manage the backup together with the matching sync permission (create, update, sync, delete)",
			nil,
		},
	}},
	{"webhooks", "Webhooks", PermissionScopeEnv, []PermissionCatalogAction{
		{"list", PermWebhooksList, "List", "", nil},
		{"create", PermWebhooksCreate, "Create", "", nil},
		{"update", PermWebhooksUpdate, "Update", "", nil},
		{"delete", PermWebhooksDelete, "Delete", "", nil},
	}},
	{"jobs", "Background Jobs", PermissionScopeEnv, []PermissionCatalogAction{
		{"manage", PermJobsManage, "Manage", "", nil},
	}},
	{"notifications", "Notifications", PermissionScopeGlobal, []PermissionCatalogAction{
		{"manage", PermNotificationsManage, "Manage", "", nil},
	}},
	{"dashboard", "Dashboard", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermDashboardRead, "Read", "", nil},
	}},
	{"system", "System", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermSystemRead, "Read", "", nil},
		{"prune", PermSystemPrune, "Prune", "", nil},
		{"upgrade", PermSystemUpgrade, "Trigger upgrade", "", nil},
	}},
	{"image-updates", "Image Updates", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermImageUpdatesRead, "Read", "", nil},
		{"check", PermImageUpdatesCheck, "Check", "", nil},
	}},
	{"vulnerabilities", "Vulnerabilities", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermVulnsRead, "Read", "", nil},
		{"scan", PermVulnsScan, "Scan", "", nil},
		{"manage", PermVulnsManage, "Manage ignores", "", nil},
	}},
	{"build-workspaces", "Build Workspaces", PermissionScopeEnv, []PermissionCatalogAction{
		{"manage", PermBuildWorkspacesManage, "Manage", "", nil},
	}},
	{"activities", "Activities", PermissionScopeEnv, []PermissionCatalogAction{
		{"read", PermActivitiesRead, "Read", "", nil},
		{"cancel", PermActivitiesCancel, "Cancel", "", nil},
		{"delete", PermActivitiesDelete, "Clear history", "", nil},
	}},
}

// PermissionCatalog returns a defensive copy of the full permission catalog.
func PermissionCatalog() []PermissionCatalogResource {
	out := make([]PermissionCatalogResource, len(permissionCatalog))
	for i, resource := range permissionCatalog {
		actions := make([]PermissionCatalogAction, len(resource.Actions))
		for j, action := range resource.Actions {
			actions[j] = PermissionCatalogAction{
				Key:         action.Key,
				Permission:  action.Permission,
				Label:       action.Label,
				Description: action.Description,
				Selectors:   slices.Clone(action.Selectors),
			}
		}
		out[i] = PermissionCatalogResource{
			Key:     resource.Key,
			Label:   resource.Label,
			Scope:   resource.Scope,
			Actions: actions,
		}
	}
	return out
}

var permissionSelectors = func() map[string][]types.SelectorKind {
	out := make(map[string][]types.SelectorKind)
	for _, resource := range permissionCatalog {
		for _, action := range resource.Actions {
			if len(action.Selectors) > 0 {
				out[action.Permission] = action.Selectors
			}
		}
	}
	return out
}()

func SelectorKindsFor(perm string) []types.SelectorKind {
	return slices.Clone(permissionSelectors[perm])
}

func IsScopable(perm string) bool {
	return len(permissionSelectors[perm]) > 0
}
