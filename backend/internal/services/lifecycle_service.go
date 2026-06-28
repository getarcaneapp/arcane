package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutils "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	buildapi "go.getarcane.app/builds/api"
)

// Lifecycle hook configuration limits and conventions.
const (
	// lifecycleWorkspaceMount is the in-container path the project dir is
	// bind-mounted to. Scripts run with this as their working dir.
	lifecycleWorkspaceMount = "/workspace"
	// lifecycleMaxOutputBytes caps the stdout/stderr we retain on the
	// GitOpsSync row. The full stream is still consumed; the tail is dropped.
	lifecycleMaxOutputBytes = 16 * 1024
	// lifecycleDefaultTimeoutSec is used when a sync has no per-sync timeout
	// configured.
	lifecycleDefaultTimeoutSec = 60
	// lifecycleDefaultMaxTimeoutSec mirrors the settings default so callers
	// always have a sane upper bound even if settings can't be loaded.
	lifecycleDefaultMaxTimeoutSec = 300
	// lifecycleStreamDrainTimeout bounds how long we wait for the log copy
	// goroutine to drain after the container exits, before force-closing.
	// Mirrors the value used by VulnerabilityService for the same purpose.
	lifecycleStreamDrainTimeout = 30 * time.Second
)

// Last-run status values written to GitOpsSync.PreDeployLastRunStatus.
const (
	lifecycleStatusSuccess = "success"
	lifecycleStatusFailed  = "failed"
	lifecycleStatusTimeout = "timeout"
)

// lifecycleEnvKeyRegex enforces POSIX-style identifier syntax for env keys
// both in admin-configured Env and in scripts' KEY=VALUE stdout capture.
var lifecycleEnvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// lifecycleExtraMount is one admin-configured bind mount for the lifecycle
// runner container. Stored on the GitOpsSync row as docker-CLI-style
// "src:tgt[:ro|:rw]" entries, one per line; never sourced from repo data.
type lifecycleExtraMount struct {
	Source   string
	Target   string
	Readonly bool
}

// LifecycleService runs pre-deploy lifecycle hooks declared on a project's
// GitOps sync. A hook is a script in the synced repo executed in a throwaway
// container immediately before the project is deployed, with optional capture
// of stdout as environment variables merged into the compose env.
//
// Trust model: the script is repo-trusted code, equivalent to compose.yaml in
// the same repo. Anyone who can push to that repo can change what the script
// does on the next deploy. The trust event is configuring a script path on
// the GitOps sync, not each individual deploy.
type LifecycleService struct {
	db              *database.DB
	settingsService *SettingsService
	eventService    *EventService
	dockerService   *DockerClientService
}

// NewLifecycleService constructs a LifecycleService wired against shared
// infrastructure. The Docker client is obtained lazily on each hook run via
// dockerService.GetClient so reconnects are transparent.
func NewLifecycleService(db *database.DB, settingsService *SettingsService, eventService *EventService, dockerService *DockerClientService) *LifecycleService {
	return &LifecycleService{
		db:              db,
		settingsService: settingsService,
		eventService:    eventService,
		dockerService:   dockerService,
	}
}

// RunPreDeploy executes the pre-deploy lifecycle hook for a project, if one
// is configured on its GitOps sync.
//
// Callers should invoke this unconditionally before deploying — when no hook
// is configured, when lifecycle hooks are disabled globally, or when the
// project is not GitOps-managed, this is a no-op and returns nil.
//
// A non-zero exit code, a script timeout, or any infrastructure failure
// returns an error that aborts the deploy. The last-run state on the
// GitOpsSync row is updated on every invocation that reaches the run step,
// regardless of outcome.
func (s *LifecycleService) RunPreDeploy(ctx context.Context, project *models.Project, actor models.User) error {
	if project == nil || project.GitOpsManagedBy == nil || *project.GitOpsManagedBy == "" {
		return nil
	}
	if !s.settingsService.GetBoolSetting(ctx, "lifecycleEnabled", false) {
		return nil
	}

	sync, err := s.loadGitOpsSyncForProjectInternal(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("failed to load gitops sync for lifecycle hook: %w", err)
	}
	if sync == nil || sync.PreDeployScriptPath == nil || strings.TrimSpace(*sync.PreDeployScriptPath) == "" {
		return nil
	}

	return s.executePreDeployInternal(ctx, project, sync, actor)
}

