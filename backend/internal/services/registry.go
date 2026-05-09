package services

// Registry is the runtime aggregator that bundles every domain service
// together. Bootstrap constructs it once at startup; subsystems that need
// access to multiple services (gRPC handlers, REST handlers, schedulers)
// receive a *Registry rather than chasing individual references.
//
// Treat fields as nil-safe at the boundary; downstream callers should still
// nil-check before use.
type Registry struct {
	AppImages         *ApplicationImagesService
	User              *UserService
	Project           *ProjectService
	Environment       *EnvironmentService
	Settings          *SettingsService
	KV                *KVService
	JobSchedule       *JobService
	SettingsSearch    *SettingsSearchService
	CustomizeSearch   *CustomizeSearchService
	Container         *ContainerService
	Image             *ImageService
	Build             *BuildService
	BuildWorkspace    *BuildWorkspaceService
	Volume            *VolumeService
	Network           *NetworkService
	Port              *PortService
	Swarm             *SwarmService
	ImageUpdate       *ImageUpdateService
	Auth              *AuthService
	Oidc              *OidcService
	Docker            *DockerClientService
	Template          *TemplateService
	ContainerRegistry *ContainerRegistryService
	System            *SystemService
	SystemUpgrade     *SystemUpgradeService
	Updater           *UpdaterService
	Event             *EventService
	Version           *VersionService
	Notification      *NotificationService
	Apprise           *AppriseService //nolint:staticcheck // Apprise still functional, deprecated in favor of Shoutrrr
	ApiKey            *ApiKeyService
	GitRepository     *GitRepositoryService
	GitOpsSync        *GitOpsSyncService
	Webhook           *WebhookService
	Font              *FontService
	Vulnerability     *VulnerabilityService
	Dashboard         *DashboardService
	Device            *DeviceService
	Pairing           *PairingService
}
