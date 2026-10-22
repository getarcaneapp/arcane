package tags

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/ratelimit"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/imageref"
)

const (
	// tagCheckWorkers bounds concurrent container policy evaluations per scan; the
	// registry limiter still caps concurrent requests per registry host.
	tagCheckWorkers = 10
)

// Service evaluates container tag policies against registry listings.
type Service struct {
	registry *registry.ContainerRegistryService
	docker   *docker.DockerClientService
	settings *settings.SettingsService
	limiter  *ratelimit.RegistryRateLimiter
}

// tagRegistry adapts the registry service for one container tag scan.
// Successful tag listings are shared per repository and digest lookups per
// reference so replicas of the same image cost one registry request, and the
// scan's manager-provided credentials never leak into the service caches.
type tagRegistry struct {
	service     *registry.ContainerRegistryService
	credentials []types.ContainerRegistryCredential
	docker      *docker.DockerClientService
	settings    *settings.SettingsService
	tags        *scanRegistryMemo[[]string]
	digests     *scanRegistryMemo[string]
}

// scanRegistryMemo memoizes successful registry lookups for one scan
// and coalesces concurrent misses for the same key into a single request.
type scanRegistryMemo[V any] struct {
	flight      singleflight.Group
	mu          sync.Mutex
	values      map[string]V
	failures    map[string]error
	keepFailure func(error) bool
}

func NewService(
	registryService *registry.ContainerRegistryService,
	dockerService *docker.DockerClientService,
	settingsService *settings.SettingsService,
	limiter *ratelimit.RegistryRateLimiter,
) *Service {
	return &Service{
		registry: registryService,
		docker:   dockerService,
		settings: settingsService,
		limiter:  limiter,
	}
}

// Check evaluates the tag policy of each listed container running a wanted
// image. Digest-tracked containers are handed to clearDigestTracked and each
// finished check is handed to save as soon as it completes.
func (s *Service) Check(
	ctx context.Context,
	listed []container.Summary,
	wanted map[string]bool,
	credentials []types.ContainerRegistryCredential,
	clearDigestTracked func(context.Context, []string) error,
	save func(context.Context, container.Summary, *types.ImageUpdateResponse) error,
) (map[string]*types.ImageUpdateResponse, error) {
	// This engine only reads. Containers excluded from automatic installation
	// (updater label or UI exclusion) are still checked; only the update-check
	// label opts a container out of monitoring, so no Settings provider is wired.
	adapter := tagRegistry{
		service:     s.registry,
		credentials: credentials,
		docker:      s.docker,
		settings:    s.settings,
		tags: &scanRegistryMemo[[]string]{
			values:      map[string][]string{},
			failures:    map[string]error{},
			keepFailure: func(err error) bool { return registry.IsRateLimitErrorString(err.Error()) },
		},
		digests: &scanRegistryMemo[string]{
			values:      map[string]string{},
			failures:    map[string]error{},
			keepFailure: func(error) bool { return false },
		},
	}
	engine, err := updater.New(updater.Config{
		RegistryTagLister:      adapter,
		RegistryDigestResolver: adapter,
		DockerClientProvider:   adapter,
		LabelPolicy:            updater.LabelPolicy{IsUpdateDisabledFunc: imageref.IsUpdateCheckDisabled},
	})
	if err != nil {
		return map[string]*types.ImageUpdateResponse{}, err
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil {
			slog.WarnContext(ctx, "close tag checker", "error", closeErr)
		}
	}()
	candidates := make([]container.Summary, 0, len(listed))
	var staleDigestContainerIDs []string
	for _, cnt := range listed {
		if !wanted[refs.NormalizeImageUpdateRef(cnt.Image)] || imageref.IsUpdateCheckDisabled(cnt.Labels) {
			continue
		}
		tagPolicy, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
		if policyErr == nil && tagPolicy.Strategy == "digest" {
			staleDigestContainerIDs = append(staleDigestContainerIDs, cnt.ID)
			continue
		}
		candidates = append(candidates, cnt)
	}
	if clearErr := clearDigestTracked(ctx, staleDigestContainerIDs); clearErr != nil {
		return map[string]*types.ImageUpdateResponse{}, clearErr
	}

	// Policies are evaluated concurrently; the shared adapter dedupes registry
	// requests across replicas and the registry limiter caps them per host.
	// Each result is persisted as soon as its check completes, so a scan that
	// reaches its deadline keeps every check that finished before it. Saves are
	// serialized and run on the group context: once the scan is cancelled the
	// remaining writes fail instead of replacing stored results with
	// cancellation errors.
	checks := make([]*types.ImageUpdateResponse, len(candidates))
	var saveMu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(tagCheckWorkers)
	for i, cnt := range candidates {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "container tag check worker", "containerId", cnt.ID)
			checks[i] = s.checkContainerTag(groupCtx, engine, adapter, cnt)
			saveMu.Lock()
			defer saveMu.Unlock()
			saveErr := save(groupCtx, cnt, checks[i])
			if saveErr == nil || groupCtx.Err() != nil {
				return saveErr
			}
			checks[i].Error = errors.Join(
				kit.Ternary(checks[i].Error != "", errors.New(checks[i].Error), nil),
				fmt.Errorf("save tag check result: %w", saveErr),
			).Error()
			return nil
		})
	}
	waitErr := g.Wait()
	results := make(map[string]*types.ImageUpdateResponse, len(candidates))
	for i, cnt := range candidates {
		if checks[i] != nil {
			results[cnt.ID] = checks[i]
		}
	}
	return results, waitErr
}

