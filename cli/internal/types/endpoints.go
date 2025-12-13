package types

import "fmt"

// ArcaneApiEndpoints holds the API endpoint path templates for the Arcane API.
// Endpoint paths may contain format specifiers (e.g., %s) for environment IDs or resource IDs.
type ArcaneApiEndpoints struct {
	// Version & Health
	AppVersionEndpoint string
	VersionEndpoint    string
	HealthEndpoint     string

	// Authentication
	AuthLoginEndpoint    string
	AuthLogoutEndpoint   string
	AuthMeEndpoint       string
	AuthPasswordEndpoint string
	AuthRefreshEndpoint  string

	// OIDC
	OIDCStatusEndpoint   string
	OIDCConfigEndpoint   string
	OIDCUrlEndpoint      string
	OIDCCallbackEndpoint string

	// API Keys
	ApiKeysEndpoint string
	ApiKeyEndpoint  string

	// Users
	UsersEndpoint string
	UserEndpoint  string

	// Environments
	EnvironmentsEndpoint     string
	EnvironmentEndpoint      string
	EnvironmentPairEndpoint  string
	EnvironmentAgentEndpoint string
	EnvironmentTestEndpoint  string

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
	ImageUpdatesCheckEndpoint      string
	ImageUpdatesCheckAllEndpoint   string
	ImageUpdatesCheckBatchEndpoint string
	ImageUpdatesCheckByIdEndpoint  string
	ImageUpdatesSummaryEndpoint    string

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
	SystemPruneEndpoint                  string
	SystemHealthEndpoint                 string
	SystemDockerInfoEndpoint             string
	SystemConvertEndpoint                string
	SystemContainersStartAllEndpoint     string
	SystemContainersStopAllEndpoint      string
	SystemContainersStartStoppedEndpoint string
	SystemUpgradeCheckEndpoint           string
	SystemUpgradeEndpoint                string

	// Updater
	UpdaterStatusEndpoint  string
	UpdaterRunEndpoint     string
	UpdaterHistoryEndpoint string

	// Settings
	SettingsEndpoint           string
	SettingsPublicEndpoint     string
	SettingsCategoriesEndpoint string
	SettingsSearchEndpoint     string

	// Notifications
	NotificationsAppriseEndpoint     string
	NotificationsAppriseTestEndpoint string
	NotificationsSettingsEndpoint    string
	NotificationSettingEndpoint      string
	NotificationTestEndpoint         string

	// Container Registries
	ContainerRegistriesEndpoint       string
	ContainerRegistryEndpoint         string
	ContainerRegistrySyncEndpoint     string
	ContainerRegistryTestEndpoint     string
	EnvironmentSyncRegistriesEndpoint string

	// Events
	EventsEndpoint            string
	EventEndpoint             string
	EventsEnvironmentEndpoint string

	// Templates
	TemplatesEndpoint           string
	TemplateEndpoint            string
	TemplatesAllEndpoint        string
	TemplatesDefaultEndpoint    string
	TemplatesFetchEndpoint      string
	TemplatesRegistriesEndpoint string
	TemplateRegistryEndpoint    string
	TemplatesVariablesEndpoint  string
	TemplateContentEndpoint     string
	TemplateDownloadEndpoint    string

	// Deployment
	DeploymentEndpoint string
	HeartbeatEndpoint  string

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
}

