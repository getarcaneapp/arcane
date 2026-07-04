package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/containerstats"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cache"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"go.getarcane.app/sys/cgroup"
	libupdater "go.getarcane.app/updater/pkg/labels"
)

type ContainerService struct {
	db              *database.DB
	dockerService   *DockerClientService
	eventService    *EventService
	imageService    *ImageService
	settingsService *SettingsService
	projectService  *ProjectService
	statsHistory    containerstats.Store
	updateInfoCache *cache.KeyedCache[string, *imagetypes.UpdateInfo]
	updateInfoSF    singleflight.Group
	iconMetaCache   *cache.TTL[projects.ArcaneComposeMetadata]
}

const (
	containerGroupByProject  = "project"
	containerNoProjectGroup  = "No Project"
	containerIconMetadataTTL = 5 * time.Second
)

type ContainerListResult struct {
	Items      []containertypes.Summary
	Groups     []containertypes.SummaryGroup
	Pagination pagination.Response
	Counts     containertypes.StatusCounts
}

func NewContainerService(ctx context.Context, db *database.DB, eventService *EventService, dockerService *DockerClientService, imageService *ImageService, settingsService *SettingsService, projectService *ProjectService) *ContainerService {
	svc := &ContainerService{
		db:              db,
		eventService:    eventService,
		dockerService:   dockerService,
		imageService:    imageService,
		settingsService: settingsService,
		projectService:  projectService,
		updateInfoCache: cache.NewKeyed[string, *imagetypes.UpdateInfo](),
		iconMetaCache:   cache.NewTTL[projects.ArcaneComposeMetadata](containerIconMetadataTTL),
	}
	svc.subscribeUpdateInfoCacheInvalidationInternal(ctx)
	return svc
}

func (s *ContainerService) subscribeUpdateInfoCacheInvalidationInternal(ctx context.Context) {
	if s.dockerService == nil || s.updateInfoCache == nil || s.dockerService.EventBus() == nil {
		return
	}
	ch := make(chan events.Message, 16)
	unsubscribe := s.dockerService.EventBus().Subscribe(events.ImageEventType, ch)
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				s.updateInfoCache.InvalidateAll()
			}
		}
	}()
}

func buildCleanNetworkingConfigInternal(containerInspect container.InspectResponse, apiVersion string) *network.NetworkingConfig {
	if containerInspect.NetworkSettings == nil || len(containerInspect.NetworkSettings.Networks) == 0 {
		return nil
	}

	endpointsConfig := libarcane.SanitizeContainerCreateEndpointSettingsForDockerAPI(containerInspect.NetworkSettings.Networks, apiVersion)
	for networkName, endpoint := range endpointsConfig {
		if endpoint == nil {
			continue
		}

		endpointCopy := *endpoint
		endpointCopy.IPAMConfig = nil
		endpointsConfig[networkName] = &endpointCopy
	}

	if len(endpointsConfig) == 0 {
		return nil
	}

	return &network.NetworkingConfig{
		EndpointsConfig: endpointsConfig,
	}
}

func buildRedeployBackupNameInternal(containerName, containerID string) string {
	backupName := containerName
	if backupName == "" {
		backupName = "arcane-redeploy"
		if len(containerID) >= 12 {
			backupName = fmt.Sprintf("%s-%s", backupName, containerID[:12])
		}
	}

	return fmt.Sprintf("%s-arcane-redeploy-%d", backupName, time.Now().Unix())
}

func shouldStartRedeployedContainerInternal(containerInfo container.InspectResponse, wasRunning bool) bool {
	if !wasRunning && containerInfo.HostConfig == nil {
		return false
	}

	shouldStart := wasRunning
	if containerInfo.HostConfig != nil {
		rp := containerInfo.HostConfig.RestartPolicy.Name
		if rp == "always" || rp == "unless-stopped" || rp == "on-failure" {
			shouldStart = true
		}
	}

	return shouldStart
}

func writeContainerProgressInternal(ctx context.Context, message string, progress int, phase string) {
	progressWriter, _ := ctx.Value(projects.ProgressWriterKey{}).(io.Writer)
	if progressWriter == nil {
		return
	}
	payload := fmt.Sprintf(`{"type":"container","phase":%q,"status":%q,"progressDetail":{"current":%d,"total":100}}`+"\n", phase, message, progress)
	if _, err := progressWriter.Write([]byte(payload)); err != nil {
		slog.DebugContext(ctx, "failed to write container progress", "phase", phase, "error", err)
	}
}

func (s *ContainerService) pullRedeployImageInternal(ctx context.Context, dockerClient *client.Client, imageName, containerID, containerName string, user models.User) error {
	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
	defer pullCancel()

	pullOptions, authErr := s.imageService.getPullOptionsWithAuth(ctx, imageName, nil)
	if authErr != nil {
		slog.WarnContext(ctx, "failed to get registry authentication for container redeploy pull; proceeding without auth",
			"image", imageName,
			"error", authErr.Error(),
		)
		pullOptions = client.ImagePullOptions{}
	}

	reader, pullErr := dockerClient.ImagePull(pullCtx, imageName, pullOptions)
	if pullErr != nil && shouldRetryAnonymousPullInternal(pullOptions, pullErr) {
		slog.WarnContext(ctx, "container redeploy image pull failed with registry auth; retrying anonymously",
			"image", imageName,
			"error", pullErr.Error(),
		)
		pullOptions = client.ImagePullOptions{}
		reader, pullErr = dockerClient.ImagePull(pullCtx, imageName, pullOptions)
	}
	if pullErr != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, models.JSON{
				"action": "redeploy",
				"step":   "pull_image_timeout",
				"image":  imageName,
			})
			return fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", imageName)
		}

		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, models.JSON{
			"action": "redeploy",
			"step":   "pull_image",
			"image":  imageName,
		})
		return fmt.Errorf("failed to pull image %s: %w", imageName, pullErr)
	}
	defer func() { _ = reader.Close() }()

	streamErr := dockerutils.ConsumeJSONMessageStream(reader, nil)
	if streamErr != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", streamErr, models.JSON{
			"action": "redeploy",
			"step":   "complete_pull",
			"image":  imageName,
		})
		return fmt.Errorf("failed to complete image pull: %w", streamErr)
	}

	return nil
}

