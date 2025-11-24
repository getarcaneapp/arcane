package common

import "fmt"

type InvalidRequestFormatError struct {
	Err error
}

func (e *InvalidRequestFormatError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("Invalid request format: %v", e.Err)
	}
	return "Invalid request format"
}

type AuthSettingsCheckError struct {
	Err error
}

func (e *AuthSettingsCheckError) Error() string {
	return "Failed to check authentication settings"
}

type LocalAuthDisabledError struct{}

func (e *LocalAuthDisabledError) Error() string {
	return "Local authentication is disabled"
}

type InvalidCredentialsError struct{}

func (e *InvalidCredentialsError) Error() string {
	return "Invalid username or password"
}

type AuthFailedError struct {
	Err error
}

func (e *AuthFailedError) Error() string {
	return "Authentication failed"
}

type UserMappingError struct {
	Err error
}

func (e *UserMappingError) Error() string {
	return "Failed to map user"
}

type NotAuthenticatedError struct{}

func (e *NotAuthenticatedError) Error() string {
	return "Not authenticated"
}

type UserRetrievalError struct {
	Err error
}

func (e *UserRetrievalError) Error() string {
	return "Failed to get user information"
}

type InvalidTokenError struct{}

func (e *InvalidTokenError) Error() string {
	return "Invalid or expired refresh token"
}

type TokenRefreshError struct {
	Err error
}

func (e *TokenRefreshError) Error() string {
	return "Failed to refresh token"
}

type PasswordRequiredError struct{}

func (e *PasswordRequiredError) Error() string {
	return "Current password is required"
}

type IncorrectPasswordError struct{}

func (e *IncorrectPasswordError) Error() string {
	return "Current password is incorrect"
}

type PasswordChangeError struct {
	Err error
}

func (e *PasswordChangeError) Error() string {
	return "Failed to change password"
}

type ImageRetrievalError struct {
	Err error
}

func (e *ImageRetrievalError) Error() string {
	return "Failed to retrieve image"
}

type ContainerIDRequiredError struct{}

func (e *ContainerIDRequiredError) Error() string {
	return "Container ID is required"
}

type WebSocketUpgradeError struct {
	Err error
}

func (e *WebSocketUpgradeError) Error() string {
	return fmt.Sprintf("Failed to upgrade connection: %v", e.Err)
}

type ExecCreationError struct {
	Err error
}

func (e *ExecCreationError) Error() string {
	return fmt.Sprintf("Error creating exec: %v", e.Err)
}

type ExecAttachError struct {
	Err error
}

func (e *ExecAttachError) Error() string {
	return fmt.Sprintf("Error attaching to exec: %v", e.Err)
}

type ContainerListError struct {
	Err error
}

func (e *ContainerListError) Error() string {
	return fmt.Sprintf("Failed to list containers: %v", e.Err)
}

type ContainerRetrievalError struct {
	Err error
}

func (e *ContainerRetrievalError) Error() string {
	return fmt.Sprintf("Failed to retrieve container: %v", e.Err)
}

type ContainerStartError struct {
	Err error
}

func (e *ContainerStartError) Error() string {
	return fmt.Sprintf("Failed to start container: %v", e.Err)
}

type ContainerStopError struct {
	Err error
}

func (e *ContainerStopError) Error() string {
	return fmt.Sprintf("Failed to stop container: %v", e.Err)
}

type ContainerRestartError struct {
	Err error
}

func (e *ContainerRestartError) Error() string {
	return fmt.Sprintf("Failed to restart container: %v", e.Err)
}

type ContainerDeleteError struct {
	Err error
}

func (e *ContainerDeleteError) Error() string {
	return fmt.Sprintf("Failed to delete container: %v", e.Err)
}

type ContainerStatusCountsError struct {
	Err error
}

func (e *ContainerStatusCountsError) Error() string {
	return fmt.Sprintf("Failed to get container counts: %v", e.Err)
}

type InvalidPortFormatError struct {
	Err error
}

func (e *InvalidPortFormatError) Error() string {
	return fmt.Sprintf("Invalid port format: %v", e.Err)
}

type ContainerCreationError struct {
	Err error
}

func (e *ContainerCreationError) Error() string {
	return fmt.Sprintf("Failed to create container: %v", e.Err)
}

type RegistryListError struct {
	Err error
}

func (e *RegistryListError) Error() string {
	return fmt.Sprintf("Failed to list registries: %v", e.Err)
}

