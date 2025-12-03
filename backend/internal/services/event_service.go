package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"go.getarcane.app/types/event"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gorm"
)

type EventService struct {
	db *database.DB
}

func NewEventService(db *database.DB) *EventService {
	return &EventService{db: db}
}

type CreateEventRequest struct {
	Type          models.EventType     `json:"type"`
	Severity      models.EventSeverity `json:"severity,omitempty"`
	Title         string               `json:"title"`
	Description   string               `json:"description,omitempty"`
	ResourceType  *string              `json:"resourceType,omitempty"`
	ResourceID    *string              `json:"resourceId,omitempty"`
	ResourceName  *string              `json:"resourceName,omitempty"`
	UserID        *string              `json:"userId,omitempty"`
	Username      *string              `json:"username,omitempty"`
	EnvironmentID *string              `json:"environmentId,omitempty"`
	Metadata      models.JSON          `json:"metadata,omitempty"`
}

func (s *EventService) CreateEvent(ctx context.Context, req CreateEventRequest) (*models.Event, error) {
	severity := req.Severity
	if severity == "" {
		severity = models.EventSeverityInfo
	}

	event := &models.Event{
		Type:          req.Type,
		Severity:      severity,
		Title:         req.Title,
		Description:   req.Description,
		ResourceType:  req.ResourceType,
		ResourceID:    req.ResourceID,
		ResourceName:  req.ResourceName,
		UserID:        req.UserID,
		Username:      req.Username,
		EnvironmentID: req.EnvironmentID,
		Metadata:      req.Metadata,
		Timestamp:     time.Now(),
		BaseModel: models.BaseModel{
			CreatedAt: time.Now(),
		},
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(event).Error; err != nil {
			return fmt.Errorf("failed to create event: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return event, nil
}

func (s *EventService) CreateEventFromDto(ctx context.Context, req event.Create) (*event.Response, error) {
	severity := models.EventSeverity(req.Severity)
	if severity == "" {
		severity = models.EventSeverityInfo
	}

	metadata := models.JSON{}
	if req.Metadata != nil {
		metadata = models.JSON(req.Metadata)
	}

	createReq := CreateEventRequest{
		Type:          models.EventType(req.Type),
		Severity:      severity,
		Title:         req.Title,
		Description:   req.Description,
		ResourceType:  req.ResourceType,
		ResourceID:    req.ResourceID,
		ResourceName:  req.ResourceName,
		UserID:        req.UserID,
		Username:      req.Username,
		EnvironmentID: req.EnvironmentID,
		Metadata:      metadata,
	}

	event, err := s.CreateEvent(ctx, createReq)
	if err != nil {
		return nil, err
	}

	return s.toEventDto(event), nil
}

func (s *EventService) ListEventsPaginated(ctx context.Context, params pagination.QueryParams) ([]event.Response, pagination.Response, error) {
	var events []models.Event
	q := s.db.WithContext(ctx).Model(&models.Event{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"title LIKE ? OR description LIKE ? OR COALESCE(resource_name, '') LIKE ? OR COALESCE(username, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	if severity := params.Filters["severity"]; severity != "" {
		q = q.Where("severity = ?", severity)
	}
	if eventType := params.Filters["type"]; eventType != "" {
		q = q.Where("type = ?", eventType)
	}
	if resourceType := params.Filters["resourceType"]; resourceType != "" {
		q = q.Where("resource_type = ?", resourceType)
	}
	if username := params.Filters["username"]; username != "" {
		q = q.Where("username = ?", username)
	}
	if environmentId := params.Filters["environmentId"]; environmentId != "" {
		q = q.Where("environment_id = ?", environmentId)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &events)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate events: %w", err)
	}

	eventDtos, mapErr := mapper.MapSlice[models.Event, event.Response](events)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map events: %w", mapErr)
	}

	return eventDtos, paginationResp, nil
}

func (s *EventService) GetEventsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]event.Response, pagination.Response, error) {
	var events []models.Event
	q := s.db.WithContext(ctx).Model(&models.Event{}).Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"title LIKE ? OR description LIKE ? OR COALESCE(resource_name, '') LIKE ? OR COALESCE(username, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	if severity := params.Filters["severity"]; severity != "" {
		q = q.Where("severity = ?", severity)
	}
	if eventType := params.Filters["type"]; eventType != "" {
		q = q.Where("type = ?", eventType)
	}
	if resourceType := params.Filters["resourceType"]; resourceType != "" {
		q = q.Where("resource_type = ?", resourceType)
	}
	if username := params.Filters["username"]; username != "" {
		q = q.Where("username = ?", username)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &events)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate events: %w", err)
	}

	eventDtos, mapErr := mapper.MapSlice[models.Event, event.Response](events)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map events: %w", mapErr)
	}

	return eventDtos, paginationResp, nil
}

func (s *EventService) DeleteEvent(ctx context.Context, eventID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&models.Event{}, "id = ?", eventID)
		if result.Error != nil {
			return fmt.Errorf("failed to delete event: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("event not found")
		}
		return nil
	})
}

