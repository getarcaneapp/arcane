package event

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/event"
	"github.com/moby/moby/api/types/events"
	"github.com/samber/mo"
	"github.com/samber/mo/option"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/streams/agg"
	"go.getarcane.app/streams/bus"
	"golang.org/x/sync/semaphore"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event/children/correlation"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

// maxConcurrentEdgeSyncs bounds in-flight event forwards to the manager.
const maxConcurrentEdgeSyncs = 16

type EventService struct {
	changes     *concurrency.Signal[struct{}]
	correlation *correlation.Service
	db          *database.DB
	cfg         *config.Config
	httpClient  *http.Client
	edgeSyncs   *semaphore.Weighted
}

func NewEventService(db *database.DB, cfg *config.Config, httpClient *http.Client) *EventService {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	return &EventService{
		changes:     concurrency.NewSignal[struct{}](),
		correlation: correlation.NewService(time.Now),
		db:          db,
		cfg:         cfg,
		httpClient:  httpClient,
		edgeSyncs:   semaphore.NewWeighted(maxConcurrentEdgeSyncs),
	}
}

type CreateEventRequest struct {
	// Internal identity for replay-safe daemon delivery; never accepted from API payloads.
	deduplicationKey string
	Type             EventType     `json:"type"`
	Severity         EventSeverity `json:"severity,omitempty"`
	Title            string        `json:"title"`
	Description      string        `json:"description,omitempty"`
	ResourceType     *string       `json:"resourceType,omitempty"`
	ResourceID       *string       `json:"resourceId,omitempty"`
	ResourceName     *string       `json:"resourceName,omitempty"`
	UserID           *string       `json:"userId,omitempty"`
	Username         *string       `json:"username,omitempty"`
	EnvironmentID    *string       `json:"environmentId,omitempty"`
	Metadata         database.JSON `json:"metadata,omitempty"`
}

func (s *EventService) CreateEvent(ctx context.Context, req CreateEventRequest) (*Event, error) {
	severity := cmp.Or(req.Severity, EventSeverityInfo)
	userID, username := normalizeEventActor(req.UserID, req.Username)

	eventRecord := &Event{
		CreatedAt:     time.Now(),
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
	}

	if req.deduplicationKey != "" {
		eventRecord.DeduplicationKey = &req.deduplicationKey
	}

	var inserted bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if req.deduplicationKey != "" {
			tx = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "deduplication_key"}}, DoNothing: true})
		}
		result := tx.Create(eventRecord)
		if result.Error != nil {
			return fmt.Errorf("failed to create event: %w", result.Error)
		}
		inserted = result.RowsAffected > 0
		return nil
	})
	if err != nil {
		return nil, err
	}

	if !inserted {
		return eventRecord, nil
	}
	s.correlateCreatedEvent(req)
	s.changes.Publish(struct{}{})
	s.forwardEventToManager(ctx, eventRecord)

	return eventRecord, nil
}

// IngestAgentEvent assigns the authenticated environment instead of trusting the agent's local ID.
func (s *EventService) IngestAgentEvent(ctx context.Context, environmentID string, req CreateEventRequest) (*Event, error) {
	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" || environmentID == "0" {
		return nil, errors.New("remote environment is required for agent event ingestion")
	}
	req.EnvironmentID = &environmentID
	return s.CreateEvent(ctx, req)
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

		if err := s.edgeSyncs.Acquire(syncCtx, 1); err != nil {
			slog.WarnContext(syncCtx, "Dropped event sync to manager; too many in flight", "type", outgoing.Type, "error", err)
			return
		}
		defer s.edgeSyncs.Release(1)

		if err := edge.PublishEventToManager(syncCtx, outgoing); err != nil {
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
	return strings.TrimSpace(httpx.ManagerBaseURL(s.cfg.ManagerApiUrl)) != ""
}