// Endpoints contains the defined API endpoints
var Endpoints = ArcaneApiEndpoints{
	// Version & Health
	AppVersionEndpoint: "/api/app-version",
	VersionEndpoint:    "/api/version",
	HealthEndpoint:     "/api/health",

	// Authentication
	AuthLoginEndpoint:    "/api/auth/login",
	AuthLogoutEndpoint:   "/api/auth/logout",
	AuthMeEndpoint:       "/api/auth/me",
	AuthPasswordEndpoint: "/api/auth/password",
	AuthRefreshEndpoint:  "/api/auth/refresh",

	// OIDC
	OIDCStatusEndpoint:   "/api/oidc/status",
	OIDCConfigEndpoint:   "/api/oidc/config",
	OIDCUrlEndpoint:      "/api/oidc/url",
	OIDCCallbackEndpoint: "/api/oidc/callback",

	// API Keys
	ApiKeysEndpoint: "/api/api-keys",
	ApiKeyEndpoint:  "/api/api-keys/%s",

	// Users
	UsersEndpoint: "/api/users",
	UserEndpoint:  "/api/users/%s",

	// Environments
	EnvironmentsEndpoint:     "/api/environments",
	EnvironmentEndpoint:      "/api/environments/%s",
	EnvironmentPairEndpoint:  "/api/environments/pair",
	EnvironmentAgentEndpoint: "/api/environments/%s/agent/pair",
	EnvironmentTestEndpoint:  "/api/environments/%s/test",

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
	ImageUpdatesCheckEndpoint:      "/api/environments/%s/image-updates/check",
	ImageUpdatesCheckAllEndpoint:   "/api/environments/%s/image-updates/check-all",
	ImageUpdatesCheckBatchEndpoint: "/api/environments/%s/image-updates/check-batch",
	ImageUpdatesCheckByIdEndpoint:  "/api/environments/%s/image-updates/check/%s",
	ImageUpdatesSummaryEndpoint:    "/api/environments/%s/image-updates/summary",

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
	SystemPruneEndpoint:                  "/api/environments/%s/system/prune",
	SystemHealthEndpoint:                 "/api/environments/%s/system/health",
	SystemDockerInfoEndpoint:             "/api/environments/%s/system/docker/info",
	SystemConvertEndpoint:                "/api/environments/%s/system/convert",
	SystemContainersStartAllEndpoint:     "/api/environments/%s/system/containers/start-all",
	SystemContainersStopAllEndpoint:      "/api/environments/%s/system/containers/stop-all",
	SystemContainersStartStoppedEndpoint: "/api/environments/%s/system/containers/start-stopped",
	SystemUpgradeCheckEndpoint:           "/api/environments/%s/system/upgrade/check",
	SystemUpgradeEndpoint:                "/api/environments/%s/system/upgrade",

	// Updater
	UpdaterStatusEndpoint:  "/api/environments/%s/updater/status",
	UpdaterRunEndpoint:     "/api/environments/%s/updater/run",
	UpdaterHistoryEndpoint: "/api/environments/%s/updater/history",

	// Settings
	SettingsEndpoint:           "/api/environments/%s/settings",
	SettingsPublicEndpoint:     "/api/environments/%s/settings/public",
	SettingsCategoriesEndpoint: "/api/settings/categories",
	SettingsSearchEndpoint:     "/api/settings/search",

	// Notifications
	NotificationsAppriseEndpoint:     "/api/environments/%s/notifications/apprise",
	NotificationsAppriseTestEndpoint: "/api/environments/%s/notifications/apprise/test",
	NotificationsSettingsEndpoint:    "/api/environments/%s/notifications/settings",
	NotificationSettingEndpoint:      "/api/environments/%s/notifications/settings/%s",
	NotificationTestEndpoint:         "/api/environments/%s/notifications/test/%s",

	// Container Registries
	ContainerRegistriesEndpoint:       "/api/container-registries",
	ContainerRegistryEndpoint:         "/api/container-registries/%s",
	ContainerRegistrySyncEndpoint:     "/api/container-registries/sync",
	ContainerRegistryTestEndpoint:     "/api/container-registries/%s/test",
	EnvironmentSyncRegistriesEndpoint: "/api/environments/%s/sync-registries",

	// Events
	EventsEndpoint:            "/api/events",
	EventEndpoint:             "/api/events/%s",
	EventsEnvironmentEndpoint: "/api/events/environment/%s",

	// Templates
	TemplatesEndpoint:           "/api/templates",
	TemplateEndpoint:            "/api/templates/%s",
	TemplatesAllEndpoint:        "/api/templates/all",
	TemplatesDefaultEndpoint:    "/api/templates/default",
	TemplatesFetchEndpoint:      "/api/templates/fetch",
	TemplatesRegistriesEndpoint: "/api/templates/registries",
	TemplateRegistryEndpoint:    "/api/templates/registries/%s",
	TemplatesVariablesEndpoint:  "/api/templates/variables",
	TemplateContentEndpoint:     "/api/templates/%s/content",
	TemplateDownloadEndpoint:    "/api/templates/%s/download",

	// Deployment & Heartbeat
	DeploymentEndpoint: "/api/environments/%s/deployment",
	HeartbeatEndpoint:  "/api/environments/%s/heartbeat",

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
}

// Auth endpoints
func (e ArcaneApiEndpoints) AuthLogin() string    { return e.AuthLoginEndpoint }
func (e ArcaneApiEndpoints) AuthLogout() string   { return e.AuthLogoutEndpoint }
func (e ArcaneApiEndpoints) AuthMe() string       { return e.AuthMeEndpoint }
func (e ArcaneApiEndpoints) AuthPassword() string { return e.AuthPasswordEndpoint }
func (e ArcaneApiEndpoints) AuthRefresh() string  { return e.AuthRefreshEndpoint }

