package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	vuln "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/vuln"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/types/v2/version"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/sys/cgroup"
	libupdater "go.getarcane.app/updater/pkg/labels"
	updatertypes "go.getarcane.app/updater/types"
)

const defaultArcaneUpgraderImageInternal = "ghcr.io/getarcaneapp/arcane:latest"

type SystemUpgradeService struct {
	upgrading       atomic.Bool
	updatingAll     atomic.Bool
	db              *database.DB
	dockerService   *DockerClientService
	versionService  *VersionService
	eventService    *EventService
	settingsService *SettingsService
}

type upgraderRuntimeOptionsInternal struct {
	ContainerEnv []string
	Mounts       []mounttypes.Mount
	NetworkMode  containertypes.NetworkMode
}

func NewSystemUpgradeService(
	db *database.DB,
	dockerService *DockerClientService,
	versionService *VersionService,
	eventService *EventService,
	settingsService *SettingsService,
) *SystemUpgradeService {
	return &SystemUpgradeService{
		db:              db,
		dockerService:   dockerService,
		versionService:  versionService,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

// CanUpgrade checks if self-upgrade is possible
func (s *SystemUpgradeService) CanUpgrade(ctx context.Context) (bool, error) {
	// Check if running in Docker
	containerId, err := s.getCurrentContainerIDInternal()
	if err != nil {
		return false, err
	}

	// Verify we can access Docker
	_, err = s.dockerService.GetClient(ctx)
	if err != nil {
		return false, errors.New("docker socket is not accessible")
	}

	// Verify we can find our container
	_, err = s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return false, err
	}

	return true, nil
}

// TriggerUpgradeViaCLI spawns the upgrade CLI command in a separate container
// This avoids self-termination issues by running the upgrade from outside.
// A zero-value target upgrades the current container to its own image tag;
// the updater engine passes an explicit target with the resolved new image.
func (s *SystemUpgradeService) TriggerUpgradeViaCLI(ctx context.Context, user models.User, target updatertypes.SelfUpdateTarget) error {
	if !s.upgrading.CompareAndSwap(false, true) {
		return common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrade is already in progress"))
	}
	defer s.upgrading.Store(false)

	containerId := strings.TrimSpace(target.ContainerID)
	if containerId == "" {
		// Fall back to the container this process runs in
		var err error
		containerId, err = s.getCurrentContainerIDInternal()
		if err != nil {
			return errors.WrapIf(err, "get current container")
		}
	}

	currentContainer, err := s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return errors.WrapIf(err, "inspect container")
	}

	containerName := strings.TrimPrefix(currentContainer.Name, "/")

	// Determine binary path based on container type (agent vs main)
	binaryPath := "/app/arcane"
	if currentContainer.Config != nil {
		binaryPath = determineUpgradeBinaryPathInternal(currentContainer.Config.Labels)
	}

	targetImage := strings.TrimSpace(target.NewImageRef)

	// Log upgrade event
	metadata := models.JSON{
		"action":        "system_upgrade_cli",
		"containerId":   containerId,
		"containerName": containerName,
		"method":        "cli",
		"targetImage":   targetImage,
	}
	if err := s.eventService.LogUserEvent(ctx, models.EventTypeSystemUpgrade, user.ID, user.Username, metadata); err != nil {
		slog.Warn("Failed to log upgrade event", "error", err)
	}

	// Run the upgrader from the image we are upgrading to, so the upgrade CLI
	// is the new version. Without a resolved target image, fall back to the
	// running container's own image reference.
	upgraderImage := targetImage
	if upgraderImage == "" {
		upgraderImage = defaultArcaneUpgraderImageInternal
		if currentContainer.Config != nil {
			if img := strings.TrimSpace(currentContainer.Config.Image); img != "" {
				upgraderImage = img
			}
		}
	}
	slog.Debug("Using upgrader image", "image", upgraderImage)

	slog.Info("Spawning upgrade CLI command", "containerName", containerName, "upgraderImage", upgraderImage)

	// Spawn the upgrade command in a detached container
	// This will run independently of the current container
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to connect to Docker")
	}

	// Pull the upgrader image first to ensure it exists
	slog.Info("Pulling upgrader image", "image", upgraderImage)

	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
	defer pullCancel()

	pullReader, err := dockerClient.ImagePull(pullCtx, upgraderImage, client.ImagePullOptions{})
	if err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return errors.Errorf("upgrader image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", upgraderImage)
		}
		return errors.WrapIf(err, "pull upgrader image")
	}
	// Drain and validate the JSON stream to complete the pull.
	if err := dockerutils.RenderJSONMessageStream(pullReader, io.Discard); err != nil {
		_ = pullReader.Close()
		return errors.WrapIf(err, "failed to complete upgrader image pull")
	}
	if closeErr := pullReader.Close(); closeErr != nil {
		slog.Warn("Failed to close upgrader image pull reader", "error", closeErr)
	}
	slog.Info("Upgrader image pulled successfully", "image", upgraderImage)

	// Try to get the /app/data mount from current container so upgrade logs persist.
	appDataMount := dockerutils.MountForDestination(currentContainer.Mounts, "/app/data", "/app/data")
	if appDataMount == nil {
		slog.Warn("Could not detect /app/data mount; upgrader logs may not persist")
	} else {
		slog.Debug("Mounting /app/data into upgrader container", "type", appDataMount.Type, "source", appDataMount.Source)
	}

	// Create the upgrader container config
	runtimeOptions, err := resolveSystemUpgraderRuntimeOptionsInternal(
		ctx,
		s.dockerService.DockerHost(),
		&currentContainer,
		func(ctx context.Context, containerPath string) (string, error) {
			return projects.GetHostPathForContainerPath(ctx, dockerClient, containerPath)
		},
		func() bool {
			_, err := cgroup.CurrentContainerID()
			return err == nil
		},
	)
	if err != nil {
		return errors.WrapIf(err, "resolve upgrader docker runtime")
	}

	upgradeCmd := []string{binaryPath, "upgrade", "--container", containerName}
	if targetImage != "" {
		upgradeCmd = append(upgradeCmd, "--image", targetImage)
	}

	config := &containertypes.Config{
		Image: upgraderImage,
		Cmd:   upgradeCmd,
		// The upgrader needs root for the Docker socket; unlike the server it
		// is short-lived and never goes through the runtime-identity drop, so
		// don't rely on the image's default user.
		User: "0:0",
		Env:  runtimeOptions.ContainerEnv,
		Labels: map[string]string{
			"com.getarcaneapp.arcane.upgrader": "true",
			"com.getarcaneapp.arcane":          "true",
		},
	}

	mounts := append([]mounttypes.Mount{}, runtimeOptions.Mounts...)
	if appDataMount != nil {
		mounts = append(mounts, *appDataMount)
	}

	keepUpgraderContainer := strings.EqualFold(strings.TrimSpace(os.Getenv("ARCANE_UPGRADE_KEEP_CONTAINER")), "true")
	if keepUpgraderContainer {
		slog.Info("Keeping upgrader container after exit (ARCANE_UPGRADE_KEEP_CONTAINER=true)")
	}

	hostConfig := &containertypes.HostConfig{
		AutoRemove:  !keepUpgraderContainer, // default: clean up after completion
		Mounts:      mounts,
		NetworkMode: runtimeOptions.NetworkMode,
	}
	// Inherit the security context that lets the running Arcane container reach
	// the Docker socket (e.g. SELinux label=disable, privileged); the upgrader
	// needs the same access on hardened hosts.
	if currentContainer.HostConfig != nil {
		hostConfig.SecurityOpt = slices.Clone(currentContainer.HostConfig.SecurityOpt)
		hostConfig.Privileged = currentContainer.HostConfig.Privileged
	}
	// On SELinux-enforcing hosts the socket carries container_var_run_t, which
	// container processes cannot connect to regardless of UID; without an
	// explicit label opt the upgrader would exit with EACCES and auto-remove.
	if !hostConfig.Privileged && !hasSELinuxLabelOptInternal(hostConfig.SecurityOpt) && daemonHasSELinuxEnabledInternal(ctx, dockerClient) {
		hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "label=disable")
	}

	containerName = fmt.Sprintf("%s-upgrader-%d", containerName, time.Now().Unix())

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
		Name:       containerName,
	})
	if err != nil {
		return errors.WrapIf(err, "create upgrader container")
	}

	// Start the upgrader container - it will run the upgrade and auto-remove
	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		return errors.WrapIf(err, "start upgrader container")
	}

	slog.Info("Upgrade container started", "upgraderId", resp.ID[:12], "upgraderName", containerName)

	return nil
}

