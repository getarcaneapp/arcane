package types

import (
	"fmt"
	"net/url"
)

const (
	VersionEndpoint    = "/api/version"
	AppVersionEndpoint = "/api/app/version"
)

func pathf(format string, args ...any) string {
	escaped := make([]any, len(args))
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			escaped[i] = url.PathEscape(s)
		} else {
			escaped[i] = arg
		}
	}
	return fmt.Sprintf(format, escaped...)
}

func AuthLogout() string { return "/api/auth/logout" }

func AuthMe() string { return "/api/auth/me" }

func AuthPassword() string { return "/api/auth/password" }

func AuthRefresh() string { return "/api/auth/refresh" }

func AuthFederated() string { return "/api/auth/federated/token" }

func OIDCDeviceCode() string { return "/api/oidc/device/code" }

func OIDCDeviceToken() string { return "/api/oidc/device/token" }

func OIDCStatus() string { return "/api/oidc/status" }

func ApiKeys() string { return "/api/apikeys" }

func ApiKey(id string) string { return pathf("/api/apikeys/%s", id) }

func AuthMeApiKeys() string { return "/api/auth/me/apikeys" }

func AuthMeApiKey(id string) string { return pathf("/api/auth/me/apikeys/%s", id) }

func Users() string { return "/api/users" }

func User(id string) string { return pathf("/api/users/%s", id) }

func UserRoleAssignments(userID string) string {
	return pathf("/api/users/%s/roles", userID)
}

func Roles() string { return "/api/roles" }

func Role(id string) string { return pathf("/api/roles/%s", id) }

func RolesAvailablePermissions() string { return "/api/roles/permissions" }

func OidcRoleMappings() string { return "/api/oidc/mappings" }

func OidcRoleMapping(id string) string { return pathf("/api/oidc/mappings/%s", id) }

func FederatedCredentials() string { return "/api/federated/credentials" }

func FederatedCredential(id string) string { return pathf("/api/federated/credentials/%s", id) }

func Environments() string { return "/api/agents" }

func Environment(id string) string { return pathf("/api/agents/%s", id) }

func EnvironmentTest(envID string) string { return pathf("/api/agents/%s/test", envID) }

func EnvironmentTunnelRestart(envID string) string {
	return pathf("/api/agents/%s/tunnel/restart", envID)
}

func EnvironmentDeployment(envID string) string {
	return pathf("/api/agents/%s/deployment", envID)
}

func EnvironmentVersion(envID string) string {
	return pathf("/api/agents/%s/version", envID)
}

func Containers(envID string) string { return pathf("/api/agents/%s/containers", envID) }

func Container(envID, containerID string) string {
	return pathf("/api/agents/%s/containers/%s", envID, containerID)
}

func ContainerAction(envID, containerID, action string) string {
	return Container(envID, containerID) + "?" + url.Values{"action": {action}}.Encode()
}

func ContainerCommit(envID, containerID string) string {
	return pathf("/api/agents/%s/containers/%s/commit", envID, containerID)
}

func ContainerUpdate(envID, containerID string) string {
	return pathf("/api/agents/%s/containers/%s/update", envID, containerID)
}

func ContainerRedeploy(envID, containerID string) string {
	return pathf("/api/agents/%s/containers/%s/redeploy", envID, containerID)
}

func ContainerConfig(envID, containerID string) string {
	return pathf("/api/agents/%s/containers/%s/config", envID, containerID)
}

func ContainerAutoUpdate(envID, containerID string) string {
	return pathf("/api/agents/%s/containers/%s/autoupdate", envID, containerID)
}

func ContainersCounts(envID string) string {
	return pathf("/api/agents/%s/containers/counts", envID)
}

func Images(envID string) string { return pathf("/api/agents/%s/images", envID) }

func Image(envID, imageID string) string {
	return pathf("/api/agents/%s/images/%s", envID, imageID)
}

func ImagesPull(envID string) string { return pathf("/api/agents/%s/images/pull", envID) }

func ImagesPrune(envID string) string { return pathf("/api/agents/%s/images/prune", envID) }

