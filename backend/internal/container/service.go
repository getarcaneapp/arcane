package container

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/docker/types"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container/children/stats"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
)

const (
	containerGroupByProject  = "project"
	containerNoProjectGroup  = "No Project"
	containerIconMetadataTTL = 5 * time.Second
	// leaseWaitTimeout bounds how long a container operation waits for one already running on the same container.
	leaseWaitTimeout = 30 * time.Second
	// rollbackTimeout bounds a redeploy rollback, which runs detached from the request so it completes after cancellation.
	rollbackTimeout = 2 * timeouts.DefaultDockerAPI
	// redeployBackupInfix joins a container's name and a Unix timestamp to name the original kept during a redeploy.
	redeployBackupInfix = "-arcane-redeploy-"
)

type ContainerService struct {
	dockerService   *dockerInternal.DockerClientService
	eventService    *event.EventService
	imageService    *image.ImageService
	settingsService *settings.SettingsService
	projectService  *project.ProjectService
	kvService       *kv.KVService
	iconMetaCache   *hot.HotCache[string, projects.ArcaneComposeMetadata]
	stats           *stats.Service
	// opLock serializes mutating operations on the same container.
	opLock utils.KeyedMutex
}

type ContainerListResult struct {
	Items      []containertypes.Summary
	Groups     []containertypes.SummaryGroup
	Pagination pagination.Response
	Counts     containertypes.StatusCounts
}

type containerLifecycleAction struct {
	action             string
	eventType          event.EventType
	metadata           database.JSON
	runContainerAction func(context.Context) error
}

type ExecSession struct {
	execID       string
	containerID  string
	hijackedResp client.HijackedResponse
	dockerClient *client.Client
	closeOnce    sync.Once
}

type demuxedLogs struct {
	io.Reader

	pipe *io.PipeReader
	logs io.ReadCloser
}

func NewContainerService(
	eventService *event.EventService,
	dockerService *dockerInternal.DockerClientService,
	imageService *image.ImageService,
	settingsService *settings.SettingsService,
	projectService *project.ProjectService,
	kvService *kv.KVService,
) *ContainerService {
	return &ContainerService{
		eventService:    eventService,
		dockerService:   dockerService,
		imageService:    imageService,
		settingsService: settingsService,
		projectService:  projectService,
		kvService:       kvService,
		iconMetaCache: hot.NewHotCache[string, projects.ArcaneComposeMetadata](hot.LRU, 1024).
			WithTTL(containerIconMetadataTTL).
			WithJanitor().
			Build(),
		stats: stats.New(dockerService.GetClient, dockerService.DockerHost, stats.ContainerResourceSampleTTL, 0, 0),
	}
}

func buildCleanNetworkingConfig(containerInspect container.InspectResponse, apiVersion string) *network.NetworkingConfig {
	if len(kit.FromPtr(containerInspect.NetworkSettings).Networks) == 0 {
		return nil
	}

	endpointsConfig := map[string]*network.EndpointSettings{}
	for networkName, endpoint := range compat.SanitizeContainerCreateEndpointSettingsForDockerAPI(containerInspect.NetworkSettings.Networks, apiVersion) {
		if endpoint == nil {
			endpointsConfig[networkName] = nil
			continue
		}

		endpointsConfig[networkName] = &network.EndpointSettings{ //exhaustruct:enforce
			IPAMConfig:          nil,
			Links:               endpoint.Links,
			Aliases:             endpoint.Aliases,
			DriverOpts:          endpoint.DriverOpts,
			GwPriority:          endpoint.GwPriority,
			NetworkID:           endpoint.NetworkID,
			EndpointID:          endpoint.EndpointID,
			Gateway:             endpoint.Gateway,
			IPAddress:           endpoint.IPAddress,
			MacAddress:          endpoint.MacAddress,
			IPPrefixLen:         endpoint.IPPrefixLen,
			IPv6Gateway:         endpoint.IPv6Gateway,
			GlobalIPv6Address:   endpoint.GlobalIPv6Address,
			GlobalIPv6PrefixLen: endpoint.GlobalIPv6PrefixLen,
			DNSNames:            endpoint.DNSNames,
		}
	}

	if len(endpointsConfig) == 0 {
		return nil
	}

	return &network.NetworkingConfig{
		EndpointsConfig: endpointsConfig,
	}
}

func (s *ContainerService) pullRedeployImage(
	ctx context.Context,
	imageName, containerID, containerName, action string,
	credentials []containerregistry.Credential,
	user usertypes.Actor,
) error {
	progressWriter, _ := ctx.Value(types.ProgressWriterKey{}).(io.Writer)
	pullErr := s.imageService.Pull(ctx, image.PullRequest{
		Ref:         imageName,
		Progress:    progressWriter,
		User:        user,
		Credentials: credentials,
	})
	if pullErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", pullErr, database.JSON{
			"action": action,
			"step":   kit.Ternary(errors.Is(pullErr, context.DeadlineExceeded), "pull_image_timeout", "pull_image"),
			"image":  imageName,
		})
	}
	return pullErr
}

// RecoverInterruptedRedeploys resolves journaled redeploy backups a restart left behind: a backup whose replacement
// is running is removed, and a backup with no replacement gets its original name back but stays stopped.
func (s *ContainerService) RecoverInterruptedRedeploys(ctx context.Context) error {
	entries, err := s.kvService.ListByPrefix(ctx, redeployJournalKeyPrefix)
	if err != nil {
		return fmt.Errorf("list redeploy journals: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	list, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
		return dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	})
	if err != nil {
		return fmt.Errorf("list containers for redeploy recovery: %w", err)
	}

	byName := make(map[string]container.Summary, len(list.Items))
	for _, c := range list.Items {
		for _, name := range c.Names {
			byName[strings.TrimPrefix(name, "/")] = c
		}
	}

	var firstErr error
	var removed, restored, skipped int
	for _, entry := range entries {
		var journal RedeployJournal
		if unmarshalErr := json.Unmarshal([]byte(entry.Value), &journal); unmarshalErr != nil {
			slog.WarnContext(ctx, "Failed to decode redeploy journal", "key", entry.Key, "error", unmarshalErr)
			continue
		}

		backup, backupExists := byName[journal.BackupName]
		if !backupExists {
			s.clearRedeployJournal(ctx, journal.OriginalID)
			continue
		}
		if backup.ID != journal.OriginalID {
			slog.WarnContext(ctx, "Skipping redeploy backup recovery: container is not the one Arcane renamed", "container", journal.BackupName, "id", backup.ID, "journaledId", journal.OriginalID)
			s.clearRedeployJournal(ctx, journal.OriginalID)
			skipped++
			continue
		}

		replacement, exists := byName[cmp.Or(journal.NewName, journal.OriginalName)]
		switch {
		case exists && replacement.State == container.StateRunning:
			if _, removeErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
				return dockerClient.ContainerRemove(ctx, backup.ID, client.ContainerRemoveOptions{Force: true})
			}); removeErr != nil {
				slog.WarnContext(ctx, "Failed to remove leftover redeploy backup", "container", journal.BackupName, "error", removeErr)
				firstErr = cmp.Or(firstErr, fmt.Errorf("remove redeploy backup %s: %w", journal.BackupName, removeErr))
				continue
			}
			s.clearRedeployJournal(ctx, journal.OriginalID)
			removed++
		case exists:
			slog.WarnContext(ctx, "Leaving redeploy backup in place: replacement exists but is not running",
				"container", journal.BackupName,
				"replacement", cmp.Or(journal.NewName, journal.OriginalName),
				"state", replacement.State)
			skipped++
		default:
			if _, renameErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRenameResult, error) {
				return dockerClient.ContainerRename(ctx, backup.ID, client.ContainerRenameOptions{NewName: journal.OriginalName})
			}); renameErr != nil {
				slog.WarnContext(ctx, "Failed to restore leftover redeploy backup", "container", journal.BackupName, "error", renameErr)
				firstErr = cmp.Or(firstErr, fmt.Errorf("restore redeploy backup %s: %w", journal.BackupName, renameErr))
				continue
			}
			s.clearRedeployJournal(ctx, journal.OriginalID)
			byName[journal.OriginalName] = backup
			restored++
			slog.WarnContext(ctx, "Restored original container from interrupted redeploy; start it manually if it should be running", "container", journal.OriginalName, "state", backup.State)
		}
	}

	if removed+restored+skipped > 0 {
		slog.InfoContext(ctx, "Recovered interrupted container redeploys", "removed", removed, "restored", restored, "skipped", skipped)
	}
	return firstErr
}

// clearRedeployJournal drops the journal once its backup is resolved; a leftover entry is retried at startup.
func (s *ContainerService) clearRedeployJournal(ctx context.Context, originalID string) {
	if err := s.kvService.Delete(ctx, redeployJournalKeyPrefix+originalID); err != nil {
		slog.WarnContext(ctx, "Failed to clear redeploy journal", "containerId", originalID, "error", err)
	}
}

func (s *ContainerService) restoreContainerAfterRedeployFailure(
	ctx context.Context,
	containerID, containerName, backupName, failedStep string,
	wasRunning bool,
	user usertypes.Actor,
) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()

	s.eventService.MarkDockerExpectation("container", containerID, containerName)
	if wasRunning {
		if _, startErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
			return dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
		}); startErr != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", startErr, database.JSON{
				"action":     "redeploy",
				"step":       "restore_start_original",
				"failedStep": failedStep,
			})
		}
	}

	if containerName == "" {
		return
	}

	if _, renameErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRenameResult, error) {
		return dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: containerName})
	}); renameErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, backupName, user.ID, user.Username, "0", renameErr, database.JSON{
			"action":     "redeploy",
			"step":       "restore_name",
			"failedStep": failedStep,
		})
		return
	}
	s.clearRedeployJournal(ctx, containerID)
}