// hasSELinuxLabelOptInternal reports whether the security options already set
// an SELinux label policy (e.g. "label=disable", "label:disable", "label=type:...").
func hasSELinuxLabelOptInternal(securityOpts []string) bool {
	for _, opt := range securityOpts {
		if strings.HasPrefix(strings.TrimSpace(opt), "label") {
			return true
		}
	}
	return false
}

func daemonHasSELinuxEnabledInternal(ctx context.Context, dockerClient *client.Client) bool {
	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		slog.Debug("Failed to query daemon info for SELinux detection", "error", err)
		return false
	}
	return slices.Contains(infoResult.Info.SecurityOptions, "name=selinux")
}

func determineUpgradeBinaryPathInternal(labels map[string]string) string {
	if libupdater.IsArcaneAgentContainer(labels) {
		return "/app/arcane-agent"
	}

	return "/app/arcane"
}

func resolveSystemUpgraderRuntimeOptionsInternal(
	ctx context.Context,
	dockerHost string,
	currentContainer *containertypes.InspectResponse,
	discoverHostPath func(context.Context, string) (string, error),
	isRunningInDocker func() bool,
) (upgraderRuntimeOptionsInternal, error) {
	options := upgraderRuntimeOptionsInternal{
		ContainerEnv: vuln.BuildDockerHostEnv(dockerHost),
	}

	scheme, socketPath, err := vuln.ParseDockerHost(dockerHost)
	if err != nil {
		return upgraderRuntimeOptionsInternal{}, errors.WrapIff(err, "resolve docker host %q", dockerHost)
	}

	if scheme != "unix" {
		options.NetworkMode = containertypes.NetworkMode(selectTrivyAutoNetworkModeInternal(currentContainer))
		return options, nil
	}

	socketSource, err := resolveTrivyUnixSocketSourceInternal(
		ctx,
		socketPath,
		discoverHostPath,
		isRunningInDocker,
	)
	if err != nil {
		return upgraderRuntimeOptionsInternal{}, errors.WrapIf(err, "resolve unix socket source")
	}

	options.Mounts = append(options.Mounts, mounttypes.Mount{
		Type:   mounttypes.TypeBind,
		Source: socketSource,
		Target: socketPath,
	})

	return options, nil
}

