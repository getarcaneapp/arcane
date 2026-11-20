package docker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker"
	"kit.getarcane.app/docker/compat"
	"kit.getarcane.app/docker/streams/bus"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/dhi"
	"getarcane.dev/arcane/v3/pkg/envcontext"
	"getarcane.dev/arcane/v3/pkg/startup"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	DockerClientService struct {
		clientLastProbe time.Time

		// Coalesce concurrent full-inventory lists into one Docker response; per-instance
		// because a WithClient copy talks to a different daemon.
		imageListFlight     singleflight.Group
		containerListFlight singleflight.Group
		diskUsageFlight     singleflight.Group
		*client.Client

		db              *database.DB
		settingsService *settings.SettingsService
		engineClient    *client.Client
		eventBus        *bus.DockerEventBus
		streamConnected *concurrency.Signal[struct{}]
		inventory       atomic.Pointer[dockerInventory]
		hostClients     map[string]*client.Client

		engine        compat.EngineCompatibilityInfo
		clientVersion string
		clientHost    string
		boundHost     string
		mu            sync.Mutex
		// hostMu guards hostClients. Clients are keyed by endpoint because two docker-host
		// environments may point at different daemons.
		hostMu sync.Mutex
	}

	dockerInventory struct {
		containers atomic.Pointer[[]container.Summary]
		images     atomic.Pointer[[]image.Summary]
	}
)

const (
	dockerClientNegotiationTimeout = 5 * time.Second
	dockerEventStreamHealthyAfter  = 30 * time.Second
)

func NewDockerClientService(ctx context.Context, db *database.DB, settingsService *settings.SettingsService, eventBusConfig bus.Config) *DockerClientService {
	service := &DockerClientService{
		db:              db,
		settingsService: settingsService,
		eventBus:        bus.NewDockerEventBus(eventBusConfig),
		streamConnected: concurrency.NewSignal[struct{}](),
	}
	context.AfterFunc(ctx, settingsService.SubscribeSettingsChanges([]string{"dockerHost"}, func([]types.SettingUpdate) {
		service.Close()
	}))
	return service
}

