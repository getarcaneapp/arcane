package types

import "fmt"

// ArcaneApiEndpoints holds the API endpoint path templates for the Arcane API.
// Endpoint paths may contain format specifiers (e.g., %s) for environment IDs or resource IDs.
type ArcaneApiEndpoints struct {
	// Version
	VersionEndpoint string

	// Authentication
	AuthLogoutEndpoint   string
	AuthMeEndpoint       string
	AuthPasswordEndpoint string
	AuthRefreshEndpoint  string

	// OIDC
	OIDCDeviceCodeEndpoint  string
	OIDCDeviceTokenEndpoint string
	OIDCStatusEndpoint      string

	// API Keys
	ApiKeysEndpoint string
	ApiKeyEndpoint  string

	// Users
	UsersEndpoint string
	UserEndpoint  string

	// Environments
	EnvironmentsEndpoint       string
	EnvironmentEndpoint        string
	EnvironmentTestEndpoint    string
	EnvironmentVersionEndpoint string

	// Containers
	ContainersEndpoint       string
	ContainerEndpoint        string
	ContainerStartEndpoint   string
	ContainerStopEndpoint    string
	ContainerRestartEndpoint string
	ContainerUpdateEndpoint  string
	ContainersCountsEndpoint string

	// Images
	ImagesEndpoint       string
	ImageEndpoint        string
	ImagesPullEndpoint   string
	ImagesPruneEndpoint  string
	ImagesCountsEndpoint string
	ImagesUploadEndpoint string

	// Image Updates
	ImageUpdatesCheckEndpoint     string
	ImageUpdatesCheckAllEndpoint  string
	ImageUpdatesCheckByIdEndpoint string
	ImageUpdatesSummaryEndpoint   string

	// Networks
	NetworksEndpoint       string
	NetworkEndpoint        string
	NetworksCountsEndpoint string
	NetworksPruneEndpoint  string

	// Volumes
	VolumesEndpoint       string
	VolumeEndpoint        string
	VolumesCountsEndpoint string
	VolumesPruneEndpoint  string
	VolumesSizesEndpoint  string
	VolumeUsageEndpoint   string

	// Projects (Stacks)
	ProjectsEndpoint        string
	ProjectEndpoint         string
	ProjectsCountsEndpoint  string
	ProjectDestroyEndpoint  string
	ProjectUpEndpoint       string
	ProjectDownEndpoint     string
	ProjectRestartEndpoint  string
	ProjectRedeployEndpoint string
	ProjectPullEndpoint     string
	ProjectIncludesEndpoint string

	// System
	SystemPruneEndpoint              string
	SystemDockerInfoEndpoint         string
	SystemContainersStartAllEndpoint string
	SystemContainersStopAllEndpoint  string
	SystemStartStoppedEndpoint       string
	SystemConvertEndpoint            string
	SystemUpgradeEndpoint            string
	SystemUpgradeCheckEndpoint       string

	// Updater
	UpdaterStatusEndpoint  string
	UpdaterRunEndpoint     string
	UpdaterHistoryEndpoint string

	// Job Schedules
	JobSchedulesEndpoint string

	// Settings
	SettingsEndpoint       string
	SettingsPublicEndpoint string

	// Notifications
	NotificationsAppriseEndpoint         string
	NotificationsAppriseTestEndpoint     string
	NotificationsSettingsEndpoint        string
	NotificationSettingsProviderEndpoint string
	NotificationsTestProviderEndpoint    string

	// Container Registries
	ContainerRegistriesEndpoint   string
	ContainerRegistryEndpoint     string
	ContainerRegistrySyncEndpoint string
	ContainerRegistryTestEndpoint string

	// Events
	EventsEndpoint            string
	EventEndpoint             string
	EventsEnvironmentEndpoint string

	// Templates
	TemplatesEndpoint           string
	TemplateEndpoint            string
	TemplatesAllEndpoint        string
	TemplatesDefaultEndpoint    string
	TemplatesRegistriesEndpoint string
	TemplateRegistryEndpoint    string
	TemplatesVariablesEndpoint  string
	TemplateContentEndpoint     string
	TemplateDownloadEndpoint    string
	TemplateFetchEndpoint       string

	// Dashboard
	DashboardActionItemsEndpoint string

	// Assets
	AppImagesFaviconEndpoint string
	AppImagesLogoEndpoint    string
	AppImagesProfileEndpoint string
	FontsMonoEndpoint        string
	FontsSansEndpoint        string
	FontsSerifEndpoint       string

	// Customization
	CustomizeCategoriesEndpoint string
	CustomizeSearchEndpoint     string

	// GitOps Syncs
	GitOpsSyncsEndpoint       string
	GitOpsSyncEndpoint        string
	GitOpsSyncStatusEndpoint  string
	GitOpsSyncTriggerEndpoint string
	GitOpsSyncFilesEndpoint   string
	GitOpsSyncsImportEndpoint string

	// Git Repositories
	GitRepositoriesEndpoint       string
	GitRepositoryEndpoint         string
	GitRepositoryTestEndpoint     string
	GitRepositoryBranchesEndpoint string
	GitRepositoryFilesEndpoint    string
	GitRepositoriesSyncEndpoint   string
}