func (s *ContainerService) prepareContainerForRedeployInternal(ctx context.Context, dockerClient *client.Client, containerID, containerName, backupName string, wasRunning bool, user models.User) error {
	if containerName != "" {
		if _, err := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: backupName}); err != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
				"action":     "redeploy",
				"step":       "rename_old",
				"backupName": backupName,
			})
			return fmt.Errorf("failed to rename existing container: %w", err)
		}
	}

	if !wasRunning {
		return nil
	}

	_, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: new(30)})
	if err == nil {
		return nil
	}

	if containerName != "" {
		if _, renameErr := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: containerName}); renameErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", renameErr, models.JSON{
				"action": "redeploy",
				"step":   "restore_name_after_stop_failure",
			})
		}
	}

	s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
		"action": "redeploy",
		"step":   "stop",
	})
	return fmt.Errorf("failed to stop container: %w", err)
}

func (s *ContainerService) restoreContainerAfterRedeployFailureInternal(ctx context.Context, dockerClient *client.Client, containerID, containerName, backupName, failedStep string, wasRunning bool, user models.User) {
	if wasRunning {
		if _, startErr := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); startErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", startErr, models.JSON{
				"action":     "redeploy",
				"step":       "restore_start_original",
				"failedStep": failedStep,
			})
		}
	}

	if containerName == "" {
		return
	}

	if _, renameErr := dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: containerName}); renameErr != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, backupName, user.ID, user.Username, "0", renameErr, models.JSON{
			"action":     "redeploy",
			"step":       "restore_name",
			"failedStep": failedStep,
		})
	}
}

type containerLifecycleActionInternal struct {
	action             string
	eventType          models.EventType
	metadata           models.JSON
	warnOnLogError     bool
	runContainerAction func(*client.Client) error
}

func (s *ContainerService) runContainerLifecycleActionInternal(ctx context.Context, containerID string, user models.User, cfg containerLifecycleActionInternal) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": cfg.action})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	metadata := models.JSON{
		"action":      cfg.action,
		"containerId": containerID,
	}
	maps.Copy(metadata, cfg.metadata)

	err = s.eventService.LogContainerEvent(ctx, cfg.eventType, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		if !cfg.warnOnLogError {
			return fmt.Errorf("failed to log action: %w", err)
		}
		slog.WarnContext(ctx, "could not log container action", "action", cfg.action, "error", err)
	}

	err = cfg.runContainerAction(dockerClient)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": cfg.action})
	}
	return err
}

func (s *ContainerService) StartContainer(ctx context.Context, containerID string, user models.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "start",
		eventType:      models.EventTypeContainerStart,
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
			return err
		},
	})
}

func (s *ContainerService) StopContainer(ctx context.Context, containerID string, user models.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:    "stop",
		eventType: models.EventTypeContainerStop,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: new(30)})
			return err
		},
	})
}

func (s *ContainerService) RestartContainer(ctx context.Context, containerID string, user models.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:    "restart",
		eventType: models.EventTypeContainerRestart,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{})
			return err
		},
	})
}

// KillContainer sends a signal to the container's main process (default SIGKILL
// when signal is empty) without removing the container.
func (s *ContainerService) KillContainer(ctx context.Context, containerID, signal string, user models.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "kill",
		eventType:      models.EventTypeContainerKill,
		metadata:       models.JSON{"signal": signal},
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: signal})
			return err
		},
	})
}

// PauseContainer suspends all processes in the container.
func (s *ContainerService) PauseContainer(ctx context.Context, containerID string, user models.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "pause",
		eventType:      models.EventTypeContainerPause,
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerPause(ctx, containerID, client.ContainerPauseOptions{})
			return err
		},
	})
}

// UnpauseContainer resumes a previously paused container.
func (s *ContainerService) UnpauseContainer(ctx context.Context, containerID string, user models.User) error {
	return s.runContainerLifecycleActionInternal(ctx, containerID, user, containerLifecycleActionInternal{
		action:         "unpause",
		eventType:      models.EventTypeContainerUnpause,
		warnOnLogError: true,
		runContainerAction: func(dockerClient *client.Client) error {
			_, err := dockerClient.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{})
			return err
		},
	})
}

// CommitContainer creates an image from a container's current filesystem.
func (s *ContainerService) CommitContainer(ctx context.Context, containerID string, req containertypes.CommitRequest, user models.User) (*containertypes.CommitResult, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return nil, errors.New("container ID is required")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "commit"})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	repository := strings.TrimSpace(req.Repository)
	tag := strings.TrimSpace(req.Tag)
	reference := repository
	if repository != "" && tag != "" {
		reference = repository + ":" + tag
	}

	result, err := dockerClient.ContainerCommit(ctx, containerID, client.ContainerCommitOptions{
		Reference: reference,
		Comment:   strings.TrimSpace(req.Comment),
		Author:    strings.TrimSpace(req.Author),
		Changes:   req.Changes,
		NoPause:   req.NoPause,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeImageError, "container", containerID, reference, user.ID, user.Username, "0", err, models.JSON{"action": "commit", "reference": reference})
		return nil, fmt.Errorf("failed to commit container: %w", err)
	}

	metadata := models.JSON{
		"action":      "commit",
		"containerId": containerID,
		"imageId":     result.ID,
		"repository":  repository,
		"tag":         tag,
		"reference":   reference,
		"noPause":     req.NoPause,
	}
	if logErr := s.eventService.LogImageEvent(ctx, models.EventTypeImageCommit, containerID, reference, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log container commit action", "container", containerID, "image", result.ID, "error", logErr)
	}

	return &containertypes.CommitResult{ID: result.ID}, nil
}

