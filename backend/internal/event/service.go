package event

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	eventtypes "github.com/getarcaneapp/arcane/types/v2/event"
	"github.com/samber/mo"
	"github.com/samber/mo/option"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gorm"
)

type EventService struct {
	db         *database.DB
	cfg        *config.Config
	httpClient *http.Client
}

func NewEventService(db *database.DB, cfg *config.Config, httpClient *http.Client) *EventService {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	return &EventService{
		db:         db,
		cfg:        cfg,
		httpClient: httpClient,
	}
}

type CreateEventRequest struct {
	Type          EventType     `json:"type"`
	Severity      EventSeverity `json:"severity,omitempty"`
	Title         string        `json:"title"`
	Description   string        `json:"description,omitempty"`
	ResourceType  *string       `json:"resourceType,omitempty"`
	ResourceID    *string       `json:"resourceId,omitempty"`
	ResourceName  *string       `json:"resourceName,omitempty"`
	UserID        *string       `json:"userId,omitempty"`
	Username      *string       `json:"username,omitempty"`
	EnvironmentID *string       `json:"environmentId,omitempty"`
	Metadata      database.JSON `json:"metadata,omitempty"`
}

func (s *EventService) CreateEvent(ctx context.Context, req CreateEventRequest) (*Event, error) {
	severity := req.Severity
	if severity == "" {
		severity = EventSeverityInfo
	}
	userID, username := normalizeEventActor(req.UserID, req.Username)

	eventRecord := &Event{
		Type:          req.Type,
		Severity:      severity,
		Title:         req.Title,
		Description:   req.Description,
		ResourceType:  req.ResourceType,
		ResourceID:    req.ResourceID,
		ResourceName:  req.ResourceName,
		UserID:        userID,
		Username:      username,
		EnvironmentID: req.EnvironmentID,
		Metadata:      req.Metadata,
		Timestamp:     time.Now(),
		CreatedAt:     time.Now(),
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(eventRecord).Error; err != nil {
			return errors.WrapIf(err, "failed to create event")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.forwardEventToManager(ctx, eventRecord)

	return eventRecord, nil
}

func (s *EventService) forwardEventToManager(ctx context.Context, eventModel *Event) {
	if eventModel == nil || s.cfg == nil || !s.cfg.AgentMode {
		return
	}

	evt := &edge.TunnelEvent{
		Type:        string(eventModel.Type),
		Severity:    string(eventModel.Severity),
		Title:       eventModel.Title,
		Description: eventModel.Description,
	}
	if eventModel.ResourceType != nil {
		evt.ResourceType = *eventModel.ResourceType
	}
	if eventModel.ResourceID != nil {
		evt.ResourceID = *eventModel.ResourceID
	}
	if eventModel.ResourceName != nil {
		evt.ResourceName = *eventModel.ResourceName
	}
	if eventModel.UserID != nil {
		evt.UserID = *eventModel.UserID
	}
	if eventModel.Username != nil {
		evt.Username = *eventModel.Username
	}
	if eventModel.Metadata != nil {
		metadataBytes, err := json.Marshal(map[string]any(eventModel.Metadata))
		if err != nil {
			slog.WarnContext(ctx, "Failed to marshal event metadata for edge sync", "type", eventModel.Type, "error", err)
		} else {
			evt.MetadataJSON = metadataBytes
		}
	}

	go func(parentCtx context.Context, outgoing *edge.TunnelEvent) {
		syncCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), 10*time.Second)
		defer cancel()

		if err := edge.PublishEventToManager(outgoing); err != nil {
			if !errors.Is(err, edge.ErrNoActiveAgentTunnel) {
				slog.WarnContext(syncCtx, "Failed to sync event to manager over edge tunnel", "type", outgoing.Type, "error", err)
				return
			}
			if !s.canForwardEventToManagerHTTP() {
				return
			}
			if httpErr := s.forwardEventToManagerHTTP(syncCtx, eventModel); httpErr != nil {
				slog.WarnContext(syncCtx, "Failed to sync event to manager over API", "type", outgoing.Type, "error", httpErr)
				return
			}
		}
	}(ctx, evt)
}

func (s *EventService) canForwardEventToManagerHTTP() bool {
	if s.cfg == nil {
		return false
	}
	if strings.TrimSpace(s.cfg.AgentToken) == "" {
		return false
	}
	return strings.TrimSpace(s.cfg.GetManagerBaseURL()) != ""
}