// Endpoints contains the defined API endpoints
var Endpoints = ArcaneApiEndpoints{ //nolint:gosec // static endpoint paths; auth-related names are not credentials
	// Version
	VersionEndpoint: "/api/version",

	// Authentication
	AuthLogoutEndpoint:   "/api/auth/logout",
	AuthMeEndpoint:       "/api/auth/me",
	AuthPasswordEndpoint: "/api/auth/password",
	AuthRefreshEndpoint:  "/api/auth/refresh",

	// OIDC
	OIDCDeviceCodeEndpoint:  "/api/oidc/device/code",
	OIDCDeviceTokenEndpoint: "/api/oidc/device/token",
	OIDCStatusEndpoint:      "/api/oidc/status",

	// API Keys
	ApiKeysEndpoint: "/api/api-keys",
	ApiKeyEndpoint:  "/api/api-keys/%s",

	// Users
	UsersEndpoint: "/api/users",
	UserEndpoint:  "/api/users/%s",

	// Environments
	EnvironmentsEndpoint:       "/api/environments",
	EnvironmentEndpoint:        "/api/environments/%s",
	EnvironmentTestEndpoint:    "/api/environments/%s/test",
	EnvironmentVersionEndpoint: "/api/environments/%s/version",

	// Containers
	ContainersEndpoint:       "/api/environments/%s/containers",
	ContainerEndpoint:        "/api/environments/%s/containers/%s",
	ContainerStartEndpoint:   "/api/environments/%s/containers/%s/start",
	ContainerStopEndpoint:    "/api/environments/%s/containers/%s/stop",
	ContainerRestartEndpoint: "/api/environments/%s/containers/%s/restart",
	ContainerUpdateEndpoint:  "/api/environments/%s/containers/%s/update",
	ContainersCountsEndpoint: "/api/environments/%s/containers/counts",

	// Images
	ImagesEndpoint:       "/api/environments/%s/images",
	ImageEndpoint:        "/api/environments/%s/images/%s",
	ImagesPullEndpoint:   "/api/environments/%s/images/pull",
	ImagesPruneEndpoint:  "/api/environments/%s/images/prune",
	ImagesCountsEndpoint: "/api/environments/%s/images/counts",
	ImagesUploadEndpoint: "/api/environments/%s/images/upload",

	// Image Updates
	ImageUpdatesCheckEndpoint:     "/api/environments/%s/image-updates/check",
	ImageUpdatesCheckAllEndpoint:  "/api/environments/%s/image-updates/check-all",
	ImageUpdatesCheckByIdEndpoint: "/api/environments/%s/image-updates/check/%s",
	ImageUpdatesSummaryEndpoint:   "/api/environments/%s/image-updates/summary",

	// Networks
	NetworksEndpoint:       "/api/environments/%s/networks",
	NetworkEndpoint:        "/api/environments/%s/networks/%s",
	NetworksCountsEndpoint: "/api/environments/%s/networks/counts",
	NetworksPruneEndpoint:  "/api/environments/%s/networks/prune",

	// Volumes
	VolumesEndpoint:       "/api/environments/%s/volumes",
	VolumeEndpoint:        "/api/environments/%s/volumes/%s",
	VolumesCountsEndpoint: "/api/environments/%s/volumes/counts",
	VolumesPruneEndpoint:  "/api/environments/%s/volumes/prune",
	VolumesSizesEndpoint:  "/api/environments/%s/volumes/sizes",
	VolumeUsageEndpoint:   "/api/environments/%s/volumes/%s/usage",

	// Projects (Stacks)
	ProjectsEndpoint:        "/api/environments/%s/projects",
	ProjectEndpoint:         "/api/environments/%s/projects/%s",
	ProjectsCountsEndpoint:  "/api/environments/%s/projects/counts",
	ProjectDestroyEndpoint:  "/api/environments/%s/projects/%s/destroy",
	ProjectUpEndpoint:       "/api/environments/%s/projects/%s/up",
	ProjectDownEndpoint:     "/api/environments/%s/projects/%s/down",
	ProjectRestartEndpoint:  "/api/environments/%s/projects/%s/restart",
	ProjectRedeployEndpoint: "/api/environments/%s/projects/%s/redeploy",
	ProjectPullEndpoint:     "/api/environments/%s/projects/%s/pull",
	ProjectIncludesEndpoint: "/api/environments/%s/projects/%s/includes",

	// System
	SystemPruneEndpoint:              "/api/environments/%s/system/prune",
	SystemDockerInfoEndpoint:         "/api/environments/%s/system/docker/info",
	SystemContainersStartAllEndpoint: "/api/environments/%s/system/containers/start-all",
	SystemContainersStopAllEndpoint:  "/api/environments/%s/system/containers/stop-all",
	SystemStartStoppedEndpoint:       "/api/environments/%s/system/containers/start-stopped",
	SystemConvertEndpoint:            "/api/environments/%s/system/convert",
	SystemUpgradeEndpoint:            "/api/environments/%s/system/upgrade",
	SystemUpgradeCheckEndpoint:       "/api/environments/%s/system/upgrade/check",

	// Updater
	UpdaterStatusEndpoint:  "/api/environments/%s/updater/status",
	UpdaterRunEndpoint:     "/api/environments/%s/updater/run",
	UpdaterHistoryEndpoint: "/api/environments/%s/updater/history",

	// Job Schedules
	JobSchedulesEndpoint: "/api/environments/%s/job-schedules",

	// Settings
	SettingsEndpoint:       "/api/environments/%s/settings",
	SettingsPublicEndpoint: "/api/environments/%s/settings/public",

	// Notifications
	NotificationsAppriseEndpoint:         "/api/environments/%s/notifications/apprise",
	NotificationsAppriseTestEndpoint:     "/api/environments/%s/notifications/apprise/test",
	NotificationsSettingsEndpoint:        "/api/environments/%s/notifications/settings",
	NotificationSettingsProviderEndpoint: "/api/environments/%s/notifications/settings/%s",
	NotificationsTestProviderEndpoint:    "/api/environments/%s/notifications/test/%s",

	// Container Registries
	ContainerRegistriesEndpoint:   "/api/container-registries",
	ContainerRegistryEndpoint:     "/api/container-registries/%s",
	ContainerRegistrySyncEndpoint: "/api/container-registries/sync",
	ContainerRegistryTestEndpoint: "/api/container-registries/%s/test",

	// Events
	EventsEndpoint:            "/api/events",
	EventEndpoint:             "/api/events/%s",
	EventsEnvironmentEndpoint: "/api/events/environment/%s",

	// Templates
	TemplatesEndpoint:           "/api/templates",
	TemplateEndpoint:            "/api/templates/%s",
	TemplatesAllEndpoint:        "/api/templates/all",
	TemplatesDefaultEndpoint:    "/api/templates/default",
	TemplatesRegistriesEndpoint: "/api/templates/registries",
	TemplateRegistryEndpoint:    "/api/templates/registries/%s",
	TemplatesVariablesEndpoint:  "/api/templates/variables",
	TemplateContentEndpoint:     "/api/templates/%s/content",
	TemplateDownloadEndpoint:    "/api/templates/%s/download",
	TemplateFetchEndpoint:       "/api/templates/fetch",

	// Dashboard
	DashboardActionItemsEndpoint: "/api/environments/%s/dashboard/action-items",

	// Assets
	AppImagesFaviconEndpoint: "/api/app-images/favicon",
	AppImagesLogoEndpoint:    "/api/app-images/logo",
	AppImagesProfileEndpoint: "/api/app-images/profile",
	FontsMonoEndpoint:        "/api/fonts/mono",
	FontsSansEndpoint:        "/api/fonts/sans",
	FontsSerifEndpoint:       "/api/fonts/serif",

	// Customization
	CustomizeCategoriesEndpoint: "/api/customize/categories",
	CustomizeSearchEndpoint:     "/api/customize/search",

	// GitOps Syncs
	GitOpsSyncsEndpoint:       "/api/environments/%s/gitops-syncs",
	GitOpsSyncEndpoint:        "/api/environments/%s/gitops-syncs/%s",
	GitOpsSyncStatusEndpoint:  "/api/environments/%s/gitops-syncs/%s/status",
	GitOpsSyncTriggerEndpoint: "/api/environments/%s/gitops-syncs/%s/sync",
	GitOpsSyncFilesEndpoint:   "/api/environments/%s/gitops-syncs/%s/files",
	GitOpsSyncsImportEndpoint: "/api/environments/%s/gitops-syncs/import",

	// Git Repositories
	GitRepositoriesEndpoint:       "/api/customize/git-repositories",
	GitRepositoryEndpoint:         "/api/customize/git-repositories/%s",
	GitRepositoryTestEndpoint:     "/api/customize/git-repositories/%s/test",
	GitRepositoryBranchesEndpoint: "/api/customize/git-repositories/%s/branches",
	GitRepositoryFilesEndpoint:    "/api/customize/git-repositories/%s/files",
	GitRepositoriesSyncEndpoint:   "/api/git-repositories/sync",
}

