package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/types/base"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	environmenttypes "github.com/getarcaneapp/arcane/types/environment"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	versiontypes "github.com/getarcaneapp/arcane/types/version"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
)

const defaultDashboardAPIKeyExpiryWindow = 14 * 24 * time.Hour
const dashboardSnapshotPreloadLimit = 50
const defaultAggregateDashboardConcurrency = 4
const defaultAggregateDashboardTimeout = 20 * time.Second
const localEnvironmentID = "0"

type DashboardService struct {
	db                   *database.DB
	dockerService        *DockerClientService
	containerService     *ContainerService
	projectService       *ProjectService
	imageService         *ImageService
	settingsService      *SettingsService
	vulnerabilityService *VulnerabilityService
	environmentService   *EnvironmentService
	versionService       *VersionService
}

type DashboardActionItemsOptions struct {
	DebugAllGood bool
}

func NewDashboardService(
	db *database.DB,
	dockerService *DockerClientService,
	containerService *ContainerService,
	projectService *ProjectService,
	imageService *ImageService,
	settingsService *SettingsService,
	vulnerabilityService *VulnerabilityService,
	environmentService *EnvironmentService,
	versionService *VersionService,
) *DashboardService {
	return &DashboardService{
		db:                   db,
		dockerService:        dockerService,
		containerService:     containerService,
		projectService:       projectService,
		imageService:         imageService,
		settingsService:      settingsService,
		vulnerabilityService: vulnerabilityService,
		environmentService:   environmentService,
		versionService:       versionService,
	}
}

