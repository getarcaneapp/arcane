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
	"go.getarcane.app/acfs"
	"go.getarcane.app/docker"
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

const (
	// lifecycleWorkspaceMount is where the project dir is mounted; scripts run there.
	lifecycleWorkspaceMount = "/workspace"
	// lifecycleMaxOutputBytes caps retained stdout/stderr; the full stream is still consumed.
	lifecycleMaxOutputBytes = 16 * 1024
	// lifecycleStreamDrainTimeout bounds the post-exit log drain, matching vulnerability scanning.
	lifecycleStreamDrainTimeout = 30 * time.Second

	// Last-run status values written to GitOpsSync.PreDeployLastRunStatus.
	LifecycleStatusSuccess = "success"
	LifecycleStatusFailed  = "failed"
	LifecycleStatusTimeout = "timeout"
)

// lifecycleEnvKeyRegex enforces POSIX-style env keys in configured Env and script stdout.
var lifecycleEnvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func New(settingsService *settings.SettingsService, dockerService *dockerInternal.DockerClientService, imageService *image.ImageService) *Service {
	return &Service{settingsService: settingsService, dockerService: dockerService, imageService: imageService}
}

// markTruncated returns the captured output with a marker when the cap was hit,
// matching what the GitOps UI shows.
func markTruncated(c *capture.Capture) string {
	out := c.String()
	if c.Truncated() {
		out += "\n...<truncated>"
	}
	return out
}

// drainLifecycleLogs waits a bounded time for the log copy to finish, then
// force-closes the stream for Docker variants that don't EOF on exit.
func drainLifecycleLogs(ctx context.Context, logsCancel context.CancelFunc, logs io.ReadCloser, logDone <-chan error) {
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

// ParseKeyValueEnv parses stdout as strict KEY=VALUE lines, skipping blanks and
// '#' comments; anything else fails the deploy rather than polluting the env.
func ParseKeyValueEnv(stdout string) (map[string]string, error) {
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

// RunScript runs the script in a runner container and returns its output and exit code.
// A per-run timeout is returned wrapping context.DeadlineExceeded, distinct from a non-zero exit.
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

	// Pull a missing runner image through the image service so registry
	// credentials and the dockerImagePullTimeout setting apply.
	if _, inspectErr := dockerClient.ImageInspect(ctx, runnerImage); inspectErr != nil {
		if s.imageService == nil {
			return "", "", 0, fmt.Errorf("failed to ensure runner image %s: image service is unavailable", runnerImage)
		}
		pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(s.settingsService.GetSettingsConfig().DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
		pullErr := s.imageService.PullImage(pullCtx, runnerImage, io.Discard, actor, nil)
		pullTimedOut := errors.Is(pullCtx.Err(), context.DeadlineExceeded)
		pullCancel()
		if pullErr != nil && pullTimedOut {
			return "", "", 0, fmt.Errorf("failed to ensure runner image %s: runner image pull timed out for %s (increase dockerImagePullTimeout setting if needed)", runnerImage, runnerImage)
		}
		if pullErr != nil {
			return "", "", 0, fmt.Errorf("failed to ensure runner image %s: pull runner image %s: %w", runnerImage, runnerImage, pullErr)
		}
	}

	// MountForSubpath mounts a volume-backed /app/data via VolumeOptions.Subpath
	// (translated host binds fail on e.g. WSL2); on the host, bind projectPath as-is.
	var workspaceMount *mount.Mount
	if inspect, inspectErr := libarcane.InspectCurrentArcaneContainer(ctx, dockerClient); inspectErr != nil {
		slog.WarnContext(ctx, "failed to derive workspace mount; falling back to bind on project path", "projectPath", projectPath, "error", inspectErr)
	} else {
		workspaceMount = docker.MountForSubpath(inspect.Mounts, projectPath, lifecycleWorkspaceMount)
	}
	if workspaceMount == nil {
		workspaceMount = &mount.Mount{Type: mount.TypeBind, Source: projectPath, Target: lifecycleWorkspaceMount}
	}
	// The workspace is read-write so scripts can write artifacts the deploy consumes.
	mounts := make([]mount.Mount, 0, 1+len(extraMounts))
	mounts = append(mounts, *workspaceMount)
	for _, m := range extraMounts {
		mounts = append(mounts, mount.Mount{Type: mount.TypeBind, Source: m.Source, Target: m.Target, ReadOnly: m.Readonly})
	}
	var env []string
	for k, v := range hookEnv {
		env = append(env, k+"="+v)
	}

	// Cmd is the executable script alone so its shebang picks the interpreter;
	// Entrypoint is cleared so images like getsops/sops don't treat it as an argument.
	config := &container.Config{
		Image:        runnerImage,
		Entrypoint:   []string{},
		Cmd:          []string{filepath.ToSlash(filepath.Join(lifecycleWorkspaceMount, scriptPath))},
		WorkingDir:   lifecycleWorkspaceMount,
		Env:          env,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Labels: map[string]string{
			libarcane.InternalResourceLabel: "true",
		},
	}
	// no-new-privileges and CapDrop ALL take setuid and default root
	// capabilities away from the script and anything it spawns.
	hostConfig := &container.HostConfig{
		Mounts:      mounts,
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
	defer func() {
		// Cleanup outlives cancellation so a cancelled run never leaks its container.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.GetDuration(apiTimeoutSec, timeouts.DefaultDockerAPI))
		defer cleanupCancel()
		if _, removeErr := dockerClient.ContainerRemove(cleanupCtx, containerID, client.ContainerRemoveOptions{Force: true}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			slog.WarnContext(cleanupCtx, "failed to remove lifecycle container", "containerId", containerID, "error", removeErr)
		}
	}()

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

	stdoutBuf := capture.New(lifecycleMaxOutputBytes)
	stderrBuf := capture.New(lifecycleMaxOutputBytes)
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
			drainLifecycleLogs(ctx, logsCancel, logs, logDone)
			return markTruncated(stdoutBuf), markTruncated(stderrBuf), exitCode, fmt.Errorf("lifecycle container reported error: %s", result.Error.Message)
		}
	case waitErr := <-waitResp.Error:
		drainLifecycleLogs(ctx, logsCancel, logs, logDone)
		if errors.Is(waitErr, context.DeadlineExceeded) || errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return markTruncated(stdoutBuf), markTruncated(stderrBuf), 0, fmt.Errorf("pre-deploy script timed out after %s: %w", timeout, context.DeadlineExceeded)
		}
		if errors.Is(waitErr, context.Canceled) {
			return markTruncated(stdoutBuf), markTruncated(stderrBuf), 0, fmt.Errorf("pre-deploy script cancelled: %w", waitErr)
		}
		if waitErr != nil {
			return markTruncated(stdoutBuf), markTruncated(stderrBuf), 0, fmt.Errorf("lifecycle container wait failed: %w", waitErr)
		}
		return markTruncated(stdoutBuf), markTruncated(stderrBuf), 0, nil
	}

	drainLifecycleLogs(ctx, logsCancel, logs, logDone)

	return markTruncated(stdoutBuf), markTruncated(stderrBuf), exitCode, nil
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
				"detail", describeLifecyclePathAccess(projectPath, absScript),
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