// Auth endpoints
func (e ArcaneApiEndpoints) AuthLogout() string   { return e.AuthLogoutEndpoint }
func (e ArcaneApiEndpoints) AuthMe() string       { return e.AuthMeEndpoint }
func (e ArcaneApiEndpoints) AuthPassword() string { return e.AuthPasswordEndpoint }
func (e ArcaneApiEndpoints) AuthRefresh() string  { return e.AuthRefreshEndpoint }

// OIDC endpoints
func (e ArcaneApiEndpoints) OIDCDeviceCode() string  { return e.OIDCDeviceCodeEndpoint }
func (e ArcaneApiEndpoints) OIDCDeviceToken() string { return e.OIDCDeviceTokenEndpoint }
func (e ArcaneApiEndpoints) OIDCStatus() string      { return e.OIDCStatusEndpoint }

// API Key endpoints
func (e ArcaneApiEndpoints) ApiKeys() string         { return e.ApiKeysEndpoint }
func (e ArcaneApiEndpoints) ApiKey(id string) string { return fmt.Sprintf(e.ApiKeyEndpoint, id) }

// User endpoints
func (e ArcaneApiEndpoints) Users() string         { return e.UsersEndpoint }
func (e ArcaneApiEndpoints) User(id string) string { return fmt.Sprintf(e.UserEndpoint, id) }

