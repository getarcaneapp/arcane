package updater

import (
	"context"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"gorm.io/gorm"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/moby/moby/client"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"
	updatertypes "go.getarcane.app/updater/types"
)

// ListTags supplies Arcane's registry credentials to the updater's tag checker.
func (s *UpdaterService) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	if s.deps.RegistryDigestResolver == nil {
		return nil, errors.New("registry service unavailable")
	}
	var credentials []containerregistry.Credential
	if s.deps.Projects != nil {
		resolved, err := s.deps.Projects.ResolveRegistryCredentials(ctx)
		if err != nil {
			return nil, err
		}
		credentials = resolved
	}
	return s.deps.RegistryDigestResolver.ListImageTags(ctx, imageRef, credentials)
}

// UpdateServiceImages persists and redeploys the selected Compose service images.
func (s *UpdaterService) UpdateServiceImages(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange) error {
	if s.deps.Projects == nil {
		return errors.New("project service unavailable")
	}
	s.appendAutoUpdateActivityMessageInternal(ctx, activityIDFromContextInternal(ctx), "Updating Compose image references", "Persisting image updates", 50)
	return s.deps.Projects.UpdateProjectServiceImages(ctx, projectID, changes, s.deps.SystemUser)
}

func (s *UpdaterService) scopedPendingRecordsInternal(ctx context.Context, records []imageupdate.ImageUpdateRecord) ([]updater.ImageUpdateRecord, error) {
	var scopedCount int64
	if err := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("container_id <> ?", "").Limit(1).Count(&scopedCount).Error; err != nil {
		return nil, err
	}
	hasScoped := scopedCount > 0
	if !hasScoped {
		out := make([]updater.ImageUpdateRecord, 0, len(records))
		for _, record := range records {
			out = append(out, imageUpdateRecordToModuleInternal(record))
		}
		return out, nil
	}
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return nil, err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return nil, err
	}
	out := make([]updater.ImageUpdateRecord, 0, len(records))
	for _, record := range records {
		converted := imageUpdateRecordToModuleInternal(record)
		if record.ContainerID != "" {
			out = append(out, converted)
			continue
		}
		for _, cnt := range listed.Items {
			resolved, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
			if policyErr != nil || resolved.Strategy == "tag" {
				continue
			}
			if refs.NormalizeImageUpdateRef(cnt.Image) != refs.NormalizeImageUpdateRef(converted.ImageRef()) {
				continue
			}
			converted.ContainerID = cnt.ID
			out = append(out, converted)
		}
	}
	return out, nil
}

func (s *UpdaterService) clearUnscopedRecordInternal(ctx context.Context, record updater.ImageUpdateRecord) error {
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return err
	}
	target, err := dockerClient.ImageInspect(ctx, record.NewImageRef())
	if err != nil {
		return err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return err
	}
	for _, cnt := range listed.Items {
		resolved, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
		if policyErr != nil || resolved.Strategy == "tag" {
			continue
		}
		if refs.NormalizeImageUpdateRef(cnt.Image) == refs.NormalizeImageUpdateRef(record.ImageRef()) && cnt.ImageID != target.ID {
			return nil
		}
	}
	return s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("id = ? AND container_id = ?", record.ID, "").Update("has_update", false).Error
}

// CheckProjectUpdates checks effective Compose service policies without deploying containers.
func (s *UpdaterService) CheckProjectUpdates(ctx context.Context, projectID string) (*projecttypes.UpdateInfo, error) {
	if s.deps.Projects == nil || s.deps.DB == nil || s.engine == nil {
		return nil, errors.New("project update checker unavailable")
	}
	details, err := s.deps.Projects.GetProjectDetails(ctx, projectID, projecttypes.DetailsOptions{IncludeServiceConfigs: true})
	if err != nil {
		return nil, err
	}
	if len(details.Services) == 0 {
		return nil, errors.New("project has no resolved Compose services to check")
	}
	records := make([]imageupdate.ImageUpdateRecord, 0, len(details.Services))
	for _, service := range details.Services {
		if strings.TrimSpace(service.Image) == "" {
			continue
		}
		records = append(records, s.checkProjectServiceInternal(ctx, projectID, service))
	}
	if err := s.deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&imageupdate.ImageUpdateRecord{}).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		return tx.Create(&records).Error
	}); err != nil {
		return nil, errors.WrapIf(err, "save project update checks")
	}
	return project.BuildConfiguredUpdateInfo(projectID, details.Services, nil, records), nil
}

func (s *UpdaterService) checkProjectServiceInternal(ctx context.Context, projectID string, service composetypes.ServiceConfig) imageupdate.ImageUpdateRecord {
	record := imageupdate.ImageUpdateRecord{ID: "project::" + projectID + "::" + service.Name, ProjectID: projectID, ServiceName: service.Name, PolicyKey: imageref.UpdatePolicyKey(service.Image, service.Labels), CheckTime: time.Now().UTC()}
	parsed, err := refs.NormalizeReference(service.Image)
	if err != nil {
		record.LastError = new(err.Error())
		return record
	}
	record.Repository = parsed.RegistryHost + "/" + parsed.Repository
	record.Tag = parsed.Tag
	record.CurrentVersion = parsed.Tag
	if service.Build != nil {
		record.UpdateType = imageupdate.UpdateTypeLocal
		return record
	}
	policy := updater.DefaultLabelPolicy()
	if policy.IsUpdateDisabled(service.Labels) {
		return record
	}
	check, err := s.engine.CheckImageUpdate(ctx, updatertypes.CheckRequest{ImageRef: service.Image, Policy: policy.TagPolicy(service.Labels)})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			record.UpdateType = imageupdate.UpdateTypeNotPulled
		} else {
			record.LastError = new(err.Error())
		}
		return record
	}
	record.HasUpdate = check.UpdateAvailable
	record.UpdateType = check.UpdateType
	if check.CurrentVersion != "" {
		record.CurrentVersion = check.CurrentVersion
	}
	if check.CurrentDigest != "" {
		record.CurrentDigest = new(check.CurrentDigest)
	}
	if check.TargetDigest != "" {
		record.LatestDigest = new(check.TargetDigest)
	}
	if check.TargetRef != "" {
		target, parseErr := refs.NormalizeReference(check.TargetRef)
		if parseErr != nil {
			record.HasUpdate = false
			record.LastError = new(parseErr.Error())
		} else {
			record.LatestVersion = new(target.Tag)
		}
	}
	return record
}