// tryRedeployViaComposeProjectInternal attempts to redeploy a compose-managed
// container by delegating to ProjectService.UpdateProjectServices, which loads
// the compose project with full project_directory / env-file / include context
// and runs pull/stop/up for just the target service.
//
// Return semantics:
//   - handled=false: this container is not eligible for the compose path (no
//     labels, project not registered in Arcane's DB, etc.). The caller should
//     fall back to the standalone Docker-API redeploy.
//   - handled=true, err==nil: compose path ran successfully; newContainerID is
//     the ID of the recreated container (or the original ID if it couldn't be
//     re-located by labels).
//   - handled=true, err!=nil: compose path was attempted and failed. The
//     caller MUST surface the error and MUST NOT fall back to the standalone
//     path, which would clobber whatever partial state ComposeUp left behind.
func (s *ContainerService) tryRedeployViaComposeProjectInternal(ctx context.Context, containerInfo container.InspectResponse, containerID, containerName string, user models.User) (string, bool, error) {
	if s.projectService == nil || containerInfo.Config == nil {
		return "", false, nil
	}
	labels := containerInfo.Config.Labels
	projectName := dockerutils.ComposeProjectLabel(labels)
	serviceName := dockerutils.ComposeServiceLabel(labels)
	if projectName == "" || serviceName == "" {
		return "", false, nil
	}

	proj, err := s.projectService.GetProjectByComposeName(ctx, projectName)
	if err != nil {
		// Distinguish "not found" (safe to fall back to standalone) from real DB
		// errors (should surface so a transient failure doesn't silently recreate
		// the container from stale cached config).
		if strings.Contains(err.Error(), "not found") {
			slog.WarnContext(ctx, "RedeployContainer: compose project not registered, falling back to standalone redeploy",
				"containerId", containerID,
				"project", projectName,
				"service", serviceName,
			)
			return "", false, nil
		}
		return "", true, fmt.Errorf("failed to look up compose project %s: %w", projectName, err)
	}
	if proj == nil {
		slog.WarnContext(ctx, "RedeployContainer: compose project not registered, falling back to standalone redeploy",
			"containerId", containerID,
			"project", projectName,
			"service", serviceName,
		)
		return "", false, nil
	}

	slog.InfoContext(ctx, "RedeployContainer: detected compose container, using project-based redeploy",
		"containerId", containerID,
		"project", projectName,
		"service", serviceName,
	)

	if err := s.projectService.UpdateProjectServices(ctx, proj.ID, []string{serviceName}, user); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
			"action":      "redeploy",
			"step":        "compose_update_services",
			"project":     projectName,
			"service":     serviceName,
			"projectId":   proj.ID,
			"projectName": proj.Name,
		})
		return "", true, fmt.Errorf("compose redeploy failed for %s/%s: %w", projectName, serviceName, err)
	}

	newID := s.findComposeServiceContainerIDInternal(ctx, projectName, serviceName)
	if newID == "" {
		// Recreated successfully but couldn't locate the new container; return the
		// original ID so the handler can degrade gracefully.
		newID = containerID
	}

	if logErr := s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDeploy, newID, containerName, user.ID, user.Username, "0", models.JSON{
		"action":        "redeploy",
		"containerId":   newID,
		"containerName": containerName,
		"project":       projectName,
		"service":       serviceName,
		"projectId":     proj.ID,
		"via":           "compose",
	}); logErr != nil {
		slog.WarnContext(ctx, "failed to log compose redeploy event", "err", logErr)
	}

	return newID, true, nil
}

// findComposeServiceContainerIDInternal locates the (presumably newly recreated)
// container for a given compose project+service pair using the compose SDK's Ps
// command. When multiple containers match (a stopped predecessor can briefly
// linger during recreation), the first running one is preferred; otherwise the
// first match is returned. Returns "" when none found.
func (s *ContainerService) findComposeServiceContainerIDInternal(ctx context.Context, projectName, serviceName string) string {
	containers, err := projects.ComposePs(ctx, &composetypes.Project{Name: projectName}, []string{serviceName}, true)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve container via compose ps after redeploy",
			"project", projectName,
			"service", serviceName,
			"err", err,
		)
		return ""
	}

	var firstMatch string
	for _, c := range containers {
		if c.Service != serviceName {
			continue
		}
		if firstMatch == "" {
			firstMatch = c.ID
		}
		if c.State == "running" {
			return c.ID
		}
	}
	return firstMatch
}

