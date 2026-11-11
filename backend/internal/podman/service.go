package podman

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/mo"

	"kit.getarcane.app"
	"kit.getarcane.app/docker/compat"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/podman/children/kube"
	"getarcane.dev/arcane/v3/internal/podman/children/pods"
	"getarcane.dev/arcane/v3/internal/podman/children/quadlets"
	"getarcane.dev/arcane/v3/internal/podman/children/secrets"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/notifications"
	libpodman "getarcane.dev/arcane/v3/pkg/podman"
	"getarcane.dev/types/v3"
)

type (
	Config struct {
		Engine        func(context.Context) (compat.EngineCompatibilityInfo, error)
		NewSystemd    func(rootless bool) libpodman.Systemd
		NotifyUpdates func(context.Context, []notifications.ContainerUpdateBatchEntry) error
		Host          func() string
	}

	Service struct {
		*detector

		pods     *pods.Service
		quadlets *quadlets.Service
		kube     *kube.Service
		secrets  *secrets.Service
	}

	detector struct {
		config     Config
		conn       *hostClient
		cachedAt   time.Time
		cachedHost string
		cached     libpodman.Runtime

		mu sync.Mutex
	}

	hostClient struct {
		err    error
		client *libpodman.Client
		host   string
	}
)

const (
	detectionTTL           = 30 * time.Second
	autoUpdateUnit         = "podman-auto-update.service"
	autoUpdatePollInterval = time.Second
)

func NewService(cfg Config) *Service {
	d := &detector{config: cfg}
	return &Service{
		detector: d,
		pods:     pods.NewService(d.detect),
		quadlets: quadlets.NewService(d.quadletRuntime),
		kube:     kube.NewService(d.detect),
		secrets:  secrets.NewService(d.detect),
	}
}

func (s *Service) Status(ctx context.Context) types.PodmanStatus {
	rt, err := s.detect(ctx)
	switch {
	case errors.Is(err, common.ErrPodmanAPIUnavailable):
		slog.WarnContext(ctx, "Podman engine detected but its libpod API is unreachable", "error", err)
		return types.PodmanStatus{
			AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable,
			Reason:         err.Error(),
		}
	case errors.Is(err, common.ErrPodmanNotDetected):
		return types.PodmanStatus{AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable}
	case err != nil:
		slog.DebugContext(ctx, "Podman detection failed", "error", err)
		return types.PodmanStatus{AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable}
	}

	availability := rt.Systemd.Availability(ctx)
	features := libpodman.DetectFeatures(rt.Version.Version, availability.Available)
	return types.PodmanStatus{
		Detected:             true,
		Version:              rt.Version.Version,
		APIVersion:           rt.Version.APIVersion,
		Rootless:             rt.Rootless,
		QuadletsSupported:    features.QuadletsSupported,
		QuadletAppsSupported: features.QuadletAppsSupported,
		AutoUpdateMode:       features.AutoUpdateMode,
		SystemdAvailable:     availability.Available,
		SystemdScope:         availability.Scope,
		SystemdReason:        availability.Reason,
		PodCount:             s.PodCount(ctx).OrElse(0),
	}
}

func (s *Service) AggregateStatus(ctx context.Context) (types.ApiResponse[types.PodmanStatus], error) {
	return httpx.CollectEnvironmentData(ctx, nil, func(environments []types.EnvironmentData[types.PodmanStatus]) types.PodmanStatus {
		detected, podCount := false, 0
		for _, environment := range environments {
			detected = detected || environment.Data.Detected
			podCount += environment.Data.PodCount
		}
		return types.PodmanStatus{
			Detected:       detected,
			AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable,
			PodCount:       podCount,
		}
	})
}

func (s *Service) Info(ctx context.Context) (types.PodmanInfo, error) {
	rt, err := s.detect(ctx)
	if err != nil {
		return types.PodmanInfo{}, err
	}
	return rt.Client.Info(ctx)
}

func (s *Service) PodCount(ctx context.Context) mo.Option[int] {
	items, err := s.pods.List(ctx)
	if err != nil {
		if !errors.Is(err, common.ErrPodmanNotDetected) {
			slog.DebugContext(ctx, "Skipping Podman pod count", "error", err)
		}
		return mo.None[int]()
	}
	return mo.Some(len(items))
}