// renameToRedeployBackup journals the original before renaming it to its backup name, so startup
// recovery only acts on backups Arcane created.
func (s *ContainerService) renameToRedeployBackup(ctx context.Context, containerID, containerName, backupName, newName string, user usertypes.Actor) error {
	journal, err := json.Marshal(RedeployJournal{
		OriginalID:   containerID,
		OriginalName: containerName,
		BackupName:   backupName,
		NewName:      newName,
		StartedAt:    time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("marshal redeploy journal: %w", err)
	}
	if setErr := s.kvService.Set(ctx, redeployJournalKeyPrefix+containerID, string(journal)); setErr != nil {
		return fmt.Errorf("write redeploy journal: %w", setErr)
	}

	if _, renameErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRenameResult, error) {
		return dockerClient.ContainerRename(ctx, containerID, client.ContainerRenameOptions{NewName: backupName})
	}); renameErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", renameErr, database.JSON{
			"action":     "redeploy",
			"step":       "rename_old",
			"backupName": backupName,
		})
		s.clearRedeployJournal(context.WithoutCancel(ctx), containerID)
		return fmt.Errorf("failed to rename existing container: %w", renameErr)
	}
	return nil
}

func (s *ContainerService) stopContainer(ctx context.Context, containerID string) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	_, err = dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{})
	return err
}

// lockContainer takes the container's operation lease, waiting up to leaseWaitTimeout for an operation already running on it.
func (s *ContainerService) lockContainer(ctx context.Context, op, containerID string) (func(), error) {
	waitCtx, cancel := context.WithTimeout(ctx, leaseWaitTimeout)
	defer cancel()
	unlock, err := s.opLock.LockContext(waitCtx, "0:container:"+containerID)
	if err != nil {
		return nil, fmt.Errorf("%s %s: waiting for in-progress operation: %w", op, containerID, err)
	}
	return unlock, nil
}

func (s *ContainerService) runContainerLifecycleAction(ctx context.Context, containerID string, user usertypes.Actor, cfg containerLifecycleAction) error {
	unlock, err := s.lockContainer(ctx, cfg.action+" container", containerID)
	if err != nil {
		return err
	}
	defer unlock()

	metadata := database.JSON{
		"action":      cfg.action,
		"containerId": containerID,
	}
	maps.Copy(metadata, cfg.metadata)

	if logErr := s.eventService.LogContainerEvent(ctx, cfg.eventType, containerID, "name", user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log container action", "action", cfg.action, "error", logErr)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", containerID, "")()
	err = cfg.runContainerAction(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": cfg.action})
	}
	return err
}

func (s *ContainerService) StartContainer(ctx context.Context, containerID string, user usertypes.Actor) error {
	return s.runContainerLifecycleAction(ctx, containerID, user, containerLifecycleAction{
		action:    "start",
		eventType: event.EventTypeContainerStart,
		runContainerAction: func(ctx context.Context) error {
			_, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
				return dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
			})
			return err
		},
	})
}

func (s *ContainerService) StopContainer(ctx context.Context, containerID string, user usertypes.Actor) error {
	return s.runContainerLifecycleAction(ctx, containerID, user, containerLifecycleAction{
		action:    "stop",
		eventType: event.EventTypeContainerStop,
		runContainerAction: func(ctx context.Context) error {
			return s.stopContainer(ctx, containerID)
		},
	})
}

func (s *ContainerService) RestartContainer(ctx context.Context, containerID string, user usertypes.Actor) error {
	return s.runContainerLifecycleAction(ctx, containerID, user, containerLifecycleAction{
		action:    "restart",
		eventType: event.EventTypeContainerRestart,
		runContainerAction: func(ctx context.Context) error {
			dockerClient, err := s.dockerService.GetClient(ctx)
			if err != nil {
				return fmt.Errorf("failed to connect to Docker: %w", err)
			}
			_, err = dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{})
			return err
		},
	})
}

// KillContainer sends a signal to the container's main process (default SIGKILL
// when signal is empty) without removing the container.
func (s *ContainerService) KillContainer(ctx context.Context, containerID, signal string, user usertypes.Actor) error {
	return s.runContainerLifecycleAction(ctx, containerID, user, containerLifecycleAction{
		action:    "kill",
		eventType: event.EventTypeContainerKill,
		metadata:  database.JSON{"signal": signal},
		runContainerAction: func(ctx context.Context) error {
			_, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerKillResult, error) {
				return dockerClient.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: signal})
			})
			return err
		},
	})
}

// PauseContainer suspends all processes in the container.
func (s *ContainerService) PauseContainer(ctx context.Context, containerID string, user usertypes.Actor) error {
	return s.runContainerLifecycleAction(ctx, containerID, user, containerLifecycleAction{
		action:    "pause",
		eventType: event.EventTypeContainerPause,
		runContainerAction: func(ctx context.Context) error {
			_, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerPauseResult, error) {
				return dockerClient.ContainerPause(ctx, containerID, client.ContainerPauseOptions{})
			})
			return err
		},
	})
}

// UnpauseContainer resumes a previously paused container.
func (s *ContainerService) UnpauseContainer(ctx context.Context, containerID string, user usertypes.Actor) error {
	return s.runContainerLifecycleAction(ctx, containerID, user, containerLifecycleAction{
		action:    "unpause",
		eventType: event.EventTypeContainerUnpause,
		runContainerAction: func(ctx context.Context) error {
			_, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerUnpauseResult, error) {
				return dockerClient.ContainerUnpause(ctx, containerID, client.ContainerUnpauseOptions{})
			})
			return err
		},
	})
}

// CommitContainer creates an image from a container's current filesystem.
func (s *ContainerService) CommitContainer(ctx context.Context, containerID string, req containertypes.CommitRequest, user usertypes.Actor) (*containertypes.CommitResult, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return nil, errors.New("container ID is required")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{"action": "commit"})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	repository := strings.TrimSpace(req.Repository)
	tag := strings.TrimSpace(req.Tag)
	reference := kit.Ternary(repository != "" && tag != "", repository+":"+tag, repository)

	result, err := dockerClient.ContainerCommit(ctx, containerID, client.ContainerCommitOptions{
		Reference: reference,
		Comment:   strings.TrimSpace(req.Comment),
		Author:    strings.TrimSpace(req.Author),
		Changes:   req.Changes,
		NoPause:   req.NoPause,
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "container", containerID, reference, user.ID, user.Username, "0", err, database.JSON{"action": "commit", "reference": reference})
		return nil, fmt.Errorf("failed to commit container: %w", err)
	}

	if logErr := s.eventService.LogImageEvent(ctx, event.EventTypeImageCommit, containerID, reference, user.ID, user.Username, "0", database.JSON{
		"action":      "commit",
		"containerId": containerID,
		"imageId":     result.ID,
		"repository":  repository,
		"tag":         tag,
		"reference":   reference,
		"noPause":     req.NoPause,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log container commit action", "container", containerID, "image", result.ID, "error", logErr)
	}

	return &containertypes.CommitResult{ID: result.ID}, nil
}

func (s *ContainerService) RedeployContainer(ctx context.Context, containerID string, user usertypes.Actor) (string, error) {
	unlock, err := s.lockContainer(ctx, "redeploy container", containerID)
	if err != nil {
		return "", err
	}
	defer unlock()

	containerJSON, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "inspect",
		})
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	containerInfo := containerJSON.Container
	if containerInfo.Config == nil {
		err = errors.New("container config is nil")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "validate_config",
		})
		return "", fmt.Errorf("failed to redeploy container: %w", err)
	}

	containerName := strings.TrimPrefix(containerInfo.Name, "/")
	apiVersion, _ := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (string, error) {
		return compat.DetectDockerAPIVersion(ctx, dockerClient), nil
	})

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	if labels.ShouldDisableArcaneServerRedeploy(containerInfo.Config.Labels, containerInfo.ID, currentContainerID, currentContainerErr) {
		err = errors.New("arcane cannot redeploy itself; use the system upgrade flow (Settings -> Updates) instead")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": "redeploy",
			"step":   "self_redeploy_blocked",
		})
		return "", err
	}

	// A container in a known compose project redeploys through project.ProjectService.UpdateProjectServices so compose
	// edits and include/env-file context are honored; the standalone path below only clones the daemon's config.
	if projectName, serviceName := docker.ComposeProjectLabel(containerInfo.Config.Labels), docker.ComposeServiceLabel(containerInfo.Config.Labels); projectName != "" && serviceName != "" {
		proj, projectErr := s.projectService.GetProjectByComposeName(ctx, projectName)
		// Real DB errors surface so a transient failure doesn't silently recreate from stale cached config.
		if projectErr != nil && !errors.Is(projectErr, common.ErrNotFound) {
			return "", fmt.Errorf("failed to look up compose project %s: %w", projectName, projectErr)
		}
		if projectErr == nil {
			slog.InfoContext(
				ctx, "RedeployContainer: detected compose container, using project-based redeploy",
				"containerId", containerID,
				"project", projectName,
				"service", serviceName,
			)

			if updateProjectServicesErr := s.projectService.UpdateProjectServices(ctx, proj.ID, []string{serviceName}, user, false); updateProjectServicesErr != nil {
				s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", updateProjectServicesErr, database.JSON{
					"action":      "redeploy",
					"step":        "compose_update_services",
					"project":     projectName,
					"service":     serviceName,
					"projectId":   proj.ID,
					"projectName": proj.Name,
				})
				return "", fmt.Errorf("compose redeploy failed for %s/%s: %w", projectName, serviceName, updateProjectServicesErr)
			}

			newID, lookupErr := projects.FindComposeServiceContainerID(ctx, s.dockerService.DockerHost(), projectName, serviceName)
			if lookupErr != nil {
				slog.WarnContext(ctx, "failed to resolve container via compose ps after redeploy", "project", projectName, "service", serviceName, "err", lookupErr)
			}

			// An unlocated replacement falls back to the original ID so the handler can degrade gracefully.
			if logErr := s.eventService.LogContainerEvent(ctx, event.EventTypeContainerDeploy, cmp.Or(newID, containerID), containerName, user.ID, user.Username, "0", database.JSON{
				"action":        "redeploy",
				"containerId":   cmp.Or(newID, containerID),
				"containerName": containerName,
				"project":       projectName,
				"service":       serviceName,
				"projectId":     proj.ID,
				"via":           "compose",
			}); logErr != nil {
				slog.WarnContext(ctx, "failed to log compose redeploy event", "err", logErr)
			}

			return cmp.Or(newID, containerID), nil
		}

		slog.WarnContext(
			ctx, "RedeployContainer: compose project not registered, falling back to standalone redeploy",
			"containerId", containerID,
			"project", projectName,
			"service", serviceName,
		)
	}

	if containerInfo.Config.Image != "" {
		if pullRedeployImageErr := s.pullRedeployImage(ctx, containerInfo.Config.Image, containerID, containerName, "redeploy", nil, user); pullRedeployImageErr != nil {
			return "", pullRedeployImageErr
		}
	}

	return s.recreateContainer(
		ctx,
		containerInfo,
		"redeploy",
		containerName,
		new(*containerInfo.Config),
		containerInfo.HostConfig,
		buildCleanNetworkingConfig(containerInfo, apiVersion),
		apiVersion,
		event.EventTypeContainerDeploy,
		user,
	)
}