func ImagesCounts(envID string) string { return pathf("/api/agents/%s/images/counts", envID) }

func ImagesUpload(envID string) string { return pathf("/api/agents/%s/images/upload", envID) }

func ImagesSearch(envID string) string { return pathf("/api/agents/%s/images/search", envID) }

func ImageHistory(envID, name string) string {
	return pathf("/api/agents/%s/images/%s/history", envID, name)
}

func ImageTag(envID, name string) string {
	return pathf("/api/agents/%s/images/%s/tag", envID, name)
}

func ImageExport(envID, name string) string {
	return pathf("/api/agents/%s/images/%s/export", envID, name)
}

func ImageAttestations(envID, name string) string {
	return pathf("/api/agents/%s/images/%s/attestations", envID, name)
}

func ImagesBuild(envID string) string { return pathf("/api/agents/%s/images/build", envID) }

func ImageBuilds(envID string) string { return pathf("/api/agents/%s/images/builds", envID) }

func ImageBuild(envID, buildID string) string {
	return pathf("/api/agents/%s/images/builds/%s", envID, buildID)
}

func UploadSessions(envID, kind string) string {
	return pathf("/api/agents/%s/uploads/%s", envID, kind)
}

func UploadSession(envID, kind, uploadID string) string {
	return pathf("/api/agents/%s/uploads/%s/%s", envID, kind, uploadID)
}

func UploadSessionChunk(envID, kind, uploadID string, index int) string {
	return pathf("/api/agents/%s/uploads/%s/%s/chunks/%d", envID, kind, uploadID, index)
}

func ImageUpdatesCheck(envID, imageRef string) string {
	return pathf("/api/agents/%s/images/updates/check", envID) + "?" + url.Values{"imageRef": {imageRef}}.Encode()
}

func ImageUpdatesCheckAction(envID, action string) string {
	return pathf("/api/agents/%s/images/updates/check", envID) + "?" + url.Values{"action": {action}}.Encode()
}

func ImageUpdatesCheckById(envID, imageID string) string {
	return pathf("/api/agents/%s/images/updates/check/%s", envID, imageID)
}

func ImageUpdatesSummary(envID string) string {
	return pathf("/api/agents/%s/images/updates/summary", envID)
}

func Networks(envID string) string { return pathf("/api/agents/%s/networks", envID) }

func Network(envID, networkID string) string {
	return pathf("/api/agents/%s/networks/%s", envID, networkID)
}

func NetworksCounts(envID string) string {
	return pathf("/api/agents/%s/networks/counts", envID)
}

func NetworksPrune(envID string) string { return pathf("/api/agents/%s/networks/prune", envID) }

func NetworksTopology(envID string) string {
	return pathf("/api/agents/%s/networks/topology", envID)
}

func NetworkAction(envID, networkID, action string) string {
	return Network(envID, networkID) + "?" + url.Values{"action": {action}}.Encode()
}

func Volumes(envID string) string { return pathf("/api/agents/%s/volumes", envID) }

func Volume(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s", envID, volumeName)
}

func VolumesCounts(envID string) string { return pathf("/api/agents/%s/volumes/counts", envID) }

func VolumesPrune(envID string) string { return pathf("/api/agents/%s/volumes/prune", envID) }

func VolumesSizes(envID string) string { return pathf("/api/agents/%s/volumes/sizes", envID) }

func VolumeUsage(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/usage", envID, volumeName)
}

func VolumeRename(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/rename", envID, volumeName)
}

func VolumeBackupPolicy(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/backups/policy", envID, volumeName)
}

func VolumeBackups(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/backups", envID, volumeName)
}

func VolumeBackup(envID, backupID string) string {
	return pathf("/api/agents/%s/volumes/backups/%s", envID, backupID)
}

func VolumeBackupAction(envID, volumeName, backupID, action string) string {
	return pathf("/api/agents/%s/volumes/%s/backups/%s", envID, volumeName, backupID) + "?" + url.Values{"action": {action}}.Encode()
}

func VolumeBackupUpload(envID, backupID string) string {
	return pathf("/api/agents/%s/volumes/backups/%s/upload", envID, backupID)
}

