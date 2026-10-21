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
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
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
	engine          compat.EngineCompatibilityInfo
	engineClient    *client.Client
	mu              sync.Mutex
	eventBus        *bus.DockerEventBus

	// Coalesce concurrent full-inventory lists into one Docker response; per-instance
	// because a WithClient copy talks to a different daemon.
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

func closeDockerClient(ctx context.Context, cli *client.Client, purpose string) {
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
	cli := s.Client
	s.mu.Unlock()
	if cli != nil {
		return cli, nil
	}

	if err := s.RefreshClient(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Client, nil
}

// RefreshClient probes the Docker daemon and recreates the cached client when
// the daemon's effective API version changed.
func (s *DockerClientService) RefreshClient(ctx context.Context) error {
	probeClient, err := client.New(client.WithHost(s.config.DockerHost))
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: failed to create Docker probe client: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, dockerClientNegotiationTimeout)
	pingResult, err := probeClient.Ping(pingCtx, client.PingOptions{})
	cancel()
	closeDockerClient(ctx, probeClient, "probe")
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: failed to negotiate Docker API version: %w", err)
	}

	apiVersion := strings.TrimSpace(pingResult.APIVersion)
	if apiVersion == "" {
		slog.WarnContext(ctx, "Docker ping did not report an API version, using minimum supported client API version", "apiVersion", client.MinAPIVersion)
		apiVersion = client.MinAPIVersion
	}

	s.mu.Lock()
	if s.Client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	cli, err := client.New(client.WithHost(s.config.DockerHost), client.WithAPIVersion(apiVersion))
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: failed to configure Docker client API version %s: %w", apiVersion, err)
	}

	s.mu.Lock()
	if s.Client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		closeDockerClient(ctx, cli, "unused after concurrent refresh")
		return nil
	}

	oldClient := s.Client
	s.Client = cli
	s.clientVersion = apiVersion
	s.clientLastProbe = time.Now()
	s.engineClient = nil
	s.mu.Unlock()

	closeDockerClient(ctx, oldClient, "replaced")

	return nil
}

func (s *DockerClientService) Engine(ctx context.Context) (compat.EngineCompatibilityInfo, error) {
	s.mu.Lock()
	if s.engineClient != nil && s.engineClient == s.Client {
		engine := s.engine
		s.mu.Unlock()
		return engine, nil
	}
	s.mu.Unlock()

	return s.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (compat.EngineCompatibilityInfo, error) {
		version, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
		if err != nil {
			return compat.EngineCompatibilityInfo{}, fmt.Errorf("failed to get Docker server version: %w", err)
		}
		info, err := dockerClient.Info(ctx, client.InfoOptions{})
		if err != nil {
			return compat.EngineCompatibilityInfo{}, fmt.Errorf("failed to get Docker info: %w", err)
		}

		engine := compat.DetectEngineCompatibility(version, info.Info)
		s.mu.Lock()
		s.engine = engine
		s.engineClient = dockerClient
		s.mu.Unlock()
		return engine, nil
	})
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
	s.engineClient = nil
	s.mu.Unlock()

	closeDockerClient(context.Background(), oldClient, "cached") //nolint:forbidigo // Shutdown close runs without a request context.
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

	// Resume from the last received event so a reconnect replays events missed during the gap.
	var lastEventNano int64
	for ctx.Err() == nil {
		if dockerClient, err := s.GetClient(ctx); err != nil {
			slog.WarnContext(ctx, "failed to connect to Docker event stream", "error", err)
		} else {
			result := dockerClient.Events(ctx, client.EventsListOptions{
				Since: kit.Ternary(lastEventNano == 0, "", fmt.Sprintf("%d.%09d", (lastEventNano+1)/int64(time.Second), (lastEventNano+1)%int64(time.Second))),
			})
			streamStart := time.Now()
			var streamErr error
		stream:
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-result.Messages:
					if !ok {
						break stream
					}
					// Stamp missing daemon timestamps once, before normal and overflow delivery diverge.
					if msg.TimeNano == 0 {
						msg.TimeNano = time.Now().UnixNano()
					}
					lastEventNano = msg.TimeNano
					s.EventBus().Publish(msg)
				case recvErr, ok := <-result.Err:
					streamErr = kit.Ternary(ok, recvErr, nil)
					break stream
				}
			}
			if streamErr != nil && !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, io.EOF) {
				slog.WarnContext(ctx, "Docker event stream stopped", "error", streamErr)
			}
			// Only a stream that stayed up counts as recovery, so a daemon that drops connections immediately keeps backing off.
			if time.Since(streamStart) >= dockerEventStreamHealthyAfter {
				eventBackoff.Reset()
			}
		}

		timer := time.NewTimer(eventBackoff.NextBackOff())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		// A restarted daemon may report a different API version, so re-probe before reconnecting.
		if err := s.RefreshClient(ctx); err != nil {
			slog.DebugContext(ctx, "failed to refresh Docker client before event stream reconnect", "error", err)
		}
	}
}

