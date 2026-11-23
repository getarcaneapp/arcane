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

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/updater"
	"kit.getarcane.app/updater/pkg/utils/tagpolicy"
	"kit.getarcane.app/updater/refs"

	"getarcane.dev/arcane/v3/internal/docker"
	registryInternal "getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/imageref"
	"getarcane.dev/arcane/v3/pkg/registry"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type (
	// Service evaluates container tag policies against registry listings.
	Service struct {
		registry *registryInternal.ContainerRegistryService
		docker   *docker.DockerClientService
		settings *settings.SettingsService
		limiter  *registry.RegistryRateLimiter
	}

	// CheckRequest supplies the container inventory, filter, credentials, and callbacks for a tag check pass.
	CheckRequest struct {
		Wanted             map[string]bool
		SeedDigests        map[string]string
		ClearDigestTracked func(context.Context, []string) error
		Save               func(context.Context, container.Summary, *types.ImageUpdateResponse) error
		Listed             []container.Summary
		Credentials        []types.ContainerRegistryCredential
	}

	// tagRegistry shares listings and digests per scan so replicas cost one request and scan credentials skip service caches.
	tagRegistry struct {
		service      *registryInternal.ContainerRegistryService
		docker       *docker.DockerClientService
		dockerClient *scanDockerClient
		settings     *settings.SettingsService
		limiter      *registry.RegistryRateLimiter
		tags         *scanRegistryMemo[[]string]
		digests      *scanRegistryMemo[string]
		credentials  []types.ContainerRegistryCredential
	}

	// scanDockerClient shares one timeout-bounded Docker client across a tag scan.
	scanDockerClient struct {
		client *client.Client
		mu     sync.Mutex
	}

	// scanRegistryMemo memoizes successful lookups for one scan and coalesces concurrent misses per key.
	scanRegistryMemo[V any] struct {
		flight      singleflight.Group
		values      map[string]V
		failures    map[string]error
		keepFailure func(error) bool
		mu          sync.Mutex
	}
)

const (
	tagCheckWorkers = 10
)

func NewService(
	registryService *registryInternal.ContainerRegistryService,
	dockerService *docker.DockerClientService,
	settingsService *settings.SettingsService,
	limiter *registry.RegistryRateLimiter,
) *Service {
	return &Service{
		registry: registryService,
		docker:   dockerService,
		settings: settingsService,
		limiter:  limiter,
	}
}