func (s *EventService) forwardEventToManagerHTTP(ctx context.Context, eventModel *Event) error {
	if eventModel == nil {
		return errors.New("event is required")
	}
	if s.cfg == nil || strings.TrimSpace(s.cfg.AgentToken) == "" {
		return errors.New("agent token is required for manager event sync")
	}

	managerEventsURL, err := managerEventEndpointURL(s.cfg.GetManagerBaseURL())
	if err != nil {
		return errors.WrapIf(err, "manager API URL is invalid for manager event sync")
	}

	payload := CreateEventRequest{
		Type:          eventModel.Type,
		Severity:      eventModel.Severity,
		Title:         eventModel.Title,
		Description:   eventModel.Description,
		ResourceType:  eventModel.ResourceType,
		ResourceID:    eventModel.ResourceID,
		ResourceName:  eventModel.ResourceName,
		UserID:        eventModel.UserID,
		Username:      eventModel.Username,
		EnvironmentID: eventModel.EnvironmentID,
	}

	if len(eventModel.Metadata) > 0 {
		payload.Metadata = eventModel.Metadata
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return errors.WrapIf(err, "failed to marshal event payload")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, managerEventsURL, bytes.NewReader(body))
	if err != nil {
		return errors.WrapIf(err, "failed to create manager event request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(utils.HeaderAgentToken, s.cfg.AgentToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return errors.WrapIf(err, "failed to send event to manager")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if readErr != nil {
		return errors.Errorf("manager event sync failed with status %d", resp.StatusCode)
	}
	return errors.Errorf("manager event sync failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
}

func managerEventEndpointURL(rawBaseURL string) (string, error) {
	trimmed := strings.TrimSpace(rawBaseURL)
	if trimmed == "" {
		return "", errors.New("manager API URL is required")
	}

	baseURL, err := url.Parse(trimmed)
	if err != nil {
		return "", errors.WrapIf(err, "failed to parse manager API URL")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return "", errors.Errorf("unsupported scheme %q", baseURL.Scheme)
	}
	if baseURL.Host == "" {
		return "", errors.New("manager API URL host is required")
	}

	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/api/events"
	return baseURL.String(), nil
}

func normalizeEventActor(userID, username *string) (*string, *string) {
	normalizedUserID := normalizeOptionalStringPtr(userID)
	normalizedUsername := normalizeOptionalStringPtr(username)

	if normalizedUsername == nil && normalizedUserID != nil {
		normalizedUsername = copyOptionalStringPtr(normalizedUserID)
	}
	if normalizedUserID == nil && normalizedUsername != nil {
		normalizedUserID = copyOptionalStringPtr(normalizedUsername)
	}
	if normalizedUserID == nil && normalizedUsername == nil {
		normalizedUserID = new("system")
		normalizedUsername = new("System")
	}

	return normalizedUserID, normalizedUsername
}

func normalizeOptionalStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func copyOptionalStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return new(*value)
}

func (s *EventService) ListEventsPaginated(ctx context.Context, params pagination.QueryParams) ([]eventtypes.Event, pagination.Response, error) {
	var events []Event
	q := s.db.WithContext(ctx).Model(&Event{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"title LIKE ? OR description LIKE ? OR COALESCE(resource_name, '') LIKE ? OR COALESCE(username, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "severity", params.Filters["severity"])
	q = applyEventTypeFilter(q, params.Filters["type"])
	q = pagination.ApplyFilter(q, "resource_type", params.Filters["resourceType"])
	q = pagination.ApplyFilter(q, "username", params.Filters["username"])
	q = pagination.ApplyFilter(q, "environment_id", params.Filters["environmentId"])

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &events)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate events")
	}

	eventDtos, mapErr := mapper.MapSlice[Event, eventtypes.Event](events)
	if mapErr != nil {
		return nil, pagination.Response{}, errors.WrapIf(mapErr, "failed to map events")
	}

	return eventDtos, paginationResp, nil
}

func (s *EventService) GetEventsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]eventtypes.Event, pagination.Response, error) {
	var events []Event
	q := s.db.WithContext(ctx).Model(&Event{}).Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"title LIKE ? OR description LIKE ? OR COALESCE(resource_name, '') LIKE ? OR COALESCE(username, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "severity", params.Filters["severity"])
	q = applyEventTypeFilter(q, params.Filters["type"])
	q = pagination.ApplyFilter(q, "resource_type", params.Filters["resourceType"])
	q = pagination.ApplyFilter(q, "username", params.Filters["username"])

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &events)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate events")
	}

	eventDtos, mapErr := mapper.MapSlice[Event, eventtypes.Event](events)
	if mapErr != nil {
		return nil, pagination.Response{}, errors.WrapIf(mapErr, "failed to map events")
	}

	return eventDtos, paginationResp, nil
}

