package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/getarcaneapp/arcane/types/v2/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/streams/bus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	dockerClientNegotiationTimeout = 5 * time.Second

	// dockerEventStreamHealthyAfter is how long an event stream must survive before
	// the reconnect backoff is considered recovered and reset.
	dockerEventStreamHealthyAfter = 30 * time.Second
)

type DockerClientService struct {
	*client.Client

	db              *database.DB
	config          *config.Config
	settingsService *settings.SettingsService
	clientVersion   string
	clientLastProbe time.Time
	mu              sync.Mutex
	eventBus        *bus.DockerEventBus

	// Coalesce concurrent full-inventory list calls so overlapping requests
	// (e.g. the Homepage widget's simultaneous counts endpoints) decode one
	// Docker response instead of one per caller. Per-instance on purpose:
	// a WithClient copy talks to a different daemon and must not share flights.
	imageListFlight     singleflight.Group
	containerListFlight singleflight.Group
}

func NewDockerClientService(_ context.Context, db *database.DB, cfg *config.Config, settingsService *settings.SettingsService, eventBusOptions ...bus.Option) *DockerClientService {
	return &DockerClientService{
		db:              db,
		config:          cfg,
		settingsService: settingsService,
		eventBus:        bus.NewDockerEventBus(eventBusOptions...),
	}
}

// WithClient returns a copy of the service bound to an already-connected Docker
// client, skipping host discovery. Used where the caller owns the connection.
func (s *DockerClientService) WithClient(dockerClient *client.Client) *DockerClientService {
	return &DockerClientService{
		db:              s.db,
		config:          s.config,
		settingsService: s.settingsService,
		Client:          dockerClient,
		eventBus:        s.eventBus,
	}
}

func newDockerClientInternal(ctx context.Context, host string) (*client.Client, error) {
	apiVersion, err := detectDockerAPIVersionInternal(ctx, host)
	if err != nil {
		return nil, err
	}

	configuredClient, err := newDockerClientWithAPIVersionInternal(host, apiVersion)
	if err != nil {
		return nil, err
	}

	return configuredClient, nil
}

func detectDockerAPIVersionInternal(ctx context.Context, host string) (string, error) {
	probeClient, err := client.New(
		client.WithHost(host),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Docker probe client: %w", err)
	}
	defer closeDockerClientInternal(ctx, probeClient, "probe")

	ctx, cancel := context.WithTimeout(ctx, dockerClientNegotiationTimeout)
	defer cancel()

	pingResult, err := probeClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to negotiate Docker API version: %w", err)
	}

	apiVersion := strings.TrimSpace(pingResult.APIVersion)
	if apiVersion == "" {
		slog.WarnContext(ctx, "Docker ping did not report an API version, using minimum supported client API version", "apiVersion", client.MinAPIVersion)
		return client.MinAPIVersion, nil
	}

	return apiVersion, nil
}

func newDockerClientWithAPIVersionInternal(host, apiVersion string) (*client.Client, error) {
	configuredClient, err := client.New(
		client.WithHost(host),
		client.WithAPIVersion(apiVersion),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to configure Docker client API version %s: %w", apiVersion, err)
	}

	return configuredClient, nil
}

func closeDockerClientInternal(ctx context.Context, cli *client.Client, purpose string) {
	if cli == nil {
		return
	}

	if err := cli.Close(); err != nil {
		slog.WarnContext(ctx, "Failed to close Docker client", "purpose", purpose, "error", err)
	}
}

// GetClient returns a singleton Docker client instance.
// It initializes the client on the first call.
func (s *DockerClientService) GetClient(ctx context.Context) (*client.Client, error) {
	s.mu.Lock()
	if s.Client != nil {
		cli := s.Client
		s.mu.Unlock()
		return cli, nil
	}
	s.mu.Unlock()

	cli, err := newDockerClientInternal(ctx, s.config.DockerHost)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	s.mu.Lock()
	if s.Client != nil {
		existingClient := s.Client
		s.mu.Unlock()
		closeDockerClientInternal(ctx, cli, "unused after concurrent initialization")
		return existingClient, nil
	}

	s.Client = cli
	s.clientVersion = cli.ClientVersion()
	s.clientLastProbe = time.Now()
	s.mu.Unlock()

	return cli, nil
}

// RefreshClient probes the Docker daemon and recreates the cached client when
// the daemon's effective API version changed.
func (s *DockerClientService) RefreshClient(ctx context.Context) error {
	apiVersion, err := detectDockerAPIVersionInternal(ctx, s.config.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: %w", err)
	}

	s.mu.Lock()
	if s.Client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	cli, err := newDockerClientWithAPIVersionInternal(s.config.DockerHost, apiVersion)
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: %w", err)
	}

	s.mu.Lock()
	if s.Client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		closeDockerClientInternal(ctx, cli, "unused after concurrent refresh")
		return nil
	}

	oldClient := s.Client
	s.Client = cli
	s.clientVersion = apiVersion
	s.clientLastProbe = time.Now()
	s.mu.Unlock()

	closeDockerClientInternal(ctx, oldClient, "replaced")

	return nil
}

