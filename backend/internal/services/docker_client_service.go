package services

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
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	docker "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cache"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"go.getarcane.app/streams/bus"
	"golang.org/x/sync/errgroup"
)

const dockerClientNegotiationTimeout = 5 * time.Second
const dockerListCacheTTL = 0
const dockerImageStateResyncActionInternal events.Action = "arcane-resync"

type DockerClientService struct {
	db                *database.DB
	config            *config.Config
	settingsService   *SettingsService
	client            *client.Client
	clientVersion     string
	clientLastProbe   time.Time
	mu                sync.Mutex
	containerCache    *cache.Cache[[]container.Summary]
	imageCache        *cache.Cache[[]image.Summary]
	networkCache      *cache.Cache[[]network.Summary]
	volumeCache       *cache.Cache[*client.VolumeListResult]
	eventBus          *bus.DockerEventBus
	subscriptionCtx   context.Context
	subscriptionStop  context.CancelFunc
	subscriptionUnsub []func()
}

func NewDockerClientService(ctx context.Context, db *database.DB, cfg *config.Config, settingsService *SettingsService) *DockerClientService {
	subscriptionCtx, subscriptionStop := context.WithCancel(ctx)
	svc := &DockerClientService{
		db:               db,
		config:           cfg,
		settingsService:  settingsService,
		containerCache:   cache.New[[]container.Summary](dockerListCacheTTL),
		imageCache:       cache.New[[]image.Summary](dockerListCacheTTL),
		networkCache:     cache.New[[]network.Summary](dockerListCacheTTL),
		volumeCache:      cache.New[*client.VolumeListResult](dockerListCacheTTL),
		eventBus:         bus.NewDockerEventBus(),
		subscriptionCtx:  subscriptionCtx,
		subscriptionStop: subscriptionStop,
	}
	svc.subscribeListCacheInvalidationInternal(subscriptionCtx)
	return svc
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
	defer closeDockerClientInternal(probeClient, "failed to close probe Docker client")

	ctx, cancel := context.WithTimeout(ctx, dockerClientNegotiationTimeout)
	defer cancel()

	pingResult, err := probeClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to negotiate Docker API version: %w", err)
	}

	apiVersion := strings.TrimSpace(pingResult.APIVersion)
	if apiVersion == "" {
		slog.WarnContext(ctx, "Docker ping did not report an API version, using minimum supported client API version", "api_version", client.MinAPIVersion)
		return client.MinAPIVersion, nil
	}

	return apiVersion, nil
}

func newDockerClientWithAPIVersionInternal(host string, apiVersion string) (*client.Client, error) {
	configuredClient, err := client.New(
		client.WithHost(host),
		client.WithAPIVersion(apiVersion),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to configure Docker client API version %s: %w", apiVersion, err)
	}

	return configuredClient, nil
}

func closeDockerClientInternal(cli *client.Client, message string) {
	if cli == nil {
		return
	}

	if err := cli.Close(); err != nil {
		slog.Warn(message, "error", err)
	}
}

// GetClient returns a singleton Docker client instance.
// It initializes the client on the first call.
func (s *DockerClientService) GetClient(ctx context.Context) (*client.Client, error) {
	s.mu.Lock()
	if s.client != nil {
		cli := s.client
		s.mu.Unlock()
		return cli, nil
	}
	s.mu.Unlock()

	cli, err := newDockerClientInternal(ctx, s.config.DockerHost)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	s.mu.Lock()
	if s.client != nil {
		existingClient := s.client
		s.mu.Unlock()
		closeDockerClientInternal(cli, "failed to close unused Docker client after concurrent initialization")
		return existingClient, nil
	}

	s.client = cli
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
	if s.client != nil && apiVersion == s.clientVersion {
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
	if s.client != nil && apiVersion == s.clientVersion {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		closeDockerClientInternal(cli, "failed to close unused Docker client after concurrent refresh")
		return nil
	}

	oldClient := s.client
	s.client = cli
	s.clientVersion = apiVersion
	s.clientLastProbe = time.Now()
	s.mu.Unlock()

	closeDockerClientInternal(oldClient, "failed to close replaced Docker client")

	return nil
}

// DockerHost returns the configured DOCKER_HOST value.
func (s *DockerClientService) DockerHost() string {
	return s.config.DockerHost
}

// Close stops Docker event subscriptions owned by this service and closes the
// cached Docker client.
func (s *DockerClientService) Close() {
	s.mu.Lock()
	if s.subscriptionStop != nil {
		s.subscriptionStop()
		s.subscriptionStop = nil
	}
	for _, unsubscribe := range s.subscriptionUnsub {
		unsubscribe()
	}
	s.subscriptionUnsub = nil
	oldClient := s.client
	s.client = nil
	s.mu.Unlock()

	closeDockerClientInternal(oldClient, "failed to close Docker client")
}

func (s *DockerClientService) EventBus() *bus.DockerEventBus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eventBus == nil {
		s.eventBus = bus.NewDockerEventBus()
	}
	return s.eventBus
}

