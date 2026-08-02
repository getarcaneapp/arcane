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

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/vuln"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
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
	Mounts       []mount.Mount
	NetworkMode  container.NetworkMode
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

// AlreadyOnNewestImage reports whether this environment's version check is confident
// it already runs the newest image. A triggered upgrade still pulls, but will then
// find nothing to swap in and skip the recreate — so callers can use this to stop
// waiting for a restart that is not coming.
func (s *SystemUpgradeService) AlreadyOnNewestImage(ctx context.Context) bool {
	if s.versionService == nil {
		return false
	}
	return agentAlreadyOnTargetInternal(s.versionService.GetAppVersionInfo(ctx))
}

// TriggerUpgradeViaCLI spawns the upgrade CLI command in a separate container and
// returns that upgrader container's ID. This avoids self-termination issues by running
// the upgrade from outside. A zero-value target upgrades the current container to its
// own image tag; the updater engine passes an explicit target with the resolved new
// image. Update-all uses the returned ID to tell an upgrade that recreated this
// container from one that found nothing to do — see watchManagerUpgraderInternal.
func (s *SystemUpgradeService) TriggerUpgradeViaCLI(ctx context.Context, user models.User, target updater.SelfUpdateTarget) (string, error) {
	if !s.upgrading.CompareAndSwap(false, true) {
		return "", common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrade is already in progress"))
	}
	defer s.upgrading.Store(false)

	containerId := strings.TrimSpace(target.ContainerID)
	if containerId == "" {
		// Fall back to the container this process runs in
		var err error
		containerId, err = s.getCurrentContainerIDInternal()
		if err != nil {
			return "", errors.WrapIf(err, "get current container")
		}
	}

	currentContainer, err := s.findArcaneContainerInternal(ctx, containerId)
	if err != nil {
		return "", errors.WrapIf(err, "inspect container")
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
		return "", errors.WrapIf(err, "failed to connect to Docker")
	}

	// Pull the upgrader image first to ensure it exists
	slog.Info("Pulling upgrader image", "image", upgraderImage)

	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
	defer pullCancel()

	pullReader, err := dockerClient.ImagePull(pullCtx, upgraderImage, client.ImagePullOptions{})
	if err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return "", errors.Errorf("upgrader image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", upgraderImage)
		}
		return "", errors.WrapIf(err, "pull upgrader image")
	}
	// Drain and validate the JSON stream to complete the pull.
	if err := dockerutils.RenderJSONMessageStream(pullReader, io.Discard); err != nil {
		_ = pullReader.Close()
		return "", errors.WrapIf(err, "failed to complete upgrader image pull")
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
		return "", errors.WrapIf(err, "resolve upgrader docker runtime")
	}

	upgradeCmd := []string{binaryPath, "upgrade", "--container", containerName}
	if targetImage != "" {
		upgradeCmd = append(upgradeCmd, "--image", targetImage)
	}

	config := &container.Config{
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

	mounts := append([]mount.Mount{}, runtimeOptions.Mounts...)
	if appDataMount != nil {
		mounts = append(mounts, *appDataMount)
	}

	keepUpgraderContainer := strings.EqualFold(strings.TrimSpace(os.Getenv("ARCANE_UPGRADE_KEEP_CONTAINER")), "true")
	if keepUpgraderContainer {
		slog.Info("Keeping upgrader container after exit (ARCANE_UPGRADE_KEEP_CONTAINER=true)")
	}

	hostConfig := &container.HostConfig{
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
		return "", errors.WrapIf(err, "create upgrader container")
	}

	// Start the upgrader container - it will run the upgrade and auto-remove
	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		return "", errors.WrapIf(err, "start upgrader container")
	}

	slog.Info("Upgrade container started", "upgraderId", resp.ID[:12], "upgraderName", containerName)

	return resp.ID, nil
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

func determineUpgradeBinaryPathInternal(containerLabels map[string]string) string {
	if labels.IsArcaneAgentContainer(containerLabels) {
		return "/app/arcane-agent"
	}

	return "/app/arcane"
}