// Environment endpoints
func (e ArcaneApiEndpoints) Environments() string { return e.EnvironmentsEndpoint }

func (e ArcaneApiEndpoints) Environment(id string) string {
	return fmt.Sprintf(e.EnvironmentEndpoint, id)
}

func (e ArcaneApiEndpoints) EnvironmentTest(envID string) string {
	return fmt.Sprintf(e.EnvironmentTestEndpoint, envID)
}

func (e ArcaneApiEndpoints) EnvironmentVersion(envID string) string {
	return fmt.Sprintf(e.EnvironmentVersionEndpoint, envID)
}

// Container endpoints
func (e ArcaneApiEndpoints) Containers(envID string) string {
	return fmt.Sprintf(e.ContainersEndpoint, envID)
}

func (e ArcaneApiEndpoints) Container(envID, containerID string) string {
	return fmt.Sprintf(e.ContainerEndpoint, envID, containerID)
}

func (e ArcaneApiEndpoints) ContainerStart(envID, containerID string) string {
	return fmt.Sprintf(e.ContainerStartEndpoint, envID, containerID)
}

func (e ArcaneApiEndpoints) ContainerStop(envID, containerID string) string {
	return fmt.Sprintf(e.ContainerStopEndpoint, envID, containerID)
}

func (e ArcaneApiEndpoints) ContainerRestart(envID, containerID string) string {
	return fmt.Sprintf(e.ContainerRestartEndpoint, envID, containerID)
}

