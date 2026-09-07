package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/moby/moby/api/types/container"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"bufio"
	"context"
	"io"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/samber/mo"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
)

type ProjectServiceInfo struct {
	Name             string                      `json:"name"`
	Image            string                      `json:"image"`
	Status           string                      `json:"status"`
	ContainerID      string                      `json:"container_id"`
	ContainerName    string                      `json:"container_name"`
	Ports            []string                    `json:"ports"`
	Health           *string                     `json:"health,omitempty"`
	IconLightURL     string                      `json:"icon_light_url,omitempty"`
	IconDarkURL      string                      `json:"icon_dark_url,omitempty"`
	ServiceConfig    *composetypes.ServiceConfig `json:"service_config,omitempty"`
	Labels           map[string]string           `json:"labels,omitempty"`
	RedeployDisabled bool                        `json:"redeploy_disabled,omitempty"`
}

func getServiceCounts(services []ProjectServiceInfo) (total int, running int) {
	total = len(services)
	for _, service := range services {
		st := strings.ToLower(strings.TrimSpace(service.Status))
		if st == "running" || st == "up" {
			running++
		}
	}
	return total, running
}

func (s *ProjectService) updateProjectStatusandCountsInternal(ctx context.Context, projectID string, status ProjectStatus) error {
	services, err := s.GetProjectServices(ctx, projectID)
	if err != nil {
		slog.Error("GetProjectServices failed during status update", "projectID", projectID, "error", err)
		return s.updateProjectStatusInternal(ctx, projectID, status)
	}

	serviceCount, runningCount := getServiceCounts(services)

	if err := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"status":        status,
		"service_count": serviceCount,
		"running_count": runningCount,
		"updated_at":    time.Now(),
	}).Error; err != nil {
		return errors.WrapIf(err, "failed to update project status and counts")
	}

	return nil
}

func (s *ProjectService) updateProjectStatusInternal(ctx context.Context, id string, status ProjectStatus) error {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", id).Updates(map[string]any{
		"status":     status,
		"updated_at": now,
	})

	if res.Error != nil {
		return errors.WrapIf(res.Error, "failed to update project status")
	}

	return nil
}

func (s *ProjectService) GetProjectServices(ctx context.Context, projectID string) ([]ProjectServiceInfo, error) {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	composeProject, composeFileFullPath, derr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb)
	if derr != nil {
		return []ProjectServiceInfo{}, errors.WrapIff(derr, "failed to load compose project in %s", projectFromDb.Path)
	}

	projectsDirectory, projectsDirErr := s.GetProjectsDirectory(ctx)
	if projectsDirErr != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for Arcane compose metadata", "path", composeFileFullPath, "error", projectsDirErr)
	}
	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)

	meta, metaErr := projects.ParseArcaneComposeMetadata(ctx, composeFileFullPath, projectsDirectory, autoInjectEnv)
	if metaErr != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFileFullPath, "error", metaErr)
	}

	containers, err := projects.ComposePs(ctx, s.dockerService.DockerHost(), composeProject, nil, true)
	if err != nil {
		slog.Error("compose ps error", "projectName", composeProject.Name, "error", err)
		return nil, errors.WrapIf(err, "failed to get compose services status")
	}
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()

	have := map[string]bool{}
	var services []ProjectServiceInfo

	// Create a map for quick lookup of service config
	serviceConfigs := make(map[string]composetypes.ServiceConfig)
	for _, svc := range composeProject.Services {
		serviceConfigs[svc.Name] = svc
	}

	for _, c := range containers {
		var health *string
		if c.Health != "" {
			health = new(string(c.Health))
		}

		var svcConfig *composetypes.ServiceConfig
		if cfg, ok := serviceConfigs[c.Service]; ok {
			svcConfig = &cfg
		}

		resolvedIcon := iconcatalog.Resolve(IconCatalogForContext(ctx), iconcatalog.FirstNonEmpty(
			projects.FindArcaneIconSet(c.Labels),
			meta.ServiceIconSets[c.Service],
			meta.ProjectIcon,
		))
		services = append(services, ProjectServiceInfo{
			Name:             c.Service,
			Image:            c.Image,
			Status:           string(c.State),
			ContainerID:      c.ID,
			ContainerName:    c.Name,
			Ports:            projects.FormatPorts(c.Publishers),
			Health:           health,
			IconLightURL:     resolvedIcon.IconLightURL,
			IconDarkURL:      resolvedIcon.IconDarkURL,
			ServiceConfig:    svcConfig,
			Labels:           c.Labels,
			RedeployDisabled: labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr),
		})
		have[c.Service] = true
	}

	for _, svc := range composeProject.Services {
		if !have[svc.Name] {
			resolvedIcon := iconcatalog.Resolve(IconCatalogForContext(ctx), iconcatalog.FirstNonEmpty(
				meta.ServiceIconSets[svc.Name],
				meta.ProjectIcon,
			))
			services = append(services, ProjectServiceInfo{
				Name:          svc.Name,
				Image:         svc.Image,
				Status:        "stopped",
				Ports:         []string{},
				IconLightURL:  resolvedIcon.IconLightURL,
				IconDarkURL:   resolvedIcon.IconDarkURL,
				ServiceConfig: new(svc),
			})
		}
	}

	return services, nil
}