func resolveSystemUpgraderRuntimeOptionsInternal(
	ctx context.Context,
	dockerHost string,
	currentContainer *container.InspectResponse,
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
		options.NetworkMode = container.NetworkMode(selectTrivyAutoNetworkModeInternal(currentContainer))
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

	options.Mounts = append(options.Mounts, mount.Mount{
		Type:   mount.TypeBind,
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
func (s *SystemUpgradeService) findArcaneContainerInternal(ctx context.Context, containerId string) (container.InspectResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return container.InspectResponse{}, err
	}

	// Try to inspect the container directly
	inspectResult, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerId, client.ContainerInspectOptions{})
	if err == nil {
		return inspectResult.Container, nil
	}

	// Fallback: search for containers with arcane image
	filter := make(client.Filters)
	filter = filter.Add("ancestor", "ghcr.io/getarcaneapp/arcane")

	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return container.InspectResponse{}, err
	}

	for _, c := range containers.Items {
		if strings.HasPrefix(c.ID, containerId) {
			inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return container.InspectResponse{}, inspectErr
			}
			return inspect.Container, nil
		}
	}

	// Try without filter - search all containers
	allContainers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return container.InspectResponse{}, err
	}

	for _, c := range allContainers.Items {
		if strings.HasPrefix(c.ID, containerId) || c.ID == containerId {
			inspect, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, c.ID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				return container.InspectResponse{}, inspectErr
			}
			return inspect.Container, nil
		}
	}

	return container.InspectResponse{}, common.Classify(common.ErrNotFound, errors.

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
	updateAllStaleThresholdInternal      = time.Hour
	updateAllAgentRequestTimeoutInternal = 15 * time.Second
	updateAllConfirmPollIntervalInternal = 10 * time.Second
	updateAllConfirmTimeoutInternal      = 2 * time.Minute
	updateAllErrorMaxLenInternal         = 500

	// How long to watch the manager's upgrader container for an exit. Generous: it
	// pulls the target image before recreating anything.
	updateAllManagerWatchTimeoutInternal = 15 * time.Minute
	// How long to let a recreate that is already underway stop this container before
	// concluding the manager was up to date and no restart is coming.
	updateAllManagerNoRestartGraceInternal = 15 * time.Second
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
		EnvironmentID:   LocalEnvironmentID,
		EnvironmentName: env.ResolveEnvironmentName(ctx, LocalEnvironmentID),
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
	managerStatus := models.EnvironmentUpdateResultStatusFailed
	if action.managerSucceeded {
		managerStatus = models.EnvironmentUpdateResultStatusUpdated
	}
	s.recordManagerResultInternal(job, managerStatus, info.CurrentVersion)
	s.finalizeUpdateAllJobInternal(ctx, job)

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
		if job.Results[i].EnvironmentID == LocalEnvironmentID {
			job.Results[i].Status = models.EnvironmentUpdateResultStatusUpdating
			break
		}
	}
	job.Status = models.EnvironmentUpdateJobStatusPendingRestart
	if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
		slog.WarnContext(ctx, "update-all: failed to persist pending_restart before manager upgrade", "jobId", job.ID, "error", err)
	}

	// Snapshot whether the manager was already on the newest image BEFORE triggering:
	// only that makes a no-op upgrader run an expected outcome. Checked after the fact
	// the same answer proves nothing, since it was already true going in.
	wasAlreadyNewest := s.AlreadyOnNewestImage(ctx)

	upgraderID, err := s.TriggerUpgradeViaCLI(ctx, user, updater.SelfUpdateTarget{})
	if err != nil {
		// Agents already ran and no restart is coming, so finalize now. This flips
		// the manager's updating row to failed with the reason.
		s.markUpdateAllFailedInternal(ctx, job, fmt.Sprintf("manager upgrade trigger failed: %v", err))
		return
	}

	slog.InfoContext(ctx, "Update-all: agents done, manager self-upgrade triggered", "jobId", job.ID, "upgraderId", upgraderID)
	s.watchManagerUpgraderInternal(ctx, job.ID, upgraderID, wasAlreadyNewest)
}

