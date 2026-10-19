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

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/podman/children/kube"
	"github.com/getarcaneapp/arcane/backend/v2/internal/podman/children/pods"
	"github.com/getarcaneapp/arcane/backend/v2/internal/podman/children/quadlets"
	"github.com/getarcaneapp/arcane/backend/v2/internal/podman/children/secrets"
	libpodman "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
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
	config    Config
	client    *libpodman.Client
	clientErr error

	mu       sync.Mutex
	cached   detection
	cachedAt time.Time

	pods     *pods.Service
	quadlets *quadlets.Service
	kube     *kube.Service
	secrets  *secrets.Service
}

type detection struct {
	detected bool
	version  podmantypes.Version
	rootless bool
	systemd  libpodman.Systemd
}

func NewService(cfg Config) *Service {
	client, clientErr := libpodman.NewClient(cfg.Host)
	s := &Service{
		config:    cfg,
		client:    client,
		clientErr: clientErr,
	}
	s.pods = pods.NewService(s.requireClient)
	s.quadlets = quadlets.NewService(s.quadletRuntime)
	s.kube = kube.NewService(s.requireClient)
	s.secrets = secrets.NewService(s.requireClient)
	return s
}

func (s *Service) Status(ctx context.Context) podmantypes.Status {
	d, err := s.detect(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Podman detection failed", "error", err)
	}
	if !d.detected {
		return podmantypes.Status{AutoUpdateMode: podmantypes.AutoUpdateModeUnavailable}
	}

	availability := d.systemd.Availability(ctx)
	features := libpodman.DetectFeatures(d.version.Version, availability.Available)
	return podmantypes.Status{
		Detected:             true,
		Version:              d.version.Version,
		APIVersion:           d.version.APIVersion,
		Rootless:             d.rootless,
		QuadletsSupported:    features.QuadletsSupported,
		QuadletAppsSupported: features.QuadletAppsSupported,
		AutoUpdateMode:       features.AutoUpdateMode,
		SystemdAvailable:     availability.Available,
		SystemdScope:         availability.Scope,
		SystemdReason:        availability.Reason,
	}
}

func (s *Service) Info(ctx context.Context) (podmantypes.Info, error) {
	client, err := s.requireClient(ctx)
	if err != nil {
		return podmantypes.Info{}, err
	}
	return client.Info(ctx)
}

func (s *Service) PodCount(ctx context.Context) (int, bool) {
	items, err := s.pods.List(ctx)
	if err != nil {
		if !errors.Is(err, common.ErrPodmanNotDetected) {
			slog.DebugContext(ctx, "Skipping Podman pod count", "error", err)
		}
		return 0, false
	}
	return len(items), true
}

func (s *Service) ContainerUnitAction(ctx context.Context, containerRef string, action func(libpodman.Systemd, context.Context, string) error) (bool, error) {
	d, err := s.detect(ctx)
	if err != nil || !d.detected || containerRef == "" {
		return false, nil
	}

	containers, err := s.client.ContainerList(ctx, []string{podmantypes.SystemdUnitLabel}, nil)
	if err != nil {
		slog.WarnContext(ctx, "Could not list quadlet-managed containers", "container", containerRef, "error", err)
		return false, nil
	}

	index := slices.IndexFunc(containers, func(c podmantypes.Container) bool {
		return strings.HasPrefix(c.ID, containerRef) || slices.Contains(c.Names, strings.TrimPrefix(containerRef, "/"))
	})
	if index < 0 {
		return false, nil
	}
	return true, action(d.systemd, ctx, containers[index].Labels[podmantypes.SystemdUnitLabel])
}

