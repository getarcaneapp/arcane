package imageupdate

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imageupdatetypes "github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/updater"

	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"gorm.io/gorm"
)

// tagCheckWorkers bounds concurrent container policy evaluations per scan; the
// registry limiter still caps concurrent requests per registry host.
const tagCheckWorkers = 10

// staleDigestRecordDeleteChunk bounds the IN list used to clear container
// records whose policy switched to digest tracking.
const staleDigestRecordDeleteChunk = 500

// tagRegistryInternal adapts the registry service for one container tag scan.
// Successful tag listings are shared per repository and digest lookups per
// reference so replicas of the same image cost one registry request, and the
// scan's manager-provided credentials never leak into the service caches.
type tagRegistryInternal struct {
	service     *registry.ContainerRegistryService
	credentials []containerregistry.Credential
	docker      *docker.DockerClientService
	settings    *settings.SettingsService
	tags        *scanRegistryMemoInternal[[]string]
	digests     *scanRegistryMemoInternal[string]
}

func newTagRegistryInternal(service *registry.ContainerRegistryService, credentials []containerregistry.Credential, dockerService *docker.DockerClientService, settingsService *settings.SettingsService) tagRegistryInternal {
	return tagRegistryInternal{
		service:     service,
		credentials: credentials,
		docker:      dockerService,
		settings:    settingsService,
		tags:        newScanRegistryMemoInternal[[]string](),
		digests:     newScanRegistryMemoInternal[string](),
	}
}

// scanRegistryMemoInternal memoizes successful registry lookups for one scan
// and coalesces concurrent misses for the same key into a single request.
// Failures are not stored: each waiting caller receives the shared error and
// the next caller retries, so a transient fault never sticks for the scan.
type scanRegistryMemoInternal[V any] struct {
	flight singleflight.Group
	mu     sync.Mutex
	values map[string]V
}

func newScanRegistryMemoInternal[V any]() *scanRegistryMemoInternal[V] {
	return &scanRegistryMemoInternal[V]{values: map[string]V{}}
}

func (m *scanRegistryMemoInternal[V]) doInternal(key string, fetch func() (V, error)) (V, error) {
	m.mu.Lock()
	value, ok := m.values[key]
	m.mu.Unlock()
	if ok {
		return value, nil
	}
	shared, err, _ := m.flight.Do(key, func() (any, error) {
		fetched, fetchErr := fetch()
		if fetchErr != nil {
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

func (r tagRegistryInternal) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	if r.service == nil {
		return nil, errors.New("registry service unavailable")
	}
	fetch := func() ([]string, error) { return r.service.ListImageTags(ctx, imageRef, r.credentials) }
	parsed, err := refs.NormalizeReference(imageRef)
	if err != nil || r.tags == nil {
		return fetch()
	}
	tags, err := r.tags.doInternal(parsed.RegistryHost+"/"+parsed.Repository, fetch)
	if err != nil {
		return nil, err
	}
	// Callers may sort or filter the listing in place.
	return slices.Clone(tags), nil
}

func (r tagRegistryInternal) ImageDigest(ctx context.Context, imageRef string) (string, error) {
	if r.service == nil {
		return "", errors.New("registry service unavailable")
	}
	fetch := func() (string, error) {
		timeoutSeconds := 0
		if r.settings != nil {
			timeoutSeconds = r.settings.GetSettingsConfig().RegistryTimeout.AsInt()
		}
		digestCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultRegistry))
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
	if err != nil || r.digests == nil {
		return fetch()
	}
	return r.digests.doInternal(parsed.NormalizedRef, fetch)
}

func (s *ImageUpdateService) checkContainerTagUpdatesInternal(ctx context.Context, imageRefs []string, credentials []containerregistry.Credential) (map[string]*imageupdatetypes.Response, error) {
	results := map[string]*imageupdatetypes.Response{}
	wanted := map[string]bool{}
	for _, imageRef := range imageRefs {
		if normalized := refs.NormalizeImageUpdateRef(imageRef); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return results, nil
	}
	dockerClient, err := s.dockerClientInternal(ctx)
	if err != nil {
		return results, err
	}
	apiCtx, cancel := s.dockerAPIContextInternal(ctx)
	listed, err := dockerClient.ContainerList(apiCtx, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return results, errors.WrapIf(err, "list containers for tag update checks")
	}

	adapter := newTagRegistryInternal(s.registryService, credentials, s.dockerService, s.settingsService)
	engine, err := updater.New(updater.Config{RegistryTagLister: adapter, RegistryDigestResolver: adapter, DockerClientProvider: adapter, Settings: adapter})
	if err != nil {
		return results, err
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil {
			slog.WarnContext(ctx, "close tag checker", "error", closeErr)
		}
	}()
	policy := updater.DefaultLabelPolicy()
	candidates := make([]container.Summary, 0, len(listed.Items))
	var staleDigestContainerIDs []string
	for _, cnt := range listed.Items {
		if !wanted[refs.NormalizeImageUpdateRef(cnt.Image)] {
			continue
		}
		tagPolicy, policyErr := tagpolicy.Resolve(cnt.Image, policy.TagPolicy(cnt.Labels))
		if policyErr == nil && tagPolicy.Strategy == "digest" {
			staleDigestContainerIDs = append(staleDigestContainerIDs, cnt.ID)
			continue
		}
		candidates = append(candidates, cnt)
	}
	if err := s.deleteContainerUpdateRecordsInternal(ctx, staleDigestContainerIDs); err != nil {
		return results, err
	}

	// Policies are evaluated concurrently; the shared adapter dedupes registry
	// requests across replicas and the registry limiter caps them per host.
	// Each result is persisted as soon as its check completes, so a scan that
	// reaches its deadline keeps every check that finished before it. Saves are
	// serialized and run on the group context: once the scan is cancelled the
	// remaining writes fail instead of replacing stored results with
	// cancellation errors.
	checks := make([]*imageupdatetypes.Response, len(candidates))
	var saveMu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(tagCheckWorkers)
	for i, cnt := range candidates {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "container tag check worker", "containerId", cnt.ID)
			checks[i] = s.checkContainerTagInternal(groupCtx, engine, cnt)
			saveMu.Lock()
			defer saveMu.Unlock()
			return s.saveContainerTagResultInternal(groupCtx, cnt, checks[i])
		})
	}
	waitErr := g.Wait()
	for i, cnt := range candidates {
		if checks[i] != nil {
			results[cnt.ID] = checks[i]
		}
	}
	return results, waitErr
}

