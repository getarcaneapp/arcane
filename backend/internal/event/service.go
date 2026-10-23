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
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/streams/agg"
	"go.getarcane.app/streams/bus"
	"golang.org/x/sync/semaphore"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event/children/correlation"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

// maxConcurrentEdgeSyncs bounds in-flight event forwards to the manager.
const (
	maxConcurrentEdgeSyncs = 16
	edgeSyncQueueTimeout   = time.Minute
	edgeSyncPublishTimeout = 10 * time.Second
)

const (
	daemonEventQueueSize     = 1024
	DaemonEventBatchSize     = 64
	daemonEventFlushInterval = 250 * time.Millisecond
)

const (
	changeKindAll             = "all"
	changeStreamFlushInterval = time.Second
	changeWaitCoalesce        = 250 * time.Millisecond
	changeWaitLimit           = timeouts.DefaultProxyRequest - 5*time.Second
)

var changeKindAliases = map[string]string{
	"activities":      "activity",
	"builds":          "build",
	"containers":      "container",
	"environments":    "environment",
	"events":          "event",
	"git":             "gitops",
	"image_updates":   "image_update",
	"images":          "image",
	"networks":        "network",
	"projects":        "project",
	"volumes":         "volume",
	"vulnerabilities": "vulnerability",
	"webhooks":        "webhook",
}

type EventService struct {
	Changes        *concurrency.Signal[types.EnvironmentChanges]
	changeEpoch    string
	changesMu      sync.RWMutex
	changeRevision uint64
	changeKinds    map[string]map[string]uint64
	correlation    *correlation.Service
	db             *database.DB
	cfg            *config.Config
	httpClient     *http.Client
	edgeSyncs      *semaphore.Weighted
	// Daemon events are persisted in batches by a writer started on first use.
	daemonQueue   chan CreateEventRequest
	daemonStart   sync.Once
	daemonDone    chan struct{}
	daemonMu      sync.RWMutex
	daemonStopped bool
}

