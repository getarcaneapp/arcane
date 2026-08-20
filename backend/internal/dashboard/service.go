package dashboard

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"

	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"

	"context"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"emperror.dev/errors"

	dockercontainer "github.com/moby/moby/api/types/container"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/types/v2/base"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
)

const (
	defaultDashboardAPIKeyExpiryWindow = 14 * 24 * time.Hour
	dashboardSnapshotPreloadLimit      = 50
	dashboardSnapshotCacheTTL          = 5 * time.Second
	dashboardSnapshotBuildTimeout      = 30 * time.Second
)

type DashboardService struct {
	db                   *database.DB
	dockerService        *docker.DockerClientService
	containerService     *container.ContainerService
	projectService       *project.ProjectService
	imageService         *image.ImageService
	settingsService      *settings.SettingsService
	vulnerabilityService *vulnerability.VulnerabilityService
	environmentService   *environment.EnvironmentService
	versionService       *version.VersionService
	volumeService        *volume.VolumeService

	// snapshotFlight + snapshotCache share one snapshot build across all
	// concurrent dashboard consumers (HTTP first paint + every stream
	// subscriber). Cached snapshots are shared pointers and must be treated
	// as immutable by callers. Indexed by
	// [includeTables][debugAllGood][iconCatalog]: full snapshots resolve
	// container icons against the requesting user's icon catalog, so each
	// catalog gets its own entry.
	snapshotFlight singleflight.Group
	snapshotCache  [2][2][2]atomic.Pointer[dashboardSnapshotCacheEntryInternal]
}

type dashboardSnapshotCacheEntryInternal struct {
	snapshot *dashboardtypes.Snapshot
	builtAt  time.Time
}

func dashboardCacheIndexInternal(flag bool) int {
	if flag {
		return 1
	}
	return 0
}

type DashboardActionItemsOptions struct {
	DebugAllGood bool
}

func NewDashboardService(
	db *database.DB,
	dockerService *docker.DockerClientService,
	containerService *container.ContainerService,
	projectService *project.ProjectService,
	imageService *image.ImageService,
	settingsService *settings.SettingsService,
	vulnerabilityService *vulnerability.VulnerabilityService,
	environmentService *environment.EnvironmentService,
	versionService *version.VersionService,
	volumeService *volume.VolumeService,
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
		volumeService:        volumeService,
	}
}

// GetSnapshot returns the dashboard snapshot, shared across all concurrent
// consumers (HTTP first paint + every stream subscriber) through a short-TTL
// cache; the result is a shared pointer and must not be mutated.
// includeTables controls whether the first-page container/image tables are
// built: stream subscribers pass false since the all-environments dashboard
// only reads the aggregate counters, skipping the per-container DTO builds,
// sorting, and icon resolution entirely.
func (s *DashboardService) GetSnapshot(ctx context.Context, options DashboardActionItemsOptions, includeTables bool) (*dashboardtypes.Snapshot, error) {
	// Trimmed snapshots skip icon resolution entirely, so every consumer
	// shares one entry regardless of the requesting user's catalog.
	catalog := iconcatalog.DefaultCatalog
	if includeTables {
		catalog = iconcatalog.Normalize(project.IconCatalogForContext(ctx))
	}

	slot := &s.snapshotCache[dashboardCacheIndexInternal(includeTables)][dashboardCacheIndexInternal(options.DebugAllGood)][dashboardCacheIndexInternal(catalog == iconcatalog.CatalogDashboardIcons)]
	if entry := slot.Load(); entry != nil && time.Since(entry.builtAt) < dashboardSnapshotCacheTTL {
		return entry.snapshot, nil
	}

	key := strconv.FormatBool(includeTables) + ":" + strconv.FormatBool(options.DebugAllGood) + ":" + catalog
	result, err, _ := s.snapshotFlight.Do(key, func() (any, error) {
		// Re-check under the flight: a caller that queued behind the winner
		// finds the fresh entry here instead of rebuilding.
		if entry := slot.Load(); entry != nil && time.Since(entry.builtAt) < dashboardSnapshotCacheTTL {
			return entry.snapshot, nil
		}

		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dashboardSnapshotBuildTimeout)
		defer cancel()

		snapshot, err := s.buildSnapshotInternal(buildCtx, options, includeTables)
		if err != nil {
			return nil, err
		}
		slot.Store(&dashboardSnapshotCacheEntryInternal{snapshot: snapshot, builtAt: time.Now()})
		return snapshot, nil
	})
	if err != nil {
		return nil, err
	}
	snapshot, ok := result.(*dashboardtypes.Snapshot)
	if !ok {
		return nil, errors.New("dashboard snapshot cache returned unexpected type")
	}
	return snapshot, nil
}