// applyEventTypeFilter filters by event type. Values containing a '.' are
// exact types (e.g. "container.start"); values without one are category
// prefixes (e.g. "container" matches "container.%"). Comma-separated values
// are OR-ed together, mirroring pagination.ApplyFilter's multi-value handling.
func applyEventTypeFilter(q *gorm.DB, value string) *gorm.DB {
	if value == "" {
		return q
	}
	var (
		exact []string
		conds []string
		args  []any
	)
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, ".") {
			exact = append(exact, part)
		} else {
			conds = append(conds, "type LIKE ?")
			args = append(args, part+".%")
		}
	}
	if len(exact) > 0 {
		conds = append(conds, "type IN ?")
		args = append(args, exact)
	}
	if len(conds) == 0 {
		return q
	}
	return q.Where(strings.Join(conds, " OR "), args...)
}

// EventSeverityCounts holds global event counts per severity.
type EventSeverityCounts struct {
	Total   int64 `json:"total"`
	Info    int64 `json:"info"`
	Success int64 `json:"success"`
	Warning int64 `json:"warning"`
	Error   int64 `json:"error"`
}

func (s *EventService) GetEventSeverityCounts(ctx context.Context) (EventSeverityCounts, error) {
	var rows []struct {
		Severity string
		Count    int64
	}
	if err := s.db.WithContext(ctx).Model(&Event{}).
		Select("severity, COUNT(*) AS count").
		Group("severity").
		Scan(&rows).Error; err != nil {
		return EventSeverityCounts{}, errors.WrapIf(err, "failed to count events by severity")
	}

	var counts EventSeverityCounts
	for _, r := range rows {
		switch EventSeverity(r.Severity) {
		case EventSeveritySuccess:
			counts.Success = r.Count
		case EventSeverityWarning:
			counts.Warning = r.Count
		case EventSeverityError:
			counts.Error = r.Count
		case EventSeverityInfo:
			counts.Info += r.Count
		default:
			// Unclassified severities fold into Info.
			counts.Info += r.Count
		}
		counts.Total += r.Count
	}
	return counts, nil
}

func (s *EventService) DeleteEvent(ctx context.Context, eventID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&Event{}, "id = ?", eventID)
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to delete event")
		}
		if result.RowsAffected == 0 {
			return errors.New("event not found")
		}
		return nil
	})
}

func (s *EventService) DeleteOldEvents(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("timestamp < ?", cutoff).Delete(&Event{})
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to delete old events")
		}
		return nil
	})
}