func (m *scanRegistryMemo[V]) do(key string, fetch func() (V, error)) (V, error) {
	m.mu.Lock()
	value, ok := m.values[key]
	failure := m.failures[key]
	m.mu.Unlock()
	if ok {
		return value, nil
	}
	if failure != nil {
		var zero V
		return zero, failure
	}
	shared, err, _ := m.flight.Do(key, func() (any, error) {
		fetched, fetchErr := fetch()
		if fetchErr != nil {
			if m.keepFailure(fetchErr) {
				m.mu.Lock()
				m.failures[key] = fetchErr
				m.mu.Unlock()
			}
			return nil, fetchErr
		}
		m.mu.Lock()
		m.values[key] = fetched
		m.mu.Unlock()
		return fetched, nil
	})
	var zero V
	if err != nil {
		return zero, err
	}
	value, ok = shared.(V)
	if !ok {
		return zero, errors.New("registry lookup returned an unexpected result type")
	}
	return value, nil
}

func (r tagRegistry) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	if r.service == nil {
		return nil, errors.New("registry service unavailable")
	}
	fetch := func() ([]string, error) { return r.service.ListImageTags(ctx, imageRef, r.credentials) }
	parsed, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return fetch()
	}
	tags, err := r.tags.do(parsed.RegistryHost+"/"+parsed.Repository, fetch)
	if err != nil {
		return nil, err
	}
	// Callers may sort or filter the listing in place.
	return slices.Clone(tags), nil
}

func (r tagRegistry) ImageDigest(ctx context.Context, imageRef string) (string, error) {
	if r.service == nil {
		return "", errors.New("registry service unavailable")
	}
	fetch := func() (string, error) {
		digestCtx, cancel := context.WithTimeout(ctx, func() time.Duration {
			if r.settings == nil {
				return timeouts.DefaultRegistry
			}
			return timeouts.GetDuration(r.settings.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry)
		}())
		defer cancel()
		result, err := r.service.InspectImageDigest(digestCtx, imageRef, r.credentials)
		if err != nil {
			return "", err
		}
		if result == nil {
			return "", errors.New("registry returned no digest")
		}
		return result.Digest, nil
	}
	parsed, err := refs.NormalizeReference(imageRef)
	if err != nil {
		return fetch()
	}
	return r.digests.do(parsed.NormalizedRef, fetch)
}

func (s *Service) checkContainerTag(ctx context.Context, engine *updater.Service, digests tagRegistry, cnt container.Summary) *types.ImageUpdateResponse {
	start := time.Now()
	failed := func(err error) *types.ImageUpdateResponse {
		return &types.ImageUpdateResponse{
			ImageRef:       cnt.Image,
			CheckTime:      start.UTC(),
			UpdateType:     string(updater.UpdateTypeTag),
			Error:          err.Error(),
			ResponseTimeMs: int(time.Since(start).Milliseconds()),
		}
	}
	parsed, err := refs.NormalizeReference(cnt.Image)
	if err != nil {
		return failed(err)
	}
	if acquireErr := s.limiter.Acquire(ctx, parsed.RegistryHost); acquireErr != nil {
		return failed(acquireErr)
	}
	defer s.limiter.Release(parsed.RegistryHost)
	checkCtx, cancel := context.WithTimeout(ctx, func() time.Duration {
		if s.settings == nil {
			return timeouts.DefaultRegistryTags
		}
		return timeouts.GetDuration(s.settings.GetSettingsConfig().RegistryTagTimeout.AsInt(), timeouts.DefaultRegistryTags)
	}())
	defer cancel()
	check, err := engine.CheckContainerUpdate(checkCtx, cnt.ID)
	if err != nil {
		return failed(err)
	}
	// A newer tag has no digest yet; resolve it so a tag rewritten during the
	// auto-update delay counts as a new target. An unresolved digest only
	// restarts that delay, so the update itself is still reported.
	targetDigest := func() string {
		if !check.UpdateAvailable || check.TargetDigest != "" {
			return check.TargetDigest
		}
		resolved, resolveErr := digests.ImageDigest(checkCtx, check.TargetRef)
		if resolveErr != nil {
			slog.DebugContext(ctx, "Could not resolve tag update digest", "containerId", cnt.ID, "targetRef", check.TargetRef, "error", resolveErr)
		}
		return resolved
	}()
	latestVersion, targetErrMessage := func() (string, string) {
		if check.TargetRef == "" {
			return "", ""
		}
		target, normalizeReferenceErr := refs.NormalizeReference(check.TargetRef)
		if normalizeReferenceErr != nil {
			return "", normalizeReferenceErr.Error()
		}
		return target.Tag, ""
	}()
	return &types.ImageUpdateResponse{
		ImageRef:       cnt.Image,
		CheckTime:      start.UTC(),
		UpdateType:     cmp.Or(check.UpdateType, string(updater.UpdateTypeTag)),
		CurrentVersion: check.CurrentVersion,
		LatestVersion:  latestVersion,
		CurrentDigest:  check.CurrentDigest,
		LatestDigest:   targetDigest,
		Error:          targetErrMessage,
		ResponseTimeMs: int(time.Since(start).Milliseconds()),
		HasUpdate:      check.UpdateAvailable && targetErrMessage == "",
	}
}