func (s *DashboardService) buildSnapshotInternal(ctx context.Context, options DashboardActionItemsOptions, includeTables bool) (*dashboardtypes.Snapshot, error) {
	if s.dockerService == nil {
		return nil, errors.New("docker service not available")
	}

	dockerSnapshot, err := s.dockerService.GetSnapshot(ctx, environment.LocalEnvironmentID)
	if err != nil {
		return nil, err
	}
	dockerContainers := dockerSnapshot.Containers
	dockerImages := dockerSnapshot.Images

	filteredContainers := container.FilterInternalContainers(dockerContainers, false)

	containerCounts := containertypes.StatusCounts{TotalContainers: len(filteredContainers)}
	for _, c := range filteredContainers {
		if c.State == "running" {
			containerCounts.RunningContainers++
		} else {
			containerCounts.StoppedContainers++
		}
	}

	var containerPage []containertypes.Summary
	var imagePage []imagetypes.Summary
	if includeTables {
		containerItems := make([]containertypes.Summary, 0, len(filteredContainers))
		currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
		if s.containerService != nil {
			containerItems = s.containerService.BuildSummaries(filteredContainers, nil, currentContainerID, currentContainerErr)
		} else {
			for _, container := range filteredContainers {
				summary := containertypes.NewSummary(container)
				summary.RedeployDisabled = labels.ShouldDisableArcaneServerRedeploy(summary.Labels, summary.ID, currentContainerID, currentContainerErr)
				containerItems = append(containerItems, summary)
			}
		}

		sort.Slice(containerItems, func(i, j int) bool {
			if containerItems[i].Created == containerItems[j].Created {
				return containerItems[i].ID < containerItems[j].ID
			}
			return containerItems[i].Created > containerItems[j].Created
		})
		containerPage = containerItems[:min(dashboardSnapshotPreloadLimit, len(containerItems))]
		if s.containerService != nil {
			s.containerService.ApplySummaryIcons(ctx, containerPage, nil)
		}

		var projectIDByName map[string]string
		if s.imageService != nil {
			projectIDByName = s.imageService.BuildProjectIDMap(ctx, filteredContainers)
		} else {
			projectIDByName = map[string]string{}
		}
		imageUsageMap := image.BuildVolumeUsageMap(filteredContainers, projectIDByName)
		imageItems := image.MapDockerImagesToDTOs(dockerImages, imageUsageMap, nil, nil)
		sort.Slice(imageItems, func(i, j int) bool {
			if imageItems[i].Size == imageItems[j].Size {
				return imageItems[i].ID < imageItems[j].ID
			}
			return imageItems[i].Size > imageItems[j].Size
		})
		imagePage = imageItems[:min(dashboardSnapshotPreloadLimit, len(imageItems))]
	}

	imageUsageCounts := docker.CountImageUsage(dockerImages, filteredContainers)

	// Uses the unfiltered container list so a volume mounted only by an internal
	// container still counts as in use, matching the volumes page.
	var volumeUsageCounts *volumetypes.UsageCounts
	if s.volumeService != nil && dockerSnapshot.Volumes != nil {
		counts := s.volumeService.CountUsageFromSnapshot(dockerSnapshot.Volumes.Items, dockerContainers)
		volumeUsageCounts = &counts
	}

	actionItems, err := s.buildActionItemsForSnapshotInternal(ctx, options, filteredContainers, dockerContainers)
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
			Pagination: buildDashboardPaginationResponseInternal(len(filteredContainers), dashboardSnapshotPreloadLimit),
		},
		Images: dashboardtypes.SnapshotImages{
			Data:       imagePage,
			Pagination: buildDashboardPaginationResponseInternal(len(dockerImages), dashboardSnapshotPreloadLimit),
		},
		ImageUsageCounts:  imageUsageCounts,
		VolumeUsageCounts: volumeUsageCounts,
		ActionItems:       *actionItems,
		Settings:          dashboardtypes.SnapshotSettings{},
		VersionInfo:       versionInfo,
	}, nil
}