func (s *DockerClientService) ensureListCachesInternal() {
	s.mu.Lock()
	if s.containerCache == nil {
		s.containerCache = cache.New[[]container.Summary](dockerListCacheTTL)
	}
	if s.imageCache == nil {
		s.imageCache = cache.New[[]image.Summary](dockerListCacheTTL)
	}
	if s.networkCache == nil {
		s.networkCache = cache.New[[]network.Summary](dockerListCacheTTL)
	}
	if s.volumeCache == nil {
		s.volumeCache = cache.New[*client.VolumeListResult](dockerListCacheTTL)
	}
	s.mu.Unlock()
}

func (s *DockerClientService) subscribeListCacheInvalidationInternal(ctx context.Context) {
	s.subscribeCacheInvalidationInternal(ctx, events.ContainerEventType, func() {
		if s.containerCache != nil {
			s.containerCache.Invalidate()
		}
	})
	s.subscribeCacheInvalidationInternal(ctx, events.ImageEventType, func() {
		if s.imageCache != nil {
			s.imageCache.Invalidate()
		}
	})
	s.subscribeCacheInvalidationInternal(ctx, events.NetworkEventType, func() {
		if s.networkCache != nil {
			s.networkCache.Invalidate()
		}
	})
	s.subscribeCacheInvalidationInternal(ctx, events.VolumeEventType, func() {
		if s.volumeCache != nil {
			s.volumeCache.Invalidate()
		}
	})
}

func (s *DockerClientService) subscribeCacheInvalidationInternal(ctx context.Context, eventType events.Type, invalidate func()) {
	ch, unsubscribe := s.EventBus().Subscribe(eventType, bus.WithSubscriberBuffer(16))
	s.mu.Lock()
	s.subscriptionUnsub = append(s.subscriptionUnsub, unsubscribe)
	s.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				invalidate()
			}
		}
	}()
}

func (s *DockerClientService) invalidateListCachesInternal() {
	s.ensureListCachesInternal()
	if s.containerCache != nil {
		s.containerCache.Invalidate()
	}
	if s.imageCache != nil {
		s.imageCache.Invalidate()
	}
	if s.networkCache != nil {
		s.networkCache.Invalidate()
	}
	if s.volumeCache != nil {
		s.volumeCache.Invalidate()
	}
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

		s.invalidateListCachesInternal()
		eventBackoff.Reset()
		result := dockerClient.Events(ctx, client.EventsListOptions{})
		s.publishImageStateResyncInternal()
		err = s.consumeEventsInternal(ctx, result.Messages, result.Err)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			slog.WarnContext(ctx, "Docker event stream stopped", "error", err)
		}
		if !sleepDockerEventBackoffInternal(ctx, eventBackoff) {
			return
		}
	}
}

func (s *DockerClientService) publishImageStateResyncInternal() {
	s.EventBus().Publish(events.Message{
		Type:   events.ImageEventType,
		Action: dockerImageStateResyncActionInternal,
	})
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
			s.EventBus().Publish(msg)
		case err, ok := <-errs:
			if !ok {
				return nil
			}
			return err
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

func (s *DockerClientService) listContainersInternal(ctx context.Context) ([]container.Summary, error) {
	s.ensureListCachesInternal()
	containerCache := s.containerCache
	return containerCache.GetOrFetch(ctx, func(ctx context.Context) ([]container.Summary, error) {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		settings := s.settingsService.GetSettingsConfig()
		apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
		defer cancel()

		containerList, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker containers: %w", err)
		}
		return containerList.Items, nil
	})
}