// recreateContainer replaces an existing container with one created from the supplied config, using
// rename-as-backup so the original can be restored when create or start fails. Shared by redeploy and edit.
func (s *ContainerService) recreateContainer(
	ctx context.Context,
	containerInfo container.InspectResponse,
	action, newName string,
	newConfig *container.Config,
	newHostConfig *container.HostConfig,
	networkingConfig *network.NetworkingConfig,
	apiVersion string,
	eventType event.EventType,
	user usertypes.Actor,
) (string, error) {
	containerName := strings.TrimPrefix(containerInfo.Name, "/")
	wasRunning := kit.FromPtr(containerInfo.State).Running

	// A running auto-remove container is deleted by the daemon the moment it stops, so its stop is
	// deferred until the replacement exists; only a start failure can still lose the original.
	stopAfterCreate := wasRunning && kit.FromPtr(containerInfo.HostConfig).AutoRemove

	var err error
	restoreHostConfig := kit.FromPtr(containerInfo.HostConfig)
	restoreHostConfig.Binds, restoreHostConfig.Mounts, err = docker.PreserveVolumeMounts(ctx, restoreHostConfig.Binds, restoreHostConfig.Mounts, containerInfo.Mounts)
	if err != nil {
		return "", fmt.Errorf("preserve volumes of container %s: %w", containerName, err)
	}
	createHostConfig := kit.FromPtr(newHostConfig)
	createHostConfig.Binds, createHostConfig.Mounts, err = docker.PreserveVolumeMounts(
		ctx,
		createHostConfig.Binds,
		createHostConfig.Mounts,
		retainedVolumeMountPoints(kit.FromPtr(containerInfo.HostConfig), createHostConfig, containerInfo.Mounts),
	)
	if err != nil {
		return "", fmt.Errorf("preserve volumes of container %s: %w", containerName, err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", containerInfo.ID, containerName)()
	backupName := fmt.Sprintf("%s%s%d", cmp.Or(containerName, "arcane-redeploy-"+containerInfo.ID[:min(12, len(containerInfo.ID))]), redeployBackupInfix, time.Now().Unix())

	// Stopping the original made the daemon delete it, so rebuild it from the inspected config.
	// A returned error means the workload is down and is joined into the start error.
	restoreAutoRemoveOriginal := func(ctx context.Context) error {
		originalConfig := *containerInfo.Config
		if len(containerInfo.ID) >= 12 && originalConfig.Hostname == containerInfo.ID[:12] {
			originalConfig.Hostname = "" // keep: copying Docker's 24-field container.Config to clear one field is unreadable
		}

		defer s.eventService.BeginDockerResourceSuppressionWindow("container", "", containerName)()
		restoreResp, restoreCreateErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
			return compat.ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, client.ContainerCreateOptions{
				Config:           &originalConfig,
				HostConfig:       &restoreHostConfig,
				NetworkingConfig: buildCleanNetworkingConfig(containerInfo, apiVersion),
				Name:             containerName,
			}, apiVersion)
		})
		if restoreCreateErr != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerInfo.ID, containerName, user.ID, user.Username, "0", restoreCreateErr, database.JSON{
				"action": "redeploy",
				"step":   "restore_create_original",
			})
			return fmt.Errorf("failed to recreate original auto-remove container: %w", restoreCreateErr)
		}

		defer s.eventService.BeginDockerResourceSuppressionWindow("container", restoreResp.ID, containerName)()
		if _, restoreStartErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
			return dockerClient.ContainerStart(ctx, restoreResp.ID, client.ContainerStartOptions{})
		}); restoreStartErr != nil {
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", restoreResp.ID, containerName, user.ID, user.Username, "0", restoreStartErr, database.JSON{
				"action": "redeploy",
				"step":   "restore_start_original",
			})
			return fmt.Errorf("failed to restart original auto-remove container: %w", restoreStartErr)
		}

		slog.InfoContext(
			ctx, "restored auto-remove container after failed recreate",
			"oldContainerId", containerInfo.ID,
			"restoredContainerId", restoreResp.ID,
			"containerName", containerName,
		)
		return nil
	}

	s.eventService.MarkDockerExpectation("container", containerInfo.ID, containerName)
	if containerName != "" {
		if renameErr := s.renameToRedeployBackup(ctx, containerInfo.ID, containerName, backupName, newName, user); renameErr != nil {
			return "", renameErr
		}
	}
	if wasRunning && !stopAfterCreate {
		if stopErr := s.stopContainer(ctx, containerInfo.ID); stopErr != nil {
			s.restoreContainerAfterRedeployFailure(ctx, containerInfo.ID, containerName, backupName, "stop", false, user)
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerInfo.ID, containerName, user.ID, user.Username, "0", stopErr, database.JSON{
				"action": "redeploy",
				"step":   "stop",
			})
			return "", fmt.Errorf("failed to stop container: %w", stopErr)
		}
	}

	if len(containerInfo.ID) >= 12 && newConfig.Hostname == containerInfo.ID[:12] {
		newConfig.Hostname = "" // keep: copying Docker's 24-field container.Config to clear one field is unreadable
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", "", newName)()
	createResp, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return compat.ContainerCreateWithCompatibilityForAPIVersion(ctx, dockerClient, client.ContainerCreateOptions{
			Config:           newConfig,
			HostConfig:       &createHostConfig,
			NetworkingConfig: networkingConfig,
			Name:             newName,
		}, apiVersion)
	})
	if err != nil {
		s.restoreContainerAfterRedeployFailure(ctx, containerInfo.ID, containerName, backupName, "create", wasRunning && !stopAfterCreate, user)
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerInfo.ID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": action,
			"step":   "create",
			"image":  newConfig.Image,
		})
		return "", fmt.Errorf("failed to recreate container: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", createResp.ID, newName)()

	if stopAfterCreate {
		if containerStopErr := s.stopContainer(ctx, containerInfo.ID); containerStopErr != nil {
			if _, removeErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
				return dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true})
			}); removeErr != nil {
				s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", removeErr, database.JSON{
					"action": action,
					"step":   "cleanup_failed_stop",
				})
			}
			s.restoreContainerAfterRedeployFailure(ctx, containerInfo.ID, containerName, backupName, "stop", false, user)
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerInfo.ID, containerName, user.ID, user.Username, "0", containerStopErr, database.JSON{
				"action": action,
				"step":   "stop",
			})
			return "", fmt.Errorf("failed to stop container: %w", containerStopErr)
		}
	}

	if wasRunning {
		if _, err = s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
			return dockerClient.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{})
		}); err != nil {
			// Remove the replacement and roll back to the original. A deferred-stop auto-remove original
			// is rebuilt from its inspected config; a failed restore joins the start error.
			rollbackCtx, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
			defer cancelRollback()
			s.eventService.MarkDockerExpectation("container", createResp.ID, containerName)
			_, removeErr := s.dockerService.Call(rollbackCtx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
				return dockerClient.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true})
			})
			if removeErr != nil {
				s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", removeErr, database.JSON{
					"action": action,
					"step":   "cleanup_failed_start",
				})
			}
			if stopAfterCreate {
				if restoreErr := restoreAutoRemoveOriginal(rollbackCtx); restoreErr != nil {
					err = errors.Join(err, kit.Ternary(removeErr != nil, fmt.Errorf("failed to remove replacement container: %w", removeErr), nil), restoreErr)
				}
				s.clearRedeployJournal(rollbackCtx, containerInfo.ID)
			} else {
				s.restoreContainerAfterRedeployFailure(rollbackCtx, containerInfo.ID, containerName, backupName, "start", wasRunning, user)
			}
			s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", createResp.ID, containerName, user.ID, user.Username, "0", err, database.JSON{
				"action": action,
				"step":   "start",
				"image":  newConfig.Image,
			})
			return "", fmt.Errorf("failed to start new container: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "container recreated successfully",
		"action", action,
		"oldContainerId", containerInfo.ID,
		"newContainerId", createResp.ID,
		"containerName", newName,
		"image", newConfig.Image,
	)

	// After a deferred stop the daemon has already auto-removed the original. A failed removal
	// keeps the journal so startup recovery removes the backup.
	if stopAfterCreate {
		s.clearRedeployJournal(ctx, containerInfo.ID)
	} else if _, containerRemoveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
		return dockerClient.ContainerRemove(ctx, containerInfo.ID, client.ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: false,
			RemoveLinks:   false,
		})
	}); containerRemoveErr != nil {
		slog.WarnContext(
			ctx, "failed to remove old container after successful recreate",
			"containerId", containerInfo.ID,
			"backupName", backupName,
			"error", containerRemoveErr,
		)
	} else {
		s.clearRedeployJournal(ctx, containerInfo.ID)
	}

	if logErr := s.eventService.LogContainerEvent(ctx, eventType, createResp.ID, newName, user.ID, user.Username, "0", database.JSON{
		"action":        action,
		"containerId":   containerInfo.ID,
		"containerName": newName,
		"image":         newConfig.Image,
	}); logErr != nil {
		slog.WarnContext(ctx, "failed to log recreate event", "err", logErr)
	}

	return createResp.ID, nil
}