func (s *LifecycleService) executePreDeployInternal(ctx context.Context, project *models.Project, sync *models.GitOpsSync, actor models.User) error {
	runnerImage := s.resolveRunnerImageInternal(ctx, sync)
	if runnerImage == "" {
		return fmt.Errorf("pre-deploy script %q is configured but no runner image is set on the GitOps sync or lifecycleDefaultRunnerImage setting", *sync.PreDeployScriptPath)
	}

	scriptPath := strings.TrimSpace(*sync.PreDeployScriptPath)
	if err := validateScriptPathInternal(project.Path, scriptPath); err != nil {
		return fmt.Errorf("invalid pre-deploy script path: %w", err)
	}

	hookEnv, err := parseLifecycleEnvTextInternal(sync.PreDeployEnv)
	if err != nil {
		return fmt.Errorf("invalid lifecycle env config: %w", err)
	}
	extraMounts, err := parseLifecycleExtraMountsTextInternal(sync.PreDeployExtraMounts)
	if err != nil {
		return fmt.Errorf("invalid lifecycle extra mounts config: %w", err)
	}

	timeout := s.resolveTimeoutInternal(ctx, sync.PreDeployTimeoutSec)

	slog.InfoContext(ctx, "running pre-deploy lifecycle hook",
		"projectID", project.ID,
		"syncID", sync.ID,
		"scriptPath", scriptPath,
		"runnerImage", runnerImage,
		"timeoutSec", int(timeout/time.Second),
	)

	start := time.Now()
	stdoutContent, stderrContent, exitCode, runErr := s.runScriptInContainerInternal(
		ctx,
		runnerImage,
		project.Path,
		scriptPath,
		hookEnv,
		extraMounts,
		sync.PreDeployNetworkMode,
		timeout,
	)
	durationMs := time.Since(start).Milliseconds()

	status := lifecycleStatusForResultInternal(exitCode, runErr)
	// stdoutBuf/stderrBuf already enforce lifecycleMaxOutputBytes each with a
	// proper "...<truncated>" marker, so the combined string is already
	// bounded and we don't slice again here — a second byte-boundary cut
	// could land mid-UTF-8 codepoint and produce garbled output.
	persistedOutput := combineLifecycleOutputInternal(stdoutContent, stderrContent)
	s.persistLastRunInternal(ctx, sync.ID, status, persistedOutput, start)
	s.emitLifecycleEventInternal(ctx, project, sync, runnerImage, status, exitCode, durationMs, runErr, actor)

	if runErr != nil {
		return runErr
	}
	if exitCode != 0 {
		return fmt.Errorf("pre-deploy script exited with status %d", exitCode)
	}
	return nil
}

func (s *LifecycleService) resolveRunnerImageInternal(ctx context.Context, sync *models.GitOpsSync) string {
	if sync != nil {
		runnerImage := strings.TrimSpace(utils.DerefString(sync.PreDeployRunnerImage))
		if runnerImage != "" {
			return runnerImage
		}
	}
	return strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage", "alpine:latest"))
}