func (s *DockerClientService) listImagesInternal(ctx context.Context) ([]image.Summary, error) {
	s.ensureListCachesInternal()
	imageCache := s.imageCache
	return imageCache.GetOrFetch(ctx, func(ctx context.Context) ([]image.Summary, error) {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		settings := s.settingsService.GetSettingsConfig()
		apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
		defer cancel()

		imageList, err := dockerClient.ImageList(apiCtx, client.ImageListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker images: %w", err)
		}
		return imageList.Items, nil
	})
}

func (s *DockerClientService) listNetworksInternal(ctx context.Context) ([]network.Summary, error) {
	s.ensureListCachesInternal()
	networkCache := s.networkCache
	return networkCache.GetOrFetch(ctx, func(ctx context.Context) ([]network.Summary, error) {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		settings := s.settingsService.GetSettingsConfig()
		apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
		defer cancel()

		networkList, err := libarcane.NetworkListWithCompatibility(apiCtx, dockerClient, client.NetworkListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker networks: %w", err)
		}
		return networkList.Items, nil
	})
}

func (s *DockerClientService) listVolumesInternal(ctx context.Context) (*client.VolumeListResult, error) {
	s.ensureListCachesInternal()
	volumeCache := s.volumeCache
	return volumeCache.GetOrFetch(ctx, func(ctx context.Context) (*client.VolumeListResult, error) {
		dockerClient, err := s.GetClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Docker: %w", err)
		}

		settings := s.settingsService.GetSettingsConfig()
		apiCtx, cancel := timeouts.WithTimeout(ctx, settings.DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
		defer cancel()

		volResp, err := dockerClient.VolumeList(apiCtx, client.VolumeListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker volumes: %w", err)
		}
		return &volResp, nil
	})
}

func (s *DockerClientService) GetSnapshot(ctx context.Context, envID string) (*dashboardtypes.DockerSnapshot, error) {
	g, groupCtx := errgroup.WithContext(ctx)

	var containers []container.Summary
	var images []image.Summary
	var networks []network.Summary
	var volumes *client.VolumeListResult

	g.Go(func() error {
		var err error
		containers, err = s.listContainersInternal(groupCtx)
		return err
	})
	g.Go(func() error {
		var err error
		images, err = s.listImagesInternal(groupCtx)
		return err
	})
	g.Go(func() error {
		var err error
		networks, err = s.listNetworksInternal(groupCtx)
		if err != nil {
			slog.WarnContext(groupCtx, "failed to list Docker networks for snapshot", "error", err)
		}
		return nil
	})
	g.Go(func() error {
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

	return &dashboardtypes.DockerSnapshot{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
	}, nil
}

func (s *DockerClientService) GetAllContainers(ctx context.Context) ([]container.Summary, int, int, int, error) {
	containers, err := s.listContainersInternal(ctx)
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

func (s *DockerClientService) GetAllImages(ctx context.Context) ([]image.Summary, int, int, int, error) {
	images, err := s.listImagesInternal(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}

	containers, err := s.listContainersInternal(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}

	inuse, unused, total := countImageUsageInternal(images, containers)

	return images, inuse, unused, total, nil
}

func countImageUsageInternal(images []image.Summary, containers []container.Summary) (inuse int, unused int, total int) {
	inUseImageIDs := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		if c.ImageID == "" {
			continue
		}
		inUseImageIDs[c.ImageID] = struct{}{}
	}

	for _, img := range images {
		total++
		if _, ok := inUseImageIDs[img.ID]; ok {
			inuse++
			continue
		}
		unused++
	}

	return inuse, unused, total
}

func (s *DockerClientService) GetAllNetworks(ctx context.Context) ([]network.Summary, int, int, int, error) {
	containers, err := s.listContainersInternal(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	inUseByID := make(map[string]bool)
	inUseByName := make(map[string]bool)
	for _, c := range containers {
		if c.NetworkSettings == nil || c.NetworkSettings.Networks == nil {
			continue
		}
		for netName, es := range c.NetworkSettings.Networks {
			if es.NetworkID != "" {
				inUseByID[es.NetworkID] = true
			}
			inUseByName[netName] = true
		}
	}

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
	containers, err := s.listContainersInternal(ctx)
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
