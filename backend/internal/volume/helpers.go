package volume

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	acfstypes "go.getarcane.app/acfs/types"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/volumehelper"
)

// volumeHelper tracks a reused helper container and its last use for idle reaping.
// inUse counts active holds so the reaper never removes a helper mid-request.
type volumeHelper struct {
	id         string
	lastUsedAt time.Time
	inUse      int
	protocol   int
}

func (s *VolumeService) requireVolumeHelperACFS(ctx context.Context, volumeName, containerID string) error {
	s.helperMu.Lock()
	helper := s.helperByVolume[volumeName]
	if helper != nil && helper.id == containerID && helper.protocol >= acfstypes.ProtocolVersion {
		s.helperMu.Unlock()
		return nil
	}
	s.helperMu.Unlock()

	stdout, stderr, err := s.execInContainer(ctx, containerID, "", []string{"acfs", "version"})
	if err != nil {
		return fmt.Errorf("%s: %w", "volume workspace requires an ACFS-capable tools image: "+strings.TrimSpace(stderr), err)
	}
	var response acfstypes.VersionResponse
	if unmarshalErr := json.Unmarshal([]byte(stdout), &response); unmarshalErr != nil {
		return fmt.Errorf("parse ACFS tools-image capability: %w", unmarshalErr)
	}
	if response.Protocol < acfstypes.ProtocolVersion {
		return fmt.Errorf("volume workspace requires ACFS protocol %d, tools image provides protocol %d", acfstypes.ProtocolVersion, response.Protocol)
	}

	s.helperMu.Lock()
	if helper = s.helperByVolume[volumeName]; helper != nil && helper.id == containerID {
		helper.protocol = response.Protocol
	}
	s.helperMu.Unlock()
	return nil
}

func (s *VolumeService) toolsImage() string {
	registry := ""
	if s.settingsService != nil {
		registry = s.settingsService.GetSettingsConfig().ToolsImageRegistry.Value
	}
	return volumehelper.ToolsImage(registry)
}

func isUnlabeledVolumeHelperContainer(c container.Summary) bool {
	if internal, _ := kit.ParseBool(c.Labels[libarcane.InternalResourceLabel]); !internal {
		return false
	}

	command := strings.ToLower(c.Command)
	return strings.Contains(command, "sleep") && strings.Contains(command, "infinity") && slices.ContainsFunc(c.Mounts, func(mounted container.MountPoint) bool {
		return mounted.Destination == "/volume"
	})
}

func isVolumeHelperContainer(c container.Summary) bool {
	if internal, _ := kit.ParseBool(c.Labels[libarcane.InternalResourceLabel]); !internal {
		return false
	}
	return strings.EqualFold(c.Labels[volumehelper.ContainerLabel], "true") || isUnlabeledVolumeHelperContainer(c)
}

func (s *VolumeService) getVolumeHelperImage(ctx context.Context) (string, error) {
	slog.DebugContext(ctx, "volume service: resolve helper image")
	toolsImage := s.toolsImage()

	if _, imageInspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, toolsImage)
	}); imageInspectErr == nil {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "tools-local", "image", toolsImage)
		return toolsImage, nil
	}

	var pullErr error
	if s.imageService != nil {
		pullImageErr := s.imageService.PullImage(ctx, toolsImage, io.Discard, types.SystemUser, nil)
		if pullImageErr == nil {
			slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "tools-pulled", "image", toolsImage)
			return toolsImage, nil
		}
		pullErr = pullImageErr
		slog.WarnContext(ctx, "volume service: failed to pull tools helper image, attempting arcane fallback", "error", pullImageErr.Error())
	} else {
		pullErr = errors.New("image service unavailable")
		slog.WarnContext(ctx, "volume service: image service unavailable, attempting arcane fallback")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get docker client: %w", err)
	}
	if fallback, ok := volumehelper.ResolveArcaneRuntimeImage(ctx, dockerClient).Get(); ok {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "arcane-fallback", "source", fallback.Source, "image", fallback.Image)
		return fallback.Image, nil
	}

	return "", fmt.Errorf("failed to resolve helper image: tools image unavailable and arcane fallback not found: %w", pullErr)
}

func (s *VolumeService) acquireVolumeHelper(ctx context.Context, volumeName string) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: acquire helper container", "volume", volumeName)

	// The returned cleanup releases a hold on the shared helper; retry when the reaper wins the resolve-to-acquire race.
	for range 3 {
		containerID, resolveHelperErr := s.resolveHelper(ctx, volumeName)
		if resolveHelperErr != nil {
			return "", nil, resolveHelperErr
		}
		if release, ok := s.acquireHelper(volumeName, containerID); ok {
			return containerID, release, nil
		}
	}
	return "", nil, errors.New("failed to acquire volume helper container")
}