// getCurrentContainerID detects if we're running in Docker and returns container ID
func (s *SystemUpgradeService) getCurrentContainerIDInternal() (string, error) {
	id, err := cgroup.CurrentContainerID()
	if err != nil {
		return "", errors.New("arcane is not running in a Docker container")
	}
	return id, nil
}

// findArcaneContainer finds the container using the ID
func (s *SystemUpgradeService) findArcaneContainerInternal(ctx context.Context, containerId string) (containertypes.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	// Try to inspect the container directly
	container, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerId, client.ContainerInspectOptions{})
	if err == nil {
		return container.Container, nil
	}

	// Fallback: search for containers with arcane image
	filter := make(client.Filters)
	filter = filter.Add("ancestor", "ghcr.io/getarcaneapp/arcane")

	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	for _, c := range containers.Items {
		if strings.HasPrefix(c.ID, containerId) {
			inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return containertypes.InspectResponse{}, inspectErr
			}
			return inspect.Container, nil
		}
	}

	// Try without filter - search all containers
	allContainers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return containertypes.InspectResponse{}, err
	}

	for _, c := range allContainers.Items {
		if strings.HasPrefix(c.ID, containerId) || c.ID == containerId {
			inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return containertypes.InspectResponse{}, inspectErr
			}
			return inspect.Container, nil
		}
	}

	return containertypes.InspectResponse{}, common.Classify(common.ErrNotFound, errors.

		// --- Fleet-wide "update all environments" orchestration ---
		//
		// Remote agents upgrade first, while the manager is still up and can orchestrate
		// and report live progress. The manager upgrades itself LAST, which recreates its
		// own container. Updates are unconditional: every environment pulls the latest
		// image whether or not it reports an update available. Because the browser loses
		// the backend across that final restart, the orchestration is a persisted
		// EnvironmentUpdateJob: StartUpdateAll runs the agents phase, triggers the manager
		// self-upgrade as the last step and leaves the job in pending_restart; on the next
		// boot ResumeUpdateAllOnStartup finalizes the manager result and closes the job.
		New("could not find Arcane container"))
}