func healthConfigFromCreate(hc *containertypes.HealthcheckCreate) *container.HealthConfig {
	if hc == nil {
		return nil
	}

	return &container.HealthConfig{
		Test:          append([]string{}, hc.Test...),
		Interval:      time.Duration(hc.Interval) * time.Second,
		Timeout:       time.Duration(hc.Timeout) * time.Second,
		StartPeriod:   time.Duration(hc.StartPeriod) * time.Second,
		StartInterval: time.Duration(hc.StartInterval) * time.Second,
		Retries:       hc.Retries,
	}
}

func retainedVolumeMountPoints(original, updated container.HostConfig, mountPoints []container.MountPoint) []container.MountPoint {
	specAt := func(hc container.HostConfig, destination string) (bool, bool) {
		if i := slices.IndexFunc(hc.Mounts, func(m mount.Mount) bool { return path.Clean(m.Target) == destination }); i >= 0 {
			return true, hc.Mounts[i].Type == mount.TypeVolume && hc.Mounts[i].Source == ""
		}
		for _, bind := range hc.Binds {
			parts := strings.SplitN(bind, ":", 3)
			if path.Clean(parts[min(1, len(parts)-1)]) == destination {
				return true, len(parts) == 1
			}
		}
		return false, false
	}

	return slices.DeleteFunc(slices.Clone(mountPoints), func(mp container.MountPoint) bool {
		inOriginal, _ := specAt(original, path.Clean(mp.Destination))
		inUpdated, anonymous := specAt(updated, path.Clean(mp.Destination))
		return kit.Ternary(inUpdated, !anonymous, inOriginal)
	})
}

// mergeEditMounts builds the desired mount set for an edit. A requested mount matching an existing one by type
// and target keeps its full original spec with the request's source and read-only flag; others are built fresh.
func mergeEditMounts(existing []mount.Mount, requested []containertypes.MountCreate) []mount.Mount {
	priorByKey := make(map[string]mount.Mount, len(existing))
	for _, prior := range existing {
		if _, seen := priorByKey[string(prior.Type)+"\x00"+prior.Target]; !seen {
			priorByKey[string(prior.Type)+"\x00"+prior.Target] = prior
		}
	}

	out := make([]mount.Mount, 0, len(requested))
	for _, req := range requested {
		prior := priorByKey[req.Type+"\x00"+req.Target]
		out = append(out, mount.Mount{ //exhaustruct:enforce
			Type:           mount.Type(req.Type),
			Source:         req.Source,
			Target:         req.Target,
			ReadOnly:       req.ReadOnly,
			Consistency:    prior.Consistency,
			BindOptions:    prior.BindOptions,
			VolumeOptions:  prior.VolumeOptions,
			ImageOptions:   prior.ImageOptions,
			TmpfsOptions:   prior.TmpfsOptions,
			ClusterOptions: prior.ClusterOptions,
		})
	}
	return out
}

func portMapFromCreate(bindings map[string][]containertypes.PortBindingCreate) (network.PortMap, error) {
	out := network.PortMap{}
	for portSpec, bindingList := range bindings {
		port, err := network.ParsePort(kit.Ternary(strings.Contains(portSpec, "/"), portSpec, portSpec+"/tcp"))
		if err != nil {
			return nil, fmt.Errorf("invalid port %s: %w", portSpec, err)
		}
		mapped := make([]network.PortBinding, 0, len(bindingList))
		for _, binding := range bindingList {
			hostIP, parseAddrErr := netip.ParseAddr(strings.TrimSpace(binding.HostIP))
			if parseAddrErr != nil && strings.TrimSpace(binding.HostIP) != "" {
				return nil, fmt.Errorf("invalid host IP %s: %w", strings.TrimSpace(binding.HostIP), parseAddrErr)
			}
			mapped = append(mapped, network.PortBinding{HostIP: hostIP, HostPort: binding.HostPort})
		}
		out[port] = mapped
	}
	return out, nil
}

func endpointIPAMFromCreate(ep containertypes.EndpointSettingsCreate) (*network.EndpointIPAMConfig, error) {
	if strings.TrimSpace(ep.IPv4Address) == "" && strings.TrimSpace(ep.IPv6Address) == "" {
		return nil, nil
	}

	ipv4, ipv4Err := netip.ParseAddr(strings.TrimSpace(ep.IPv4Address))
	if ipv4Err != nil && strings.TrimSpace(ep.IPv4Address) != "" {
		return nil, fmt.Errorf("invalid IPv4 address %s: %w", strings.TrimSpace(ep.IPv4Address), ipv4Err)
	}
	ipv6, ipv6Err := netip.ParseAddr(strings.TrimSpace(ep.IPv6Address))
	if ipv6Err != nil && strings.TrimSpace(ep.IPv6Address) != "" {
		return nil, fmt.Errorf("invalid IPv6 address %s: %w", strings.TrimSpace(ep.IPv6Address), ipv6Err)
	}
	return &network.EndpointIPAMConfig{IPv4Address: ipv4, IPv6Address: ipv6}, nil
}

// applyEditToContainerConfig returns cfg with the non-nil form-owned sections of an edit applied. Replaced port
// bindings recompute ExposedPorts as the new keys plus previously exposed but unbound ports (image EXPOSE entries).
func applyEditToContainerConfig(cfg container.Config, previousBindings network.PortMap, req containertypes.Edit) (container.Config, error) {
	newBindings, err := portMapFromCreate(kit.FromPtr(kit.FromPtr(req.HostConfig).PortBindings))
	if err != nil {
		return container.Config{}, err
	}
	exposed := network.PortSet{}
	for port := range newBindings {
		exposed[port] = struct{}{}
	}
	for port := range cfg.ExposedPorts {
		if _, wasBound := previousBindings[port]; !wasBound {
			exposed[port] = struct{}{}
		}
	}

	return container.Config{ //exhaustruct:enforce
		Hostname:        kit.Ternary(req.Hostname != nil, strings.TrimSpace(kit.FromPtr(req.Hostname)), cfg.Hostname),
		Domainname:      cfg.Domainname,
		User:            mo.PointerToOption(req.User).OrElse(cfg.User),
		AttachStdin:     cfg.AttachStdin,
		AttachStdout:    cfg.AttachStdout,
		AttachStderr:    cfg.AttachStderr,
		ExposedPorts:    kit.Ternary(kit.FromPtr(req.HostConfig).PortBindings != nil, exposed, cfg.ExposedPorts),
		Tty:             cfg.Tty,
		OpenStdin:       cfg.OpenStdin,
		StdinOnce:       cfg.StdinOnce,
		Env:             kit.Ternary(req.Environment != nil, append([]string{}, kit.FromPtr(req.Environment)...), cfg.Env),
		Cmd:             kit.Ternary(req.Command != nil, append([]string{}, kit.FromPtr(req.Command)...), cfg.Cmd),
		Healthcheck:     kit.Ternary(req.ClearHealthcheck, nil, cmp.Or(healthConfigFromCreate(req.Healthcheck), cfg.Healthcheck)),
		ArgsEscaped:     cfg.ArgsEscaped,
		Image:           cmp.Or(strings.TrimSpace(kit.FromPtr(req.Image)), cfg.Image),
		Volumes:         cfg.Volumes,
		WorkingDir:      mo.PointerToOption(req.WorkingDir).OrElse(cfg.WorkingDir),
		Entrypoint:      kit.Ternary(req.Entrypoint != nil, append([]string{}, kit.FromPtr(req.Entrypoint)...), cfg.Entrypoint),
		NetworkDisabled: cfg.NetworkDisabled,
		OnBuild:         cfg.OnBuild,
		Labels:          kit.Ternary(req.Labels != nil, maps.Clone(kit.FromPtr(req.Labels)), cfg.Labels),
		StopSignal:      cfg.StopSignal,
		StopTimeout:     cfg.StopTimeout,
		Shell:           cfg.Shell,
	}, nil
}

