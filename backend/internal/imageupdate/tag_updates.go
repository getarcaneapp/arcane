package imageupdate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imageupdatetypes "github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/updater"

	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"

	"gorm.io/gorm"
)

type tagRegistryInternal struct {
	service     *registry.ContainerRegistryService
	credentials []containerregistry.Credential
	docker      *docker.DockerClientService
	settings    *settings.SettingsService
}

func (r tagRegistryInternal) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	if r.service == nil {
		return nil, errors.New("registry service unavailable")
	}
	return r.service.ListImageTags(ctx, imageRef, r.credentials)
}

func (r tagRegistryInternal) ImageDigest(ctx context.Context, imageRef string) (string, error) {
	if r.service == nil {
		return "", errors.New("registry service unavailable")
	}
	result, err := r.service.InspectImageDigest(ctx, imageRef, r.credentials)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", errors.New("registry returned no digest")
	}
	return result.Digest, nil
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

	adapter := tagRegistryInternal{service: s.registryService, credentials: credentials, docker: s.dockerService, settings: s.settingsService}
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
	for _, cnt := range listed.Items {
		if !wanted[refs.NormalizeImageUpdateRef(cnt.Image)] {
			continue
		}
		tagPolicy, policyErr := tagpolicy.Resolve(cnt.Image, policy.TagPolicy(cnt.Labels))
		if policyErr == nil && tagPolicy.Strategy == "digest" {
			if s.db != nil {
				if err := s.db.WithContext(ctx).Where("container_id = ?", cnt.ID).Delete(&ImageUpdateRecord{}).Error; err != nil {
					return results, err
				}
			}
			continue
		}
		result := s.checkContainerTagInternal(ctx, engine, cnt)
		results[cnt.ID] = result
		if err := s.saveContainerTagResultInternal(ctx, cnt, result); err != nil {
			return results, err
		}
	}
	return results, nil
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
	checkCtx, cancel := s.registryContextInternal(ctx)
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