func (s *Service) ContainerUnitAction(ctx context.Context, containerRef string, action func(libpodman.Systemd, context.Context, string) error) (bool, error) {
	rt, err := s.detect(ctx)
	if err != nil || containerRef == "" {
		return false, nil
	}

	containers, err := rt.Client.ContainerList(ctx, nil, nil)
	if err != nil {
		slog.WarnContext(ctx, "Could not list containers", "container", containerRef, "error", err)
		return false, nil
	}

	match, found := resolveContainer(containers, containerRef)
	if !found {
		return false, nil
	}

	unit := types.QuadletUnit(match.Labels)
	if unit == "" {
		return false, nil
	}
	availability := rt.Systemd.Availability(ctx)
	managed, err := s.quadletManaged(ctx, rt, unit, availability)
	if err != nil {
		return true, fmt.Errorf("verify quadlet unit %s for container %s: %w", unit, containerRef, err)
	}
	if !managed {
		return false, nil
	}
	if !availability.Available {
		return true, common.Classify(common.ErrUnavailable, fmt.Errorf("container %s is managed by systemd unit %s, but Arcane cannot reach systemd: %s", containerRef, unit, availability.Reason))
	}
	return true, action(rt.Systemd, ctx, unit)
}

func (s *Service) GuardQuadletManaged(ctx context.Context, labels map[string]string) error {
	unit := types.QuadletUnit(labels)
	if unit == "" {
		return nil
	}

	rt, err := s.detect(ctx)
	if errors.Is(err, common.ErrPodmanNotDetected) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify quadlet unit %s: %w", unit, err)
	}

	managed, err := s.quadletManaged(ctx, rt, unit, rt.Systemd.Availability(ctx))
	if err != nil {
		return fmt.Errorf("verify quadlet unit %s: %w", unit, err)
	}
	if managed {
		return fmt.Errorf("systemd unit %s: %w", unit, common.ErrContainerQuadletManaged)
	}
	return nil
}