const (
	localEnvironmentIDInternal     = "0"
	managerEnvironmentNameInternal = "Local"

	updateAllStaleThresholdInternal      = time.Hour
	updateAllAgentRequestTimeoutInternal = 15 * time.Second
	updateAllConfirmPollIntervalInternal = 10 * time.Second
	updateAllConfirmTimeoutInternal      = 2 * time.Minute
	updateAllErrorMaxLenInternal         = 500
)

// StartUpdateAll begins a fleet-wide update. The agents phase runs first in the
// background (while the manager is up); the agents goroutine triggers the manager
// self-upgrade as its final step (job left pending_restart, finalized at next
// boot). Every environment pulls the latest image, whether or not it reports an
// update available.
func (s *SystemUpgradeService) StartUpdateAll(ctx context.Context, user models.User, env *EnvironmentService) (*models.EnvironmentUpdateJob, error) {
	// Guard the check-then-create against concurrent callers (e.g. a double-click).
	// A dedicated flag rather than s.upgrading, because the manager branch below
	// calls TriggerUpgradeViaCLI which acquires s.upgrading itself. The persisted
	// job row is the durable guard once committed; this only closes the in-process
	// window before that row exists.
	if !s.updatingAll.CompareAndSwap(false, true) {
		return nil, common.Classify(common.ErrUpdateAllInProgress, errors.New("an update-all job is already in progress"))
	}
	defer s.updatingAll.Store(false)

	active, err := s.activeUpdateAllJobInternal(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "check for active update-all job")
	}
	if active != nil {
		return nil, common.Classify(common.ErrUpdateAllInProgress, errors.New("an update-all job is already in progress"))
	}

	info := s.versionService.GetAppVersionInfo(ctx)

	managerResult := models.EnvironmentUpdateResult{
		EnvironmentID:   localEnvironmentIDInternal,
		EnvironmentName: managerEnvironmentNameInternal,
		FromVersion:     info.CurrentVersion,
	}

	// Seed a pending row for every remote environment up front so the dialog can
	// show the whole fleet immediately instead of popping rows in as each finishes.
	// Best effort: the agents phase re-lists authoritatively and fills any gaps.
	remoteResults := s.seedRemoteResultsInternal(ctx, env)

	job := &models.EnvironmentUpdateJob{
		UserID:                user.ID,
		Username:              user.Username,
		ManagerVersionAtStart: info.CurrentVersion,
		ManagerDigestAtStart:  info.CurrentDigest,
		ManagerTargetVersion:  updateAllTargetVersionInternal(info),
	}

	// Manager upgrades LAST. Seed its row pending; the agents-phase goroutine
	// flips it to updating right before triggering the self-upgrade.
	managerResult.Status = models.EnvironmentUpdateResultStatusPending
	managerResult.ToVersion = job.ManagerTargetVersion

	// Running from the start: the agents phase happens first, while the backend is
	// up and can report progress. The manager self-upgrade fires at the very end of
	// the agents goroutine.
	job.Status = models.EnvironmentUpdateJobStatusRunning
	job.Results = append(models.EnvironmentUpdateResults{managerResult}, remoteResults...)

	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, errors.WrapIf(err, "create update-all job")
	}

	slog.InfoContext(ctx, "Update-all started; upgrading agents first", "jobId", job.ID, "user", user.Username)
	go s.runAgentsPhaseInternal(context.WithoutCancel(ctx), job.ID, env, user)

	return job, nil
}