type RegistryMappingError struct {
	Err error
}

func (e *RegistryMappingError) Error() string {
	return fmt.Sprintf("Failed to map registry: %v", e.Err)
}

type RegistryRetrievalError struct {
	Err error
}

func (e *RegistryRetrievalError) Error() string {
	return fmt.Sprintf("Failed to retrieve registry: %v", e.Err)
}

type RegistryCreationError struct {
	Err error
}

func (e *RegistryCreationError) Error() string {
	return fmt.Sprintf("Failed to create registry: %v", e.Err)
}

type RegistryUpdateError struct {
	Err error
}

func (e *RegistryUpdateError) Error() string {
	return fmt.Sprintf("Failed to update registry: %v", e.Err)
}

type RegistryDeletionError struct {
	Err error
}

func (e *RegistryDeletionError) Error() string {
	return fmt.Sprintf("Failed to delete registry: %v", e.Err)
}

type TokenDecryptionError struct {
	Err error
}

func (e *TokenDecryptionError) Error() string {
	return fmt.Sprintf("Failed to decrypt token: %v", e.Err)
}

type RegistryTestError struct {
	Err error
}

func (e *RegistryTestError) Error() string {
	return fmt.Sprintf("Registry test failed: %v", e.Err)
}

type RegistrySyncError struct {
	Err error
}

func (e *RegistrySyncError) Error() string {
	return fmt.Sprintf("Failed to sync registries: %v", e.Err)
}

type QueryParameterRequiredError struct{}

func (e *QueryParameterRequiredError) Error() string {
	return "Query parameter is required"
}

type EnvironmentNotFoundError struct{}

func (e *EnvironmentNotFoundError) Error() string {
	return "Environment not found"
}

type AgentTokenPersistenceError struct {
	Err error
}

func (e *AgentTokenPersistenceError) Error() string {
	return "Failed to persist agent token"
}

type AgentPairingError struct {
	Err error
}

func (e *AgentPairingError) Error() string {
	return fmt.Sprintf("Agent pairing failed: %v", e.Err)
}

type EnvironmentCreationError struct {
	Err error
}

func (e *EnvironmentCreationError) Error() string {
	return fmt.Sprintf("Failed to create environment: %v", e.Err)
}

type EnvironmentMappingError struct {
	Err error
}

func (e *EnvironmentMappingError) Error() string {
	return "Failed to map environment"
}

type EnvironmentListError struct {
	Err error
}

func (e *EnvironmentListError) Error() string {
	return "Failed to fetch environments"
}

type EnvironmentUpdateError struct {
	Err error
}

func (e *EnvironmentUpdateError) Error() string {
	return "Failed to update environment"
}

type LocalEnvironmentDeletionError struct{}

func (e *LocalEnvironmentDeletionError) Error() string {
	return "Cannot delete local environment"
}

type EnvironmentDeletionError struct {
	Err error
}

func (e *EnvironmentDeletionError) Error() string {
	return fmt.Sprintf("Failed to delete environment: %v", e.Err)
}

type HeartbeatUpdateError struct {
	Err error
}

func (e *HeartbeatUpdateError) Error() string {
	return "Failed to update heartbeat"
}

type EventListError struct {
	Err error
}

func (e *EventListError) Error() string {
	return fmt.Sprintf("Failed to list events: %v", e.Err)
}

type EnvironmentIDRequiredError struct{}

func (e *EnvironmentIDRequiredError) Error() string {
	return "Environment ID is required"
}

type EventCreationError struct {
	Err error
}

func (e *EventCreationError) Error() string {
	return fmt.Sprintf("Failed to create event: %v", e.Err)
}

type EventIDRequiredError struct{}

func (e *EventIDRequiredError) Error() string {
	return "Event ID is required"
}

type EventDeletionError struct {
	Err error
}

func (e *EventDeletionError) Error() string {
	return fmt.Sprintf("Failed to delete event: %v", e.Err)
}

type ImageListError struct {
	Err error
}

func (e *ImageListError) Error() string {
	return fmt.Sprintf("Failed to list images: %v", e.Err)
}

type ImageNotFoundError struct {
	Err error
}

func (e *ImageNotFoundError) Error() string {
	return fmt.Sprintf("Image not found: %v", e.Err)
}

type ImageRemovalError struct {
	Err error
}

