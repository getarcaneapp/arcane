// Package hostshell builds the privileged throwaway-container configuration
// used to reach the real Docker host from inside Arcane (or an Arcane
// agent), via a helper container run with --privileged --pid=host and an
// nsenter into PID 1's namespaces.
//
// This is deliberately a separate label namespace and host-config shape from
// pkg/libarcane/volumehelper: the volume-helper reaper
// (VolumeService.CleanupOrphanedVolumeHelpers) force-removes any container
// carrying volumehelper.ContainerLabel on every startup sweep, which would
// kill a live host-shell session if the two shared a label.
//
// Every container this package configures is full root on the Docker host.
// Callers must gate use behind an explicit, off-by-default opt-in and must
// never skip Preflight.
package hostshell

import (
	"context"
	"io"
	"regexp"
	"strings"

	"emperror.dev/errors"

	containertypes "github.com/moby/moby/api/types/container"
	systemtypes "github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
)

const (
	// DefaultToolsImage is used when no image override is configured.
	DefaultToolsImage = volumehelper.DefaultToolsImage

	// ContainerLabel marks a helper container spawned by this package. Kept
	// distinct from volumehelper.ContainerLabel on purpose — see package doc.
	ContainerLabel = "com.getarcaneapp.host-shell"
	// SessionLabel carries the caller-chosen session/run id, so orphan
	// cleanup and diagnostics can identify a specific helper.
	SessionLabel = "com.getarcaneapp.host-shell.session"

	// DefaultShell is used by the interactive terminal when the caller does
	// not request a specific shell.
	DefaultShell = "/bin/sh"

	// maxShellLength bounds the ?shell= query value before it is ever
	// inspected; this becomes argv on the host as root.
	maxShellLength = 128
)

// Sentinel errors returned by Preflight and ValidateShell. Callers should
// errors.Is against these to render a specific, actionable message instead
// of a generic failure.
var (
	// ErrInvalidShell means the requested shell path failed ValidateShell.
	ErrInvalidShell = errors.New("invalid shell path")
	// ErrHostNotLinux means the Docker daemon's OSType is not "linux". There
	// is no PID 1 to nsenter into on Windows containers.
	ErrHostNotLinux = errors.New("host shell requires a Linux Docker host")
	// ErrDockerDesktop means the daemon is Docker Desktop. PID 1 there is the
	// LinuxKit VM init, not the user's actual machine — nsenter would give a
	// root shell in a hidden VM while the user believes it is their host.
	// Refused outright rather than silently doing the wrong thing.
	ErrDockerDesktop = errors.New("host shell is not supported on Docker Desktop")
	// ErrRootlessDaemon means the daemon reports a rootless security option.
	// nsenter -t 1 fails with EPERM under rootless Docker/Podman; refused
	// up front with a clear message instead of a cryptic exec failure.
	ErrRootlessDaemon = errors.New("host shell is not supported on a rootless Docker daemon")
	// ErrImageUnavailable means the helper image could not be inspected or
	// pulled. Wrapped with the image name and the underlying pull error.
	ErrImageUnavailable = errors.New("host shell helper image unavailable")
)

// shellPattern matches an absolute path made of path-safe characters, with
// no "..", no whitespace, and no shell metacharacters. It intentionally does
// not allow arguments — a shell is a path, not a command line.
var shellPattern = regexp.MustCompile(`^/[A-Za-z0-9._\-/]+$`)

// Labels returns the labels applied to a host-shell helper container.
// sessionID identifies the caller's session or run for diagnostics and
// targeted cleanup; it is never interpreted, only stored.
func Labels(sessionID string) map[string]string {
	return map[string]string{
		libarcane.InternalResourceLabel: "true",
		ContainerLabel:                  "true",
		SessionLabel:                    sessionID,
	}
}

// ValidateShell checks that shell is safe to place into container argv and
// pass to nsenter as the target command, and returns it unchanged on
// success. It is not a general command validator: it rejects anything that
// looks like it carries arguments or shell syntax, because a shell here must
// be a single executable path.
func ValidateShell(shell string) (string, error) {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		return "", errors.WrapIf(ErrInvalidShell, "shell must not be empty")
	}
	if len(shell) > maxShellLength {
		return "", errors.WrapIf(ErrInvalidShell, "shell path is too long")
	}
	if strings.Contains(shell, "..") {
		return "", errors.WrapIf(ErrInvalidShell, "shell path must not contain '..'")
	}
	if !shellPattern.MatchString(shell) {
		return "", errors.WrapIf(ErrInvalidShell, "shell must be an absolute path with no arguments or special characters")
	}
	return shell, nil
}

