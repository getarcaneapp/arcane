package services

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/converter"
	"github.com/goccy/go-yaml"
	containertypes "go.getarcane.app/types/container"
	"go.getarcane.app/types/system"
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
		slog.Bool("containers", req.Containers),
		slog.Bool("images", req.Images),
		slog.Bool("volumes", req.Volumes),
		slog.Bool("networks", req.Networks),
		slog.Bool("build_cache", req.BuildCache),
		slog.Bool("dangling", req.Dangling))

	result := &system.PruneAllResult{Success: true}
	var mu sync.Mutex

	// 1. Prune Containers first (sequential) as it may free up other resources
	if req.Containers {
		slog.InfoContext(ctx, "Pruning stopped containers...")
		if err := s.pruneContainers(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Container pruning failed: %v", err))
			result.Success = false
		}
	}

	// 2. Prune other resources in parallel
	g, ctx := errgroup.WithContext(ctx)

	if req.Images {
		g.Go(func() error {
			danglingOnly := req.Dangling
			if settingsDangling, _ := s.getDanglingModeFromSettings(ctx); settingsDangling != danglingOnly {
				slog.DebugContext(ctx, "Prune request overriding stored image prune mode",
					slog.Bool("settings_dangling_only", settingsDangling),
					slog.Bool("request_dangling_only", danglingOnly))
			}

			slog.InfoContext(ctx, "Pruning images...", slog.Bool("dangling_only", danglingOnly))
			localResult := &system.PruneAllResult{}
			if err := s.pruneImages(ctx, danglingOnly, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Image pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.ImagesDeleted = append(result.ImagesDeleted, localResult.ImagesDeleted...)
				result.SpaceReclaimed += localResult.SpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.BuildCache {
		g.Go(func() error {
			slog.InfoContext(ctx, "Pruning build cache...")
			localResult := &system.PruneAllResult{}
			if err := s.pruneBuildCache(ctx, localResult, !req.Dangling); err != nil {
				slog.WarnContext(ctx, "Build cache pruning encountered an error", slog.String("error", err.Error()))
				// Build cache errors are often non-critical, but we log them
			} else {
				mu.Lock()
				result.SpaceReclaimed += localResult.SpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.Volumes {
		g.Go(func() error {
			slog.InfoContext(ctx, "Pruning unused volumes...")
			localResult := &system.PruneAllResult{}
			if err := s.pruneVolumes(ctx, localResult); err != nil {
				mu.Lock()
				result.Errors = append(result.Errors, fmt.Sprintf("Volume pruning failed: %v", err))
				result.Success = false
				mu.Unlock()
			} else {
				mu.Lock()
				result.VolumesDeleted = append(result.VolumesDeleted, localResult.VolumesDeleted...)
				result.SpaceReclaimed += localResult.SpaceReclaimed
				mu.Unlock()
			}
			return nil
		})
	}

	if req.Networks {
		g.Go(func() error {
			slog.InfoContext(ctx, "Pruning unused networks...")
			localResult := &system.PruneAllResult{}
			if err := s.pruneNetworks(ctx, localResult); err != nil {
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

	slog.InfoContext(ctx, "Selective prune operation completed",
		slog.Bool("success", result.Success),
		slog.Int("containers_pruned", len(result.ContainersPruned)),
		slog.Int("images_deleted", len(result.ImagesDeleted)),
		slog.Int("volumes_deleted", len(result.VolumesDeleted)),
		slog.Int("networks_deleted", len(result.NetworksDeleted)),
		slog.Uint64("space_reclaimed", result.SpaceReclaimed),
		slog.Int("error_count", len(result.Errors)))

	return result, nil
}

func (s *SystemService) getDanglingModeFromSettings(ctx context.Context) (bool, error) {
	pruneMode := s.settingsService.GetStringSetting(ctx, "dockerPruneMode", "dangling")

	switch pruneMode {
	case "dangling":
		return true, nil
	case "all":
		return false, nil
	default:
		return true, nil
	}
}

func (s *SystemService) performBatchContainerAction(ctx context.Context, containers []container.Summary, actionName string, shouldProcess func(container.Summary) bool, action func(context.Context, string) error) *containertypes.ActionResult {
	result := &containertypes.ActionResult{Success: true}
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	// Limit concurrency to avoid overwhelming Docker daemon
	g.SetLimit(5)

	for _, container := range containers {
		c := container // capture loop var
		if !shouldProcess(c) {
			continue
		}

		g.Go(func() error {
			err := action(ctx, c.ID)

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
			// Skip Arcane server container
			return c.Labels == nil || c.Labels["com.getarcaneapp.arcane.server"] != "true"
		},
		func(ctx context.Context, id string) error {
			return s.containerService.StopContainer(ctx, id, systemUser)
		}), nil
}

func (s *SystemService) pruneContainers(ctx context.Context, result *system.PruneAllResult) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := filters.NewArgs()

	report, err := dockerClient.ContainersPrune(ctx, filterArgs)
	if err != nil {
		return fmt.Errorf("failed to prune containers: %w", err)
	}

	result.ContainersPruned = report.ContainersDeleted
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneImages(ctx context.Context, danglingOnly bool, result *system.PruneAllResult) error {
	slog.DebugContext(ctx, "Starting image pruning", slog.Bool("dangling_only", danglingOnly))

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	var filterArgs filters.Args

	if danglingOnly {
		slog.DebugContext(ctx, "Configured to prune only dangling images")
		filterArgs = filters.NewArgs(filters.Arg("dangling", "true"))
	} else {
		slog.DebugContext(ctx, "Configured to prune all unused images (including non-dangling)")
		filterArgs = filters.NewArgs(filters.Arg("dangling", "false"))
	}

	report, err := dockerClient.ImagesPrune(ctx, filterArgs)
	if err != nil {
		return fmt.Errorf("failed to prune images: %w", err)
	}

	slog.InfoContext(ctx, "Image pruning completed",
		slog.Int("images_deleted", len(report.ImagesDeleted)),
		slog.Uint64("bytes_reclaimed", report.SpaceReclaimed))

	// Collect IDs to delete from DB
	var idsToDelete []string
	for _, imgReport := range report.ImagesDeleted {
		if imgReport.Deleted != "" {
			idsToDelete = append(idsToDelete, imgReport.Deleted)
		} else if imgReport.Untagged != "" {
			idsToDelete = append(idsToDelete, imgReport.Untagged)
		}
	}

	// Batch delete update records
	if len(idsToDelete) > 0 && s.db != nil {
		if err := s.db.WithContext(ctx).Where("id IN ?", idsToDelete).Delete(&models.ImageUpdateRecord{}).Error; err != nil {
			slog.WarnContext(ctx, "Failed to delete image update records",
				slog.Int("count", len(idsToDelete)),
				slog.String("error", err.Error()))
		}
	}

	result.ImagesDeleted = idsToDelete
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneBuildCache(ctx context.Context, result *system.PruneAllResult, pruneAllCache bool) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("build cache pruning failed (connection): %w", err).Error())
		slog.ErrorContext(ctx, "Error connecting to Docker for build cache prune", slog.String("error", err.Error()))
		return fmt.Errorf("failed to connect to Docker for build cache prune: %w", err)
	}

	options := build.CachePruneOptions{
		All: pruneAllCache,
	}

	slog.DebugContext(ctx, "starting build cache pruning", slog.Bool("prune_all", pruneAllCache))
	report, err := dockerClient.BuildCachePrune(ctx, options)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("build cache pruning failed: %w", err).Error())
		slog.ErrorContext(ctx, "Error pruning build cache", slog.String("error", err.Error()))
		return fmt.Errorf("failed to prune build cache: %w", err)
	}

	slog.InfoContext(ctx, "build cache pruning completed",
		slog.Int("cache_entries_deleted", len(report.CachesDeleted)),
		slog.Uint64("bytes_reclaimed", report.SpaceReclaimed),
	)

	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneVolumes(ctx context.Context, result *system.PruneAllResult) error {
	// Prune ALL unused volumes (both named and anonymous)
	// Note: Docker API only prunes volumes that are NOT in use by any containers (running or stopped)
	// With all=true, it will remove both named and anonymous unused volumes
	// With all=false, it only removes anonymous (unnamed) unused volumes
	allVolumes := true
	report, err := s.volumeService.PruneVolumesWithOptions(ctx, allVolumes)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Volume prune completed",
		slog.Int("volumes_deleted", len(report.VolumesDeleted)),
		slog.Uint64("space_reclaimed", report.SpaceReclaimed))

	result.VolumesDeleted = report.VolumesDeleted
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func (s *SystemService) pruneNetworks(ctx context.Context, result *system.PruneAllResult) error {
	// Note: Docker API only prunes networks that are NOT in use by any containers
	report, err := s.networkService.PruneNetworks(ctx)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Network prune completed",
		slog.Int("networks_deleted", len(report.NetworksDeleted)))

	result.NetworksDeleted = report.NetworksDeleted
	return nil
}

func (s *SystemService) ParseDockerRunCommand(command string) (*models.DockerRunCommand, error) {
	if command == "" {
		return nil, fmt.Errorf("docker run command must be a non-empty string")
	}

	cmd := strings.TrimSpace(command)
	cmd = regexp.MustCompile(`^docker\s+run\s+`).ReplaceAllString(cmd, "")

	if cmd == "" {
		return nil, fmt.Errorf("no arguments found after 'docker run'")
	}

	result := &models.DockerRunCommand{}
	tokens, err := converter.ParseCommandTokens(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command tokens: %w", err)
	}

	if len(tokens) == 0 {
		return nil, fmt.Errorf("no valid tokens found in docker run command")
	}

	if err := converter.ParseTokens(tokens, result); err != nil {
		return nil, err
	}

	if result.Image == "" {
		return nil, fmt.Errorf("no Docker image specified in command")
	}

	return result, nil
}

func (s *SystemService) ConvertToDockerCompose(parsed *models.DockerRunCommand) (string, string, string, error) {
	if parsed.Image == "" {
		return "", "", "", fmt.Errorf("cannot convert to Docker Compose: no image specified")
	}

	serviceName := parsed.Name
	if serviceName == "" {
		serviceName = "app"
	}

	service := models.DockerComposeService{
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
		service.Healthcheck = &models.DockerComposeHealthcheck{
			Test: parsed.HealthCheck,
		}
	}

	if parsed.MemoryLimit != "" || parsed.CPULimit != "" {
		service.Deploy = &models.DockerComposeDeploy{
			Resources: &models.DockerComposeResources{
				Limits: &models.DockerComposeResourceLimits{},
			},
		}
		if parsed.MemoryLimit != "" {
			service.Deploy.Resources.Limits.Memory = parsed.MemoryLimit
		}
		if parsed.CPULimit != "" {
			service.Deploy.Resources.Limits.CPUs = parsed.CPULimit
		}
	}

	compose := models.DockerComposeConfig{
		Services: map[string]models.DockerComposeService{
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