func (e *ImageRemovalError) Error() string {
	return fmt.Sprintf("Failed to remove image: %v", e.Err)
}

type ImagePullError struct {
	ImageName string
	Err       error
}

func (e *ImagePullError) Error() string {
	return fmt.Sprintf("Failed to pull image '%s': %v", e.ImageName, e.Err)
}

type ImagePruneError struct {
	Err error
}

func (e *ImagePruneError) Error() string {
	return fmt.Sprintf("Failed to prune images: %v", e.Err)
}

type ImageUsageCountsError struct {
	Err error
}

func (e *ImageUsageCountsError) Error() string {
	return fmt.Sprintf("Failed to get image usage counts: %v", e.Err)
}

type InvalidMultipartFormError struct {
	Err error
}

func (e *InvalidMultipartFormError) Error() string {
	return fmt.Sprintf("Invalid multipart form: %v", e.Err)
}

type FileUploadReadError struct {
	Err error
}

func (e *FileUploadReadError) Error() string {
	return fmt.Sprintf("Failed to read upload: %v", e.Err)
}

type NoFileUploadedError struct{}

func (e *NoFileUploadedError) Error() string {
	return "No file uploaded"
}

type InvalidFileFormatError struct{}

func (e *InvalidFileFormatError) Error() string {
	return "Invalid file format. Only Docker image tar archives are allowed (.tar, .tar.gz, .tgz, .tar.xz)"
}

type ImageLoadError struct {
	Err error
}

func (e *ImageLoadError) Error() string {
	return fmt.Sprintf("Failed to load image: %v", e.Err)
}

type ImageRefRequiredError struct{}

func (e *ImageRefRequiredError) Error() string {
	return "imageRef query parameter is required"
}

type ImageUpdateCheckError struct {
	Err error
}

func (e *ImageUpdateCheckError) Error() string {
	return fmt.Sprintf("Failed to check image update: %v", e.Err)
}

type ImageIDRequiredError struct{}

func (e *ImageIDRequiredError) Error() string {
	return "imageId parameter is required"
}

type ImageRefListRequiredError struct{}

func (e *ImageRefListRequiredError) Error() string {
	return "At least one imageRef is required"
}

type BatchImageUpdateCheckError struct {
	Err error
}

func (e *BatchImageUpdateCheckError) Error() string {
	return fmt.Sprintf("Failed to check image updates: %v", e.Err)
}

type AllImageUpdateCheckError struct {
	Err error
}

func (e *AllImageUpdateCheckError) Error() string {
	return fmt.Sprintf("Failed to check all images: %v", e.Err)
}

type UpdateSummaryError struct {
	Err error
}

func (e *UpdateSummaryError) Error() string {
	return fmt.Sprintf("Failed to get update summary: %v", e.Err)
}

type NetworkListError struct {
	Err error
}

func (e *NetworkListError) Error() string {
	return fmt.Sprintf("Failed to list networks: %v", e.Err)
}

type NetworkNotFoundError struct {
	Err error
}

func (e *NetworkNotFoundError) Error() string {
	return fmt.Sprintf("Network not found: %v", e.Err)
}

type NetworkMappingError struct {
	Err error
}

func (e *NetworkMappingError) Error() string {
	return fmt.Sprintf("Failed to map network: %v", e.Err)
}

type NetworkCreationError struct {
	Err error
}

func (e *NetworkCreationError) Error() string {
	return fmt.Sprintf("Failed to create network: %v", e.Err)
}

type NetworkRemovalError struct {
	Err error
}

func (e *NetworkRemovalError) Error() string {
	return fmt.Sprintf("Failed to remove network: %v", e.Err)
}

type NetworkUsageCountsError struct {
	Err error
}

func (e *NetworkUsageCountsError) Error() string {
	return fmt.Sprintf("Failed to get network counts: %v", e.Err)
}

type NetworkPruneError struct {
	Err error
}

func (e *NetworkPruneError) Error() string {
	return fmt.Sprintf("Failed to prune networks: %v", e.Err)
}

type NotificationSettingsListError struct {
	Err error
}

func (e *NotificationSettingsListError) Error() string {
	return fmt.Sprintf("Failed to list notification settings: %v", e.Err)
}

type InvalidNotificationProviderError struct{}

func (e *InvalidNotificationProviderError) Error() string {
	return "invalid provider"
}

type NotificationSettingsNotFoundError struct{}