// resolveHelper returns volumeName's shared helper, creating it once via singleflight so concurrent misses never orphan one.
// Creation can pull an image, so it runs outside helperMu.
func (s *VolumeService) resolveHelper(ctx context.Context, volumeName string) (string, error) {
	resultCh := s.helperGroup.DoChan(volumeName, func() (any, error) {
		helper, err := s.getReusableHelper(ctx, volumeName)
		if err != nil {
			return nil, err
		}
		if containerID, ok := helper.Get(); ok {
			return containerID, nil
		}

		// Detached so one caller leaving does not abort the shared create, but bounded so a hung pull cannot wedge the key.
		createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerImagePull)
		defer cancel()

		containerID, _, createErr := s.startHelperContainer(createCtx, volumeName)
		if createErr != nil {
			return nil, createErr
		}

		s.helperMu.Lock()
		s.helperByVolume[volumeName] = &volumeHelper{id: containerID, lastUsedAt: time.Now()}
		s.helperMu.Unlock()
		return containerID, nil
	})

	// DoChan lets a canceled caller return at once while the shared create finishes for the others.
	var shared any
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("canceled waiting for volume helper container: %w", ctx.Err())
	case result := <-resultCh:
		if result.Err != nil {
			return "", result.Err
		}
		shared = result.Val
	}

	containerID, ok := shared.(string)
	if !ok || containerID == "" {
		return "", errors.New("failed to resolve volume helper container")
	}
	return containerID, nil
}

// acquireHelper takes a hold on volumeName's helper, or reports false if it was removed since being resolved.
// Release refreshes the idle clock so the timeout counts from the end of the last request.
func (s *VolumeService) acquireHelper(volumeName, containerID string) (func(), bool) {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()

	helper := s.helperByVolume[volumeName]
	if helper == nil || helper.id != containerID {
		return nil, false
	}

	helper.inUse++
	helper.lastUsedAt = time.Now()

	return func() {
		s.helperMu.Lock()
		defer s.helperMu.Unlock()
		if current := s.helperByVolume[volumeName]; current == helper {
			helper.inUse--
			helper.lastUsedAt = time.Now()
		}
	}, true
}

// startHelperContainer creates and starts a helper container and returns a cleanup that removes it.
func (s *VolumeService) startHelperContainer(ctx context.Context, volumeName string) (string, func(), error) {
	helperImage, err := s.getVolumeHelperImage(ctx)
	if err != nil {
		return "", nil, err
	}

	config := &container.Config{
		Image:           helperImage,
		Cmd:             []string{"sleep", "infinity"},
		NetworkDisabled: true,
		Labels:          volumehelper.Labels(),
	}

	hostConfig := volumehelper.HostConfig(helperImage, []string{volumeName + ":/volume"}, nil)

	resp, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerCreateResult, error) {
		return dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config:     config,
			HostConfig: hostConfig,
		})
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp container: %w", err)
	}

	if _, containerStartErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerStartResult, error) {
		return dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	}); containerStartErr != nil {
		startErr := fmt.Errorf("failed to start temp container: %w", containerStartErr)
		if _, cleanupErr := s.dockerService.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		}); cleanupErr != nil && !errdefs.IsNotFound(cleanupErr) {
			return "", nil, errors.Join(startErr, fmt.Errorf("failed to remove temp container after start failure: %w", cleanupErr))
		}
		return "", nil, startErr
	}

	cleanup := func() {
		_, _ = s.dockerService.Call(context.WithoutCancel(ctx), func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		})
	}

	return resp.ID, cleanup, nil
}

func (s *VolumeService) getReusableHelper(ctx context.Context, volumeName string) (mo.Option[string], error) {
	s.helperMu.Lock()
	helper := s.helperByVolume[volumeName]
	s.helperMu.Unlock()
	if helper == nil || helper.id == "" {
		return mo.None[string](), nil
	}

	inspect, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
		return compat.ContainerInspectWithCompatibility(ctx, dockerClient, helper.id, client.ContainerInspectOptions{})
	})
	if err != nil && !errdefs.IsNotFound(err) {
		return mo.None[string](), fmt.Errorf("failed to inspect volume helper container: %w", err)
	}
	running := err == nil && kit.FromPtr(inspect.Container.State).Running
	if err == nil && !running {
		_, removeErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, helper.id, volumehelper.RemoveOptions())
		})
		if removeErr != nil && !errdefs.IsNotFound(removeErr) {
			return mo.None[string](), fmt.Errorf("failed to remove stopped volume helper container: %w", removeErr)
		}
	}
	if !running {
		s.helperMu.Lock()
		if s.helperByVolume[volumeName] == helper {
			delete(s.helperByVolume, volumeName)
		}
		s.helperMu.Unlock()
		return mo.None[string](), nil
	}

	s.touchHelper(volumeName)

	return mo.Some(helper.id), nil
}