func NewEventService(db *database.DB, cfg *config.Config, httpClient *http.Client) *EventService {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	return &EventService{
		Changes:     concurrency.NewSignal[types.EnvironmentChanges](),
		changeEpoch: uuid.New().String(),
		changeKinds: map[string]map[string]uint64{},
		correlation: correlation.NewService(time.Now),
		db:          db,
		cfg:         cfg,
		httpClient:  httpClient,
		edgeSyncs:   semaphore.NewWeighted(maxConcurrentEdgeSyncs),
		daemonQueue: make(chan CreateEventRequest, daemonEventQueueSize),
		daemonDone:  make(chan struct{}),
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
	records, err := s.persistEvents(ctx, []CreateEventRequest{req})
	if err != nil {
		return nil, err
	}
	return records[0], nil
}

// persistEvents inserts requests in one transaction, skipping replayed deduplication keys.
func (s *EventService) persistEvents(ctx context.Context, reqs []CreateEventRequest) ([]*Event, error) {
	records := make([]*Event, len(reqs))
	inserted := make([]bool, len(reqs))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, req := range reqs {
			// A missing actor half mirrors the other; no actor at all is the system.
			userID, username := strings.TrimSpace(kit.FromPtr(req.UserID)), strings.TrimSpace(kit.FromPtr(req.Username))
			records[index] = &Event{
				CreatedAt:        time.Now(),
				Type:             req.Type,
				Severity:         cmp.Or(req.Severity, EventSeverityInfo),
				Title:            req.Title,
				Description:      req.Description,
				ResourceType:     req.ResourceType,
				ResourceID:       req.ResourceID,
				ResourceName:     req.ResourceName,
				UserID:           new(cmp.Or(userID, username, "system")),
				Username:         new(cmp.Or(username, userID, "System")),
				EnvironmentID:    req.EnvironmentID,
				Metadata:         req.Metadata,
				Timestamp:        time.Now(),
				DeduplicationKey: kit.Ternary(req.deduplicationKey != "", &req.deduplicationKey, nil),
			}
			create := tx
			if req.deduplicationKey != "" {
				create = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "deduplication_key"}}, DoNothing: true})
			}
			result := create.Create(records[index])
			if result.Error != nil {
				return fmt.Errorf("failed to create event: %w", result.Error)
			}
			inserted[index] = result.RowsAffected > 0
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for index, req := range reqs {
		if !inserted[index] {
			continue
		}
		s.correlateCreatedEvent(req)
		s.PublishChange(kit.FromPtr(req.EnvironmentID), string(req.Type))
		s.forwardEventToManager(ctx, records[index])
	}
	return records, nil
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
	if s.cfg == nil || !s.cfg.EdgeAgent {
		return
	}

	evt := &edge.TunnelEvent{
		Type:         string(eventModel.Type),
		Severity:     string(eventModel.Severity),
		Title:        eventModel.Title,
		Description:  eventModel.Description,
		ResourceType: kit.FromPtr(eventModel.ResourceType),
		ResourceID:   kit.FromPtr(eventModel.ResourceID),
		ResourceName: kit.FromPtr(eventModel.ResourceName),
		UserID:       kit.FromPtr(eventModel.UserID),
		Username:     kit.FromPtr(eventModel.Username),
		MetadataJSON: func() []byte {
			if eventModel.Metadata == nil {
				return nil
			}
			metadataBytes, err := json.Marshal(map[string]any(eventModel.Metadata))
			if err != nil {
				slog.WarnContext(ctx, "Failed to marshal event metadata for edge sync", "type", eventModel.Type, "error", err)
			}
			return metadataBytes
		}(),
	}

	go func(parentCtx context.Context, outgoing *edge.TunnelEvent) {
		baseCtx := context.WithoutCancel(parentCtx)
		acquireCtx, cancelAcquire := context.WithTimeout(baseCtx, edgeSyncQueueTimeout)
		acquireErr := s.edgeSyncs.Acquire(acquireCtx, 1)
		cancelAcquire()
		if acquireErr != nil {
			slog.WarnContext(baseCtx, "Dropped event sync to manager; too many in flight", "type", outgoing.Type, "error", acquireErr)
			return
		}
		defer s.edgeSyncs.Release(1)

		syncCtx, cancel := context.WithTimeout(baseCtx, edgeSyncPublishTimeout)
		defer cancel()

		if err := edge.PublishEventToManager(syncCtx, outgoing); err != nil {
			if !errors.Is(err, edge.ErrNoActiveAgentTunnel) {
				slog.WarnContext(syncCtx, "Failed to sync event to manager over edge tunnel", "type", outgoing.Type, "error", err)
				return
			}
			if strings.TrimSpace(s.cfg.AgentToken) == "" || strings.TrimSpace(httpx.ManagerBaseURL(s.cfg.ManagerApiUrl)) == "" {
				return
			}
			if httpErr := s.forwardEventToManagerHTTP(syncCtx, eventModel); httpErr != nil {
				slog.WarnContext(syncCtx, "Failed to sync event to manager over API", "type", outgoing.Type, "error", httpErr)
				return
			}
		}
	}(ctx, evt)
}

func (s *EventService) forwardEventToManagerHTTP(ctx context.Context, eventModel *Event) error {
	baseURL, err := url.Parse(strings.TrimSpace(httpx.ManagerBaseURL(s.cfg.ManagerApiUrl)))
	if err != nil {
		return fmt.Errorf("manager API URL is invalid for manager event sync: failed to parse manager API URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("manager API URL is invalid for manager event sync: unsupported scheme %q", baseURL.Scheme)
	}
	if baseURL.Host == "" {
		return errors.New("manager API URL is invalid for manager event sync: manager API URL host is required")
	}

	body, err := json.Marshal(CreateEventRequest{
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
		Metadata:      eventModel.Metadata,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, (&url.URL{
		Scheme: baseURL.Scheme,
		User:   baseURL.User,
		Host:   baseURL.Host,
		Path:   strings.TrimRight(baseURL.Path, "/") + "/api/events",
	}).String(), bytes.NewReader(body))
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

func (s *EventService) ListEventsPaginated(ctx context.Context, params pagination.QueryParams) ([]types.Event, pagination.Response, error) {
	return paginateEvents(pagination.ApplyFilter(s.db.WithContext(ctx).Model(&Event{}), "environment_id", params.Filters["environmentId"]), params)
}

func (s *EventService) GetEventsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]types.Event, pagination.Response, error) {
	return paginateEvents(s.db.WithContext(ctx).Model(&Event{}).Where("environment_id = ?", environmentID), params)
}

// paginateEvents applies the shared search and filters to q, then pages and maps the rows.
func paginateEvents(q *gorm.DB, params pagination.QueryParams) ([]types.Event, pagination.Response, error) {
	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"title LIKE ? OR description LIKE ? OR COALESCE(resource_name, '') LIKE ? OR COALESCE(username, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	// Type values with a '.' are exact types; others are category prefixes. Comma-separated values are OR-ed.
	var typeConds []string
	var typeArgs []any
	var exactTypes []string
	for part := range strings.SplitSeq(params.Filters["type"], ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case strings.Contains(part, "."):
			exactTypes = append(exactTypes, part)
		default:
			typeConds = append(typeConds, "type LIKE ?")
			typeArgs = append(typeArgs, part+".%")
		}
	}
	if len(exactTypes) > 0 {
		typeConds = append(typeConds, "type IN ?")
		typeArgs = append(typeArgs, exactTypes)
	}
	if len(typeConds) > 0 {
		q = q.Where(strings.Join(typeConds, " OR "), typeArgs...)
	}

	q = pagination.ApplyFilter(q, "severity", params.Filters["severity"])
	q = pagination.ApplyFilter(q, "resource_type", params.Filters["resourceType"])
	q = pagination.ApplyFilter(q, "username", params.Filters["username"])

	var eventRecords []Event
	paginationResp, err := pagination.PaginateAndSortDB(params, q, &eventRecords)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate events: %w", err)
	}

	eventDtos, mapErr := mapping.MapSlice[Event, types.Event](eventRecords)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map events: %w", mapErr)
	}

	return eventDtos, paginationResp, nil
}