func (s *DashboardService) GetSnapshot(ctx context.Context, options DashboardActionItemsOptions) (*dashboardtypes.Snapshot, error) {
	if s.dockerService == nil {
		return nil, fmt.Errorf("docker service not available")
	}

	var (
		dockerContainers []dockercontainer.Summary
		dockerImages     []dockerimage.Summary
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		containers, err := s.listDashboardContainersInternal(groupCtx)
		if err != nil {
			return fmt.Errorf("failed to load dashboard containers: %w", err)
		}
		dockerContainers = containers
		return nil
	})

	g.Go(func() error {
		images, err := s.listDashboardImagesInternal(groupCtx)
		if err != nil {
			return fmt.Errorf("failed to load dashboard images: %w", err)
		}
		dockerImages = images
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	filteredContainers := filterInternalContainers(dockerContainers, false)
	containerItems := make([]containertypes.Summary, 0, len(filteredContainers))
	if s.containerService != nil {
		containerItems = s.containerService.buildContainerSummaries(filteredContainers, nil)
	} else {
		for _, container := range filteredContainers {
			containerItems = append(containerItems, containertypes.NewSummary(container))
		}
	}

	containerCounts := containertypes.StatusCounts{TotalContainers: len(containerItems)}
	if s.containerService != nil {
		containerCounts = s.containerService.calculateContainerStatusCounts(containerItems)
	} else {
		for _, item := range containerItems {
			if item.State == "running" {
				containerCounts.RunningContainers++
			} else {
				containerCounts.StoppedContainers++
			}
		}
	}

	sort.Slice(containerItems, func(i, j int) bool {
		if containerItems[i].Created == containerItems[j].Created {
			return containerItems[i].ID < containerItems[j].ID
		}
		return containerItems[i].Created > containerItems[j].Created
	})
	containerPage := limitDashboardItemsInternal(containerItems, dashboardSnapshotPreloadLimit)

	var projectIDByName map[string]string
	if s.imageService != nil {
		projectIDByName = s.imageService.BuildProjectIDMap(ctx, filteredContainers)
	} else {
		projectIDByName = map[string]string{}
	}
	imageUsageMap := buildUsageMapInternal(filteredContainers, projectIDByName)
	imageItems := mapDockerImagesToDTOs(dockerImages, imageUsageMap, nil, nil)
	sort.Slice(imageItems, func(i, j int) bool {
		if imageItems[i].Size == imageItems[j].Size {
			return imageItems[i].ID < imageItems[j].ID
		}
		return imageItems[i].Size > imageItems[j].Size
	})
	imagePage := limitDashboardItemsInternal(imageItems, dashboardSnapshotPreloadLimit)

	imageUsageCounts := imagetypes.UsageCounts{}
	imageUsageCounts.Inuse, imageUsageCounts.Unused, imageUsageCounts.Total = countImageUsageInternal(dockerImages, filteredContainers)
	for _, img := range dockerImages {
		imageUsageCounts.TotalSize += img.Size
	}

	actionItems, err := s.buildActionItemsForSnapshotInternal(ctx, options, filteredContainers, dockerImages)
	if err != nil {
		return nil, err
	}

	return &dashboardtypes.Snapshot{
		Containers: dashboardtypes.SnapshotContainers{
			Data:       containerPage,
			Counts:     containerCounts,
			Pagination: buildDashboardPaginationResponseInternal(len(containerItems), dashboardSnapshotPreloadLimit),
		},
		Images: dashboardtypes.SnapshotImages{
			Data:       imagePage,
			Pagination: buildDashboardPaginationResponseInternal(len(imageItems), dashboardSnapshotPreloadLimit),
		},
		ImageUsageCounts: imageUsageCounts,
		ActionItems:      *actionItems,
		Settings:         dashboardtypes.SnapshotSettings{},
	}, nil
}

func (s *DashboardService) GetActionItems(ctx context.Context, options DashboardActionItemsOptions) (*dashboardtypes.ActionItems, error) {
	if options.DebugAllGood {
		return &dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}}, nil
	}

	var (
		stoppedContainers         int
		pendingResourceUpdates    int
		actionableVulnerabilities int
		expiringAPIKeys           int
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		count, err := s.getStoppedContainersCountInternal(groupCtx)
		if err != nil {
			return err
		}
		stoppedContainers = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getPendingResourceUpdatesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		pendingResourceUpdates = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getActionableVulnerabilitiesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		actionableVulnerabilities = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getExpiringAPIKeysCountInternal(groupCtx)
		if err != nil {
			return err
		}
		expiringAPIKeys = count
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	actionItems := make([]dashboardtypes.ActionItem, 0, 4)

	if stoppedContainers > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindStoppedContainers,
			Count:    stoppedContainers,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	if pendingResourceUpdates > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindImageUpdates,
			Count:    pendingResourceUpdates,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	if actionableVulnerabilities > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindActionableVulnerabilities,
			Count:    actionableVulnerabilities,
			Severity: dashboardtypes.ActionItemSeverityCritical,
		})
	}

	if expiringAPIKeys > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindExpiringKeys,
			Count:    expiringAPIKeys,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	return &dashboardtypes.ActionItems{Items: actionItems}, nil
}