func (s *ProjectService) GetProjectContent(ctx context.Context, projectID string) (composeContent, envContent, overrideContent string, err error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return "", "", "", err
	}

	composePath, composeErr := s.ResolveProjectComposeFile(ctx, proj)
	if composeErr != nil && !errors.Is(composeErr, common.ErrProjectComposeFileNotFound) {
		return "", "", "", composeErr
	}

	composeContent, envContent, err = projects.ReadProjectFiles(ctx, proj.Path, composePath)
	if err != nil {
		return "", "", "", err
	}

	return composeContent, envContent, projects.ReadComposeOverrideContent(proj.Path), nil
}

func (s *ProjectService) populateDetailsComposeContentInternal(ctx context.Context, proj *Project, opts project.DetailsOptions, composeSelection []string, resp *project.Details) error {
	if !opts.IncludeComposeContent {
		return nil
	}
	composeContent, _, overrideContent, err := s.GetProjectContent(ctx, proj.ID)
	if err != nil {
		return errors.WrapIf(err, "failed to read project compose content")
	}
	resp.ComposeContent = composeContent
	resp.OverrideFileName, resp.OverrideContent = resolveDetailsOverrideInternal(proj.Path, overrideContent, composeSelection)
	return nil
}

func (s *ProjectService) GetProjectDetails(ctx context.Context, projectID string, opts project.DetailsOptions) (project.Details, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return project.Details{}, err
	}
	projectsDir, projectsDirErr := s.GetProjectsDirectory(ctx)
	if projectsDirErr != nil {
		// Relative paths and the .env.global selection layer degrade without a
		// projects directory; keep the details response intact but log it.
		slog.WarnContext(ctx, "failed to resolve projects directory for project details", "projectID", projectID, "error", projectsDirErr)
	}

	var resp project.Details
	if err := mapper.MapStruct(proj, &resp); err != nil {
		return project.Details{}, errors.WrapIf(err, "failed to map project")
	}

	resp.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	resp.IsArchived = proj.IsArchived
	resp.ArchivedAt = proj.ArchivedAt
	resp.HasBuildDirective = proj.BuildImageRefsJSON != nil && len(projects.ParseImageRefsJSON(*proj.BuildImageRefsJSON)) > 0
	resp.DirName = mo.PointerToOption(proj.DirName).OrEmpty()
	resp.RelativePath = getProjectRelativePathInternal(projectsDir, proj.Path)
	resp.GitOpsManagedBy = proj.GitOpsManagedBy
	meta := s.ProjectMetadata(ctx, *proj, nil)
	applyResolvedProjectIconInternal(&resp, iconcatalog.Resolve(IconCatalogForContext(ctx), meta.ProjectIcon))
	resp.URLs = meta.ProjectURLS
	resp.Tags, err = s.GetProjectTags(ctx, projectID)
	if err != nil {
		return project.Details{}, err
	}

	// Default counts/status from DB (will be overridden if runtime check succeeds)
	resp.ServiceCount = proj.ServiceCount
	resp.RunningCount = proj.RunningCount
	resp.Status = string(proj.Status)

	// COMPOSE_FILE in the project's .env selects the compose file set; when set,
	// `docker compose` skips auto-overrides and Arcane deploys exactly this list.
	// ComposeFiles is populated only for a multi-file selection. A broken
	// selection keeps the details response intact but logs the failure so the
	// configuration problem is diagnosable.
	composeSelection, selErr := projects.ComposeFileEnvSelection(ctx, projectsDir, proj.Path)
	if selErr != nil {
		slog.WarnContext(ctx, "failed to resolve COMPOSE_FILE selection for project details", "projectID", proj.ID, "path", proj.Path, "error", selErr)
		composeSelection = nil
	}
	resp.ComposeFiles = composeSelectionRelativePathsInternal(proj.Path, composeSelection)

	if err := s.populateDetailsComposeContentInternal(ctx, proj, opts, composeSelection, &resp); err != nil {
		return project.Details{}, err
	}
	if opts.IncludeEnvState {
		envState, err := projects.ReadProjectEnvState(proj.Path)
		if err != nil {
			return project.Details{}, errors.WrapIf(err, "failed to read project env state")
		}
		effectiveEnvContent, err := resolveStoredEffectiveEnvContentInternal(envState)
		if err != nil {
			return project.Details{}, err
		}
		resp.EnvContent = effectiveEnvContent
	}

	s.enrichComposeDetailsInternal(ctx, proj, opts, &resp)
	s.enrichWithGitOpsInfo(ctx, proj, &resp)

	// Refresh runtime status/counts even when callers do not request the full
	// runtime service array. DB values are only a fallback when Docker lookup
	// or compose loading fails.
	services, serr := s.GetProjectServices(ctx, projectID)
	if serr == nil && services != nil {
		resp.ServiceCount = len(services)
		_, runningCount := getServiceCounts(services)
		resp.RunningCount = runningCount
		resp.Status = string(calculateProjectStatus(services))

		if opts.IncludeRuntimeServices || opts.IncludeUpdateInfo {
			resp.RuntimeServices = buildProjectRuntimeServicesInternal(services)
			for _, svc := range services {
				if svc.RedeployDisabled {
					resp.RedeployDisabled = true
					break
				}
			}
		}
	}

	if opts.IncludeUpdateInfo {
		s.enrichProjectUpdateInfoInternal(ctx, &resp)
	}
	if !opts.IncludeRuntimeServices {
		resp.RuntimeServices = nil
	}
	if !opts.IncludeServiceConfigs {
		resp.Services = nil
	}

	return resp, nil
}

