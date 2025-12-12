package models

import (
	"time"
)

type EventType string
type EventSeverity string

const (
	// Event types
	EventTypeContainerStart   EventType = "container.start"
	EventTypeContainerStop    EventType = "container.stop"
	EventTypeContainerRestart EventType = "container.restart"
	EventTypeContainerDelete  EventType = "container.delete"
	EventTypeContainerCreate  EventType = "container.create"
	EventTypeContainerScan    EventType = "container.scan"
	EventTypeContainerUpdate  EventType = "container.update"
	EventTypeContainerError   EventType = "container.error"

	EventTypeImagePull   EventType = "image.pull"
	EventTypeImageLoad   EventType = "image.load"
	EventTypeImageDelete EventType = "image.delete"
	EventTypeImageScan   EventType = "image.scan"
	EventTypeImageError  EventType = "image.error"

	EventTypeProjectDeploy EventType = "project.deploy"
	EventTypeProjectDelete EventType = "project.delete"
	EventTypeProjectStart  EventType = "project.start"
	EventTypeProjectStop   EventType = "project.stop"
	EventTypeProjectCreate EventType = "project.create"
	EventTypeProjectUpdate EventType = "project.update"
	EventTypeProjectError  EventType = "project.error"

	EventTypeGitRepositoryCreate EventType = "git.repository.create"
	EventTypeGitRepositoryUpdate EventType = "git.repository.update"
	EventTypeGitRepositoryDelete EventType = "git.repository.delete"
	EventTypeGitRepositoryTest   EventType = "git.repository.test"
	EventTypeGitRepositoryError  EventType = "git.repository.error"

	EventTypeGitSyncCreate EventType = "git.sync.create"
	EventTypeGitSyncUpdate EventType = "git.sync.update"
	EventTypeGitSyncDelete EventType = "git.sync.delete"
	EventTypeGitSyncRun    EventType = "git.sync.run"
	EventTypeGitSyncError  EventType = "git.sync.error"

	EventTypeVolumeCreate EventType = "volume.create"
	EventTypeVolumeDelete EventType = "volume.delete"
	EventTypeVolumeError  EventType = "volume.error"

	EventTypeNetworkCreate EventType = "network.create"
	EventTypeNetworkDelete EventType = "network.delete"
	EventTypeNetworkError  EventType = "network.error"

	EventTypeSystemPrune      EventType = "system.prune"
	EventTypeUserLogin        EventType = "user.login"
	EventTypeUserLogout       EventType = "user.logout"
	EventTypeSystemAutoUpdate EventType = "system.auto_update"
	EventTypeSystemUpgrade    EventType = "system.upgrade"

	EventTypeEnvironmentCreate            EventType = "environment.create"
	EventTypeEnvironmentUpdate            EventType = "environment.update"
	EventTypeEnvironmentDelete            EventType = "environment.delete"
	EventTypeEnvironmentApiKeyRegenerated EventType = "environment.api_key.regenerated"

	// Event severities
	EventSeverityInfo    EventSeverity = "info"
	EventSeverityWarning EventSeverity = "warning"
	EventSeverityError   EventSeverity = "error"
	EventSeveritySuccess EventSeverity = "success"
)

type Event struct {
	Type          EventType     `json:"type" sortable:"true"`
	Severity      EventSeverity `json:"severity" sortable:"true"`
	Title         string        `json:"title" sortable:"true"`
	Description   string        `json:"description"`
	ResourceType  *string       `json:"resourceType,omitempty" sortable:"true"`
	ResourceID    *string       `json:"resourceId,omitempty" sortable:"true"`
	ResourceName  *string       `json:"resourceName,omitempty" sortable:"true"`
	UserID        *string       `json:"userId,omitempty" sortable:"true"`
	Username      *string       `json:"username,omitempty" sortable:"true"`
	EnvironmentID *string       `json:"environmentId,omitempty"`
	Metadata      JSON          `json:"metadata,omitempty" gorm:"type:text"`
	Timestamp     time.Time     `json:"timestamp" sortable:"true"`
	BaseModel
}

func (Event) TableName() string {
	return "events"
}