// OIDC endpoints
func (e ArcaneApiEndpoints) OIDCStatus() string   { return e.OIDCStatusEndpoint }
func (e ArcaneApiEndpoints) OIDCConfig() string   { return e.OIDCConfigEndpoint }
func (e ArcaneApiEndpoints) OIDCUrl() string      { return e.OIDCUrlEndpoint }
func (e ArcaneApiEndpoints) OIDCCallback() string { return e.OIDCCallbackEndpoint }

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
func (e ArcaneApiEndpoints) EnvironmentPair() string { return e.EnvironmentPairEndpoint }
func (e ArcaneApiEndpoints) EnvironmentAgent(envID string) string {
	return fmt.Sprintf(e.EnvironmentAgentEndpoint, envID)
}
func (e ArcaneApiEndpoints) EnvironmentTest(envID string) string {
	return fmt.Sprintf(e.EnvironmentTestEndpoint, envID)
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
func (e ArcaneApiEndpoints) ImageUpdatesCheckBatch(envID string) string {
	return fmt.Sprintf(e.ImageUpdatesCheckBatchEndpoint, envID)
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
func (e ArcaneApiEndpoints) SystemHealth(envID string) string {
	return fmt.Sprintf(e.SystemHealthEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemDockerInfo(envID string) string {
	return fmt.Sprintf(e.SystemDockerInfoEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemConvert(envID string) string {
	return fmt.Sprintf(e.SystemConvertEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemContainersStartAll(envID string) string {
	return fmt.Sprintf(e.SystemContainersStartAllEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemContainersStopAll(envID string) string {
	return fmt.Sprintf(e.SystemContainersStopAllEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemContainersStartStopped(envID string) string {
	return fmt.Sprintf(e.SystemContainersStartStoppedEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemUpgradeCheck(envID string) string {
	return fmt.Sprintf(e.SystemUpgradeCheckEndpoint, envID)
}
func (e ArcaneApiEndpoints) SystemUpgrade(envID string) string {
	return fmt.Sprintf(e.SystemUpgradeEndpoint, envID)
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

// Settings endpoints
func (e ArcaneApiEndpoints) Settings(envID string) string {
	return fmt.Sprintf(e.SettingsEndpoint, envID)
}
func (e ArcaneApiEndpoints) SettingsPublic(envID string) string {
	return fmt.Sprintf(e.SettingsPublicEndpoint, envID)
}
func (e ArcaneApiEndpoints) SettingsCategories() string { return e.SettingsCategoriesEndpoint }
func (e ArcaneApiEndpoints) SettingsSearch() string     { return e.SettingsSearchEndpoint }

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
func (e ArcaneApiEndpoints) NotificationSetting(envID, provider string) string {
	return fmt.Sprintf(e.NotificationSettingEndpoint, envID, provider)
}
func (e ArcaneApiEndpoints) NotificationTest(envID, provider string) string {
	return fmt.Sprintf(e.NotificationTestEndpoint, envID, provider)
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
func (e ArcaneApiEndpoints) EnvironmentSyncRegistries(envID string) string {
	return fmt.Sprintf(e.EnvironmentSyncRegistriesEndpoint, envID)
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
func (e ArcaneApiEndpoints) TemplatesFetch() string      { return e.TemplatesFetchEndpoint }
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

// Deployment & Heartbeat endpoints
func (e ArcaneApiEndpoints) Deployment(envID string) string {
	return fmt.Sprintf(e.DeploymentEndpoint, envID)
}
func (e ArcaneApiEndpoints) Heartbeat(envID string) string {
	return fmt.Sprintf(e.HeartbeatEndpoint, envID)
}

// Version & Health endpoints
func (e ArcaneApiEndpoints) AppVersion() string { return e.AppVersionEndpoint }
func (e ArcaneApiEndpoints) Version() string    { return e.VersionEndpoint }
func (e ArcaneApiEndpoints) Health() string     { return e.HealthEndpoint }

// Legacy methods for backwards compatibility
func (e ArcaneApiEndpoints) FormatContainers(envID string) string {
	return e.Containers(envID)
}

func (e ArcaneApiEndpoints) UseImageEndpoint(action string, envID string) string {
	switch action {
	case "list":
		return e.Images(envID)
	case "get":
		return e.Images(envID)
	case "pull":
		return e.ImagesPull(envID)
	case "delete":
		return e.Images(envID)
	case "prune":
		return e.ImagesPrune(envID)
	case "counts":
		return e.ImagesCounts(envID)
	case "upload":
		return e.ImagesUpload(envID)
	default:
		return ""
	}
}