func VolumeBackupDownload(envID, backupID string) string {
	return pathf("/api/agents/%s/volumes/backups/%s/download", envID, backupID)
}

func VolumeBackupFiles(envID, backupID string) string {
	return pathf("/api/agents/%s/volumes/backups/%s/files", envID, backupID)
}

func VolumeBackupUploadRestore(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/backups/upload", envID, volumeName)
}

func VolumeWorkspace(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/workspace", envID, volumeName)
}

func VolumeWorkspaceFile(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/workspace/file", envID, volumeName)
}

func VolumeWorkspaceFileDownload(envID, volumeName string) string {
	return pathf("/api/agents/%s/volumes/%s/workspace/file/download", envID, volumeName)
}

func Projects(envID string) string { return pathf("/api/agents/%s/projects", envID) }

func Project(envID, projectID string) string {
	return pathf("/api/agents/%s/projects/%s", envID, projectID)
}

func ProjectsCounts(envID string) string {
	return pathf("/api/agents/%s/projects/counts", envID)
}

func ProjectsTags(envID string) string { return pathf("/api/agents/%s/projects/tags", envID) }

func ProjectTags(envID, projectID string) string {
	return pathf("/api/agents/%s/projects/%s/tags", envID, projectID)
}

func ProjectAction(envID, projectID, action string, services []string) string {
	return Project(envID, projectID) + "?" + url.Values{"action": {action}, "services": services}.Encode()
}

func ProjectDeploy(envID, projectID, action string) string {
	return pathf("/api/agents/%s/projects/%s/deploy", envID, projectID) + "?" + url.Values{"action": {action}}.Encode()
}

func ProjectBuild(envID, projectID string) string {
	return pathf("/api/agents/%s/projects/%s/build", envID, projectID)
}

func ProjectWorkspace(envID, projectID string) string {
	return pathf("/api/agents/%s/projects/%s/workspace", envID, projectID)
}

func ProjectWorkspaceFile(envID, projectID string) string {
	return pathf("/api/agents/%s/projects/%s/workspace/file", envID, projectID)
}

func ProjectWorkspaceFileDownload(envID, projectID string) string {
	return pathf("/api/agents/%s/projects/%s/workspace/file/download", envID, projectID)
}

func SystemPrune(envID string) string { return pathf("/api/agents/%s/system/prune", envID) }

func SystemDockerInfo(envID string) string {
	return pathf("/api/agents/%s/system/docker/info", envID)
}

func SystemContainersAction(envID, action string) string {
	return pathf("/api/agents/%s/system/containers", envID) + "?" + url.Values{"action": {action}}.Encode()
}

func SystemConvert(envID string) string { return pathf("/api/agents/%s/system/convert", envID) }

func SystemUpgrade(envID string) string { return pathf("/api/agents/%s/system/upgrade", envID) }

func SystemUpgradeCheck(envID string) string {
	return pathf("/api/agents/%s/system/upgrade/check", envID)
}

func SystemUpgradeJobs(envID string) string {
	return pathf("/api/agents/%s/system/upgrade/jobs", envID)
}

func SystemUpgradeJobCurrent(envID string) string {
	return pathf("/api/agents/%s/system/upgrade/jobs/current", envID)
}

func UpdaterStatus(envID string) string { return pathf("/api/agents/%s/updater/status", envID) }

func UpdaterRun(envID string) string { return pathf("/api/agents/%s/updater/run", envID) }

func UpdaterHistory(envID string) string {
	return pathf("/api/agents/%s/updater/history", envID)
}

func JobSchedules(envID string) string { return pathf("/api/agents/%s/jobs/schedules", envID) }

func Jobs(envID string) string { return pathf("/api/agents/%s/jobs", envID) }

func JobAction(envID, jobID, action string) string {
	return pathf("/api/agents/%s/jobs/%s", envID, jobID) + "?" + url.Values{"action": {action}}.Encode()
}

func Settings(envID string) string { return pathf("/api/agents/%s/settings", envID) }

func ManagerSettings() string { return "/api/settings" }

func AuthenticationSettings() string { return "/api/settings/authentication" }

