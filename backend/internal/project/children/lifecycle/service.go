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
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/capture"

	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

// Service runs pre-deploy scripts in throwaway runner containers.
type Service struct {
	dockerService *dockerInternal.DockerClientService
	imageService  *image.ImageService
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

func New(dockerService *dockerInternal.DockerClientService, imageService *image.ImageService) *Service {
	return &Service{
		dockerService: dockerService,
		imageService:  imageService,
	}
}

// markTruncated returns the captured output with a marker when the cap was hit, matching the GitOps UI.
func markTruncated(c *capture.Capture) string {
	out := c.String()
	if c.Truncated() {
		out += "\n...<truncated>"
	}
	return out
}

// drainLifecycleLogs waits a bounded time for the log copy, then force-closes streams that don't EOF on exit.
func drainLifecycleLogs(ctx context.Context, logsCancel context.CancelFunc, logs io.ReadCloser, logDone <-chan error) {
	timer := time.NewTimer(lifecycleStreamDrainTimeout)
	defer timer.Stop()
	select {
	case err := <-logDone:
		if err != nil {
			slog.DebugContext(ctx, "lifecycle log copy ended with error", "error", err)
		}
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

// ParseKeyValueEnv parses stdout as strict KEY=VALUE lines, skipping blanks and '#' comments.
// Anything else fails the deploy rather than polluting the env.
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
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE, got %q", lineNum, line)
		}
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
	// Pull a missing runner image through the image service so credentials and the pull timeout apply.
	if _, inspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, runnerImage)
	}); inspectErr != nil {
		if pullErr := s.imageService.Pull(ctx, image.PullRequest{
			Ref:      runnerImage,
			Progress: io.Discard,
			User:     actor,
		}); pullErr != nil {
			return "", "", 0, fmt.Errorf("failed to ensure runner image %s: %w", runnerImage, pullErr)
		}
	}

	// MountForSubpath mounts a volume-backed /app/data via VolumeOptions.Subpath (host binds fail on e.g. WSL2).
	// On the host, or when the mount cannot be derived, projectPath is bound as-is.
	inspect, inspectErr := s.dockerService.Call(ctx, libarcane.InspectCurrentArcaneContainer)
	if inspectErr != nil {
		slog.WarnContext(ctx, "failed to derive workspace mount; falling back to bind on project path", "projectPath", projectPath, "error", inspectErr)
	}
	// The workspace is read-write so scripts can write artifacts the deploy consumes.
	mounts := []mount.Mount{*cmp.Or(
		docker.MountForSubpath(kit.FromPtr(inspect).Mounts, projectPath, lifecycleWorkspaceMount),
		&mount.Mount{Type: mount.TypeBind, Source: projectPath, Target: lifecycleWorkspaceMount},
	)}
	for _, m := range extraMounts {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.Readonly,
		})
	}
	var env []string
	for k, v := range hookEnv {
		env = append(env, k+"="+v)
	}

	resp, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
			// Cmd is the script alone so its shebang picks the interpreter; Entrypoint is cleared for images like getsops/sops.
			Config: &container.Config{
				Image:        runnerImage,
				Entrypoint:   []string{},
				Cmd:          []string{filepath.ToSlash(filepath.Join(lifecycleWorkspaceMount, scriptPath))},
				WorkingDir:   lifecycleWorkspaceMount,
				Env:          env,
				AttachStdout: true,
				AttachStderr: true,
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
			},
			// no-new-privileges and CapDrop ALL strip setuid and root capabilities from the script and its children.
			HostConfig: &container.HostConfig{
				Mounts:      mounts,
				NetworkMode: container.NetworkMode(cmp.Or(strings.TrimSpace(networkMode), "none")),
				SecurityOpt: []string{"no-new-privileges:true"},
				CapDrop:     []string{"ALL"},
			},
		})
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("create lifecycle container: %w", err)
	}
	defer func() {
		// Cleanup outlives cancellation so a cancelled run never leaks its container.
		cleanupCtx := context.WithoutCancel(ctx)
		if _, removeErr := s.dockerService.Call(cleanupCtx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
		}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			slog.WarnContext(cleanupCtx, "failed to remove lifecycle container", "containerId", resp.ID, "error", removeErr)
		}
	}()

	if _, containerStartErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	}); containerStartErr != nil {
		return "", "", 0, fmt.Errorf("start lifecycle container: %w", containerStartErr)
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	logsCtx, logsCancel := context.WithCancel(ctx)
	defer logsCancel()
	logs, err := dockerClient.ContainerLogs(logsCtx, resp.ID, client.ContainerLogsOptions{
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
	waitResp := dockerClient.ContainerWait(waitCtx, resp.ID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})

	select {
	case result := <-waitResp.Result:
		exitCode = result.StatusCode
		if kit.FromPtr(result.Error).Message != "" {
			drainLifecycleLogs(ctx, logsCancel, logs, logDone)
			return markTruncated(stdoutBuf), markTruncated(stderrBuf), exitCode, fmt.Errorf("lifecycle container reported error: %s", result.Error.Message)
		}
	case waitErr := <-waitResp.Error:
		if waitErr != nil {
			if _, killErr := s.dockerService.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.ContainerKillResult, error) {
				return dockerClient.ContainerKill(ctx, resp.ID, client.ContainerKillOptions{})
			}); killErr != nil && !errdefs.IsNotFound(killErr) {
				slog.WarnContext(ctx, "failed to kill lifecycle container", "containerId", resp.ID, "error", killErr)
			}
		}
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

// ValidateScriptPath rejects paths that escape the project directory or refer to symlinks or directories.
func ValidateScriptPath(ctx context.Context, projectPath, scriptPath string) error {
	if scriptPath == "" {
		return errors.New("script path is empty")
	}
	// scriptPath is a POSIX repo path; path.IsAbs behaves the same on Windows.
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
			// The runner image's user may read a path Arcane's uid cannot traverse (#3373); let the run surface real problems.
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
