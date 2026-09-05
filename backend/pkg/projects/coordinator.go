package projects

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
)

type composeCoordinatorInternal struct {
	commands projecttypes.ComposeCommands
}

// NewCoordinator owns Compose workflow sequencing using the configured SDK commands.
func NewCoordinator(commands projecttypes.ComposeCommands) projecttypes.ComposeCoordinator {
	if commands.Stop == nil {
		commands.Stop = ComposeStop
	}
	if commands.Up == nil {
		commands.Up = ComposeUp
	}
	return &composeCoordinatorInternal{commands: commands}
}

// Deploy runs hooks before loading, prepares images, then reconciles the project.
func (c *composeCoordinatorInternal) Deploy(ctx context.Context, request projecttypes.ComposeDeployment) (_ *composetypes.Project, err error) {
	defer func() {
		if err != nil && request.Recover != nil {
			request.Recover(ctx)
		}
	}()
	if request.PreDeploy != nil {
		if err := request.PreDeploy(ctx); err != nil {
			return nil, errors.WrapIf(err, "pre-deploy lifecycle hook failed")
		}
	}
	model, err := request.Load(ctx)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to load compose project in %s", request.ProjectPath)
	}
	operations, err := request.ResolveImages(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "resolve registry credentials")
	}
	pullPolicy := ""
	forceRecreate, recreateVolumes := false, false
	if request.Options != nil {
		pullPolicy = NormalizeDeployPullPolicy(request.Options.PullPolicy)
		forceRecreate, recreateVolumes = request.Options.ForceRecreate, request.Options.RecreateVolumes
	}
	if pullPolicy == "" {
		pullPolicy = NormalizeDeployPullPolicy(request.DefaultPullPolicy)
	}
	if pullPolicy == "" {
		pullPolicy = "missing"
	}
	if err := c.PrepareImagesForDeploy(ctx, request.ProjectID, model, request.Progress, operations, pullPolicy); err != nil {
		return nil, errors.WrapIf(err, "failed to prepare project images for deploy")
	}
	removeOrphans := ResolveRemoveOrphans(request.GitOpsManaged, request.Options)
	slog.InfoContext(ctx, "starting compose up with health check support", "projectID", request.ProjectID, "projectName", model.Name, "services", len(model.Services), "removeOrphans", removeOrphans)
	if err := c.commands.Up(ctx, model, nil, removeOrphans, forceRecreate, recreateVolumes, request.AuthConfigs, request.WaitTimeout); err != nil {
		slog.ErrorContext(ctx, "compose up failed", "projectName", model.Name, "projectID", request.ProjectID, "error", err)
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "context deadline exceeded") {
			return nil, errors.WrapIf(err, "deployment timed out waiting for services - long-running 'service_healthy'/'service_completed_successfully' dependencies may need a higher Deploy Wait Timeout setting, and 'service_healthy' requires a healthcheck")
		}
		return nil, errors.WrapIf(err, "failed to deploy project")
	}
	slog.InfoContext(ctx, "compose up completed successfully", "projectID", request.ProjectID, "projectName", model.Name)
	return model, nil
}

// ResolveRemoveOrphans preserves GitOps reconciliation and explicit caller opt-in.
func ResolveRemoveOrphans(gitOpsManaged bool, options *projecttypes.DeployOptions) bool {
	return gitOpsManaged || (options != nil && options.RemoveOrphans)
}

// UpdateServices validates scope before pulling or stopping any service.
func (c *composeCoordinatorInternal) UpdateServices(ctx context.Context, request projecttypes.ComposeServiceUpdate) error {
	selected, err := SelectServices(request.Project, request.Services)
	if err != nil {
		if request.RestoreBeforeMutation != nil {
			request.RestoreBeforeMutation(ctx)
		}
		return err
	}
	if err := c.PullServices(ctx, selected, request.Services, request.Images, request.Progress); err != nil {
		if request.RestoreBeforeMutation != nil {
			request.RestoreBeforeMutation(ctx)
		}
		return errors.WrapIf(err, "pull updated service images")
	}
	if err := c.commands.Stop(ctx, selected, request.Services); err != nil {
		slog.WarnContext(ctx, "compose stop failed, continuing", "error", err)
	}
	if err := c.commands.Up(ctx, selected, request.Services, false, true, false, request.AuthConfigs, request.WaitTimeout); err != nil {
		if request.Recover != nil {
			request.Recover(ctx)
		}
		return errors.WrapIf(err, "failed to up services")
	}
	return nil
}

