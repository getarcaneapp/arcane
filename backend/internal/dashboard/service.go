package dashboard

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
)

const (
	defaultDashboardAPIKeyExpiryWindow = 14 * 24 * time.Hour
	dashboardSnapshotPreloadLimit      = 50
	dashboardSnapshotCacheTTL          = 5 * time.Second
	dashboardSnapshotBuildTimeout      = 30 * time.Second
	dashboardInventoryCollectTimeout   = 15 * time.Second
)

type DashboardService struct {
	db                   *database.DB
	dockerService        *dockerInternal.DockerClientService
	containerService     *container.ContainerService
	projectService       *project.ProjectService
	imageService         *image.ImageService
	settingsService      *settings.SettingsService
	vulnerabilityService *vulnerability.VulnerabilityService
	environmentService   *environment.EnvironmentService
	versionService       *version.VersionService
	volumeService        *volume.VolumeService

	// Shared, immutable snapshot builds indexed by [includeTables][debugAllGood][iconCatalog];
	// full snapshots resolve icons per catalog, so each catalog gets its own entry.
	snapshotFlight singleflight.Group
	snapshotCache  [2][2][2]atomic.Pointer[dashboardSnapshotCacheEntry]
}

type dashboardSnapshotCacheEntry struct {
	snapshot *dashboard.Snapshot
	builtAt  time.Time
}

type DashboardActionItemsOptions struct {
	DebugAllGood bool
}

func NewDashboardService(
	db *database.DB,
	dockerService *dockerInternal.DockerClientService,
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

// GetSnapshot returns a short-TTL cached snapshot shared by all consumers; it must not be mutated.
// includeTables builds the first-page tables; stream subscribers pass false and read only counters.
func (s *DashboardService) GetSnapshot(ctx context.Context, options DashboardActionItemsOptions, includeTables bool) (*dashboard.Snapshot, error) {
	// Trimmed snapshots skip icon resolution, so every consumer shares one entry.
	catalog := iconcatalog.DefaultCatalog
	if includeTables {
		catalog = iconcatalog.Normalize(project.IconCatalogForContext(ctx))
	}

	tables := kit.Ternary(includeTables, 1, 0)
	debug := kit.Ternary(options.DebugAllGood, 1, 0)
	icons := kit.Ternary(catalog == iconcatalog.CatalogDashboardIcons, 1, 0)
	slot := &s.snapshotCache[tables][debug][icons]
	if entry := slot.Load(); entry != nil && time.Since(entry.builtAt) < dashboardSnapshotCacheTTL {
		return entry.snapshot, nil
	}

	key := strconv.FormatBool(includeTables) + ":" + strconv.FormatBool(options.DebugAllGood) + ":" + catalog
	result, err, _ := s.snapshotFlight.Do(key, func() (any, error) {
		// Re-check so a caller queued behind the winner reuses its fresh entry.
		if entry := slot.Load(); entry != nil && time.Since(entry.builtAt) < dashboardSnapshotCacheTTL {
			return entry.snapshot, nil
		}

		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dashboardSnapshotBuildTimeout)
		defer cancel()

		snapshot, err := s.buildSnapshot(buildCtx, options, includeTables)
		if err != nil {
			return nil, err
		}
		slot.Store(&dashboardSnapshotCacheEntry{snapshot: snapshot, builtAt: time.Now()})
		return snapshot, nil
	})
	if err != nil {
		return nil, err
	}
	snapshot, ok := result.(*dashboard.Snapshot)
	if !ok {
		return nil, errors.New("dashboard snapshot cache returned unexpected type")
	}
	return snapshot, nil
}

