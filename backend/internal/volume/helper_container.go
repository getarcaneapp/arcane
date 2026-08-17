package volume

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"

	"emperror.dev/errors"

	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	acfstypes "go.getarcane.app/acfs/types"
)

// volumeHelper tracks a reused helper container and the last
// time it serviced a request, so idle helpers can be reaped. inUse counts the
// requests currently holding the helper: the reaper must not remove a helper
// mid-download just because it was acquired longer ago than the idle timeout.
type volumeHelper struct {
	id         string
	lastUsedAt time.Time
	inUse      int
	protocol   int
}

func (s *VolumeService) requireVolumeHelperACFSInternal(ctx context.Context, volumeName, containerID string) error {
	s.helperMu.Lock()
	helper := s.helperByVolume[volumeName]
	if helper != nil && helper.id == containerID && helper.protocol >= acfstypes.ProtocolVersion {
		s.helperMu.Unlock()
		return nil
	}
	s.helperMu.Unlock()

	stdout, stderr, err := s.execInContainerInternal(ctx, containerID, []string{"acfs", "version"})
	if err != nil {
		return errors.WrapIf(err, "volume workspace requires an ACFS-capable tools image: "+strings.TrimSpace(stderr))
	}
	var response acfstypes.VersionResponse
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		return errors.WrapIf(err, "parse ACFS tools-image capability")
	}
	if response.Protocol < acfstypes.ProtocolVersion {
		return errors.Errorf("volume workspace requires ACFS protocol %d, tools image provides protocol %d", acfstypes.ProtocolVersion, response.Protocol)
	}

	s.helperMu.Lock()
	if helper = s.helperByVolume[volumeName]; helper != nil && helper.id == containerID {
		helper.protocol = response.Protocol
	}
	s.helperMu.Unlock()
	return nil
}

const volumeHelperImage = volumehelper.DefaultToolsImage

func isUnlabeledVolumeHelperContainerInternal(c container.Summary) bool {
	if !libarcane.IsInternalContainer(c.Labels) {
		return false
	}

	command := strings.ToLower(c.Command)
	if !strings.Contains(command, "sleep") || !strings.Contains(command, "infinity") {
		return false
	}

	for _, mounted := range c.Mounts {
		if mounted.Destination == "/volume" {
			return true
		}
	}

	return false
}

func isVolumeHelperContainerInternal(c container.Summary) bool {
	if isUnlabeledVolumeHelperContainerInternal(c) {
		return true
	}
	if !libarcane.IsInternalContainer(c.Labels) {
		return false
	}

	return strings.EqualFold(c.Labels[volumehelper.ContainerLabel], "true")
}

func getVolumeHelperImageInternal(ctx context.Context, dockerService *docker.DockerClientService, imageService *image.ImageService, dockerClient *client.Client) (string, error) {
	slog.DebugContext(ctx, "volume service: resolve helper image")
	var err error
	if dockerClient == nil {
		if dockerService == nil {
			return "", errors.New("docker service unavailable")
		}
		dockerClient, err = dockerService.GetClient(ctx)
		if err != nil {
			return "", errors.WrapIf(err, "failed to get docker client")
		}
	}

	if _, err := dockerClient.ImageInspect(ctx, volumeHelperImage); err == nil {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "tools-local", "image", volumeHelperImage)
		return volumeHelperImage, nil
	}

	var pullErr error
	if imageService != nil {
		pullImageErr := imageService.PullImage(ctx, volumeHelperImage, io.Discard, common.SystemUser, nil)
		if pullImageErr == nil {
			slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "tools-pulled", "image", volumeHelperImage)
			return volumeHelperImage, nil
		}
		pullErr = pullImageErr
		slog.WarnContext(ctx, "volume service: failed to pull tools helper image, attempting arcane fallback", "error", pullImageErr.Error())
	} else {
		pullErr = errors.New("image service unavailable")
		slog.WarnContext(ctx, "volume service: image service unavailable, attempting arcane fallback")
	}

	if fallback, ok := volumehelper.ResolveArcaneRuntimeImage(ctx, dockerClient).Get(); ok {
		slog.InfoContext(ctx, "volume service: helper image strategy selected", "strategy", "arcane-fallback", "source", fallback.Source, "image", fallback.Image)
		return fallback.Image, nil
	}

	return "", errors.WrapIf(pullErr, "failed to resolve helper image: tools image unavailable and arcane fallback not found")
}