func SettingsPublic(envID string) string {
	return pathf("/api/agents/%s/settings/public", envID)
}

func NotificationsSettings() string {
	return "/api/notifications/settings"
}

func NotificationSettingsProvider(provider string) string {
	return pathf("/api/notifications/settings/%s", provider)
}

func NotificationSettingsProviderAction(provider, action string) string {
	return NotificationSettingsProvider(provider) + "?" + url.Values{"action": {action}}.Encode()
}

func ContainerRegistries() string { return "/api/registries" }

func ContainerRegistry(id string) string { return pathf("/api/registries/%s", id) }

func ContainerRegistryAction(id, action string) string {
	return ContainerRegistry(id) + "?" + url.Values{"action": {action}}.Encode()
}

func ContainerRegistriesPullUsage() string { return "/api/registries/usage" }

func Events() string { return "/api/events" }

func Event(id string) string { return pathf("/api/events/%s", id) }

func EventsEnvironment(envID string) string {
	return pathf("/api/events/agent/%s", envID)
}

func EventsStats() string { return "/api/events/stats" }

func Templates() string { return "/api/templates" }

func Template(id string) string { return pathf("/api/templates/%s", id) }

func TemplatesAll() string { return "/api/templates/all" }

func TemplatesDefault() string { return "/api/templates/default" }

func TemplatesRegistries() string { return "/api/templates/registries" }

func TemplateRegistry(id string) string {
	return pathf("/api/templates/registries/%s", id)
}

func TemplateContent(id string) string { return pathf("/api/templates/%s/content", id) }

func TemplateFetch() string { return "/api/templates/fetch" }

func TemplateAction(id, action string) string {
	return pathf("/api/templates/%s", id) + "?" + url.Values{"action": {action}}.Encode()
}

func Variables() string { return "/api/variables" }

func Variable(id string) string { return pathf("/api/variables/%s", id) }

func VariablesSync() string { return "/api/variables/sync" }

func Backups() string { return "/api/backups" }

func Backup(id string) string {
	return pathf("/api/agents/0/backups/%s", id)
}

func BackupsPolicies() string { return "/api/backups/policies" }

func BackupsRecoveryKey() string { return "/api/backups/recovery/key" }

func BackupsRecoveryKeyGenerate() string { return "/api/backups/recovery/key?action=generate" }

func BackupsDiscover() string { return "/api/backups/discover" }

func BackupUpload(id string) string {
	return pathf("/api/agents/0/backups/%s/upload", id)
}

func BackupAction(id, action string) string {
	return Backup(id) + "?" + url.Values{"action": {action}}.Encode()
}

func BackupsS3() string { return "/api/backups/s3" }

func BackupsS3Options() string { return "/api/backups/s3/options" }

func BackupsS3Destination(id string) string {
	return pathf("/api/backups/s3/%s", id)
}

func BackupsS3Test() string { return "/api/backups/s3/test" }

func BackupsS3DestinationAction(id, action string) string {
	return BackupsS3Destination(id) + "?" + url.Values{"action": {action}}.Encode()
}

func BackupsS3DestinationInUse(id string) string {
	return pathf("/api/backups/s3/%s/usage", id)
}

func ImageVulnerabilitiesScan(envID, imageID string) string {
	return pathf("/api/agents/%s/images/%s/vulnerabilities/scan", envID, imageID)
}

func ImageVulnerabilities(envID, imageID string) string {
	return pathf("/api/agents/%s/images/%s/vulnerabilities", envID, imageID)
}

func ImageVulnerabilitiesSummary(envID, imageID string) string {
	return pathf("/api/agents/%s/images/%s/vulnerabilities/summary", envID, imageID)
}

func ImageVulnerabilitiesList(envID, imageID string) string {
	return pathf("/api/agents/%s/images/%s/vulnerabilities/list", envID, imageID)
}

func ImagesVulnerabilitiesSummaries(envID string) string {
	return pathf("/api/agents/%s/images/vulnerabilities/summaries", envID)
}

func VulnerabilitiesScannerStatus(envID string) string {
	return pathf("/api/agents/%s/vulnerabilities/scanner", envID)
}

