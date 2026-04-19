package services

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/dockerrun"
	libupdater "github.com/getarcaneapp/arcane/backend/pkg/libarcane/updater"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/getarcaneapp/arcane/types/system"
	"github.com/goccy/go-yaml"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
)

type SystemService struct {
	db               *database.DB
	dockerService    *DockerClientService
	containerService *ContainerService
	imageService     *ImageService
	volumeService    *VolumeService
	networkService   *NetworkService
	settingsService  *SettingsService
}

func NewSystemService(
	db *database.DB,
	dockerService *DockerClientService,
	containerService *ContainerService,
	imageService *ImageService,
	volumeService *VolumeService,
	networkService *NetworkService,
	settingsService *SettingsService,
) *SystemService {
	return &SystemService{
		db:               db,
		dockerService:    dockerService,
		containerService: containerService,
		imageService:     imageService,
		volumeService:    volumeService,
		networkService:   networkService,
		settingsService:  settingsService,
	}
}

var systemUser = models.User{
	Username: "System",
}

func (s *SystemService) PruneAll(ctx context.Context, req system.PruneAllRequest) (*system.PruneAllResult, error) {
	slog.InfoContext(ctx, "Starting selective prune operation",
		"containers", req.Containers,
		"images", req.Images,
		"volumes", req.Volumes,
		"networks", req.Networks,
		"build_cache", req.BuildCache,
	)

	result := &system.PruneAllResult{Success: true}
	var mu sync.Mutex

	// 1. Prune Containers first (sequential) as it may free up other resources
	if req.Containers != nil && req.Containers.Mode != system.PruneContainerModeNone {
		slog.InfoContext(ctx, "Pruning containers...", "mode", req.Containers.Mode, "until", req.Containers.Until)
		if err := s.pruneContainersInternal(ctx, *req.Containers, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Container pruning failed: %v", err))
			result.Success = false
		}
	}

	// 2. Prune other resources in parallel
	g, groupCtx := errgroup.WithContext(ctx)

	if req.Images != nil && req.Images.Mode != system.PruneImageModeNone {
		g.Go(func() error {
			slog.InfoContext(groupCtx, "Pruning images...", "mode", req.Images.Mode, "until", req.Images.Until)
			localResult := &system.PruneAllResult{}
			if err := s.pruneImagesInternal(groupCtx, *req.Images, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Image pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.ImagesDeleted = append(result.ImagesDeleted, localResult.ImagesDeleted...)
				result.SpaceReclaimed += localResult.SpaceReclaimed
				result.ImageSpaceReclaimed += localResult.ImageSpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.BuildCache != nil && req.BuildCache.Mode != system.PruneBuildCacheModeNone {
		g.Go(func() error {
			slog.InfoContext(groupCtx, "Pruning build cache...", "mode", req.BuildCache.Mode, "until", req.BuildCache.Until)
			localResult := &system.PruneAllResult{}
			if err := s.pruneBuildCacheInternal(groupCtx, *req.BuildCache, localResult); err != nil {
				slog.WarnContext(groupCtx, "Build cache pruning encountered an error", "error", err.Error())
				// Build cache errors are often non-critical, but we log them
			} else {
				mu.Lock()
				result.SpaceReclaimed += localResult.SpaceReclaimed
				result.BuildCacheSpaceReclaimed += localResult.BuildCacheSpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.Volumes != nil && req.Volumes.Mode != system.PruneVolumeModeNone {
		g.Go(func() error {
			slog.InfoContext(groupCtx, "Pruning volumes...", "mode", req.Volumes.Mode)
			localResult := &system.PruneAllResult{}
			if err := s.pruneVolumesInternal(groupCtx, *req.Volumes, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Volume pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.VolumesDeleted = append(result.VolumesDeleted, localResult.VolumesDeleted...)
				result.SpaceReclaimed += localResult.SpaceReclaimed
				result.VolumeSpaceReclaimed += localResult.VolumeSpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.Networks != nil && req.Networks.Mode != system.PruneNetworkModeNone {
		g.Go(func() error {
			slog.InfoContext(groupCtx, "Pruning networks...", "mode", req.Networks.Mode, "until", req.Networks.Until)
			localResult := &system.PruneAllResult{}
			if err := s.pruneNetworksInternal(groupCtx, *req.Networks, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Network pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.NetworksDeleted = append(result.NetworksDeleted, localResult.NetworksDeleted...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "Prune operations failed", "error", err)
	}

	slog.InfoContext(ctx, "Selective prune operation completed", "success", result.Success, "containers_pruned", len(result.ContainersPruned), "images_deleted", len(result.ImagesDeleted), "volumes_deleted", len(result.VolumesDeleted), "networks_deleted", len(result.NetworksDeleted), "space_reclaimed", result.SpaceReclaimed, "error_count", len(result.Errors))

	return result, nil
}

func (s *SystemService) performBatchContainerAction(ctx context.Context, containers []container.Summary, actionName string, shouldProcess func(container.Summary) bool, action func(context.Context, string) error) *containertypes.ActionResult {
	result := &containertypes.ActionResult{Success: true}
	var mu sync.Mutex

	g, groupCtx := errgroup.WithContext(ctx)
	// Limit concurrency to avoid overwhelming Docker daemon
	g.SetLimit(5)

	for _, container := range containers {
		c := container // capture loop var
		if !shouldProcess(c) {
			continue
		}

		g.Go(func() error {
			err := action(groupCtx, c.ID)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.Failed = append(result.Failed, c.ID)
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to %s container %s: %v", actionName, c.ID, err))
				result.Success = false
			} else {
				if actionName == "start" {
					result.Started = append(result.Started, c.ID)
				} else {
					result.Stopped = append(result.Stopped, c.ID)
				}
			}
			return nil
		})
	}

	_ = g.Wait()
	return result
}

func (s *SystemService) StartAllContainers(ctx context.Context) (*containertypes.ActionResult, error) {
	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return &containertypes.ActionResult{
			Success: false,
			Errors:  []string{fmt.Sprintf("Failed to list containers: %v", err)},
		}, err
	}

	return s.performBatchContainerAction(ctx, containers, "start",
		func(c container.Summary) bool { return c.State != "running" },
		func(ctx context.Context, id string) error {
			return s.containerService.StartContainer(ctx, id, systemUser)
		}), nil
}

func (s *SystemService) StartAllStoppedContainers(ctx context.Context) (*containertypes.ActionResult, error) {
	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return &containertypes.ActionResult{
			Success: false,
			Errors:  []string{fmt.Sprintf("Failed to list containers: %v", err)},
		}, err
	}

	return s.performBatchContainerAction(ctx, containers, "start",
		func(c container.Summary) bool { return c.State == "exited" },
		func(ctx context.Context, id string) error {
			return s.containerService.StartContainer(ctx, id, systemUser)
		}), nil
}

func (s *SystemService) StopAllContainers(ctx context.Context) (*containertypes.ActionResult, error) {
	containers, _, _, _, err := s.dockerService.GetAllContainers(ctx)
	if err != nil {
		return &containertypes.ActionResult{
			Success: false,
			Errors:  []string{fmt.Sprintf("Failed to list containers: %v", err)},
		}, err
	}

	return s.performBatchContainerAction(ctx, containers, "stop",
		func(c container.Summary) bool {
			// Skip Arcane container
			return !libupdater.IsArcaneContainer(c.Labels)
		},
		func(ctx context.Context, id string) error {
			return s.containerService.StopContainer(ctx, id, systemUser)
		}), nil
}

func (s *SystemService) pruneContainersInternal(ctx context.Context, options system.PruneContainersOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	if options.Mode == system.PruneContainerModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return fmt.Errorf("container prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	}

	report, err := dockerClient.ContainerPrune(ctx, client.ContainerPruneOptions{Filters: filterArgs})
	if err != nil {
		return fmt.Errorf("failed to prune containers: %w", err)
	}

	result.ContainersPruned = report.Report.ContainersDeleted
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	result.ContainerSpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneImagesInternal(ctx context.Context, options system.PruneImagesOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	switch options.Mode {
	case system.PruneImageModeNone:
		return fmt.Errorf("image prune mode none is not allowed")
	case system.PruneImageModeDangling:
		filterArgs = filterArgs.Add("dangling", "true")
	case system.PruneImageModeAll:
		filterArgs = filterArgs.Add("dangling", "false")
	case system.PruneImageModeOlderThan:
		if strings.TrimSpace(options.Until) == "" {
			return fmt.Errorf("image prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("dangling", "false")
		filterArgs = filterArgs.Add("until", options.Until)
	default:
		return fmt.Errorf("unsupported image prune mode: %s", options.Mode)
	}

	report, err := dockerClient.ImagePrune(ctx, client.ImagePruneOptions{Filters: filterArgs})
	if err != nil {
		return fmt.Errorf("failed to prune images: %w", err)
	}

	slog.InfoContext(ctx, "Image pruning completed", "images_deleted", len(report.Report.ImagesDeleted), "bytes_reclaimed", report.Report.SpaceReclaimed)

	// Collect IDs to delete from DB
	var idsToDelete []string
	for _, imgReport := range report.Report.ImagesDeleted {
		if imgReport.Deleted != "" {
			idsToDelete = append(idsToDelete, imgReport.Deleted)
		} else if imgReport.Untagged != "" {
			idsToDelete = append(idsToDelete, imgReport.Untagged)
		}
	}

	// Batch delete update records
	if len(idsToDelete) > 0 && s.db != nil {
		if err := s.db.WithContext(ctx).Where("id IN ?", idsToDelete).Delete(&models.ImageUpdateRecord{}).Error; err != nil {
			slog.WarnContext(ctx, "Failed to delete image update records", "count", len(idsToDelete), "error", err.Error())
		}
	}

	result.ImagesDeleted = idsToDelete
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	result.ImageSpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneBuildCacheInternal(ctx context.Context, options system.PruneBuildCacheOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("build cache pruning failed (connection): %w", err).Error())
		slog.ErrorContext(ctx, "Error connecting to Docker for build cache prune", "error", err.Error())
		return fmt.Errorf("failed to connect to Docker for build cache prune: %w", err)
	}

	pruneOptions := client.BuildCachePruneOptions{
		All: options.Mode == system.PruneBuildCacheModeAll,
	}
	if options.Mode == system.PruneBuildCacheModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return fmt.Errorf("build cache prune mode olderThan requires until")
		}
		pruneOptions.Filters = make(client.Filters)
		pruneOptions.Filters = pruneOptions.Filters.Add("until", options.Until)
	}

	slog.DebugContext(ctx, "starting build cache pruning", "mode", options.Mode, "until", options.Until)
	report, err := dockerClient.BuildCachePrune(ctx, pruneOptions)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("build cache pruning failed: %w", err).Error())
		slog.ErrorContext(ctx, "Error pruning build cache", "error", err.Error())
		return fmt.Errorf("failed to prune build cache: %w", err)
	}

	slog.InfoContext(ctx, "build cache pruning completed", "cache_entries_deleted", len(report.Report.CachesDeleted), "bytes_reclaimed", report.Report.SpaceReclaimed)

	result.SpaceReclaimed += report.Report.SpaceReclaimed
	result.BuildCacheSpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneVolumesInternal(ctx context.Context, options system.PruneVolumesOptions, result *system.PruneAllResult) error {
	allVolumes := options.Mode == system.PruneVolumeModeAll
	report, err := s.volumeService.PruneVolumesWithOptions(ctx, allVolumes)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Volume prune completed", "volumes_deleted", len(report.VolumesDeleted), "space_reclaimed", report.SpaceReclaimed)

	result.VolumesDeleted = report.VolumesDeleted
	result.SpaceReclaimed += report.SpaceReclaimed
	result.VolumeSpaceReclaimed += report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneNetworksInternal(ctx context.Context, options system.PruneNetworksOptions, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)
	if options.Mode == system.PruneNetworkModeOlderThan {
		if strings.TrimSpace(options.Until) == "" {
			return fmt.Errorf("network prune mode olderThan requires until")
		}
		filterArgs = filterArgs.Add("until", options.Until)
	}

	report, err := dockerClient.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: filterArgs})
	if err != nil {
		return fmt.Errorf("failed to prune networks: %w", err)
	}

	slog.InfoContext(ctx, "Network prune completed", "networks_deleted", len(report.Report.NetworksDeleted))

	result.NetworksDeleted = report.Report.NetworksDeleted
	return nil
}

func (s *SystemService) ParseDockerRunCommand(command string) (*system.DockerRunCommand, error) {
	if command == "" {
		return nil, fmt.Errorf("docker run command must be a non-empty string")
	}

	cmd := strings.TrimSpace(command)
	cmd = regexp.MustCompile(`^docker\s+run\s+`).ReplaceAllString(cmd, "")

	if cmd == "" {
		return nil, fmt.Errorf("no arguments found after 'docker run'")
	}

	result := &system.DockerRunCommand{}
	tokens, err := dockerrun.ParseCommandTokens(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command tokens: %w", err)
	}

	if len(tokens) == 0 {
		return nil, fmt.Errorf("no valid tokens found in docker run command")
	}

	if err := dockerrun.ParseTokens(tokens, result); err != nil {
		return nil, err
	}

	if result.Image == "" {
		return nil, fmt.Errorf("no Docker image specified in command")
	}

	return result, nil
}

func (s *SystemService) ConvertToDockerCompose(parsed *system.DockerRunCommand) (string, string, string, error) {
	if parsed.Image == "" {
		return "", "", "", fmt.Errorf("cannot convert to Docker Compose: no image specified")
	}

	serviceName := parsed.Name
	if serviceName == "" {
		serviceName = "app"
	}

	service := system.DockerComposeService{
		Image: parsed.Image,
	}

	if parsed.Name != "" {
		service.ContainerName = parsed.Name
	}

	if len(parsed.Ports) > 0 {
		service.Ports = parsed.Ports
	}

	if len(parsed.Volumes) > 0 {
		service.Volumes = parsed.Volumes
	}

	if len(parsed.Environment) > 0 {
		service.Environment = parsed.Environment
	}

	if len(parsed.Networks) > 0 {
		service.Networks = parsed.Networks
	}

	if parsed.Restart != "" {
		service.Restart = parsed.Restart
	}

	if parsed.Workdir != "" {
		service.WorkingDir = parsed.Workdir
	}

	if parsed.User != "" {
		service.User = parsed.User
	}

	if parsed.Entrypoint != "" {
		service.Entrypoint = parsed.Entrypoint
	}

	if parsed.Command != "" {
		service.Command = parsed.Command
	}

	if parsed.Interactive && parsed.TTY {
		service.StdinOpen = true
		service.TTY = true
	}

	if parsed.Privileged {
		service.Privileged = true
	}

	if len(parsed.Labels) > 0 {
		service.Labels = parsed.Labels
	}

	if parsed.HealthCheck != "" {
		service.Healthcheck = &system.DockerComposeHealthcheck{
			Test: parsed.HealthCheck,
		}
	}

	if parsed.MemoryLimit != "" || parsed.CPULimit != "" {
		service.Deploy = &system.DockerComposeDeploy{
			Resources: &system.DockerComposeResources{
				Limits: &system.DockerComposeResourceLimits{},
			},
		}
		if parsed.MemoryLimit != "" {
			service.Deploy.Resources.Limits.Memory = parsed.MemoryLimit
		}
		if parsed.CPULimit != "" {
			service.Deploy.Resources.Limits.CPUs = parsed.CPULimit
		}
	}

	compose := system.DockerComposeConfig{
		Services: map[string]system.DockerComposeService{
			serviceName: service,
		},
	}

	// Convert to YAML
	yamlData, err := yaml.Marshal(&compose)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to convert to YAML: %w", err)
	}

	// Generate environment variables file content
	envVars := strings.Join(parsed.Environment, "\n")

	return string(yamlData), envVars, serviceName, nil
}

func (s *SystemService) GetDiskUsagePath(ctx context.Context) string {
	cfg := s.settingsService.GetSettingsConfig()
	if cfg == nil {
		return "/"
	}

	path := cfg.DiskUsagePath.Value
	if path == "" {
		path = "/"
	}
	return path
}