// DockerHost returns the configured DOCKER_HOST value.
func (s *DockerClientService) DockerHost() string {
	if s == nil || s.config == nil {
		return ""
	}
	return s.config.DockerHost
}

// Close closes the cached Docker client.
func (s *DockerClientService) Close() {
	s.mu.Lock()
	oldClient := s.Client
	s.Client = nil
	s.mu.Unlock()

	closeDockerClientInternal(context.Background(), oldClient, "cached") //nolint:forbidigo // Shutdown close runs without a request context.
}

func (s *DockerClientService) EventBus() *bus.DockerEventBus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eventBus == nil {
		s.eventBus = bus.NewDockerEventBus()
	}
	return s.eventBus
}

func (s *DockerClientService) WatchEvents(ctx context.Context) {
	eventBackoff := backoff.NewExponentialBackOff()
	eventBackoff.InitialInterval = 500 * time.Millisecond
	eventBackoff.MaxInterval = 30 * time.Second

	for ctx.Err() == nil {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			slog.WarnContext(ctx, "failed to connect to Docker event stream", "error", err)
			if !sleepDockerEventBackoffInternal(ctx, eventBackoff) {
				return
			}
			continue
		}

		result := dockerClient.Events(ctx, client.EventsListOptions{})
		streamStart := time.Now()
		err = s.consumeEventsInternal(ctx, result.Messages, result.Err)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			slog.WarnContext(ctx, "Docker event stream stopped", "error", err)
		}

		// Only a stream that actually stayed up counts as recovery. Resetting
		// unconditionally before Events() meant a daemon that accepts the
		// connection and drops it immediately was reconnected at the floor
		// interval forever.
		if time.Since(streamStart) >= dockerEventStreamHealthyAfter {
			eventBackoff.Reset()
		}

		if !sleepDockerEventBackoffInternal(ctx, eventBackoff) {
			return
		}
	}
}

func (s *DockerClientService) consumeEventsInternal(ctx context.Context, messages <-chan events.Message, errs <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			// Stamp missing daemon timestamps once, before normal and overflow delivery diverge.
			if msg.TimeNano == 0 {
				msg.TimeNano = time.Now().UnixNano()
			}
			s.EventBus().Publish(msg)
		case err, ok := <-errs:
			return kit.Ternary(!ok, nil, err)
		}
	}
}

func sleepDockerEventBackoffInternal(ctx context.Context, eventBackoff *backoff.ExponentialBackOff) bool {
	delay := eventBackoff.NextBackOff()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// listCoalescedInternal runs op through the given singleflight group so
// concurrent callers share one Docker request and one decoded result. The
// flight runs on a context detached from the initiating caller, bounded only
// by op's own Docker API timeout, so one canceled waiter cannot cancel work
// other waiters still need; each waiter stops waiting when its own context
// ends. Results and errors are shared only among concurrent callers —
// singleflight drops the key once the flight completes, so nothing is cached.
// Callers must treat the shared result as immutable.
func listCoalescedInternal[T any](ctx context.Context, group *singleflight.Group, op func(context.Context) (T, error)) (T, error) {
	var zero T

	ch := group.DoChan("list", func() (any, error) {
		return op(context.WithoutCancel(ctx))
	})

	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return zero, res.Err
		}
		val, ok := res.Val.(T)
		if !ok {
			return zero, errors.New("docker list flight returned unexpected type")
		}
		return val, nil
	}
}

func (s *DockerClientService) apiTimeoutInternal() time.Duration {
	if s.settingsService == nil {
		return timeouts.DefaultDockerAPI
	}
	return timeouts.GetDuration(s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
}

func (s *DockerClientService) ListContainers(ctx context.Context) ([]container.Summary, error) {
	return listCoalescedInternal(ctx, &s.containerListFlight, func(ctx context.Context) ([]container.Summary, error) {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		apiCtx, cancel := context.WithTimeout(ctx, s.apiTimeoutInternal())
		defer cancel()

		containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker containers: %w", err)
		}
		return containerList.Items, nil
	})
}

func (s *DockerClientService) ListImages(ctx context.Context) ([]image.Summary, error) {
	return listCoalescedInternal(ctx, &s.imageListFlight, func(ctx context.Context) ([]image.Summary, error) {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		apiCtx, cancel := context.WithTimeout(ctx, s.apiTimeoutInternal())
		defer cancel()

		imageList, err := dockerClient.ImageList(apiCtx, client.ImageListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker images: %w", err)
		}
		return imageList.Items, nil
	})
}