func (s *EventService) forwardEventToManagerHTTP(ctx context.Context, eventModel *Event) error {
	if eventModel == nil {
		return errors.New("event is required")
	}
	if s.cfg == nil || strings.TrimSpace(s.cfg.AgentToken) == "" {
		return errors.New("agent token is required for manager event sync")
	}

	managerEventsURL, err := managerEventEndpointURL(httpx.ManagerBaseURL(s.cfg.ManagerApiUrl))
	if err != nil {
		return fmt.Errorf("manager API URL is invalid for manager event sync: %w", err)
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
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, managerEventsURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create manager event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.HeaderAgentToken, s.cfg.AgentToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send event to manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if readErr != nil {
		return fmt.Errorf("manager event sync failed with status %d", resp.StatusCode)
	}
	return fmt.Errorf("manager event sync failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
}

func managerEventEndpointURL(rawBaseURL string) (string, error) {
	trimmed := strings.TrimSpace(rawBaseURL)
	if trimmed == "" {
		return "", errors.New("manager API URL is required")
	}

	baseURL, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("failed to parse manager API URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", baseURL.Scheme)
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
		normalizedUsername = new(*normalizedUserID)
	}
	if normalizedUserID == nil && normalizedUsername != nil {
		normalizedUserID = new(*normalizedUsername)
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

func (s *EventService) ListEventsPaginated(ctx context.Context, params pagination.QueryParams) ([]event.Event, pagination.Response, error) {
	var eventRecords []Event
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

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &eventRecords)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate events: %w", err)
	}

	eventDtos, mapErr := mapping.MapSlice[Event, event.Event](eventRecords)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map events: %w", mapErr)
	}

	return eventDtos, paginationResp, nil
}

func (s *EventService) GetEventsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]event.Event, pagination.Response, error) {
	var eventRecords []Event
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

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &eventRecords)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate events: %w", err)
	}

	eventDtos, mapErr := mapping.MapSlice[Event, event.Event](eventRecords)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map events: %w", mapErr)
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

func (s *EventService) GetEventSeverityCounts(ctx context.Context) (event.SeverityCounts, error) {
	var rows []struct {
		Severity string
		Count    int64
	}
	if err := s.db.WithContext(ctx).Model(&Event{}).
		Select("severity, COUNT(*) AS count").
		Group("severity").
		Scan(&rows).Error; err != nil {
		return event.SeverityCounts{}, fmt.Errorf("failed to count events by severity: %w", err)
	}

	var counts event.SeverityCounts
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
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&Event{}, "id = ?", eventID)
		if result.Error != nil {
			return fmt.Errorf("failed to delete event: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("event not found")
		}
		return nil
	})
	if err == nil {
		s.changes.Publish(struct{}{})
	}
	return err
}

func (s *EventService) DeleteOldEvents(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("timestamp < ?", cutoff).Delete(&Event{})
		if result.Error != nil {
			return fmt.Errorf("failed to delete old events: %w", result.Error)
		}
		deleted = result.RowsAffected
		return nil
	})
	if err == nil && deleted > 0 {
		s.changes.Publish(struct{}{})
	}
	return err
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

	eventMetadata := cloneEventMetadata(metadata)
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

func cloneEventMetadata(metadata database.JSON) database.JSON {
	if metadata == nil {
		return database.JSON{}
	}

	cloned := make(database.JSON, len(metadata))
	for k, v := range metadata {
		cloned[k] = cloneEventMetadataValue(v)
	}
	return cloned
}

