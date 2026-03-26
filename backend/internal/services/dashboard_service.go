package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/types/base"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
)

const defaultDashboardAPIKeyExpiryWindow = 14 * 24 * time.Hour
const dashboardSnapshotPreloadLimit = 50

type DashboardService struct {
	db                   *database.DB
	dockerService        *DockerClientService
	containerService     *ContainerService
	settingsService      *SettingsService
	vulnerabilityService *VulnerabilityService
}

type DashboardActionItemsOptions struct {
	DebugAllGood bool
}

func NewDashboardService(
	db *database.DB,
	dockerService *DockerClientService,
	containerService *ContainerService,
	settingsService *SettingsService,
	vulnerabilityService *VulnerabilityService,
) *DashboardService {
	return &DashboardService{
		db:                   db,
		dockerService:        dockerService,
		containerService:     containerService,
		settingsService:      settingsService,
		vulnerabilityService: vulnerabilityService,
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

	projectIDByName := buildProjectIDMapInternal(ctx, s.db, filteredContainers)
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
		Settings: dashboardtypes.SnapshotSettings{
			DockerPruneMode: s.getDashboardDockerPruneModeInternal(ctx),
		},
	}, nil
}

func (s *DashboardService) GetActionItems(ctx context.Context, options DashboardActionItemsOptions) (*dashboardtypes.ActionItems, error) {
	if options.DebugAllGood {
		return &dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}}, nil
	}

	var (
		stoppedContainers         int
		pendingImageUpdates       int
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
		count, err := s.getPendingImageUpdatesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		pendingImageUpdates = count
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

	if pendingImageUpdates > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindImageUpdates,
			Count:    pendingImageUpdates,
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

func (s *DashboardService) buildActionItemsForSnapshotInternal(
	ctx context.Context,
	options DashboardActionItemsOptions,
	containers []dockercontainer.Summary,
	images []dockerimage.Summary,
) (*dashboardtypes.ActionItems, error) {
	if options.DebugAllGood {
		return &dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}}, nil
	}

	var (
		pendingImageUpdates       int
		actionableVulnerabilities int
		expiringAPIKeys           int
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		count, err := s.getPendingImageUpdatesCountForImageIDs(groupCtx, extractDockerImageIDsInternal(images))
		if err != nil {
			return err
		}
		pendingImageUpdates = count
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

	return buildDashboardActionItemsInternal(stoppedContainers, pendingImageUpdates, actionableVulnerabilities, expiringAPIKeys), nil
}

func buildDashboardActionItemsInternal(
	stoppedContainers int,
	pendingImageUpdates int,
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

	if pendingImageUpdates > 0 {
		actionItems = append(actionItems, dashboardtypes.ActionItem{
			Kind:     dashboardtypes.ActionItemKindImageUpdates,
			Count:    pendingImageUpdates,
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

func (s *DashboardService) getPendingImageUpdatesCountInternal(ctx context.Context) (int, error) {
	if s.db == nil || s.dockerService == nil {
		return 0, nil
	}

	images, _, _, _, err := s.dockerService.GetAllImages(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load images for update counts: %w", err)
	}

	return s.getPendingImageUpdatesCountForImageIDs(ctx, extractDockerImageIDsInternal(images))
}

func (s *DashboardService) getPendingImageUpdatesCountForImageIDs(ctx context.Context, imageIDs []string) (int, error) {
	if s.db == nil || len(imageIDs) == 0 {
		return 0, nil
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.ImageUpdateRecord{}).
		Where("id IN ? AND has_update = ?", imageIDs, true).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count pending image updates: %w", err)
	}

	return int(count), nil
}

func (s *DashboardService) getDashboardDockerPruneModeInternal(ctx context.Context) string {
	if s.settingsService == nil {
		return "dangling"
	}

	return s.settingsService.GetStringSetting(ctx, "dockerPruneMode", "dangling")
}

func (s *DashboardService) getActionableVulnerabilitiesCountInternal(ctx context.Context) (int, error) {
	if s.vulnerabilityService == nil {
		return 0, nil
	}

	summary, err := s.vulnerabilityService.GetEnvironmentSummary(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load vulnerability summary: %w", err)
	}

	if summary == nil || summary.Summary == nil {
		return 0, nil
	}

	return summary.Summary.Critical + summary.Summary.High, nil
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

func extractDockerImageIDsInternal(images []dockerimage.Summary) []string {
	if len(images) == 0 {
		return nil
	}

	imageIDs := make([]string, 0, len(images))
	for _, img := range images {
		if img.ID == "" {
			continue
		}
		imageIDs = append(imageIDs, img.ID)
	}

	return imageIDs
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