func (s *DashboardService) buildSnapshot(ctx context.Context, options DashboardActionItemsOptions, includeTables bool) (*dashboard.Snapshot, error) {
	if s.dockerService == nil {
		return nil, errors.New("docker service not available")
	}

	dockerSnapshot, err := s.dockerService.GetSnapshot(ctx, environment.LocalEnvironmentID)
	if err != nil {
		return nil, err
	}
	dockerContainers := dockerSnapshot.Containers
	dockerImages := dockerSnapshot.Images

	filteredContainers := container.FilterExcludedContainers(dockerContainers, false, false)
	imageConsumers := container.FilterExcludedContainers(dockerContainers, false, true)

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
			containerItems = s.containerService.BuildSummaries(ctx, filteredContainers, nil, currentContainerID, currentContainerErr)
		} else {
			var excluded map[string]bool
			if s.settingsService != nil {
				excluded = docker.ExcludedContainerNameSet(s.settingsService.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""))
			}
			for _, container := range filteredContainers {
				summary := containertypes.NewSummary(container)
				summary.RedeployDisabled = labels.ShouldDisableArcaneServerRedeploy(summary.Labels, summary.ID, currentContainerID, currentContainerErr)
				summary.AutoUpdateEnabled = !labels.IsUpdateDisabled(container.Labels) && !docker.ContainerNameExcluded(container.Names, excluded)
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
			projectIDByName = s.imageService.BuildProjectIDMap(ctx, imageConsumers)
		} else {
			projectIDByName = map[string]string{}
		}
		imageUsageMap := image.BuildVolumeUsageMap(imageConsumers, projectIDByName)
		imageItems := image.MapDockerImagesToDTOs(dockerImages, dockerContainers, imageUsageMap, nil, nil)
		sort.Slice(imageItems, func(i, j int) bool {
			if imageItems[i].Size == imageItems[j].Size {
				return imageItems[i].ID < imageItems[j].ID
			}
			return imageItems[i].Size > imageItems[j].Size
		})
		imagePage = imageItems[:min(dashboardSnapshotPreloadLimit, len(imageItems))]
	}

	imageUsageCounts := dockerInternal.CountImageUsage(dockerImages, imageConsumers)

	// Unfiltered containers keep volumes mounted only by internal containers in use, matching the volumes page.
	var volumeUsageCounts *volumetypes.UsageCounts
	if s.volumeService != nil && dockerSnapshot.Volumes != nil {
		counts := s.volumeService.CountUsageFromSnapshot(dockerSnapshot.Volumes.Items, dockerContainers)
		volumeUsageCounts = &counts
	}

	actionItems, err := s.buildActionItems(ctx, options, containerCounts.StoppedContainers, dockerContainers)
	if err != nil {
		return nil, err
	}

	var versionInfo *versiontypes.Info
	if s.versionService != nil {
		versionInfo = s.versionService.GetAppVersionInfo(ctx)
	}

	preloadParams := pagination.QueryParams{Params: pagination.Params{Limit: dashboardSnapshotPreloadLimit}}

	return &dashboard.Snapshot{
		Containers: dashboard.SnapshotContainers{
			Data:       containerPage,
			Counts:     containerCounts,
			Pagination: handlerutil.PaginationResponse(pagination.BuildResponse(int64(len(filteredContainers)), int64(len(filteredContainers)), preloadParams)),
		},
		Images: dashboard.SnapshotImages{
			Data:       imagePage,
			Pagination: handlerutil.PaginationResponse(pagination.BuildResponse(int64(len(dockerImages)), int64(len(dockerImages)), preloadParams)),
		},
		ImageUsageCounts:  imageUsageCounts,
		VolumeUsageCounts: volumeUsageCounts,
		ActionItems:       *actionItems,
		Settings:          dashboard.SnapshotSettings{},
		VersionInfo:       versionInfo,
	}, nil
}