// applyEditToHostConfig overwrites the non-nil form-owned HostConfig sections of an edit request onto hc.
// Everything else (sysctls, ulimits, log drivers, devices, tmpfs, DNS, ...) carries over unchanged.
func applyEditToHostConfig(hc *container.HostConfig, req containertypes.Edit) error {
	if req.HostConfig == nil {
		return nil
	}

	newBindings, err := portMapFromCreate(kit.FromPtr(req.HostConfig.PortBindings))
	if err != nil {
		return err
	}

	if req.HostConfig.RestartPolicy != nil {
		hc.RestartPolicy = container.RestartPolicy{ // keep: Docker's ~70-field container.HostConfig is unreadable as a literal
			Name:              container.RestartPolicyMode(req.HostConfig.RestartPolicy.Name),
			MaximumRetryCount: req.HostConfig.RestartPolicy.MaximumRetryCount,
		}
	}
	hc.Binds = kit.Ternary(req.HostConfig.Binds != nil, append([]string{}, kit.FromPtr(req.HostConfig.Binds)...), hc.Binds)          // keep: ~70-field HostConfig
	hc.Mounts = kit.Ternary(req.HostConfig.Mounts != nil, mergeEditMounts(hc.Mounts, kit.FromPtr(req.HostConfig.Mounts)), hc.Mounts) // keep: ~70-field HostConfig
	hc.PortBindings = kit.Ternary(req.HostConfig.PortBindings != nil, newBindings, hc.PortBindings)                                  // keep: ~70-field HostConfig
	hc.Privileged = mo.PointerToOption(req.HostConfig.Privileged).OrElse(hc.Privileged)                                              // keep: ~70-field HostConfig
	hc.CapAdd = kit.Ternary(req.HostConfig.CapAdd != nil, append([]string{}, kit.FromPtr(req.HostConfig.CapAdd)...), hc.CapAdd)      // keep: ~70-field HostConfig
	hc.CapDrop = kit.Ternary(req.HostConfig.CapDrop != nil, append([]string{}, kit.FromPtr(req.HostConfig.CapDrop)...), hc.CapDrop)  // keep: ~70-field HostConfig
	hc.AutoRemove = mo.PointerToOption(req.HostConfig.AutoRemove).OrElse(hc.AutoRemove)                                              // keep: ~70-field HostConfig
	hc.ReadonlyRootfs = mo.PointerToOption(req.HostConfig.ReadonlyRootfs).OrElse(hc.ReadonlyRootfs)                                  // keep: ~70-field HostConfig
	hc.Memory = mo.PointerToOption(req.HostConfig.Memory).OrElse(hc.Memory)                                                          // keep: ~70-field HostConfig
	hc.MemorySwap = mo.PointerToOption(req.HostConfig.MemorySwap).OrElse(hc.MemorySwap)                                              // keep: ~70-field HostConfig
	hc.NanoCPUs = mo.PointerToOption(req.HostConfig.NanoCPUs).OrElse(hc.NanoCPUs)                                                    // keep: ~70-field HostConfig
	hc.CPUShares = mo.PointerToOption(req.HostConfig.CPUShares).OrElse(hc.CPUShares)                                                 // keep: ~70-field HostConfig

	return nil
}

// buildEditNetworkingConfig builds the networking config for an edit recreate. A nil request preserves the existing
// attachments like redeploy; otherwise it is the full desired endpoint set, keeping settings of endpoints that stay.
// Containers using host/none/container: network modes never get endpoint edits.
func buildEditNetworkingConfig(containerInspect container.InspectResponse, req *containertypes.NetworkingConfigCreate, apiVersion string) (*network.NetworkingConfig, error) {
	if mode := kit.FromPtr(containerInspect.HostConfig).NetworkMode; mode.IsHost() || mode.IsNone() || mode.IsContainer() {
		return nil, nil
	}

	if req == nil {
		return buildCleanNetworkingConfig(containerInspect, apiVersion), nil
	}

	existing := kit.FromPtr(buildCleanNetworkingConfig(containerInspect, apiVersion)).EndpointsConfig
	endpoints := make(map[string]*network.EndpointSettings, len(req.EndpointsConfig))
	for name, epReq := range req.EndpointsConfig {
		ipam, err := endpointIPAMFromCreate(epReq)
		if err != nil {
			return nil, err
		}
		prior := kit.FromPtr(existing[name])
		endpoints[name] = &network.EndpointSettings{ //exhaustruct:enforce
			IPAMConfig:          ipam,
			Links:               prior.Links,
			Aliases:             append([]string{}, epReq.Aliases...),
			DriverOpts:          prior.DriverOpts,
			GwPriority:          prior.GwPriority,
			NetworkID:           prior.NetworkID,
			EndpointID:          prior.EndpointID,
			Gateway:             prior.Gateway,
			IPAddress:           prior.IPAddress,
			MacAddress:          prior.MacAddress,
			IPPrefixLen:         prior.IPPrefixLen,
			IPv6Gateway:         prior.IPv6Gateway,
			GlobalIPv6Address:   prior.GlobalIPv6Address,
			GlobalIPv6PrefixLen: prior.GlobalIPv6PrefixLen,
			DNSNames:            prior.DNSNames,
		}
	}

	if len(endpoints) == 0 {
		return nil, nil
	}
	return &network.NetworkingConfig{EndpointsConfig: endpoints}, nil
}

// GetContainerEditConfig returns the purpose-built DTO backing the container
// edit form.
func (s *ContainerService) GetContainerEditConfig(ctx context.Context, containerID string) (containertypes.EditConfig, error) {
	containerJSON, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	})
	if err != nil {
		return containertypes.EditConfig{}, fmt.Errorf("failed to inspect container: %w", err)
	}

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	return containertypes.NewEditConfigFromInspect(
		&containerJSON.Container,
		docker.ComposeProjectLabel(kit.FromPtr(containerJSON.Container.Config).Labels) != "",
		labels.ShouldDisableArcaneServerRedeploy(kit.FromPtr(containerJSON.Container.Config).Labels, containerJSON.Container.ID, currentContainerID, currentContainerErr),
	), nil
}

// EditContainer applies a user-supplied config diff onto the container's
// inspected config and recreates it. Sections not owned by the edit form are
// preserved from the existing container.
func (s *ContainerService) EditContainer(ctx context.Context, containerID string, req containertypes.Edit, user usertypes.Actor) (string, error) {
	unlock, err := s.lockContainer(ctx, "edit container", containerID)
	if err != nil {
		return "", err
	}
	defer unlock()

	containerJSON, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	})
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, "", user.ID, user.Username, "0", err, database.JSON{
			"action": "edit",
			"step":   "inspect",
		})
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	containerInfo := containerJSON.Container
	if containerInfo.Config == nil {
		return "", errors.New("container config is nil")
	}

	containerName := strings.TrimPrefix(containerInfo.Name, "/")

	if localProject := docker.ComposeProjectLabel(containerInfo.Config.Labels); localProject != "" {
		return "", fmt.Errorf("compose project %s: %w", localProject, common.ErrContainerComposeManaged)
	}

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	if labels.ShouldDisableArcaneServerRedeploy(containerInfo.Config.Labels, containerInfo.ID, currentContainerID, currentContainerErr) {
		err = errors.New("arcane cannot edit itself; use the system upgrade flow (Settings -> Updates) instead")
		s.eventService.LogErrorEvent(ctx, event.EventTypeContainerError, "container", containerID, containerName, user.ID, user.Username, "0", err, database.JSON{
			"action": "edit",
			"step":   "self_edit_blocked",
		})
		return "", err
	}

	newName := cmp.Or(strings.TrimPrefix(strings.TrimSpace(kit.FromPtr(req.Name)), "/"), containerName)
	if newName != containerName {
		if _, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, dockerClient, newName, client.ContainerInspectOptions{})
		}); inspectErr == nil {
			return "", fmt.Errorf("container name %s: %w", newName, common.ErrContainerNameTaken)
		}
	}

	apiVersion, _ := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (string, error) {
		return compat.DetectDockerAPIVersion(ctx, dockerClient), nil
	})

	newHostConfig := kit.FromPtr(containerInfo.HostConfig)
	newConfig, err := applyEditToContainerConfig(*containerInfo.Config, newHostConfig.PortBindings, req)
	if err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}
	if applyEditToHostConfigErr := applyEditToHostConfig(&newHostConfig, req); applyEditToHostConfigErr != nil {
		return "", common.Classify(common.ErrValidation, applyEditToHostConfigErr)
	}
	networkingConfig, err := buildEditNetworkingConfig(containerInfo, req.NetworkingConfig, apiVersion)
	if err != nil {
		return "", common.Classify(common.ErrValidation, err)
	}

	// Unlike redeploy, edit only pulls when the effective image is missing
	// locally so an unrelated config change never surprise-upgrades the image.
	if newConfig.Image != "" {
		if _, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
			return dockerClient.ImageInspect(ctx, newConfig.Image)
		}); inspectErr != nil {
			if pullRedeployImageErr := s.pullRedeployImage(ctx, newConfig.Image, containerID, containerName, "edit", req.Credentials, user); pullRedeployImageErr != nil {
				return "", pullRedeployImageErr
			}
		}
	}

	return s.recreateContainer(ctx, containerInfo, "edit", newName, &newConfig, &newHostConfig, networkingConfig, apiVersion, event.EventTypeContainerUpdate, user)
}