func VulnerabilitiesSummary(envID string) string {
	return pathf("/api/agents/%s/vulnerabilities/summary", envID)
}

func VulnerabilitiesAll(envID string) string {
	return pathf("/api/agents/%s/vulnerabilities/all", envID)
}

func VulnerabilitiesIgnored(envID string) string {
	return pathf("/api/agents/%s/vulnerabilities/ignored", envID)
}

func VulnerabilityIgnored(envID, ignoreID string) string {
	return pathf("/api/agents/%s/vulnerabilities/ignored/%s", envID, ignoreID)
}

func Activities(envID string) string { return pathf("/api/agents/%s/activities", envID) }

func Activity(envID, activityID string) string {
	return pathf("/api/agents/%s/activities/%s", envID, activityID)
}

func ActivityAction(envID, activityID, action string) string {
	return Activity(envID, activityID) + "?" + url.Values{"action": {action}}.Encode()
}

func ActivitiesHistory(envID string) string {
	return pathf("/api/agents/%s/activities/history", envID)
}

func Webhooks(envID string) string { return pathf("/api/agents/%s/webhooks", envID) }

func Webhook(envID, webhookID string) string {
	return pathf("/api/agents/%s/webhooks/%s", envID, webhookID)
}

func WebhookTrigger(token string) string { return pathf("/api/webhooks/trigger/%s", token) }

func PodmanStatus(envID string) string { return pathf("/api/agents/%s/podman/status", envID) }

func PodmanAutoUpdate(envID string) string {
	return pathf("/api/agents/%s/podman/autoupdate", envID)
}

func PodmanPods(envID string) string { return pathf("/api/agents/%s/podman/pods", envID) }

func PodmanPod(envID, podID string) string {
	return pathf("/api/agents/%s/podman/pods/%s", envID, podID)
}

func PodmanPodAction(envID, podID, action string) string {
	return PodmanPod(envID, podID) + "?" + url.Values{"action": {action}}.Encode()
}

func PodmanQuadlets(envID string) string {
	return pathf("/api/agents/%s/podman/quadlets", envID)
}

func PodmanQuadlet(envID, name string) string {
	return pathf("/api/agents/%s/podman/quadlets/%s", envID, name)
}

func PodmanQuadletAction(envID, name, action string) string {
	return PodmanQuadlet(envID, name) + "?" + url.Values{"action": {action}}.Encode()
}

func PodmanKube(envID, action string) string {
	return pathf("/api/agents/%s/podman/kube/%s", envID, action)
}

func PodmanSecrets(envID string) string { return pathf("/api/agents/%s/podman/secrets", envID) }

func PodmanSecret(envID, secretID string) string {
	return pathf("/api/agents/%s/podman/secrets/%s", envID, secretID)
}

func GitOpsSyncs(envID string) string { return pathf("/api/agents/%s/gitops/syncs", envID) }

func GitOpsSync(envID, syncID string) string {
	return pathf("/api/agents/%s/gitops/syncs/%s", envID, syncID)
}

func GitOpsSyncStatus(envID, syncID string) string {
	return pathf("/api/agents/%s/gitops/syncs/%s/status", envID, syncID)
}

func GitOpsSyncAction(envID, syncID, action string) string {
	return GitOpsSync(envID, syncID) + "?" + url.Values{"action": {action}}.Encode()
}

func GitOpsSyncFiles(envID, syncID string) string {
	return pathf("/api/agents/%s/gitops/syncs/%s/files", envID, syncID)
}

func GitOpsSyncsImport(envID string) string {
	return pathf("/api/agents/%s/gitops/syncs/import", envID)
}

func GitRepositories() string { return "/api/gitops/repositories" }

func GitRepository(id string) string { return pathf("/api/gitops/repositories/%s", id) }

func GitRepositoryAction(id, action string) string {
	return GitRepository(id) + "?" + url.Values{"action": {action}}.Encode()
}

func GitRepositoryBranches(id string) string {
	return pathf("/api/gitops/repositories/%s/branches", id)
}

func GitRepositoryFiles(id string) string {
	return pathf("/api/gitops/repositories/%s/files", id)
}