// Preflight decides whether host-shell access should be attempted against
// the connected daemon at all, independent of any per-request validation.
// It never returns a generic error — every failure is one of the sentinels
// above so the caller (and ultimately the UI) can render a specific reason.
func Preflight(info systemtypes.Info) error {
	if !strings.EqualFold(info.OSType, "linux") {
		return errors.WrapIff(ErrHostNotLinux, "daemon OSType is %q", info.OSType)
	}
	if strings.Contains(info.OperatingSystem, "Docker Desktop") {
		return ErrDockerDesktop
	}
	for _, opt := range info.SecurityOptions {
		if strings.Contains(opt, "name=rootless") {
			return ErrRootlessDaemon
		}
	}
	return nil
}

// Config builds the container.Config for a host-shell helper. cmd is the
// full argv the container runs as PID 1 inside its own namespaces — for the
// interactive terminal this is NsenterCommand(...) with the requested shell;
// for a scripted run it is NsenterCommand(...) with a shell invoked in
// stdin-script mode (e.g. "/bin/sh", "-s"). env entries are applied as the
// process environment; callers must never fold parameter values into cmd or
// any other string field instead of env.
func Config(image, sessionID string, cmd, env []string, tty bool) *containertypes.Config {
	return &containertypes.Config{
		Image: image,
		// Entrypoint is cleared because the tools image may set one; without
		// this, cmd would become arguments to that entrypoint instead of
		// replacing it (same reasoning as the lifecycle runner image).
		Entrypoint:   []string{},
		Cmd:          cmd,
		Env:          env,
		Tty:          tty,
		OpenStdin:    true,
		StdinOnce:    true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Labels:       Labels(sessionID),
	}
}

// HostConfig builds the HostConfig that grants a helper container the
// namespaces it needs to nsenter into PID 1. This is full root on the
// Docker host — every caller-facing entry point into this package must be
// gated behind an explicit, off-by-default opt-in.
//
// AutoRemove is deliberately false: callers force-remove the container
// themselves so a failure during setup or teardown is observable and the
// container remains inspectable in the window before removal.
func HostConfig() *containertypes.HostConfig {
	return &containertypes.HostConfig{
		Privileged:  true,
		PidMode:     containertypes.PidMode("host"),
		NetworkMode: containertypes.NetworkMode("host"),
		IpcMode:     containertypes.IpcMode("host"),
		UTSMode:     containertypes.UTSMode("host"),
		AutoRemove:  false,
	}
}

// NsenterCommand builds the argv that enters PID 1's mount, UTS, IPC,
// network and PID namespaces and then execs target. When workingDir is
// non-empty it is passed to nsenter via -w rather than interpolated into any
// shell text, so it never needs quoting.
func NsenterCommand(workingDir string, target []string) []string {
	args := []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p"}
	if workingDir != "" {
		args = append(args, "-w", workingDir)
	}
	args = append(args, "--")
	return append(args, target...)
}

// RemoveOptions returns the container remove options used for host-shell
// helpers.
func RemoveOptions() client.ContainerRemoveOptions {
	return client.ContainerRemoveOptions{Force: true}
}

// IsHelperContainer reports whether a container summary is a host-shell
// helper, for orphan-cleanup sweeps at startup.
func IsHelperContainer(labels map[string]string) bool {
	return strings.EqualFold(labels[ContainerLabel], "true")
}

// ResolveImage returns the image to use for a host-shell helper container.
// override (the hostTerminalImage setting) wins when set; otherwise it
// defaults to DefaultToolsImage. Unlike volumehelper.ResolveHelperImage,
// there is deliberately no fallback to the Arcane runtime image: that image
// has no guaranteed nsenter binary, and silently substituting it on a
// root-shell code path would be surprising in a way that matters. A pull
// failure is returned wrapped in ErrImageUnavailable, naming the image, so
// the caller can point an air-gapped operator at pre-pulling it or setting
// hostTerminalImage.
func ResolveImage(ctx context.Context, dockerClient *client.Client, override string) (string, error) {
	if dockerClient == nil {
		return "", errors.New("docker service unavailable")
	}

	image := strings.TrimSpace(override)
	if image == "" {
		image = DefaultToolsImage
	}

	if _, err := dockerClient.ImageInspect(ctx, image); err == nil {
		return image, nil
	}

	pullReader, pullErr := dockerClient.ImagePull(ctx, image, client.ImagePullOptions{})
	if pullErr != nil {
		return "", errors.WrapIff(ErrImageUnavailable, "%s: %s", image, pullErr.Error())
	}
	if pullReader == nil {
		return "", errors.WrapIff(ErrImageUnavailable, "%s: pull returned no response body", image)
	}
	defer func() { _ = pullReader.Close() }()
	if _, err := io.Copy(io.Discard, pullReader); err != nil {
		return "", errors.WrapIff(ErrImageUnavailable, "%s: %s", image, err.Error())
	}

	return image, nil
}