func (s *ContainerService) GetContainerByReference(ctx context.Context, ref string) (*container.InspectResponse, error) {
	containerInspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, ref, client.ContainerInspectOptions{})
	})
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
	updates, err := s.imageService.GetUpdateInfoByContainers(ctx, []container.Summary{{
		ID:      details.ID,
		Image:   details.Image,
		ImageID: details.ImageID,
		Labels:  details.Labels,
	}})
	if err != nil {
		slog.WarnContext(ctx, "Failed to fetch container update info", "error", err)
	}
	resolvedIcon := s.resolveContainerIcon(ctx, details.Labels, map[string]projects.ArcaneComposeMetadata{})

	return containertypes.Details{ //exhaustruct:enforce
		ID:               details.ID,
		Name:             details.Name,
		Image:            details.Image,
		ImageID:          details.ImageID,
		Created:          details.Created,
		State:            details.State,
		Config:           details.Config,
		HostConfig:       details.HostConfig,
		NetworkSettings:  details.NetworkSettings,
		Ports:            details.Ports,
		Mounts:           details.Mounts,
		Labels:           details.Labels,
		ComposeInfo:      details.ComposeInfo,
		IconLightURL:     resolvedIcon.IconLightURL,
		IconDarkURL:      resolvedIcon.IconDarkURL,
		RedeployDisabled: labels.ShouldDisableArcaneServerRedeploy(details.Labels, details.ID, currentContainerID, currentContainerErr),
		AutoUpdateEnabled: !labels.IsUpdateDisabled(details.Labels) &&
			!docker.ContainerNameExcluded([]string{details.Name}, docker.ExcludedContainerNameSet(s.settingsService.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""))),
		UpdateInfo: updates[details.ID],
		ActivityID: details.ActivityID,
	}, nil
}

// containerProcessesPsArgs lists ps argument sets in preference order: procps first, then
// BusyBox, which rejects %cpu/%mem. Windows daemons ignore the arguments.
var containerProcessesPsArgs = [][]string{
	{"-eo", "pid,user,%cpu,%mem,etime,cmd"},
	{"-o", "pid,user,etime,args"},
}

// GetContainerProcesses returns Docker's `top` snapshot for a container.
func (s *ContainerService) GetContainerProcesses(ctx context.Context, containerID string) (containertypes.Processes, error) {
	result, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerTopResult, error) {
		var result client.ContainerTopResult
		var err error
		for _, args := range containerProcessesPsArgs {
			result, err = dockerClient.ContainerTop(ctx, containerID, client.ContainerTopOptions{Arguments: args})
			if err == nil || errdefs.IsNotFound(err) || errdefs.IsConflict(err) || ctx.Err() != nil {
				break
			}
		}
		return result, err
	})
	if err != nil {
		return containertypes.Processes{}, common.ClassifyDockerError(fmt.Errorf("failed to list container processes: %w", err))
	}
	return containertypes.Processes{
		Titles:    kit.Ternary(result.Titles == nil, []string{}, result.Titles),
		Processes: kit.Ternary(result.Processes == nil, [][]string{}, result.Processes),
	}, nil
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

func (s *ContainerService) DeleteContainer(ctx context.Context, containerID string, force, removeVolumes bool, user usertypes.Actor) error {
	unlock, err := s.lockContainer(ctx, "delete container", containerID)
	if err != nil {
		return err
	}
	defer unlock()

	// Get container mounts before deletion if we need to remove volumes
	var volumesToRemove []string
	if removeVolumes {
		containerJSON, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		})
		if inspectErr == nil {
			for _, mount := range containerJSON.Container.Mounts {
				// Only collect named volumes (not bind mounts or tmpfs)
				if mount.Type == "volume" && mount.Name != "" {
					volumesToRemove = append(volumesToRemove, mount.Name)
				}
			}
		}
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", containerID, "")()
	volumeReleases := make([]func(), 0, len(volumesToRemove))
	defer func() {
		for _, release := range slices.Backward(volumeReleases) {
			release()
		}
	}()
	for _, volumeName := range volumesToRemove {
		volumeReleases = append(volumeReleases, s.eventService.BeginDockerResourceSuppressionWindow("volume", volumeName, volumeName))
	}
	_, err = s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
		return dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
			Force:         force,
			RemoveVolumes: removeVolumes,
			RemoveLinks:   false,
		})
	})
	if err != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeContainerError,
			"container",
			containerID,
			"",
			user.ID,
			user.Username,
			"0",
			err,
			database.JSON{
				"action":        "delete",
				"force":         force,
				"removeVolumes": removeVolumes,
			},
		)
		return fmt.Errorf("failed to delete container: %w", err)
	}

	// Remove named volumes if requested
	for _, volumeName := range volumesToRemove {
		if _, removeErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeRemoveResult, error) {
			return dockerClient.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: false})
		}); removeErr != nil {
			// Log but don't fail if volume removal fails (might be in use by another container)
			s.eventService.LogErrorEvent(ctx, event.EventTypeVolumeError, "volume", volumeName, "", user.ID, user.Username, "0", removeErr, database.JSON{"action": "delete", "container": containerID})
		}
	}

	if logErr := s.eventService.LogContainerEvent(ctx, event.EventTypeContainerDelete, containerID, "name", user.ID, user.Username, "0", database.JSON{
		"action":      "delete",
		"containerId": containerID,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log container action", "action", "delete", "error", logErr)
	}

	return nil
}

func (s *ContainerService) CreateContainer(
	ctx context.Context,
	config *container.Config,
	hostConfig *container.HostConfig,
	networkingConfig *network.NetworkingConfig,
	containerName string,
	user usertypes.Actor,
	credentials []containerregistry.Credential,
) (
	*container.InspectResponse,
	error,
) {
	if _, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, config.Image)
	}); inspectErr != nil {
		// Image not found locally, need to pull it
		if pullErr := s.pullRedeployImage(ctx, config.Image, "", containerName, "create", credentials, user); pullErr != nil {
			return nil, pullErr
		}
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", "", containerName)()
	resp, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return compat.ContainerCreateWithCompatibility(ctx, dockerClient, client.ContainerCreateOptions{
			Config:           config,
			HostConfig:       hostConfig,
			NetworkingConfig: networkingConfig,
			Name:             containerName,
		})
	})
	if err != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeContainerError,
			"container",
			"",
			containerName,
			user.ID,
			user.Username,
			"0",
			err,
			database.JSON{
				"action": "create",
				"image":  config.Image,
				"step":   "create",
			},
		)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("container", resp.ID, containerName)()
	if logErr := s.eventService.LogContainerEvent(ctx, event.EventTypeContainerCreate, resp.ID, "name", user.ID, user.Username, "0", database.JSON{
		"action":      "create",
		"containerId": resp.ID,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log container stop action", "error", logErr)
	}

	if _, containerStartErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	}); containerStartErr != nil {
		_, _ = s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		})
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeContainerError,
			"container",
			resp.ID,
			containerName,
			user.ID,
			user.Username,
			"0",
			containerStartErr,
			database.JSON{
				"action": "create",
				"image":  config.Image,
				"step":   "start",
			},
		)
		return nil, fmt.Errorf("failed to start container: %w", containerStartErr)
	}

	containerJSON, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, resp.ID, client.ContainerInspectOptions{})
	})
	if err != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeContainerError,
			"container",
			resp.ID,
			containerName,
			user.ID,
			user.Username,
			"0",
			err,
			database.JSON{
				"action": "create",
				"image":  config.Image,
				"step":   "inspect",
			},
		)
		return nil, fmt.Errorf("failed to inspect created container: %w", err)
	}

	return new(containerJSON.Container), nil
}

func (s *ContainerService) StreamStats(ctx context.Context, containerID string, statsChan chan<- any) error {
	return s.stats.Stream(ctx, containerID, statsChan)
}

// ResourceSampleLimit caps how many requested containers one SampleResources call reads.
const ResourceSampleLimit = 500

// SampleResources samples the running containers among the first ResourceSampleLimit ids,
// refetching samples older than maxAge. Blank, stopped and unknown IDs are omitted.
func (s *ContainerService) SampleResources(ctx context.Context, ids []string, maxAge time.Duration) (map[string]*containertypes.ResourceSample, error) {
	requested := slices.DeleteFunc(slices.Clone(ids[:min(len(ids), ResourceSampleLimit)]), func(id string) bool {
		return strings.TrimSpace(id) == ""
	})
	if len(requested) == 0 {
		return map[string]*containertypes.ResourceSample{}, nil
	}

	dockerContainers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	matched := make([]containertypes.Summary, 0, len(requested))
	for _, dc := range dockerContainers {
		if dc.State == container.StateRunning && slices.Contains(requested, dc.ID) {
			matched = append(matched, containertypes.Summary{ID: dc.ID, State: string(dc.State)})
		}
	}
	return s.stats.Collect(ctx, matched, maxAge)
}