func (s *DockerClientService) WithClient(dockerClient *client.Client) *DockerClientService {
	return &DockerClientService{
		db:              s.db,
		settingsService: s.settingsService,
		Client:          dockerClient,
		boundHost:       dockerClient.DaemonHost(),
		eventBus:        s.eventBus,
		streamConnected: s.streamConnected,
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

func (s *DockerClientService) GetClient(ctx context.Context) (*client.Client, error) {
	if host := s.HostForContext(ctx); host != "" {
		return s.GetHostClient(ctx, host)
	}

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
	host := s.DockerHost()
	probeClient, err := client.New(client.WithHost(host))
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
	if s.Client != nil && apiVersion == s.clientVersion && host == s.clientHost {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	dropInventoryOnWrite := func(method string) {
		if inventory := s.inventory.Load(); inventory != nil && method != http.MethodGet && method != http.MethodHead {
			s.inventory.CompareAndSwap(inventory, &dockerInventory{})
		}
	}
	cli, err := client.New(
		client.WithHost(host),
		client.WithAPIVersion(apiVersion),
		client.WithHTTPRequestHook(func(req *http.Request) error {
			dropInventoryOnWrite(req.Method)
			return nil
		}),
		client.WithHTTPResponseHook(func(resp *http.Response) {
			dropInventoryOnWrite(kit.FromPtr(resp.Request).Method)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to refresh Docker client: failed to configure Docker client API version %s: %w", apiVersion, err)
	}

	s.mu.Lock()
	if host != s.DockerHost() {
		s.mu.Unlock()
		closeDockerClient(ctx, cli, "stale host after settings change")
		return s.RefreshClient(ctx)
	}
	if s.Client != nil && apiVersion == s.clientVersion && host == s.clientHost {
		s.clientLastProbe = time.Now()
		s.mu.Unlock()
		closeDockerClient(ctx, cli, "unused after concurrent refresh")
		return nil
	}

	oldClient := s.Client
	s.Client = cli
	s.clientVersion = apiVersion
	s.clientHost = host
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

func (s *DockerClientService) DockerHost() string {
	if s.boundHost != "" {
		return s.boundHost
	}
	return startup.ResolveDockerHost(s.settingsService.GetSettingsConfig().DockerHost.Value, os.Stat)
}

func (s *DockerClientService) Close() {
	s.mu.Lock()
	oldClient := s.Client
	s.Client = nil
	s.engineClient = nil
	s.mu.Unlock()
	s.inventory.Store(nil)

	closeDockerClient(context.Background(), oldClient, "cached") //nolint:forbidigo // Shutdown close runs without a request context.

	s.hostMu.Lock()
	hostClients := s.hostClients
	s.hostClients = nil
	s.hostMu.Unlock()
	for host, cli := range hostClients {
		closeDockerClient(context.Background(), cli, "docker-host shutdown "+host) //nolint:forbidigo // Shutdown close runs without a request context.
	}
}

// HostForContext returns the Docker endpoint the request is scoped to, or an empty string when it
// targets the manager's own daemon (the local environment and background work carry no target).
func (s *DockerClientService) HostForContext(ctx context.Context) string {
	host := strings.TrimSpace(envcontext.DockerHostFromContext(ctx))
	if host == "" || host == s.DockerHost() {
		return ""
	}
	return host
}

// ResolveHostForContext returns the daemon endpoint a request must use: the environment target when
// the request carries one (a docker-host environment), and the manager's own daemon otherwise. It is
// for the calls that hand a host to the compose/CLI layer, which cannot read the request context.
func (s *DockerClientService) ResolveHostForContext(ctx context.Context) string {
	return cmp.Or(s.HostForContext(ctx), s.DockerHost())
}

// GetHostClient returns a cached Docker client for an explicit endpoint. A docker-host environment
// is served in-process against its own daemon, so its client is cached per endpoint instead of
// replacing the manager's own client.
func (s *DockerClientService) GetHostClient(ctx context.Context, host string) (*client.Client, error) {
	host = strings.TrimSpace(host)
	if host == "" || host == s.DockerHost() {
		return s.GetClient(context.WithoutCancel(ctx))
	}

	s.hostMu.Lock()
	if cli, ok := s.hostClients[host]; ok {
		s.hostMu.Unlock()
		return cli, nil
	}
	s.hostMu.Unlock()

	cli, err := newHostDockerClientInternal(ctx, host)
	if err != nil {
		return nil, err
	}

	s.hostMu.Lock()
	if s.hostClients == nil {
		s.hostClients = make(map[string]*client.Client)
	}
	if existing, ok := s.hostClients[host]; ok {
		s.hostMu.Unlock()
		closeDockerClient(ctx, cli, "unused after concurrent initialization")
		return existing, nil
	}
	s.hostClients[host] = cli
	s.hostMu.Unlock()

	return cli, nil
}

// PingHost verifies that a Docker endpoint is reachable.
func (s *DockerClientService) PingHost(ctx context.Context, host string) error {
	cli, err := s.GetHostClient(ctx, host)
	if err != nil {
		return err
	}

	pingCtx, cancel := context.WithTimeout(ctx, dockerClientNegotiationTimeout)
	defer cancel()

	if _, pingErr := cli.Ping(pingCtx, client.PingOptions{}); pingErr != nil {
		return fmt.Errorf("failed to reach Docker host %s: %w", host, pingErr)
	}
	return nil
}

// CloseHostClient drops the cached client for an endpoint, so the next request reconnects. Used
// when a docker-host environment changes or is removed.
func (s *DockerClientService) CloseHostClient(ctx context.Context, host string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return
	}

	s.hostMu.Lock()
	cli, ok := s.hostClients[host]
	delete(s.hostClients, host)
	s.hostMu.Unlock()

	if ok {
		closeDockerClient(ctx, cli, "docker-host environment removed")
	}
}

// newHostDockerClientInternal negotiates the daemon's API version and builds a client for it.
func newHostDockerClientInternal(ctx context.Context, host string) (*client.Client, error) {
	probeClient, err := client.New(client.WithHost(host))
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker probe client for %s: %w", host, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, dockerClientNegotiationTimeout)
	pingResult, err := probeClient.Ping(pingCtx, client.PingOptions{})
	cancel()
	closeDockerClient(ctx, probeClient, "probe")
	if err != nil {
		return nil, fmt.Errorf("failed to negotiate Docker API version for %s: %w", host, err)
	}

	apiVersion := strings.TrimSpace(pingResult.APIVersion)
	if apiVersion == "" {
		apiVersion = client.MinAPIVersion
	}

	cli, err := client.New(client.WithHost(host), client.WithAPIVersion(apiVersion))
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client for %s: %w", host, err)
	}
	return cli, nil
}

func (s *DockerClientService) EventBus() *bus.DockerEventBus {
	return s.eventBus
}

func (s *DockerClientService) EventStreamConnected() *concurrency.Signal[struct{}] {
	return s.streamConnected
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
			s.inventory.Store(&dockerInventory{})
			s.streamConnected.Publish(struct{}{})
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
					if !strings.HasPrefix(string(msg.Action), "exec_") {
						s.inventory.Store(&dockerInventory{})
					}
					s.EventBus().Publish(msg)
				case recvErr, ok := <-result.Err:
					streamErr = kit.Ternary(ok, recvErr, nil)
					break stream
				}
			}
			s.inventory.Store(nil)
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

func (s *DockerClientService) ListCoalesced[T any](ctx context.Context, group *singleflight.Group, key string, op func(context.Context, *client.Client) (T, error)) (T, error) {
	var zero T
	// Never coalesce requests aimed at different daemons: a docker-host environment
	// and the manager's own daemon can list at the same time during fan-out.
	key = s.HostForContext(ctx) + "\x00" + key

	ch := group.DoChan(key, func() (any, error) {
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

func (s *DockerClientService) Call[T any](ctx context.Context, op func(context.Context, *client.Client) (T, error)) (T, error) {
	dockerClient, err := s.GetClient(ctx)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	timeout := timeouts.GetDuration(s.settingsService.GetSettingsConfig().DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	apiCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return op(apiCtx, dockerClient)
}

func (s *DockerClientService) inventoryForContext(ctx context.Context) *dockerInventory {
	// The inventory is fed by the manager's event stream, so it only describes the
	// manager's own daemon. A docker-host target must always query its own daemon.
	if s.HostForContext(ctx) != "" {
		return nil
	}
	return s.inventory.Load()
}

func (s *DockerClientService) ListContainers(ctx context.Context, live bool) ([]container.Summary, error) {
	inventory := s.inventoryForContext(ctx)
	if inventory != nil && !live {
		if cached := inventory.containers.Load(); cached != nil {
			return *cached, nil
		}
	}
	return s.ListCoalesced(ctx, &s.containerListFlight, fmt.Sprintf("%p", inventory), func(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error) {
		containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker containers: %w", err)
		}
		if inventory != nil {
			inventory.containers.Store(&containerList.Items)
		}
		return containerList.Items, nil
	})
}

func (s *DockerClientService) ListImages(ctx context.Context) ([]image.Summary, error) {
	inventory := s.inventoryForContext(ctx)
	if inventory != nil {
		if cached := inventory.images.Load(); cached != nil {
			return *cached, nil
		}
	}
	return s.ListCoalesced(ctx, &s.imageListFlight, fmt.Sprintf("%p", inventory), func(ctx context.Context, dockerClient *client.Client) ([]image.Summary, error) {
		imageList, err := dockerClient.ImageList(ctx, client.ImageListOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("failed to list Docker images: %w", err)
		}
		if inventory != nil {
			inventory.images.Store(&imageList.Items)
		}
		return imageList.Items, nil
	})
}

func (s *DockerClientService) GetDiskUsage(ctx context.Context) (client.DiskUsageResult, error) {
	return s.ListCoalesced(ctx, &s.diskUsageFlight, "", func(ctx context.Context, dockerClient *client.Client) (client.DiskUsageResult, error) {
		usageCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDockerDiskUsage)
		defer cancel()
		return dockerClient.DiskUsage(usageCtx, client.DiskUsageOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			BuildCache: true,
			Verbose:    true,
		})
	})
}

func (s *DockerClientService) GetSnapshot(ctx context.Context, envID string) (*types.DashboardDockerSnapshot, error) {
	g, groupCtx := errgroup.WithContext(ctx)

	var containers []container.Summary
	var images []image.Summary
	var networks []network.Summary
	var volumes *client.VolumeListResult

	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		containers, err = s.ListContainers(groupCtx, false)
		return err
	})
	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "docker snapshot worker")

		var err error
		images, err = s.ListImages(groupCtx)
		return err
	})
	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "docker snapshot worker")

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
		defer concurrency.RecoverToError(&workerErr, "docker snapshot worker")

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
	containers, err := s.ListContainers(ctx, false)
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

