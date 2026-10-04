// Package lifecycle runs pre-deploy hook scripts in isolated runner
// containers and captures their output.
package lifecycle

import (
	"bufio"
	"cmp"
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

	"github.com/containerd/errdefs"
	lifecycletype "github.com/getarcaneapp/arcane/types/v2/lifecycle"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/capture"

	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

// Service runs pre-deploy scripts in throwaway runner containers.
type Service struct {
	settingsService *settings.SettingsService
	dockerService   *dockerInternal.DockerClientService
	imageService    *image.ImageService
}

func New(settingsService *settings.SettingsService, dockerService *dockerInternal.DockerClientService, imageService *image.ImageService) *Service {
	return &Service{settingsService: settingsService, dockerService: dockerService, imageService: imageService}
}

// markTruncatedInternal returns the captured output, appending a truncation
// marker when the capture hit its byte cap, matching what the GitOps UI shows.
func MarkTruncated(c truncatableCaptureInternal) string {
	out := c.String()
	if c.Truncated() {
		out += "\n...<truncated>"
	}
	return out
}

// truncatableCaptureInternal is satisfied by the capped build log capture. It is
// declared locally so the lifecycle service depends on the shared capped-output
// writer's behaviour, not on build-domain types.
type truncatableCaptureInternal interface {
	String() string
	Truncated() bool
}

// drainLifecycleLogsInternal waits for the log copy goroutine to finish with
// a bounded deadline, then force-closes the stream. Mirrors the trivy
// pattern, which exists to tolerate Docker variants that don't EOF cleanly
// when a container exits.
func DrainLifecycleLogs(ctx context.Context, logsCancel context.CancelFunc, logs io.ReadCloser, logDone <-chan error) {
	timer := time.NewTimer(LifecycleStreamDrainTimeout)
	defer timer.Stop()
	select {
	case <-logDone:
	case <-timer.C:
		slog.DebugContext(ctx, "lifecycle log stream did not close after container exit; force-closing")
	}
	logsCancel()
	_ = logs.Close()
}

const (
	// Lifecycle hook configuration limits and conventions.
	// lifecycleWorkspaceMount is the in-container path the project dir is
	// bind-mounted to. Scripts run with this as their working dir.
	LifecycleWorkspaceMount = "/workspace"
	// lifecycleMaxOutputBytes caps the stdout/stderr we retain on the
	// GitOpsSync row. The full stream is still consumed; the tail is dropped.
	LifecycleMaxOutputBytes = 16 * 1024
	// lifecycleStreamDrainTimeout bounds how long we wait for the log copy
	// goroutine to drain after the container exits, before force-closing.
	// Mirrors the value used by vulnerability scanning for the same purpose.
	LifecycleStreamDrainTimeout = 30 * time.Second

	// Last-run status values written to GitOpsSync.PreDeployLastRunStatus.
	LifecycleStatusSuccess = "success"
	LifecycleStatusFailed  = "failed"
	LifecycleStatusTimeout = "timeout"
)

func RemoveLifecycleContainer(ctx context.Context, dockerClient *client.Client, containerID string, apiTimeoutSec int) {
	if dockerClient == nil || containerID == "" {
		return
	}
	cleanupCtx := context.WithoutCancel(ctx)
	cleanupCtx, cleanupCancel := context.WithTimeout(cleanupCtx, timeouts.GetDuration(apiTimeoutSec, timeouts.DefaultDockerAPI))
	defer cleanupCancel()
	if _, err := dockerClient.ContainerRemove(cleanupCtx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
		slog.WarnContext(cleanupCtx, "failed to remove lifecycle container", "containerId", containerID, "error", err)
	}
}

func CombineLifecycleOutput(stdout, stderr string) string {
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

func LifecycleStatusForResult(exitCode int64, runErr error) string {
	if runErr != nil {
		return kit.Ternary(errors.Is(runErr, context.DeadlineExceeded), LifecycleStatusTimeout, LifecycleStatusFailed)
	}
	return kit.Ternary(exitCode != 0, LifecycleStatusFailed, LifecycleStatusSuccess)
}

func BuildLifecycleMounts(workspace *mount.Mount, extras []lifecycletype.ExtraMount) []mount.Mount {
	mounts := make([]mount.Mount, 0, 1+len(extras))
	// The project directory is mounted read-write so scripts can write
	// artifacts the deploy then consumes — e.g. `sops -d secrets.enc.env > .env`.
	// Anything written persists on the host until the next sync overwrites it.
	// The workspace mount itself is built by MountForSubpath
	// so it carries the right Type (bind or volume) and any required
	// VolumeOptions.Subpath.
	mounts = append(mounts, *workspace)
	for _, m := range extras {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.Readonly,
		})
	}
	return mounts
}

func EnvMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// parseKeyValueEnvInternal parses stdout as a strict newline-separated list of
// KEY=VALUE pairs and returns them as a map. Blank lines and lines starting
// with '#' (after trimming leading whitespace) are ignored. Anything else is
// rejected — we'd rather fail a deploy than silently merge garbage into the
// compose env.
func ParseKeyValueEnv(stdout string) (map[string]string, error) {
	env := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), LifecycleMaxOutputBytes)

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
		if !LifecycleEnvKeyRegex.MatchString(key) {
			return nil, fmt.Errorf("line %d: invalid env key %q", lineNum, key)
		}
		env[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stdout: %w", err)
	}
	return env, nil
}

// lifecycleEnvKeyRegex enforces POSIX-style identifier syntax for env keys
// both in admin-configured Env and in scripts' KEY=VALUE stdout capture.
var LifecycleEnvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// RunnerImage prefers the sync's configured runner image over the
// lifecycleDefaultRunnerImage setting.
func (s *Service) RunnerImage(ctx context.Context, configured *string) string {
	if runnerImage := strings.TrimSpace(mo.PointerToOption(configured).OrEmpty()); runnerImage != "" {
		return runnerImage
	}
	return strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage", "alpine:latest"))
}

// RunScript performs the docker run + log capture + wait.
// On the happy path it returns stdout, stderr, the script's exit code and a
// nil error. context.DeadlineExceeded from the per-run timeout is wrapped and
// returned as the error so callers can distinguish a timeout from a non-zero
// exit.
func (s *Service) RunScript(
	ctx context.Context,
	runnerImage string,
	projectPath string,
	scriptPath string,
	hookEnv map[string]string,
	extraMounts []lifecycletype.ExtraMount,
	networkMode string,
	timeout time.Duration,
	actor user.Actor,
) (stdoutContent, stderrContent string, exitCode int64, err error) {
	dockerClient, dErr := s.dockerService.GetClient(ctx)
	if dErr != nil {
		return "", "", 0, fmt.Errorf("failed to connect to Docker: %w", dErr)
	}

	if ensureRunnerImageErr := s.ensureRunnerImageInternal(ctx, dockerClient, runnerImage, actor); ensureRunnerImageErr != nil {
		return "", "", 0, fmt.Errorf("failed to ensure runner image %s: %w", runnerImage, ensureRunnerImageErr)
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
	var workspaceMount *mount.Mount
	if inspect, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient); inspectErr != nil {
		slog.WarnContext(ctx, "failed to derive workspace mount; falling back to bind on project path", "projectPath", projectPath, "error", inspectErr)
	} else {
		workspaceMount = docker.MountForSubpath(inspect.Mounts, projectPath, LifecycleWorkspaceMount)
	}
	if workspaceMount == nil {
		workspaceMount = &mount.Mount{Type: mount.TypeBind, Source: projectPath, Target: LifecycleWorkspaceMount}
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
	config := &container.Config{
		Image:        runnerImage,
		Entrypoint:   []string{},
		Cmd:          []string{filepath.ToSlash(filepath.Join(LifecycleWorkspaceMount, scriptPath))},
		WorkingDir:   LifecycleWorkspaceMount,
		Env:          EnvMapToSlice(hookEnv),
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
	hostConfig := &container.HostConfig{
		Mounts:      BuildLifecycleMounts(workspaceMount, extraMounts),
		NetworkMode: container.NetworkMode(cmp.Or(strings.TrimSpace(networkMode), "none")),
		SecurityOpt: []string{"no-new-privileges:true"},
		CapDrop:     []string{"ALL"},
		AutoRemove:  false,
	}

	apiTimeoutSec := s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt()
	createCtx, createCancel := context.WithTimeout(ctx, timeouts.GetDuration(apiTimeoutSec, timeouts.DefaultDockerAPI))
	defer createCancel()
	resp, err := dockerClient.ContainerCreate(createCtx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("create lifecycle container: %w", err)
	}
	containerID := resp.ID
	defer RemoveLifecycleContainer(ctx, dockerClient, containerID, apiTimeoutSec)

	startCtx, startCancel := context.WithTimeout(ctx, timeouts.GetDuration(apiTimeoutSec, timeouts.DefaultDockerAPI))
	defer startCancel()
	if _, containerStartErr := dockerClient.ContainerStart(startCtx, containerID, client.ContainerStartOptions{}); containerStartErr != nil {
		return "", "", 0, fmt.Errorf("start lifecycle container: %w", containerStartErr)
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

	stdoutBuf := capture.New(LifecycleMaxOutputBytes)
	stderrBuf := capture.New(LifecycleMaxOutputBytes)
	logDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(stdoutBuf, stderrBuf, logs)
		logDone <- copyErr
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
	defer waitCancel()
	waitResp := dockerClient.ContainerWait(waitCtx, containerID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})

	select {
	case result := <-waitResp.Result:
		exitCode = result.StatusCode
		if result.Error != nil && result.Error.Message != "" {
			DrainLifecycleLogs(ctx, logsCancel, logs, logDone)
			return MarkTruncated(stdoutBuf), MarkTruncated(stderrBuf), exitCode, fmt.Errorf("lifecycle container reported error: %s", result.Error.Message)
		}
	case waitErr := <-waitResp.Error:
		DrainLifecycleLogs(ctx, logsCancel, logs, logDone)
		if errors.Is(waitErr, context.DeadlineExceeded) || errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return MarkTruncated(stdoutBuf), MarkTruncated(stderrBuf), 0, fmt.Errorf("pre-deploy script timed out after %s: %w", timeout, context.DeadlineExceeded)
		}
		if errors.Is(waitErr, context.Canceled) {
			return MarkTruncated(stdoutBuf), MarkTruncated(stderrBuf), 0, fmt.Errorf("pre-deploy script cancelled: %w", waitErr)
		}
		if waitErr != nil {
			return MarkTruncated(stdoutBuf), MarkTruncated(stderrBuf), 0, fmt.Errorf("lifecycle container wait failed: %w", waitErr)
		}
		return MarkTruncated(stdoutBuf), MarkTruncated(stderrBuf), 0, nil
	}

	DrainLifecycleLogs(ctx, logsCancel, logs, logDone)

	return MarkTruncated(stdoutBuf), MarkTruncated(stderrBuf), exitCode, nil
}

// ensureRunnerImageInternal makes sure the runner image is available locally,
// pulling it on a miss. The pull is delegated to the image service so the
// configured registry credentials (and its anonymous retry) apply, bounded by
// the dockerImagePullTimeout setting so operators on slow networks can tune it.
func (s *Service) ensureRunnerImageInternal(ctx context.Context, dockerClient *client.Client, imageName string, actor user.Actor) error {
	if _, err := dockerClient.ImageInspect(ctx, imageName); err == nil {
		return nil
	}
	if s.imageService == nil {
		return errors.New("image service is unavailable")
	}

	pullTimeoutSec := s.settingsService.GetSettingsConfig().DockerImagePullTimeout.AsInt()
	pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(pullTimeoutSec, timeouts.DefaultDockerImagePull))
	defer pullCancel()

	if err := s.imageService.PullImage(pullCtx, imageName, io.Discard, actor, nil); err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("runner image pull timed out for %s (increase dockerImagePullTimeout setting if needed)", imageName)
		}
		return fmt.Errorf("pull runner image %s: %w", imageName, err)
	}
	return nil
}