// watchManagerUpgraderInternal closes out a job whose manager never restarted. The
// upgrader skips the container recreate when the pull lands on the image already
// running, so this process survives it — and the job, persisted pending_restart for
// the next boot to finalize, would otherwise wait for a restart that never comes.
//
// Runs at the tail of the agents-phase goroutine. When the manager IS recreated this
// process is stopped instead and ResumeUpdateAllOnStartup finalizes on the next boot.
// wasAlreadyNewest is the pre-trigger version check, the only evidence that a run
// which changed nothing was supposed to change nothing.
func (s *SystemUpgradeService) watchManagerUpgraderInternal(ctx context.Context, jobID, upgraderID string, wasAlreadyNewest bool) {
	exit, exitCode := s.waitForUpgraderExitInternal(ctx, upgraderID)
	if exit == upgraderExitUnobservedInternal {
		// Never saw it finish, so a recreate may still be coming — but it may equally
		// never come, and pending_restart is only ever resolved at boot. Wait the run
		// out rather than returning: an unobserved exit must not strand the job.
		s.closeOutUnobservedUpgradeInternal(ctx, jobID)
		return
	}

	// A recreate stops this container, but not instantly — the stop carries a grace
	// period during which this goroutine still runs. Wait it out so a restart that is
	// already underway wins: if the manager is being replaced, the process dies here.
	time.Sleep(updateAllManagerNoRestartGraceInternal)

	job, err := s.getUpdateAllJobByIDInternal(ctx, jobID)
	if err != nil || job == nil {
		slog.WarnContext(ctx, "update-all: failed to reload job after upgrader exit", "jobId", jobID, "error", err)
		return
	}
	if job.Status != models.EnvironmentUpdateJobStatusPendingRestart {
		return
	}

	if exit == upgraderExitFailedInternal {
		s.markUpdateAllFailedInternal(ctx, job, fmt.Sprintf("manager upgrade failed: upgrader exited with code %d", exitCode))
		return
	}

	info := s.versionService.GetAppVersionInfo(ctx)

	// Still running here means the container was never recreated, so the upgrader either
	// found the image already current or died before it got that far. With no exit code
	// to tell those apart, a no-op is only credible when the pre-trigger check already
	// said there was nothing to swap in and the post-run check still agrees: an after-
	// the-fact "already newest" on its own restates what was true before the run, so it
	// cannot vouch for it — and on a manager that was due an update it would launder a
	// dead upgrader (stale newest-image state, a check that regressed to the running
	// image) into a green row and zero logged failures, leaving a broken upgrade with no
	// retry path. Anything less is a failure; the job closes either way, since no restart
	// is coming.
	if exit == upgraderExitCodeLostInternal && (!wasAlreadyNewest || !agentAlreadyOnTargetInternal(info)) {
		s.markUpdateAllFailedInternal(ctx, job, "manager upgrade could not be confirmed: the upgrader exited without a readable status and this manager was not a confirmed no-op upgrade")
		return
	}

	s.recordManagerResultInternal(job, models.EnvironmentUpdateResultStatusUpToDate, info.CurrentVersion)
	slog.InfoContext(ctx, "Update-all: manager was already up to date; no restart needed", "jobId", job.ID, "version", info.CurrentVersion)
	s.finalizeUpdateAllJobInternal(ctx, job)
}