func (e *NotificationSettingsNotFoundError) Error() string {
	return "Settings not found"
}

type NotificationSettingsUpdateError struct {
	Err error
}

func (e *NotificationSettingsUpdateError) Error() string {
	return fmt.Sprintf("Failed to update notification settings: %v", e.Err)
}

type NotificationSettingsDeletionError struct {
	Err error
}

func (e *NotificationSettingsDeletionError) Error() string {
	return fmt.Sprintf("Failed to delete notification settings: %v", e.Err)
}

type NotificationTestError struct {
	Err error
}

func (e *NotificationTestError) Error() string {
	return fmt.Sprintf("Failed to send test notification: %v", e.Err)
}

type AppriseSettingsNotFoundError struct{}

func (e *AppriseSettingsNotFoundError) Error() string {
	return "Apprise settings not found"
}

type AppriseSettingsUpdateError struct {
	Err error
}

func (e *AppriseSettingsUpdateError) Error() string {
	return fmt.Sprintf("Failed to update Apprise settings: %v", e.Err)
}

type AppriseTestError struct {
	Err error
}

func (e *AppriseTestError) Error() string {
	return fmt.Sprintf("Failed to send Apprise test notification: %v", e.Err)
}

type OidcStatusError struct {
	Err error
}

func (e *OidcStatusError) Error() string {
	return fmt.Sprintf("Failed to retrieve OIDC status: %v", e.Err)
}

type OidcStatusCheckError struct{}

func (e *OidcStatusCheckError) Error() string {
	return "Failed to check OIDC status"
}

type OidcDisabledError struct{}

func (e *OidcDisabledError) Error() string {
	return "OIDC authentication is disabled"
}

type OidcAuthUrlGenerationError struct {
	Err error
}

func (e *OidcAuthUrlGenerationError) Error() string {
	return fmt.Sprintf("Failed to generate OIDC auth URL: %v", e.Err)
}

type OidcStateCookieError struct{}

func (e *OidcStateCookieError) Error() string {
	return "Missing or invalid OIDC state cookie"
}

type OidcCallbackError struct {
	Err error
}

func (e *OidcCallbackError) Error() string {
	return fmt.Sprintf("OIDC callback failed: %v", e.Err)
}

type OidcConfigError struct{}

func (e *OidcConfigError) Error() string {
	return "Failed to get OIDC configuration"
}

type SkipOnboardingError struct {
	Err error
}

func (e *SkipOnboardingError) Error() string {
	return fmt.Sprintf("Failed to skip onboarding: %v", e.Err)
}

type ResetOnboardingError struct {
	Err error
}

func (e *ResetOnboardingError) Error() string {
	return fmt.Sprintf("Failed to reset onboarding: %v", e.Err)
}

type ProjectListError struct {
	Err error
}

func (e *ProjectListError) Error() string {
	return fmt.Sprintf("Failed to list projects: %v", e.Err)
}

type ProjectIDRequiredError struct{}

func (e *ProjectIDRequiredError) Error() string {
	return "Project ID is required"
}

type ProjectDeploymentError struct {
	Err error
}

func (e *ProjectDeploymentError) Error() string {
	return fmt.Sprintf("Failed to deploy project: %v", e.Err)
}

type ProjectDownError struct {
	Err error
}

func (e *ProjectDownError) Error() string {
	return fmt.Sprintf("Failed to bring down project: %v", e.Err)
}

type ProjectCreationError struct {
	Err error
}

func (e *ProjectCreationError) Error() string {
	return fmt.Sprintf("Failed to create project: %v", e.Err)
}

type ProjectDetailsError struct {
	Err error
}

func (e *ProjectDetailsError) Error() string {
	return fmt.Sprintf("Failed to get project details: %v", e.Err)
}

type ProjectRedeploymentError struct {
	Err error
}

func (e *ProjectRedeploymentError) Error() string {
	return fmt.Sprintf("Failed to redeploy project: %v", e.Err)
}

type ProjectDestroyError struct {
	Err error
}

func (e *ProjectDestroyError) Error() string {
	return fmt.Sprintf("Failed to destroy project: %v", e.Err)
}

type ProjectUpdateError struct {
	Err error
}

func (e *ProjectUpdateError) Error() string {
	return fmt.Sprintf("Failed to update project: %v", e.Err)
}

type ProjectRestartError struct {
	Err error
}