func (r tagRegistry) DockerClient(ctx context.Context) (*client.Client, error) {
	if r.docker == nil {
		return nil, errors.New("docker service unavailable")
	}
	return r.docker.GetClient(ctx)
}

// AttachContainerUpdates binds each container's policy result to the
// image result sharing its normalized reference. The image-only snapshot is
// taken before the first container result is attached so it stays untouched.
// A container candidate promotes the image result to its version update when
// the image itself has none.
func AttachContainerUpdates(results, containerUpdates map[string]*types.ImageUpdateResponse) map[string]*types.ImageUpdateResponse {
	containerIDsByRef := make(map[string][]string, len(containerUpdates))
	for id, update := range containerUpdates {
		normalized := refs.NormalizeImageUpdateRef(update.ImageRef)
		containerIDsByRef[normalized] = append(containerIDsByRef[normalized], id)
	}
	for _, ids := range containerIDsByRef {
		slices.Sort(ids)
	}
	attached := make(map[string]*types.ImageUpdateResponse, len(results))
	for imageRef, result := range results {
		ids := containerIDsByRef[refs.NormalizeImageUpdateRef(imageRef)]
		if len(ids) == 0 {
			attached[imageRef] = result
			continue
		}
		scoped := make(map[string]*types.ImageUpdateResponse, len(result.ContainerUpdates)+len(ids))
		maps.Copy(scoped, result.ContainerUpdates)
		for _, id := range ids {
			scoped[id] = containerUpdates[id]
		}
		promoted := func() *types.ImageUpdateResponse {
			if index := slices.IndexFunc(ids, func(id string) bool { return containerUpdates[id].HasUpdate }); !result.HasUpdate && index >= 0 {
				return containerUpdates[ids[index]]
			}
			return result
		}()
		attached[imageRef] = &types.ImageUpdateResponse{ //exhaustruct:enforce
			ImageUpdate: cmp.Or(result.ImageUpdate, &types.ImageUpdateResponse{ //exhaustruct:enforce
				ImageUpdate:      nil,
				ImageRef:         result.ImageRef,
				ContainerUpdates: nil,
				CheckTime:        result.CheckTime,
				ActivityID:       result.ActivityID,
				UpdateType:       result.UpdateType,
				CurrentVersion:   result.CurrentVersion,
				LatestVersion:    result.LatestVersion,
				CurrentDigest:    result.CurrentDigest,
				LatestDigest:     result.LatestDigest,
				Error:            result.Error,
				AuthMethod:       result.AuthMethod,
				AuthUsername:     result.AuthUsername,
				AuthRegistry:     result.AuthRegistry,
				ResponseTimeMs:   result.ResponseTimeMs,
				HasUpdate:        result.HasUpdate,
				UsedCredential:   result.UsedCredential,
			}),
			ImageRef:         result.ImageRef,
			ContainerUpdates: scoped,
			CheckTime:        result.CheckTime,
			ActivityID:       result.ActivityID,
			UpdateType:       promoted.UpdateType,
			CurrentVersion:   promoted.CurrentVersion,
			LatestVersion:    promoted.LatestVersion,
			CurrentDigest:    result.CurrentDigest,
			LatestDigest:     result.LatestDigest,
			Error:            result.Error,
			AuthMethod:       result.AuthMethod,
			AuthUsername:     result.AuthUsername,
			AuthRegistry:     result.AuthRegistry,
			ResponseTimeMs:   result.ResponseTimeMs,
			HasUpdate:        promoted.HasUpdate,
			UsedCredential:   result.UsedCredential,
		}
	}
	return attached
}