// composeSelectionRelativePathsInternal converts a multi-file COMPOSE_FILE
// selection (absolute paths) into ordered project-relative paths for the details
// DTO. It returns nil for a single-file or empty selection, where ComposeFileName
// is authoritative.
func composeSelectionRelativePathsInternal(projectPath string, selection []string) []string {
	if len(selection) <= 1 {
		return nil
	}
	rels := make([]string, 0, len(selection))
	for _, f := range selection {
		if rel, err := filepath.Rel(projectPath, f); err == nil {
			rels = append(rels, rel)
		} else {
			rels = append(rels, filepath.Base(f))
		}
	}
	return rels
}

// resolveDetailsOverrideInternal resolves the override file name and content for
// the details response. When a COMPOSE_FILE selection is active and does not
// include the override, it is suppressed so the UI does not invite edits that
// deploy would ignore.
func resolveDetailsOverrideInternal(projectPath, overrideContent string, composeSelection []string) (fileName, content string) {
	overridePath := projects.DetectComposeOverrideFile(projectPath)
	if suppressOverrideForComposeSelectionInternal(composeSelection, overridePath) {
		return "", ""
	}
	if overridePath != "" {
		fileName = filepath.Base(overridePath)
	}
	return fileName, overrideContent
}

// suppressOverrideForComposeSelectionInternal reports whether an Arcane-managed
// override should be hidden from the details response: a COMPOSE_FILE selection
// is active and does not list the detected override file (docker skips
// auto-overrides for an explicit file set). Paths are compared in full, not by
// base name, so a same-named file in a subdirectory does not count as the
// project-root override.
func suppressOverrideForComposeSelectionInternal(selection []string, overridePath string) bool {
	if len(selection) == 0 {
		return false
	}
	if overridePath == "" {
		return true
	}
	absOverride, err := filepath.Abs(filepath.Clean(overridePath))
	if err != nil {
		return true
	}
	// Selection entries are already absolute, cleaned paths.
	return !slices.Contains(selection, absOverride)
}