// closeOutUnobservedUpgradeInternal fails a job whose upgrader outcome was never
// observed and whose manager then never restarted. Waiting is what makes the answer
// safe: a recreate kills this process, so still being here once the job would be
// considered stale proves no restart is coming. Nothing else would close it — only a
// boot resolves pending_restart — and until it closes, every later update-all is
// refused as already in progress.
func (s *SystemUpgradeService) closeOutUnobservedUpgradeInternal(ctx context.Context, jobID string) {
	job, err := s.getUpdateAllJobByIDInternal(ctx, jobID)
	if err != nil || job == nil {
		slog.WarnContext(ctx, "update-all: failed to reload job after unobserved upgrader exit", "jobId", jobID, "error", err)
		return
	}
	if job.Status != models.EnvironmentUpdateJobStatusPendingRestart {
		return
	}

	// Hold until the job hits the same staleness bound a resumed job is judged by, so a
	// slow upgrader still gets its full window to recreate this container.
	if remaining := time.Until(job.CreatedAt.Add(updateAllStaleThresholdInternal)); remaining > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(remaining):
		}
	}

	job, err = s.getUpdateAllJobByIDInternal(ctx, jobID)
	if err != nil || job == nil {
		slog.WarnContext(ctx, "update-all: failed to reload stale job", "jobId", jobID, "error", err)
		return
	}
	if job.Status != models.EnvironmentUpdateJobStatusPendingRestart {
		return
	}

	slog.WarnContext(ctx, "update-all: manager upgrade was never observed and no restart followed", "jobId", job.ID)
	s.markUpdateAllFailedInternal(ctx, job, "manager upgrade could not be observed: the upgrader run was never seen to finish and the manager did not restart")
}

// upgraderExitInternal is how much the watcher managed to learn about an upgrader run.
type upgraderExitInternal int

const (
	// The run never finished observably: nothing may be concluded from it.
	upgraderExitUnobservedInternal upgraderExitInternal = iota
	// The upgrader exited 0.
	upgraderExitSucceededInternal
	// The upgrader exited non-zero.
	upgraderExitFailedInternal
	// The upgrader is gone, but its exit code was lost with it — success and failure
	// are indistinguishable.
	upgraderExitCodeLostInternal
)

// waitForUpgraderExitInternal blocks until the upgrader container stops, reporting what
// could be learned about how it ended along with its exit code when one was observed.
func (s *SystemUpgradeService) waitForUpgraderExitInternal(ctx context.Context, upgraderID string) (upgraderExitInternal, int64) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.WarnContext(ctx, "update-all: cannot watch upgrader container", "upgraderId", upgraderID, "error", err)
		return upgraderExitUnobservedInternal, 0
	}

	waitCtx, cancel := context.WithTimeout(ctx, updateAllManagerWatchTimeoutInternal)
	defer cancel()

	wait := dockerClient.ContainerWait(waitCtx, upgraderID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})
	select {
	case result, ok := <-wait.Result:
		// A closed channel hands back a zero-value response, which is indistinguishable
		// from a clean exit 0 — nothing was delivered, so nothing may be concluded.
		if !ok {
			slog.WarnContext(ctx, "update-all: upgrader wait ended without a status", "upgraderId", upgraderID)
			return upgraderExitUnobservedInternal, 0
		}
		if result.StatusCode != 0 {
			return upgraderExitFailedInternal, result.StatusCode
		}
		// The daemon fills in status code 0 alongside a wait error (the container was
		// removed mid-wait, the wait itself broke): the exit is real but its code is not.
		if result.Error != nil {
			slog.WarnContext(ctx, "update-all: upgrader wait reported an error", "upgraderId", upgraderID, "error", result.Error.Message)
			return upgraderExitCodeLostInternal, 0
		}
		return upgraderExitSucceededInternal, 0
	case err, ok := <-wait.Error:
		// The upgrader runs with AutoRemove, so a fast run can be gone before the wait
		// is acknowledged. It exited, but the code went with it — a failed upgrade must
		// not be mistaken for a clean no-op, so report the ambiguity rather than a code.
		if cerrdefs.IsNotFound(err) {
			return upgraderExitCodeLostInternal, 0
		}
		if !ok || err == nil {
			slog.WarnContext(ctx, "update-all: upgrader wait ended without a status", "upgraderId", upgraderID)
		} else {
			slog.WarnContext(ctx, "update-all: failed waiting for upgrader container", "upgraderId", upgraderID, "error", err)
		}
		return upgraderExitUnobservedInternal, 0
	case <-waitCtx.Done():
		slog.WarnContext(ctx, "update-all: timed out waiting for upgrader container", "upgraderId", upgraderID)
		return upgraderExitUnobservedInternal, 0
	}
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

	// An agent that reports no update available and already runs the target has
	// nothing to swap in: its upgrader pulls, finds the same image and skips the
	// recreate. Confirming that would only burn the poll window waiting for a version
	// change that cannot come, so record it directly.
	if agentAlreadyOnTargetInternal(&info) {
		result.Status = models.EnvironmentUpdateResultStatusUpToDate
		result.ToVersion = info.CurrentVersion
		return
	}

	if s.confirmAgentUpgradedInternal(ctx, env, envID, info) {
		result.Status = models.EnvironmentUpdateResultStatusUpdated
	} else {
		// Upgrade fired but the new version was not confirmed within the wait window.
		result.Status = models.EnvironmentUpdateResultStatusTriggered
	}
}

