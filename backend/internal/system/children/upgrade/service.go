package upgrade

import (
	"bufio"
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/streams/agg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/refs"
	updatertypes "go.getarcane.app/updater/types"
	"golang.org/x/mod/semver"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/version"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

var (
	upgradeLogName = regexp.MustCompile(`^arcane-upgrade-\d+\.log$`)
	// exactReleaseTag pins an exact release (vX.Y.Z, prereleases included); channels like v2 or v2.9 are mutable.
	exactReleaseTag          = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+].*)?$`)
	activeUpgradeJobStatuses = []UpgradeJobStatus{UpgradeJobStatusPendingRestart, UpgradeJobStatusRunning}
	errUpgradeJobInProgress  = common.Classify(common.ErrUpgradeJobInProgress, errors.New("an upgrade job is already in progress"))
)

const (
	// The run never finished observably: nothing may be concluded from it.
	upgraderExitUnobserved upgraderExit = 0
	upgraderExitSucceeded  upgraderExit = 1
	upgraderExitFailed     upgraderExit = 2
	// The upgrader is gone and its exit code with it: success and failure are indistinguishable.
	upgraderExitCodeLost upgraderExit = 3

	upgradeJobStaleThreshold      = time.Hour
	upgradeJobAgentRequestTimeout = 15 * time.Second
	upgradeJobRedialMaxBackoff    = 5 * time.Second
	upgradeJobLegacyPollInterval  = 10 * time.Second
	upgradeJobPublishInterval     = 250 * time.Millisecond
	upgradeJobConfirmTimeout      = 5 * time.Minute
	upgradeJobErrorMaxLen         = 500
	upgradeJobMaxParallelAgents   = 4
	upgradeJobWorkflowName        = "update-all"
	// Generous: the upgrader pulls the target image before recreating anything.
	upgradeJobManagerWatchTimeout = 15 * time.Minute
	// How long a recreate already underway gets to stop this container before no restart is assumed.
	upgradeJobManagerNoRestartGrace = 15 * time.Second
)

type Service struct {
	upgrading    atomic.Bool
	startingJob  atomic.Bool
	resultMu     sync.Mutex
	progress     *progressFeed
	jobHub       *agg.Hub[types.UpgradeJob]
	jobChanged   chan struct{}
	latestJob    atomic.Pointer[types.UpgradeJob]
	flow         *flow.Engine
	environments *environment.EnvironmentService
	roles        *role.RoleService
	// upgradeJobWorkflow upgrades agents in parallel, then the manager last.
	upgradeJobWorkflow *flow.Workflow
	db                 *database.DB
	dockerService      *dockerInternal.DockerClientService
	versionService     *version.VersionService
	eventService       *event.EventService
	settingsService    *settings.SettingsService
	projectService     *project.ProjectService
	registryService    *registry.ContainerRegistryService
	keepUpgrader       bool
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

// remoteAgent is one environment the upgrade job workflow upgrades.
type remoteAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// upgradeJobInput is the upgrade job payload: the job and the API key that requested it.
type upgradeJobInput struct {
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
	fromVersion string
	toVersion   string
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
	registryService *registry.ContainerRegistryService,
	resolveRuntimeOptions func(
		ctx context.Context,
		dockerHost string,
		currentContainer *container.InspectResponse,
		discoverHostPath func(context.Context, string) (string, error),
		isRunningInDocker func() bool,
		selectReachableNetwork func(context.Context, *container.InspectResponse, string) string,
	) ([]string, []mount.Mount, container.NetworkMode, error),
	keepUpgrader bool,
) *Service {
	return &Service{
		db:                    db,
		dockerService:         dockerService,
		versionService:        versionService,
		eventService:          eventService,
		settingsService:       settingsService,
		projectService:        projectService,
		registryService:       registryService,
		resolveRuntimeOptions: resolveRuntimeOptions,
		keepUpgrader:          keepUpgrader,
		progress:              newProgressFeed(),
		jobHub:                agg.NewHub[types.UpgradeJob](),
		jobChanged:            make(chan struct{}, 1),
	}
}

// RegisterWorkflows defines the upgrade job workflow while the host is still unstarted. Its payload names the job;
// the job row holds every result, so a redelivered task resumes from what the row records.
func (s *Service) RegisterWorkflows(engine *flow.Engine, environments *environment.EnvironmentService, roles *role.RoleService) error {
	var err error
	s.flow, s.environments, s.roles = engine, environments, roles
	s.upgradeJobWorkflow, err = engine.Define(flow.Definition{
		Name:        upgradeJobWorkflowName,
		Version:     2,
		Fingerprint: "2e931d8b90fa1ef53d566ac5616cb3ab1da3ea8da166ad728ca6d92cfc9a05bc",
		Concurrency: 1,
		Timeout:     24 * time.Hour,
		Steps: []workflow.StepSpec{
			workflow.Step("discover", engine.Handler(s.discoverAgents), workflow.WithMaxAttempts(1)),
			workflow.ForEach("agents", engine.Handler(s.upgradeAgentTask), workflow.WithItemsFrom("discover"), workflow.WithMaxParallel(upgradeJobMaxParallelAgents),
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
	self, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
	if inspectErr != nil {
		return false, errors.New("arcane is not running in a Docker container")
	}
	if unit := types.QuadletUnit(kit.FromPtr(self.Config).Labels); unit != "" {
		return false, fmt.Errorf("arcane runs from the Quadlet unit %s; update it with podman auto-update instead", unit)
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
func (s *Service) TriggerUpgradeViaCLI(ctx context.Context, user types.Actor, target updater.SelfUpdateTarget) (string, error) {
	prepared, err := s.prepareUpgrade(ctx, user, target, "")
	if err != nil {
		return "", err
	}
	return s.runPreparedUpgrade(ctx, prepared)
}

// TriggerUpgradeAsync validates synchronously, then pulls and spawns the upgrader in the background (#3628).
// The run follows the app lifecycle with a deadline so a stalled daemon cannot hold the upgrading guard forever.
func (s *Service) TriggerUpgradeAsync(ctx context.Context, user types.Actor, targetVersion string) error {
	prepared, err := s.prepareUpgrade(ctx, user, updater.SelfUpdateTarget{}, targetVersion)
	if err != nil {
		return err
	}
	localSettings := s.settingsService.GetSettingsConfig()
	runTimeout := timeouts.GetDuration(localSettings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull) + time.Minute
	runCtx, cancel := context.WithTimeout(utils.ActivityRuntimeContext(ctx, nil), runTimeout)
	go func() {
		defer cancel()
		_, runPreparedUpgradeErr := s.runPreparedUpgrade(runCtx, prepared)
		if runPreparedUpgradeErr == nil {
			return
		}
		slog.ErrorContext(ctx, "Background self-upgrade failed", "error", runPreparedUpgradeErr, "targetImage", prepared.targetImage)
		eventCtx, eventCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer eventCancel()
		if _, createEventErr := s.eventService.CreateEvent(eventCtx, event.CreateEventRequest{
			Type:        event.EventTypeSystemUpgrade,
			Severity:    event.EventSeverityError,
			Title:       "Self-upgrade failed",
			Description: runPreparedUpgradeErr.Error(),
			UserID:      new(user.ID),
			Username:    new(user.Username),
			Metadata: database.JSON{
				"action":        "system_upgrade_cli",
				"containerName": prepared.containerName,
				"targetImage":   prepared.targetImage,
				"pullImage":     prepared.pullImage,
			},
		}); createEventErr != nil {
			slog.WarnContext(eventCtx, "Failed to log self-upgrade failure event", "error", createEventErr)
		}
	}()
	return nil
}

// prepareUpgrade takes the upgrading guard, released by runPreparedUpgrade (or here on error).
func (s *Service) prepareUpgrade(ctx context.Context, user types.Actor, target updater.SelfUpdateTarget, targetVersion string) (prepared *preparedUpgrade, err error) {
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
	runningUpgraders, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerListResult, error) {
		return cli.ContainerList(ctx, client.ContainerListOptions{
			Filters: make(client.Filters).Add("label", libarcane.LabelArcaneUpgrader+"=true").Add("status", "running"),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("check for running upgraders: %w", err)
	}
	if len(runningUpgraders.Items) > 0 {
		return nil, common.Classify(common.ErrUpgradeInProgress, errors.New("an upgrader container is already running"))
	}
	current, err := func() (container.InspectResponse, error) {
		containerID := strings.TrimSpace(target.ContainerID)
		if containerID == "" {
			self, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient)
			if inspectErr != nil {
				return container.InspectResponse{}, errors.New("get current container: arcane is not running in a Docker container")
			}
			return *self, nil
		}
		inspect, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, cli, containerID, client.ContainerInspectOptions{})
		})
		if inspectErr == nil {
			return inspect.Container, nil
		}
		// Fall back to a prefix match across every container.
		containers, listErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerListResult, error) {
			return cli.ContainerList(ctx, client.ContainerListOptions{All: true})
		})
		if listErr != nil {
			return container.InspectResponse{}, fmt.Errorf("inspect container: %w", listErr)
		}
		idx := slices.IndexFunc(containers.Items, func(c container.Summary) bool { return strings.HasPrefix(c.ID, containerID) })
		if idx < 0 {
			return container.InspectResponse{}, fmt.Errorf("inspect container: %w", common.Classify(common.ErrNotFound, errors.New("could not find Arcane container")))
		}
		if inspect, inspectErr = s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, cli, containers.Items[idx].ID, client.ContainerInspectOptions{})
		}); inspectErr != nil {
			return container.InspectResponse{}, fmt.Errorf("inspect container: %w", inspectErr)
		}
		return inspect.Container, nil
	}()
	if err != nil {
		return nil, err
	}
	if unit := types.QuadletUnit(kit.FromPtr(current.Config).Labels); unit != "" {
		return nil, fmt.Errorf("systemd unit %s: %w", unit, common.ErrContainerQuadletManaged)
	}

	containerName := strings.TrimPrefix(current.Name, "/")
	config := kit.FromPtr(current.Config)
	binaryPath := kit.Ternary(labels.IsArcaneAgentContainer(config.Labels), "/app/arcane-agent", "/app/arcane")

	info := kit.FromPtr(s.versionService.GetAppVersionInfo(ctx))
	targetImage := strings.TrimSpace(target.NewImageRef)
	if targetImage == "" {
		// A blank target resolves against the version check; a manager-supplied version outranks this instance's own.
		if targetImage, err = resolveSelfUpgradeTargetImage(config.Image, &types.VersionInfo{
			NewestVersion: cmp.Or(strings.TrimSpace(targetVersion), info.NewestVersion),
			NewestDigest:  info.NewestDigest,
		}); err != nil {
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
		"fromVersion":   info.CurrentVersion,
	}
	if logUserEventErr := s.eventService.LogUserEvent(ctx, event.EventTypeSystemUpgrade, user.ID, user.Username, metadata); logUserEventErr != nil {
		slog.WarnContext(ctx, "Failed to log upgrade event", "error", logUserEventErr)
	}

	toVersion := cmp.Or(strings.TrimSpace(targetVersion), info.NewestVersion, info.NewestDigest, targetImage)
	s.progress.start(info.CurrentVersion, toVersion)
	return &preparedUpgrade{
		dockerClient:  dockerClient,
		current:       current,
		containerName: containerName,
		binaryPath:    binaryPath,
		targetImage:   targetImage,
		pullImage:     pullImage,
		fromVersion:   info.CurrentVersion,
		toVersion:     toVersion,
		saveCompose:   saveCompose,
	}, nil
}

func (s *Service) runPreparedUpgrade(ctx context.Context, prepared *preparedUpgrade) (_ string, err error) {
	defer s.upgrading.Store(false)
	defer func() {
		if err != nil {
			s.progress.finish(err.Error())
		}
	}()
	s.progress.emit(types.UpgradeProgressEventStage, 0, string(UpgradeStagePulling))

	// The upgrader runs from the target image, so the upgrade CLI is the new version.
	dockerClient := prepared.dockerClient
	slog.InfoContext(ctx, "Pulling upgrader image", "containerName", prepared.containerName, "image", prepared.pullImage)

	localSettings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(localSettings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
	defer pullCancel()

	registryAuth, err := s.registryService.GetRegistryAuthForImage(pullCtx, prepared.pullImage)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve registry credentials for upgrader image; pulling anonymously", "image", prepared.pullImage, "error", err)
	}
	pullReader, err := dockerClient.ImagePull(pullCtx, prepared.pullImage, client.ImagePullOptions{RegistryAuth: registryAuth})
	if err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("upgrader image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", prepared.pullImage)
		}
		return "", fmt.Errorf("pull upgrader image: %w", err)
	}
	if pullErr := s.reportPull(pullReader); pullErr != nil {
		_ = pullReader.Close()
		return "", fmt.Errorf("failed to complete upgrader image pull: %w", pullErr)
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

	mounts := append([]mount.Mount{}, runtimeMounts...)
	if appDataMount != nil {
		mounts = append(mounts, *appDataMount)
	}

	if s.keepUpgrader {
		slog.InfoContext(ctx, "Keeping upgrader container after exit (ARCANE_UPGRADE_KEEP_CONTAINER=true)")
	}

	// Inherit the security context (SELinux label, privileged) that lets Arcane reach the socket on hardened hosts.
	securityOpt := slices.Clone(kit.FromPtr(prepared.current.HostConfig).SecurityOpt)
	// On SELinux-enforcing hosts the socket is container_var_run_t, so without a label opt the upgrader exits with EACCES.
	if !kit.FromPtr(prepared.current.HostConfig).Privileged &&
		!slices.ContainsFunc(securityOpt, func(opt string) bool { return strings.HasPrefix(strings.TrimSpace(opt), "label") }) {
		infoResult, infoErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.SystemInfoResult, error) {
			return cli.Info(ctx, client.InfoOptions{})
		})
		if infoErr != nil {
			slog.DebugContext(ctx, "Failed to query daemon info for SELinux detection", "error", infoErr)
		} else if slices.Contains(infoResult.Info.SecurityOptions, "name=selinux") {
			securityOpt = append(securityOpt, "label=disable")
		}
	}

	upgraderName := fmt.Sprintf("%s-upgrader-%d", prepared.containerName, time.Now().Unix())
	resp, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerCreateResult, error) {
		return cli.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config: &container.Config{
				Image: prepared.pullImage,
				Cmd:   upgradeCmd,
				// Root for the Docker socket: the short-lived upgrader never goes through the runtime-identity drop.
				User: "0:0",
				Env:  containerEnv,
				Labels: map[string]string{
					libarcane.LabelArcaneUpgrader: "true",
					"com.getarcaneapp.arcane":     "true",
				},
			},
			HostConfig: &container.HostConfig{
				AutoRemove:  !s.keepUpgrader,
				Mounts:      mounts,
				NetworkMode: networkMode,
				SecurityOpt: securityOpt,
				Privileged:  kit.FromPtr(prepared.current.HostConfig).Privileged,
			},
			NetworkingConfig: func() *network.NetworkingConfig {
				if networkMode == "" || !networkMode.IsUserDefined() {
					return nil
				}
				return &network.NetworkingConfig{
					EndpointsConfig: map[string]*network.EndpointSettings{
						string(networkMode): {Aliases: []string{"arcane-upgrader"}},
					},
				}
			}(),
			Name: upgraderName,
		})
	})
	if err != nil {
		return "", fmt.Errorf("create upgrader container: %w", err)
	}

	if _, containerStartErr := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerStartResult, error) {
		return cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	}); containerStartErr != nil {
		_, _ = s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerRemoveResult, error) {
			return cli.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		})
		return "", fmt.Errorf("start upgrader container: %w", containerStartErr)
	}

	slog.InfoContext(ctx, "Upgrade container started", "upgraderId", resp.ID[:12], "upgraderName", upgraderName)
	s.progress.emit(types.UpgradeProgressEventStage, pullPercentCeiling, string(UpgradeStageUpgrading))
	go s.followUpgrader(context.WithoutCancel(ctx), resp.ID)
	return resp.ID, nil
}

func (s *Service) reportPull(pullReader io.Reader) error {
	lines, writer := io.Pipe()
	defer func() { _ = lines.Close() }()
	go func() {
		_ = writer.CloseWithError(docker.RenderJSONMessageStream(pullReader, writer))
	}()
	layers := map[string]bool{}
	scanner := bufio.NewScanner(lines)
	for scanner.Scan() {
		line := scanner.Text()
		s.progress.emit(types.UpgradeProgressEventLog, trackPullLine(layers, line), line)
	}
	return scanner.Err()
}

func (s *Service) followUpgrader(ctx context.Context, upgraderID string) {
	ctx, cancel := context.WithTimeout(ctx, upgradeJobManagerWatchTimeout)
	defer cancel()
	lines := make(chan string, progressFeedBuffer)
	go func() {
		defer close(lines)
		logs, err := s.dockerService.Call(ctx, func(ctx context.Context, cli *client.Client) (client.ContainerLogsResult, error) {
			return cli.ContainerLogs(ctx, upgraderID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Follow: true})
		})
		if err == nil {
			err = docker.StreamMultiplexedLogs(ctx, logs, lines)
		}
		if err != nil {
			slog.DebugContext(ctx, "Cannot follow upgrader logs", "upgraderId", upgraderID, "error", err)
			s.progress.emit(types.UpgradeProgressEventLog, 0, "Upgrader logs are unavailable; waiting for the upgrade to finish")
		}
	}()
	exit, exitCode := upgraderExitUnobserved, int64(0)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		exit, exitCode = s.waitForUpgrader(ctx, upgraderID)
	}()
	for line := range lines {
		s.progress.emit(parseUpgraderLine(line))
	}
	<-exited
	select {
	case <-ctx.Done():
	case <-time.After(upgradeJobManagerNoRestartGrace):
	}
	s.progress.finish(kit.Ternary(exit == upgraderExitFailed, fmt.Sprintf("Upgrader exited with code %d", exitCode), "Upgrader exited without restarting Arcane"))
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
func resolveSelfUpgradeTargetImage(currentImageRef string, info *types.VersionInfo) (string, error) {
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

// StartUpgradeJob records an upgrade job and submits the workflow that upgrades the requested agents, then the manager.
// No environment IDs means every enabled environment. Each one pulls the latest image, whether or not it reports an update.
func (s *Service) StartUpgradeJob(ctx context.Context, user types.Actor, environmentIDs []string) (*UpgradeJob, error) {
	// Closes the in-process check-then-create window (e.g. a double-click); the job row is the durable guard.
	if !s.startingJob.CompareAndSwap(false, true) {
		return nil, errUpgradeJobInProgress
	}
	defer s.startingJob.Store(false)

	active, err := s.findUpgradeJob(ctx, "status IN ?", activeUpgradeJobStatuses)
	if err != nil {
		return nil, fmt.Errorf("check for active upgrade job: %w", err)
	}
	if active != nil {
		return nil, errUpgradeJobInProgress
	}

	remotes, err := s.environments.ListRemoteEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list remote environments: %w", err)
	}
	agents := make([]remoteAgent, len(remotes))
	for i, remote := range remotes {
		agents[i] = remoteAgent{ID: remote.ID, Name: remote.Name}
	}
	info := s.versionService.GetAppVersionInfo(ctx)
	results, err := seedUpgradeResults(environmentIDs, UpgradeJobResult{
		EnvironmentID:   environment.LocalEnvironmentID,
		EnvironmentName: s.environments.ResolveEnvironmentName(ctx, environment.LocalEnvironmentID),
		Status:          UpgradeResultStatusPending,
		FromVersion:     info.CurrentVersion,
		ToVersion:       cmp.Or(info.NewestVersion, info.NewestDigest, info.CurrentVersion, info.CurrentDigest),
	}, agents)
	if err != nil {
		return nil, err
	}
	job := &UpgradeJob{
		Status:                UpgradeJobStatusRunning,
		UserID:                user.ID,
		Username:              user.Username,
		ManagerVersionAtStart: info.CurrentVersion,
		ManagerDigestAtStart:  info.CurrentDigest,
		ManagerTargetVersion:  cmp.Or(info.NewestVersion, info.NewestDigest),
		Results:               results,
	}
	if createUpgradeJobErr := s.saveJob(ctx, job); createUpgradeJobErr != nil {
		return nil, fmt.Errorf("create upgrade job: %w", createUpgradeJobErr)
	}

	keyID, _ := ctx.Value(middleware.ContextKeyApiKeyID).(string)
	instanceID, submitErr := s.flow.Submit(ctx, s.upgradeJobWorkflow, upgradeJobInput{
		JobID: job.ID,
		KeyID: keyID,
	}, activitylib.StartRequest{})
	if submitErr != nil {
		s.markUpgradeJobFailed(context.WithoutCancel(ctx), job, fmt.Sprintf("failed to start upgrade job: %v", submitErr))
		return nil, fmt.Errorf("start upgrade job: %w", submitErr)
	}
	go s.watchUpgradeJob(context.WithoutCancel(ctx), job.ID, instanceID)
	slog.InfoContext(ctx, "Upgrade job started; upgrading agents first", "jobId", job.ID, "user", user.Username, "environments", len(job.Results))
	return job, nil
}

// watchUpgradeJob fails a job whose workflow ends without reaching the manager step, which would leave it running
// and refuse every later run.
func (s *Service) watchUpgradeJob(ctx context.Context, jobID, instanceID string) {
	outcome, err := s.flow.Wait(ctx, s.upgradeJobWorkflow, instanceID)
	if err != nil || outcome.Status == types.RunStatusSucceeded {
		return
	}
	current, err := s.loadUpgradeJob(ctx, jobID)
	if err != nil || current.Status != UpgradeJobStatusRunning {
		return
	}
	s.markUpgradeJobFailed(ctx, current, cmp.Or(outcome.Message, "upgrade job did not finish"))
}

// ResumeUpgradeJobOnStartup finalizes a job the manager left pending_restart by self-upgrading,
// and fails a running job whose workflow is gone or whose manager was upgraded outside it. It is a no-op when nothing is pending.
func (s *Service) ResumeUpgradeJobOnStartup(ctx context.Context) {
	job, err := s.findUpgradeJob(ctx, "status IN ?", activeUpgradeJobStatuses)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load pending upgrade job on startup", "error", err)
		return
	}
	if job == nil {
		return
	}

	info := s.versionService.GetAppVersionInfo(ctx)
	// A running job resumes with its workflow; one without a live workflow cannot resume, so fail it.
	if job.Status == UpgradeJobStatusRunning {
		standalone, standaloneErr := s.flow.Standalone(ctx)
		index := slices.IndexFunc(standalone, func(run types.SchedulerRun) bool { return run.JobID == upgradeJobWorkflowName })
		if standaloneErr == nil && index >= 0 && !upgradeLanded(job.ManagerVersionAtStart, job.ManagerDigestAtStart, "", info.CurrentVersion, info.CurrentDigest) {
			go s.watchUpgradeJob(context.WithoutCancel(ctx), job.ID, standalone[index].ID)
			return
		}
		s.markUpgradeJobFailed(ctx, job, "interrupted by manager restart")
		return
	}

	action := resolveResumeAction(job, info.CurrentVersion, info.CurrentDigest, time.Now())
	if action.markStale {
		s.markUpgradeJobFailed(ctx, job, "upgrade job is stale; manager did not restart in time")
		return
	}

	// The agents ran before the restart; only the manager row is left to settle.
	if manager := slices.IndexFunc(job.Results, func(r UpgradeJobResult) bool {
		return r.EnvironmentID == environment.LocalEnvironmentID
	}); manager >= 0 {
		lines, replayErr := readUpgradeLog(libarcane.UpgradeLogDirectory, job.CreatedAt)
		if replayErr != nil {
			slog.WarnContext(ctx, "upgrade job: failed to replay manager upgrade log", "jobId", job.ID, "error", replayErr)
		}
		sink := &progressSink{service: s, job: job, index: manager, baseSet: true}
		for _, line := range lines {
			sink.apply(ctx, types.UpgradeProgressEvent{
				Type:    types.UpgradeProgressEventLog,
				Message: line,
				At:      time.Now(),
				Replay:  true,
			})
		}
	}
	managerStatus := kit.Ternary(action.managerSucceeded, UpgradeResultStatusUpdated, UpgradeResultStatusFailed)
	s.finalizeUpgradeJob(ctx, job, managerStatus, info.CurrentVersion)
	slog.InfoContext(ctx, "Finalized upgrade job after manager restart", "jobId", job.ID, "managerUpgraded", action.managerSucceeded)
}

// resolveResumeAction marks a pending_restart job stale after the threshold, otherwise reports whether the manager upgrade landed.
func resolveResumeAction(job *UpgradeJob, currentVersion, currentDigest string, now time.Time) resumeAction {
	if now.Sub(job.CreatedAt) > upgradeJobStaleThreshold {
		return resumeAction{markStale: true}
	}
	return resumeAction{managerSucceeded: upgradeLanded(job.ManagerVersionAtStart, job.ManagerDigestAtStart, job.ManagerTargetVersion, currentVersion, currentDigest)}
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

// discoverAgents lists the remote environments the job targets, which the agents step upgrades in parallel.
func (s *Service) discoverAgents(ctx context.Context, t flow.Task) (any, error) {
	var input upgradeJobInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	job, err := s.loadUpgradeJob(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	agents := make([]remoteAgent, 0, len(job.Results))
	for _, row := range job.Results {
		if row.EnvironmentID != environment.LocalEnvironmentID {
			agents = append(agents, remoteAgent{ID: row.EnvironmentID, Name: row.EnvironmentName})
		}
	}
	return agents, nil
}

// upgradeAgentTask triggers and confirms one remote environment's upgrade, persisting each stage for the status endpoint.
// A redelivered task skips an agent whose result is already terminal.
func (s *Service) upgradeAgentTask(ctx context.Context, t flow.Task) (_ any, err error) {
	var input upgradeJobInput
	var remote remoteAgent
	if decodeErr := errors.Join(t.Payload(&input), t.DecodeItem(&remote)); decodeErr != nil {
		return nil, decodeErr
	}
	job, err := s.loadUpgradeJob(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	if job.Status != UpgradeJobStatusRunning {
		return nil, nil
	}
	if allowed, authorizeErr := s.authorizeUpgradeJob(ctx, job, input.KeyID); !allowed {
		return nil, authorizeErr
	}
	index := upsertPendingResult(job, remote.ID, remote.Name)
	result := &job.Results[index]
	if result.Status != UpgradeResultStatusPending && result.Status != UpgradeResultStatusUpdating {
		return nil, nil
	}
	// A saved starting or later stage means the trigger may have reached the agent; sending it again could start a second upgrader.
	// Only a later stage proves the agent accepted it.
	triggered := result.Status == UpgradeResultStatusUpdating && result.Stage != "" && result.Stage != UpgradeStageChecking
	unconfirmed := triggered && result.Stage == UpgradeStageStarting
	settle := func(status UpgradeResultStatus, message string) {
		*result = UpgradeJobResult{ //exhaustruct:enforce
			EnvironmentID:   result.EnvironmentID,
			EnvironmentName: result.EnvironmentName,
			Status:          status,
			FromVersion:     result.FromVersion,
			FromDigest:      result.FromDigest,
			ToVersion:       result.ToVersion,
			Stage:           result.Stage,
			StageStartedAt:  result.StageStartedAt,
			Percent:         kit.Ternary(status == UpgradeResultStatusUpdated, 100, result.Percent),
			Logs:            result.Logs,
			Error:           message[:min(len(message), upgradeJobErrorMaxLen)],
		}
	}
	settle(UpgradeResultStatusUpdating, result.Error)
	if persistErr := s.saveResult(ctx, job, index); persistErr != nil {
		slog.WarnContext(ctx, "upgrade job: failed to persist updating status", "jobId", job.ID, "environmentId", remote.ID, "error", persistErr)
	}
	defer func() {
		*result = result.clearStage()
		// Shutdown: leave the row updating at its stage so the redelivered task resumes it.
		if err = ctx.Err(); err != nil {
			return
		}
		if persistErr := s.saveResult(ctx, job, index); persistErr != nil {
			slog.WarnContext(ctx, "upgrade job: failed to persist progress", "jobId", job.ID, "environmentId", remote.ID, "error", persistErr)
		}
	}()

	var info types.VersionInfo
	if triggered {
		info = types.VersionInfo{
			CurrentVersion: result.FromVersion,
			CurrentDigest:  result.FromDigest,
		}
	} else {
		s.setUpdateStage(ctx, job, index, UpgradeStageChecking)
		versionCtx, cancel := context.WithTimeout(ctx, upgradeJobAgentRequestTimeout)
		checkErr := s.environments.ProxyJSONRequest(versionCtx, remote.ID, http.MethodGet, "/api/app/version", nil, &info)
		cancel()
		if checkErr != nil {
			settle(upgradeJobAgentFailureStatus(checkErr), checkErr.Error())
			return nil, nil
		}
		// The manager's newest release outranks the agent's own check, so the recorded target tracks what was sent.
		var triggerBody []byte
		if newest := strings.TrimSpace(s.versionService.GetAppVersionInfo(ctx).NewestVersion); newest != "" {
			body, marshalErr := json.Marshal(TriggerUpgradeBody{TargetVersion: newest})
			if marshalErr != nil {
				slog.WarnContext(ctx, "upgrade job: failed to marshal trigger body", "environmentId", remote.ID, "error", marshalErr)
			} else {
				info.NewestVersion, triggerBody = newest, body // keep: versiontypes.Info is decoded by ProxyJSONRequest and has ~20 fields
			}
		}
		// The saved starting stage keeps a replay from sending the trigger again, so the trigger waits on it.
		*result = UpgradeJobResult{ //exhaustruct:enforce
			EnvironmentID:   result.EnvironmentID,
			EnvironmentName: result.EnvironmentName,
			Status:          result.Status,
			FromVersion:     info.CurrentVersion,
			FromDigest:      info.CurrentDigest,
			ToVersion:       cmp.Or(info.NewestVersion, info.NewestDigest, info.CurrentVersion, info.CurrentDigest),
			Stage:           UpgradeStageStarting,
			StageStartedAt:  new(time.Now()),
			Percent:         0,
			Logs:            nil,
			Error:           result.Error,
		}
		if saveErr := s.saveResult(ctx, job, index); saveErr != nil {
			settle(UpgradeResultStatusFailed, "Could not save upgrade progress: "+saveErr.Error())
			return nil, nil
		}
		triggerCtx, cancel := context.WithTimeout(ctx, upgradeJobAgentRequestTimeout)
		resp, triggerErr := s.environments.ExecuteRemoteRequest(triggerCtx, remote.ID, http.MethodPost, "/api/environments/0/system/upgrade", triggerBody)
		cancel()
		if triggerErr == nil {
			triggerErr = resp.RequireSuccess()
		}
		if triggerErr != nil {
			settle(UpgradeResultStatusFailed, triggerErr.Error())
			return nil, nil
		}

		// An agent already on the target finds nothing to swap in; confirming would only burn the poll window.
		if info.AlreadyOnNewest() {
			*result = UpgradeJobResult{ //exhaustruct:enforce
				EnvironmentID:   result.EnvironmentID,
				EnvironmentName: result.EnvironmentName,
				Status:          UpgradeResultStatusUpToDate,
				FromVersion:     result.FromVersion,
				FromDigest:      result.FromDigest,
				ToVersion:       info.CurrentVersion,
				Stage:           result.Stage,
				StageStartedAt:  result.StageStartedAt,
				Percent:         100,
				Logs:            result.Logs,
				Error:           result.Error,
			}
			return nil, nil
		}
	}

	// An unconfirmed request keeps its starting stage, so another restart still knows nothing confirmed it.
	stage := func(next UpgradeStage) {
		if !unconfirmed {
			s.setUpdateStage(ctx, job, index, next)
		}
	}
	sink := &progressSink{service: s, job: job, index: index, keepStage: unconfirmed}
	waitCtx, cancel := context.WithTimeout(ctx, upgradeJobConfirmTimeout)
	defer cancel()
	runtimeChanged, unsubscribe := s.environments.SubscribeRuntimeChanges()
	defer unsubscribe()
	for backoff := time.Second; waitCtx.Err() == nil; backoff = min(2*backoff, upgradeJobRedialMaxBackoff) {
		doneMessage, done, streamErr := s.followAgentProgress(waitCtx, remote.ID, sink)
		sink.flush(ctx)
		statusErr, _ := errors.AsType[*remenv.StatusError](streamErr)
		legacy := slices.Contains([]int{http.StatusNotFound, http.StatusMethodNotAllowed}, kit.FromPtr(statusErr).StatusCode)
		if done || legacy {
			stage(UpgradeStageVerifying)
			versionCtx, versionCancel := context.WithTimeout(waitCtx, upgradeJobAgentRequestTimeout)
			var current types.VersionInfo
			streamErr = s.environments.ProxyJSONRequest(versionCtx, remote.ID, http.MethodGet, "/api/app/version", nil, &current)
			versionCancel()
			switch {
			case streamErr != nil:
			// An unconfirmed request proves nothing by the agent merely matching the target, so it needs a real change.
			case upgradeLanded(
				info.CurrentVersion,
				info.CurrentDigest,
				kit.Ternary(unconfirmed || strings.TrimPrefix(result.ToVersion, "v") == strings.TrimPrefix(info.CurrentVersion, "v") || result.ToVersion == info.CurrentDigest, "", result.ToVersion),
				current.CurrentVersion,
				current.CurrentDigest,
			):
				settle(UpgradeResultStatusUpdated, result.Error)
				return nil, nil
			case done:
				settle(UpgradeResultStatusFailed, cmp.Or(doneMessage, "Agent finished the upgrade without changing version"))
				return nil, nil
			}
		}
		slog.DebugContext(ctx, "upgrade job: agent progress unavailable", "environmentId", remote.ID, "legacy", legacy, "error", streamErr)
		stage(kit.Ternary(streamErr == nil, UpgradeStageVerifying, UpgradeStageReconnecting))
		select {
		case <-waitCtx.Done():
		case <-runtimeChanged:
		case <-time.After(kit.Ternary(legacy, upgradeJobLegacyPollInterval, backoff)):
		}
	}
	if ctx.Err() != nil {
		return nil, nil
	}
	if unconfirmed {
		settle(UpgradeResultStatusFailed, "Upgrade request was interrupted before the agent confirmed it")
		return nil, nil
	}
	settle(UpgradeResultStatusTriggered, result.Error)
	return nil, nil
}

// upgradeManagerTask triggers the manager's own self-upgrade last. The job is persisted pending_restart BEFORE the
// trigger, so a redelivery never triggers twice and the next boot finalizes it.
func (s *Service) upgradeManagerTask(ctx context.Context, t flow.Task) (any, error) {
	var input upgradeJobInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	job, err := s.loadUpgradeJob(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	allowed, err := func() (bool, error) {
		if job.Status != UpgradeJobStatusRunning {
			return false, nil
		}
		return s.authorizeUpgradeJob(ctx, job, input.KeyID)
	}()
	if err != nil {
		return nil, err
	}
	manager := slices.IndexFunc(job.Results, func(r UpgradeJobResult) bool {
		return r.EnvironmentID == environment.LocalEnvironmentID
	})
	if allowed && manager < 0 {
		s.finalizeUpgradeJob(ctx, job, "", "")
	}
	if allowed && manager >= 0 {
		// Every agent task has finished, so an agent row still in progress belongs to a task that failed first.
		results := make(UpgradeJobResults, 0, len(job.Results))
		for _, row := range job.Results {
			switch {
			case row.EnvironmentID == environment.LocalEnvironmentID:
				results = append(results, UpgradeJobResult{ //exhaustruct:enforce
					EnvironmentID:   row.EnvironmentID,
					EnvironmentName: row.EnvironmentName,
					Status:          UpgradeResultStatusUpdating,
					FromVersion:     row.FromVersion,
					FromDigest:      row.FromDigest,
					ToVersion:       row.ToVersion,
					Stage:           UpgradeStageStarting,
					StageStartedAt:  new(time.Now()),
					Percent:         0,
					Logs:            row.Logs,
					Error:           row.Error,
				})
			case row.Status == UpgradeResultStatusPending || row.Status == UpgradeResultStatusUpdating:
				results = append(results, UpgradeJobResult{ //exhaustruct:enforce
					EnvironmentID:   row.EnvironmentID,
					EnvironmentName: row.EnvironmentName,
					Status:          UpgradeResultStatusFailed,
					FromVersion:     row.FromVersion,
					FromDigest:      row.FromDigest,
					ToVersion:       row.ToVersion,
					Stage:           "",
					StageStartedAt:  nil,
					Percent:         row.Percent,
					Logs:            row.Logs,
					Error:           "Agent upgrade did not finish",
				})
			default:
				results = append(results, row)
			}
		}
		*job = UpgradeJob{ //exhaustruct:enforce
			BaseModel:             job.BaseModel,
			Status:                UpgradeJobStatusPendingRestart,
			UserID:                job.UserID,
			Username:              job.Username,
			ManagerVersionAtStart: job.ManagerVersionAtStart,
			ManagerDigestAtStart:  job.ManagerDigestAtStart,
			ManagerTargetVersion:  job.ManagerTargetVersion,
			Results:               results,
			Error:                 job.Error,
			CompletedAt:           job.CompletedAt,
		}
		// A replay that misses pending_restart would trigger the manager upgrade again, so the trigger waits on it.
		if savePendingRestartErr := s.saveJob(ctx, job); savePendingRestartErr != nil {
			return nil, fmt.Errorf("persist pending_restart before manager upgrade: %w", savePendingRestartErr)
		}
		go s.triggerManagerUpgrade(context.WithoutCancel(ctx), job, manager)
		return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Upgrade job finished; " + string(UpgradeJobStatusPendingRestart)}, nil
	}
	if job.Status == UpgradeJobStatusFailed {
		return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: kit.FromPtr(job.Error)}, nil
	}
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: "Upgrade job finished; " + string(job.Status)}, nil
}

func (s *Service) triggerManagerUpgrade(ctx context.Context, job *UpgradeJob, manager int) {
	// Only a manager already on the newest image makes a no-op upgrader run expected; checked after the fact it proves nothing.
	wasAlreadyNewest := s.AlreadyOnNewestImage(ctx)
	_, events, _, unsubscribe := s.progress.subscribe()
	defer unsubscribe()
	sink := &progressSink{service: s, job: job, index: manager}
	var upgraderID string
	var triggerErr error
	triggered := make(chan struct{})
	go func() {
		defer close(triggered)
		upgraderID, triggerErr = s.TriggerUpgradeViaCLI(ctx, types.Actor{ID: job.UserID, Username: job.Username}, updater.SelfUpdateTarget{})
	}()
	sink.follow(ctx, events, triggered)
	if triggerErr != nil {
		// No restart is coming, so this flips the manager's updating row to failed now.
		s.markUpgradeJobFailed(ctx, job, fmt.Sprintf("manager upgrade trigger failed: %v", triggerErr))
		return
	}
	slog.InfoContext(ctx, "Upgrade job: agents done, manager self-upgrade triggered", "jobId", job.ID, "upgraderId", upgraderID)
	s.watchManagerUpgrader(ctx, sink, events, upgraderID, wasAlreadyNewest)
}

// watchManagerUpgrader closes out a pending_restart job whose manager was never recreated, which a recreate would have
// prevented by stopping this process. wasAlreadyNewest is the pre-trigger check that a no-op run was expected.
func (s *Service) watchManagerUpgrader(
	ctx context.Context,
	sink *progressSink,
	events <-chan types.UpgradeProgressEvent,
	upgraderID string,
	wasAlreadyNewest bool,
) {
	exit, exitCode := upgraderExitUnobserved, int64(0)
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		exit, exitCode = s.waitForUpgrader(ctx, upgraderID)
	}()
	sink.follow(ctx, events, observed)
	job := sink.job

	// Let a recreate already underway stop this process; an unobserved run waits out the stale window instead.
	select {
	case <-ctx.Done():
		return
	case <-time.After(kit.Ternary(exit == upgraderExitUnobserved, time.Until(job.CreatedAt.Add(upgradeJobStaleThreshold)), upgradeJobManagerNoRestartGrace)):
	}

	job, err := s.loadUpgradeJob(ctx, job.ID)
	if err != nil {
		slog.WarnContext(ctx, "upgrade job: failed to reload job after upgrader exit", "upgraderId", upgraderID, "error", err)
		return
	}
	if job.Status != UpgradeJobStatusPendingRestart {
		return
	}

	info := s.versionService.GetAppVersionInfo(ctx)
	switch {
	case exit == upgraderExitUnobserved:
		slog.WarnContext(ctx, "upgrade job: manager upgrade was never observed and no restart followed", "jobId", job.ID)
		s.markUpgradeJobFailed(ctx, job, "manager upgrade could not be observed: the upgrader run was never seen to finish and the manager did not restart")
	case exit == upgraderExitFailed:
		s.markUpgradeJobFailed(ctx, job, fmt.Sprintf("manager upgrade failed: upgrader exited with code %d", exitCode))
	case exit == upgraderExitCodeLost && (!wasAlreadyNewest || !info.AlreadyOnNewest()):
		// Without an exit code a no-op is only credible when the check said so both before and after the run.
		s.markUpgradeJobFailed(ctx, job, "manager upgrade could not be confirmed: the upgrader exited without a readable status and this manager was not a confirmed no-op upgrade")
	default:
		slog.InfoContext(ctx, "Upgrade job: manager was already up to date; no restart needed", "jobId", job.ID, "version", info.CurrentVersion)
		s.finalizeUpgradeJob(ctx, job, UpgradeResultStatusUpToDate, info.CurrentVersion)
	}
}

func (s *Service) waitForUpgrader(ctx context.Context, upgraderID string) (upgraderExit, int64) {
	exit, exitCode := upgraderExitUnobserved, int64(0)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.WarnContext(ctx, "upgrade job: cannot watch upgrader container", "upgraderId", upgraderID, "error", err)
	} else {
		waitCtx, cancel := context.WithTimeout(ctx, upgradeJobManagerWatchTimeout)
		wait := dockerClient.ContainerWait(waitCtx, upgraderID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
		select {
		case result, ok := <-wait.Result:
			// A closed channel yields a zero response that looks like exit 0; a wait error comes with a filler code 0.
			switch {
			case !ok:
				slog.WarnContext(ctx, "upgrade job: upgrader wait ended without a status", "upgraderId", upgraderID)
			case result.StatusCode != 0:
				exit, exitCode = upgraderExitFailed, result.StatusCode
			case result.Error != nil:
				slog.WarnContext(ctx, "upgrade job: upgrader wait reported an error", "upgraderId", upgraderID, "error", result.Error.Message)
				exit = upgraderExitCodeLost
			default:
				exit = upgraderExitSucceeded
			}
		case waitErr := <-wait.Error:
			// AutoRemove can delete a fast upgrader before the wait lands: it exited, but its code went with it.
			if errdefs.IsNotFound(waitErr) {
				exit = upgraderExitCodeLost
			} else {
				slog.WarnContext(ctx, "upgrade job: failed waiting for upgrader container", "upgraderId", upgraderID, "error", waitErr)
			}
		case <-waitCtx.Done():
			slog.WarnContext(ctx, "upgrade job: timed out waiting for upgrader container", "upgraderId", upgraderID)
		}
		cancel()
	}
	return exit, exitCode
}

// upsertPendingResult returns the index of envID's result row, appending a pending row when seeding missed it.
func upsertPendingResult(job *UpgradeJob, envID, envName string) int {
	if idx := slices.IndexFunc(job.Results, func(r UpgradeJobResult) bool { return r.EnvironmentID == envID }); idx >= 0 {
		return idx
	}
	pending := UpgradeJobResult{
		EnvironmentID:   envID,
		EnvironmentName: envName,
		Status:          UpgradeResultStatusPending,
	}
	*job = UpgradeJob{ //exhaustruct:enforce
		BaseModel:             job.BaseModel,
		Status:                job.Status,
		UserID:                job.UserID,
		Username:              job.Username,
		ManagerVersionAtStart: job.ManagerVersionAtStart,
		ManagerDigestAtStart:  job.ManagerDigestAtStart,
		ManagerTargetVersion:  job.ManagerTargetVersion,
		Results:               append(job.Results, pending),
		Error:                 job.Error,
		CompletedAt:           job.CompletedAt,
	}
	return len(job.Results) - 1
}

// upgradeJobAgentFailureStatus classifies a failed agent pre-check: a reached environment whose request failed or timed
// out is a failure; only a never-reachable one is an offline skip.
func upgradeJobAgentFailureStatus(err error) UpgradeResultStatus {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return UpgradeResultStatusFailed
	}
	if _, ok := errors.AsType[*remenv.StatusError](err); ok {
		return UpgradeResultStatusFailed
	}
	return UpgradeResultStatusSkippedOffline
}

// setUpdateStage records result's step and persists the job; re-entering the current stage keeps its start time.
func (s *Service) setUpdateStage(ctx context.Context, job *UpgradeJob, index int, stage UpgradeStage) {
	result := &job.Results[index]
	if result.Stage == stage {
		return
	}
	*result = UpgradeJobResult{ //exhaustruct:enforce
		EnvironmentID:   result.EnvironmentID,
		EnvironmentName: result.EnvironmentName,
		Status:          result.Status,
		FromVersion:     result.FromVersion,
		FromDigest:      result.FromDigest,
		ToVersion:       result.ToVersion,
		Stage:           stage,
		StageStartedAt:  new(time.Now()),
		Percent:         result.Percent,
		Logs:            result.Logs,
		Error:           result.Error,
	}
	if err := s.saveResult(ctx, job, index); err != nil {
		slog.WarnContext(ctx, "upgrade job: failed to persist stage", "jobId", job.ID, "environmentId", result.EnvironmentID, "stage", stage, "error", err)
	}
}

func (s *Service) saveResult(ctx context.Context, job *UpgradeJob, index int) error {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	latest, err := s.loadUpgradeJob(ctx, job.ID)
	if err != nil {
		return err
	}
	row := job.Results[index]
	at := slices.IndexFunc(latest.Results, func(candidate UpgradeJobResult) bool {
		return candidate.EnvironmentID == row.EnvironmentID
	})
	if at < 0 {
		latest.Results = append(latest.Results, row)
	} else {
		latest.Results[at] = row
	}
	return s.saveJob(ctx, latest)
}

// GetCurrentUpgradeJob returns the most recently created upgrade job, or nil.
func (s *Service) GetCurrentUpgradeJob(ctx context.Context) (*UpgradeJob, error) {
	return s.findUpgradeJob(ctx)
}

func (s *Service) saveJob(ctx context.Context, job *UpgradeJob) error {
	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		return err
	}
	snapshot, err := mapping.MapOne[*UpgradeJob, types.UpgradeJob](job)
	if err != nil {
		slog.WarnContext(ctx, "upgrade job: failed to map job for the stream", "jobId", job.ID, "error", err)
		return nil
	}
	s.latestJob.Store(&snapshot)
	select {
	case s.jobChanged <- struct{}{}:
	default:
	}
	return nil
}

func (s *Service) RunStreamProducer(ctx context.Context, events chan<- types.UpgradeJob) {
	s.jobHub.Subscribe(ctx, "current",
		func(runCtx context.Context, publish func(types.UpgradeJob)) {
			job, err := s.GetCurrentUpgradeJob(runCtx)
			switch {
			case err != nil:
				slog.WarnContext(runCtx, "upgrade job: failed to load current job for the stream", "error", err)
			case job != nil:
				if snapshot, mapErr := mapping.MapOne[*UpgradeJob, types.UpgradeJob](job); mapErr == nil {
					publish(snapshot)
				}
			}
			for {
				select {
				case <-runCtx.Done():
					return
				case <-s.jobChanged:
					if latest := s.latestJob.Load(); latest != nil {
						publish(*latest)
					}
				}
			}
		},
		func(job types.UpgradeJob) bool {
			return agg.Send(ctx, events, job)
		})
}

func (s *Service) StreamProgress(ctx context.Context, deliver func(types.UpgradeProgressEvent) bool) {
	history, live, active, unsubscribe := s.progress.subscribe()
	defer unsubscribe()
	if !active && len(history) == 0 {
		lines, err := readUpgradeLog(libarcane.UpgradeLogDirectory, time.Now().Add(-upgradeJobManagerWatchTimeout))
		if err != nil {
			slog.WarnContext(ctx, "Failed to replay upgrade log", "error", err)
		}
		info := kit.FromPtr(s.versionService.GetAppVersionInfo(ctx))
		for _, line := range lines {
			if !deliver(types.UpgradeProgressEvent{
				Type:      types.UpgradeProgressEventLog,
				Message:   line,
				ToVersion: info.CurrentVersion,
				At:        time.Now(),
				Replay:    true,
			}) {
				return
			}
		}
		deliver(types.UpgradeProgressEvent{
			Type:      types.UpgradeProgressEventDone,
			Percent:   100,
			ToVersion: info.CurrentVersion,
			At:        time.Now(),
			Replay:    true,
		})
		return
	}
	for _, update := range history {
		if !deliver(update) {
			return
		}
	}
	if !active {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-live:
			if !ok || !deliver(update) || update.Type == types.UpgradeProgressEventDone {
				return
			}
		}
	}
}

func (s *Service) followAgentProgress(ctx context.Context, envID string, sink *progressSink) (string, bool, error) {
	resp, err := s.environments.OpenRemoteStream(ctx, envID, "/api/environments/0/system/upgrade/progress")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	doneMessage, done := "", false
	sink.replaying = false
	err = readProgressStream(resp.Body, func(update types.UpgradeProgressEvent) bool {
		sink.apply(ctx, update)
		doneMessage, done = update.Message, update.Type == types.UpgradeProgressEventDone
		return !done
	})
	if err == nil && !done {
		err = io.ErrUnexpectedEOF
	}
	return doneMessage, done, err
}

type progressSink struct {
	service   *Service
	job       *UpgradeJob
	index     int
	keepStage bool
	dirty     bool
	savedAt   time.Time
	replaying bool
	baseSet   bool
	baseLogs  []string
}

func (p *progressSink) apply(ctx context.Context, update types.UpgradeProgressEvent) {
	row := &p.job.Results[p.index]
	stageChanged := update.Type == types.UpgradeProgressEventStage && !p.keepStage && UpgradeStage(update.Message) != row.Stage
	logs := row.Logs
	if update.Replay && !p.replaying {
		if !p.baseSet {
			p.baseLogs, p.baseSet = row.Logs, true
		}
		logs, p.replaying = p.baseLogs, true
	}
	*row = UpgradeJobResult{
		EnvironmentID:   row.EnvironmentID,
		EnvironmentName: row.EnvironmentName,
		Status:          row.Status,
		FromVersion:     row.FromVersion,
		FromDigest:      row.FromDigest,
		ToVersion:       row.ToVersion,
		Stage:           kit.Ternary(stageChanged, UpgradeStage(update.Message), row.Stage),
		StageStartedAt:  kit.Ternary(stageChanged, new(time.Now()), row.StageStartedAt),
		Percent:         max(row.Percent, update.Percent),
		Logs:            kit.Ternary(update.Type == types.UpgradeProgressEventLog, appendLogs(logs, update.Message), logs),
		Error:           row.Error,
	}
	p.dirty = true
	if stageChanged || update.Type == types.UpgradeProgressEventDone || time.Since(p.savedAt) >= upgradeJobPublishInterval {
		p.flush(ctx)
	}
}

func (p *progressSink) flush(ctx context.Context) {
	if !p.dirty {
		return
	}
	if err := p.service.saveResult(ctx, p.job, p.index); err != nil {
		slog.WarnContext(ctx, "upgrade job: failed to persist progress", "jobId", p.job.ID, "environmentId", p.job.Results[p.index].EnvironmentID, "error", err)
	}
	p.dirty, p.savedAt = false, time.Now()
}

func (p *progressSink) follow(ctx context.Context, events <-chan types.UpgradeProgressEvent, done <-chan struct{}) {
	for {
		select {
		case <-done:
			p.flush(ctx)
			return
		case update, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			p.apply(ctx, update)
		}
	}
}

// findUpgradeJob returns the newest job matching the inline GORM conditions, or nil when none does.
func (s *Service) findUpgradeJob(ctx context.Context, conds ...any) (*UpgradeJob, error) {
	var jobs []UpgradeJob
	if err := s.db.WithContext(ctx).Order("created_at DESC").Limit(1).Find(&jobs, conds...).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

// loadUpgradeJob loads a job by ID, failing when it no longer exists.
func (s *Service) loadUpgradeJob(ctx context.Context, id string) (*UpgradeJob, error) {
	job, err := s.findUpgradeJob(ctx, "id = ?", id)
	if err == nil && job == nil {
		err = fmt.Errorf("upgrade job %s not found", id)
	}
	return job, err
}

// authorizeUpgradeJob reports whether the job's requester may still upgrade, failing the job when not.
// A task recovered after a restart never passed the request's permission check. Shutdown returns its
// error so the task resumes instead of failing the job.
func (s *Service) authorizeUpgradeJob(ctx context.Context, job *UpgradeJob, keyID string) (bool, error) {
	permissions, err := s.roles.ResolveExecutionPermissions(ctx, job.UserID, keyID)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err == nil && permissions.Allows(authz.PermSystemUpgrade, environment.LocalEnvironmentID) {
		return true, nil
	}
	s.markUpgradeJobFailed(ctx, job, "requesting user no longer has permission to upgrade")
	return false, nil
}

func (s *Service) markUpgradeJobFailed(ctx context.Context, job *UpgradeJob, reason string) {
	results := make(UpgradeJobResults, 0, len(job.Results))
	for _, row := range job.Results {
		if row.Status != UpgradeResultStatusUpdating && row.Status != UpgradeResultStatusPending {
			results = append(results, row.clearStage())
			continue
		}
		results = append(results, UpgradeJobResult{ //exhaustruct:enforce
			EnvironmentID:   row.EnvironmentID,
			EnvironmentName: row.EnvironmentName,
			Status:          UpgradeResultStatusFailed,
			FromVersion:     row.FromVersion,
			FromDigest:      row.FromDigest,
			ToVersion:       row.ToVersion,
			Stage:           "",
			StageStartedAt:  nil,
			Percent:         row.Percent,
			Logs:            row.Logs,
			Error:           reason,
		})
	}
	*job = UpgradeJob{ //exhaustruct:enforce
		BaseModel:             job.BaseModel,
		Status:                UpgradeJobStatusFailed,
		UserID:                job.UserID,
		Username:              job.Username,
		ManagerVersionAtStart: job.ManagerVersionAtStart,
		ManagerDigestAtStart:  job.ManagerDigestAtStart,
		ManagerTargetVersion:  job.ManagerTargetVersion,
		Results:               results,
		Error:                 &reason,
		CompletedAt:           new(time.Now()),
	}
	if err := s.saveJob(ctx, job); err != nil {
		slog.WarnContext(ctx, "upgrade job: failed to mark job failed", "jobId", job.ID, "error", err)
	}

	// LogUserEvent always records an info-severity "completed" event, so create the error event directly.
	if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:        event.EventTypeSystemUpgrade,
		Severity:    event.EventSeverityError,
		Title:       "Upgrade job failed",
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
		slog.WarnContext(ctx, "upgrade job: failed to log failure event", "jobId", job.ID, "error", err)
	}

	slog.WarnContext(ctx, "Upgrade job failed", "jobId", job.ID, "reason", reason)
}

// finalizeUpgradeJob settles the manager row, completes the job and records the audit event.
func (s *Service) finalizeUpgradeJob(ctx context.Context, job *UpgradeJob, managerStatus UpgradeResultStatus, managerVersion string) {
	results := make(UpgradeJobResults, 0, len(job.Results))
	failed := 0
	for _, row := range job.Results {
		switch {
		case row.EnvironmentID == environment.LocalEnvironmentID:
			row = UpgradeJobResult{ //exhaustruct:enforce
				EnvironmentID:   row.EnvironmentID,
				EnvironmentName: row.EnvironmentName,
				Status:          managerStatus,
				FromVersion:     row.FromVersion,
				FromDigest:      row.FromDigest,
				ToVersion:       kit.Ternary(managerStatus == UpgradeResultStatusFailed, row.ToVersion, managerVersion),
				Stage:           row.Stage,
				StageStartedAt:  row.StageStartedAt,
				Percent:         kit.Ternary(managerStatus == UpgradeResultStatusFailed, row.Percent, 100),
				Logs:            row.Logs,
				Error:           kit.Ternary(managerStatus == UpgradeResultStatusFailed, "manager version did not change after upgrade", row.Error),
			}
		case row.Status == UpgradeResultStatusPending || row.Status == UpgradeResultStatusUpdating:
			row = UpgradeJobResult{ //exhaustruct:enforce
				EnvironmentID:   row.EnvironmentID,
				EnvironmentName: row.EnvironmentName,
				Status:          UpgradeResultStatusFailed,
				FromVersion:     row.FromVersion,
				FromDigest:      row.FromDigest,
				ToVersion:       row.ToVersion,
				Stage:           row.Stage,
				StageStartedAt:  row.StageStartedAt,
				Percent:         row.Percent,
				Logs:            row.Logs,
				Error:           "Agent upgrade did not finish",
			}
		}
		if row.Status == UpgradeResultStatusFailed {
			failed++
		}
		results = append(results, row.clearStage())
	}
	*job = UpgradeJob{ //exhaustruct:enforce
		BaseModel:             job.BaseModel,
		Status:                UpgradeJobStatusCompleted,
		UserID:                job.UserID,
		Username:              job.Username,
		ManagerVersionAtStart: job.ManagerVersionAtStart,
		ManagerDigestAtStart:  job.ManagerDigestAtStart,
		ManagerTargetVersion:  job.ManagerTargetVersion,
		Results:               results,
		Error:                 job.Error,
		CompletedAt:           new(time.Now()),
	}
	if err := s.saveJob(ctx, job); err != nil {
		slog.WarnContext(ctx, "upgrade job: failed to finalize job", "jobId", job.ID, "error", err)
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
			slog.WarnContext(ctx, "Failed to log upgrade job event", "jobId", job.ID, "error", err)
		}
		return
	}
	// Completed with failures: a warning event keeps them visible in the audit log.
	if _, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:        event.EventTypeSystemUpgrade,
		Severity:    event.EventSeverityWarning,
		Title:       "Upgrade job completed with errors",
		Description: fmt.Sprintf("%d of %d environments failed to update", failed, len(job.Results)),
		UserID:      new(job.UserID),
		Username:    new(job.Username),
		Metadata:    metadata,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to log upgrade job event", "jobId", job.ID, "error", err)
	}
}

// PruneUpgradeLogs removes expired upgrade logs from this instance's data directory.
func (s *Service) PruneUpgradeLogs(ctx context.Context, dataDir string, now time.Time) (int, error) {
	retentionDays := s.settingsService.GetIntSetting(ctx, "upgradeLogRetentionDays")
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