func (s *ContainerService) ListContainersPaginated(
	ctx context.Context,
	params pagination.QueryParams,
	includeAll bool,
	includeInternal bool,
	includeHidden bool,
	groupBy string,
) (ContainerListResult, error) {
	dockerContainers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return ContainerListResult{}, err
	}
	if !includeAll {
		running := make([]container.Summary, 0, len(dockerContainers))
		for _, dc := range dockerContainers {
			if dc.State == container.StateRunning || dc.State == container.StatePaused || dc.State == container.StateRestarting {
				running = append(running, dc)
			}
		}
		dockerContainers = running
	}

	dockerContainers = FilterExcludedContainers(dockerContainers, includeInternal, includeHidden)
	// Update lookup failures degrade to missing status rather than a failed list.
	updateInfoMap, err := s.imageService.GetUpdateInfoByContainers(ctx, dockerContainers)
	if err != nil {
		slog.WarnContext(ctx, "Failed to fetch container update info", "error", err)
	}
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	items := s.BuildSummaries(ctx, dockerContainers, updateInfoMap, currentContainerID, currentContainerErr)

	config := s.buildContainerPaginationConfig()
	counts := s.CalculateStatusCounts(items)

	if groupBy == containerGroupByProject {
		result := config.SearchOrderAndPaginate(items, pagination.QueryParams{
			SearchQuery: params.SearchQuery,
			SortParams:  params.SortParams,
			Params:      pagination.Params{Limit: -1, SkipCount: params.SkipCount},
			Filters:     params.Filters,
		})
		groups, paginationResp := paginateContainerProjectGroups(result, params)

		// Icons must be resolved before flattening: groups hold value copies,
		// so the flattened items only carry icons applied to the groups first.
		metadataByProject := map[string]projects.ArcaneComposeMetadata{}
		for gi := range groups {
			s.ApplySummaryIcons(ctx, groups[gi].Items, metadataByProject)
		}

		return ContainerListResult{
			Items:      flattenContainerProjectGroups(groups),
			Groups:     groups,
			Pagination: paginationResp,
			Counts:     counts,
		}, nil
	}

	// Resource sorting is limited to the ungrouped list: project-grouped mode keeps the existing group pagination.
	if containertypes.IsResourceSort(params.Sort) {
		// Apply search and filters, collect stats for the matching containers, then sort and paginate.
		filtered := make([]containertypes.Summary, 0, len(items))
		for _, item := range items {
			if config.MatchesSearchAndFilters(item, params) {
				filtered = append(filtered, item)
			}
		}

		samples, collectErr := s.stats.Collect(ctx, filtered, stats.ContainerResourceSampleTTL)
		if collectErr != nil {
			return ContainerListResult{}, collectErr
		}
		sampled := make([]containertypes.Summary, 0, len(filtered))
		for _, item := range filtered {
			sampled = append(sampled, containertypes.Summary{ //exhaustruct:enforce
				EnvironmentTag:    item.EnvironmentTag,
				ID:                item.ID,
				Names:             item.Names,
				Image:             item.Image,
				ImageID:           item.ImageID,
				Command:           item.Command,
				Created:           item.Created,
				Ports:             item.Ports,
				Labels:            item.Labels,
				State:             item.State,
				Status:            item.Status,
				HostConfig:        item.HostConfig,
				NetworkSettings:   item.NetworkSettings,
				Mounts:            item.Mounts,
				IconLightURL:      item.IconLightURL,
				IconDarkURL:       item.IconDarkURL,
				UpdateStrategy:    item.UpdateStrategy,
				UpdateInfo:        item.UpdateInfo,
				RedeployDisabled:  item.RedeployDisabled,
				AutoUpdateEnabled: item.AutoUpdateEnabled,
				Hidden:            item.Hidden,
				ResourceSample:    samples[item.ID],
			})
		}

		page := config.OrderAndPaginate(sampled, params)
		s.ApplySummaryIcons(ctx, page, nil)

		return ContainerListResult{
			Items:      page,
			Pagination: pagination.BuildResponse(int64(len(sampled)), int64(len(items)), params),
			Counts:     counts,
		}, nil
	}

	result := config.SearchOrderAndPaginate(items, params)
	s.ApplySummaryIcons(ctx, result.Items, nil)

	return ContainerListResult{
		Items:      result.Items,
		Pagination: pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params),
		Counts:     counts,
	}, nil
}

func (s *ContainerService) AggregateContainers(ctx context.Context, params pagination.QueryParams, groupBy string) (ContainerPaginatedResponse, error) {
	collection, err := httpx.CollectEnvironments[ContainerPaginatedResponse](ctx, url.Values{"start": {"0"}, "limit": {"-1"}, "groupBy": nil})
	if err != nil {
		return ContainerPaginatedResponse{}, err
	}

	pages := make([]pagination.EnvironmentPage[containertypes.Summary], 0, len(collection.Results))
	running, stopped, total := 0, 0, 0
	resourceSortSupported := true
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[containertypes.Summary]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
		running += result.Value.Counts.RunningContainers
		stopped += result.Value.Counts.StoppedContainers
		total += result.Value.Counts.TotalContainers
		resourceSortSupported = resourceSortSupported && result.Value.ResourceSortSupported
	}

	if groupBy != containerGroupByProject {
		data, resp := pagination.MergeEnvironmentPages(s.buildContainerPaginationConfig(), pages, params)
		return ContainerPaginatedResponse{
			Success: true,
			Data:    data,
			Counts: containertypes.StatusCounts{
				RunningContainers: running,
				StoppedContainers: stopped,
				TotalContainers:   total,
			},
			Pagination:            handlerutil.PaginationResponse(resp),
			ResourceSortSupported: resourceSortSupported,
			FailedEnvironments:    collection.Failures,
		}, nil
	}

	data, resp := pagination.MergeEnvironmentPages(s.buildContainerPaginationConfig(), pages, pagination.QueryParams{
		SearchQuery: params.SearchQuery,
		SortParams:  params.SortParams,
		Params:      pagination.Params{Limit: -1, SkipCount: params.SkipCount},
		Filters:     params.Filters,
	})
	groups, groupedResp := paginateContainerProjectGroups(pagination.FilterResult[containertypes.Summary]{
		Items:          data,
		TotalCount:     resp.TotalItems,
		TotalAvailable: resp.GrandTotalItems,
	}, params)
	return ContainerPaginatedResponse{
		Success: true,
		Data:    flattenContainerProjectGroups(groups),
		Groups:  groups,
		Counts: containertypes.StatusCounts{
			RunningContainers: running,
			StoppedContainers: stopped,
			TotalContainers:   total,
		},
		Pagination:            handlerutil.PaginationResponse(groupedResp),
		ResourceSortSupported: resourceSortSupported,
		FailedEnvironments:    collection.Failures,
	}, nil
}