func (e ArcaneApiEndpoints) ContainerUpdate(envID, containerID string) string {
	return fmt.Sprintf(e.ContainerUpdateEndpoint, envID, containerID)
}

func (e ArcaneApiEndpoints) ContainersCounts(envID string) string {
	return fmt.Sprintf(e.ContainersCountsEndpoint, envID)
}

// Image endpoints
func (e ArcaneApiEndpoints) Images(envID string) string { return fmt.Sprintf(e.ImagesEndpoint, envID) }

func (e ArcaneApiEndpoints) Image(envID, imageID string) string {
	return fmt.Sprintf(e.ImageEndpoint, envID, imageID)
}

func (e ArcaneApiEndpoints) ImagesPull(envID string) string {
	return fmt.Sprintf(e.ImagesPullEndpoint, envID)
}

func (e ArcaneApiEndpoints) ImagesPrune(envID string) string {
	return fmt.Sprintf(e.ImagesPruneEndpoint, envID)
}

func (e ArcaneApiEndpoints) ImagesCounts(envID string) string {
	return fmt.Sprintf(e.ImagesCountsEndpoint, envID)
}

func (e ArcaneApiEndpoints) ImagesUpload(envID string) string {
	return fmt.Sprintf(e.ImagesUploadEndpoint, envID)
}

// Image Update endpoints
func (e ArcaneApiEndpoints) ImageUpdatesCheck(envID string) string {
	return fmt.Sprintf(e.ImageUpdatesCheckEndpoint, envID)
}

func (e ArcaneApiEndpoints) ImageUpdatesCheckAll(envID string) string {
	return fmt.Sprintf(e.ImageUpdatesCheckAllEndpoint, envID)
}

func (e ArcaneApiEndpoints) ImageUpdatesCheckById(envID, imageID string) string {
	return fmt.Sprintf(e.ImageUpdatesCheckByIdEndpoint, envID, imageID)
}

func (e ArcaneApiEndpoints) ImageUpdatesSummary(envID string) string {
	return fmt.Sprintf(e.ImageUpdatesSummaryEndpoint, envID)
}

// Network endpoints
func (e ArcaneApiEndpoints) Networks(envID string) string {
	return fmt.Sprintf(e.NetworksEndpoint, envID)
}

func (e ArcaneApiEndpoints) Network(envID, networkID string) string {
	return fmt.Sprintf(e.NetworkEndpoint, envID, networkID)
}

func (e ArcaneApiEndpoints) NetworksCounts(envID string) string {
	return fmt.Sprintf(e.NetworksCountsEndpoint, envID)
}

func (e ArcaneApiEndpoints) NetworksPrune(envID string) string {
	return fmt.Sprintf(e.NetworksPruneEndpoint, envID)
}

// Volume endpoints
func (e ArcaneApiEndpoints) Volumes(envID string) string {
	return fmt.Sprintf(e.VolumesEndpoint, envID)
}

func (e ArcaneApiEndpoints) Volume(envID, volumeName string) string {
	return fmt.Sprintf(e.VolumeEndpoint, envID, volumeName)
}

func (e ArcaneApiEndpoints) VolumesCounts(envID string) string {
	return fmt.Sprintf(e.VolumesCountsEndpoint, envID)
}

func (e ArcaneApiEndpoints) VolumesPrune(envID string) string {
	return fmt.Sprintf(e.VolumesPruneEndpoint, envID)
}

func (e ArcaneApiEndpoints) VolumesSizes(envID string) string {
	return fmt.Sprintf(e.VolumesSizesEndpoint, envID)
}

func (e ArcaneApiEndpoints) VolumeUsage(envID, volumeName string) string {
	return fmt.Sprintf(e.VolumeUsageEndpoint, envID, volumeName)
}