func (s *EventService) GetEventSeverityCounts(ctx context.Context) (types.EventSeverityCounts, error) {
	var rows []struct {
		Severity string
		Count    int64
	}
	if err := s.db.WithContext(ctx).Model(&Event{}).
		Select("severity, COUNT(*) AS count").
		Group("severity").
		Scan(&rows).Error; err != nil {
		return types.EventSeverityCounts{}, fmt.Errorf("failed to count events by severity: %w", err)
	}

	var counts types.EventSeverityCounts
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
			return common.Classify(common.ErrNotFound, errors.New("event not found"))
		}
		return nil
	})
	if err == nil {
		s.PublishChange("0", "event")
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
		s.PublishChange("0", "event")
	}
	return err
}

func (s *EventService) LogContainerEvent(ctx context.Context, eventType EventType, containerID, containerName, userID, username, environmentID string, metadata database.JSON) error {
	return s.logResourceEvent(ctx, eventType, "container", containerID, containerName, userID, username, environmentID, metadata)
}

func (s *EventService) LogImageEvent(ctx context.Context, eventType EventType, imageID, imageName, userID, username, environmentID string, metadata database.JSON) error {
	return s.logResourceEvent(ctx, eventType, "image", imageID, imageName, userID, username, environmentID, metadata)
}

func (s *EventService) LogProjectEvent(ctx context.Context, eventType EventType, projectID, projectName, userID, username, environmentID string, metadata database.JSON) error {
	return s.logResourceEvent(ctx, eventType, "project", projectID, projectName, userID, username, environmentID, metadata)
}

// logResourceEvent records a successful operation on a resource using its eventDefinitions entry.
func (s *EventService) logResourceEvent(ctx context.Context, eventType EventType, resourceType, resourceID, resourceName, userID, username, environmentID string, metadata database.JSON) error {
	title, description, severity := describeEvent(eventType, resourceType, resourceName)
	_, err := s.CreateEvent(ctx, CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new(resourceType),
		ResourceID:    new(resourceID),
		ResourceName:  new(resourceName),
		UserID:        new(userID),
		Username:      new(username),
		EnvironmentID: new(environmentID),
		Metadata:      metadata,
	})
	return err
}

func (s *EventService) LogUserEvent(ctx context.Context, eventType EventType, userID, username string, metadata database.JSON) error {
	title, description, severity := describeEvent(eventType, "user", username)
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
	return s.logResourceEvent(ctx, eventType, "volume", volumeID, volumeName, userID, username, environmentID, metadata)
}

func (s *EventService) LogNetworkEvent(ctx context.Context, eventType EventType, networkID, networkName, userID, username, environmentID string, metadata database.JSON) error {
	return s.logResourceEvent(ctx, eventType, "network", networkID, networkName, userID, username, environmentID, metadata)
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

// describeEvent formats an event's title, description, and severity, with a generic fallback for undefined types.
func describeEvent(eventType EventType, resourceType, resourceName string) (string, string, EventSeverity) {
	definition, ok := eventDefinitions[eventType]
	if !ok {
		return "Event: " + string(eventType), fmt.Sprintf("%s operation performed on %s '%s'", string(eventType), resourceType, resourceName), EventSeverityInfo
	}
	return fmt.Sprintf(definition.TitleFormat, resourceName), fmt.Sprintf(definition.DescriptionFormat, resourceName), definition.Severity
}

func (s *EventService) PublishChange(environmentID, kind string) {
	if s == nil {
		return
	}
	head, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(kind)), ".")
	kind = cmp.Or(changeKindAliases[head], head)
	if kind == "" {
		return
	}
	environmentID = cmp.Or(strings.TrimSpace(environmentID), "0")

	s.changesMu.Lock()
	s.changeRevision++
	kinds := maps.Clone(s.changeKinds[environmentID])
	if kinds == nil {
		kinds = map[string]uint64{}
	}
	kinds[kind] = s.changeRevision
	s.changeKinds[environmentID] = kinds
	s.changesMu.Unlock()

	s.Changes.Publish(types.EnvironmentChanges{
		EnvironmentID: environmentID,
		Epoch:         s.changeEpoch,
		Kinds:         kinds,
	})
}