func paginateContainerProjectGroups(
	result pagination.FilterResult[containertypes.Summary],
	params pagination.QueryParams,
) ([]containertypes.SummaryGroup, pagination.Response) {
	groups := groupContainersByProject(result.Items)
	totalCount := len(result.Items)

	if params.Limit <= 0 {
		return groups, pagination.BuildResponse(int64(totalCount), result.TotalAvailable, params)
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

func groupContainersByProject(items []containertypes.Summary) []containertypes.SummaryGroup {
	groupNames := make([]string, 0)
	itemsByGroup := make(map[string][]containertypes.Summary)
	for _, item := range items {
		groupName := cmp.Or(docker.ComposeProjectLabel(item.Labels), containerNoProjectGroup)
		if _, exists := itemsByGroup[groupName]; !exists {
			groupNames = append(groupNames, groupName)
		}
		itemsByGroup[groupName] = append(itemsByGroup[groupName], item)
	}

	groups := make([]containertypes.SummaryGroup, 0, len(groupNames))
	for _, groupName := range groupNames {
		groups = append(groups, containertypes.SummaryGroup{GroupName: groupName, Items: itemsByGroup[groupName]})
	}
	return groups
}

func flattenContainerProjectGroups(groups []containertypes.SummaryGroup) []containertypes.Summary {
	flattened := make([]containertypes.Summary, 0)
	for _, group := range groups {
		flattened = append(flattened, group.Items...)
	}

	return flattened
}

// FilterExcludedContainers removes internal and hidden containers unless their corresponding inclusion flags are enabled.
func FilterExcludedContainers(containers []container.Summary, includeInternal, includeHidden bool) []container.Summary {
	if includeInternal && includeHidden {
		return containers
	}

	filtered := make([]container.Summary, 0, len(containers))
	for _, dc := range containers {
		internal, _ := kit.ParseBool(dc.Labels[libarcane.InternalResourceLabel])
		hidden, _ := kit.ParseBool(dc.Labels[libarcane.HiddenResourceLabel])
		if (!includeInternal && internal) || (!includeHidden && hidden) {
			continue
		}
		filtered = append(filtered, dc)
	}
	return filtered
}

func (s *ContainerService) BuildSummaries(
	ctx context.Context,
	containers []container.Summary,
	updateInfoMap map[string]*imagetypes.UpdateInfo,
	currentContainerID string,
	currentContainerErr error,
) []containertypes.Summary {
	items := make([]containertypes.Summary, 0, len(containers))
	excluded := docker.ExcludedContainerNameSet(s.settingsService.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""))
	for _, dc := range containers {
		summary := containertypes.NewSummary(dc)
		policy := updater.DefaultLabelPolicy().TagPolicy(dc.Labels)
		resolved, policyErr := tagpolicy.Resolve(dc.Image, policy)
		hidden, _ := kit.ParseBool(dc.Labels[libarcane.HiddenResourceLabel])
		items = append(items, containertypes.Summary{ //exhaustruct:enforce
			EnvironmentTag:    summary.EnvironmentTag,
			ID:                summary.ID,
			Names:             summary.Names,
			Image:             summary.Image,
			ImageID:           summary.ImageID,
			Command:           summary.Command,
			Created:           summary.Created,
			Ports:             summary.Ports,
			Labels:            summary.Labels,
			State:             summary.State,
			Status:            summary.Status,
			HostConfig:        summary.HostConfig,
			NetworkSettings:   summary.NetworkSettings,
			Mounts:            summary.Mounts,
			IconLightURL:      summary.IconLightURL,
			IconDarkURL:       summary.IconDarkURL,
			UpdateStrategy:    kit.Ternary(policyErr != nil, cmp.Or(policy.Strategy, "auto"), resolved.Strategy),
			UpdateInfo:        updateInfoMap[dc.ID],
			RedeployDisabled:  labels.ShouldDisableArcaneServerRedeploy(summary.Labels, summary.ID, currentContainerID, currentContainerErr),
			AutoUpdateEnabled: !labels.IsUpdateDisabled(dc.Labels) && !docker.ContainerNameExcluded(dc.Names, excluded),
			Hidden:            hidden,
			ResourceSample:    summary.ResourceSample,
		})
	}
	return items
}

// ApplySummaryIcons resolves icons for a page of summaries.
// Icon resolution is deferred until after pagination so the cost is bounded by
// page size rather than the full container list.
func (s *ContainerService) ApplySummaryIcons(ctx context.Context, summaries []containertypes.Summary, metadataByProject map[string]projects.ArcaneComposeMetadata) {
	if metadataByProject == nil {
		metadataByProject = map[string]projects.ArcaneComposeMetadata{}
	}
	for i := range summaries {
		resolvedIcon := s.resolveContainerIcon(ctx, summaries[i].Labels, metadataByProject)
		summaries[i].IconLightURL = resolvedIcon.IconLightURL // keep: the exported API enriches caller-owned rows in place
		summaries[i].IconDarkURL = resolvedIcon.IconDarkURL   // keep: the exported API enriches caller-owned rows in place
	}
}

func (s *ContainerService) resolveContainerIcon(ctx context.Context, localLabels map[string]string, metadataByProject map[string]projects.ArcaneComposeMetadata) iconcatalog.ResolvedIconSet {
	explicitIcon := projects.FindArcaneIconSet(localLabels)
	projectName := docker.ComposeProjectLabel(localLabels)
	if !explicitIcon.IsEmpty() || projectName == "" {
		return iconcatalog.Resolve(project.IconCatalogForContext(ctx), explicitIcon)
	}

	meta, ok := metadataByProject[projectName]
	if !ok {
		meta, ok, _ = s.iconMetaCache.Get(projectName)
	}
	if !ok {
		meta = func() projects.ArcaneComposeMetadata {
			proj, err := s.projectService.GetProjectByComposeName(ctx, projectName)
			if err != nil {
				return projects.ArcaneComposeMetadata{ServiceIconSets: map[string]projects.IconSet{}}
			}
			return s.projectService.ProjectMetadata(ctx, *proj, nil)
		}()
		s.iconMetaCache.Set(projectName, meta)
	}
	metadataByProject[projectName] = meta

	return iconcatalog.Resolve(project.IconCatalogForContext(ctx), cmp.Or(
		explicitIcon,
		meta.ServiceIconSets[docker.ComposeServiceLabel(localLabels)],
		meta.ProjectIcon,
	))
}

func (s *ContainerService) buildContainerPaginationConfig() pagination.Config[containertypes.Summary] {
	comparePorts := func(descending bool) pagination.SortOption[containertypes.Summary] {
		return func(a, b containertypes.Summary) int {
			hasPortsA, portA := lowestContainerPortSortValue(a.Ports)
			hasPortsB, portB := lowestContainerPortSortValue(b.Ports)

			switch {
			case !hasPortsA && !hasPortsB, portA == portB:
				return stats.CompareContainerNamesForSort(a, b)
			case !hasPortsA:
				return 1
			case !hasPortsB:
				return -1
			default:
				return kit.Ternary((portA < portB) != descending, -1, 1)
			}
		}
	}

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
		SortBindings: []pagination.SortBinding[containertypes.Summary]{
			{
				Key: "name",
				Fn:  stats.CompareContainerNamesForSort,
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
				Fn:     comparePorts(false),
				DescFn: comparePorts(true),
			},
			{
				Key: "created",
				Fn: func(a, b containertypes.Summary) int {
					return cmp.Compare(a.Created, b.Created)
				},
			},
			{
				Key:    containertypes.SortCPUUsage,
				Fn:     stats.ContainerResourceSampleSort(containertypes.SortCPUUsage, false),
				DescFn: stats.ContainerResourceSampleSort(containertypes.SortCPUUsage, true),
			},
			{
				Key:    containertypes.SortMemoryUsage,
				Fn:     stats.ContainerResourceSampleSort(containertypes.SortMemoryUsage, false),
				DescFn: stats.ContainerResourceSampleSort(containertypes.SortMemoryUsage, true),
			},
		},
		FilterAccessors: []pagination.FilterAccessor[containertypes.Summary]{
			{
				Key: "updates",
				Fn: func(c containertypes.Summary, filterValue string) bool {
					switch filterValue {
					case "has_update":
						return kit.FromPtr(c.UpdateInfo).HasUpdate
					case "up_to_date":
						return c.UpdateInfo != nil && !c.UpdateInfo.HasUpdate && c.UpdateInfo.Error == ""
					case "error":
						return kit.FromPtr(c.UpdateInfo).Error != ""
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
					value, valid := kit.ParseBool(filterValue)
					return !valid || (docker.ComposeProjectLabel(c.Labels) == "" && !labels.IsSwarmTask(c.Labels)) == value
				},
			},
			{
				Key:     "label",
				NoSplit: true,
				Fn: func(c containertypes.Summary, filterValue string) bool {
					key, value, hasValue := strings.Cut(filterValue, "=")
					if strings.TrimSpace(key) == "" {
						return true
					}
					actual, ok := c.Labels[strings.TrimSpace(key)]
					return ok && (!hasValue || actual == value)
				},
			},
		},
	}
}

func lowestContainerPortSortValue(ports []containertypes.Port) (bool, int) {
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

	lowest := cmp.Or(lowestPublished, lowestPrivate)
	return lowest > 0, lowest
}

func (s *ContainerService) CalculateStatusCounts(items []containertypes.Summary) containertypes.StatusCounts {
	running := 0
	for _, c := range items {
		if c.State == "running" {
			running++
		}
	}
	return containertypes.StatusCounts{
		RunningContainers: running,
		StoppedContainers: len(items) - running,
		TotalContainers:   len(items),
	}
}

// CreateExec creates an exec instance in the container
func (s *ContainerService) CreateExec(ctx context.Context, containerID string, cmd []string, cols, rows uint) (string, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	execResp, err := dockerClient.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
		ConsoleSize:  client.ConsoleSize{Height: rows, Width: cols},
		Cmd:          cmd,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	return execResp.ID, nil
}

func (s *ContainerService) ResizeExec(ctx context.Context, execID string, cols, rows uint) error {
	_, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ExecResizeResult, error) {
		return dockerClient.ExecResize(ctx, execID, client.ExecResizeOptions{Height: rows, Width: cols})
	})
	if err != nil {
		return fmt.Errorf("failed to resize exec %s: %w", execID, err)
	}
	return nil
}

func (e *ExecSession) Stdin() io.WriteCloser { return e.hijackedResp.Conn }

func (e *ExecSession) Stdout() io.Reader { return e.hijackedResp.Reader }

// Close terminates the exec session and kills the process if still running.
func (e *ExecSession) Close(ctx context.Context) error {
	var closeErr error
	e.closeOnce.Do(func() {
		slog.DebugContext(ctx, "Closing exec session", "execId", e.execID, "containerId", e.containerID)

		deadline, _ := ctx.Deadline()
		_ = e.hijackedResp.Conn.SetWriteDeadline(deadline)
		_, _ = e.hijackedResp.Conn.Write([]byte{0x03, 0x04})
		_, _ = e.hijackedResp.Conn.Write([]byte("exit\n"))
		e.hijackedResp.Close()

		pollCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			inspect, err := e.dockerClient.ExecInspect(pollCtx, e.execID, client.ExecInspectOptions{})
			if err != nil {
				closeErr = fmt.Errorf("inspect exec %s: %w", e.execID, err)
				return
			}
			if !inspect.Running {
				return
			}
			select {
			case <-pollCtx.Done():
				closeErr = fmt.Errorf("exec %s still running with pid %d after session close", e.execID, inspect.PID)
				return
			case <-ticker.C:
			}
		}
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

func (d *demuxedLogs) Close() error {
	return errors.Join(d.logs.Close(), d.pipe.Close())
}

func (s *ContainerService) openLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (io.ReadCloser, client.ContainerInspectResult, error) {
	containerInspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	})
	if err != nil {
		return nil, client.ContainerInspectResult{}, fmt.Errorf("failed to inspect container for logs: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, client.ContainerInspectResult{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	logs, err := dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, client.ContainerInspectResult{}, fmt.Errorf("failed to get container logs: %w", err)
	}
	return logs, containerInspect, nil
}

func (s *ContainerService) StreamLogs(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	logs, containerInspect, err := s.openLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
	})
	if err != nil {
		return err
	}
	defer func() { _ = logs.Close() }()

	return docker.StreamContainerLogs(ctx, logs, logsChan, follow, kit.FromPtr(containerInspect.Container.Config).Tty)
}

// DownloadLogs returns every log line Docker retains for the container as a
// single plain-text stream plus the attachment filename. Callers must Close it.
func (s *ContainerService) DownloadLogs(ctx context.Context, containerID string) (io.ReadCloser, string, error) {
	logs, containerInspect, err := s.openLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "all",
		Timestamps: true,
	})
	if err != nil {
		return nil, "", err
	}

	filename := "container-" + containerInspect.Container.ID[:min(12, len(containerInspect.Container.ID))] + "-logs.log"
	if kit.FromPtr(containerInspect.Container.Config).Tty {
		return logs, filename, nil
	}

	pr, pw := io.Pipe()
	go func() {
		_, copyErr := stdcopy.StdCopy(pw, pw, logs)
		if docker.IsExpectedStreamEndError(copyErr) {
			copyErr = nil
		}
		_ = pw.CloseWithError(copyErr)
	}()
	return &demuxedLogs{
		Reader: pr,
		pipe:   pr,
		logs:   logs,
	}, filename, nil
}