func buildProjectRuntimeServicesInternal(services []ProjectServiceInfo) []project.RuntimeService {
	runtimeServices := make([]project.RuntimeService, len(services))
	for i, svc := range services {
		runtimeServices[i] = project.RuntimeService{
			Name:             svc.Name,
			Image:            svc.Image,
			Status:           svc.Status,
			ContainerID:      svc.ContainerID,
			ContainerLabels:  svc.Labels,
			ContainerName:    svc.ContainerName,
			Ports:            svc.Ports,
			Health:           svc.Health,
			IconLightURL:     svc.IconLightURL,
			IconDarkURL:      svc.IconDarkURL,
			ServiceConfig:    svc.ServiceConfig,
			RedeployDisabled: svc.RedeployDisabled,
		}
	}
	return runtimeServices
}

func (s *ProjectService) enrichWithIncludeFiles(ctx context.Context, composeFile string, resp *project.Details) {
	if strings.TrimSpace(composeFile) == "" {
		return
	}

	// Load environment variables so that include paths with ${VAR} references are expanded
	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory, _ := projects.GetProjectsDirectory(ctx, strings.TrimSpace(cfg.ProjectsDirectory.Value))
	envLoader := projects.NewEnvLoader(projectsDirectory, filepath.Dir(composeFile), utils.BoolOrDefault(cfg.AutoInjectEnv.Value, false))
	envMap, _, _ := envLoader.LoadEnvironment(ctx)

	includes, parseErr := projects.ParseIncludes(composeFile, envMap, false)
	if parseErr == nil {
		var includeFiles []project.IncludeFile
		for _, inc := range includes {
			includeFiles = append(includeFiles, project.IncludeFile{
				Path:         inc.Path,
				RelativePath: inc.RelativePath,
			})
		}
		resp.IncludeFiles = includeFiles
	} else {
		slog.WarnContext(ctx, "Failed to parse includes", "error", parseErr, "path", composeFile)
	}
}

func (s *ProjectService) enrichProjectUpdateInfoInternal(ctx context.Context, resp *project.Details) {
	if resp == nil {
		return
	}

	imageRefs := projects.ImageRefsFromComposeConfigs(resp.Services)
	if len(imageRefs) == 0 {
		imageRefs = projects.ImageRefsFromRuntimeServices(resp.RuntimeServices)
	}

	var updateInfoByRef map[string]*imagetypes.UpdateInfo
	if len(imageRefs) > 0 && s.imageService != nil {
		lookupResult, err := s.imageService.GetUpdateInfoByImageRefs(ctx, imageRefs)
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch project update info", "projectID", resp.ID, "projectName", resp.Name, "error", err)
		} else {
			updateInfoByRef = lookupResult
		}
	}

	scoped := s.getProjectContainerUpdateInfoInternal(ctx, []project.Details{*resp})
	if len(resp.Services) > 0 {
		records := s.getProjectServiceUpdateRecordsInternal(ctx, []string{resp.ID})
		resp.UpdateInfo = BuildConfiguredUpdateInfo(resp.ID, resp.Services, updateInfoByRef, records, configuredRuntimeServiceUpdateInfoInternal(resp.Services, resp.RuntimeServices, scoped))
		return
	}
	resp.UpdateInfo = BuildUpdateInfoSummary(imageRefs, mergeProjectContainerUpdateInfoInternal(updateInfoByRef, resp.RuntimeServices, scoped))
}