// ResumeUpdateAllOnStartup is called once at manager startup. When the manager
// self-upgraded as the final step of an update-all (job left pending_restart), the
// agents phase already ran before the restart — so this finalizes the manager's own
// result and closes the job. It is a no-op when there is nothing pending.
func (s *SystemUpgradeService) ResumeUpdateAllOnStartup(ctx context.Context) {
	job, err := s.activeUpdateAllJobInternal(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load pending update-all job on startup", "error", err)
		return
	}
	if job == nil {
		return
	}

	// A job left running means the manager died mid-agents-phase, before it reached
	// the manager step; we can't safely resume partial progress, so fail it.
	if job.Status == models.EnvironmentUpdateJobStatusRunning {
		s.markUpdateAllFailedInternal(ctx, job, "interrupted by manager restart")
		return
	}

	info := s.versionService.GetAppVersionInfo(ctx)
	action := resolveResumeActionInternal(job, info.CurrentVersion, info.CurrentDigest, time.Now())

	if action.markStale {
		s.markUpdateAllFailedInternal(ctx, job, "update-all job is stale; manager did not restart in time")
		return
	}

	// The agents phase already ran before the restart. Finalize the manager's own
	// result and close the job — do not re-run the agents phase.
	s.recordManagerResultInternal(job, action.managerSucceeded, info.CurrentVersion)

	job.Status = models.EnvironmentUpdateJobStatusCompleted
	job.CompletedAt = new(time.Now())
	if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
		slog.WarnContext(ctx, "Failed to finalize resumed update-all job", "jobId", job.ID, "error", err)
		return
	}

	s.logUpdateAllEventInternal(ctx, job)
	slog.InfoContext(ctx, "Finalized update-all job after manager restart", "jobId", job.ID, "managerUpgraded", action.managerSucceeded)
}

type resumeActionInternal struct {
	markStale        bool
	managerSucceeded bool
}

// resolveResumeActionInternal is the pure decision for a resumed pending_restart
// job: stale if it has waited too long, otherwise the manager upgrade is considered
// successful when either the version or the digest changed from the at-start values
// (digest covers non-semver, digest-pinned installs), or when the manager is already
// on the recorded target — a force-update of an up-to-date manager recreates the
// container without changing either.
func resolveResumeActionInternal(job *models.EnvironmentUpdateJob, currentVersion, currentDigest string, now time.Time) resumeActionInternal {
	if now.Sub(job.CreatedAt) > updateAllStaleThresholdInternal {
		return resumeActionInternal{markStale: true}
	}

	versionChanged := job.ManagerVersionAtStart != "" && currentVersion != job.ManagerVersionAtStart
	digestChanged := job.ManagerDigestAtStart != "" && currentDigest != job.ManagerDigestAtStart
	// ManagerTargetVersion holds the newest version tag when known, otherwise the
	// newest digest — compare against both current identifiers.
	onTarget := job.ManagerTargetVersion != "" &&
		(strings.TrimPrefix(currentVersion, "v") == strings.TrimPrefix(job.ManagerTargetVersion, "v") ||
			currentDigest == job.ManagerTargetVersion)

	return resumeActionInternal{managerSucceeded: versionChanged || digestChanged || onTarget}
}

// runAgentsPhaseInternal upgrades every online remote environment sequentially,
// persisting progress after each one so the status endpoint can report live
// progress, then triggers the manager's own self-upgrade as the final step
// (leaving the job pending_restart for the next boot to finalize).
func (s *SystemUpgradeService) runAgentsPhaseInternal(ctx context.Context, jobID string, env *EnvironmentService, user models.User) {
	job, err := s.getUpdateAllJobByIDInternal(ctx, jobID)
	if err != nil || job == nil {
		slog.WarnContext(ctx, "update-all: failed to reload job for agents phase", "jobId", jobID, "error", err)
		return
	}

	envs, err := env.ListRemoteEnvironments(ctx)
	if err != nil {
		s.markUpdateAllFailedInternal(ctx, job, fmt.Sprintf("failed to list remote environments: %v", err))
		return
	}

	for _, remote := range envs {
		// Find the row seeded at job start (or append one if seeding missed this
		// environment) and mark it updating so the dialog shows a live indicator on
		// the row currently being processed.
		idx := upsertPendingResultInternal(job, remote.ID, remote.Name)
		job.Results[idx].Status = models.EnvironmentUpdateResultStatusUpdating
		if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
			slog.WarnContext(ctx, "update-all: failed to persist updating status", "jobId", job.ID, "environmentId", remote.ID, "error", err)
		}

		result := job.Results[idx]
		s.upgradeAgentInternal(ctx, env, remote.ID, &result)
		job.Results[idx] = result

		if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
			slog.WarnContext(ctx, "update-all: failed to persist progress", "jobId", job.ID, "environmentId", remote.ID, "error", err)
		}
	}

	// All remote agents processed. Handle the manager LAST: flip its row pending ->
	// updating and move the job to pending_restart, persisting BEFORE the trigger so
	// that if the manager dies the instant the upgrader starts, the next boot sees
	// pending_restart and finalizes it.
	for i := range job.Results {
		if job.Results[i].EnvironmentID == localEnvironmentIDInternal {
			job.Results[i].Status = models.EnvironmentUpdateResultStatusUpdating
			break
		}
	}
	job.Status = models.EnvironmentUpdateJobStatusPendingRestart
	if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
		slog.WarnContext(ctx, "update-all: failed to persist pending_restart before manager upgrade", "jobId", job.ID, "error", err)
	}

	if err := s.TriggerUpgradeViaCLI(ctx, user, updatertypes.SelfUpdateTarget{}); err != nil {
		// Agents already ran and no restart is coming, so finalize now. This flips
		// the manager's updating row to failed with the reason.
		s.markUpdateAllFailedInternal(ctx, job, fmt.Sprintf("manager upgrade trigger failed: %v", err))
		return
	}

	slog.InfoContext(ctx, "Update-all: agents done, manager self-upgrade triggered; finalizing on next boot", "jobId", job.ID)
}