// runScriptInContainerInternal performs the docker run + log capture + wait.
// On the happy path it returns stdout, stderr, the script's exit code and a
// nil error. context.DeadlineExceeded from the per-run timeout is wrapped and
// returned as the error so callers can distinguish a timeout from a non-zero
// exit.
func (s *LifecycleService) runScriptInContainerInternal(
	ctx context.Context,
	runnerImage string,
	projectPath string,
	scriptPath string,
	hookEnv map[string]string,
	extraMounts []lifecycleExtraMount,
	networkMode string,
	timeout time.Duration,
) (stdoutContent string, stderrContent string, exitCode int64, err error) {
	dockerClient, dErr := s.dockerService.GetClient(ctx)
	if dErr != nil {
		return "", "", 0, fmt.Errorf("failed to connect to Docker: %w", dErr)
	}

	if err := s.ensureRunnerImageInternal(ctx, dockerClient, runnerImage); err != nil {
		return "", "", 0, fmt.Errorf("failed to ensure runner image %s: %w", runnerImage, err)
	}

	// Resolve the workspace mount. When Arcane runs inside a container whose
	// /app/data is backed by a named volume, a plain bind-mount of the
	// translated host path (e.g. /var/lib/docker/volumes/.../_data/projects/X)
	// is unreliable — Docker Desktop on WSL2 refuses it outright, and any
	// daemon with non-trivial volume storage may behave differently. Using
	// the named volume directly with VolumeOptions.Subpath sidesteps the
	// translation entirely. For host-bind /app/data the helper returns a
	// plain bind that points at the right host subdir. If Arcane is running
	// on the host (no container inspect available), fall back to a bind on
	// the project path as-is.
	workspaceMount, mountErr := dockerutils.MountForCurrentContainerSubpath(ctx, dockerClient, projectPath, lifecycleWorkspaceMount)
	if mountErr != nil {
		slog.WarnContext(ctx, "failed to derive workspace mount; falling back to bind on project path", "projectPath", projectPath, "error", mountErr)
	}
	if workspaceMount == nil {
		workspaceMount = &mounttypes.Mount{Type: mounttypes.TypeBind, Source: projectPath, Target: lifecycleWorkspaceMount}
	}

	// Cmd is the script path alone — no interpreter wrapper. The script's
	// shebang selects the interpreter (which must exist in the runner image),
	// and the file must be executable on the host (git preserves +x through
	// clone, so a committed +x script works transparently). This keeps
	// Arcane out of the language-choice business and matches standard
	// docker run semantics.
	//
	// Entrypoint is explicitly cleared because many purpose-built images set
	// ENTRYPOINT to their primary tool (e.g. the official getsops/sops image
	// has ENTRYPOINT=["sops"]). Without this override, our Cmd would become
	// an argument to that tool rather than replacing it.
	config := &containertypes.Config{
		Image:        runnerImage,
		Entrypoint:   []string{},
		Cmd:          []string{filepath.ToSlash(filepath.Join(lifecycleWorkspaceMount, scriptPath))},
		WorkingDir:   lifecycleWorkspaceMount,
		Env:          envMapToSliceInternal(hookEnv),
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Labels: map[string]string{
			libarcane.InternalResourceLabel: "true",
		},
	}
	// no-new-privileges blocks the script (or anything it spawns) from
	// gaining capabilities via setuid binaries inside the runner image.
	// CapDrop ALL removes the default capability set Docker grants to root
	// in a container (NET_RAW, NET_BIND_SERVICE, SETUID, etc.) — a hook
	// script decrypting secrets or generating config has no need for any
	// of them, and dropping them takes the most-common privilege-escalation
	// primitives off the table even if a malicious script gets in.
	hostConfig := &containertypes.HostConfig{
		Mounts:      buildLifecycleMountsInternal(workspaceMount, extraMounts),
		NetworkMode: resolveLifecycleNetworkModeInternal(networkMode),
		SecurityOpt: []string{"no-new-privileges:true"},
		CapDrop:     []string{"ALL"},
		AutoRemove:  false,
	}

	apiTimeoutSec := s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt()
	createCtx, createCancel := timeouts.WithTimeout(ctx, apiTimeoutSec, timeouts.DefaultDockerAPI)
	defer createCancel()
	resp, err := dockerClient.ContainerCreate(createCtx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("create lifecycle container: %w", err)
	}
	containerID := resp.ID
	defer removeLifecycleContainerInternal(ctx, dockerClient, containerID, apiTimeoutSec)

	startCtx, startCancel := timeouts.WithTimeout(ctx, apiTimeoutSec, timeouts.DefaultDockerAPI)
	defer startCancel()
	if _, err := dockerClient.ContainerStart(startCtx, containerID, client.ContainerStartOptions{}); err != nil {
		return "", "", 0, fmt.Errorf("start lifecycle container: %w", err)
	}

	logsCtx, logsCancel := context.WithCancel(ctx)
	defer logsCancel()
	logs, err := dockerClient.ContainerLogs(logsCtx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("stream lifecycle container logs: %w", err)
	}

	stdoutBuf := buildapi.NewLogCapture(lifecycleMaxOutputBytes)
	stderrBuf := buildapi.NewLogCapture(lifecycleMaxOutputBytes)
	logDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(stdoutBuf, stderrBuf, logs)
		logDone <- copyErr
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
	defer waitCancel()
	waitResp := dockerClient.ContainerWait(waitCtx, containerID, client.ContainerWaitOptions{
		Condition: containertypes.WaitConditionNotRunning,
	})

	select {
	case result := <-waitResp.Result:
		exitCode = result.StatusCode
		if result.Error != nil && result.Error.Message != "" {
			drainLifecycleLogsInternal(ctx, logsCancel, logs, logDone)
			return markTruncatedInternal(stdoutBuf), markTruncatedInternal(stderrBuf), exitCode, fmt.Errorf("lifecycle container reported error: %s", result.Error.Message)
		}
	case waitErr := <-waitResp.Error:
		drainLifecycleLogsInternal(ctx, logsCancel, logs, logDone)
		if errors.Is(waitErr, context.DeadlineExceeded) || errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return markTruncatedInternal(stdoutBuf), markTruncatedInternal(stderrBuf), 0, fmt.Errorf("pre-deploy script timed out after %s: %w", timeout, context.DeadlineExceeded)
		}
		if errors.Is(waitErr, context.Canceled) {
			return markTruncatedInternal(stdoutBuf), markTruncatedInternal(stderrBuf), 0, fmt.Errorf("pre-deploy script cancelled: %w", waitErr)
		}
		return markTruncatedInternal(stdoutBuf), markTruncatedInternal(stderrBuf), 0, fmt.Errorf("lifecycle container wait failed: %w", waitErr)
	}

	drainLifecycleLogsInternal(ctx, logsCancel, logs, logDone)

	return markTruncatedInternal(stdoutBuf), markTruncatedInternal(stderrBuf), exitCode, nil
}

