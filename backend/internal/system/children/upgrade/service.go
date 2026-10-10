package upgrade

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	versiontypes "github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/refs"
	updatertypes "go.getarcane.app/updater/types"
	"golang.org/x/mod/semver"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

var (
	upgradeLogName = regexp.MustCompile(`^arcane-upgrade-\d+\.log$`)
	// exactReleaseTag pins an exact release (vX.Y.Z, prereleases included); channels like v2 or v2.9 are mutable.
	exactReleaseTag         = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+].*)?$`)
	activeUpdateAllStatuses = []EnvironmentUpdateJobStatus{EnvironmentUpdateJobStatusPendingRestart, EnvironmentUpdateJobStatusRunning}
	errUpdateAllInProgress  = common.Classify(common.ErrUpdateAllInProgress, errors.New("an update-all job is already in progress"))
)

const (
	// The run never finished observably: nothing may be concluded from it.
	upgraderExitUnobserved upgraderExit = 0
	upgraderExitSucceeded  upgraderExit = 1
	upgraderExitFailed     upgraderExit = 2
	// The upgrader is gone and its exit code with it: success and failure are indistinguishable.
	upgraderExitCodeLost upgraderExit = 3

	updateAllStaleThreshold      = time.Hour
	updateAllAgentRequestTimeout = 15 * time.Second
	updateAllConfirmPollInterval = 10 * time.Second
	updateAllConfirmTimeout      = 5 * time.Minute
	updateAllErrorMaxLen         = 500
	updateAllWorkflowName        = "update-all"
	// Generous: the upgrader pulls the target image before recreating anything.
	updateAllManagerWatchTimeout = 15 * time.Minute
	// How long a recreate already underway gets to stop this container before no restart is assumed.
	updateAllManagerNoRestartGrace = 15 * time.Second
)

type Service struct {
	upgrading    atomic.Bool
	updatingAll  atomic.Bool
	flow         *flow.Engine
	environments *environment.EnvironmentService
	roles        *role.RoleService
	// updateAllWorkflow upgrades every agent in turn, then the manager last.
	updateAllWorkflow *flow.Workflow
	db                *database.DB
	dockerService     *dockerInternal.DockerClientService
	versionService    *version.VersionService
	eventService      *event.EventService
	settingsService   *settings.SettingsService
	projectService    *project.ProjectService
	// resolveRuntimeOptions determines how the upgrader container reaches the Docker daemon.
	resolveRuntimeOptions func(
		ctx context.Context,
		dockerHost string,
		currentContainer *container.InspectResponse,
		discoverHostPath func(context.Context, string) (string, error),
		isRunningInDocker func() bool,
		selectReachableNetwork func(context.Context, *container.InspectResponse, string) string,
	) ([]string, []mount.Mount, container.NetworkMode, error)
}

// remoteAgent is one environment the update-all workflow upgrades.
type remoteAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// updateAllInput is the update-all payload: the job and the API key that requested it.
type updateAllInput struct {
	JobID string `json:"jobId"`
	KeyID string `json:"keyId,omitempty"`
}

type preparedUpgrade struct {
	dockerClient  *client.Client
	current       container.InspectResponse
	containerName string
	binaryPath    string
	// targetImage is what the recreated container runs; pullImage is the immutable reference pulled (same unless frozen).
	targetImage string
	pullImage   string
	// saveCompose persists a tag change in the container's Compose project.
	saveCompose func(context.Context)
}

type resumeAction struct {
	markStale        bool
	managerSucceeded bool
}

// upgraderExit is how much the watcher learned about an upgrader run.
type upgraderExit int

func NewService(
	db *database.DB,
	dockerService *dockerInternal.DockerClientService,
	versionService *version.VersionService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	projectService *project.ProjectService,
	resolveRuntimeOptions func(
		ctx context.Context,
		dockerHost string,
		currentContainer *container.InspectResponse,
		discoverHostPath func(context.Context, string) (string, error),
		isRunningInDocker func() bool,
		selectReachableNetwork func(context.Context, *container.InspectResponse, string) string,
	) ([]string, []mount.Mount, container.NetworkMode, error),
) *Service {
	return &Service{
		db:                    db,
		dockerService:         dockerService,
		versionService:        versionService,
		eventService:          eventService,
		settingsService:       settingsService,
		projectService:        projectService,
		resolveRuntimeOptions: resolveRuntimeOptions,
	}
}

// RegisterWorkflows defines the update-all workflow while the host is still unstarted. Its payload names the job;
// the job row holds every result, so a redelivered task resumes from what the row records.
func (s *Service) RegisterWorkflows(engine *flow.Engine, environments *environment.EnvironmentService, roles *role.RoleService) error {
	var err error
	s.flow, s.environments, s.roles = engine, environments, roles
	s.updateAllWorkflow, err = engine.Define(flow.Definition{
		Name:        updateAllWorkflowName,
		Version:     1,
		Fingerprint: "65b33dbbe55a5018517b2e8506d9b487f322803ed5a9dfec0edbf5103c96e586",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Steps: []workflow.StepSpec{
			workflow.Step("discover", engine.Handler(s.discoverAgents), workflow.WithMaxAttempts(1)),
			workflow.ForEach("agents", engine.Handler(s.upgradeAgentTask), workflow.WithItemsFrom("discover"), workflow.WithMaxParallel(1),
				workflow.WithMaxAttempts(1), workflow.WithFailurePolicy(workflow.TolerateFailures)),
			workflow.Step("manager", engine.Handler(s.upgradeManagerTask), workflow.WithMaxAttempts(1)),
		},
	})
	return err
}

// CanUpgrade checks if self-upgrade is possible.
func (s *Service) CanUpgrade(ctx context.Context) (bool, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return false, errors.New("docker socket is not accessible")
	}
	if _, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient); inspectErr != nil {
		return false, errors.New("arcane is not running in a Docker container")
	}
	return true, nil
}

// AlreadyOnNewestImage reports whether the version check is confident this environment runs the newest image,
// so a triggered upgrade will find nothing to swap in and no restart will follow.
func (s *Service) AlreadyOnNewestImage(ctx context.Context) bool {
	return s.versionService.GetAppVersionInfo(ctx).AlreadyOnNewest()
}

// TriggerUpgradeViaCLI runs the upgrade from a separate upgrader container and returns its ID.
// A zero-value target resolves the image from the version check; an explicit target is used as-is.
func (s *Service) TriggerUpgradeViaCLI(ctx context.Context, user usertypes.Actor, target updater.SelfUpdateTarget) (string, error) {
	prepared, err := s.prepareUpgrade(ctx, user, target, "")
	if err != nil {
		return "", err
	}
	return s.runPreparedUpgrade(ctx, prepared)
}

// TriggerUpgradeAsync validates synchronously, then pulls and spawns the upgrader in the background (#3628).
// The run follows the app lifecycle with a deadline so a stalled daemon cannot hold the upgrading guard forever.
func (s *Service) TriggerUpgradeAsync(ctx context.Context, user usertypes.Actor, targetVersion string) error {
	prepared, err := s.prepareUpgrade(ctx, user, updater.SelfUpdateTarget{}, targetVersion)
	if err != nil {
		return err
	}
	localSettings := s.settingsService.GetSettingsConfig()
	runTimeout := timeouts.GetDuration(localSettings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull) + time.Minute
	runCtx, cancel := context.WithTimeout(utils.ActivityRuntimeContext(ctx, nil), runTimeout)
	go func() {
		defer cancel()
		if _, runPreparedUpgradeErr := s.runPreparedUpgrade(runCtx, prepared); runPreparedUpgradeErr != nil {
			slog.ErrorContext(ctx, "Background self-upgrade failed", "error", runPreparedUpgradeErr, "targetImage", prepared.targetImage)
		}
	}()
	return nil
}

// prepareUpgrade takes the upgrading guard, released by runPreparedUpgrade (or here on error).
func (s *Service) prepareUpgrade(ctx context.Context, user usertypes.Actor, target updater.SelfUpdateTarget, targetVersion string) (prepared *preparedUpgrade, err error) {
	if !s.upgrading.CompareAndSwap(false, true) {
		return nil, common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrade is already in progress"))
	}
	defer func() {
		if err != nil {
			s.upgrading.Store(false)
		}
	}()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	var current container.InspectResponse
	if containerID := strings.TrimSpace(target.ContainerID); containerID != "" {
		inspect, inspectErr := compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			// Fall back to a prefix match across every container.
			containers, listErr := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
			if listErr != nil {
				return nil, fmt.Errorf("inspect container: %w", listErr)
			}
			idx := slices.IndexFunc(containers.Items, func(c container.Summary) bool { return strings.HasPrefix(c.ID, containerID) })
			if idx < 0 {
				return nil, fmt.Errorf("inspect container: %w", common.Classify(common.ErrNotFound, errors.New("could not find Arcane container")))
			}
			if inspect, inspectErr = compat.ContainerInspectWithCompatibility(ctx, dockerClient, containers.Items[idx].ID, client.ContainerInspectOptions{}); inspectErr != nil {
				return nil, fmt.Errorf("inspect container: %w", inspectErr)
			}
		}
		current = inspect.Container
	} else {
		self, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
		if inspectErr != nil {
			return nil, errors.New("get current container: arcane is not running in a Docker container")
		}
		current = *self
	}

	containerName := strings.TrimPrefix(current.Name, "/")
	config := kit.FromPtr(current.Config)
	binaryPath := kit.Ternary(labels.IsArcaneAgentContainer(config.Labels), "/app/arcane-agent", "/app/arcane")

	targetImage := strings.TrimSpace(target.NewImageRef)
	if targetImage == "" {
		// A blank target resolves against the version check; a manager-supplied version outranks this instance's own.
		info := kit.FromPtr(s.versionService.GetAppVersionInfo(ctx))
		info.NewestVersion = cmp.Or(strings.TrimSpace(targetVersion), info.NewestVersion)
		if targetImage, err = resolveSelfUpgradeTargetImage(config.Image, &info); err != nil {
			return nil, fmt.Errorf("resolve upgrade target image: %w", err)
		}
	}
	targetImage, saveCompose, err := s.configuredTargetImage(ctx, current, targetImage)
	if err != nil {
		return nil, err
	}
	pullImage := cmp.Or(strings.TrimSpace(target.PullImageRef), targetImage)

	metadata := database.JSON{
		"action":        "system_upgrade_cli",
		"containerId":   current.ID,
		"containerName": containerName,
		"method":        "cli",
		"targetImage":   targetImage,
		"pullImage":     pullImage,
	}
	if logUserEventErr := s.eventService.LogUserEvent(ctx, event.EventTypeSystemUpgrade, user.ID, user.Username, metadata); logUserEventErr != nil {
		slog.WarnContext(ctx, "Failed to log upgrade event", "error", logUserEventErr)
	}

	return &preparedUpgrade{
		dockerClient:  dockerClient,
		current:       current,
		containerName: containerName,
		binaryPath:    binaryPath,
		targetImage:   targetImage,
		pullImage:     pullImage,
		saveCompose:   saveCompose,
	}, nil
}

func (s *Service) runPreparedUpgrade(ctx context.Context, prepared *preparedUpgrade) (string, error) {
	defer s.upgrading.Store(false)

	// The upgrader runs from the target image, so the upgrade CLI is the new version.
	dockerClient := prepared.dockerClient
	slog.InfoContext(ctx, "Pulling upgrader image", "containerName", prepared.containerName, "image", prepared.pullImage)

	localSettings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
	defer pullCancel()

	pullReader, err := dockerClient.ImagePull(pullCtx, prepared.pullImage, client.ImagePullOptions{})
	if err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("upgrader image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", prepared.pullImage)
		}
		return "", fmt.Errorf("pull upgrader image: %w", err)
	}
	// Drain and validate the JSON stream to complete the pull.
	if renderJSONMessageStreamErr := docker.RenderJSONMessageStream(pullReader, io.Discard); renderJSONMessageStreamErr != nil {
		_ = pullReader.Close()
		return "", fmt.Errorf("failed to complete upgrader image pull: %w", renderJSONMessageStreamErr)
	}
	if closeErr := pullReader.Close(); closeErr != nil {
		slog.WarnContext(ctx, "Failed to close upgrader image pull reader", "error", closeErr)
	}
	slog.InfoContext(ctx, "Upgrader image pulled successfully", "image", prepared.pullImage)
	if prepared.saveCompose != nil {
		prepared.saveCompose(ctx)
	}

	// Mount /app/data from the current container so upgrade logs persist.
	appDataMount := docker.MountForDestination(prepared.current.Mounts, libarcane.UpgradeLogDirectory, libarcane.UpgradeLogDirectory)
	if appDataMount == nil {
		slog.WarnContext(ctx, "Could not detect /app/data mount; upgrader logs may not persist")
	} else {
		slog.DebugContext(ctx, "Mounting /app/data into upgrader container", "type", appDataMount.Type, "source", appDataMount.Source)
	}

	containerEnv, runtimeMounts, networkMode, err := s.resolveRuntimeOptions(
		ctx,
		s.dockerService.DockerHost(),
		&prepared.current,
		func(ctx context.Context, containerPath string) (string, error) {
			return projects.GetHostPathForContainerPath(ctx, dockerClient, containerPath)
		},
		func() bool {
			_, currentContainerIDErr := cgroup.CurrentContainerID()
			return currentContainerIDErr == nil
		},
		func(ctx context.Context, inspect *container.InspectResponse, dockerHost string) string {
			return docker.SelectDockerHostReachableNetworkMode(ctx, dockerClient, inspect, dockerHost)
		},
	)
	if err != nil {
		return "", fmt.Errorf("resolve upgrader docker runtime: %w", err)
	}

	upgradeCmd := []string{prepared.binaryPath, "upgrade", "--container", prepared.containerName}
	if prepared.targetImage != "" {
		upgradeCmd = append(upgradeCmd, "--image", prepared.targetImage)
	}
	if prepared.pullImage != prepared.targetImage {
		upgradeCmd = append(upgradeCmd, "--pull-image", prepared.pullImage)
	}

	config := &container.Config{
		Image: prepared.pullImage,
		Cmd:   upgradeCmd,
		// Root for the Docker socket: the short-lived upgrader never goes through the runtime-identity drop.
		User: "0:0",
		Env:  containerEnv,
		Labels: map[string]string{
			"com.getarcaneapp.arcane.upgrader": "true",
			"com.getarcaneapp.arcane":          "true",
		},
	}

	mounts := append([]mount.Mount{}, runtimeMounts...)
	if appDataMount != nil {
		mounts = append(mounts, *appDataMount)
	}

	keepUpgraderContainer, _ := kit.ParseBool(os.Getenv("ARCANE_UPGRADE_KEEP_CONTAINER"))
	if keepUpgraderContainer {
		slog.InfoContext(ctx, "Keeping upgrader container after exit (ARCANE_UPGRADE_KEEP_CONTAINER=true)")
	}

	hostConfig := &container.HostConfig{
		AutoRemove:  !keepUpgraderContainer,
		Mounts:      mounts,
		NetworkMode: networkMode,
	}
	// Inherit the security context (SELinux label, privileged) that lets Arcane reach the socket on hardened hosts.
	if prepared.current.HostConfig != nil {
		hostConfig.SecurityOpt = slices.Clone(prepared.current.HostConfig.SecurityOpt)
		hostConfig.Privileged = prepared.current.HostConfig.Privileged
	}
	// On SELinux-enforcing hosts the socket is container_var_run_t, so without a label opt the upgrader exits with EACCES.
	hasLabelOpt := slices.ContainsFunc(hostConfig.SecurityOpt, func(opt string) bool { return strings.HasPrefix(strings.TrimSpace(opt), "label") })
	if !hostConfig.Privileged && !hasLabelOpt {
		infoResult, infoErr := dockerClient.Info(ctx, client.InfoOptions{})
		if infoErr != nil {
			slog.DebugContext(ctx, "Failed to query daemon info for SELinux detection", "error", infoErr)
		} else if slices.Contains(infoResult.Info.SecurityOptions, "name=selinux") {
			hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "label=disable")
		}
	}

	upgraderName := fmt.Sprintf("%s-upgrader-%d", prepared.containerName, time.Now().Unix())
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
		Name:       upgraderName,
	})
	if err != nil {
		return "", fmt.Errorf("create upgrader container: %w", err)
	}

	if _, containerStartErr := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); containerStartErr != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		return "", fmt.Errorf("start upgrader container: %w", containerStartErr)
	}

	slog.InfoContext(ctx, "Upgrade container started", "upgraderId", resp.ID[:12], "upgraderName", upgraderName)
	return resp.ID, nil
}

// configuredTargetImage spells targetImage like the container's Compose service (or its runtime image when unmanaged).
// A tag change also returns the Compose edit to save before the upgrader starts.
func (s *Service) configuredTargetImage(ctx context.Context, current container.InspectResponse, targetImage string) (string, func(context.Context), error) {
	// Digest targets cannot be written as Compose tags.
	if refs.NormalizeImageUpdateRef(targetImage) == "" || current.Config == nil {
		return targetImage, nil, nil
	}
	runtimeRef := current.Config.Image
	projectName, serviceName := docker.ComposeProjectLabel(current.Config.Labels), docker.ComposeServiceLabel(current.Config.Labels)
	if projectName == "" || serviceName == "" {
		return refs.PreserveConfiguredRef(runtimeRef, targetImage), nil, nil
	}
	projectID, composeRef, err := s.projectService.ComposeServiceImage(ctx, projectName, serviceName)
	if errors.Is(err, common.ErrNotFound) {
		return refs.PreserveConfiguredRef(runtimeRef, targetImage), nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("resolve Compose service %s/%s: %w", projectName, serviceName, err)
	}
	selected := refs.PreserveConfiguredRef(composeRef, targetImage)
	normalizedCompose := refs.NormalizeImageUpdateRef(composeRef)
	if normalizedCompose == refs.NormalizeImageUpdateRef(targetImage) {
		return selected, nil, nil
	}
	if normalizedCompose != refs.NormalizeImageUpdateRef(runtimeRef) {
		return "", nil, fmt.Errorf("compose service %s/%s image %s does not match running image %s", projectName, serviceName, composeRef, runtimeRef)
	}
	changes := map[string]updatertypes.ServiceImageChange{serviceName: {ExpectedRef: runtimeRef, TargetRef: selected}}
	return selected, func(ctx context.Context) {
		if _, saveErr := s.projectService.SaveProjectServiceImages(ctx, projectID, changes); saveErr != nil {
			slog.WarnContext(ctx, "Compose image not updated for self-upgrade; update it manually", "project", projectName, "service", serviceName, "image", selected, "error", saveErr)
		}
	}, nil
}

// resolveSelfUpgradeTargetImage moves exact release tags to the newest release and digest pins to the newest digest;
// mutable channels keep their reference. Unresolved or older exact-version targets fail (#3687).
func resolveSelfUpgradeTargetImage(currentImageRef string, info *versiontypes.Info) (string, error) {
	currentImageRef = strings.TrimSpace(currentImageRef)
	if currentImageRef == "" {
		return "", errors.New("running container has no image reference to upgrade from")
	}

	parsed, err := reference.Parse(currentImageRef)
	if err != nil {
		return "", fmt.Errorf("parse current image reference %q: %w", currentImageRef, err)
	}
	named, ok := parsed.(reference.Named)
	if !ok {
		return "", fmt.Errorf("current image reference %q is not a named image", currentImageRef)
	}

	newest := kit.FromPtr(info)
	newestDigest, newestVersion := strings.TrimSpace(newest.NewestDigest), strings.TrimSpace(newest.NewestVersion)

	if _, isDigested := parsed.(reference.Digested); isDigested {
		if newestDigest == "" {
			return "", errors.New("image is digest-pinned but no newest digest could be resolved; refusing an upgrade that could not move it")
		}
		targetDigest := digest.Digest(newestDigest)
		if validateErr := targetDigest.Validate(); validateErr != nil {
			return "", fmt.Errorf("resolved newest digest %q is not a valid digest: %w", newestDigest, validateErr)
		}
		withDigest, withDigestErr := reference.WithDigest(named, targetDigest)
		if withDigestErr != nil {
			return "", fmt.Errorf("build target reference for %q: %w", named.Name(), withDigestErr)
		}
		return withDigest.String(), nil
	}

	tagged, isTagged := parsed.(reference.Tagged)
	if !isTagged || !exactReleaseTag.MatchString(tagged.Tag()) {
		// The pull re-resolves a mutable channel, so keeping the reference advances the digest in place.
		return currentImageRef, nil
	}

	if newestVersion == "" {
		return "", fmt.Errorf("running exact release %q but the newest release could not be resolved", tagged.Tag())
	}
	newestTag := kit.EnsurePrefix(newestVersion, "v")
	if !semver.IsValid(newestTag) {
		return "", fmt.Errorf("resolved newest version %q is not a valid semver release", newestVersion)
	}
	if semver.Compare(newestTag, kit.EnsurePrefix(tagged.Tag(), "v")) < 0 {
		return "", fmt.Errorf("newest release %q is older than the running %q; refusing to downgrade", newestVersion, tagged.Tag())
	}

	withTag, err := reference.WithTag(named, newestTag)
	if err != nil {
		return "", fmt.Errorf("build target reference for %q: %w", named.Name(), err)
	}
	return withTag.String(), nil
}

// StartUpdateAll records an update-all job and submits the workflow that upgrades every agent, then the manager.
// Every environment pulls the latest image, whether or not it reports an update.
func (s *Service) StartUpdateAll(ctx context.Context, user usertypes.Actor) (*EnvironmentUpdateJob, error) {
	// Closes the in-process check-then-create window (e.g. a double-click); the job row is the durable guard.
	if !s.updatingAll.CompareAndSwap(false, true) {
		return nil, errUpdateAllInProgress
	}
	defer s.updatingAll.Store(false)

	active, err := s.findUpdateAllJob(ctx, "status IN ?", activeUpdateAllStatuses)
	if err != nil {
		return nil, fmt.Errorf("check for active update-all job: %w", err)
	}
	if active != nil {
		return nil, errUpdateAllInProgress
	}

	info := s.versionService.GetAppVersionInfo(ctx)
	job := &EnvironmentUpdateJob{
		Status:                EnvironmentUpdateJobStatusRunning,
		UserID:                user.ID,
		Username:              user.Username,
		ManagerVersionAtStart: info.CurrentVersion,
		ManagerDigestAtStart:  info.CurrentDigest,
		ManagerTargetVersion:  cmp.Or(info.NewestVersion, info.NewestDigest, info.CurrentVersion, info.CurrentDigest),
	}
	// The manager row stays pending until the final manager step.
	job.Results = EnvironmentUpdateResults{{
		EnvironmentID:   environment.LocalEnvironmentID,
		EnvironmentName: s.environments.ResolveEnvironmentName(ctx, environment.LocalEnvironmentID),
		Status:          EnvironmentUpdateResultStatusPending,
		FromVersion:     info.CurrentVersion,
		ToVersion:       job.ManagerTargetVersion,
	}}
	// Seed every remote row so the dialog shows the whole fleet at once; the agents step fills any gaps.
	if remotes, listErr := s.environments.ListRemoteEnvironments(ctx); listErr != nil {
		slog.WarnContext(ctx, "update-all: failed to pre-list remote environments for seeding", "error", listErr)
	} else {
		for _, remote := range remotes {
			upsertPendingResult(job, remote.ID, remote.Name)
		}
	}

	if createUpgradeJobErr := s.db.WithContext(ctx).Create(job).Error; createUpgradeJobErr != nil {
		return nil, fmt.Errorf("create update-all job: %w", createUpgradeJobErr)
	}

	input := updateAllInput{JobID: job.ID}
	input.KeyID, _ = ctx.Value(middleware.ContextKeyApiKeyID).(string)
	instanceID, submitErr := s.flow.Submit(ctx, s.updateAllWorkflow, input, activitylib.StartRequest{})
	if submitErr != nil {
		s.markUpdateAllFailed(context.WithoutCancel(ctx), job, fmt.Sprintf("failed to start update-all: %v", submitErr))
		return nil, fmt.Errorf("start update-all: %w", submitErr)
	}
	go s.watchUpdateAll(context.WithoutCancel(ctx), job.ID, instanceID)
	slog.InfoContext(ctx, "Update-all started; upgrading agents first", "jobId", job.ID, "user", user.Username)
	return job, nil
}

// watchUpdateAll fails a job whose workflow ends without reaching the manager step, which would leave it running
// and refuse every later run.
func (s *Service) watchUpdateAll(ctx context.Context, jobID, instanceID string) {
	outcome, err := s.flow.Wait(ctx, s.updateAllWorkflow, instanceID)
	if err != nil || outcome.Status == scheduler.Succeeded {
		return
	}
	current, err := s.loadUpdateAllJob(ctx, jobID)
	if err != nil || current.Status != EnvironmentUpdateJobStatusRunning {
		return
	}
	s.markUpdateAllFailed(ctx, current, cmp.Or(outcome.Message, "update-all did not finish"))
}

// ResumeUpdateAllOnStartup finalizes a job the manager left pending_restart by self-upgrading,
// and fails a running job whose workflow is gone. It is a no-op when nothing is pending.
func (s *Service) ResumeUpdateAllOnStartup(ctx context.Context) {
	job, err := s.findUpdateAllJob(ctx, "status IN ?", activeUpdateAllStatuses)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load pending update-all job on startup", "error", err)
		return
	}
	if job == nil {
		return
	}

	// A running job resumes with its workflow; one without a live workflow cannot resume, so fail it.
	if job.Status == EnvironmentUpdateJobStatusRunning {
		standalone, standaloneErr := s.flow.Standalone(ctx)
		if index := slices.IndexFunc(standalone, func(run scheduler.Run) bool { return run.JobID == updateAllWorkflowName }); standaloneErr == nil && index >= 0 {
			go s.watchUpdateAll(context.WithoutCancel(ctx), job.ID, standalone[index].ID)
			return
		}
		s.markUpdateAllFailed(ctx, job, "interrupted by manager restart")
		return
	}

	info := s.versionService.GetAppVersionInfo(ctx)
	action := resolveResumeAction(job, info.CurrentVersion, info.CurrentDigest, time.Now())
	if action.markStale {
		s.markUpdateAllFailed(ctx, job, "update-all job is stale; manager did not restart in time")
		return
	}

	// The agents ran before the restart; only the manager row is left to settle.
	managerStatus := kit.Ternary(action.managerSucceeded, EnvironmentUpdateResultStatusUpdated, EnvironmentUpdateResultStatusFailed)
	s.finalizeUpdateAllJob(ctx, job, managerStatus, info.CurrentVersion)
	slog.InfoContext(ctx, "Finalized update-all job after manager restart", "jobId", job.ID, "managerUpgraded", action.managerSucceeded)
}

// resolveResumeAction marks a pending_restart job stale after the threshold, otherwise reports whether the manager upgrade landed.
func resolveResumeAction(job *EnvironmentUpdateJob, currentVersion, currentDigest string, now time.Time) resumeAction {
	if now.Sub(job.CreatedAt) > updateAllStaleThreshold {
		return resumeAction{markStale: true}
	}
	landed := upgradeLanded(job.ManagerVersionAtStart, job.ManagerDigestAtStart, job.ManagerTargetVersion, currentVersion, currentDigest)
	return resumeAction{managerSucceeded: landed}
}

// upgradeLanded reports whether the version or digest moved off its baseline, or already matches target (a version or
// digest), so a force-update that recreates the same image still counts.
func upgradeLanded(fromVersion, fromDigest, target, currentVersion, currentDigest string) bool {
	versionChanged := fromVersion != "" && currentVersion != fromVersion
	// An agent whose Docker lookup failed reports no digest, which proves nothing.
	digestChanged := fromDigest != "" && currentDigest != "" && currentDigest != fromDigest
	onTarget := target != "" && (strings.TrimPrefix(currentVersion, "v") == strings.TrimPrefix(target, "v") || currentDigest == target)
	return versionChanged || digestChanged || onTarget
}

// discoverAgents lists the remote environments the agents step upgrades in turn.
func (s *Service) discoverAgents(ctx context.Context, t flow.Task) (any, error) {
	envs, err := s.environments.ListRemoteEnvironments(ctx)
	if err != nil {
		var input updateAllInput
		if payloadErr := t.Payload(&input); payloadErr != nil {
			return nil, errors.Join(err, payloadErr)
		}
		if job, loadErr := s.loadUpdateAllJob(ctx, input.JobID); loadErr == nil {
			s.markUpdateAllFailed(ctx, job, fmt.Sprintf("failed to list remote environments: %v", err))
		}
		return nil, err
	}
	agents := make([]remoteAgent, len(envs))
	for i, remote := range envs {
		agents[i] = remoteAgent{ID: remote.ID, Name: remote.Name}
	}
	return agents, nil
}

// upgradeAgentTask triggers and confirms one remote environment's upgrade, persisting each stage for the status endpoint.
// A redelivered task skips an agent whose result is already terminal.
func (s *Service) upgradeAgentTask(ctx context.Context, t flow.Task) (_ any, err error) {
	var input updateAllInput
	var remote remoteAgent
	if decodeErr := errors.Join(t.Payload(&input), t.DecodeItem(&remote)); decodeErr != nil {
		return nil, decodeErr
	}
	job, err := s.loadUpdateAllJob(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	if job.Status != EnvironmentUpdateJobStatusRunning {
		return nil, nil
	}
	if allowed, authorizeErr := s.authorizeUpdateAll(ctx, job, input.KeyID); !allowed {
		return nil, authorizeErr
	}
	result := &job.Results[upsertPendingResult(job, remote.ID, remote.Name)]
	if result.Status != EnvironmentUpdateResultStatusPending && result.Status != EnvironmentUpdateResultStatusUpdating {
		return nil, nil
	}
	// A saved starting or later stage means the trigger may have reached the agent; sending it again could start a second upgrader.
	// Only a later stage proves the agent accepted it.
	triggered := result.Status == EnvironmentUpdateResultStatusUpdating && result.Stage != "" && result.Stage != EnvironmentUpdateStageChecking
	unconfirmed := triggered && result.Stage == EnvironmentUpdateStageStarting
	result.Status = EnvironmentUpdateResultStatusUpdating
	if persistErr := s.db.WithContext(ctx).Save(job).Error; persistErr != nil {
		slog.WarnContext(ctx, "update-all: failed to persist updating status", "jobId", job.ID, "environmentId", remote.ID, "error", persistErr)
	}
	defer func() {
		result.clearStage()
		// Shutdown: leave the row updating at its stage so the redelivered task resumes it.
		if err = ctx.Err(); err != nil {
			return
		}
		if persistErr := s.db.WithContext(ctx).Save(job).Error; persistErr != nil {
			slog.WarnContext(ctx, "update-all: failed to persist progress", "jobId", job.ID, "environmentId", remote.ID, "error", persistErr)
		}
	}()

	var info versiontypes.Info
	if triggered {
		info.CurrentVersion, info.CurrentDigest = result.FromVersion, result.FromDigest
	} else {
		s.setUpdateStage(ctx, job, result, EnvironmentUpdateStageChecking)
		versionCtx, cancel := context.WithTimeout(ctx, updateAllAgentRequestTimeout)
		checkErr := s.environments.ProxyJSONRequest(versionCtx, remote.ID, http.MethodGet, "/api/app-version", nil, &info)
		cancel()
		if checkErr != nil {
			msg := checkErr.Error()
			result.Status, result.Error = updateAllAgentFailureStatus(checkErr), msg[:min(len(msg), updateAllErrorMaxLen)]
			return nil, nil
		}
		// The manager's newest release outranks the agent's own check, so the recorded target tracks what was sent.
		var triggerBody []byte
		if newest := strings.TrimSpace(s.versionService.GetAppVersionInfo(ctx).NewestVersion); newest != "" {
			body, marshalErr := json.Marshal(TriggerUpgradeBody{TargetVersion: newest})
			if marshalErr != nil {
				slog.WarnContext(ctx, "update-all: failed to marshal trigger body", "environmentId", remote.ID, "error", marshalErr)
			} else {
				info.NewestVersion, triggerBody = newest, body
			}
		}
		result.FromVersion, result.FromDigest = info.CurrentVersion, info.CurrentDigest
		result.ToVersion = cmp.Or(info.NewestVersion, info.NewestDigest, info.CurrentVersion, info.CurrentDigest)

		// The saved starting stage keeps a replay from sending the trigger again, so the trigger waits on it.
		result.Stage, result.StageStartedAt = EnvironmentUpdateStageStarting, new(time.Now())
		if saveErr := s.db.WithContext(ctx).Save(job).Error; saveErr != nil {
			result.Status, result.Error = EnvironmentUpdateResultStatusFailed, "Could not save upgrade progress: "+saveErr.Error()
			return nil, nil
		}
		triggerCtx, cancel := context.WithTimeout(ctx, updateAllAgentRequestTimeout)
		resp, triggerErr := s.environments.ExecuteRemoteRequest(triggerCtx, remote.ID, http.MethodPost, "/api/environments/0/system/upgrade", triggerBody)
		cancel()
		if triggerErr == nil {
			triggerErr = resp.RequireSuccess()
		}
		if triggerErr != nil {
			msg := triggerErr.Error()
			result.Status, result.Error = EnvironmentUpdateResultStatusFailed, msg[:min(len(msg), updateAllErrorMaxLen)]
			return nil, nil
		}

		// An agent already on the target finds nothing to swap in; confirming would only burn the poll window.
		if info.AlreadyOnNewest() {
			result.Status, result.ToVersion = EnvironmentUpdateResultStatusUpToDate, info.CurrentVersion
			return nil, nil
		}
	}

	// Poll until the agent's version lands; past the window the upgrade is only recorded as triggered.
	// An unconfirmed request keeps its starting stage, so another restart still knows nothing confirmed it.
	stage := func(next EnvironmentUpdateStage) {
		if !unconfirmed {
			s.setUpdateStage(ctx, job, result, next)
		}
	}
	stage(EnvironmentUpdateStageReconnecting)
	ticker := time.NewTicker(updateAllConfirmPollInterval)
	defer ticker.Stop()
	for deadline := time.Now().Add(updateAllConfirmTimeout); time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
		}
		pollCtx, pollCancel := context.WithTimeout(ctx, updateAllAgentRequestTimeout)
		var current versiontypes.Info
		pollErr := s.environments.ProxyJSONRequest(pollCtx, remote.ID, http.MethodGet, "/api/app-version", nil, &current)
		pollCancel()
		if pollErr != nil {
			stage(EnvironmentUpdateStageReconnecting)
			continue
		}
		stage(EnvironmentUpdateStageVerifying)
		// An unconfirmed request proves nothing by the agent merely matching the target, so it needs a real change.
		if upgradeLanded(info.CurrentVersion, info.CurrentDigest, kit.Ternary(unconfirmed, "", result.ToVersion), current.CurrentVersion, current.CurrentDigest) {
			result.Status = EnvironmentUpdateResultStatusUpdated
			return nil, nil
		}
	}
	if unconfirmed {
		result.Status, result.Error = EnvironmentUpdateResultStatusFailed, "Upgrade request was interrupted before the agent confirmed it"
		return nil, nil
	}
	result.Status = EnvironmentUpdateResultStatusTriggered
	return nil, nil
}

// upgradeManagerTask triggers the manager's own self-upgrade last. The job is persisted pending_restart BEFORE the
// trigger, so a redelivery never triggers twice and the next boot finalizes it.
func (s *Service) upgradeManagerTask(ctx context.Context, t flow.Task) (any, error) {
	var input updateAllInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	job, err := s.loadUpdateAllJob(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	allowed := false
	if job.Status == EnvironmentUpdateJobStatusRunning {
		if allowed, err = s.authorizeUpdateAll(ctx, job, input.KeyID); err != nil {
			return nil, err
		}
	}
	if allowed {
		// Every agent task has finished, so an agent row still in progress belongs to a task that failed first.
		for i := range job.Results {
			pending := job.Results[i].Status == EnvironmentUpdateResultStatusPending || job.Results[i].Status == EnvironmentUpdateResultStatusUpdating
			if job.Results[i].EnvironmentID != environment.LocalEnvironmentID && pending {
				job.Results[i].clearStage()
				job.Results[i].Status, job.Results[i].Error = EnvironmentUpdateResultStatusFailed, "Agent upgrade did not finish"
			}
		}
		manager := slices.IndexFunc(job.Results, func(r EnvironmentUpdateResult) bool { return r.EnvironmentID == environment.LocalEnvironmentID })
		if manager >= 0 {
			job.Results[manager].Status = EnvironmentUpdateResultStatusUpdating
			job.Results[manager].Stage = EnvironmentUpdateStageStarting
			job.Results[manager].StageStartedAt = new(time.Now())
		}
		job.Status = EnvironmentUpdateJobStatusPendingRestart
		// A replay that misses pending_restart would trigger the manager upgrade again, so the trigger waits on it.
		if savePendingRestartErr := s.db.WithContext(ctx).Save(job).Error; savePendingRestartErr != nil {
			return nil, fmt.Errorf("persist pending_restart before manager upgrade: %w", savePendingRestartErr)
		}
		// Only a manager already on the newest image makes a no-op upgrader run expected; checked after the fact it proves nothing.
		wasAlreadyNewest := s.AlreadyOnNewestImage(ctx)
		upgraderID, triggerErr := s.TriggerUpgradeViaCLI(ctx, usertypes.Actor{ID: job.UserID, Username: job.Username}, updater.SelfUpdateTarget{})
		if triggerErr != nil {
			// No restart is coming, so this flips the manager's updating row to failed now.
			s.markUpdateAllFailed(ctx, job, fmt.Sprintf("manager upgrade trigger failed: %v", triggerErr))
		} else {
			if manager >= 0 {
				s.setUpdateStage(ctx, job, &job.Results[manager], EnvironmentUpdateStageReconnecting)
			}
			slog.InfoContext(ctx, "Update-all: agents done, manager self-upgrade triggered", "jobId", job.ID, "upgraderId", upgraderID)
			s.watchManagerUpgrader(ctx, job, upgraderID, wasAlreadyNewest)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if job, err = s.loadUpdateAllJob(ctx, input.JobID); err != nil {
			return nil, err
		}
	}
	if job.Status == EnvironmentUpdateJobStatusFailed {
		return scheduler.Outcome{Status: scheduler.Failed, Message: kit.FromPtr(job.Error)}, nil
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, Message: "Update-all finished; " + string(job.Status)}, nil
}

// watchManagerUpgrader closes out a pending_restart job whose manager was never recreated, which a recreate would have
// prevented by stopping this process. wasAlreadyNewest is the pre-trigger check that a no-op run was expected.
func (s *Service) watchManagerUpgrader(ctx context.Context, job *EnvironmentUpdateJob, upgraderID string, wasAlreadyNewest bool) {
	exit, exitCode := upgraderExitUnobserved, int64(0)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.WarnContext(ctx, "update-all: cannot watch upgrader container", "upgraderId", upgraderID, "error", err)
	} else {
		waitCtx, cancel := context.WithTimeout(ctx, updateAllManagerWatchTimeout)
		wait := dockerClient.ContainerWait(waitCtx, upgraderID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
		select {
		case result, ok := <-wait.Result:
			// A closed channel yields a zero response that looks like exit 0; a wait error comes with a filler code 0.
			switch {
			case !ok:
				slog.WarnContext(ctx, "update-all: upgrader wait ended without a status", "upgraderId", upgraderID)
			case result.StatusCode != 0:
				exit, exitCode = upgraderExitFailed, result.StatusCode
			case result.Error != nil:
				slog.WarnContext(ctx, "update-all: upgrader wait reported an error", "upgraderId", upgraderID, "error", result.Error.Message)
				exit = upgraderExitCodeLost
			default:
				exit = upgraderExitSucceeded
			}
		case waitErr := <-wait.Error:
			// AutoRemove can delete a fast upgrader before the wait lands: it exited, but its code went with it.
			if errdefs.IsNotFound(waitErr) {
				exit = upgraderExitCodeLost
			} else {
				slog.WarnContext(ctx, "update-all: failed waiting for upgrader container", "upgraderId", upgraderID, "error", waitErr)
			}
		case <-waitCtx.Done():
			slog.WarnContext(ctx, "update-all: timed out waiting for upgrader container", "upgraderId", upgraderID)
		}
		cancel()
	}

	// Let a recreate already underway stop this process; an unobserved run waits out the stale window instead.
	hold := updateAllManagerNoRestartGrace
	if exit == upgraderExitUnobserved {
		hold = time.Until(job.CreatedAt.Add(updateAllStaleThreshold))
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(hold):
	}

	job, err = s.loadUpdateAllJob(ctx, job.ID)
	if err != nil {
		slog.WarnContext(ctx, "update-all: failed to reload job after upgrader exit", "upgraderId", upgraderID, "error", err)
		return
	}
	if job.Status != EnvironmentUpdateJobStatusPendingRestart {
		return
	}

	info := s.versionService.GetAppVersionInfo(ctx)
	switch {
	case exit == upgraderExitUnobserved:
		slog.WarnContext(ctx, "update-all: manager upgrade was never observed and no restart followed", "jobId", job.ID)
		s.markUpdateAllFailed(ctx, job, "manager upgrade could not be observed: the upgrader run was never seen to finish and the manager did not restart")
	case exit == upgraderExitFailed:
		s.markUpdateAllFailed(ctx, job, fmt.Sprintf("manager upgrade failed: upgrader exited with code %d", exitCode))
	case exit == upgraderExitCodeLost && (!wasAlreadyNewest || !info.AlreadyOnNewest()):
		// Without an exit code a no-op is only credible when the check said so both before and after the run.
		s.markUpdateAllFailed(ctx, job, "manager upgrade could not be confirmed: the upgrader exited without a readable status and this manager was not a confirmed no-op upgrade")
	default:
		slog.InfoContext(ctx, "Update-all: manager was already up to date; no restart needed", "jobId", job.ID, "version", info.CurrentVersion)
		s.finalizeUpdateAllJob(ctx, job, EnvironmentUpdateResultStatusUpToDate, info.CurrentVersion)
	}
}

// upsertPendingResult returns the index of envID's result row, appending a pending row when seeding missed it.
func upsertPendingResult(job *EnvironmentUpdateJob, envID, envName string) int {
	if idx := slices.IndexFunc(job.Results, func(r EnvironmentUpdateResult) bool { return r.EnvironmentID == envID }); idx >= 0 {
		return idx
	}
	job.Results = append(job.Results, EnvironmentUpdateResult{
		EnvironmentID:   envID,
		EnvironmentName: envName,
		Status:          EnvironmentUpdateResultStatusPending,
	})
	return len(job.Results) - 1
}

// updateAllAgentFailureStatus classifies a failed agent pre-check: a reached environment whose request failed or timed
// out (poll-mode tunnels connect on demand) is a failure; only a never-reachable one is an offline skip.
func updateAllAgentFailureStatus(err error) EnvironmentUpdateResultStatus {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return EnvironmentUpdateResultStatusFailed
	}
	if _, ok := errors.AsType[*remenv.StatusError](err); ok {
		return EnvironmentUpdateResultStatusFailed
	}
	return EnvironmentUpdateResultStatusSkippedOffline
}

// setUpdateStage records result's step and persists the job; re-entering the current stage keeps its start time.
func (s *Service) setUpdateStage(ctx context.Context, job *EnvironmentUpdateJob, result *EnvironmentUpdateResult, stage EnvironmentUpdateStage) {
	if result.Stage == stage {
		return
	}
	result.Stage = stage
	result.StageStartedAt = new(time.Now())
	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		slog.WarnContext(ctx, "update-all: failed to persist stage", "jobId", job.ID, "environmentId", result.EnvironmentID, "stage", stage, "error", err)
	}
}

// GetLatestUpdateAllJob returns the most recently created update-all job, or nil.
func (s *Service) GetLatestUpdateAllJob(ctx context.Context) (*EnvironmentUpdateJob, error) {
	return s.findUpdateAllJob(ctx)
}

// findUpdateAllJob returns the newest job matching the inline GORM conditions, or nil when none does.
func (s *Service) findUpdateAllJob(ctx context.Context, conds ...any) (*EnvironmentUpdateJob, error) {
	var jobs []EnvironmentUpdateJob
	if err := s.db.WithContext(ctx).Order("created_at DESC").Limit(1).Find(&jobs, conds...).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

// loadUpdateAllJob loads a job by ID, failing when it no longer exists.
func (s *Service) loadUpdateAllJob(ctx context.Context, id string) (*EnvironmentUpdateJob, error) {
	job, err := s.findUpdateAllJob(ctx, "id = ?", id)
	if err == nil && job == nil {
		err = fmt.Errorf("update-all job %s not found", id)
	}
	return job, err
}

// authorizeUpdateAll reports whether the job's requester may still upgrade, failing the job when not.
// A task recovered after a restart never passed the request's permission check. Shutdown returns its
// error so the task resumes instead of failing the job.
func (s *Service) authorizeUpdateAll(ctx context.Context, job *EnvironmentUpdateJob, keyID string) (bool, error) {
	permissions, err := s.roles.ResolveExecutionPermissions(ctx, job.UserID, keyID)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err == nil && permissions.Allows(authz.PermSystemUpgrade, environment.LocalEnvironmentID) {
		return true, nil
	}
	s.markUpdateAllFailed(ctx, job, "requesting user no longer has permission to upgrade")
	return false, nil
}

func (s *Service) markUpdateAllFailed(ctx context.Context, job *EnvironmentUpdateJob, reason string) {
	job.Status = EnvironmentUpdateJobStatusFailed
	job.Error = &reason
	job.CompletedAt = new(time.Now())
	for i := range job.Results {
		job.Results[i].clearStage()
		if job.Results[i].Status == EnvironmentUpdateResultStatusUpdating {
			job.Results[i].Status = EnvironmentUpdateResultStatusFailed
			job.Results[i].Error = reason
		}
	}
	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		slog.WarnContext(ctx, "update-all: failed to mark job failed", "jobId", job.ID, "error", err)
	}

	// LogUserEvent always records an info-severity "completed" event, so create the error event directly.
	if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:        event.EventTypeSystemUpgrade,
		Severity:    event.EventSeverityError,
		Title:       "Update all environments failed",
		Description: reason,
		UserID:      new(job.UserID),
		Username:    new(job.Username),
		Metadata: database.JSON{
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

// finalizeUpdateAllJob settles the manager row, completes the job and records the audit event.
func (s *Service) finalizeUpdateAllJob(ctx context.Context, job *EnvironmentUpdateJob, managerStatus EnvironmentUpdateResultStatus, managerVersion string) {
	if idx := slices.IndexFunc(job.Results, func(r EnvironmentUpdateResult) bool { return r.EnvironmentID == environment.LocalEnvironmentID }); idx >= 0 {
		job.Results[idx].Status = managerStatus
		if managerStatus == EnvironmentUpdateResultStatusFailed {
			job.Results[idx].Error = "manager version did not change after upgrade"
		} else {
			job.Results[idx].ToVersion = managerVersion
		}
	}
	job.Status = EnvironmentUpdateJobStatusCompleted
	job.CompletedAt = new(time.Now())
	failed := 0
	for i := range job.Results {
		job.Results[i].clearStage()
		if job.Results[i].Status == EnvironmentUpdateResultStatusFailed {
			failed++
		}
	}
	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		slog.WarnContext(ctx, "update-all: failed to finalize job", "jobId", job.ID, "error", err)
		return
	}

	metadata := database.JSON{
		"action":       "update_all_environments",
		"jobId":        job.ID,
		"environments": len(job.Results),
		"failed":       failed,
	}
	if failed == 0 {
		if err := s.eventService.LogUserEvent(ctx, event.EventTypeSystemUpgrade, job.UserID, job.Username, metadata); err != nil {
			slog.WarnContext(ctx, "Failed to log update-all event", "jobId", job.ID, "error", err)
		}
		return
	}
	// Completed with failures: a warning event keeps them visible in the audit log.
	if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:        event.EventTypeSystemUpgrade,
		Severity:    event.EventSeverityWarning,
		Title:       "Update all environments completed with errors",
		Description: fmt.Sprintf("%d of %d environments failed to update", failed, len(job.Results)),
		UserID:      new(job.UserID),
		Username:    new(job.Username),
		Metadata:    metadata,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to log update-all event", "jobId", job.ID, "error", err)
	}
}

// PruneUpgradeLogs removes expired upgrade logs from this instance's data directory.
func (s *Service) PruneUpgradeLogs(ctx context.Context, dataDir string, now time.Time) (int, error) {
	retentionDays := s.settingsService.GetIntSetting(ctx, "upgradeLogRetentionDays", 3)
	if retentionDays == 0 {
		return 0, nil
	}
	if retentionDays < 0 || retentionDays > 3650 {
		return 0, errors.New("upgrade log retention must be between 0 and 3650 days")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	root, err := os.OpenRoot(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open upgrade log directory: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			slog.WarnContext(ctx, "Failed to close upgrade log directory", "error", closeErr)
		}
	}()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return 0, fmt.Errorf("read upgrade log directory: %w", err)
	}

	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	removed := 0
	var failures []error
	for _, entry := range entries {
		if cancellationErr := ctx.Err(); cancellationErr != nil {
			return removed, errors.Join(append(failures, cancellationErr)...)
		}
		if !upgradeLogName.MatchString(entry.Name()) {
			continue
		}
		info, lstatErr := root.Lstat(entry.Name())
		if errors.Is(lstatErr, os.ErrNotExist) {
			continue
		}
		if lstatErr != nil {
			failures = append(failures, fmt.Errorf("inspect upgrade log %s: %w", entry.Name(), lstatErr))
			continue
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if removeErr := root.Remove(entry.Name()); removeErr != nil {
			if !errors.Is(removeErr, os.ErrNotExist) {
				failures = append(failures, fmt.Errorf("remove upgrade log %s: %w", entry.Name(), removeErr))
			}
			continue
		}
		removed++
	}
	return removed, errors.Join(failures...)
}