func (s *Service) quadletManaged(ctx context.Context, rt libpodman.Runtime, unit string, availability types.PodmanSystemdAvailability) (bool, error) {
	if availability.Available {
		status, err := rt.Systemd.UnitStatus(ctx, unit)
		if errors.Is(err, common.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return status.LoadState == "loaded", nil
	}
	if !libpodman.DetectFeatures(rt.Version.Version, false).QuadletsSupported {
		return true, nil
	}

	installed, err := s.quadlets.List(ctx, nil)
	if err != nil {
		return false, err
	}
	name, instance, templated := strings.Cut(unit, "@")
	return slices.ContainsFunc(installed, func(quadlet types.QuadletDetail) bool {
		return quadlet.UnitName == unit || (templated && quadlet.UnitName == name+"@"+path.Ext(instance))
	}), nil
}

func resolveContainer(containers []types.PodmanContainer, ref string) (types.PodmanContainer, bool) {
	if index := slices.IndexFunc(containers, func(c types.PodmanContainer) bool {
		return c.ID == ref
	}); index >= 0 {
		return containers[index], true
	}
	if index := slices.IndexFunc(containers, func(c types.PodmanContainer) bool {
		return slices.Contains(c.Names, strings.TrimPrefix(ref, "/"))
	}); index >= 0 {
		return containers[index], true
	}
	matches := slices.DeleteFunc(slices.Clone(containers), func(c types.PodmanContainer) bool {
		return !strings.HasPrefix(c.ID, ref)
	})
	if len(matches) != 1 {
		return types.PodmanContainer{}, false
	}
	return matches[0], true
}

func (s *Service) AutoUpdate(ctx context.Context, opts types.PodmanAutoUpdateOptions) (types.PodmanAutoUpdateResult, error) {
	rt, err := s.detect(ctx)
	if err != nil {
		return types.PodmanAutoUpdateResult{}, err
	}

	availability := rt.Systemd.Availability(ctx)
	switch libpodman.DetectFeatures(rt.Version.Version, availability.Available).AutoUpdateMode {
	case types.PodmanAutoUpdateModeREST:
		result, updateErr := rt.Client.AutoUpdate(ctx, opts)
		if updateErr != nil {
			return types.PodmanAutoUpdateResult{}, updateErr
		}
		if !opts.DryRun {
			entries := make([]notifications.ContainerUpdateBatchEntry, 0, len(result.Reports))
			for _, report := range result.Reports {
				if report.Updated == "true" {
					entries = append(entries, notifications.ContainerUpdateBatchEntry{
						ContainerName: report.ContainerName,
						ImageRef:      report.ImageName,
					})
				}
			}
			if notifyErr := s.config.NotifyUpdates(ctx, entries); notifyErr != nil {
				slog.WarnContext(ctx, "Could not send Podman auto-update notification", "error", notifyErr)
			}
		}
		return result, nil
	case types.PodmanAutoUpdateModeSystemd:
		if opts.DryRun {
			return types.PodmanAutoUpdateResult{}, common.Classify(common.ErrBadRequest, errors.New("auto-update dry run requires Podman 6.2 or later"))
		}
		if len(opts.Filters) > 0 || opts.Rollback != nil || opts.TLSVerify != nil || opts.Authfile != "" {
			return types.PodmanAutoUpdateResult{}, common.Classify(common.ErrBadRequest, errors.New("auto-update filters, rollback, TLS verification and authfile options require Podman 6.2 or later"))
		}
		startErr := rt.Systemd.Start(ctx, autoUpdateUnit)
		if startErr != nil && !errors.Is(startErr, common.ErrConflict) {
			return types.PodmanAutoUpdateResult{}, startErr
		}
		unit, statusErr := rt.Systemd.UnitStatus(ctx, autoUpdateUnit)
		for statusErr == nil && unit.ActiveState == "activating" {
			select {
			case <-ctx.Done():
				return types.PodmanAutoUpdateResult{}, ctx.Err()
			case <-time.After(autoUpdatePollInterval):
			}
			unit, statusErr = rt.Systemd.UnitStatus(ctx, autoUpdateUnit)
		}
		if statusErr != nil {
			return types.PodmanAutoUpdateResult{}, statusErr
		}
		return types.PodmanAutoUpdateResult{
			Mode:    types.PodmanAutoUpdateModeSystemd,
			Reports: []types.PodmanAutoUpdateReport{},
			Errors:  kit.Ternary(unit.ActiveState == "failed" || startErr != nil, []string{autoUpdateUnit + " failed: " + unit.SubState}, []string{}),
			Unit:    &unit,
		}, nil
	default:
		return types.PodmanAutoUpdateResult{}, common.Classify(common.ErrConflict, fmt.Errorf("podman auto-update is unavailable: %s", cmp.Or(availability.Reason, "systemd is not reachable")))
	}
}

func (s *detector) detect(ctx context.Context) (libpodman.Runtime, error) {
	host := s.config.Host()
	s.mu.Lock()
	cached, fresh := s.cached, s.cachedHost == host && time.Since(s.cachedAt) < detectionTTL
	s.mu.Unlock()
	if fresh {
		return cached, nil
	}

	engine, err := s.config.Engine(ctx)
	if err != nil {
		return libpodman.Runtime{}, err
	}
	if engine.Name != "podman" {
		return libpodman.Runtime{}, common.ErrPodmanNotDetected
	}
	s.mu.Lock()
	if s.conn == nil || s.conn.host != host {
		client, clientErr := libpodman.NewClient(host)
		s.conn = &hostClient{
			err:    clientErr,
			client: client,
			host:   host,
		}
	}
	conn := s.conn
	s.mu.Unlock()
	if conn.err != nil {
		return libpodman.Runtime{}, fmt.Errorf("%w: %w", common.ErrPodmanAPIUnavailable, conn.err)
	}

	version, err := conn.client.Version(ctx)
	if err != nil {
		return libpodman.Runtime{}, fmt.Errorf("%w: %w", common.ErrPodmanAPIUnavailable, err)
	}
	info, err := conn.client.Info(ctx)
	if err != nil {
		return libpodman.Runtime{}, fmt.Errorf("%w: %w", common.ErrPodmanAPIUnavailable, err)
	}

	rt := libpodman.Runtime{
		Client:   conn.client,
		Version:  version,
		Rootless: info.Host.Security.Rootless,
		Systemd:  s.config.NewSystemd(info.Host.Security.Rootless),
	}
	s.mu.Lock()
	s.cached, s.cachedAt, s.cachedHost = rt, time.Now(), host
	s.mu.Unlock()
	return rt, nil
}

func (s *detector) quadletRuntime(ctx context.Context) (quadlets.Runtime, error) {
	rt, err := s.detect(ctx)
	if err != nil {
		return quadlets.Runtime{}, err
	}

	features := libpodman.DetectFeatures(rt.Version.Version, false)
	if !features.QuadletsSupported {
		return quadlets.Runtime{}, common.Classify(common.ErrBadRequest, fmt.Errorf("quadlet management requires Podman 5.8 or later; this host runs %s", rt.Version.Version))
	}
	return quadlets.Runtime{
		Client:        rt.Client,
		Systemd:       rt.Systemd,
		AppsSupported: features.QuadletAppsSupported,
	}, nil
}
