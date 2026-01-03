package arcaneupdater

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const (
	DefaultPreUpdateTimeout  = 60 * time.Second
	DefaultPostUpdateTimeout = 60 * time.Second
)

// LifecycleHookResult contains the result of executing a lifecycle hook
type LifecycleHookResult struct {
	Executed   bool
	SkipUpdate bool // True if exit code was ExitCodeSkipUpdate (75)
	ExitCode   int
	Output     string
	Error      error
}

// ExecutePreUpdateCommand runs the pre-update lifecycle hook on a container
// Returns SkipUpdate=true if the command exits with code 75 (EX_TEMPFAIL)
func ExecutePreUpdateCommand(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPreUpdate)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	timeout := getTimeout(labels, LabelPreUpdateTimeout, DefaultPreUpdateTimeout)

	slog.DebugContext(ctx, "ExecutePreUpdateCommand: running pre-update hook",
		"containerID", containerID,
		"command", cmd,
		"timeout", timeout)

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, timeout)
}

// ExecutePostUpdateCommand runs the post-update lifecycle hook on a container
func ExecutePostUpdateCommand(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPostUpdate)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	timeout := getTimeout(labels, LabelPostUpdateTimeout, DefaultPostUpdateTimeout)

	slog.DebugContext(ctx, "ExecutePostUpdateCommand: running post-update hook",
		"containerID", containerID,
		"command", cmd,
		"timeout", timeout)

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, timeout)
}

// ExecutePreCheckCommand runs the pre-check lifecycle hook on a container
func ExecutePreCheckCommand(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPreCheck)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, DefaultPreUpdateTimeout)
}

// ExecutePostCheckCommand runs the post-check lifecycle hook on a container
func ExecutePostCheckCommand(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPostCheck)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, DefaultPostUpdateTimeout)
}

func executeLifecycleCommand(ctx context.Context, dcli *client.Client, containerID string, cmd []string, timeout time.Duration) LifecycleHookResult {
	result := LifecycleHookResult{Executed: true}

	// Create exec configuration
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	// Create the exec instance
	execResp, err := dcli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		result.Error = fmt.Errorf("failed to create exec: %w", err)
		slog.WarnContext(ctx, "executeLifecycleCommand: failed to create exec",
			"containerID", containerID,
			"error", err)
		return result
	}

	// Attach to the exec instance to get output
	attachResp, err := dcli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		result.Error = fmt.Errorf("failed to attach to exec: %w", err)
		slog.WarnContext(ctx, "executeLifecycleCommand: failed to attach to exec",
			"containerID", containerID,
			"error", err)
		return result
	}
	defer attachResp.Close()

	// Read output with timeout
	outputChan := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go func() {
		var buf bytes.Buffer
		_, err := buf.ReadFrom(attachResp.Reader)
		if err != nil {
			errChan <- err
			return
		}
		outputChan <- buf.Bytes()
	}()

	// Wait for output or timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		result.Error = fmt.Errorf("lifecycle command timed out after %v", timeout)
		slog.WarnContext(ctx, "executeLifecycleCommand: command timed out",
			"containerID", containerID,
			"timeout", timeout)
		return result
	case err := <-errChan:
		result.Error = fmt.Errorf("error reading output: %w", err)
		return result
	case output := <-outputChan:
		result.Output = string(output)
	}

	// Inspect the exec to get exit code
	execInspect, err := dcli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		result.Error = fmt.Errorf("failed to inspect exec: %w", err)
		slog.WarnContext(ctx, "executeLifecycleCommand: failed to inspect exec",
			"containerID", containerID,
			"error", err)
		return result
	}

	result.ExitCode = execInspect.ExitCode

	// Check for skip update signal (exit code 75 = EX_TEMPFAIL)
	if result.ExitCode == ExitCodeSkipUpdate {
		result.SkipUpdate = true
		slog.InfoContext(ctx, "executeLifecycleCommand: container requested skip update",
			"containerID", containerID,
			"exitCode", result.ExitCode)
		return result
	}

	// Non-zero exit code (other than 75) is an error
	if result.ExitCode != 0 {
		result.Error = fmt.Errorf("lifecycle command exited with code %d", result.ExitCode)
		slog.WarnContext(ctx, "executeLifecycleCommand: command failed",
			"containerID", containerID,
			"exitCode", result.ExitCode,
			"output", result.Output)
		return result
	}

	slog.DebugContext(ctx, "executeLifecycleCommand: command completed successfully",
		"containerID", containerID,
		"exitCode", result.ExitCode)

	return result
}

func getTimeout(labels map[string]string, timeoutLabel string, defaultTimeout time.Duration) time.Duration {
	if labels == nil {
		return defaultTimeout
	}

	for k, v := range labels {
		if k == timeoutLabel {
			v = strings.TrimSpace(v)
			if v == "" {
				return defaultTimeout
			}

			// Try parsing as seconds
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}

			// Try parsing as duration string
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}

	return defaultTimeout
}