func (s *DashboardService) GetEnvironmentsOverview(
	ctx context.Context,
	options DashboardActionItemsOptions,
) (*dashboardtypes.EnvironmentsOverview, error) {
	if s.environmentService == nil {
		return nil, fmt.Errorf("environment service not available")
	}

	environments, err := s.environmentService.ListVisibleEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}

	overview := &dashboardtypes.EnvironmentsOverview{
		Environments: make([]dashboardtypes.EnvironmentOverview, len(environments)),
	}

	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(defaultAggregateDashboardConcurrency)

	for i := range environments {
		index := i
		env := environments[i]

		g.Go(func() error {
			overview.Environments[index] = s.buildEnvironmentOverviewInternal(groupCtx, env, options)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to build environments overview: %w", err)
	}

	sort.SliceStable(overview.Environments, func(i, j int) bool {
		left := overview.Environments[i].Environment
		right := overview.Environments[j].Environment
		if left.ID == localEnvironmentID {
			return true
		}
		if right.ID == localEnvironmentID {
			return false
		}
		return left.Name < right.Name
	})

	overview.Summary = summarizeEnvironmentOverviewInternal(overview.Environments)
	return overview, nil
}

func (s *DashboardService) buildActionItemsForSnapshotInternal(
	ctx context.Context,
	options DashboardActionItemsOptions,
	containers []dockercontainer.Summary,
	_ []dockerimage.Summary,
) (*dashboardtypes.ActionItems, error) {
	if options.DebugAllGood {
		return &dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}}, nil
	}

	var (
		pendingResourceUpdates    int
		actionableVulnerabilities int
		expiringAPIKeys           int
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		count, err := s.getPendingResourceUpdatesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		pendingResourceUpdates = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getActionableVulnerabilitiesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		actionableVulnerabilities = count
		return nil
	})

	g.Go(func() error {
		count, err := s.getExpiringAPIKeysCountInternal(groupCtx)
		if err != nil {
			return err
		}
		expiringAPIKeys = count
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	stoppedContainers := 0
	for _, container := range containers {
		if container.State != "running" {
			stoppedContainers++
		}
	}

	return buildDashboardActionItemsInternal(stoppedContainers, pendingResourceUpdates, actionableVulnerabilities, expiringAPIKeys), nil
}

func buildDashboardActionItemsInternal(
	stoppedContainers int,
	pendingResourceUpdates int,
	actionableVulnerabilities int,
	expiringAPIKeys int,
) *dashboardtypes.ActionItems {
	actionItems := make([]dashboardtypes.ActionItem, 0, 4)

	if stoppedContainers > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindStoppedContainers,
			Count:    stoppedContainers,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	if pendingResourceUpdates > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindImageUpdates,
			Count:    pendingResourceUpdates,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	if actionableVulnerabilities > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindActionableVulnerabilities,
			Count:    actionableVulnerabilities,
			Severity: dashboardtypes.ActionItemSeverityCritical,
		})
	}

	if expiringAPIKeys > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindExpiringKeys,
			Count:    expiringAPIKeys,
			Severity: dashboardtypes.ActionItemSeverityWarning,
		})
	}

	return &dashboardtypes.ActionItems{Items: actionItems}
}

func (s *DashboardService) buildEnvironmentOverviewInternal(
	ctx context.Context,
	env environmenttypes.Environment,
	options DashboardActionItemsOptions,
) dashboardtypes.EnvironmentOverview {
	overview := dashboardtypes.EnvironmentOverview{
		Environment:      env,
		Containers:       containertypes.StatusCounts{},
		ImageUsageCounts: imagetypes.UsageCounts{},
		ActionItems:      dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}},
		Settings:         dashboardtypes.SnapshotSettings{},
		SnapshotState:    dashboardtypes.EnvironmentSnapshotStateSkipped,
	}

	if !env.Enabled || !shouldFetchEnvironmentSnapshotInternal(env) {
		return overview
	}

	var (
		snapshot    *dashboardtypes.Snapshot
		snapshotErr error
		versionInfo *versiontypes.Info
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		snapshot, snapshotErr = s.getSnapshotForEnvironmentInternal(groupCtx, env, options)
		return nil
	})

	g.Go(func() error {
		versionInfo = s.getVersionInfoForEnvironmentInternal(groupCtx, env)
		return nil
	})

	_ = g.Wait()

	if versionInfo != nil {
		overview.VersionInfo = versionInfo
	}

	if snapshotErr != nil {
		message := snapshotErr.Error()
		overview.SnapshotState = dashboardtypes.EnvironmentSnapshotStateError
		overview.SnapshotError = &message
		return overview
	}

	overview.Containers = snapshot.Containers.Counts
	overview.ImageUsageCounts = snapshot.ImageUsageCounts
	overview.ActionItems = snapshot.ActionItems
	overview.Settings = snapshot.Settings
	overview.SnapshotState = dashboardtypes.EnvironmentSnapshotStateReady

	return overview
}

func shouldFetchEnvironmentSnapshotInternal(env environmenttypes.Environment) bool {
	switch env.Status {
	case string(models.EnvironmentStatusOnline), string(models.EnvironmentStatusStandby):
		return true
	default:
		return false
	}
}