func (s *ContainerService) RedeployContainer(ctx context.Context, containerID string, user models.User) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "get_client",
		})
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "inspect",
		})
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	containerInfo := containerJSON.Container
	if containerInfo.Config == nil {
		err = errors.New("container config is nil")
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "validate_config",
		})
		return "", fmt.Errorf("failed to redeploy container: %w", err)
	}

	containerName := strings.TrimPrefix(containerInfo.Name, "/")
	imageName := containerInfo.Config.Image
	wasRunning := containerInfo.State != nil && containerInfo.State.Running
	apiVersion := libarcane.DetectDockerAPIVersion(ctx, dockerClient)

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	if libupdater.ShouldDisableArcaneServerRedeploy(containerInfo.Config.Labels, containerInfo.ID, currentContainerID, currentContainerErr) {
		err = &common.ArcaneSelfRedeployError{}
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "self_redeploy_blocked",
		})
		return "", err
	}

	// If this container belongs to a known compose project, redeploy through the
	// compose-aware path so that compose file changes (healthchecks, env, etc.) and
	// the project's include/project_directory/env-file context are honored. The
	// standalone Docker-API path below only clones the existing container config
	// from the daemon and would silently ignore any compose edits.
	if newID, handled, composeErr := s.tryRedeployViaComposeProjectInternal(ctx, containerInfo, containerID, containerName, user); handled {
		if composeErr != nil {
			return "", composeErr
		}
		return newID, nil
	}

	metadata := models.JSON{
		"action":        "redeploy",
		"containerId":   containerID,
		"containerName": containerName,
		"image":         imageName,
	}

	if imageName != "" {
		writeContainerProgressInternal(ctx, "Pulling latest container image", 20, "pull")
		if err := s.pullRedeployImageInternal(ctx, dockerClient, imageName, containerID, containerName, user); err != nil {
			return "", err
		}
	}

	backupName := buildRedeployBackupNameInternal(containerName, containerID)
	writeContainerProgressInternal(ctx, "Preparing existing container", 45, "prepare")
	if err := s.prepareContainerForRedeployInternal(ctx, dockerClient, containerID, containerName, backupName, wasRunning, user); err != nil {
		return "", err
	}

	networkingConfig := buildCleanNetworkingConfigInternal(containerInfo, apiVersion)

	newConfig := *containerInfo.Config
	if len(containerID) >= 12 && newConfig.Hostname == containerID[:12] {
		newConfig.Hostname = ""
	}

	writeContainerProgressInternal(ctx, "Creating replacement container", 65, "create")
	createResp, err := libarcane.ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           &newConfig,
		HostConfig:       containerInfo.HostConfig,
		NetworkingConfig: networkingConfig,
		Name:             containerName,
	}, apiVersion)
	if err != nil {
		s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerID, containerName, backupName, "create", wasRunning, user)
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, models.JSON{
			"action": "redeploy",
			"step":   "create",
			"image":  imageName,
		})
		return "", fmt.Errorf("failed to recreate container: %w", err)
	}

	if shouldStartRedeployedContainerInternal(containerInfo, wasRunning) {
		writeContainerProgressInternal(ctx, "Starting replacement container", 80, "start")
		_, err = dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{})
		if err != nil {
			if _, removeErr := dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true}); removeErr != nil {
				s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", removeErr, models.JSON{
					"action": "redeploy",
					"step":   "cleanup_failed_start",
				})
			}
			s.restoreContainerAfterRedeployFailureInternal(ctx, dockerClient, containerID, containerName, backupName, "start", wasRunning, user)
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{
				"action": "redeploy",
				"step":   "start",
				"image":  imageName,
			})
			return "", fmt.Errorf("failed to start new container: %w", err)
		}
	}

	slog.InfoContext(ctx, "container redeployed successfully",
		"oldContainerId", containerID,
		"newContainerId", createResp.ID,
		"containerName", containerName,
		"image", imageName,
	)

	if _, err := dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: false,
		RemoveLinks:   false,
	}); err != nil {
		slog.WarnContext(ctx, "failed to remove old container after successful redeploy",
			"containerId", containerID,
			"backupName", backupName,
			"error", err,
		)
	}

	if logErr := s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDeploy, createResp.ID, containerName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "failed to log deploy event", "err", logErr)
	}

	writeContainerProgressInternal(ctx, "Container redeployed", 100, "complete")
	return createResp.ID, nil
}

func (s *ContainerService) GetContainerByReference(ctx context.Context, ref string) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerInspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, ref, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("container not found: %w", err)
	}

	return new(containerInspect.Container), nil
}

func (s *ContainerService) GetContainerByID(ctx context.Context, id string) (*container.InspectResponse, error) {
	return s.GetContainerByReference(ctx, id)
}

func (s *ContainerService) GetContainerDetails(ctx context.Context, id string) (containertypes.Details, error) {
	containerInspect, err := s.GetContainerByID(ctx, id)
	if err != nil {
		return containertypes.Details{}, err
	}

	details := containertypes.NewDetails(containerInspect)
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	details.RedeployDisabled = libupdater.ShouldDisableArcaneServerRedeploy(details.Labels, details.ID, currentContainerID, currentContainerErr)
	s.applyContainerDetailsIconInternal(ctx, &details)

	return details, nil
}

// GetContainerNameByReference resolves a container's clean name from a Docker ID or name.
func (s *ContainerService) GetContainerNameByReference(ctx context.Context, ref string) (string, error) {
	info, err := s.GetContainerByReference(ctx, ref)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(info.Name, "/"), nil
}

// GetContainerNameByID resolves a container's clean name from its Docker ID.
func (s *ContainerService) GetContainerNameByID(ctx context.Context, id string) (string, error) {
	return s.GetContainerNameByReference(ctx, id)
}

func (s *ContainerService) DeleteContainer(ctx context.Context, containerID string, force bool, removeVolumes bool, user models.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Get container mounts before deletion if we need to remove volumes
	var volumesToRemove []string
	if removeVolumes {
		containerJSON, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		if inspectErr == nil {
			for _, mount := range containerJSON.Container.Mounts {
				// Only collect named volumes (not bind mounts or tmpfs)
				if mount.Type == "volume" && mount.Name != "" {
					volumesToRemove = append(volumesToRemove, mount.Name)
				}
			}
		}
	}

	_, err = dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
		RemoveLinks:   false,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete", "force": force, "removeVolumes": removeVolumes})
		return fmt.Errorf("failed to delete container: %w", err)
	}

	// Remove named volumes if requested
	if removeVolumes && len(volumesToRemove) > 0 {
		for _, volumeName := range volumesToRemove {
			if _, removeErr := dockerClient.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: false}); removeErr != nil {
				// Log but don't fail if volume removal fails (might be in use by another container)
				s.eventService.LogErrorEvent(ctx, models.EventTypeVolumeError, "volume", volumeName, "", user.ID, user.Username, "0", removeErr, models.JSON{"action": "delete", "container": containerID})
			}
		}
	}

	metadata := models.JSON{
		"action":      "delete",
		"containerId": containerID,
	}

	err = s.eventService.LogContainerEvent(ctx, models.EventTypeContainerDelete, containerID, "name", user.ID, user.Username, "0", metadata)
	if err != nil {
		return fmt.Errorf("failed to log action: %w", err)
	}

	return nil
}