// agentAlreadyOnTargetInternal reports whether a version check is confident that the
// environment already runs the newest image. Only a positive answer is actionable:
// anything unresolved or contradictory reports false, which keeps the full
// trigger-and-confirm flow rather than claiming there was nothing to do.
func agentAlreadyOnTargetInternal(info *version.Info) bool {
	if info == nil || info.UpdateAvailable {
		return false
	}

	currentDigest := strings.TrimSpace(info.CurrentDigest)
	newestDigest := strings.TrimSpace(info.NewestDigest)

	// Digests are the only thing that catches a mutable tag rebuilt at the same version,
	// since the semver track's UpdateAvailable ignores them entirely. Once either side
	// resolves, both must resolve and agree: with only one in hand there is no way to
	// rule out that the pull is about to replace the image, so a matching version tag
	// must not be trusted on its own.
	if currentDigest != "" || newestDigest != "" {
		return currentDigest == newestDigest
	}

	// No digest information at all — the resolved version tag is all there is to go on.
	if info.NewestVersion != "" {
		return strings.TrimPrefix(info.NewestVersion, "v") == strings.TrimPrefix(info.CurrentVersion, "v")
	}
	return false
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

// recordManagerResultInternal sets the manager (env "0") entry to its final status:
// updated after a confirmed restart, up_to_date when the pull found nothing to swap
// in, or failed otherwise.
func (s *SystemUpgradeService) recordManagerResultInternal(job *models.EnvironmentUpdateJob, status models.EnvironmentUpdateResultStatus, currentVersion string) {
	for i := range job.Results {
		if job.Results[i].EnvironmentID != LocalEnvironmentID {
			continue
		}
		job.Results[i].Status = status
		switch status {
		case models.EnvironmentUpdateResultStatusUpdated, models.EnvironmentUpdateResultStatusUpToDate:
			job.Results[i].ToVersion = currentVersion
		case models.EnvironmentUpdateResultStatusFailed:
			job.Results[i].Error = "manager version did not change after upgrade"
		case models.EnvironmentUpdateResultStatusPending,
			models.EnvironmentUpdateResultStatusUpdating,
			models.EnvironmentUpdateResultStatusTriggered,
			models.EnvironmentUpdateResultStatusSkippedOffline:
			// Nothing more to record: the row keeps the target version it was seeded
			// with, since none of these outcomes establishes what it ended up running.
		}
		return
	}
}

// finalizeUpdateAllJobInternal closes a job out as completed and records the audit
// event. The per-environment rows must already carry their final statuses.
func (s *SystemUpgradeService) finalizeUpdateAllJobInternal(ctx context.Context, job *models.EnvironmentUpdateJob) {
	job.Status = models.EnvironmentUpdateJobStatusCompleted
	job.CompletedAt = new(time.Now())
	if err := s.persistUpdateAllJobInternal(ctx, job); err != nil {
		slog.WarnContext(ctx, "update-all: failed to finalize job", "jobId", job.ID, "error", err)
		return
	}
	s.logUpdateAllEventInternal(ctx, job)
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