func (s *ProjectService) enrichProjectsWithUpdateInfoInternal(
	ctx context.Context,
	projectsList []Project,
	details []project.Details,
) {
	if len(projectsList) == 0 || len(details) == 0 {
		return
	}

	imageRefsByProjectID := make(map[string][]string, len(projectsList))
	allImageRefs := make([]string, 0)
	servicesByProjectID := make(map[string][]composetypes.ServiceConfig, len(projectsList))
	projectIDs := make([]string, 0, len(projectsList))
	cfg := s.settingsService.GetSettingsOrDefaults(ctx)

	type imageRefsResult struct {
		projectID string
		refs      []string
		services  []composetypes.ServiceConfig
	}

	sem := make(chan struct{}, maxConcurrentComposeReads)
	resultsCh := make(chan imageRefsResult, len(projectsList))

	var wg sync.WaitGroup
	for _, proj := range projectsList {
		projectIDs = append(projectIDs, proj.ID)

		wg.Add(1)
		go func(proj Project) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			composeProject, err := s.getCachedComposeProjectInternal(ctx, &proj, cfg)
			if err != nil {
				slog.WarnContext(ctx, "failed to resolve project services for update summary", "projectID", proj.ID, "projectName", proj.Name, "error", err)
				resultsCh <- imageRefsResult{projectID: proj.ID, refs: projects.ParseImageRefsJSON(proj.ImageRefsJSON)}
				return
			}
			services := make([]composetypes.ServiceConfig, 0, len(composeProject.Services))
			for _, service := range composeProject.Services {
				services = append(services, service)
			}
			resultsCh <- imageRefsResult{projectID: proj.ID, refs: projects.ImageRefsFromComposeConfigs(services), services: services}
		}(proj)
	}

	wg.Wait()
	close(resultsCh)

	for result := range resultsCh {
		imageRefsByProjectID[result.projectID] = result.refs
		servicesByProjectID[result.projectID] = result.services
		allImageRefs = append(allImageRefs, result.refs...)
	}

	var updateInfoByRef map[string]*imagetypes.UpdateInfo
	if len(allImageRefs) > 0 && s.imageService != nil {
		lookupResult, err := s.imageService.GetUpdateInfoByImageRefs(ctx, allImageRefs)
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch project list update info", "error", err)
		} else {
			updateInfoByRef = lookupResult
		}
	}

	records := s.getProjectServiceUpdateRecordsInternal(ctx, projectIDs)
	scoped := s.getProjectContainerUpdateInfoInternal(ctx, details)
	for i := range details {
		if services := servicesByProjectID[details[i].ID]; len(services) > 0 {
			details[i].UpdateInfo = BuildConfiguredUpdateInfo(details[i].ID, services, updateInfoByRef, records, configuredRuntimeServiceUpdateInfoInternal(services, details[i].RuntimeServices, scoped))
			continue
		}
		refs := imageRefsByProjectID[details[i].ID]
		if len(refs) == 0 {
			refs = projects.ImageRefsFromRuntimeServices(details[i].RuntimeServices)
		}
		details[i].UpdateInfo = BuildUpdateInfoSummary(refs, mergeProjectContainerUpdateInfoInternal(updateInfoByRef, details[i].RuntimeServices, scoped))
	}
}

func (s *ProjectService) getProjectServiceUpdateRecordsInternal(ctx context.Context, projectIDs []string) []imageupdate.ImageUpdateRecord {
	if s.db == nil || len(projectIDs) == 0 {
		return nil
	}
	var records []imageupdate.ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("project_id <> ? AND project_id IN ?", "", projectIDs).Find(&records).Error; err != nil {
		slog.WarnContext(ctx, "failed to fetch project service update checks", "error", err)
		return nil
	}
	return records
}