func (s *ContainerService) CreateContainer(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, containerName string, user models.User, credentials []containerregistry.Credential) (*container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.ImageInspect(ctx, config.Image)
	if err != nil {
		// Image not found locally, need to pull it
		pullOptions, authErr := s.imageService.getPullOptionsWithAuth(ctx, config.Image, credentials)
		if authErr != nil {
			slog.WarnContext(ctx, "Failed to get registry authentication for container image; proceeding without auth",
				"image", config.Image,
				"error", authErr.Error())
			pullOptions = client.ImagePullOptions{}
		}

		settings := s.settingsService.GetSettingsConfig()
		pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
		defer pullCancel()

		reader, pullErr := dockerClient.ImagePull(pullCtx, config.Image, pullOptions)
		if pullErr != nil {
			if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
				s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, models.JSON{"action": "create", "image": config.Image, "step": "pull_image_timeout"})
				return nil, fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", config.Image)
			}
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", pullErr, models.JSON{"action": "create", "image": config.Image, "step": "pull_image"})
			return nil, fmt.Errorf("failed to pull image %s: %w", config.Image, pullErr)
		}
		defer func() { _ = reader.Close() }()

		streamErr := dockerutils.ConsumeJSONMessageStream(reader, nil)
		if streamErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", streamErr, models.JSON{"action": "create", "image": config.Image, "step": "complete_pull"})
			return nil, fmt.Errorf("failed to complete image pull: %w", streamErr)
		}
	}

	resp, err := libarcane.ContainerCreateWithCompatibility(ctx, dockerClient, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Name:             containerName,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", "", containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "create"})
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	metadata := models.JSON{
		"action":      "create",
		"containerId": resp.ID,
	}

	if logErr := s.eventService.LogContainerEvent(ctx, models.EventTypeContainerCreate, resp.ID, "name", user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log container stop action", "error", logErr)
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "start"})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	containerJSON, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, resp.ID, client.ContainerInspectOptions{})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeContainerError, "container", resp.ID, containerName, user.ID, user.Username, "0", err, models.JSON{"action": "create", "image": config.Image, "step": "inspect"})
		return nil, fmt.Errorf("failed to inspect created container: %w", err)
	}

	return new(containerJSON.Container), nil
}

func (s *ContainerService) StreamStats(ctx context.Context, containerID string, statsChan chan<- any) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	stats, err := dockerClient.ContainerStats(ctx, containerID, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return fmt.Errorf("failed to start stats stream: %w", err)
	}
	defer func() { _ = stats.Body.Close() }()

	decoder := json.NewDecoder(stats.Body)
	historySent := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var statsData container.StatsResponse
		if err := decoder.Decode(&statsData); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to decode stats: %w", err)
		}

		recordedAt := statsData.Read
		if recordedAt.IsZero() {
			recordedAt = time.Now()
		}

		payload := containertypes.StatsStreamPayload{
			StatsResponse:        statsData,
			CurrentHistorySample: containerstats.BuildSample(statsData),
		}
		payload.StatsHistory = s.statsHistory.Record(
			containerID,
			payload.CurrentHistorySample,
			!historySent,
			recordedAt,
		)
		historySent = true

		select {
		case statsChan <- payload:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *ContainerService) StreamLogs(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerInspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect container for logs: %w", err)
	}

	options := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
	}

	logs, err := dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return fmt.Errorf("failed to get container logs: %w", err)
	}
	defer func() { _ = logs.Close() }()

	isTTY := containerInspect.Container.Config != nil && containerInspect.Container.Config.Tty
	return dockerutils.StreamContainerLogs(ctx, logs, logsChan, follow, isTTY)
}