// buildActionItems derives the dashboard's action badges; allContainers is the raw snapshot
// list, reused so the update count need not re-list containers.
func (s *DashboardService) buildActionItems(ctx context.Context, options DashboardActionItemsOptions, stoppedContainers int, allContainers []dockercontainer.Summary) (*dashboard.ActionItems, error) {
	if options.DebugAllGood {
		return &dashboard.ActionItems{Items: []dashboard.ActionItem{}}, nil
	}

	var (
		pendingResourceUpdates    int
		actionableVulnerabilities int
		expiringAPIKeys           int
	)

	g, groupCtx := errgroup.WithContext(ctx)

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "dashboard action item worker")

		if s.db == nil || s.dockerService == nil {
			return nil
		}
		standaloneContainers := make([]dockercontainer.Summary, 0, len(allContainers))
		for _, c := range container.FilterExcludedContainers(allContainers, false, false) {
			if docker.ComposeProjectLabel(c.Labels) == "" {
				standaloneContainers = append(standaloneContainers, c)
			}
		}
		containerCount, err := s.getPendingContainerUpdatesCount(groupCtx, standaloneContainers)
		if err != nil {
			return err
		}
		// Hidden service identities stay so project counting can exclude their Compose and cached image records.
		projectCount := 0
		if s.projectService != nil {
			if projectCount, err = s.projectService.CountProjectsWithPendingUpdates(groupCtx, allContainers); err != nil {
				return fmt.Errorf("failed to count projects with updates: %w", err)
			}
		}
		pendingResourceUpdates = containerCount + projectCount
		return nil
	})

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "dashboard action item worker")

		if s.vulnerabilityService == nil {
			return nil
		}
		actionableVulnerabilities, workerErr = s.vulnerabilityService.ActionableCountExcludingIgnored(groupCtx)
		return workerErr
	})

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "dashboard action item worker")

		if s.db == nil {
			return nil
		}
		var count int64
		err := s.db.WithContext(groupCtx).
			Model(&apikey.ApiKey{}).
			Where("expires_at IS NOT NULL").
			Where("expires_at <= ?", time.Now().Add(defaultDashboardAPIKeyExpiryWindow)).
			Count(&count).Error
		if err != nil {
			return fmt.Errorf("failed to count expiring API keys: %w", err)
		}
		expiringAPIKeys = int(count)
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	candidates := []dashboard.ActionItem{
		{Kind: dashboard.ActionItemKindStoppedContainers, Count: stoppedContainers, Severity: dashboard.ActionItemSeverityWarning},
		{Kind: dashboard.ActionItemKindImageUpdates, Count: pendingResourceUpdates, Severity: dashboard.ActionItemSeverityWarning},
		{Kind: dashboard.ActionItemKindActionableVulnerabilities, Count: actionableVulnerabilities, Severity: dashboard.ActionItemSeverityCritical},
		{Kind: dashboard.ActionItemKindExpiringKeys, Count: expiringAPIKeys, Severity: dashboard.ActionItemSeverityWarning},
	}
	items := make([]dashboard.ActionItem, 0, len(candidates))
	for _, item := range candidates {
		if item.Count > 0 {
			items = append(items, item)
		}
	}
	return &dashboard.ActionItems{Items: items}, nil
}

// getPendingContainerUpdatesCount counts standalone containers whose stored check reports an
// update, using the container list's lookup. Containers sharing one image each count once.
func (s *DashboardService) getPendingContainerUpdatesCount(ctx context.Context, containers []dockercontainer.Summary) (int, error) {
	if s.imageService == nil || len(containers) == 0 {
		return 0, nil
	}
	updates, err := s.imageService.GetUpdateInfoByContainers(ctx, containers)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve pending container updates: %w", err)
	}
	count := 0
	for _, info := range updates {
		if info != nil && info.HasUpdate {
			count++
		}
	}
	return count, nil
}

type inventoryGauges struct {
	containers, health, images, imageSize, volumes, networks, projects metric.Int64ObservableGauge
	swarmNodes, swarmServices, swarmTasks                              metric.Int64ObservableGauge
}