func (s *VolumeService) acquireVolumeHelperInternal(ctx context.Context, volumeName string) (string, func(), error) {
	slog.DebugContext(ctx, "volume service: acquire helper container", "volume", volumeName)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", nil, err
	}

	// Helpers are shared and reaped when idle; the caller's cleanup
	// releases its in-use hold instead of removing the container. The resolve →
	// acquire gap is racy against the reaper, so retry a resolve whose helper
	// was reaped before this caller could take its hold.
	for range 3 {
		containerID, err := s.resolveHelperInternal(ctx, dockerClient, volumeName)
		if err != nil {
			return "", nil, err
		}
		if release, ok := s.acquireHelperInternal(volumeName, containerID); ok {
			return containerID, release, nil
		}
	}
	return "", nil, errors.New("failed to acquire volume helper container")
}

// resolveHelperInternal returns the shared helper container ID
// for volumeName, creating it if needed. singleflight collapses concurrent
// misses: without it both requests create a helper and the loser is orphaned,
// holding the volume mount with no cleanup path until Arcane restarts. The
// creation itself can pull an image, so it must not run under helperMu.
func (s *VolumeService) resolveHelperInternal(ctx context.Context, dockerClient *client.Client, volumeName string) (string, error) {
	resultCh := s.helperGroup.DoChan(volumeName, func() (any, error) {
		if containerID, ok := s.getReusableHelperInternal(ctx, dockerClient, volumeName).Get(); ok {
			return containerID, nil
		}

		// Detached from the caller's context so a client that walks away
		// mid-create doesn't abort the helper the other waiters are blocked on,
		// but still bounded: WithoutCancel alone would let a hung daemon or
		// image pull wedge this singleflight key forever, queueing every later
		// workspace request for the volume behind it with no recovery until restart.
		createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerImagePull)
		defer cancel()

		containerID, _, createErr := s.startHelperContainerInternal(createCtx, dockerClient, volumeName)
		if createErr != nil {
			return nil, createErr
		}

		s.helperMu.Lock()
		s.helperByVolume[volumeName] = &volumeHelper{id: containerID, lastUsedAt: time.Now()}
		s.helperMu.Unlock()
		return containerID, nil
	})

	// DoChan instead of Do so a caller whose request is canceled unblocks
	// immediately; the shared create keeps running for the remaining waiters
	// (and lands in helperByVolume for the next request even if none remain).
	var shared any
	select {
	case <-ctx.Done():
		return "", errors.WrapIf(ctx.Err(), "canceled waiting for volume helper container")
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

// acquireHelperInternal takes an in-use hold on volumeName's helper, reporting
// false when the helper has been removed (e.g. reaped) since it was resolved.
// The returned release drops the hold and refreshes the idle clock, so the idle
// timeout is measured from the end of the last request, not its start — a
// download running longer than the timeout used to have its helper reaped out
// from under it, truncating the stream.
func (s *VolumeService) acquireHelperInternal(volumeName, containerID string) (func(), bool) {
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

// startHelperContainerInternal creates and starts a volume helper container and
// returns a cleanup that removes it. Tracking of reusable helpers is
// the caller's responsibility.
func (s *VolumeService) startHelperContainerInternal(ctx context.Context, dockerClient *client.Client, volumeName string) (string, func(), error) {
	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
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

	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     config,
		HostConfig: hostConfig,
	})
	if err != nil {
		return "", nil, errors.WrapIf(err, "failed to create temp container")
	}

	if _, err := dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = dockerClient.ContainerRemove(ctx, resp.ID, volumehelper.RemoveOptions())
		return "", nil, errors.WrapIf(err, "failed to start temp container")
	}

	cleanup := func() {
		_, _ = dockerClient.ContainerRemove(context.WithoutCancel(ctx), resp.ID, volumehelper.RemoveOptions())
	}

	return resp.ID, cleanup, nil
}

func (s *VolumeService) getReusableHelperInternal(ctx context.Context, dockerClient *client.Client, volumeName string) mo.Option[string] {
	s.helperMu.Lock()
	helper := s.helperByVolume[volumeName]
	s.helperMu.Unlock()
	if helper == nil || helper.id == "" {
		return mo.None[string]()
	}

	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, helper.id, client.ContainerInspectOptions{})
	if err != nil || inspect.Container.State == nil || !inspect.Container.State.Running {
		s.helperMu.Lock()
		delete(s.helperByVolume, volumeName)
		s.helperMu.Unlock()
		return mo.None[string]()
	}

	s.touchHelperInternal(volumeName)

	return mo.Some(helper.id)
}

// touchHelperInternal records that the helper for volumeName just serviced a
// request, resetting its idle clock. No-op if the entry is gone.
func (s *VolumeService) touchHelperInternal(volumeName string) {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	if helper := s.helperByVolume[volumeName]; helper != nil {
		helper.lastUsedAt = time.Now()
	}
}