func cloneEventMetadataValue(value any) any {
	switch typed := value.(type) {
	case database.JSON:
		return cloneEventMetadata(typed)
	case map[string]any:
		return cloneEventMetadata(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneEventMetadataValue(typed[i])
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
	EventTypeVolumeRename:             {"Volume renamed: %s", "Volume '%s' has been renamed", EventSeveritySuccess},
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

	EventTypeNetworkCreate:     {"Network created: %s", "Network '%s' has been created", EventSeveritySuccess},
	EventTypeNetworkDelete:     {"Network deleted: %s", "Network '%s' has been deleted", EventSeverityWarning},
	EventTypeNetworkConnect:    {"Network connected: %s", "A container has been connected to network '%s'", EventSeveritySuccess},
	EventTypeNetworkDisconnect: {"Network disconnected: %s", "A container has been disconnected from network '%s'", EventSeverityInfo},
	EventTypeNetworkError:      {"Network error: %s", "An error occurred with network '%s'", EventSeverityError},

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

// RunStreamProducer signals committed event changes so clients can refresh their current query.
func (s *EventService) RunStreamProducer(ctx context.Context, streamEvents chan<- event.StreamEvent) {
	changed := make(chan struct{}, 1)
	unsubscribe := s.changes.Subscribe(func(struct{}) {
		// One pending invalidation covers all changes, without blocking event persistence.
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer unsubscribe()

	// Subscribe first so connecting and reconnecting clients cannot miss a change.
	if !agg.Send(ctx, streamEvents, event.StreamEvent{Type: "changed", Timestamp: time.Now()}) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			if !agg.Send(ctx, streamEvents, event.StreamEvent{Type: "changed", Timestamp: time.Now()}) {
				return
			}
		}
	}
}

const daemonEventChanBuffer = 256

// SubscribeDockerEvents prepares subscriptions before the watcher starts. The caller
// runs the returned worker and calls cleanup after joining it, including on failed startup.
func (s *EventService) SubscribeDockerEvents(eventBus *bus.DockerEventBus) (run func(context.Context) error, cleanup func()) {
	containers, unsubscribeContainers := eventBus.Subscribe(events.ContainerEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	images, unsubscribeImages := eventBus.Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	volumes, unsubscribeVolumes := eventBus.Subscribe(events.VolumeEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	networks, unsubscribeNetworks := eventBus.Subscribe(events.NetworkEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	cleanup = sync.OnceFunc(func() {
		unsubscribeContainers()
		unsubscribeImages()
		unsubscribeVolumes()
		unsubscribeNetworks()
	})
	return func(ctx context.Context) error {
		return s.runDockerEvents(ctx, containers, images, volumes, networks)
	}, cleanup
}

func (s *EventService) runDockerEvents(ctx context.Context, containers, images, volumes, networks <-chan events.Message) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for containers != nil || images != nil || volumes != nil || networks != nil {
		var msg events.Message
		var ok bool
		select {
		case <-ctx.Done():
			return nil
		case msg, ok = <-containers:
			if !ok {
				containers = nil
				continue
			}
		case msg, ok = <-images:
			if !ok {
				images = nil
				continue
			}
		case msg, ok = <-volumes:
			if !ok {
				volumes = nil
				continue
			}
		case msg, ok = <-networks:
			if !ok {
				networks = nil
				continue
			}
		case <-ticker.C:
			s.correlation.Prune()
			continue
		}
		s.RecordDockerEvent(ctx, msg)
	}
	return nil
}

// RecordDockerEvent persists a daemon occurrence once across normal and overflow delivery.
func (s *EventService) RecordDockerEvent(ctx context.Context, msg events.Message) {
	if ctx.Err() != nil {
		return
	}
	req, ok := mapDaemonEvent(msg)
	if !ok {
		return
	}
	if s.ShouldSuppressDaemonEvent(string(msg.Type), msg.Actor.ID, *req.ResourceName, msg.Actor.Attributes["com.docker.compose.project"]) {
		return
	}
	if msg.TimeNano != 0 {
		identity, err := json.Marshal(msg, json.Deterministic(true))
		if err != nil {
			slog.WarnContext(ctx, "Failed to identify Docker daemon event", "type", req.Type, "resourceId", msg.Actor.ID, "error", err)
			return
		}
		req.deduplicationKey = fmt.Sprintf("%x", sha256.Sum256(identity))
	}
	if _, err := s.CreateEvent(ctx, req); err != nil {
		slog.WarnContext(ctx, "Failed to log Docker daemon event", "type", req.Type, "resourceId", msg.Actor.ID, "error", err)
	}
}

var daemonEventTypes = map[events.Type]map[events.Action]EventType{
	events.ContainerEventType: {
		events.ActionCreate:                EventTypeContainerCreate,
		events.ActionStart:                 EventTypeContainerStart,
		events.ActionStop:                  EventTypeContainerStop,
		events.ActionRestart:               EventTypeContainerRestart,
		events.ActionDie:                   EventTypeContainerDie,
		events.ActionOOM:                   EventTypeContainerOOM,
		events.ActionKill:                  EventTypeContainerKill,
		events.ActionDestroy:               EventTypeContainerDelete,
		events.ActionPause:                 EventTypeContainerPause,
		events.ActionUnPause:               EventTypeContainerUnpause,
		events.ActionRename:                EventTypeContainerRename,
		events.ActionUpdate:                EventTypeContainerUpdate,
		events.ActionHealthStatusUnhealthy: EventTypeContainerUnhealthy,
	},
	events.ImageEventType: {
		events.ActionPull:   EventTypeImagePull,
		events.ActionDelete: EventTypeImageDelete,
		events.ActionTag:    EventTypeImageTag,
		events.ActionUnTag:  EventTypeImageUntag,
		events.ActionImport: EventTypeImageImport,
		events.ActionLoad:   EventTypeImageLoad,
		events.ActionPrune:  EventTypeImagePrune,
	},
	events.VolumeEventType: {
		events.ActionCreate:  EventTypeVolumeCreate,
		events.ActionDestroy: EventTypeVolumeDelete,
		events.ActionPrune:   EventTypeVolumePrune,
	},
	events.NetworkEventType: {
		events.ActionCreate:  EventTypeNetworkCreate,
		events.ActionDestroy: EventTypeNetworkDelete,
		events.ActionPrune:   EventTypeNetworkPrune,
	},
}

func mapDaemonEvent(msg events.Message) (CreateEventRequest, bool) {
	eventType := daemonEventTypes[msg.Type][msg.Action]
	if eventType == "" || (msg.Actor.ID == "" && msg.Action != events.ActionPrune) {
		return CreateEventRequest{}, false
	}
	abnormalExit := eventType == EventTypeContainerDie && msg.Actor.Attributes["exitCode"] != "0"
	severity := kit.Ternary(abnormalExit || eventType == EventTypeContainerUnhealthy, EventSeverityWarning, EventSeverityInfo)
	if eventType == EventTypeContainerOOM {
		severity = EventSeverityError
	}

	name := cmp.Or(msg.Actor.Attributes["name"], msg.Actor.ID, "prune")
	metadata := database.JSON{"source": "docker", "action": string(msg.Action), "scope": msg.Scope}
	for _, key := range []string{"name", "image", "exitCode", "signal"} {
		if value, ok := msg.Actor.Attributes[key]; ok {
			metadata[key] = value
		}
	}
	for attr, key := range map[string]string{"com.docker.compose.project": "composeProject", "com.docker.compose.service": "composeService"} {
		if value, ok := msg.Actor.Attributes[attr]; ok {
			metadata[key] = value
		}
	}
	return CreateEventRequest{
		Type: eventType, Severity: severity,
		Title:        fmt.Sprintf("Docker %s %s: %s", msg.Type, msg.Action, name),
		Description:  fmt.Sprintf("%s '%s': %s observed from the Docker daemon", msg.Type, name, msg.Action),
		ResourceType: new(string(msg.Type)), ResourceID: new(msg.Actor.ID), ResourceName: new(name),
		EnvironmentID: new("0"), Metadata: metadata,
	}, true
}

// MarkDockerExpectation correlates subsequent daemon observations with a local Arcane action.
func (s *EventService) MarkDockerExpectation(resourceType, resourceID, resourceName string) {
	if s == nil {
		return
	}
	s.correlation.MarkExpectation(resourceType, resourceID, resourceName)
}

// BeginComposeSuppressionWindow covers daemon events labeled with the Compose project.
func (s *EventService) BeginComposeSuppressionWindow(composeProject string) func() {
	if s == nil {
		return func() {}
	}
	return s.correlation.BeginComposeWindow(composeProject)
}

// BeginDockerResourceSuppressionWindow covers a mutation for its full duration and cleanup grace.
func (s *EventService) BeginDockerResourceSuppressionWindow(resourceType, resourceID, resourceName string) func() {
	if s == nil {
		return func() {}
	}
	return s.correlation.BeginResourceWindow(resourceType, resourceID, resourceName)
}

// BeginDockerTypeSuppressionWindow covers bulk daemon operations whose per-item IDs are unknown until they finish.
func (s *EventService) BeginDockerTypeSuppressionWindow(resourceType string) func() {
	if s == nil {
		return func() {}
	}
	return s.correlation.BeginTypeWindow(resourceType)
}

// SetDockerUpdatingContainers supplies the updater's live identities without coupling domains.
func (s *EventService) SetDockerUpdatingContainers(source func() []string) {
	if s == nil {
		return
	}
	s.correlation.SetUpdatingContainers(source)
}

// ShouldSuppressDaemonEvent reports whether a daemon observation matches a recent local action.
func (s *EventService) ShouldSuppressDaemonEvent(resourceType, actorID, actorName, composeProject string) bool {
	if s == nil {
		return false
	}
	return s.correlation.ShouldSuppress(resourceType, actorID, actorName, composeProject)
}

func (s *EventService) correlateCreatedEvent(req CreateEventRequest) {
	if req.ResourceType == nil || req.Metadata["source"] == "docker" {
		return
	}
	if req.EnvironmentID != nil && *req.EnvironmentID != "" && *req.EnvironmentID != "0" {
		return
	}
	// Failure and observational records do not identify a successful Docker mutation.
	//exhaustive:ignore
	switch req.Type {
	case EventTypeContainerStart, EventTypeContainerStop, EventTypeContainerRestart,
		EventTypeContainerDelete, EventTypeContainerCreate, EventTypeContainerUpdate,
		EventTypeContainerDeploy, EventTypeContainerKill, EventTypeContainerPause, EventTypeContainerUnpause,
		EventTypeImagePull, EventTypeImageLoad, EventTypeImageTag, EventTypeImageCommit, EventTypeImageDelete,
		EventTypeVolumeCreate, EventTypeVolumeDelete, EventTypeVolumeRename,
		EventTypeNetworkCreate, EventTypeNetworkDelete:
	default:
		return
	}
	var id, name string
	if req.ResourceID != nil {
		id = *req.ResourceID
	}
	if req.ResourceName != nil {
		name = *req.ResourceName
	}
	s.MarkDockerExpectation(*req.ResourceType, id, name)
}