// ensureRunnerImageInternal makes sure the runner image is available locally,
// pulling it on a miss. Mirrors the pattern used by VulnerabilityService for
// the Trivy scanner image, including reuse of the dockerImagePullTimeout
// setting so operators on slow networks can tune it once.
func (s *LifecycleService) ensureRunnerImageInternal(ctx context.Context, dockerClient *client.Client, image string) error {
	if _, err := dockerClient.ImageInspect(ctx, image); err == nil {
		return nil
	}

	pullTimeoutSec := s.settingsService.GetSettingsConfig().DockerImagePullTimeout.AsInt()
	pullCtx, pullCancel := timeouts.WithTimeout(ctx, pullTimeoutSec, timeouts.DefaultDockerImagePull)
	defer pullCancel()

	pullReader, err := dockerClient.ImagePull(pullCtx, image, client.ImagePullOptions{})
	if err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("runner image pull timed out for %s (increase dockerImagePullTimeout setting if needed)", image)
		}
		return fmt.Errorf("pull runner image %s: %w", image, err)
	}
	defer func() { _ = pullReader.Close() }()

	if err := dockerutils.ConsumeJSONMessageStream(pullReader, nil); err != nil {
		return fmt.Errorf("failed to complete runner image pull: %w", err)
	}
	return nil
}

func (s *LifecycleService) resolveTimeoutInternal(ctx context.Context, perSyncTimeoutSec int) time.Duration {
	timeoutSec := perSyncTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = lifecycleDefaultTimeoutSec
	}
	maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec", lifecycleDefaultMaxTimeoutSec)
	if maxTimeoutSec > 0 && timeoutSec > maxTimeoutSec {
		timeoutSec = maxTimeoutSec
	}
	return time.Duration(timeoutSec) * time.Second
}

func (s *LifecycleService) persistLastRunInternal(ctx context.Context, syncID, status, output string, runAt time.Time) {
	persistCtx := context.WithoutCancel(ctx)
	err := s.db.WithContext(persistCtx).
		Model(&models.GitOpsSync{}).
		Where("id = ?", syncID).
		Updates(map[string]any{
			"pre_deploy_last_run_at":     runAt,
			"pre_deploy_last_run_status": status,
			"pre_deploy_last_run_output": output,
		}).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to persist lifecycle last-run state", "syncID", syncID, "error", err)
	}
}

func (s *LifecycleService) emitLifecycleEventInternal(
	ctx context.Context,
	project *models.Project,
	sync *models.GitOpsSync,
	runnerImage string,
	status string,
	exitCode int64,
	durationMs int64,
	runErr error,
	actor models.User,
) {
	severity := models.EventSeveritySuccess
	title := "Pre-deploy lifecycle hook succeeded: " + project.Name
	description := fmt.Sprintf("Script %s exited with code %d in %dms", utils.DerefString(sync.PreDeployScriptPath), exitCode, durationMs)
	if status != lifecycleStatusSuccess {
		severity = models.EventSeverityWarning
		title = fmt.Sprintf("Pre-deploy lifecycle hook %s: %s", status, project.Name)
		if runErr != nil {
			description = runErr.Error()
		}
	}

	metadata := models.JSON{
		"scriptPath":   utils.DerefString(sync.PreDeployScriptPath),
		"runnerImage":  runnerImage,
		"exitCode":     exitCode,
		"durationMs":   durationMs,
		"gitopsSyncId": sync.ID,
		"status":       status,
	}

	_, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeLifecycleExecute,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("project"),
		ResourceID:    new(project.ID),
		ResourceName:  new(project.Name),
		EnvironmentID: new(sync.EnvironmentID),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		Metadata:      metadata,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to emit lifecycle.execute event", "syncID", sync.ID, "error", err)
	}
}