func (s *DashboardService) getSnapshotForEnvironmentInternal(
	ctx context.Context,
	env environmenttypes.Environment,
	options DashboardActionItemsOptions,
) (*dashboardtypes.Snapshot, error) {
	reqCtx, cancel := context.WithTimeout(ctx, defaultAggregateDashboardTimeout)
	defer cancel()

	if env.ID == localEnvironmentID {
		return s.GetSnapshot(reqCtx, options)
	}

	respBody, statusCode, err := s.environmentService.ProxyRequest(
		reqCtx,
		env.ID,
		"GET",
		buildEnvironmentDashboardProxyPathInternal(options),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to proxy dashboard snapshot: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("unexpected dashboard status code: %d", statusCode)
	}

	var response base.ApiResponse[dashboardtypes.Snapshot]
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to decode dashboard snapshot: %w", err)
	}
	if !response.Success {
		return nil, fmt.Errorf("dashboard snapshot request was not successful")
	}

	return &response.Data, nil
}

func buildEnvironmentDashboardProxyPathInternal(options DashboardActionItemsOptions) string {
	if options.DebugAllGood {
		return fmt.Sprintf("/api/environments/%s/dashboard?debugAllGood=true", localEnvironmentID)
	}

	return fmt.Sprintf("/api/environments/%s/dashboard", localEnvironmentID)
}

func (s *DashboardService) getVersionInfoForEnvironmentInternal(
	ctx context.Context,
	env environmenttypes.Environment,
) *versiontypes.Info {
	if s.versionService == nil {
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultAggregateDashboardTimeout)
	defer cancel()

	if env.ID == localEnvironmentID {
		return s.versionService.GetAppVersionInfo(reqCtx)
	}

	if s.environmentService == nil {
		return nil
	}

	respBody, statusCode, err := s.environmentService.ProxyRequest(
		reqCtx,
		env.ID,
		"GET",
		"/api/app-version",
		nil,
	)
	if err != nil {
		slog.DebugContext(reqCtx, "Failed to fetch environment version info for dashboard", "environment_id", env.ID, "error", err)
		return nil
	}
	if statusCode < 200 || statusCode >= 300 {
		slog.DebugContext(reqCtx, "Unexpected environment version status code for dashboard", "environment_id", env.ID, "status_code", statusCode)
		return nil
	}

	var versionInfo versiontypes.Info
	if err := json.Unmarshal(respBody, &versionInfo); err != nil {
		slog.DebugContext(reqCtx, "Failed to decode environment version info for dashboard", "environment_id", env.ID, "error", err)
		return nil
	}

	return &versionInfo
}

func (s *DashboardService) getStoppedContainersCountInternal(ctx context.Context) (int, error) {
	if s.dockerService == nil {
		return 0, nil
	}

	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load container counts: %w", err)
	}

	stoppedCount := 0
	for _, container := range containers {
		if libarcane.IsInternalContainer(container.Labels) {
			continue
		}

		if container.State != "running" {
			stoppedCount++
		}
	}

	return stoppedCount, nil
}

func summarizeEnvironmentOverviewInternal(items []dashboardtypes.EnvironmentOverview) dashboardtypes.EnvironmentsSummary {
	summary := dashboardtypes.EnvironmentsSummary{}

	for _, item := range items {
		summary.TotalEnvironments++

		if !item.Environment.Enabled {
			summary.DisabledEnvironments++
		} else {
			switch item.Environment.Status {
			case string(models.EnvironmentStatusOnline):
				summary.OnlineEnvironments++
			case string(models.EnvironmentStatusStandby):
				summary.StandbyEnvironments++
			case string(models.EnvironmentStatusPending):
				summary.PendingEnvironments++
			case string(models.EnvironmentStatusError):
				summary.ErrorEnvironments++
			default:
				summary.OfflineEnvironments++
			}
		}

		summary.Containers.RunningContainers += item.Containers.RunningContainers
		summary.Containers.StoppedContainers += item.Containers.StoppedContainers
		summary.Containers.TotalContainers += item.Containers.TotalContainers

		summary.ImageUsageCounts.Inuse += item.ImageUsageCounts.Inuse
		summary.ImageUsageCounts.Unused += item.ImageUsageCounts.Unused
		summary.ImageUsageCounts.Total += item.ImageUsageCounts.Total
		summary.ImageUsageCounts.TotalSize += item.ImageUsageCounts.TotalSize

		if len(item.ActionItems.Items) > 0 {
			summary.EnvironmentsWithActionItems++
		}
	}

	return summary
}