// BuildConfiguredUpdateInfo matches checks to current services and aggregates their results.
func BuildConfiguredUpdateInfo(projectID string, services []composetypes.ServiceConfig, byRef map[string]*imagetypes.UpdateInfo, records []imageupdate.ImageUpdateRecord, runtimeUpdates ...map[string]*imagetypes.UpdateInfo) *project.UpdateInfo {
	checks := make(map[string]*imageupdate.ImageUpdateRecord)
	for i := range records {
		if records[i].ProjectID == projectID {
			checks[records[i].ServiceName] = &records[i]
		}
	}
	serviceUpdates := make(map[string]project.ServiceUpdateInfo, len(services))
	selected := make(map[string]*imagetypes.UpdateInfo)
	runtime := make([]project.RuntimeService, 0, len(services))
	unknownRefs := make(map[string]bool)
	for _, service := range services {
		imageRef := strings.TrimSpace(service.Image)
		if imageRef == "" {
			continue
		}
		var info *imagetypes.UpdateInfo
		record := checks[service.Name]
		policy, policyErr := tagpolicy.Resolve(imageRef, updater.DefaultLabelPolicy().TagPolicy(service.Labels))
		if record != nil && record.PolicyKey == imageref.UpdatePolicyKey(imageRef, service.Labels) {
			info = record.UpdateInfo()
		} else if policyErr == nil && policy.Strategy == "digest" {
			info = byRef[imageRef]
		}

		if len(runtimeUpdates) > 0 && runtimeUpdates[0][service.Name] != nil {
			info = mergeProjectContainerUpdateInfoInternal(nil, []project.RuntimeService{{ContainerID: "preview", Image: imageRef}, {ContainerID: "runtime", Image: imageRef}}, map[string]*imagetypes.UpdateInfo{"preview": info, "runtime": runtimeUpdates[0][service.Name]})[imageRef]
		}
		serviceUpdates[service.Name] = project.ServiceUpdateInfo{ImageRef: imageRef, UpdateInfo: info}
		selected[service.Name] = info
		runtime = append(runtime, project.RuntimeService{ContainerID: service.Name, Image: imageRef})
		if info == nil {
			unknownRefs[imageRef] = true
		}
	}
	merged := mergeProjectContainerUpdateInfoInternal(nil, runtime, selected)
	for imageRef := range unknownRefs {
		if info := merged[imageRef]; info == nil || !info.HasUpdate {
			delete(merged, imageRef)
		}
	}
	summary := BuildUpdateInfoSummary(projects.ImageRefsFromComposeConfigs(services), merged)
	summary.ServiceUpdates = serviceUpdates
	return summary
}

// configuredRuntimeServiceUpdateInfoInternal also binds runtime checks to the
// current Compose source, since container labels may predate an operator edit.
func configuredRuntimeServiceUpdateInfoInternal(services []composetypes.ServiceConfig, runtime []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) map[string]*imagetypes.UpdateInfo {
	configs := make(map[string]composetypes.ServiceConfig, len(services))
	for _, service := range services {
		configs[service.Name] = service
	}
	grouped := make(map[string][]project.RuntimeService)
	for _, service := range runtime {
		name := service.Name
		if name == "" {
			name = dockerutil.ComposeServiceLabel(service.ContainerLabels)
		}
		config, ok := configs[name]
		if !ok || scoped[service.ContainerID] == nil {
			continue
		}
		if imageref.UpdatePolicyKey(config.Image, config.Labels) != imageref.UpdatePolicyKey(service.Image, service.ContainerLabels) {
			continue
		}
		service.Image = config.Image
		grouped[name] = append(grouped[name], service)
	}
	result := make(map[string]*imagetypes.UpdateInfo, len(grouped))
	for name, replicas := range grouped {
		result[name] = mergeProjectContainerUpdateInfoInternal(nil, replicas, scoped)[strings.TrimSpace(configs[name].Image)]
	}
	return result
}

func (s *ProjectService) getProjectContainerUpdateInfoInternal(ctx context.Context, details []project.Details) map[string]*imagetypes.UpdateInfo {
	if s.imageService == nil {
		return nil
	}
	var containers []container.Summary
	for _, detail := range details {
		for _, service := range detail.RuntimeServices {
			if service.ContainerID != "" {
				containers = append(containers, container.Summary{ID: service.ContainerID, Image: service.Image, Labels: service.ContainerLabels})
			}
		}
	}
	scoped, err := s.imageService.GetUpdateInfoByContainers(ctx, containers)
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch project container update info", "error", err)
		return nil
	}
	return scoped
}