// loadGitOpsSyncForProjectInternal returns the GitOps sync linked to the
// given project, or nil if the project is not GitOps-managed. A nil sync
// with nil error is a normal outcome, not a failure.
func (s *LifecycleService) loadGitOpsSyncForProjectInternal(ctx context.Context, projectID string) (*models.GitOpsSync, error) {
	if projectID == "" {
		return nil, nil
	}

	var sync models.GitOpsSync
	err := s.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		First(&sync).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sync, nil
}

// validateScriptPathInternal rejects paths that escape the project directory
// or refer to symlinks/binaries. Reuses the same safety helper that gates
// the existing project include-file editor.
func validateScriptPathInternal(projectPath, scriptPath string) error {
	if scriptPath == "" {
		return errors.New("script path is empty")
	}
	// scriptPath is a POSIX repo path, not a host path; use path.IsAbs so the
	// check behaves the same on Windows-based contributor machines.
	if path.IsAbs(filepath.ToSlash(scriptPath)) {
		return fmt.Errorf("script path %q must be relative to the project directory", scriptPath)
	}

	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}
	absScript, err := filepath.Abs(filepath.Join(absProject, scriptPath))
	if err != nil {
		return fmt.Errorf("resolve script path: %w", err)
	}
	if !projects.IsSafeSubdirectory(absProject, absScript) {
		return fmt.Errorf("script path %q escapes project directory", scriptPath)
	}

	info, err := os.Lstat(absScript)
	if err != nil {
		return fmt.Errorf("stat script %q: %w", scriptPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("script path %q is a symlink; symlinks are not allowed", scriptPath)
	}
	if info.IsDir() {
		return fmt.Errorf("script path %q refers to a directory", scriptPath)
	}
	return nil
}

// parseLifecycleEnvTextInternal reads admin-configured env config as the
// same KEY=VALUE text format used by .env files: one entry per line, blank
// and "#"-prefixed lines ignored, keys must match POSIX identifier syntax.
// Reuses the strict parser also used for stdout-capture.
func parseLifecycleEnvTextInternal(raw *string) (map[string]string, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return map[string]string{}, nil
	}
	env, err := parseKeyValueEnvInternal(*raw)
	if err != nil {
		return nil, fmt.Errorf("invalid env entry: %w", err)
	}
	return env, nil
}

// parseLifecycleExtraMountsTextInternal reads admin-configured bind mounts
// in docker-CLI "src:tgt[:ro|:rw]" form, one per line. Blank and
// "#"-prefixed lines are ignored. Both source and target must be absolute
// paths; mode defaults to read-write.
func parseLifecycleExtraMountsTextInternal(raw *string) ([]lifecycleExtraMount, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}

	var mounts []lifecycleExtraMount
	scanner := bufio.NewScanner(strings.NewReader(*raw))
	scanner.Buffer(make([]byte, 0, 4*1024), 64*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("line %d: expected src:tgt[:ro|:rw], got %q", lineNum, line)
		}

		mount := lifecycleExtraMount{Source: parts[0], Target: parts[1]}
		if len(parts) == 3 {
			switch parts[2] {
			case "ro":
				mount.Readonly = true
			case "rw":
				mount.Readonly = false
			default:
				return nil, fmt.Errorf("line %d: invalid mode %q (expected \"ro\" or \"rw\")", lineNum, parts[2])
			}
		}

		// Mount sources/targets are interpreted by the Docker daemon as POSIX
		// host/container paths, so use path.IsAbs to avoid host-OS quirks
		// (filepath.IsAbs("/x") returns false on Windows).
		if !path.IsAbs(filepath.ToSlash(mount.Source)) {
			return nil, fmt.Errorf("line %d: source %q must be an absolute path", lineNum, mount.Source)
		}
		if !path.IsAbs(filepath.ToSlash(mount.Target)) {
			return nil, fmt.Errorf("line %d: target %q must be an absolute path", lineNum, mount.Target)
		}

		mounts = append(mounts, mount)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read extra mounts config: %w", err)
	}
	return mounts, nil
}