func (s *EventService) LogContainerEvent(ctx context.Context, eventType EventType, containerID, containerName, userID, username, environmentID string, metadata database.JSON) error {
	title := s.generateEventTitle(eventType, containerName)
	description := s.generateEventDescription(eventType, "container", containerName)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("container"),
		ResourceID:    new(containerID),
		ResourceName:  new(containerName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogImageEvent(ctx context.Context, eventType EventType, imageID, imageName, userID, username, environmentID string, metadata database.JSON) error {
	title := s.generateEventTitle(eventType, imageName)
	description := s.generateEventDescription(eventType, "image", imageName)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("image"),
		ResourceID:    new(imageID),
		ResourceName:  new(imageName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogProjectEvent(ctx context.Context, eventType EventType, projectID, projectName, userID, username, environmentID string, metadata database.JSON) error {
	title := s.generateEventTitle(eventType, projectName)
	description := s.generateEventDescription(eventType, "project", projectName)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("project"),
		ResourceID:    new(projectID),
		ResourceName:  new(projectName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogUserEvent(ctx context.Context, eventType EventType, userID, username string, metadata database.JSON) error {
	title := s.generateEventTitle(eventType, username)
	description := s.generateEventDescription(eventType, "user", username)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:        eventType,
		Severity:    severity,
		Title:       title,
		Description: description,
		UserID:      new(userID),
		Username:    new(username),
		Metadata:    metadata,
	})
	return err
}

func (s *EventService) LogVolumeEvent(ctx context.Context, eventType EventType, volumeID, volumeName, userID, username, environmentID string, metadata database.JSON) error {
	title := s.generateEventTitle(eventType, volumeName)
	description := s.generateEventDescription(eventType, "volume", volumeName)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("volume"),
		ResourceID:    new(volumeID),
		ResourceName:  new(volumeName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogNetworkEvent(ctx context.Context, eventType EventType, networkID, networkName, userID, username, environmentID string, metadata database.JSON) error {
	title := s.generateEventTitle(eventType, networkName)
	description := s.generateEventDescription(eventType, "network", networkName)
	severity := s.getEventSeverity(eventType)

	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("network"),
		ResourceID:    new(networkID),
		ResourceName:  new(networkName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogErrorEvent(ctx context.Context, eventType EventType, resourceType, resourceID, resourceName, userID, username, environmentID string, err error, metadata database.JSON) {
	if err == nil {
		return
	}

	// Detach cancellation but keep a bounded timeout to avoid unbounded goroutine fanout.
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	eventMetadata := cloneEventMetadataInternal(metadata)
	eventMetadata["error"] = err.Error()

	titleCaser := cases.Title(language.English)
	title := titleCaser.String(resourceType) + " error"
	if resourceName != "" {
		title = fmt.Sprintf("%s error: %s", titleCaser.String(resourceType), resourceName)
	}

	description := fmt.Sprintf("Failed to perform operation on %s: %s", resourceType, err.Error())

	_, logErr := s.CreateEvent(logCtx, CreateEventRequest{
		Type:          eventType,
		Severity:      EventSeverityError,
		Title:         title,
		Description:   description,
		ResourceType:  new(resourceType),
		ResourceID:    new(resourceID),
		ResourceName:  new(resourceName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      eventMetadata,
	})
	if logErr != nil {
		slog.ErrorContext(logCtx, "Failed to log error event", "error", logErr)
	}
}

func cloneEventMetadataInternal(metadata database.JSON) database.JSON {
	if metadata == nil {
		return database.JSON{}
	}

	cloned := make(database.JSON, len(metadata))
	for k, v := range metadata {
		cloned[k] = cloneEventMetadataValueInternal(v)
	}
	return cloned
}

func cloneEventMetadataValueInternal(value any) any {
	switch typed := value.(type) {
	case database.JSON:
		return cloneEventMetadataInternal(typed)
	case map[string]any:
		return cloneEventMetadataInternal(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneEventMetadataValueInternal(typed[i])
		}
		return out
	default:
		return value
	}
}

type eventDefinition struct {
	TitleFormat       string
	DescriptionFormat string
	Severity          EventSeverity
}

var eventDefinitions = map[EventType]eventDefinition{
	EventTypeContainerStart:   {"Container started: %s", "Container '%s' has been started", EventSeveritySuccess},
	EventTypeContainerStop:    {"Container stopped: %s", "Container '%s' has been stopped", EventSeverityInfo},
	EventTypeContainerRestart: {"Container restarted: %s", "Container '%s' has been restarted", EventSeverityInfo},
	EventTypeContainerDelete:  {"Container deleted: %s", "Container '%s' has been deleted", EventSeverityWarning},
	EventTypeContainerCreate:  {"Container created: %s", "Container '%s' has been created", EventSeveritySuccess},
	EventTypeContainerScan:    {"Container scanned: %s", "Security scan completed for container '%s'", EventSeverityInfo},
	EventTypeContainerUpdate:  {"Container updated: %s", "Container '%s' has been updated", EventSeverityInfo},
	EventTypeContainerError:   {"Container error: %s", "An error occurred with container '%s'", EventSeverityError},

	EventTypeImagePull:   {"Image pulled: %s", "Image '%s' has been pulled", EventSeveritySuccess},
	EventTypeImageLoad:   {"Image loaded: %s", "Image '%s' has been loaded from archive", EventSeveritySuccess},
	EventTypeImageDelete: {"Image deleted: %s", "Image '%s' has been deleted", EventSeverityWarning},
	EventTypeImageScan:   {"Image scanned: %s", "Security scan completed for image '%s'", EventSeverityInfo},
	EventTypeImageError:  {"Image error: %s", "An error occurred with image '%s'", EventSeverityError},

	EventTypeProjectDeploy: {"Project deployed: %s", "Project '%s' has been deployed", EventSeveritySuccess},
	EventTypeProjectDelete: {"Project deleted: %s", "Project '%s' has been deleted", EventSeverityWarning},
	EventTypeProjectStart:  {"Project started: %s", "Project '%s' has been started", EventSeveritySuccess},
	EventTypeProjectStop:   {"Project stopped: %s", "Project '%s' has been stopped", EventSeverityInfo},
	EventTypeProjectCreate: {"Project created: %s", "Project '%s' has been created", EventSeveritySuccess},
	EventTypeProjectUpdate: {"Project updated: %s", "Project '%s' has been updated", EventSeverityInfo},
	EventTypeProjectError:  {"Project error: %s", "An error occurred with project '%s'", EventSeverityError},

	EventTypeVolumeCreate:             {"Volume created: %s", "Volume '%s' has been created", EventSeveritySuccess},
	EventTypeVolumeDelete:             {"Volume deleted: %s", "Volume '%s' has been deleted", EventSeverityWarning},
	EventTypeVolumeError:              {"Volume error: %s", "An error occurred with volume '%s'", EventSeverityError},
	EventTypeVolumeFileCreate:         {"Volume file created: %s", "A file or directory was created in volume '%s'", EventSeveritySuccess},
	EventTypeVolumeFileDelete:         {"Volume file deleted: %s", "A file or directory was deleted in volume '%s'", EventSeverityWarning},
	EventTypeVolumeFileUpload:         {"Volume file uploaded: %s", "A file was uploaded to volume '%s'", EventSeveritySuccess},
	EventTypeVolumeFileUpdate:         {"Volume workspace updated: %s", "Files in volume '%s' were updated", EventSeverityInfo},
	EventTypeVolumeWorkspaceUpdate:    {"Volume workspace updated: %s", "Workspace files in volume '%s' were updated", EventSeverityInfo},
	EventTypeVolumeBackupCreate:       {"Volume backup created: %s", "A backup was created for volume '%s'", EventSeveritySuccess},
	EventTypeVolumeBackupDelete:       {"Volume backup deleted: %s", "A backup was deleted for volume '%s'", EventSeverityWarning},
	EventTypeVolumeBackupRestore:      {"Volume backup restored: %s", "A backup was restored for volume '%s'", EventSeverityWarning},
	EventTypeVolumeBackupRestoreFiles: {"Volume backup files restored: %s", "Selected files were restored for volume '%s'", EventSeverityWarning},
	EventTypeVolumeBackupDownload:     {"Volume backup downloaded: %s", "A backup was downloaded for volume '%s'", EventSeverityInfo},

	EventTypeNetworkCreate: {"Network created: %s", "Network '%s' has been created", EventSeveritySuccess},
	EventTypeNetworkDelete: {"Network deleted: %s", "Network '%s' has been deleted", EventSeverityWarning},
	EventTypeNetworkError:  {"Network error: %s", "An error occurred with network '%s'", EventSeverityError},

	EventTypeSystemPrune:      {"System prune completed", "System resources have been pruned", EventSeverityInfo},
	EventTypeSystemAutoUpdate: {"System auto-update completed", "System auto-update process has completed", EventSeverityInfo},
	EventTypeSystemUpgrade:    {"System upgrade completed", "System upgrade process has completed", EventSeverityInfo},

	EventTypeUserLogin:         {"User logged in: %s", "User '%s' has logged in", EventSeverityInfo},
	EventTypeUserLogout:        {"User logged out: %s", "User '%s' has logged out", EventSeverityInfo},
	EventTypeFederatedExchange: {"Federated credential exchange: %s", "Federated credential exchange for '%s'", EventSeverityInfo},
}

func (s *EventService) generateEventTitle(eventType EventType, resourceName string) string {
	definition, ok := eventDefinitions[eventType]
	return option.Map(func(def eventDefinition) string {
		return fmt.Sprintf(def.TitleFormat, resourceName)
	})(mo.TupleToOption(definition, ok)).OrElse("Event: " + string(eventType))
}

func (s *EventService) generateEventDescription(eventType EventType, resourceType, resourceName string) string {
	definition, ok := eventDefinitions[eventType]
	return option.Map(func(def eventDefinition) string {
		return fmt.Sprintf(def.DescriptionFormat, resourceName)
	})(mo.TupleToOption(definition, ok)).OrElse(
		fmt.Sprintf("%s operation performed on %s '%s'", string(eventType), resourceType, resourceName),
	)
}

func (s *EventService) getEventSeverity(eventType EventType) EventSeverity {
	definition, ok := eventDefinitions[eventType]
	return option.Map(func(def eventDefinition) EventSeverity {
		return def.Severity
	})(mo.TupleToOption(definition, ok)).OrElse(EventSeverityInfo)
}