// ObserveInventory reports the local Docker engine's inventory through arcane.docker.* and arcane.projects gauges.
func (s *DashboardService) ObserveInventory(meter metric.Meter) error {
	var g inventoryGauges
	var errs [10]error
	g.containers, errs[0] = meter.Int64ObservableGauge("arcane.docker.containers",
		metric.WithDescription("Containers on the local Docker engine by state"), metric.WithUnit("{container}"))
	g.health, errs[1] = meter.Int64ObservableGauge("arcane.docker.containers.health",
		metric.WithDescription("Containers with a healthcheck by health status"), metric.WithUnit("{container}"))
	g.images, errs[2] = meter.Int64ObservableGauge("arcane.docker.images",
		metric.WithDescription("Images on the local Docker engine by usage"), metric.WithUnit("{image}"))
	g.imageSize, errs[3] = meter.Int64ObservableGauge("arcane.docker.images.size",
		metric.WithDescription("Total size of images on the local Docker engine"), metric.WithUnit("By"))
	g.volumes, errs[4] = meter.Int64ObservableGauge("arcane.docker.volumes",
		metric.WithDescription("Volumes on the local Docker engine by usage, excluding Arcane-internal volumes"), metric.WithUnit("{volume}"))
	g.networks, errs[5] = meter.Int64ObservableGauge("arcane.docker.networks",
		metric.WithDescription("Networks on the local Docker engine by usage; built-in networks report as default"), metric.WithUnit("{network}"))
	g.projects, errs[6] = meter.Int64ObservableGauge("arcane.projects",
		metric.WithDescription("Arcane Compose projects by status"), metric.WithUnit("{project}"))
	g.swarmNodes, errs[7] = meter.Int64ObservableGauge("arcane.docker.swarm.nodes",
		metric.WithDescription("Swarm nodes by role and state, reported by swarm managers"), metric.WithUnit("{node}"))
	g.swarmServices, errs[8] = meter.Int64ObservableGauge("arcane.docker.swarm.services",
		metric.WithDescription("Swarm services, reported by swarm managers"), metric.WithUnit("{service}"))
	g.swarmTasks, errs[9] = meter.Int64ObservableGauge("arcane.docker.swarm.service.tasks",
		metric.WithDescription("Running and desired tasks per swarm service, reported by swarm managers"), metric.WithUnit("{task}"))
	if err := errors.Join(errs[:]...); err != nil {
		return err
	}

	_, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, dashboardInventoryCollectTimeout)
		defer cancel()
		snapshot, err := s.dockerService.GetSnapshot(ctx, environment.LocalEnvironmentID)
		if err != nil {
			slog.DebugContext(ctx, "Skipping Docker inventory metrics", "error", err)
			return nil
		}

		states := map[dockercontainer.ContainerState]int64{
			dockercontainer.StateCreated: 0, dockercontainer.StateRunning: 0, dockercontainer.StatePaused: 0, dockercontainer.StateRestarting: 0,
			dockercontainer.StateRemoving: 0, dockercontainer.StateExited: 0, dockercontainer.StateDead: 0,
		}
		health := map[dockercontainer.HealthStatus]int64{dockercontainer.Starting: 0, dockercontainer.Healthy: 0, dockercontainer.Unhealthy: 0}
		for _, c := range snapshot.Containers {
			states[c.State]++
			if c.Health != nil && c.Health.Status != dockercontainer.NoHealthcheck {
				health[c.Health.Status]++
			}
		}
		for state, n := range states {
			o.ObserveInt64(g.containers, n, metric.WithAttributes(attribute.String("container.state", string(state))))
		}
		for status, n := range health {
			o.ObserveInt64(g.health, n, metric.WithAttributes(attribute.String("container.health.status", string(status))))
		}

		images := dockerInternal.CountImageUsage(snapshot.Images, container.FilterExcludedContainers(snapshot.Containers, false, true))
		o.ObserveInt64(g.images, int64(images.Inuse), metric.WithAttributes(attribute.String("usage", "in_use")))
		o.ObserveInt64(g.images, int64(images.Unused), metric.WithAttributes(attribute.String("usage", "unused")))
		o.ObserveInt64(g.imageSize, images.TotalSize)

		if s.volumeService != nil && snapshot.Volumes != nil {
			volumes := s.volumeService.CountUsageFromSnapshot(snapshot.Volumes.Items, snapshot.Containers)
			o.ObserveInt64(g.volumes, int64(volumes.Inuse), metric.WithAttributes(attribute.String("usage", "in_use")))
			o.ObserveInt64(g.volumes, int64(volumes.Unused), metric.WithAttributes(attribute.String("usage", "unused")))
		}

		if len(snapshot.Networks) > 0 {
			inuse, unused, total := dockerInternal.CountNetworkUsage(snapshot.Networks, snapshot.Containers)
			o.ObserveInt64(g.networks, int64(inuse), metric.WithAttributes(attribute.String("usage", "in_use")))
			o.ObserveInt64(g.networks, int64(unused), metric.WithAttributes(attribute.String("usage", "unused")))
			o.ObserveInt64(g.networks, int64(total-inuse-unused), metric.WithAttributes(attribute.String("usage", "default")))
		}

		if s.projectService != nil {
			if counts, countErr := s.projectService.GetProjectStatusCounts(ctx); countErr != nil {
				slog.DebugContext(ctx, "Skipping project inventory metrics", "error", countErr)
			} else {
				other := counts.TotalProjects - counts.RunningProjects - counts.StoppedProjects - counts.ArchivedProjects
				for status, n := range map[string]int{"running": counts.RunningProjects, "stopped": counts.StoppedProjects, "archived": counts.ArchivedProjects, "unknown": other} {
					o.ObserveInt64(g.projects, int64(n), metric.WithAttributes(attribute.String("project.status", status)))
				}
			}
		}

		s.observeSwarm(ctx, o, &g)
		return nil
	}, g.containers, g.health, g.images, g.imageSize, g.volumes, g.networks, g.projects, g.swarmNodes, g.swarmServices, g.swarmTasks)
	return err
}