func (s *EventService) EnvironmentChanges(environmentID string) types.EnvironmentChanges {
	s.changesMu.RLock()
	defer s.changesMu.RUnlock()
	return types.EnvironmentChanges{
		EnvironmentID: environmentID,
		Epoch:         s.changeEpoch,
		Kinds:         s.changeKinds[environmentID],
	}
}

func (s *EventService) WaitForChanges(ctx context.Context, environmentID, epoch string, since, waitSeconds uint64) types.EnvironmentChanges {
	wake := make(chan struct{}, 1)
	unsubscribe := s.Changes.Subscribe(func(changes types.EnvironmentChanges) {
		if changes.EnvironmentID != environmentID {
			return
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	defer unsubscribe()

	current := s.EnvironmentChanges(environmentID)
	if epoch != current.Epoch || slices.ContainsFunc(slices.Collect(maps.Values(current.Kinds)), func(revision uint64) bool {
		return revision > since
	}) {
		return current
	}

	limit := time.NewTimer(time.Duration(min(cmp.Or(waitSeconds, uint64(changeWaitLimit/time.Second)), uint64(changeWaitLimit/time.Second))) * time.Second)
	defer limit.Stop()
	select {
	case <-ctx.Done():
	case <-limit.C:
	case <-wake:
		coalesce := time.NewTimer(changeWaitCoalesce)
		defer coalesce.Stop()
		select {
		case <-ctx.Done():
		case <-coalesce.C:
		}
	}
	return s.EnvironmentChanges(environmentID)
}

func (s *EventService) RelayChanges(environmentID string, previous, current types.EnvironmentChanges) {
	switch {
	case previous.Epoch != current.Epoch:
		s.PublishChange(environmentID, changeKindAll)
	default:
		for kind, revision := range current.Kinds {
			if revision > previous.Kinds[kind] {
				s.PublishChange(environmentID, kind)
			}
		}
	}
}

func (s *EventService) RunStreamProducer(ctx context.Context, ps *authz.PermissionSet, streamEvents chan<- types.EventStreamEvent) {
	accessible := ps.AccessibleEnvironmentIDs()
	changed := make(chan struct{}, 1)
	unsubscribe := s.Changes.Subscribe(func(changes types.EnvironmentChanges) {
		if accessible != nil && !slices.Contains(accessible, changes.EnvironmentID) {
			return
		}
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer unsubscribe()

	send := func() bool {
		s.changesMu.RLock()
		changes := make([]types.EnvironmentChanges, 0, len(s.changeKinds))
		for _, environmentID := range slices.Sorted(maps.Keys(s.changeKinds)) {
			if accessible == nil || slices.Contains(accessible, environmentID) {
				changes = append(changes, types.EnvironmentChanges{
					EnvironmentID: environmentID,
					Epoch:         s.changeEpoch,
					Kinds:         s.changeKinds[environmentID],
				})
			}
		}
		s.changesMu.RUnlock()
		return agg.Send(ctx, streamEvents, types.EventStreamEvent{
			Type:      "changed",
			Timestamp: time.Now(),
			Changes:   changes,
		})
	}

	if !send() {
		return
	}
	lastSent := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
		}
		flush := time.NewTimer(time.Until(lastSent.Add(changeStreamFlushInterval)))
		select {
		case <-ctx.Done():
			flush.Stop()
			return
		case <-flush.C:
		}
		select {
		case <-changed:
		default:
		}
		if !send() {
			return
		}
		lastSent = time.Now()
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
	swarmChannels := make([]<-chan events.Message, 0, len(swarmDaemonEventTypes))
	swarmUnsubscribes := make([]func(), 0, len(swarmDaemonEventTypes))
	for _, eventType := range swarmDaemonEventTypes {
		channel, unsubscribe := eventBus.Subscribe(eventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
		swarmChannels = append(swarmChannels, channel)
		swarmUnsubscribes = append(swarmUnsubscribes, unsubscribe)
	}
	cleanup = sync.OnceFunc(func() {
		unsubscribeContainers()
		unsubscribeImages()
		unsubscribeVolumes()
		unsubscribeNetworks()
		for _, unsubscribe := range swarmUnsubscribes {
			unsubscribe()
		}
	})
	return func(ctx context.Context) error {
		swarmEvents := make(chan events.Message, daemonEventChanBuffer)
		var forwarders sync.WaitGroup
		for _, channel := range swarmChannels {
			forwarders.Go(func() {
				for msg := range channel {
					select {
					case swarmEvents <- msg:
					case <-ctx.Done():
						return
					}
				}
			})
		}
		go func() {
			forwarders.Wait()
			close(swarmEvents)
		}()
		return s.runDockerEvents(ctx, containers, images, volumes, networks, swarmEvents)
	}, cleanup
}

func (s *EventService) runDockerEvents(ctx context.Context, containers, images, volumes, networks, swarmEvents <-chan events.Message) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for containers != nil || images != nil || volumes != nil || networks != nil || swarmEvents != nil {
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
		case msg, ok = <-swarmEvents:
			if !ok {
				swarmEvents = nil
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
		switch {
		case slices.Contains(swarmDaemonEventTypes, msg.Type):
			s.PublishChange("0", "swarm")
		case msg.Type == events.NetworkEventType:
			s.PublishChange("0", string(events.NetworkEventType))
		}
		return
	}
	if s.ShouldSuppressDaemonEvent(string(msg.Type), msg.Actor.ID, *req.ResourceName, msg.Actor.Attributes["com.docker.compose.project"]) {
		s.PublishChange("0", string(msg.Type))
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
	s.daemonStart.Do(func() { go s.writeDaemonEvents(context.WithoutCancel(ctx)) })
	s.daemonMu.RLock()
	queued := false
	if !s.daemonStopped {
		select {
		case s.daemonQueue <- req:
			queued = true
		default:
		}
	}
	s.daemonMu.RUnlock()
	if queued {
		return
	}
	// A full queue or stopped writer falls back to a direct insert rather than dropping the event.
	if _, err := s.CreateEvent(ctx, req); err != nil {
		slog.WarnContext(ctx, "Failed to log Docker daemon event", "type", req.Type, "resourceId", msg.Actor.ID, "error", err)
	}
}

func (s *EventService) writeDaemonEvents(ctx context.Context) {
	defer close(s.daemonDone)
	batch := make([]CreateEventRequest, 0, DaemonEventBatchSize)
	var flush <-chan time.Time
	for open := true; open; {
		select {
		case req, ok := <-s.daemonQueue:
			open = ok
			if ok {
				batch = append(batch, req)
				if flush == nil {
					flush = time.After(daemonEventFlushInterval)
				}
				if len(batch) < DaemonEventBatchSize {
					continue
				}
			}
		case <-flush:
		}
		if _, err := s.persistEvents(ctx, batch); err != nil {
			slog.WarnContext(ctx, "Failed to persist Docker daemon event batch; retrying individually", "count", len(batch), "error", err)
			for _, req := range batch {
				if _, createErr := s.CreateEvent(ctx, req); createErr != nil {
					slog.WarnContext(ctx, "Failed to log Docker daemon event", "type", req.Type, "resourceId", kit.FromPtr(req.ResourceID), "error", createErr)
				}
			}
		}
		batch = batch[:0]
		flush = nil
	}
}

// Stop flushes queued daemon events; later observations are persisted directly.
func (s *EventService) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	s.daemonMu.Lock()
	if !s.daemonStopped {
		s.daemonStopped = true
		close(s.daemonQueue)
	}
	s.daemonMu.Unlock()
	s.daemonStart.Do(func() { close(s.daemonDone) })
	select {
	case <-s.daemonDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("failed to flush Docker daemon events: %w", ctx.Err())
	}
}

var swarmDaemonEventTypes = []events.Type{
	events.ServiceEventType,
	events.NodeEventType,
	events.SecretEventType,
	events.ConfigEventType,
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
	if cmp.Or(kit.FromPtr(req.EnvironmentID), "0") != "0" {
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
	s.correlation.MarkExpectation(*req.ResourceType, kit.FromPtr(req.ResourceID), kit.FromPtr(req.ResourceName))
}