// Project endpoints
func (e ArcaneApiEndpoints) Projects(envID string) string {
	return fmt.Sprintf(e.ProjectsEndpoint, envID)
}

func (e ArcaneApiEndpoints) Project(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectsCounts(envID string) string {
	return fmt.Sprintf(e.ProjectsCountsEndpoint, envID)
}

func (e ArcaneApiEndpoints) ProjectDestroy(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectDestroyEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectUp(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectUpEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectDown(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectDownEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectRestart(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectRestartEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectRedeploy(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectRedeployEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectPull(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectPullEndpoint, envID, projectID)
}

func (e ArcaneApiEndpoints) ProjectIncludes(envID, projectID string) string {
	return fmt.Sprintf(e.ProjectIncludesEndpoint, envID, projectID)
}

// System endpoints
func (e ArcaneApiEndpoints) SystemPrune(envID string) string {
	return fmt.Sprintf(e.SystemPruneEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemDockerInfo(envID string) string {
	return fmt.Sprintf(e.SystemDockerInfoEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemContainersStartAll(envID string) string {
	return fmt.Sprintf(e.SystemContainersStartAllEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemContainersStopAll(envID string) string {
	return fmt.Sprintf(e.SystemContainersStopAllEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemStartStopped(envID string) string {
	return fmt.Sprintf(e.SystemStartStoppedEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemConvert(envID string) string {
	return fmt.Sprintf(e.SystemConvertEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemUpgrade(envID string) string {
	return fmt.Sprintf(e.SystemUpgradeEndpoint, envID)
}

func (e ArcaneApiEndpoints) SystemUpgradeCheck(envID string) string {
	return fmt.Sprintf(e.SystemUpgradeCheckEndpoint, envID)
}

// Updater endpoints
func (e ArcaneApiEndpoints) UpdaterStatus(envID string) string {
	return fmt.Sprintf(e.UpdaterStatusEndpoint, envID)
}

func (e ArcaneApiEndpoints) UpdaterRun(envID string) string {
	return fmt.Sprintf(e.UpdaterRunEndpoint, envID)
}

func (e ArcaneApiEndpoints) UpdaterHistory(envID string) string {
	return fmt.Sprintf(e.UpdaterHistoryEndpoint, envID)
}

// Job schedule endpoints
func (e ArcaneApiEndpoints) JobSchedules(envID string) string {
	return fmt.Sprintf(e.JobSchedulesEndpoint, envID)
}

// Settings endpoints
func (e ArcaneApiEndpoints) Settings(envID string) string {
	return fmt.Sprintf(e.SettingsEndpoint, envID)
}

func (e ArcaneApiEndpoints) SettingsPublic(envID string) string {
	return fmt.Sprintf(e.SettingsPublicEndpoint, envID)
}

// Notification endpoints
func (e ArcaneApiEndpoints) NotificationsApprise(envID string) string {
	return fmt.Sprintf(e.NotificationsAppriseEndpoint, envID)
}

func (e ArcaneApiEndpoints) NotificationsAppriseTest(envID string) string {
	return fmt.Sprintf(e.NotificationsAppriseTestEndpoint, envID)
}

func (e ArcaneApiEndpoints) NotificationsSettings(envID string) string {
	return fmt.Sprintf(e.NotificationsSettingsEndpoint, envID)
}

func (e ArcaneApiEndpoints) NotificationSettingsProvider(envID, provider string) string {
	return fmt.Sprintf(e.NotificationSettingsProviderEndpoint, envID, provider)
}

func (e ArcaneApiEndpoints) NotificationsTestProvider(envID, provider string) string {
	return fmt.Sprintf(e.NotificationsTestProviderEndpoint, envID, provider)
}

// Container Registry endpoints
func (e ArcaneApiEndpoints) ContainerRegistries() string { return e.ContainerRegistriesEndpoint }

func (e ArcaneApiEndpoints) ContainerRegistry(id string) string {
	return fmt.Sprintf(e.ContainerRegistryEndpoint, id)
}
func (e ArcaneApiEndpoints) ContainerRegistrySync() string { return e.ContainerRegistrySyncEndpoint }
func (e ArcaneApiEndpoints) ContainerRegistryTest(id string) string {
	return fmt.Sprintf(e.ContainerRegistryTestEndpoint, id)
}

// Event endpoints
func (e ArcaneApiEndpoints) Events() string         { return e.EventsEndpoint }
func (e ArcaneApiEndpoints) Event(id string) string { return fmt.Sprintf(e.EventEndpoint, id) }
func (e ArcaneApiEndpoints) EventsEnvironment(envID string) string {
	return fmt.Sprintf(e.EventsEnvironmentEndpoint, envID)
}

// Template endpoints
func (e ArcaneApiEndpoints) Templates() string           { return e.TemplatesEndpoint }
func (e ArcaneApiEndpoints) Template(id string) string   { return fmt.Sprintf(e.TemplateEndpoint, id) }
func (e ArcaneApiEndpoints) TemplatesAll() string        { return e.TemplatesAllEndpoint }
func (e ArcaneApiEndpoints) TemplatesDefault() string    { return e.TemplatesDefaultEndpoint }
func (e ArcaneApiEndpoints) TemplatesRegistries() string { return e.TemplatesRegistriesEndpoint }
func (e ArcaneApiEndpoints) TemplateRegistry(id string) string {
	return fmt.Sprintf(e.TemplateRegistryEndpoint, id)
}
func (e ArcaneApiEndpoints) TemplatesVariables() string { return e.TemplatesVariablesEndpoint }
func (e ArcaneApiEndpoints) TemplateContent(id string) string {
	return fmt.Sprintf(e.TemplateContentEndpoint, id)
}
func (e ArcaneApiEndpoints) TemplateDownload(id string) string {
	return fmt.Sprintf(e.TemplateDownloadEndpoint, id)
}
func (e ArcaneApiEndpoints) TemplateFetch() string { return e.TemplateFetchEndpoint }

// Dashboard endpoints
func (e ArcaneApiEndpoints) DashboardActionItems(envID string) string {
	return fmt.Sprintf(e.DashboardActionItemsEndpoint, envID)
}

// GitOps Sync endpoints
func (e ArcaneApiEndpoints) GitOpsSyncs(envID string) string {
	return fmt.Sprintf(e.GitOpsSyncsEndpoint, envID)
}
func (e ArcaneApiEndpoints) GitOpsSync(envID, syncID string) string {
	return fmt.Sprintf(e.GitOpsSyncEndpoint, envID, syncID)
}
func (e ArcaneApiEndpoints) GitOpsSyncStatus(envID, syncID string) string {
	return fmt.Sprintf(e.GitOpsSyncStatusEndpoint, envID, syncID)
}
func (e ArcaneApiEndpoints) GitOpsSyncTrigger(envID, syncID string) string {
	return fmt.Sprintf(e.GitOpsSyncTriggerEndpoint, envID, syncID)
}
func (e ArcaneApiEndpoints) GitOpsSyncFiles(envID, syncID string) string {
	return fmt.Sprintf(e.GitOpsSyncFilesEndpoint, envID, syncID)
}
func (e ArcaneApiEndpoints) GitOpsSyncsImport(envID string) string {
	return fmt.Sprintf(e.GitOpsSyncsImportEndpoint, envID)
}

// Git Repository endpoints
func (e ArcaneApiEndpoints) GitRepositories() string { return e.GitRepositoriesEndpoint }
func (e ArcaneApiEndpoints) GitRepository(id string) string {
	return fmt.Sprintf(e.GitRepositoryEndpoint, id)
}
func (e ArcaneApiEndpoints) GitRepositoryTest(id string) string {
	return fmt.Sprintf(e.GitRepositoryTestEndpoint, id)
}
func (e ArcaneApiEndpoints) GitRepositoryBranches(id string) string {
	return fmt.Sprintf(e.GitRepositoryBranchesEndpoint, id)
}
func (e ArcaneApiEndpoints) GitRepositoryFiles(id string) string {
	return fmt.Sprintf(e.GitRepositoryFilesEndpoint, id)
}
func (e ArcaneApiEndpoints) GitRepositoriesSync() string { return e.GitRepositoriesSyncEndpoint }