// parseKeyValueEnvInternal parses stdout as a strict newline-separated list of
// KEY=VALUE pairs and returns them as a map. Blank lines and lines starting
// with '#' (after trimming leading whitespace) are ignored. Anything else is
// rejected — we'd rather fail a deploy than silently merge garbage into the
// compose env.
func parseKeyValueEnvInternal(stdout string) (map[string]string, error) {
	env := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), lifecycleMaxOutputBytes)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE, got %q", lineNum, line)
		}
		key := line[:idx]
		value := line[idx+1:]
		if !lifecycleEnvKeyRegex.MatchString(key) {
			return nil, fmt.Errorf("line %d: invalid env key %q", lineNum, key)
		}
		env[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stdout: %w", err)
	}
	return env, nil
}

func envMapToSliceInternal(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func buildLifecycleMountsInternal(workspace *mounttypes.Mount, extras []lifecycleExtraMount) []mounttypes.Mount {
	mounts := make([]mounttypes.Mount, 0, 1+len(extras))
	// The project directory is mounted read-write so scripts can write
	// artifacts the deploy then consumes — e.g. `sops -d secrets.enc.env > .env`.
	// Anything written persists on the host until the next sync overwrites it.
	// The workspace mount itself is built by MountForCurrentContainerSubpath
	// so it carries the right Type (bind or volume) and any required
	// VolumeOptions.Subpath.
	mounts = append(mounts, *workspace)
	for _, m := range extras {
		mounts = append(mounts, mounttypes.Mount{
			Type:     mounttypes.TypeBind,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.Readonly,
		})
	}
	return mounts
}

// resolveLifecycleNetworkModeInternal maps the stored PreDeployNetworkMode
// string to the containertypes.NetworkMode value used by the Docker SDK.
// An empty stored value is treated as "none" so scripts never accidentally
// run on the default bridge network.
func resolveLifecycleNetworkModeInternal(mode string) containertypes.NetworkMode {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		trimmed = "none"
	}
	return containertypes.NetworkMode(trimmed)
}

func lifecycleStatusForResultInternal(exitCode int64, runErr error) string {
	if runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) {
			return lifecycleStatusTimeout
		}
		return lifecycleStatusFailed
	}
	if exitCode != 0 {
		return lifecycleStatusFailed
	}
	return lifecycleStatusSuccess
}

func combineLifecycleOutputInternal(stdout, stderr string) string {
	stdout = strings.TrimRight(stdout, "\n")
	stderr = strings.TrimRight(stderr, "\n")
	switch {
	case stdout == "" && stderr == "":
		return ""
	case stderr == "":
		return stdout
	case stdout == "":
		return "--- stderr ---\n" + stderr
	default:
		return stdout + "\n--- stderr ---\n" + stderr
	}
}

func removeLifecycleContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string, apiTimeoutSec int) {
	if dockerClient == nil || containerID == "" {
		return
	}
	cleanupCtx := context.WithoutCancel(ctx)
	cleanupCtx, cleanupCancel := timeouts.WithTimeout(cleanupCtx, apiTimeoutSec, timeouts.DefaultDockerAPI)
	defer cleanupCancel()
	if _, err := dockerClient.ContainerRemove(cleanupCtx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		slog.WarnContext(cleanupCtx, "failed to remove lifecycle container", "containerID", containerID, "error", err)
	}
}

// drainLifecycleLogsInternal waits for the log copy goroutine to finish with
// a bounded deadline, then force-closes the stream. Mirrors the trivy
// pattern, which exists to tolerate Docker variants that don't EOF cleanly
// when a container exits.
func drainLifecycleLogsInternal(ctx context.Context, logsCancel context.CancelFunc, logs io.ReadCloser, logDone <-chan error) {
	timer := time.NewTimer(lifecycleStreamDrainTimeout)
	defer timer.Stop()
	select {
	case <-logDone:
	case <-timer.C:
		slog.DebugContext(ctx, "lifecycle log stream did not close after container exit; force-closing")
	}
	logsCancel()
	_ = logs.Close()
}

// truncatableCaptureInternal is satisfied by the capped build log capture. It is
// declared locally so the lifecycle service depends on the shared capped-output
// writer's behaviour, not on build-domain types.
type truncatableCaptureInternal interface {
	String() string
	Truncated() bool
}

// markTruncatedInternal returns the captured output, appending a truncation
// marker when the capture hit its byte cap, matching what the GitOps UI shows.
func markTruncatedInternal(c truncatableCaptureInternal) string {
	out := c.String()
	if c.Truncated() {
		out += "\n...<truncated>"
	}
	return out
}