// seedRemoteResultsInternal builds a pending result row for every remote environment
// so the dialog can render the whole fleet immediately. Best effort: on error it
// returns nil and the agents phase appends rows as it processes them.
func (s *SystemUpgradeService) seedRemoteResultsInternal(ctx context.Context, env *EnvironmentService) models.EnvironmentUpdateResults {
	envs, err := env.ListRemoteEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "update-all: failed to pre-list remote environments for seeding", "error", err)
		return nil
	}
	results := make(models.EnvironmentUpdateResults, 0, len(envs))
	for _, remote := range envs {
		results = append(results, models.EnvironmentUpdateResult{
			EnvironmentID:   remote.ID,
			EnvironmentName: remote.Name,
			Status:          models.EnvironmentUpdateResultStatusPending,
		})
	}
	return results
}

// upsertPendingResultInternal returns the index of the existing result row for envID,
// appending a new pending row when seeding missed it (e.g. the seed list failed, or a
// new environment was registered after the job started).
func upsertPendingResultInternal(job *models.EnvironmentUpdateJob, envID, envName string) int {
	for i := range job.Results {
		if job.Results[i].EnvironmentID == envID {
			return i
		}
	}
	job.Results = append(job.Results, models.EnvironmentUpdateResult{
		EnvironmentID:   envID,
		EnvironmentName: envName,
		Status:          models.EnvironmentUpdateResultStatusPending,
	})
	return len(job.Results) - 1
}

// upgradeAgentInternal triggers and confirms a single remote environment's
// self-upgrade, recording the outcome on result. The upgrade always runs — the
// agent pulls the latest image even when it reports no update available.
func (s *SystemUpgradeService) upgradeAgentInternal(ctx context.Context, env *EnvironmentService, envID string, result *models.EnvironmentUpdateResult) {
	versionCtx, cancel := context.WithTimeout(ctx, updateAllAgentRequestTimeoutInternal)
	var info version.Info
	err := env.ProxyJSONRequest(versionCtx, envID, http.MethodGet, "/api/app-version", nil, &info)
	cancel()
	if err != nil {
		result.Status = updateAllAgentFailureStatusInternal(err)
		result.Error = truncateUpdateAllErrorInternal(err)
		return
	}
	result.FromVersion = info.CurrentVersion
	result.ToVersion = updateAllTargetVersionInternal(&info)

	triggerCtx, cancel := context.WithTimeout(ctx, updateAllAgentRequestTimeoutInternal)
	resp, err := env.ExecuteRemoteRequest(triggerCtx, envID, http.MethodPost, "/api/environments/0/system/upgrade", nil)
	cancel()
	if err != nil {
		result.Status = models.EnvironmentUpdateResultStatusFailed
		result.Error = truncateUpdateAllErrorInternal(err)
		return
	}
	if err := resp.RequireSuccess(); err != nil {
		result.Status = models.EnvironmentUpdateResultStatusFailed
		result.Error = truncateUpdateAllErrorInternal(err)
		return
	}

	if s.confirmAgentUpgradedInternal(ctx, env, envID, info) {
		result.Status = models.EnvironmentUpdateResultStatusUpdated
	} else {
		// Upgrade fired but the new version was not confirmed within the wait window.
		result.Status = models.EnvironmentUpdateResultStatusTriggered
	}
}