func (e *ProjectRestartError) Error() string {
	return fmt.Sprintf("Failed to restart project: %v", e.Err)
}

type ProjectStatusCountsError struct {
	Err error
}

func (e *ProjectStatusCountsError) Error() string {
	return fmt.Sprintf("Failed to get project status counts: %v", e.Err)
}

type SettingsMappingError struct {
	Err error
}

func (e *SettingsMappingError) Error() string {
	return "Failed to map settings"
}

type AuthSettingsUpdateError struct{}

func (e *AuthSettingsUpdateError) Error() string {
	return "Authentication settings can only be updated from the main environment"
}

type SettingsUpdateError struct {
	Err error
}

func (e *SettingsUpdateError) Error() string {
	return "Failed to update settings"
}

type DockerConnectionError struct {
	Err error
}

func (e *DockerConnectionError) Error() string {
	return fmt.Sprintf("Failed to connect to Docker: %v", e.Err)
}

type DockerPingError struct {
	Err error
}

func (e *DockerPingError) Error() string {
	return fmt.Sprintf("Docker is not responsive: %v", e.Err)
}

type DockerVersionError struct {
	Err error
}

func (e *DockerVersionError) Error() string {
	return fmt.Sprintf("Failed to get Docker version: %v", e.Err)
}

type DockerInfoError struct {
	Err error
}

func (e *DockerInfoError) Error() string {
	return fmt.Sprintf("Failed to get Docker info: %v", e.Err)
}

type DockerContainerListError struct {
	Err error
}

func (e *DockerContainerListError) Error() string {
	return fmt.Sprintf("Failed to list containers: %v", e.Err)
}

type DockerImageListError struct {
	Err error
}

func (e *DockerImageListError) Error() string {
	return fmt.Sprintf("Failed to list images: %v", e.Err)
}

type SystemPruneError struct {
	Err error
}

func (e *SystemPruneError) Error() string {
	return fmt.Sprintf("Failed to prune resources: %v", e.Err)
}

type ContainerStartAllError struct {
	Err error
}

func (e *ContainerStartAllError) Error() string {
	return fmt.Sprintf("Failed to start containers: %v", e.Err)
}

type ContainerStartStoppedError struct {
	Err error
}

func (e *ContainerStartStoppedError) Error() string {
	return fmt.Sprintf("Failed to start stopped containers: %v", e.Err)
}

type ContainerStopAllError struct {
	Err error
}

func (e *ContainerStopAllError) Error() string {
	return fmt.Sprintf("Failed to stop containers: %v", e.Err)
}

type DockerRunParseError struct {
	Err error
}

func (e *DockerRunParseError) Error() string {
	return "Failed to parse docker run command. Please check the syntax."
}

type DockerComposeConversionError struct {
	Err error
}

func (e *DockerComposeConversionError) Error() string {
	return "Failed to convert to Docker Compose format."
}

type UpgradeCheckError struct {
	Err error
}

func (e *UpgradeCheckError) Error() string {
	return fmt.Sprintf("Failed to check for updates: %v", e.Err)
}

type UpgradeTriggerError struct {
	Err error
}

func (e *UpgradeTriggerError) Error() string {
	return fmt.Sprintf("Failed to initiate upgrade: %v", e.Err)
}

type TemplateListError struct {
	Err error
}

func (e *TemplateListError) Error() string {
	return fmt.Sprintf("Failed to get templates: %v", e.Err)
}

type TemplateMappingError struct {
	Err error
}

func (e *TemplateMappingError) Error() string {
	return fmt.Sprintf("Failed to map templates: %v", e.Err)
}

type TemplateIDRequiredError struct{}

func (e *TemplateIDRequiredError) Error() string {
	return "Template ID is required"
}

type TemplateNotFoundError struct {
	Err error
}

func (e *TemplateNotFoundError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("Template not found: %v", e.Err)
	}
	return "Template not found"
}

type TemplateRetrievalError struct {
	Err error
}

func (e *TemplateRetrievalError) Error() string {
	return fmt.Sprintf("Failed to get template: %v", e.Err)
}

type TemplateContentError struct {
	Err error
}

func (e *TemplateContentError) Error() string {
	return fmt.Sprintf("Failed to get template content: %v", e.Err)
}

type TemplateCreationError struct {
	Err error
}

func (e *TemplateCreationError) Error() string {
	return fmt.Sprintf("Failed to create template: %v", e.Err)
}