func (s *Service) AutoUpdate(ctx context.Context, opts podmantypes.AutoUpdateOptions) (podmantypes.AutoUpdateResult, error) {
	d, err := s.require(ctx)
	if err != nil {
		return podmantypes.AutoUpdateResult{}, err
	}

	availability := d.systemd.Availability(ctx)
	switch libpodman.DetectFeatures(d.version.Version, availability.Available).AutoUpdateMode {
	case podmantypes.AutoUpdateModeREST:
		result, updateErr := s.client.AutoUpdate(ctx, opts)
		if updateErr != nil {
			return podmantypes.AutoUpdateResult{}, updateErr
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
	case podmantypes.AutoUpdateModeSystemd:
		if opts.DryRun {
			return podmantypes.AutoUpdateResult{}, common.Classify(common.ErrBadRequest, errors.New("auto-update dry run requires Podman 6.2 or later"))
		}
		if startErr := d.systemd.Start(ctx, autoUpdateUnit); startErr != nil {
			return podmantypes.AutoUpdateResult{}, startErr
		}
		unit, statusErr := d.systemd.UnitStatus(ctx, autoUpdateUnit)
		for statusErr == nil && unit.ActiveState == "activating" {
			select {
			case <-ctx.Done():
				return podmantypes.AutoUpdateResult{}, ctx.Err()
			case <-time.After(autoUpdatePollInterval):
			}
			unit, statusErr = d.systemd.UnitStatus(ctx, autoUpdateUnit)
		}
		if statusErr != nil {
			return podmantypes.AutoUpdateResult{}, statusErr
		}
		return podmantypes.AutoUpdateResult{
			Mode:    podmantypes.AutoUpdateModeSystemd,
			Reports: []podmantypes.AutoUpdateReport{},
			Errors:  kit.Ternary(unit.ActiveState == "failed", []string{autoUpdateUnit + " failed: " + unit.SubState}, []string{}),
			Unit:    &unit,
		}, nil
	default:
		return podmantypes.AutoUpdateResult{}, common.Classify(common.ErrConflict, fmt.Errorf("podman auto-update is unavailable: %s", cmp.Or(availability.Reason, "systemd is not reachable")))
	}
}

func (s *Service) detect(ctx context.Context) (detection, error) {
	s.mu.Lock()
	cached, fresh := s.cached, time.Since(s.cachedAt) < detectionTTL
	s.mu.Unlock()
	if fresh {
		return cached, nil
	}

	engine, err := s.config.Engine(ctx)
	if err != nil {
		return detection{}, err
	}
	if engine.Name != "podman" {
		return detection{}, nil
	}
	if s.clientErr != nil {
		return detection{}, s.clientErr
	}

	version, err := s.client.Version(ctx)
	if err != nil {
		return detection{}, err
	}
	info, err := s.client.Info(ctx)
	if err != nil {
		return detection{}, err
	}

	d := detection{
		detected: true,
		version:  version,
		rootless: info.Host.Security.Rootless,
		systemd:  s.config.NewSystemd(info.Host.Security.Rootless),
	}
	s.mu.Lock()
	s.cached, s.cachedAt = d, time.Now()
	s.mu.Unlock()
	return d, nil
}

func (s *Service) require(ctx context.Context) (detection, error) {
	d, err := s.detect(ctx)
	if err != nil {
		return detection{}, err
	}
	if !d.detected {
		return detection{}, common.ErrPodmanNotDetected
	}
	return d, nil
}

func (s *Service) requireClient(ctx context.Context) (*libpodman.Client, error) {
	if _, err := s.require(ctx); err != nil {
		return nil, err
	}
	return s.client, nil
}

func (s *Service) quadletRuntime(ctx context.Context) (quadlets.Runtime, error) {
	d, err := s.require(ctx)
	if err != nil {
		return quadlets.Runtime{}, err
	}

	features := libpodman.DetectFeatures(d.version.Version, false)
	if !features.QuadletsSupported {
		return quadlets.Runtime{}, common.Classify(common.ErrBadRequest, fmt.Errorf("quadlet management requires Podman 5.8 or later; this host runs %s", d.version.Version))
	}
	return quadlets.Runtime{
		Client:        s.client,
		Systemd:       d.systemd,
		AppsSupported: features.QuadletAppsSupported,
	}, nil
}
