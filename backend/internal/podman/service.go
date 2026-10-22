package podman

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/kube"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/pods"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/quadlets"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman/children/secrets"
	libpodman "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/notifications"
)

const (
	detectionTTL           = 30 * time.Second
	autoUpdateUnit         = "podman-auto-update.service"
	autoUpdatePollInterval = time.Second
)

type Config struct {
	Engine        func(context.Context) (compat.EngineCompatibilityInfo, error)
	Host          string
	NewSystemd    func(rootless bool) libpodman.Systemd
	NotifyUpdates func(context.Context, []notifications.ContainerUpdateBatchEntry) error
}

type Service struct {
	*detector

	pods     *pods.Service
	quadlets *quadlets.Service
	kube     *kube.Service
	secrets  *secrets.Service
}

type detector struct {
	config    Config
	client    *libpodman.Client
	clientErr error

	mu       sync.Mutex
	cached   libpodman.Runtime
	cachedAt time.Time
}

func NewService(cfg Config) *Service {
	client, clientErr := libpodman.NewClient(cfg.Host)
	d := &detector{
		config:    cfg,
		client:    client,
		clientErr: clientErr,
	}
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
	if err != nil {
		if !errors.Is(err, common.ErrPodmanNotDetected) {
			slog.DebugContext(ctx, "Podman detection failed", "error", err)
		}
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
	}
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
	if !libpodman.DetectFeatures(rt.Version.Version, false).QuadletsSupported || !rt.Systemd.Availability(ctx).Available {
		return false, nil
	}

	containers, err := rt.Client.ContainerList(ctx, []string{types.PodmanSystemdUnitLabel}, nil)
	if err != nil {
		slog.WarnContext(ctx, "Could not list quadlet-managed containers", "container", containerRef, "error", err)
		return false, nil
	}

	index := slices.IndexFunc(containers, func(c types.PodmanContainer) bool {
		return strings.HasPrefix(c.ID, containerRef) || slices.Contains(c.Names, strings.TrimPrefix(containerRef, "/"))
	})
	if index < 0 {
		return false, nil
	}

	unit := types.QuadletUnit(containers[index].Labels)
	if unit == "" {
		return false, nil
	}
	managed, err := s.quadletManaged(ctx, rt, unit)
	if err != nil {
		return true, fmt.Errorf("verify quadlet unit %s for container %s: %w", unit, containerRef, err)
	}
	if !managed {
		return false, nil
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

	managed, err := s.quadletManaged(ctx, rt, unit)
	if err != nil {
		return fmt.Errorf("verify quadlet unit %s: %w", unit, err)
	}
	if managed {
		return fmt.Errorf("systemd unit %s: %w", unit, common.ErrContainerQuadletManaged)
	}
	return nil
}

func (s *Service) quadletManaged(ctx context.Context, rt libpodman.Runtime, unit string) (bool, error) {
	if !libpodman.DetectFeatures(rt.Version.Version, false).QuadletsSupported {
		return true, nil
	}

	installed, err := s.quadlets.List(ctx, nil)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(installed, func(quadlet types.QuadletDetail) bool {
		return quadlet.UnitName == unit
	}), nil
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
		if startErr := rt.Systemd.Start(ctx, autoUpdateUnit); startErr != nil {
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
			Errors:  kit.Ternary(unit.ActiveState == "failed", []string{autoUpdateUnit + " failed: " + unit.SubState}, []string{}),
			Unit:    &unit,
		}, nil
	default:
		return types.PodmanAutoUpdateResult{}, common.Classify(common.ErrConflict, fmt.Errorf("podman auto-update is unavailable: %s", cmp.Or(availability.Reason, "systemd is not reachable")))
	}
}

func (s *detector) detect(ctx context.Context) (libpodman.Runtime, error) {
	s.mu.Lock()
	cached, fresh := s.cached, time.Since(s.cachedAt) < detectionTTL
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
	if s.clientErr != nil {
		return libpodman.Runtime{}, s.clientErr
	}

	version, err := s.client.Version(ctx)
	if err != nil {
		return libpodman.Runtime{}, err
	}
	info, err := s.client.Info(ctx)
	if err != nil {
		return libpodman.Runtime{}, err
	}

	rt := libpodman.Runtime{
		Client:   s.client,
		Version:  version,
		Rootless: info.Host.Security.Rootless,
		Systemd:  s.config.NewSystemd(info.Host.Security.Rootless),
	}
	s.mu.Lock()
	s.cached, s.cachedAt = rt, time.Now()
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