func (s *DockerClientService) listNetworksInternal(ctx context.Context) ([]network.Summary, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	apiCtx, cancel := context.WithTimeout(ctx, s.apiTimeoutInternal())
	defer cancel()

	networkList, err := compat.NetworkListWithCompatibility(apiCtx, dockerClient, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker networks: %w", err)
	}
	return networkList.Items, nil
}

func (s *DockerClientService) listVolumesInternal(ctx context.Context) (*client.VolumeListResult, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	apiCtx, cancel := context.WithTimeout(ctx, s.apiTimeoutInternal())
	defer cancel()

	volResp, err := dockerClient.VolumeList(apiCtx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker volumes: %w", err)
	}
	return &volResp, nil
}

func (s *DockerClientService) GetSnapshot(ctx context.Context, envID string) (*dashboard.DockerSnapshot, error) {
	g, groupCtx := errgroup.WithContext(ctx)

	var containers []container.Summary
	var images []image.Summary
	var networks []network.Summary
	var volumes *client.VolumeListResult

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		containers, err = s.ListContainers(groupCtx)
		return err
	})
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		images, err = s.ListImages(groupCtx)
		return err
	})
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		networks, err = s.listNetworksInternal(groupCtx)
		if err != nil {
			slog.WarnContext(groupCtx, "failed to list Docker networks for snapshot", "error", err)
		}
		return nil
	})
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		volumes, err = s.listVolumesInternal(groupCtx)
		if err != nil {
			slog.WarnContext(groupCtx, "failed to list Docker volumes for snapshot", "error", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &dashboard.DockerSnapshot{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
	}, nil
}

func (s *DockerClientService) GetAllContainers(ctx context.Context) ([]container.Summary, int, int, int, error) {
	containers, err := s.ListContainers(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}

	var running, stopped, total int
	for _, c := range containers {
		total++
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}

	return containers, running, stopped, total, nil
}

// GetAllImages lists images and containers concurrently and returns the
// images together with their usage counts, including total image size.
func (s *DockerClientService) GetAllImages(ctx context.Context) ([]image.Summary, imagetypes.UsageCounts, error) {
	g, groupCtx := errgroup.WithContext(ctx)

	var images []image.Summary
	var containers []container.Summary

	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker image list worker")

		var err error
		images, err = s.ListImages(groupCtx)
		return err
	})
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker container list worker")

		var err error
		containers, err = s.ListContainers(groupCtx)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, imagetypes.UsageCounts{}, err
	}

	return images, CountImageUsage(images, containers), nil
}

// CountImageUsage tallies in-use, unused, and total image counts, plus the
// summed size of all images. An image is in use when a container references
// its ID.
func CountImageUsage(images []image.Summary, containers []container.Summary) imagetypes.UsageCounts {
	inUseImageIDs := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		if c.ImageID == "" {
			continue
		}
		inUseImageIDs[c.ImageID] = struct{}{}
	}

	var counts imagetypes.UsageCounts
	for _, img := range images {
		counts.Total++
		counts.TotalSize += img.Size
		if _, ok := inUseImageIDs[img.ID]; ok {
			counts.Inuse++
			continue
		}
		counts.Unused++
	}

	return counts
}

func (s *DockerClientService) GetAllNetworks(ctx context.Context) ([]network.Summary, int, int, int, error) {
	containers, err := s.ListContainers(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	inUseByID, inUseByName := docker.BuildNetworkUsageMaps(containers)

	networks, err := s.listNetworksInternal(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}

	var inuse, unused, total int
	for _, n := range networks {
		total++ // total includes all networks (including defaults)

		// Only count non-default networks towards in-use/unused breakdown
		if !docker.IsDefaultNetwork(n.Name) {
			used := inUseByID[n.ID] || inUseByName[n.Name]
			if used {
				inuse++
			} else {
				unused++
			}
		}
	}

	// Return order: inuse, unused, total (matches handler expectations)
	return networks, inuse, unused, total, nil
}

func (s *DockerClientService) GetAllVolumes(ctx context.Context) ([]*volume.Volume, int, int, int, error) {
	containers, err := s.ListContainers(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	ref := make(map[string]int64, len(containers))
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				ref[m.Name]++
			}
		}
	}

	volResp, err := s.listVolumesInternal(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	volumeItems := volResp.Items
	volumes := make([]*volume.Volume, 0, len(volumeItems))
	for i := range volumeItems {
		volumes = append(volumes, &volumeItems[i])
	}

	var inuse, unused, total int
	for _, v := range volumes {
		total++
		if ref[v.Name] > 0 {
			inuse++
		} else {
			unused++
		}
	}

	return volumes, inuse, unused, total, nil
}
