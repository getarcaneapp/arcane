package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	"go.getarcane.app/sys/cgroup"
	libupdater "go.getarcane.app/updater/pkg/labels"
)

const defaultDashboardAPIKeyExpiryWindow = 14 * 24 * time.Hour
const dashboardSnapshotPreloadLimit = 50
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
		return nil, errors.New("docker service not available")
	}

	dockerSnapshot, err := s.dockerService.GetSnapshot(ctx, localEnvironmentID)
	if err != nil {
		return nil, err
	}
	dockerContainers := dockerSnapshot.Containers
	dockerImages := dockerSnapshot.Images

	filteredContainers := filterInternalContainers(dockerContainers, false)
	containerItems := make([]containertypes.Summary, 0, len(filteredContainers))
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	if s.containerService != nil {
		containerItems = s.containerService.buildContainerSummaries(filteredContainers, nil, currentContainerID, currentContainerErr)
	} else {
		for _, container := range filteredContainers {
			summary := containertypes.NewSummary(container)
			summary.RedeployDisabled = libupdater.ShouldDisableArcaneServerRedeploy(summary.Labels, summary.ID, currentContainerID, currentContainerErr)
			containerItems = append(containerItems, summary)
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
	if s.containerService != nil {
		s.containerService.applyContainerSummaryIconsInternal(ctx, containerPage, nil)
	}

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

	var versionInfo *versiontypes.Info
	if s.versionService != nil {
		versionInfo = s.versionService.GetAppVersionInfo(ctx)
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
		VersionInfo:      versionInfo,
	}, nil
}

func (s *DashboardService) buildActionItemsForSnapshotInternal(
	ctx context.Context,
	options DashboardActionItemsOptions,
	containers []dockercontainer.Summary,
	_ any,
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

func (s *DashboardService) getPendingResourceUpdatesCountInternal(ctx context.Context) (int, error) {
	if s.db == nil || s.dockerService == nil {
		return 0, nil
	}

	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load containers for update counts: %w", err)
	}

	filteredContainers := filterInternalContainers(containers, false)
	standaloneContainers := filterStandaloneDockerContainersInternal(filteredContainers)
	containerCount, err := s.getPendingContainerUpdatesCountForImageIDsInternal(ctx, collectImageIDs(standaloneContainers))
	if err != nil {
		return 0, err
	}

	projectCount, err := s.getPendingProjectUpdatesCountInternal(ctx)
	if err != nil {
		return 0, err
	}

	return containerCount + projectCount, nil
}

func filterStandaloneDockerContainersInternal(containers []dockercontainer.Summary) []dockercontainer.Summary {
	filtered := make([]dockercontainer.Summary, 0, len(containers))
	for _, c := range containers {
		if dockerutils.ComposeProjectLabel(c.Labels) != "" {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
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