func (s *VolumeService) CleanupHelperContainers(ctx context.Context) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to get docker client for helper cleanup", "error", err)
		return
	}

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
		if _, err := dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions()); err != nil {
			slog.WarnContext(ctx, "failed to remove helper container", "container_id", containerID, "error", err.Error())
		}
	}
}

// ReapIdleHelpers removes reused helper containers that have
// not serviced a request within idleTimeout. It is map-driven (orphaned helpers
// not tracked in helperByVolume are left to the startup orphan sweep). Entries
// are removed from the map before the container is removed, so a concurrent
// request simply gets a cache miss and re-creates a fresh helper.
func (s *VolumeService) ReapIdleHelpers(ctx context.Context, idleTimeout time.Duration) (int, error) {
	if idleTimeout <= 0 {
		return 0, nil
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to get docker client for idle helper reap")
	}

	staleIDs := s.collectStaleHelperIDsInternal(time.Now(), idleTimeout)

	removed := 0
	for _, containerID := range staleIDs {
		if _, err := dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions()); err != nil {
			slog.WarnContext(ctx, "failed to remove idle helper container", "container_id", containerID, "error", err.Error())
			continue
		}
		removed++
	}

	return removed, nil
}

// collectStaleHelperIDsInternal removes idle (and any nil) entries from the helper
// map and returns the container IDs that should be removed. Pure map/mutex
// bookkeeping with no Docker calls, so it can be unit-tested directly. Entries are
// dropped before their containers are removed so a concurrent request gets a clean
// cache miss and re-creates a fresh helper.
func (s *VolumeService) collectStaleHelperIDsInternal(now time.Time, idleTimeout time.Duration) []string {
	staleIDs := make([]string, 0)
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	for volumeName, helper := range s.helperByVolume {
		if helper == nil {
			delete(s.helperByVolume, volumeName)
			continue
		}
		// A helper with active holds is serving a request right now (e.g. a
		// download streaming for longer than the idle timeout); it becomes
		// reapable once released, since release refreshes lastUsedAt.
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

// StopHelper removes the reused helper for a single volume, if
// one exists. It is idempotent: stopping a volume with no active helper returns
// nil.
func (s *VolumeService) StopHelper(ctx context.Context, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return nil
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return errors.WrapIf(err, "failed to get docker client for helper stop")
	}

	containerID := s.takeHelperIDInternal(volumeName)
	if containerID == "" {
		return nil
	}

	if _, err := dockerClient.ContainerRemove(ctx, containerID, volumehelper.RemoveOptions()); err != nil {
		return errors.WrapIf(err, "failed to remove helper container")
	}

	return nil
}

// takeHelperIDInternal removes the helper entry for volumeName and returns its
// container ID, or "" if there was none. Pure map/mutex bookkeeping.
func (s *VolumeService) takeHelperIDInternal(volumeName string) string {
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

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return 0, errors.WrapIf(err, "failed to get docker client for orphan helper cleanup")
	}

	containers, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return 0, errors.WrapIf(err, "failed to list containers for orphan helper cleanup")
	}

	removedCount := 0
	for _, c := range containers.Items {
		if !isVolumeHelperContainerInternal(c) {
			continue
		}

		if _, err := dockerClient.ContainerRemove(ctx, c.ID, volumehelper.RemoveOptions()); err != nil {
			slog.WarnContext(ctx,
				"volume service: failed to remove orphaned volume helper container",
				"container_id", c.ID,
				"container_names", c.Names,
				"error", err.Error(),
			)
			continue
		}

		removedCount++
	}

	return removedCount, nil
}

func (s *VolumeService) removeHelperEntry(volumeName string) {
	if strings.TrimSpace(volumeName) == "" {
		return
	}
	s.helperMu.Lock()
	delete(s.helperByVolume, volumeName)
	s.helperMu.Unlock()
}

func (s *VolumeService) execInContainerInternal(ctx context.Context, containerID string, cmd []string) (string, string, error) {
	slog.DebugContext(ctx, "volume service: exec in container", "container_id", containerID, "cmd", cmd)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	exitCode, err := dockerutil.ExecInContainer(ctx, dockerClient, containerID, client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}, &stdout, &stderr)
	if err != nil {
		return stdout.String(), stderr.String(), err
	}

	if exitCode != 0 {
		execErr := strings.TrimSpace(stderr.String())
		if execErr == "" {
			execErr = strings.TrimSpace(stdout.String())
		}
		if execErr != "" {
			return stdout.String(), stderr.String(), errors.Errorf("command exited with code %d: %s", exitCode, execErr)
		}
		return stdout.String(), stderr.String(), errors.Errorf("command exited with code %d", exitCode)
	}

	return stdout.String(), stderr.String(), nil
}