// touchHelper resets the idle clock of volumeName's helper, if any.
func (s *VolumeService) touchHelper(volumeName string) {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	if helper := s.helperByVolume[volumeName]; helper != nil {
		helper.lastUsedAt = time.Now()
	}
}

func (s *VolumeService) CleanupHelperContainers(ctx context.Context) {
	s.helperMu.Lock()
	helperIDs := make([]string, 0, len(s.helperByVolume))
	for _, helper := range s.helperByVolume {
		if helper != nil && helper.id != "" {
			helperIDs = append(helperIDs, helper.id)
		}
	}
	s.helperByVolume = make(map[string]*volumeHelper)
	s.helperMu.Unlock()

	for _, containerID := range helperIDs {
		if _, containerRemoveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions())
		}); containerRemoveErr != nil {
			slog.WarnContext(ctx, "failed to remove helper container", "containerId", containerID, "error", containerRemoveErr.Error())
		}
	}
}

// ReapIdleHelpers removes tracked helpers idle for idleTimeout; untracked orphans are left to the startup sweep.
func (s *VolumeService) ReapIdleHelpers(ctx context.Context, idleTimeout time.Duration) (int, error) {
	if idleTimeout < 0 {
		return 0, nil
	}

	staleIDs := s.collectStaleHelperIDs(time.Now(), idleTimeout)

	removed := 0
	for _, containerID := range staleIDs {
		if _, containerRemoveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions())
		}); containerRemoveErr != nil {
			slog.WarnContext(ctx, "failed to remove idle helper container", "containerId", containerID, "error", containerRemoveErr.Error())
			continue
		}
		removed++
	}

	return removed, nil
}

// collectStaleHelperIDs drops idle and nil entries and returns their container IDs.
// Entries go before the containers so a concurrent request re-creates a fresh helper.
func (s *VolumeService) collectStaleHelperIDs(now time.Time, idleTimeout time.Duration) []string {
	staleIDs := make([]string, 0)
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	for volumeName, helper := range s.helperByVolume {
		if helper == nil {
			delete(s.helperByVolume, volumeName)
			continue
		}
		// Held helpers are serving a request; release refreshes lastUsedAt.
		if helper.inUse > 0 {
			continue
		}
		if now.Sub(helper.lastUsedAt) >= idleTimeout {
			staleIDs = append(staleIDs, helper.id)
			delete(s.helperByVolume, volumeName)
		}
	}
	return staleIDs
}

// StopHelper removes the reused helper for a single volume; it is a no-op when none exists.
func (s *VolumeService) StopHelper(ctx context.Context, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return nil
	}

	containerID := s.takeHelperID(volumeName)
	if containerID == "" {
		return nil
	}

	if _, containerRemoveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
		return dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions())
	}); containerRemoveErr != nil {
		return fmt.Errorf("failed to remove helper container: %w", containerRemoveErr)
	}

	return nil
}

// takeHelperID removes volumeName's helper entry and returns its container ID, or "".
func (s *VolumeService) takeHelperID(volumeName string) string {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	helper := s.helperByVolume[volumeName]
	delete(s.helperByVolume, volumeName)
	if helper == nil {
		return ""
	}
	return helper.id
}

func (s *VolumeService) CleanupOrphanedVolumeHelpers(ctx context.Context) (int, error) {
	slog.DebugContext(ctx, "volume service: cleanup orphaned volume helper containers")

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list containers for orphan helper cleanup: %w", err)
	}

	removedCount := 0
	for _, c := range containers {
		if !isVolumeHelperContainer(c) {
			continue
		}

		if _, containerRemoveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerRemoveResult, error) {
			return dockerClient.ContainerRemove(ctx, c.ID, volumehelper.RemoveOptions())
		}); containerRemoveErr != nil {
			slog.WarnContext(
				ctx,
				"volume service: failed to remove orphaned volume helper container",
				"containerId", c.ID,
				"containerNames", c.Names,
				"error", containerRemoveErr.Error(),
			)
			continue
		}

		removedCount++
	}

	return removedCount, nil
}

func (s *VolumeService) execInContainer(ctx context.Context, containerID, execUser string, cmd []string) (string, string, error) {
	slog.DebugContext(ctx, "volume service: exec in container", "containerId", containerID, "user", execUser, "cmd", cmd)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	exitCode, err := docker.ExecInContainer(ctx, dockerClient, containerID, client.ExecCreateOptions{
		User:         execUser,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}, &stdout, &stderr)
	if err != nil {
		return stdout.String(), stderr.String(), err
	}

	if exitCode != 0 {
		execErr := cmp.Or(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()))
		if execErr != "" {
			return stdout.String(), stderr.String(), fmt.Errorf("command exited with code %d: %s", exitCode, execErr)
		}
		return stdout.String(), stderr.String(), fmt.Errorf("command exited with code %d", exitCode)
	}

	return stdout.String(), stderr.String(), nil
}
