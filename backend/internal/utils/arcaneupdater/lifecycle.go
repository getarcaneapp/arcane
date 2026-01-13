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

// LifecycleHookSecurityContext contains security context for lifecycle hook execution.
// This is used to determine whether lifecycle hooks should be allowed to run.
type LifecycleHookSecurityContext struct {
	// HooksEnabled indicates whether lifecycle hooks are globally enabled in settings.
	HooksEnabled bool
	// ProjectCreatedByAdmin indicates whether the project was created by an admin user.
	// Hooks are only executed for projects created by admins.
	ProjectCreatedByAdmin bool
}

// IsAuthorized returns true if lifecycle hooks are authorized to run.
// Hooks require both:
// 1. Global setting lifecycleHooksEnabled = true
// 2. Project was created by an admin user
func (s *LifecycleHookSecurityContext) IsAuthorized() bool {
	if s == nil {
		return false
	}
	return s.HooksEnabled && s.ProjectCreatedByAdmin
}

// LifecycleHookResult contains the result of executing a lifecycle hook
type LifecycleHookResult struct {
	Executed       bool
	SkipUpdate     bool // True if exit code was ExitCodeSkipUpdate (75)
	ExitCode       int
	Output         string
	Error          error
	SecurityDenied bool // True if hook was blocked due to security policy
}

// ExecutePreUpdateCommand runs the pre-update lifecycle hook on a container
// Returns SkipUpdate=true if the command exits with code 75 (EX_TEMPFAIL)
// DEPRECATED: Use ExecutePreUpdateCommandSecure for security-checked execution.
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

// ExecutePreUpdateCommandSecure runs the pre-update lifecycle hook with security checks.
// Returns SecurityDenied=true if the hook was blocked due to security policy.
func ExecutePreUpdateCommandSecure(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string, security *LifecycleHookSecurityContext) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPreUpdate)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	// Security check: verify hooks are authorized
	if !security.IsAuthorized() {
		reason := "lifecycle hooks disabled"
		if security != nil && security.HooksEnabled && !security.ProjectCreatedByAdmin {
			reason = "project not created by admin"
		}
		slog.WarnContext(ctx, "ExecutePreUpdateCommandSecure: hook blocked by security policy",
			"containerID", containerID,
			"reason", reason,
			"hooksEnabled", security != nil && security.HooksEnabled,
			"projectCreatedByAdmin", security != nil && security.ProjectCreatedByAdmin)
		return LifecycleHookResult{Executed: false, SecurityDenied: true, Error: fmt.Errorf("lifecycle hook blocked: %s", reason)}
	}

	timeout := getTimeout(labels, LabelPreUpdateTimeout, DefaultPreUpdateTimeout)

	slog.InfoContext(ctx, "ExecutePreUpdateCommandSecure: running pre-update hook (authorized)",
		"containerID", containerID,
		"command", cmd,
		"timeout", timeout)

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, timeout)
}

// ExecutePostUpdateCommand runs the post-update lifecycle hook on a container
// DEPRECATED: Use ExecutePostUpdateCommandSecure for security-checked execution.
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

// ExecutePostUpdateCommandSecure runs the post-update lifecycle hook with security checks.
// Returns SecurityDenied=true if the hook was blocked due to security policy.
func ExecutePostUpdateCommandSecure(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string, security *LifecycleHookSecurityContext) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPostUpdate)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	// Security check: verify hooks are authorized
	if !security.IsAuthorized() {
		reason := "lifecycle hooks disabled"
		if security != nil && security.HooksEnabled && !security.ProjectCreatedByAdmin {
			reason = "project not created by admin"
		}
		slog.WarnContext(ctx, "ExecutePostUpdateCommandSecure: hook blocked by security policy",
			"containerID", containerID,
			"reason", reason)
		return LifecycleHookResult{Executed: false, SecurityDenied: true, Error: fmt.Errorf("lifecycle hook blocked: %s", reason)}
	}

	timeout := getTimeout(labels, LabelPostUpdateTimeout, DefaultPostUpdateTimeout)

	slog.InfoContext(ctx, "ExecutePostUpdateCommandSecure: running post-update hook (authorized)",
		"containerID", containerID,
		"command", cmd,
		"timeout", timeout)

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, timeout)
}

// ExecutePreCheckCommand runs the pre-check lifecycle hook on a container
// DEPRECATED: Use ExecutePreCheckCommandSecure for security-checked execution.
func ExecutePreCheckCommand(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPreCheck)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, DefaultPreUpdateTimeout)
}

// ExecutePreCheckCommandSecure runs the pre-check lifecycle hook with security checks.
// Returns SecurityDenied=true if the hook was blocked due to security policy.
func ExecutePreCheckCommandSecure(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string, security *LifecycleHookSecurityContext) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPreCheck)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	// Security check: verify hooks are authorized
	if !security.IsAuthorized() {
		slog.WarnContext(ctx, "ExecutePreCheckCommandSecure: hook blocked by security policy",
			"containerID", containerID)
		return LifecycleHookResult{Executed: false, SecurityDenied: true, Error: fmt.Errorf("lifecycle hook blocked by security policy")}
	}

	slog.InfoContext(ctx, "ExecutePreCheckCommandSecure: running pre-check hook (authorized)",
		"containerID", containerID,
		"command", cmd)

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, DefaultPreUpdateTimeout)
}

// ExecutePostCheckCommand runs the post-check lifecycle hook on a container
// DEPRECATED: Use ExecutePostCheckCommandSecure for security-checked execution.
func ExecutePostCheckCommand(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPostCheck)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	return executeLifecycleCommand(ctx, dcli, containerID, cmd, DefaultPostUpdateTimeout)
}

// ExecutePostCheckCommandSecure runs the post-check lifecycle hook with security checks.
// Returns SecurityDenied=true if the hook was blocked due to security policy.
func ExecutePostCheckCommandSecure(ctx context.Context, dcli *client.Client, containerID string, labels map[string]string, security *LifecycleHookSecurityContext) LifecycleHookResult {
	cmd := GetLifecycleCommand(labels, LabelPostCheck)
	if len(cmd) == 0 {
		return LifecycleHookResult{Executed: false}
	}

	// Security check: verify hooks are authorized
	if !security.IsAuthorized() {
		slog.WarnContext(ctx, "ExecutePostCheckCommandSecure: hook blocked by security policy",
			"containerID", containerID)
		return LifecycleHookResult{Executed: false, SecurityDenied: true, Error: fmt.Errorf("lifecycle hook blocked by security policy")}
	}

	slog.InfoContext(ctx, "ExecutePostCheckCommandSecure: running post-check hook (authorized)",
		"containerID", containerID,
		"command", cmd)

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