func (s *ContainerService) ListContainersPaginated(
	ctx context.Context,
	params pagination.QueryParams,
	includeAll bool,
	includeInternal bool,
	groupBy string,
) (ContainerListResult, error) {
	var dockerContainers []container.Summary
	if includeAll {
		var err error
		dockerContainers, err = s.dockerService.listContainersInternal(ctx)
		if err != nil {
			return ContainerListResult{}, err
		}
	} else {
		dockerClient, err := s.dockerService.GetClient(ctx)
		if err != nil {
			return ContainerListResult{}, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
		if err != nil {
			return ContainerListResult{}, fmt.Errorf("failed to list Docker containers: %w", err)
		}
		dockerContainers = containerList.Items
	}

	dockerContainers = filterInternalContainers(dockerContainers, includeInternal)
	imageIDs := collectImageIDs(dockerContainers)
	updateInfoMap := s.getUpdateInfoMap(ctx, imageIDs)
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	items := s.buildContainerSummaries(dockerContainers, updateInfoMap, currentContainerID, currentContainerErr)

	config := s.buildContainerPaginationConfig()
	counts := s.calculateContainerStatusCounts(items)

	if groupBy == containerGroupByProject {
		ungroupedParams := params
		ungroupedParams.Start = 0
		ungroupedParams.Limit = -1

		result := pagination.SearchOrderAndPaginate(items, ungroupedParams, config)
		groups, paginationResp := paginateContainerProjectGroupsInternal(result, params)

		// Icons must be resolved before flattening: groups hold value copies,
		// so the flattened items only carry icons applied to the groups first.
		metadataByProject := map[string]projects.ArcaneComposeMetadata{}
		for gi := range groups {
			s.applyContainerSummaryIconsInternal(ctx, groups[gi].Items, metadataByProject)
		}

		return ContainerListResult{
			Items:      flattenContainerProjectGroupsInternal(groups),
			Groups:     groups,
			Pagination: paginationResp,
			Counts:     counts,
		}, nil
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)
	s.applyContainerSummaryIconsInternal(ctx, result.Items, nil)
	paginationResp := pagination.BuildResponseFromFilterResult(result, params)

	return ContainerListResult{
		Items:      result.Items,
		Pagination: paginationResp,
		Counts:     counts,
	}, nil
}

func paginateContainerProjectGroupsInternal(
	result pagination.FilterResult[containertypes.Summary],
	params pagination.QueryParams,
) ([]containertypes.SummaryGroup, pagination.Response) {
	groups := groupContainersByProjectInternal(result.Items)
	totalCount := len(result.Items)

	if params.Limit <= 0 {
		return groups, pagination.Response{
			TotalPages:      1,
			TotalItems:      int64(totalCount),
			CurrentPage:     1,
			ItemsPerPage:    totalCount,
			GrandTotalItems: result.TotalAvailable,
		}
	}

	requestedPage := max((params.Start/params.Limit)+1, 1)

	// Pages are contiguous runs of whole groups: a group is never split, so the
	// group that crosses the limit finishes its page. One walk over group sizes
	// finds the requested page's group range without materializing other pages.
	totalPages := 0
	pageStart, currentCount := 0, 0
	selStart, selEnd := 0, 0
	lastStart, lastEnd := 0, 0

	closePage := func(end int) {
		totalPages++
		if totalPages == requestedPage {
			selStart, selEnd = pageStart, end
		}
		lastStart, lastEnd = pageStart, end
		pageStart, currentCount = end, 0
	}

	for i := range groups {
		currentCount += len(groups[i].Items)
		if currentCount >= params.Limit {
			closePage(i + 1)
		}
	}
	if pageStart < len(groups) || totalPages == 0 {
		closePage(len(groups))
	}

	if requestedPage > totalPages {
		requestedPage = totalPages
		selStart, selEnd = lastStart, lastEnd
	}

	return groups[selStart:selEnd], pagination.Response{
		TotalPages:      int64(totalPages),
		TotalItems:      int64(totalCount),
		CurrentPage:     requestedPage,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: result.TotalAvailable,
	}
}

func groupContainersByProjectInternal(items []containertypes.Summary) []containertypes.SummaryGroup {
	groups := make([]containertypes.SummaryGroup, 0)
	groupIndexes := make(map[string]int)

	for _, item := range items {
		groupName := getContainerProjectNameInternal(item)
		groupIndex, exists := groupIndexes[groupName]
		if !exists {
			groupIndex = len(groups)
			groupIndexes[groupName] = groupIndex
			groups = append(groups, containertypes.SummaryGroup{GroupName: groupName})
		}

		groups[groupIndex].Items = append(groups[groupIndex].Items, item)
	}

	return groups
}

func flattenContainerProjectGroupsInternal(groups []containertypes.SummaryGroup) []containertypes.Summary {
	flattened := make([]containertypes.Summary, 0)
	for _, group := range groups {
		flattened = append(flattened, group.Items...)
	}

	return flattened
}

func getContainerProjectNameInternal(container containertypes.Summary) string {
	if container.Labels == nil {
		return containerNoProjectGroup
	}

	projectName := dockerutils.ComposeProjectLabel(container.Labels)
	if projectName == "" {
		return containerNoProjectGroup
	}

	return projectName
}

func filterInternalContainers(containers []container.Summary, includeInternal bool) []container.Summary {
	if includeInternal {
		return containers
	}

	filtered := make([]container.Summary, 0, len(containers))
	for _, dc := range containers {
		if libarcane.IsInternalContainer(dc.Labels) {
			continue
		}
		filtered = append(filtered, dc)
	}
	return filtered
}

func collectImageIDs(containers []container.Summary) []string {
	imageIDSet := make(map[string]struct{}, len(containers))
	for _, dc := range containers {
		if dc.ImageID != "" {
			imageIDSet[dc.ImageID] = struct{}{}
		}
	}

	imageIDs := make([]string, 0, len(imageIDSet))
	for id := range imageIDSet {
		imageIDs = append(imageIDs, id)
	}
	return imageIDs
}

func (s *ContainerService) getUpdateInfoMap(ctx context.Context, imageIDs []string) map[string]*imagetypes.UpdateInfo {
	if s.imageService == nil || len(imageIDs) == 0 {
		return make(map[string]*imagetypes.UpdateInfo)
	}

	if s.updateInfoCache == nil {
		updateInfoMap, err := s.imageService.GetUpdateInfoByImageIDs(ctx, imageIDs)
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch image update info for containers", "error", err)
			return make(map[string]*imagetypes.UpdateInfo)
		}
		return updateInfoMap
	}

	updateInfoMap := make(map[string]*imagetypes.UpdateInfo, len(imageIDs))
	var uncachedIDs []string
	for _, imageID := range imageIDs {
		info, ok := s.updateInfoCache.Get(imageID)
		if !ok {
			uncachedIDs = append(uncachedIDs, imageID)
			continue
		}
		if info != nil {
			updateInfoMap[imageID] = info
		}
	}

	if len(uncachedIDs) > 0 {
		// Singleflight keyed by the uncached set so concurrent list requests
		// that miss on the same images share one batch query.
		slices.Sort(uncachedIDs)
		res, err, _ := s.updateInfoSF.Do(strings.Join(uncachedIDs, ","), func() (any, error) {
			infos, fetchErr := s.imageService.GetUpdateInfoByImageIDs(ctx, uncachedIDs)
			if fetchErr != nil {
				return nil, fetchErr
			}
			for _, imageID := range uncachedIDs {
				// Cache nil results too so absent rows don't refetch until invalidation.
				s.updateInfoCache.Set(imageID, infos[imageID])
			}
			return infos, nil
		})
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch image update info for container images", "imageIDs", len(uncachedIDs), "error", err)
			return updateInfoMap
		}
		infos, _ := res.(map[string]*imagetypes.UpdateInfo)
		for _, imageID := range uncachedIDs {
			if info := infos[imageID]; info != nil {
				updateInfoMap[imageID] = info
			}
		}
	}
	return updateInfoMap
}