func mergeProjectContainerUpdateInfoInternal(base map[string]*imagetypes.UpdateInfo, services []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) map[string]*imagetypes.UpdateInfo {
	result := make(map[string]*imagetypes.UpdateInfo, len(base))
	maps.Copy(result, base)
	seen := make(map[string]bool)
	for _, service := range services {
		info := scoped[service.ContainerID]
		if info == nil {
			continue
		}
		imageRef := strings.TrimSpace(service.Image)
		if imageRef == "" {
			continue
		}
		merged := *info
		if seen[imageRef] {
			previous := result[imageRef]
			merged.HasUpdate = merged.HasUpdate || previous.HasUpdate
			if previous.CheckTime.After(merged.CheckTime) {
				merged.CheckTime = previous.CheckTime
			}
			if previous.LatestVersion != merged.LatestVersion || previous.LatestDigest != merged.LatestDigest || previous.UpdateType != merged.UpdateType {
				merged.LatestVersion, merged.LatestDigest, merged.UpdateType = "", "", ""
			}
			if merged.Error == "" {
				merged.Error = previous.Error
			}
		}
		result[imageRef] = &merged
		seen[imageRef] = true
	}
	return result
}

func (s *ProjectService) getProjectImageRefsFromComposeInternal(ctx context.Context, proj Project, cfg *settings.Settings) ([]string, []string, error) {
	composeProject, err := s.getCachedComposeProjectInternal(ctx, &proj, cfg)
	if err != nil {
		return nil, nil, errors.WrapIf(err, "load compose project")
	}

	return projects.ImageRefsFromComposeServices(composeProject.Services), projects.BuildImageRefsFromComposeProject(composeProject), nil
}

// BuildUpdateInfoSummary aggregates image checks into a project update summary.
func BuildUpdateInfoSummary(
	imageRefs []string,
	updateInfoByRef map[string]*imagetypes.UpdateInfo,
) *project.UpdateInfo {
	imageCount := len(imageRefs)
	summary := &project.UpdateInfo{
		Status:     "unknown",
		HasUpdate:  false,
		ImageCount: imageCount,
		ImageRefs:  append([]string(nil), imageRefs...),
	}

	if imageCount == 0 {
		return summary
	}

	var latestCheckTime *time.Time

	for _, imageRef := range imageRefs {
		info := updateInfoByRef[imageRef]
		if info == nil {
			continue
		}

		summary.CheckedImageCount++
		if summary.UpdateInfoByRef == nil {
			summary.UpdateInfoByRef = make(map[string]imagetypes.UpdateInfo)
		}
		summary.UpdateInfoByRef[imageRef] = *info
		if info.HasUpdate {
			summary.HasUpdate = true
			summary.ImagesWithUpdates++
			summary.UpdatedImageRefs = append(summary.UpdatedImageRefs, imageRef)
		}
		if info.UpdateType == imageupdate.UpdateTypeNotPulled {
			summary.ImagesNotPulled++
			summary.NotPulledImageRefs = append(summary.NotPulledImageRefs, imageRef)
		}
		if strings.TrimSpace(info.Error) != "" {
			summary.ErrorCount++
			if summary.ErrorMessage == nil {
				summary.ErrorMessage = new(strings.TrimSpace(info.Error))
			}
		}
		if !info.CheckTime.IsZero() && (latestCheckTime == nil || info.CheckTime.After(*latestCheckTime)) {
			latestCheckTime = new(info.CheckTime)
		}
	}

	summary.LastCheckedAt = latestCheckTime

	switch {
	case summary.ImagesWithUpdates > 0:
		summary.Status = "has_update"
	case summary.ErrorCount > 0:
		summary.Status = "error"
	case summary.ImagesNotPulled > 0:
		summary.Status = "not_pulled"
	case summary.CheckedImageCount == imageCount:
		summary.Status = "up_to_date"
	default:
		summary.Status = "unknown"
	}

	return summary
}