// SelectServices enables requested profiles and returns a separate, scoped model.
func SelectServices(model *composetypes.Project, services []string) (*composetypes.Project, error) {
	if model == nil {
		return nil, errors.New("compose project is nil")
	}
	enabled, err := model.WithServicesEnabled(services...)
	if err != nil {
		return nil, err
	}
	selected, err := enabled.WithSelectedServices(services)
	if err != nil {
		return nil, err
	}
	return selected.WithoutUnnecessaryResources(), nil
}

// PullServices pulls each selected registry image once through the application image service.
func (c *composeCoordinatorInternal) PullServices(ctx context.Context, model *composetypes.Project, services []string, operations projecttypes.ComposeImageOperations, progress io.Writer) error {
	for _, imageRef := range SelectedImageRefs(model, services) {
		if err := operations.Pull(ctx, imageRef, progress); err != nil {
			return err
		}
	}
	return nil
}

func (c *composeCoordinatorInternal) imageRefreshDueInternal(ctx context.Context, imageRef string, window time.Duration, operations projecttypes.ComposeImageOperations) bool {
	lastTagged, err := operations.LastTagged(ctx, imageRef)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve image last-tag time; treating refresh as due", "image", imageRef, "error", err)
		return true
	}
	return lastTagged.IsZero() || time.Now().After(lastTagged.Add(window))
}

func (c *composeCoordinatorInternal) EnsureImagesPresent(ctx context.Context, model *composetypes.Project, progressWriter io.Writer, operations projecttypes.ComposeImageOperations) error {
	for img, step := range BuildImagePullPlan(model) {
		exists, ierr := operations.Exists(ctx, img)
		if ierr != nil && step.Mode != ImagePullModeAlways {
			slog.WarnContext(ctx, "failed to check local image existence", "image", img, "error", ierr)
			// Non-fatal: attempt to pull to be safe
		}

		if step.Mode == ImagePullModeNever {
			if ierr != nil {
				slog.WarnContext(ctx, "pull_policy is 'never' but image presence check failed; continuing without pull", "image", img, "error", ierr)
				continue
			}
			if !exists {
				return errors.Errorf("image %s is not available locally and pull_policy is 'never'", img)
			}
			slog.DebugContext(ctx, "pull_policy is 'never'; using local image without pull", "image", img)
			continue
		}

		if step.Mode == ImagePullModeIfMissing && exists {
			slog.DebugContext(ctx, "image already present locally; skipping pull", "image", img)
			continue
		}

		if step.Mode == ImagePullModeRefresh && exists && !c.imageRefreshDueInternal(ctx, img, step.RefreshAfter, operations) {
			slog.DebugContext(ctx, "pull_policy refresh window has not elapsed; skipping pull", "image", img, "window", step.RefreshAfter)
			continue
		}

		if err := operations.Pull(ctx, img, progressWriter); err != nil {
			return err
		}
	}
	return nil
}