func (s *ContainerService) buildContainerSummaries(containers []container.Summary, updateInfoMap map[string]*imagetypes.UpdateInfo, currentContainerID string, currentContainerErr error) []containertypes.Summary {
	items := make([]containertypes.Summary, 0, len(containers))
	for _, dc := range containers {
		summary := containertypes.NewSummary(dc)
		if info, exists := updateInfoMap[dc.ImageID]; exists {
			summary.UpdateInfo = info
		}
		summary.RedeployDisabled = libupdater.ShouldDisableArcaneServerRedeploy(summary.Labels, summary.ID, currentContainerID, currentContainerErr)
		items = append(items, summary)
	}
	return items
}

// applyContainerSummaryIconsInternal resolves icons for a page of summaries.
// Icon resolution is deferred until after pagination so the cost is bounded by
// page size rather than the full container list.
func (s *ContainerService) applyContainerSummaryIconsInternal(ctx context.Context, summaries []containertypes.Summary, metadataByProject map[string]projects.ArcaneComposeMetadata) {
	if metadataByProject == nil {
		metadataByProject = map[string]projects.ArcaneComposeMetadata{}
	}
	for i := range summaries {
		s.applyContainerSummaryIconInternal(ctx, &summaries[i], metadataByProject)
	}
}

func (s *ContainerService) applyContainerSummaryIconInternal(ctx context.Context, summary *containertypes.Summary, metadataByProject map[string]projects.ArcaneComposeMetadata) {
	if summary == nil {
		return
	}
	resolvedIcon := s.resolveContainerIconInternal(ctx, summary.Labels, metadataByProject)
	summary.IconLightURL = resolvedIcon.IconLightURL
	summary.IconDarkURL = resolvedIcon.IconDarkURL
}

func (s *ContainerService) applyContainerDetailsIconInternal(ctx context.Context, details *containertypes.Details) {
	if details == nil {
		return
	}
	resolvedIcon := s.resolveContainerIconInternal(ctx, details.Labels, nil)
	details.IconLightURL = resolvedIcon.IconLightURL
	details.IconDarkURL = resolvedIcon.IconDarkURL
}

func (s *ContainerService) resolveContainerIconInternal(ctx context.Context, labels map[string]string, metadataByProject map[string]projects.ArcaneComposeMetadata) iconcatalog.ResolvedIconSet {
	explicitIcon := projects.FindArcaneIconSet(labels)
	if !explicitIcon.IsEmpty() {
		return s.resolveIconSetInternal(ctx, explicitIcon)
	}

	projectName := dockerutils.ComposeProjectLabel(labels)
	if projectName == "" || s == nil || s.projectService == nil {
		return s.resolveIconSetInternal(ctx, explicitIcon)
	}

	meta := s.getCachedProjectIconMetadataInternal(ctx, projectName, metadataByProject)

	serviceName := dockerutils.ComposeServiceLabel(labels)
	return s.resolveIconSetInternal(ctx, iconcatalog.FirstNonEmpty(
		explicitIcon,
		meta.ServiceIconSets[serviceName],
		meta.ProjectIcon,
	))
}

func (s *ContainerService) getCachedProjectIconMetadataInternal(ctx context.Context, projectName string, metadataByProject map[string]projects.ArcaneComposeMetadata) projects.ArcaneComposeMetadata {
	if metadataByProject != nil {
		if meta, ok := metadataByProject[projectName]; ok {
			return meta
		}
	}

	if s.iconMetaCache != nil {
		if meta, ok := s.iconMetaCache.Get(projectName); ok {
			if metadataByProject != nil {
				metadataByProject[projectName] = meta
			}
			return meta
		}
	}

	meta := projects.ArcaneComposeMetadata{ServiceIconSets: map[string]projects.IconSet{}}
	proj, err := s.projectService.GetProjectByComposeName(ctx, projectName)
	if err == nil && proj != nil {
		meta = s.projectService.getProjectMetadataForProject(ctx, *proj)
	}
	if s.iconMetaCache != nil {
		s.iconMetaCache.Put(projectName, meta)
	}
	if metadataByProject != nil {
		metadataByProject[projectName] = meta
	}
	return meta
}

func (s *ContainerService) resolveIconSetInternal(ctx context.Context, iconSet iconcatalog.IconSet) iconcatalog.ResolvedIconSet {
	catalog := iconcatalog.DefaultCatalog
	if s != nil && s.settingsService != nil {
		catalog = s.settingsService.GetStringSetting(ctx, "iconCatalog", iconcatalog.DefaultCatalog)
	}
	return iconcatalog.Resolve(catalog, iconSet)
}

func (s *ContainerService) buildContainerPaginationConfig() pagination.Config[containertypes.Summary] {
	return pagination.Config[containertypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[containertypes.Summary]{
			func(c containertypes.Summary) (string, error) {
				if len(c.Names) > 0 {
					return c.Names[0], nil
				}
				return "", nil
			},
			func(c containertypes.Summary) (string, error) { return c.Image, nil },
			func(c containertypes.Summary) (string, error) { return c.State, nil },
			func(c containertypes.Summary) (string, error) { return c.Status, nil },
		},
		SortBindings:    s.buildContainerSortBindings(),
		FilterAccessors: s.buildContainerFilterAccessors(),
	}
}

func (s *ContainerService) buildContainerSortBindings() []pagination.SortBinding[containertypes.Summary] {
	return []pagination.SortBinding[containertypes.Summary]{
		{
			Key: "name",
			Fn: func(a, b containertypes.Summary) int {
				nameA, nameB := "", ""
				if len(a.Names) > 0 {
					nameA = a.Names[0]
				}
				if len(b.Names) > 0 {
					nameB = b.Names[0]
				}
				return strings.Compare(nameA, nameB)
			},
		},
		{
			Key: "image",
			Fn: func(a, b containertypes.Summary) int {
				return strings.Compare(a.Image, b.Image)
			},
		},
		{
			Key: "state",
			Fn: func(a, b containertypes.Summary) int {
				return strings.Compare(a.State, b.State)
			},
		},
		{
			Key: "status",
			Fn: func(a, b containertypes.Summary) int {
				return strings.Compare(a.Status, b.Status)
			},
		},
		{
			Key:    "ports",
			Fn:     compareContainerPortsForSortInternal,
			DescFn: compareContainerPortsForSortDescInternal,
		},
		{
			Key: "created",
			Fn: func(a, b containertypes.Summary) int {
				if a.Created < b.Created {
					return -1
				}
				if a.Created > b.Created {
					return 1
				}
				return 0
			},
		},
	}
}