func (s *ProjectService) enrichWithGitOpsInfo(ctx context.Context, proj *Project, resp *project.Details) {
	if proj.GitOpsManagedBy != nil {
		var syncRecord GitOpsSync
		if err := s.db.WithContext(ctx).Preload("Repository").Where("id = ?", *proj.GitOpsManagedBy).First(&syncRecord).Error; err == nil {
			resp.LastSyncCommit = syncRecord.LastSyncCommit
			if syncRecord.Repository != nil {
				resp.GitRepositoryURL = syncRecord.Repository.URL
			}
		}
	}
}

func (s *ProjectService) enrichComposeDetailsInternal(ctx context.Context, proj *Project, opts project.DetailsOptions, resp *project.Details) {
	composeFile, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		return
	}
	resp.ComposeFileName = filepath.Base(composeFile)
	if opts.IncludeIncludeFiles {
		s.enrichWithIncludeFiles(ctx, composeFile, resp)
	}
	if !opts.IncludeServiceConfigs && !opts.IncludeUpdateInfo {
		return
	}

	composeProj, loadErr := s.getCachedComposeProjectInternal(ctx, proj, nil)
	if loadErr != nil {
		slog.WarnContext(ctx, "failed to load compose service configs", "path", composeFile, "error", loadErr)
		return
	}

	if composeProj == nil {
		return
	}

	// Convert map to slice
	svcList := make([]composetypes.ServiceConfig, 0, len(composeProj.Services))
	hasBuildDirective := false
	for _, svc := range composeProj.Services {
		svcList = append(svcList, svc)
		if svc.Build != nil {
			hasBuildDirective = true
		}
	}
	resp.Services = svcList
	resp.HasBuildDirective = resp.HasBuildDirective || hasBuildDirective
}

func (s *ProjectService) StreamProjectLogs(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	done := make(chan error, 2)

	// Reader goroutine: forward lines to channel
	go func() {
		// Closing the read half unblocks any pending pw.Write in ComposeLogs.
		// Without it, an abandoned tail (ctx cancel, or a >1MiB line tripping
		// bufio.ErrTooLong) wedges the writer goroutine forever and this
		// function never collects its second done value.
		defer func() { _ = pr.CloseWithError(io.ErrClosedPipe) }()

		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case logsChan <- sc.Text():
			}
		}
		done <- sc.Err()
	}()

	// Writer goroutine: compose logs -> pipe
	go func() {
		err := projects.ComposeLogs(ctx, projects.NormalizeProjectName(proj.Name), pw, follow, tail, since, timestamps)
		_ = pw.Close()
		done <- err
	}()

	// Wait for both goroutines to finish to avoid sending on a closed channel
	err1 := <-done
	err2 := <-done

	for _, e := range []error{err1, err2} {
		if e != nil && !errors.Is(e, io.EOF) && !errors.Is(e, context.Canceled) {
			return e
		}
	}
	return nil
}

func (s *ProjectService) CountServicesFromCompose(ctx context.Context, p Project) (int, error) {
	proj, _, err := s.loadComposeProjectForProjectInternal(ctx, &p)
	if err != nil {
		return 0, err
	}

	return len(proj.Services), nil
}

func calculateProjectStatus(services []ProjectServiceInfo) ProjectStatus {
	if len(services) == 0 {
		return ProjectStatusUnknown
	}

	runningCount := 0
	stoppedCount := 0

	for _, svc := range services {
		state := strings.ToLower(strings.TrimSpace(svc.Status))
		switch state {
		case "running", "up":
			runningCount++
		case "exited", "stopped", "dead":
			stoppedCount++
		}
	}

	if runningCount == len(services) {
		return ProjectStatusRunning
	}
	if runningCount > 0 {
		return ProjectStatusPartiallyRunning
	}
	if stoppedCount > 0 {
		return ProjectStatusStopped
	}
	return ProjectStatusUnknown
}