// ListCoalesced shares one detached Call among concurrent callers; each waiter stops on its own
// context, nothing is cached, and the shared result must be treated as immutable.
// Streaming calls (logs, events, attach, pull, exec, stats, wait) must not use it because Call applies a deadline.
func (s *DockerClientService) ListCoalesced[T any](ctx context.Context, group *singleflight.Group, op func(context.Context, *client.Client) (T, error)) (T, error) {
	var zero T

	ch := group.DoChan("list", func() (any, error) {
		return s.Call(context.WithoutCancel(ctx), op)
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

// Call runs op against the cached client under the configured Docker API timeout.
// Streaming calls (logs, events, attach, pull, exec, stats, wait) must not use it because it applies a deadline.
func (s *DockerClientService) Call[T any](ctx context.Context, op func(context.Context, *client.Client) (T, error)) (T, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	timeout := timeouts.DefaultDockerAPI
	if s.settingsService != nil {
		timeout = timeouts.GetDuration(s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	}
	apiCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return op(apiCtx, dockerClient)
}

func (s *DockerClientService) ListContainers(ctx context.Context) ([]container.Summary, error) {
	return s.ListCoalesced(ctx, &s.containerListFlight, func(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error) {
		containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker containers: %w", err)
		}
		return containerList.Items, nil
	})
}

func (s *DockerClientService) ListImages(ctx context.Context) ([]image.Summary, error) {
	return s.ListCoalesced(ctx, &s.imageListFlight, func(ctx context.Context, dockerClient *client.Client) ([]image.Summary, error) {
		imageList, err := dockerClient.ImageList(ctx, client.ImageListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker images: %w", err)
		}
		return imageList.Items, nil
	})
}

func (s *DockerClientService) GetSnapshot(ctx context.Context, envID string) (*types.DashboardDockerSnapshot, error) {
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
		networks, err = s.Call(groupCtx, func(ctx context.Context, dockerClient *client.Client) ([]network.Summary, error) {
			networkList, listErr := compat.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
			if listErr != nil {
				return nil, fmt.Errorf("failed to list Docker networks: %w", listErr)
			}
			return networkList.Items, nil
		})
		if err != nil {
			slog.WarnContext(groupCtx, "failed to list Docker networks for snapshot", "error", err)
		}
		return nil
	})
	g.Go(func() (workerErr error) {
		defer utils.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		volumes, err = s.Call(groupCtx, func(ctx context.Context, dockerClient *client.Client) (*client.VolumeListResult, error) {
			volResp, listErr := dockerClient.VolumeList(ctx, client.VolumeListOptions{})
			if listErr != nil {
				return nil, fmt.Errorf("failed to list Docker volumes: %w", listErr)
			}
			return &volResp, nil
		})
		if err != nil {
			slog.WarnContext(groupCtx, "failed to list Docker volumes for snapshot", "error", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &types.DashboardDockerSnapshot{
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
func (s *DockerClientService) GetAllImages(ctx context.Context) ([]image.Summary, types.ImageUsageCounts, error) {
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
		return nil, types.ImageUsageCounts{}, err
	}

	return images, CountImageUsage(images, containers), nil
}

// CountImageUsage tallies in-use, unused, and total image counts plus the summed
// image size. An image is in use when a container references its ID.
func CountImageUsage(images []image.Summary, containers []container.Summary) types.ImageUsageCounts {
	inUseImageIDs := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		if c.ImageID == "" {
			continue
		}
		inUseImageIDs[c.ImageID] = struct{}{}
	}

	var counts types.ImageUsageCounts
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
	networks, err := s.Call(ctx, func(ctx context.Context, dockerClient *client.Client) ([]network.Summary, error) {
		networkList, listErr := compat.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
		if listErr != nil {
			return nil, fmt.Errorf("failed to list Docker networks: %w", listErr)
		}
		return networkList.Items, nil
	})
	if err != nil {
		return nil, 0, 0, 0, err
	}
	_, unused, total := CountNetworkUsage(networks, containers)
	// Default networks are never unused, matching the network list counts.
	return networks, total - unused, unused, total, nil
}

// CountNetworkUsage returns in-use and unused counts for non-default networks, and the total of all networks.
func CountNetworkUsage(networks []network.Summary, containers []container.Summary) (inuse, unused, total int) {
	inUseByID, inUseByName := docker.BuildNetworkUsageMaps(containers)
	for _, n := range networks {
		total++
		if docker.IsDefaultNetwork(n.Name) {
			continue
		}
		if inUseByID[n.ID] || inUseByName[n.Name] {
			inuse++
		} else {
			unused++
		}
	}
	return inuse, unused, total
}