func (s *DashboardService) getPendingResourceUpdatesCountInternal(ctx context.Context) (int, error) {
	if s.db == nil || s.dockerService == nil {
		return 0, nil
	}

	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load containers for update counts: %w", err)
	}

	filteredContainers := filterInternalContainers(containers, false)
	containerCount, err := s.getPendingContainerUpdatesCountForImageIDsInternal(ctx, collectImageIDs(filteredContainers))
	if err != nil {
		return 0, err
	}

	projectCount, err := s.getPendingProjectUpdatesCountInternal(ctx)
	if err != nil {
		return 0, err
	}

	return containerCount + projectCount, nil
}

func (s *DashboardService) getPendingContainerUpdatesCountForImageIDsInternal(ctx context.Context, imageIDs []string) (int, error) {
	if s.db == nil || len(imageIDs) == 0 {
		return 0, nil
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.ImageUpdateRecord{}).
		Where("id IN ? AND has_update = ?", imageIDs, true).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count pending container updates: %w", err)
	}

	return int(count), nil
}

func (s *DashboardService) getPendingProjectUpdatesCountInternal(ctx context.Context) (int, error) {
	if s.projectService == nil {
		return 0, nil
	}

	count, err := s.projectService.countProjectsByUpdateStatusInternal(ctx, "has_update")
	if err != nil {
		return 0, fmt.Errorf("failed to count projects with updates: %w", err)
	}

	return count, nil
}
func (s *DashboardService) getActionableVulnerabilitiesCountInternal(ctx context.Context) (int, error) {
	if s.vulnerabilityService == nil {
		return 0, nil
	}

	return s.vulnerabilityService.getActionableCountExcludingIgnoredInternal(ctx)
}

func (s *DashboardService) getExpiringAPIKeysCountInternal(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.ApiKey{}).
		Where("expires_at IS NOT NULL").
		Where("expires_at <= ?", time.Now().Add(defaultDashboardAPIKeyExpiryWindow)).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count expiring API keys: %w", err)
	}

	return int(count), nil
}

func (s *DashboardService) listDashboardContainersInternal(ctx context.Context) ([]dockercontainer.Summary, error) {
	if s.dockerService == nil {
		return nil, fmt.Errorf("docker service not available")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	apiCtx, cancel := timeouts.WithTimeout(ctx, s.getDockerAPITimeoutSecondsInternal(ctx), timeouts.DefaultDockerAPI)
	defer cancel()

	containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker containers: %w", err)
	}

	return containerList.Items, nil
}

func (s *DashboardService) listDashboardImagesInternal(ctx context.Context) ([]dockerimage.Summary, error) {
	if s.dockerService == nil {
		return nil, fmt.Errorf("docker service not available")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	apiCtx, cancel := timeouts.WithTimeout(ctx, s.getDockerAPITimeoutSecondsInternal(ctx), timeouts.DefaultDockerAPI)
	defer cancel()

	imageList, err := dockerClient.ImageList(apiCtx, client.ImageListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker images: %w", err)
	}

	return imageList.Items, nil
}

func (s *DashboardService) getDockerAPITimeoutSecondsInternal(ctx context.Context) int {
	if s.settingsService == nil {
		return 0
	}

	return s.settingsService.GetIntSetting(ctx, "dockerApiTimeout", 0)
}

func buildDashboardPaginationResponseInternal(totalItems int, limit int) base.PaginationResponse {
	if limit <= 0 {
		limit = dashboardSnapshotPreloadLimit
	}

	totalPages := 1
	if totalItems > 0 {
		totalPages = (totalItems + limit - 1) / limit
	}

	return base.PaginationResponse{
		TotalPages:      int64(totalPages),
		TotalItems:      int64(totalItems),
		CurrentPage:     1,
		ItemsPerPage:    limit,
		GrandTotalItems: int64(totalItems),
	}
}

func limitDashboardItemsInternal[T any](items []T, limit int) []T {
	if limit <= 0 || len(items) <= limit {
		return items
	}

	return items[:limit]
}