type TemplateUpdateError struct {
	Err error
}

func (e *TemplateUpdateError) Error() string {
	return fmt.Sprintf("Failed to update template: %v", e.Err)
}

type TemplateDeletionError struct {
	Err error
}

func (e *TemplateDeletionError) Error() string {
	return fmt.Sprintf("Failed to delete template: %v", e.Err)
}

type DefaultTemplateSaveError struct {
	Err error
}

func (e *DefaultTemplateSaveError) Error() string {
	return fmt.Sprintf("Failed to save default template: %v", e.Err)
}

type RegistryIDRequiredError struct{}

func (e *RegistryIDRequiredError) Error() string {
	return "Registry ID is required"
}

type RegistryNotFoundError struct {
	Err error
}

func (e *RegistryNotFoundError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("Registry not found: %v", e.Err)
	}
	return "Registry not found"
}

type RegistryFetchError struct {
	Err error
}

func (e *RegistryFetchError) Error() string {
	return fmt.Sprintf("Failed to fetch registry: %v", e.Err)
}

type InvalidJSONResponseError struct {
	Err error
}

func (e *InvalidJSONResponseError) Error() string {
	return fmt.Sprintf("Invalid JSON response: %v", e.Err)
}

type TemplateAlreadyLocalError struct{}

func (e *TemplateAlreadyLocalError) Error() string {
	return "Template is already local"
}

type TemplateDownloadError struct {
	Err error
}

func (e *TemplateDownloadError) Error() string {
	return fmt.Sprintf("Failed to download template: %v", e.Err)
}

type GlobalVariablesRetrievalError struct {
	Err error
}

func (e *GlobalVariablesRetrievalError) Error() string {
	return fmt.Sprintf("Failed to retrieve global variables: %v", e.Err)
}

type GlobalVariablesUpdateError struct {
	Err error
}

func (e *GlobalVariablesUpdateError) Error() string {
	return fmt.Sprintf("Failed to update global variables: %v", e.Err)
}

type UpdaterRunError struct {
	Err error
}

func (e *UpdaterRunError) Error() string {
	return fmt.Sprintf("Failed to run updater: %v", e.Err)
}

type UpdaterHistoryError struct {
	Err error
}

func (e *UpdaterHistoryError) Error() string {
	return fmt.Sprintf("Failed to get updater history: %v", e.Err)
}

type UserListError struct {
	Err error
}

func (e *UserListError) Error() string {
	return fmt.Sprintf("Failed to list users: %v", e.Err)
}

type PasswordHashError struct {
	Err error
}

func (e *PasswordHashError) Error() string {
	return "Failed to hash password"
}

type UserCreationError struct {
	Err error
}

func (e *UserCreationError) Error() string {
	return "Failed to create user"
}

type UserNotFoundError struct{}

func (e *UserNotFoundError) Error() string {
	return "User not found"
}

type UserUpdateError struct {
	Err error
}

func (e *UserUpdateError) Error() string {
	return "Failed to update user"
}

type UserDeletionError struct {
	Err error
}

func (e *UserDeletionError) Error() string {
	return "Failed to delete user"
}

type VolumeListError struct {
	Err error
}

func (e *VolumeListError) Error() string {
	return fmt.Sprintf("Failed to list volumes: %v", e.Err)
}

type VolumeNotFoundError struct {
	Err error
}

func (e *VolumeNotFoundError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("Volume not found: %v", e.Err)
	}
	return "Volume not found"
}

type VolumeCreationError struct {
	Err error
}

func (e *VolumeCreationError) Error() string {
	return fmt.Sprintf("Failed to create volume: %v", e.Err)
}

type VolumeDeletionError struct {
	Err error
}

func (e *VolumeDeletionError) Error() string {
	return fmt.Sprintf("Failed to delete volume: %v", e.Err)
}

type VolumePruneError struct {
	Err error
}

func (e *VolumePruneError) Error() string {
	return fmt.Sprintf("Failed to prune volumes: %v", e.Err)
}

type VolumeUsageError struct {
	Err error
}

func (e *VolumeUsageError) Error() string {
	return fmt.Sprintf("Failed to get volume usage: %v", e.Err)
}

type VolumeCountsError struct {
	Err error
}

func (e *VolumeCountsError) Error() string {
	return fmt.Sprintf("Failed to get volume counts: %v", e.Err)
}