func (s *EventService) DeleteOldEvents(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("timestamp < ?", cutoff).Delete(&models.Event{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete old events: %w", result.Error)
		}
		return nil
	})
}

func (s *EventService) LogContainerEvent(ctx context.Context, eventType models.EventType, containerID, containerName, userID, username, environmentID string, metadata models.JSON) error {
	title := s.generateEventTitle(eventType, containerName)
	description := s.generateEventDescription(eventType, "container", containerName)
	severity := s.getEventSeverity(eventType)

	resourceType := "container"
	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &containerID,
		ResourceName:  &containerName,
		UserID:        &userID,
		Username:      &username,
		EnvironmentID: &environmentID,
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogImageEvent(ctx context.Context, eventType models.EventType, imageID, imageName, userID, username, environmentID string, metadata models.JSON) error {
	title := s.generateEventTitle(eventType, imageName)
	description := s.generateEventDescription(eventType, "image", imageName)
	severity := s.getEventSeverity(eventType)

	resourceType := "image"
	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &imageID,
		ResourceName:  &imageName,
		UserID:        &userID,
		Username:      &username,
		EnvironmentID: &environmentID,
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogProjectEvent(ctx context.Context, eventType models.EventType, projectID, projectName, userID, username, environmentID string, metadata models.JSON) error {
	title := s.generateEventTitle(eventType, projectName)
	description := s.generateEventDescription(eventType, "project", projectName)
	severity := s.getEventSeverity(eventType)

	resourceType := "project"
	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &projectID,
		ResourceName:  &projectName,
		UserID:        &userID,
		Username:      &username,
		EnvironmentID: &environmentID,
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogUserEvent(ctx context.Context, eventType models.EventType, userID, username string, metadata models.JSON) error {
	title := s.generateEventTitle(eventType, username)
	description := s.generateEventDescription(eventType, "user", username)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:        eventType,
		Severity:    severity,
		Title:       title,
		Description: description,
		UserID:      &userID,
		Username:    &username,
		Metadata:    metadata,
	})
	return err
}

func (s *EventService) LogVolumeEvent(ctx context.Context, eventType models.EventType, volumeID, volumeName, userID, username, environmentID string, metadata models.JSON) error {
	title := s.generateEventTitle(eventType, volumeName)
	description := s.generateEventDescription(eventType, "volume", volumeName)
	severity := s.getEventSeverity(eventType)

	resourceType := "volume"
	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &volumeID,
		ResourceName:  &volumeName,
		UserID:        &userID,
		Username:      &username,
		EnvironmentID: &environmentID,
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogNetworkEvent(ctx context.Context, eventType models.EventType, networkID, networkName, userID, username, environmentID string, metadata models.JSON) error {
	title := s.generateEventTitle(eventType, networkName)
	description := s.generateEventDescription(eventType, "network", networkName)
	severity := s.getEventSeverity(eventType)

	resourceType := "network"
	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &networkID,
		ResourceName:  &networkName,
		UserID:        &userID,
		Username:      &username,
		EnvironmentID: &environmentID,
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogErrorEvent(ctx context.Context, eventType models.EventType, resourceType, resourceID, resourceName, userID, username, environmentID string, err error, metadata models.JSON) {
	if err == nil {
		return
	}

	// Run error logging in background to prevent blocking the main flow
	// Detach context to ensure logging completes even if request is canceled
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		// Set a timeout for the background logging
		logCtx, cancel := context.WithTimeout(bgCtx, 30*time.Second)
		defer cancel()

		if metadata == nil {
			metadata = models.JSON{}
		}
		metadata["error"] = err.Error()

		titleCaser := cases.Title(language.English)
		title := fmt.Sprintf("%s error", titleCaser.String(resourceType))
		if resourceName != "" {
			title = fmt.Sprintf("%s error: %s", titleCaser.String(resourceType), resourceName)
		}

		description := fmt.Sprintf("Failed to perform operation on %s: %s", resourceType, err.Error())

		_, logErr := s.CreateEvent(logCtx, CreateEventRequest{
			Type:          eventType,
			Severity:      models.EventSeverityError,
			Title:         title,
			Description:   description,
			ResourceType:  &resourceType,
			ResourceID:    &resourceID,
			ResourceName:  &resourceName,
			UserID:        &userID,
			Username:      &username,
			EnvironmentID: &environmentID,
			Metadata:      metadata,
		})
		if logErr != nil {
			slog.ErrorContext(logCtx, "Failed to log error event", "error", logErr)
		}
	}()
}

var eventDefinitions = map[models.EventType]struct {
	TitleFormat       string
	DescriptionFormat string
	Severity          models.EventSeverity
}{
	models.EventTypeContainerStart:   {"Container started: %s", "Container '%s' has been started", models.EventSeveritySuccess},
	models.EventTypeContainerStop:    {"Container stopped: %s", "Container '%s' has been stopped", models.EventSeverityInfo},
	models.EventTypeContainerRestart: {"Container restarted: %s", "Container '%s' has been restarted", models.EventSeverityInfo},
	models.EventTypeContainerDelete:  {"Container deleted: %s", "Container '%s' has been deleted", models.EventSeverityWarning},
	models.EventTypeContainerCreate:  {"Container created: %s", "Container '%s' has been created", models.EventSeveritySuccess},
	models.EventTypeContainerScan:    {"Container scanned: %s", "Security scan completed for container '%s'", models.EventSeverityInfo},
	models.EventTypeContainerUpdate:  {"Container updated: %s", "Container '%s' has been updated", models.EventSeverityInfo},
	models.EventTypeContainerError:   {"Container error: %s", "An error occurred with container '%s'", models.EventSeverityError},

	models.EventTypeImagePull:   {"Image pulled: %s", "Image '%s' has been pulled", models.EventSeveritySuccess},
	models.EventTypeImageLoad:   {"Image loaded: %s", "Image '%s' has been loaded from archive", models.EventSeveritySuccess},
	models.EventTypeImageDelete: {"Image deleted: %s", "Image '%s' has been deleted", models.EventSeverityWarning},
	models.EventTypeImageScan:   {"Image scanned: %s", "Security scan completed for image '%s'", models.EventSeverityInfo},
	models.EventTypeImageError:  {"Image error: %s", "An error occurred with image '%s'", models.EventSeverityError},

	models.EventTypeProjectDeploy: {"Project deployed: %s", "Project '%s' has been deployed", models.EventSeveritySuccess},
	models.EventTypeProjectDelete: {"Project deleted: %s", "Project '%s' has been deleted", models.EventSeverityWarning},
	models.EventTypeProjectStart:  {"Project started: %s", "Project '%s' has been started", models.EventSeveritySuccess},
	models.EventTypeProjectStop:   {"Project stopped: %s", "Project '%s' has been stopped", models.EventSeverityInfo},
	models.EventTypeProjectCreate: {"Project created: %s", "Project '%s' has been created", models.EventSeveritySuccess},
	models.EventTypeProjectUpdate: {"Project updated: %s", "Project '%s' has been updated", models.EventSeverityInfo},
	models.EventTypeProjectError:  {"Project error: %s", "An error occurred with project '%s'", models.EventSeverityError},

	models.EventTypeVolumeCreate: {"Volume created: %s", "Volume '%s' has been created", models.EventSeveritySuccess},
	models.EventTypeVolumeDelete: {"Volume deleted: %s", "Volume '%s' has been deleted", models.EventSeverityWarning},
	models.EventTypeVolumeError:  {"Volume error: %s", "An error occurred with volume '%s'", models.EventSeverityError},

	models.EventTypeNetworkCreate: {"Network created: %s", "Network '%s' has been created", models.EventSeveritySuccess},
	models.EventTypeNetworkDelete: {"Network deleted: %s", "Network '%s' has been deleted", models.EventSeverityWarning},
	models.EventTypeNetworkError:  {"Network error: %s", "An error occurred with network '%s'", models.EventSeverityError},

	models.EventTypeSystemPrune:      {"System prune completed", "System resources have been pruned", models.EventSeverityInfo},
	models.EventTypeSystemAutoUpdate: {"System auto-update completed", "System auto-update process has completed", models.EventSeverityInfo},
	models.EventTypeSystemUpgrade:    {"System upgrade completed", "System upgrade process has completed", models.EventSeverityInfo},

	models.EventTypeUserLogin:  {"User logged in: %s", "User '%s' has logged in", models.EventSeverityInfo},
	models.EventTypeUserLogout: {"User logged out: %s", "User '%s' has logged out", models.EventSeverityInfo},
}

func (s *EventService) toEventDto(e *models.Event) *event.Response {
	var metadata map[string]interface{}
	if e.Metadata != nil {
		metadata = map[string]interface{}(e.Metadata)
	}

	return &event.Response{
		ID:            e.ID,
		Type:          string(e.Type),
		Severity:      string(e.Severity),
		Title:         e.Title,
		Description:   e.Description,
		ResourceType:  e.ResourceType,
		ResourceID:    e.ResourceID,
		ResourceName:  e.ResourceName,
		UserID:        e.UserID,
		Username:      e.Username,
		EnvironmentID: e.EnvironmentID,
		Metadata:      metadata,
		Timestamp:     e.Timestamp,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
}

func (s *EventService) generateEventTitle(eventType models.EventType, resourceName string) string {
	if def, ok := eventDefinitions[eventType]; ok {
		return fmt.Sprintf(def.TitleFormat, resourceName)
	}
	return fmt.Sprintf("Event: %s", string(eventType))
}

func (s *EventService) generateEventDescription(eventType models.EventType, resourceType, resourceName string) string {
	if def, ok := eventDefinitions[eventType]; ok {
		return fmt.Sprintf(def.DescriptionFormat, resourceName)
	}
	return fmt.Sprintf("%s operation performed on %s '%s'", string(eventType), resourceType, resourceName)
}

func (s *EventService) getEventSeverity(eventType models.EventType) models.EventSeverity {
	if def, ok := eventDefinitions[eventType]; ok {
		return def.Severity
	}
	return models.EventSeverityInfo
}