// Check evaluates each wanted container's tag policy and saves each result as soon as it finishes.
func (s *Service) Check(ctx context.Context, req CheckRequest) (map[string]*types.ImageUpdateResponse, error) {
	candidates := make([]container.Summary, 0, len(req.Listed))
	var staleDigestContainerIDs []string
	for _, cnt := range req.Listed {
		if !req.Wanted[refs.NormalizeImageUpdateRef(cnt.Image)] || imageref.IsUpdateCheckDisabled(cnt.Labels) {
			continue
		}
		tagPolicy, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
		if policyErr == nil && tagPolicy.Strategy == "digest" {
			staleDigestContainerIDs = append(staleDigestContainerIDs, cnt.ID)
			continue
		}
		candidates = append(candidates, cnt)
	}
	if clearErr := req.ClearDigestTracked(ctx, staleDigestContainerIDs); clearErr != nil {
		return map[string]*types.ImageUpdateResponse{}, clearErr
	}
	if len(candidates) == 0 {
		return map[string]*types.ImageUpdateResponse{}, nil
	}

	seededDigests := make(map[string]string, len(req.SeedDigests))
	for imageRef, digestValue := range req.SeedDigests {
		if digestValue == "" {
			continue
		}
		if parsed, parseErr := refs.NormalizeReference(imageRef); parseErr == nil {
			seededDigests[parsed.NormalizedRef] = digestValue
		}
	}

	// Read-only: only the update-check label opts a container out, so no Settings provider is wired.
	adapter := tagRegistry{
		service:      s.registry,
		credentials:  req.Credentials,
		docker:       s.docker,
		dockerClient: &scanDockerClient{},
		settings:     s.settings,
		limiter:      s.limiter,
		tags: &scanRegistryMemo[[]string]{
			values:      map[string][]string{},
			failures:    map[string]error{},
			keepFailure: func(err error) bool { return registryInternal.IsRateLimitErrorString(err.Error()) },
		},
		digests: &scanRegistryMemo[string]{
			values:      seededDigests,
			failures:    map[string]error{},
			keepFailure: func(error) bool { return false },
		},
	}
	defer adapter.dockerClient.close(ctx)
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

	// Saves are serialized on the group context so a cancelled scan keeps finished results without overwriting them.
	checks := make([]*types.ImageUpdateResponse, len(candidates))
	var saveMu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(tagCheckWorkers)
	for i, cnt := range candidates {
		g.Go(func() (workerErr error) {
			defer concurrency.RecoverToError(&workerErr, "container tag check worker", "containerId", cnt.ID)
			checks[i] = s.checkContainerTag(groupCtx, engine, adapter, cnt)
			saveMu.Lock()
			defer saveMu.Unlock()
			saveErr := req.Save(groupCtx, cnt, checks[i])
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
	parsed, err := refs.NormalizeReference(imageRef)
	fetch := func() ([]string, error) {
		registryHost := kit.Ternary(err == nil, parsed.RegistryHost, "")
		if acquireErr := r.limiter.Acquire(ctx, registryHost); acquireErr != nil {
			return nil, acquireErr
		}
		defer r.limiter.Release(registryHost)
		listCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(r.settings.GetSettingsConfig().RegistryTagTimeout.AsInt(), timeouts.DefaultRegistryTags))
		defer cancel()
		return r.service.ListImageTags(listCtx, imageRef, r.credentials)
	}
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
	parsed, err := refs.NormalizeReference(imageRef)
	fetch := func() (string, error) {
		registryHost := kit.Ternary(err == nil, parsed.RegistryHost, "")
		if acquireErr := r.limiter.Acquire(ctx, registryHost); acquireErr != nil {
			return "", acquireErr
		}
		defer r.limiter.Release(registryHost)
		digestCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(r.settings.GetSettingsConfig().RegistryTimeout.AsInt(), timeouts.DefaultRegistry))
		defer cancel()
		result, inspectErr := r.service.InspectImageDigest(digestCtx, imageRef, r.credentials)
		if inspectErr != nil {
			return "", inspectErr
		}
		if result == nil {
			return "", errors.New("registry returned no digest")
		}
		return result.Digest, nil
	}
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
	if _, err := refs.NormalizeReference(cnt.Image); err != nil {
		return failed(err)
	}
	check, err := engine.CheckContainerUpdate(ctx, cnt.ID)
	if err != nil {
		return failed(err)
	}
	// Resolve a new tag's digest so a tag rewritten during the auto-update delay counts as a new target.
	targetDigest := func() string {
		if !check.UpdateAvailable || check.TargetDigest != "" {
			return check.TargetDigest
		}
		resolved, resolveErr := digests.ImageDigest(ctx, check.TargetRef)
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
	r.dockerClient.mu.Lock()
	defer r.dockerClient.mu.Unlock()
	if r.dockerClient.client != nil {
		return r.dockerClient.client, nil
	}
	base, err := r.docker.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	timeout := timeouts.GetDuration(r.settings.GetSettingsConfig().DockerAPITimeout.AsInt(), timeouts.DefaultDockerAPI)
	opts := []client.Opt{
		client.WithHost(base.DaemonHost()),
		client.WithTimeout(timeout),
	}
	if version := base.ClientVersion(); version != "" {
		opts = append(opts, client.WithAPIVersion(version))
	}
	cli, err := client.New(opts...)
	if err != nil {
		return nil, err
	}
	r.dockerClient.client = cli
	return cli, nil
}

func (c *scanDockerClient) close(ctx context.Context) {
	c.mu.Lock()
	cli := c.client
	c.client = nil
	c.mu.Unlock()
	if cli == nil {
		return
	}
	if err := cli.Close(); err != nil {
		slog.WarnContext(ctx, "close tag check docker client", "error", err)
	}
}

// AttachContainerUpdates binds container results to image results by normalized reference, snapshotting the image result first.
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
				CurrentCreated:   result.CurrentCreated,
				LatestCreated:    result.LatestCreated,
				Error:            result.Error,
				BlockedReason:    result.BlockedReason,
				AuthMethod:       result.AuthMethod,
				AuthUsername:     result.AuthUsername,
				AuthRegistry:     result.AuthRegistry,
				ResponseTimeMs:   result.ResponseTimeMs,
				HasUpdate:        result.HasUpdate,
				Blocked:          result.Blocked,
				UsedCredential:   result.UsedCredential,
				ReleaseURL:       result.ReleaseURL,
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
			CurrentCreated:   promoted.CurrentCreated,
			LatestCreated:    promoted.LatestCreated,
			Error:            result.Error,
			BlockedReason:    promoted.BlockedReason,
			AuthMethod:       result.AuthMethod,
			AuthUsername:     result.AuthUsername,
			AuthRegistry:     result.AuthRegistry,
			ResponseTimeMs:   result.ResponseTimeMs,
			HasUpdate:        promoted.HasUpdate,
			Blocked:          promoted.Blocked,
			UsedCredential:   result.UsedCredential,
			ReleaseURL:       result.ReleaseURL,
		}
	}
	return attached
}