func (c *composeCoordinatorInternal) PrepareImagesForDeploy(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	progressWriter io.Writer,
	operations projecttypes.ComposeImageOperations,
	pullPolicyOverride string,
) error {
	if project == nil {
		return nil
	}

	for name, svc := range project.Services {
		svc, imageName, updated := PrepareDeployServiceConfig(projectID, project.Name, name, svc)
		if updated {
			project.Services[name] = svc
		}

		if imageName == "" {
			continue
		}

		decision := DecideDeployImageAction(svc, pullPolicyOverride)
		if updated {
			decision = DeployImageDecision{Build: true}
		}
		if err := c.ensureDeployServiceImageReadyInternal(ctx, projectID, project, name, svc, imageName, decision, progressWriter, operations); err != nil {
			return err
		}

		// pre_start hook images and type:image volume sources cannot be built:
		// they follow the service's pull decision minus the build/fallback paths.
		dependentDecision := DeployImageDecision{PullIfMissing: true}
		switch {
		case decision.RequireLocalOnly:
			dependentDecision = DeployImageDecision{RequireLocalOnly: true}
		case decision.PullAlways:
			dependentDecision = DeployImageDecision{PullAlways: true}
		case decision.PullIfStale:
			dependentDecision = DeployImageDecision{PullIfStale: true, StaleAfter: decision.StaleAfter}
		}
		dependentImages := api.GetDependentImages(svc, project.Name)
		for _, vol := range svc.Volumes {
			if vol.Type == composetypes.VolumeTypeImage && strings.TrimSpace(vol.Source) != "" {
				dependentImages = append(dependentImages, strings.TrimSpace(vol.Source))
			}
		}
		for _, img := range dependentImages {
			if err := c.ensureDeployServiceImageReadyInternal(ctx, projectID, project, name, svc, img, dependentDecision, progressWriter, operations); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *composeCoordinatorInternal) ensureDeployServiceImageReadyInternal(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	imageName string,
	decision DeployImageDecision,
	progressWriter io.Writer,
	operations projecttypes.ComposeImageOperations,
) error {
	if decision.Build {
		return c.buildServiceImageForDeployInternal(ctx, projectID, project, serviceName, svc, progressWriter, operations)
	}

	exists, err := operations.Exists(ctx, imageName)
	if err != nil {
		slog.WarnContext(ctx, "failed to check local image existence", "image", imageName, "error", err)
	}

	if decision.RequireLocalOnly {
		if !exists {
			return errors.Errorf("image %s is not available locally and pull_policy is set to never", imageName)
		}
		return nil
	}

	var lastTagged time.Time
	if decision.PullIfStale && exists {
		var tagErr error
		lastTagged, tagErr = operations.LastTagged(ctx, imageName)
		if tagErr != nil {
			// Unknown last-tag time counts as stale, matching the missing-record
			// behavior of compose's refresh handling.
			slog.WarnContext(ctx, "failed to resolve image last-tag time; treating refresh as due", "image", imageName, "error", tagErr)
		}
	}
	if !ShouldPullDeployImage(decision, exists, lastTagged) {
		return nil
	}

	err = operations.Pull(ctx, imageName, progressWriter)
	if err == nil {
		return nil
	}
	if svc.Build != nil && decision.FallbackBuildOnPullFail {
		slog.WarnContext(ctx, "image pull failed, falling back to build", "service", serviceName, "image", imageName, "error", err)
		return c.buildServiceImageForDeployInternal(ctx, projectID, project, serviceName, svc, progressWriter, operations)
	}
	return errors.WrapIff(err, "failed to pull image %s", imageName)
}

func (c *composeCoordinatorInternal) buildServiceImageForDeployInternal(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	progressWriter io.Writer,
	operations projecttypes.ComposeImageOperations,
) error {
	if operations.Build == nil {
		return errors.Errorf("build service not available for service %s", serviceName)
	}

	buildReq, updatedSvc, updated, err := PrepareServiceBuildRequest(projectID, project, serviceName, svc, projecttypes.BuildOptions{}, operations.BuildProvider)
	if err != nil {
		return err
	}
	if updated {
		project.Services[serviceName] = updatedSvc
	}

	if err := operations.Build(ctx, buildReq, progressWriter, serviceName); err != nil {
		return err
	}

	return nil
}

func (c *composeCoordinatorInternal) BuildServices(ctx context.Context, projectID string, project *composetypes.Project, options projecttypes.BuildOptions, progressWriter io.Writer, operations projecttypes.ComposeImageOperations) error {
	if operations.Build == nil {
		return nil
	}
	if project == nil {
		return nil
	}

	selected := NormalizeBuildSelections(options.Services)

	buildCount := 0
	for name, svc := range project.Services {
		if svc.Build == nil {
			continue
		}
		if !ServiceSelected(selected, name) {
			continue
		}

		buildReq, updatedSvc, updated, err := PrepareServiceBuildRequest(projectID, project, name, svc, options, operations.BuildProvider)
		if err != nil {
			return err
		}
		if updated {
			project.Services[name] = updatedSvc
		}

		buildCount++
		if err := operations.Build(ctx, buildReq, progressWriter, name); err != nil {
			return err
		}
	}

	if buildCount == 0 && len(selected) > 0 {
		return errors.Errorf("no build-enabled services matched: %s", strings.Join(options.Services, ", "))
	}

	return nil
}

// NormalizeBuildSelections turns a user-supplied service list into a lookup set,
// dropping blanks. An empty set means "every service".
func NormalizeBuildSelections(services []string) map[string]struct{} {
	selected := map[string]struct{}{}
	for _, name := range services {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		selected[name] = struct{}{}
	}
	return selected
}

// ServiceSelected reports whether name is in the selection built by
// NormalizeBuildSelections. An empty selection selects everything.
func ServiceSelected(selected map[string]struct{}, name string) bool {
	if len(selected) == 0 {
		return true
	}
	_, ok := selected[name]
	return ok
}

// EnsureServiceImage gives a build-only service a deterministic local image tag
// so the built image has a stable name to deploy from. It reports whether the
// service config was changed.
func EnsureServiceImage(projectID, projectName, serviceName string, svc composetypes.ServiceConfig) (string, composetypes.ServiceConfig, bool) {
	imageName := strings.TrimSpace(svc.Image)
	if imageName == "" {
		imageName = BuildLocalImageTag(projectID, projectName, serviceName)
		svc.Image = imageName
		return imageName, svc, true
	}
	return imageName, svc, false
}

// PrepareDeployServiceConfig resolves the image a service deploys as, naming
// build-only services via EnsureServiceImage. It reports whether the service
// config was changed.
func PrepareDeployServiceConfig(projectID, projectName, serviceName string, svc composetypes.ServiceConfig) (composetypes.ServiceConfig, string, bool) {
	if svc.Build == nil {
		return svc, strings.TrimSpace(svc.Image), false
	}

	resolvedImage, updatedSvc, updated := EnsureServiceImage(projectID, projectName, serviceName, svc)
	return updatedSvc, resolvedImage, updated
}

// ShouldPullDeployImage reports whether a deploy must pull, given the resolved
// pull decision, whether the image is already present locally, and when the
// local image was last tagged (zero when unknown). Refresh policies mirror
// compose v5.5.0's `up`: a present image is re-pulled only once its window
// has elapsed since the engine's last-tag time.
func ShouldPullDeployImage(decision DeployImageDecision, exists bool, lastTagged time.Time) bool {
	if decision.PullAlways {
		return true
	}
	if !exists {
		return decision.PullIfMissing || decision.PullIfStale
	}
	if decision.PullIfStale {
		return lastTagged.IsZero() || time.Now().After(lastTagged.Add(decision.StaleAfter))
	}
	return false
}

// SelectedImageRefs returns the distinct pullable image refs of the selected
// services — build services are excluded, since their images come from a build.
func SelectedImageRefs(compProj *composetypes.Project, servicesToUpdate []string) []string {
	if compProj == nil {
		return nil
	}

	selected := NormalizeBuildSelections(servicesToUpdate)
	refs := make([]string, 0, len(compProj.Services))
	seen := make(map[string]struct{}, len(compProj.Services))

	for name, svc := range compProj.Services {
		if !ServiceSelected(selected, name) || svc.Build != nil {
			continue
		}

		imageRef := strings.TrimSpace(svc.Image)
		if imageRef == "" {
			continue
		}
		if _, exists := seen[imageRef]; exists {
			continue
		}

		seen[imageRef] = struct{}{}
		refs = append(refs, imageRef)
	}

	return refs
}