// updateAllAgentFailureStatusInternal classifies a failed agent pre-check. An
// environment we actually reached but whose request failed or timed out is a real
// failure, not an offline skip: poll-mode agents connect on demand, so a slow
// tunnel round-trip surfaces here as a deadline even though the agent is online.
// Only errors that look like the environment was never reachable (no tunnel,
// connection refused, DNS failure, …) stay an offline skip.
func updateAllAgentFailureStatusInternal(err error) models.EnvironmentUpdateResultStatus {
	// The tunnel/connection was established but the request did not finish: it
	// either timed out or was canceled (e.g. the parent context was aborted).
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return models.EnvironmentUpdateResultStatusFailed
	}
	// The environment answered with a non-success status — reached, not offline.
	if _, ok := stderrors.AsType[*remenv.StatusError](err); ok {
		return models.EnvironmentUpdateResultStatusFailed
	}
	return models.EnvironmentUpdateResultStatusSkippedOffline
}

// confirmAgentUpgradedInternal polls the agent's version until it moves off the
// pre-upgrade baseline or reports the upgrade target, or the wait window elapses.
// The target is the same fallback-resolved identifier recorded in ToVersion, so a
// force-update of an already-latest agent — including one whose version check
// could not determine the latest release — confirms on a same-image recreation
// instead of timing out to triggered.
func (s *SystemUpgradeService) confirmAgentUpgradedInternal(ctx context.Context, env *EnvironmentService, envID string, baseline version.Info) bool {
	target := updateAllTargetVersionInternal(&baseline)
	deadline := time.Now().Add(updateAllConfirmTimeoutInternal)
	ticker := time.NewTicker(updateAllConfirmPollIntervalInternal)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			reqCtx, cancel := context.WithTimeout(ctx, updateAllAgentRequestTimeoutInternal)
			var info version.Info
			err := env.ProxyJSONRequest(reqCtx, envID, http.MethodGet, "/api/app-version", nil, &info)
			cancel()
			if err == nil {
				versionChanged := baseline.CurrentVersion != "" && info.CurrentVersion != baseline.CurrentVersion
				digestChanged := baseline.CurrentDigest != "" && info.CurrentDigest != baseline.CurrentDigest
				onTarget := target != "" &&
					(strings.TrimPrefix(info.CurrentVersion, "v") == strings.TrimPrefix(target, "v") ||
						info.CurrentDigest == target)
				if versionChanged || digestChanged || onTarget {
					return true
				}
			}
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}

// GetLatestUpdateAllJob returns the most recently created update-all job, or nil.
func (s *SystemUpgradeService) GetLatestUpdateAllJob(ctx context.Context) (*models.EnvironmentUpdateJob, error) {
	var jobs []models.EnvironmentUpdateJob
	if err := s.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(1).
		Find(&jobs).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

func (s *SystemUpgradeService) activeUpdateAllJobInternal(ctx context.Context) (*models.EnvironmentUpdateJob, error) {
	var jobs []models.EnvironmentUpdateJob
	if err := s.db.WithContext(ctx).
		Where("status IN ?", []string{
			string(models.EnvironmentUpdateJobStatusPendingRestart),
			string(models.EnvironmentUpdateJobStatusRunning),
		}).
		Order("created_at DESC").
		Limit(1).
		Find(&jobs).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

func (s *SystemUpgradeService) getUpdateAllJobByIDInternal(ctx context.Context, id string) (*models.EnvironmentUpdateJob, error) {
	var jobs []models.EnvironmentUpdateJob
	if err := s.db.WithContext(ctx).
		Where("id = ?", id).
		Limit(1).
		Find(&jobs).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

func (s *SystemUpgradeService) persistUpdateAllJobInternal(ctx context.Context, job *models.EnvironmentUpdateJob) error {
	return s.db.WithContext(ctx).Save(job).Error
}

func (s *SystemUpgradeService) markUpdateAllFailedInternal(ctx context.Context, job *models.EnvironmentUpdateJob, reason string) {
	job.Status = models.EnvironmentUpdateJobStatusFailed
	job.Error = &reason
	job.CompletedAt = new(time.Now())
	for i := range job.Results {
		if job.Results[i].Status == models.EnvironmentUpdateResultStatusUpdating {
			job.Results[i].Status = models.EnvironmentUpdateResultStatusFailed
			job.Results[i].Error = reason
		}
	}
	if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
		slog.WarnContext(ctx, "update-all: failed to mark job failed", "jobId", job.ID, "error", err)
	}

	// Surface the failure in the events audit log; the success path logs via
	// logUpdateAllEventInternal. LogUserEvent hardcodes an info-severity "completed"
	// title, so create the event directly with error severity and the reason.
	if _, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:        models.EventTypeSystemUpgrade,
		Severity:    models.EventSeverityError,
		Title:       "Update all environments failed",
		Description: reason,
		UserID:      new(job.UserID),
		Username:    new(job.Username),
		Metadata: models.JSON{
			"action":       "update_all_environments",
			"jobId":        job.ID,
			"environments": len(job.Results),
			"reason":       reason,
		},
	}); err != nil {
		slog.WarnContext(ctx, "update-all: failed to log failure event", "jobId", job.ID, "error", err)
	}

	slog.WarnContext(ctx, "Update-all job failed", "jobId", job.ID, "reason", reason)
}