func compareContainerPortsForSortInternal(a, b containertypes.Summary) int {
	hasPortsA, portA := lowestContainerPortSortValueInternal(a.Ports)
	hasPortsB, portB := lowestContainerPortSortValueInternal(b.Ports)

	switch {
	case !hasPortsA && !hasPortsB:
		return compareContainerNamesForSortInternal(a, b)
	case !hasPortsA:
		return 1
	case !hasPortsB:
		return -1
	case portA < portB:
		return -1
	case portA > portB:
		return 1
	default:
		return compareContainerNamesForSortInternal(a, b)
	}
}

func compareContainerPortsForSortDescInternal(a, b containertypes.Summary) int {
	hasPortsA, portA := lowestContainerPortSortValueInternal(a.Ports)
	hasPortsB, portB := lowestContainerPortSortValueInternal(b.Ports)

	switch {
	case !hasPortsA && !hasPortsB:
		return compareContainerNamesForSortInternal(a, b)
	case !hasPortsA:
		return 1
	case !hasPortsB:
		return -1
	case portA > portB:
		return -1
	case portA < portB:
		return 1
	default:
		return compareContainerNamesForSortInternal(a, b)
	}
}

func lowestContainerPortSortValueInternal(ports []containertypes.Port) (bool, int) {
	if len(ports) == 0 {
		return false, 0
	}

	lowestPublished := 0
	lowestPrivate := 0
	for _, port := range ports {
		if port.PublicPort > 0 && (lowestPublished == 0 || port.PublicPort < lowestPublished) {
			lowestPublished = port.PublicPort
		}
		if port.PrivatePort > 0 && (lowestPrivate == 0 || port.PrivatePort < lowestPrivate) {
			lowestPrivate = port.PrivatePort
		}
	}

	switch {
	case lowestPublished > 0:
		return true, lowestPublished
	case lowestPrivate > 0:
		return true, lowestPrivate
	default:
		return false, 0
	}
}

func compareContainerNamesForSortInternal(a, b containertypes.Summary) int {
	nameA, nameB := "", ""
	if len(a.Names) > 0 {
		nameA = a.Names[0]
	}
	if len(b.Names) > 0 {
		nameB = b.Names[0]
	}
	return strings.Compare(nameA, nameB)
}

func (s *ContainerService) buildContainerFilterAccessors() []pagination.FilterAccessor[containertypes.Summary] {
	return []pagination.FilterAccessor[containertypes.Summary]{
		{
			Key: "updates",
			Fn: func(c containertypes.Summary, filterValue string) bool {
				switch filterValue {
				case "has_update":
					return c.UpdateInfo != nil && c.UpdateInfo.HasUpdate
				case "up_to_date":
					return c.UpdateInfo != nil && !c.UpdateInfo.HasUpdate && c.UpdateInfo.Error == ""
				case "error":
					return c.UpdateInfo != nil && c.UpdateInfo.Error != ""
				case "unknown":
					return c.UpdateInfo == nil
				default:
					return true
				}
			},
		},
		{
			Key: "standalone",
			Fn: func(c containertypes.Summary, filterValue string) bool {
				isStandalone := dockerutils.ComposeProjectLabel(c.Labels) == ""
				switch filterValue {
				case "true", "1":
					return isStandalone
				case "false", "0":
					return !isStandalone
				default:
					return true
				}
			},
		},
	}
}

func (s *ContainerService) calculateContainerStatusCounts(items []containertypes.Summary) containertypes.StatusCounts {
	counts := containertypes.StatusCounts{
		TotalContainers: len(items),
	}
	for _, c := range items {
		if c.State == "running" {
			counts.RunningContainers++
		} else {
			counts.StoppedContainers++
		}
	}
	return counts
}

// CreateExec creates an exec instance in the container
func (s *ContainerService) CreateExec(ctx context.Context, containerID string, cmd []string) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	execConfig := client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
		Cmd:          cmd,
	}

	execResp, err := dockerClient.ExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	return execResp.ID, nil
}

// ExecSession manages the lifecycle of a Docker exec session.
type ExecSession struct {
	execID       string
	containerID  string
	hijackedResp client.HijackedResponse
	dockerClient *client.Client
	closeOnce    sync.Once
}

func (e *ExecSession) Stdin() io.WriteCloser { return e.hijackedResp.Conn }
func (e *ExecSession) Stdout() io.Reader     { return e.hijackedResp.Reader }

// Close terminates the exec session and kills the process if still running.
func (e *ExecSession) Close(ctx context.Context) error {
	var closeErr error
	e.closeOnce.Do(func() {
		slog.Debug("Closing exec session", "execID", e.execID, "containerID", e.containerID)

		// Send EOF (Ctrl-D) then exit to terminate the shell gracefully.
		_, _ = e.hijackedResp.Conn.Write([]byte{0x04})
		time.Sleep(50 * time.Millisecond)
		_, _ = e.hijackedResp.Conn.Write([]byte("exit\n"))
		time.Sleep(100 * time.Millisecond)

		e.hijackedResp.Close()
	})

	return closeErr
}

// AttachExec attaches to an exec instance and returns an ExecSession for lifecycle management.
func (s *ContainerService) AttachExec(ctx context.Context, containerID, execID string) (*ExecSession, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	execAttach, err := dockerClient.ExecAttach(ctx, execID, client.ExecAttachOptions{
		TTY: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}

	return &ExecSession{
		execID:       execID,
		containerID:  containerID,
		hijackedResp: execAttach.HijackedResponse,
		dockerClient: dockerClient,
	}, nil
}