// buildActionItemsForSnapshotInternal derives the dashboard's action badges.
// filteredContainers drives the stopped-container badge; allContainers is the raw
// snapshot list, passed to the update count so it need not re-list containers.
func (s *DashboardService) buildActionItemsForSnapshotInternal(
	ctx context.Context,
	options DashboardActionItemsOptions,
	filteredContainers []dockercontainer.Summary,
	allContainers []dockercontainer.Summary,
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

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "dashboard action item worker")

		count, err := s.getPendingResourceUpdatesCountInternal(groupCtx, allContainers)
		if err != nil {
			return err
		}
		pendingResourceUpdates = count
		return nil
	})

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "dashboard action item worker")

		count, err := s.getActionableVulnerabilitiesCountInternal(groupCtx)
		if err != nil {
			return err
		}
		actionableVulnerabilities = count
		return nil
	})

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "dashboard action item worker")

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
	for _, container := range filteredContainers {
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

// getPendingResourceUpdatesCountInternal counts standalone containers and projects
// with a pending image update. allContainers is the snapshot's container list,
// reused rather than re-listed: this used to issue its own GetAllContainers on
// top of the two the project count triggered, for a single badge number.
func (s *DashboardService) getPendingResourceUpdatesCountInternal(ctx context.Context, allContainers []dockercontainer.Summary) (int, error) {
	if s.db == nil || s.dockerService == nil {
		return 0, nil
	}

	filteredContainers := container.FilterInternalContainers(allContainers, false)
	standaloneContainers := filterStandaloneDockerContainersInternal(filteredContainers)
	containerCount, err := s.getPendingContainerUpdatesCountForImageIDsInternal(ctx, container.CollectImageIDs(standaloneContainers))
	if err != nil {
		return 0, err
	}

	projectCount, err := s.getPendingProjectUpdatesCountInternal(ctx, allContainers)
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
		Model(&imageupdate.ImageUpdateRecord{}).
		Where("id IN ? AND has_update = ?", imageIDs, true).
		Count(&count).Error
	if err != nil {
		return 0, errors.WrapIf(err, "failed to count pending container updates")
	}

	return int(count), nil
}

func (s *DashboardService) getPendingProjectUpdatesCountInternal(ctx context.Context, allContainers []dockercontainer.Summary) (int, error) {
	if s.projectService == nil {
		return 0, nil
	}

	count, err := s.projectService.CountProjectsWithPendingUpdates(ctx, allContainers)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to count projects with updates")
	}

	return count, nil
}
func (s *DashboardService) getActionableVulnerabilitiesCountInternal(ctx context.Context) (int, error) {
	if s.vulnerabilityService == nil {
		return 0, nil
	}

	return s.vulnerabilityService.ActionableCountExcludingIgnored(ctx)
}

func (s *DashboardService) getExpiringAPIKeysCountInternal(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&apikey.ApiKey{}).
		Where("expires_at IS NOT NULL").
		Where("expires_at <= ?", time.Now().Add(defaultDashboardAPIKeyExpiryWindow)).
		Count(&count).Error
	if err != nil {
		return 0, errors.WrapIf(err, "failed to count expiring API keys")
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