// Timeout caps the per-sync timeout at the lifecycleMaxTimeoutSec setting.
func (s *Service) Timeout(ctx context.Context, perSyncTimeoutSec int) time.Duration {
	timeoutSec := perSyncTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = lifecycletype.DefaultTimeoutSec
	}
	maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec", lifecycletype.DefaultMaxTimeoutSec)
	if maxTimeoutSec > 0 && timeoutSec > maxTimeoutSec {
		timeoutSec = maxTimeoutSec
	}
	return time.Duration(timeoutSec) * time.Second
}

// ValidateScriptPath rejects paths that escape the project directory
// or refer to symlinks/binaries. Reuses the same safety helper that gates
// the existing project include-file editor.
func ValidateScriptPath(ctx context.Context, projectPath, scriptPath string) error {
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

	entry, err := acfs.Stat(ctx, absProject, "/"+filepath.ToSlash(scriptPath), false)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			// The stat runs as Arcane's own uid, but the script executes in the
			// runner container as that image's user — which may well be able to
			// read a path Arcane cannot traverse (#3373). Proceed and let the
			// container run surface any real problem.
			slog.WarnContext(ctx, "cannot inspect pre-deploy script as the Arcane user; proceeding, the runner container may still be able to read it",
				"scriptPath", scriptPath,
				"detail", describeLifecyclePathAccessInternal(projectPath, absScript),
			)
			return nil
		}
		return fmt.Errorf("stat script %q: %w", scriptPath, err)
	}
	if entry.IsSymlink {
		return fmt.Errorf("script path %q is a symlink; symlinks are not allowed", scriptPath)
	}
	if entry.IsDirectory {
		return fmt.Errorf("script path %q refers to a directory", scriptPath)
	}
	return nil
}