func (s *DockerClientService) GetAllImages(ctx context.Context) ([]image.Summary, types.ImageUsageCounts, error) {
	g, groupCtx := errgroup.WithContext(ctx)

	var images []image.Summary
	var containers []container.Summary

	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "docker image list worker")

		var err error
		images, err = s.ListImages(groupCtx)
		return err
	})
	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "docker container list worker")

		var err error
		containers, err = s.ListContainers(groupCtx, false)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, types.ImageUsageCounts{}, err
	}

	return images, CountImageUsage(images, containers, time.Now()), nil
}

func CountImageUsage(images []image.Summary, containers []container.Summary, now time.Time) types.ImageUsageCounts {
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
		switch kit.FromPtr(dhi.Info(img.Labels, now)).SupportStatus {
		case types.HardenedImageEndingSoon:
			counts.HardenedEndingSoon++
		case types.HardenedImageEndOfLife:
			counts.HardenedEndOfLife++
		}
		if _, ok := inUseImageIDs[img.ID]; ok {
			counts.Inuse++
			continue
		}
		counts.Unused++
	}

	return counts
}

func (s *DockerClientService) GetAllNetworks(ctx context.Context) ([]network.Summary, int, int, int, error) {
	g, groupCtx := errgroup.WithContext(ctx)
	var containers []container.Summary
	var networks []network.Summary
	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "network container list worker")
		var err error
		containers, err = s.ListContainers(groupCtx, false)
		return err
	})
	g.Go(func() (workerErr error) {
		defer concurrency.RecoverToError(&workerErr, "network list worker")
		var err error
		networks, err = s.Call(groupCtx, func(ctx context.Context, dockerClient *client.Client) ([]network.Summary, error) {
			networkList, listErr := compat.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
			if listErr != nil {
				return nil, fmt.Errorf("failed to list Docker networks: %w", listErr)
			}
			return networkList.Items, nil
		})
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, 0, 0, 0, err
	}
	_, unused, total := CountNetworkUsage(networks, containers)
	// Default networks are never unused, matching the network list counts.
	return networks, total - unused, unused, total, nil
}

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