// deleteContainerUpdateRecordsInternal clears the stored results of containers
// whose policy no longer tracks tags, in bounded IN-list chunks.
func (s *ImageUpdateService) deleteContainerUpdateRecordsInternal(ctx context.Context, containerIDs []string) error {
	if s.db == nil {
		return nil
	}
	for chunk := range slices.Chunk(containerIDs, staleDigestRecordDeleteChunk) {
		if err := s.db.WithContext(ctx).Where("container_id IN ?", chunk).Delete(&ImageUpdateRecord{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ImageUpdateService) checkContainerTagInternal(ctx context.Context, engine *updater.Service, cnt container.Summary) *imageupdatetypes.Response {
	start := time.Now()
	result := &imageupdatetypes.Response{CheckTime: time.Now().UTC(), UpdateType: UpdateTypeTag, ImageRef: cnt.Image}
	defer func() { result.ResponseTimeMs = int(time.Since(start).Milliseconds()) }()
	parsed, err := refs.NormalizeReference(cnt.Image)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if s.registryLimiter != nil {
		if err := s.registryLimiter.Acquire(ctx, parsed.RegistryHost); err != nil {
			result.Error = err.Error()
			return result
		}
		defer s.registryLimiter.Release(parsed.RegistryHost)
	}
	timeoutSeconds := 0
	if s.settingsService != nil {
		timeoutSeconds = s.settingsService.GetSettingsConfig().RegistryTagTimeout.AsInt()
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeouts.GetDuration(timeoutSeconds, timeouts.DefaultRegistryTags))
	defer cancel()
	check, err := engine.CheckContainerUpdate(checkCtx, cnt.ID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.HasUpdate = check.UpdateAvailable
	result.UpdateType = check.UpdateType
	if result.UpdateType == "" {
		result.UpdateType = UpdateTypeTag
	}
	result.CurrentVersion = check.CurrentVersion
	result.CurrentDigest = check.CurrentDigest
	result.LatestDigest = check.TargetDigest
	if check.TargetRef != "" {
		target, err := refs.NormalizeReference(check.TargetRef)
		if err != nil {
			result.Error = err.Error()
			result.HasUpdate = false
			return result
		}
		result.LatestVersion = target.Tag
	}
	return result
}

func (r tagRegistryInternal) DockerClient(ctx context.Context) (*client.Client, error) {
	if r.docker == nil {
		return nil, errors.New("docker service unavailable")
	}
	return r.docker.GetClient(ctx)
}

func (r tagRegistryInternal) ExcludedContainers(ctx context.Context) ([]string, error) {
	if r.settings == nil {
		return nil, nil
	}
	return strings.Split(r.settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""), ","), nil
}

func (s *ImageUpdateService) saveContainerTagResultInternal(ctx context.Context, cnt container.Summary, result *imageupdatetypes.Response) error {
	if s.db == nil {
		return nil
	}
	parsed, err := refs.NormalizeReference(cnt.Image)
	if err != nil {
		return err
	}
	id := "container::" + cnt.ID
	policyKey := imageref.UpdatePolicyKey(cnt.Image, cnt.Labels)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A previous good result must not survive a rate limit under a different policy.
		if err := tx.Where("id = ? AND policy_key <> ?", id, policyKey).Delete(&ImageUpdateRecord{}).Error; err != nil {
			return err
		}
		if err := savePreparedUpdateResultWithTxInternal(tx, id, parsed.RegistryHost+"/"+parsed.Repository, parsed.Tag, result); err != nil {
			return err
		}
		return tx.Model(&ImageUpdateRecord{}).Where("id = ?", id).Updates(map[string]any{"container_id": cnt.ID, "image_id": cnt.ImageID, "policy_key": policyKey}).Error
	})
}

func attachContainerUpdatesInternal(results map[string]*imageupdatetypes.Response, containerUpdates map[string]*imageupdatetypes.Response) {
	for imageRef, result := range results {
		for id, update := range containerUpdates {
			if refs.NormalizeImageUpdateRef(update.ImageRef) != refs.NormalizeImageUpdateRef(imageRef) {
				continue
			}
			if result.ImageUpdate == nil {
				imageOnly := *result
				imageOnly.ContainerUpdates = nil
				result.ImageUpdate = &imageOnly
			}
			if result.ContainerUpdates == nil {
				result.ContainerUpdates = map[string]*imageupdatetypes.Response{}
			}
			result.ContainerUpdates[id] = update
			if update.HasUpdate {
				result.HasUpdate = true
			}
		}
	}
}

func containerUpdateNotificationKeyInternal(record *ImageUpdateRecord) string {
	imageRef := fmt.Sprintf("%s:%s", record.Repository, record.Tag)
	if record.ContainerID != "" {
		return imageRef + " (container " + record.ContainerID + ")"
	}
	return imageRef
}