// recordManagerResultInternal updates the manager (env "0") entry after the restart.
func (s *SystemUpgradeService) recordManagerResultInternal(job *models.EnvironmentUpdateJob, succeeded bool, currentVersion string) {
	for i := range job.Results {
		if job.Results[i].EnvironmentID != localEnvironmentIDInternal {
			continue
		}
		if succeeded {
			job.Results[i].Status = models.EnvironmentUpdateResultStatusUpdated
			job.Results[i].ToVersion = currentVersion
		} else {
			job.Results[i].Status = models.EnvironmentUpdateResultStatusFailed
			job.Results[i].Error = "manager version did not change after upgrade"
		}
		return
	}
}

func (s *SystemUpgradeService) logUpdateAllEventInternal(ctx context.Context, job *models.EnvironmentUpdateJob) {
	failed := 0
	for _, r := range job.Results {
		if r.Status == models.EnvironmentUpdateResultStatusFailed {
			failed++
		}
	}

	metadata := models.JSON{
		"action":       "update_all_environments",
		"jobId":        job.ID,
		"environments": len(job.Results),
		"failed":       failed,
	}

	// All environments succeeded: log the standard completed (info) event.
	if failed == 0 {
		if err := s.eventService.LogUserEvent(ctx, models.EventTypeSystemUpgrade, job.UserID, job.Username, metadata); err != nil {
			slog.WarnContext(ctx, "Failed to log update-all event", "jobId", job.ID, "error", err)
		}
		return
	}

	// The job ran to completion but some environments failed to update — record a
	// warning-severity event so those failures still show in the audit log.
	if _, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:        models.EventTypeSystemUpgrade,
		Severity:    models.EventSeverityWarning,
		Title:       "Update all environments completed with errors",
		Description: fmt.Sprintf("%d of %d environments failed to update", failed, len(job.Results)),
		UserID:      new(job.UserID),
		Username:    new(job.Username),
		Metadata:    metadata,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to log update-all event", "jobId", job.ID, "error", err)
	}
}

// updateAllTargetVersionInternal picks the best human-readable target identifier:
// the newest version tag if known, otherwise the newest digest. When the version
// check could not determine the latest release (offline, rate-limited) it falls
// back to the current identifiers: updates run unconditionally, so the target of a
// force-update with an unknown latest is wherever the pull lands — recording the
// current version keeps the resume check able to recognize a same-image recreation
// as success instead of finalizing it as failed.
func updateAllTargetVersionInternal(info *version.Info) string {
	if info == nil {
		return ""
	}
	if info.NewestVersion != "" {
		return info.NewestVersion
	}
	if info.NewestDigest != "" {
		return info.NewestDigest
	}
	if info.CurrentVersion != "" {
		return info.CurrentVersion
	}
	return info.CurrentDigest
}

func truncateUpdateAllErrorInternal(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > updateAllErrorMaxLenInternal {
		return msg[:updateAllErrorMaxLenInternal]
	}
	return msg
}