// observeSwarm reports nodes and services only when the local engine is a swarm manager; the ping keeps non-swarm hosts cheap.
func (s *DashboardService) observeSwarm(ctx context.Context, o metric.Observer, g *inventoryGauges) {
	cli, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return
	}
	ping, err := cli.Ping(ctx, client.PingOptions{})
	if err != nil || ping.SwarmStatus == nil || !ping.SwarmStatus.ControlAvailable {
		return
	}

	if nodes, nodeErr := cli.NodeList(ctx, client.NodeListOptions{}); nodeErr != nil {
		slog.DebugContext(ctx, "Skipping swarm node metrics", "error", nodeErr)
	} else {
		counts := map[[2]string]int64{}
		for _, node := range nodes.Items {
			counts[[2]string{string(node.Spec.Role), string(node.Status.State)}]++
		}
		for key, n := range counts {
			o.ObserveInt64(g.swarmNodes, n, metric.WithAttributes(attribute.String("swarm.node.role", key[0]), attribute.String("swarm.node.state", key[1])))
		}
	}

	services, err := cli.ServiceList(ctx, client.ServiceListOptions{Status: true})
	if err != nil {
		slog.DebugContext(ctx, "Skipping swarm service metrics", "error", err)
		return
	}
	o.ObserveInt64(g.swarmServices, int64(len(services.Items)))
	for _, service := range services.Items {
		status := cmp.Or(service.ServiceStatus, &swarm.ServiceStatus{})
		name := attribute.String("swarm.service.name", service.Spec.Name)
		o.ObserveInt64(g.swarmTasks, int64(status.RunningTasks), metric.WithAttributes(name, attribute.String("swarm.task.state", "running")))
		o.ObserveInt64(g.swarmTasks, int64(status.DesiredTasks), metric.WithAttributes(name, attribute.String("swarm.task.state", "desired")))
	}
}
