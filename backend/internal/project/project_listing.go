package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"emperror.dev/errors"
	composeapi "github.com/docker/compose/v5/pkg/api"

	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// listGlobalComposeContainersInternal routes the global compose-container
// list through the shared Docker client singleton when one is wired, avoiding
// a fresh docker CLI per call.
func (s *ProjectService) listGlobalComposeContainersInternal(ctx context.Context) ([]container.Summary, error) {
	var dockerClient client.APIClient
	if s.dockerService != nil {
		cli, err := s.dockerService.GetClient(ctx)
		if err != nil {
			return nil, err
		}
		dockerClient = cli
	}
	return projects.ListGlobalComposeContainers(ctx, dockerClient, s.dockerService.DockerHost())
}

func groupComposeContainersByProjectInternal(containers []container.Summary) map[string][]container.Summary {
	containersByProject := make(map[string][]container.Summary)
	for _, c := range containers {
		projectName := dockerutil.ComposeProjectLabel(c.Labels)
		if projectName != "" {
			containersByProject[projectName] = append(containersByProject[projectName], c)
		}
	}
	return containersByProject
}

// lookupProjectContainersInternal matches project names first, then the working
// directory if it identifies exactly one Compose project group.
func lookupProjectContainersInternal(p Project, containersByProject map[string][]container.Summary) []container.Summary {
	normName := projects.NormalizeProjectName(p.Name)
	if c := containersByProject[normName]; len(c) > 0 {
		return c
	}
	if p.ComposeProjectName != nil && *p.ComposeProjectName != normName {
		if c := containersByProject[*p.ComposeProjectName]; len(c) > 0 {
			return c
		}
	}
	if p.Path == "" {
		return nil
	}
	projectPath := filepath.Clean(p.Path)
	var matched []container.Summary
	var matchedProjectName string
	for projectName, containers := range containersByProject {
		for _, c := range containers {
			workingDir := c.Labels[composeapi.WorkingDirLabel]
			if workingDir != "" && filepath.Clean(workingDir) == projectPath {
				if matched != nil && matchedProjectName != projectName {
					return nil
				}
				matchedProjectName = projectName
				matched = append(matched, c)
			}
		}
	}
	return matched
}

func projectServiceInfoFromContainerInternal(ctx context.Context, c container.Summary, meta projects.ArcaneComposeMetadata, currentContainerID string, currentContainerErr error) ProjectServiceInfo {
	svcName := dockerutil.ComposeServiceLabel(c.Labels)

	var health *string
	statusLower := strings.ToLower(c.Status)
	switch {
	case strings.Contains(statusLower, "(healthy)"):
		health = new("healthy")
	case strings.Contains(statusLower, "(unhealthy)"):
		health = new("unhealthy")
	case strings.Contains(statusLower, "(starting)"):
		health = new("starting")
	}

	resolvedIcon := resolveServiceIconInternal(IconCatalogForContext(ctx), c.Labels, svcName, meta)
	return ProjectServiceInfo{
		Name:             svcName,
		Image:            c.Image,
		Status:           string(c.State),
		ContainerID:      c.ID,
		ContainerName:    dockerutil.ContainerNameFromNames(c.Names),
		Ports:            projects.FormatDockerPorts(c.Ports),
		Health:           health,
		IconLightURL:     resolvedIcon.IconLightURL,
		IconDarkURL:      resolvedIcon.IconDarkURL,
		Labels:           c.Labels,
		RedeployDisabled: labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr),
	}
}

// resolveServiceIconInternal picks a service icon from its container labels,
// then the compose metadata's per-service and project-level icon sets.
func resolveServiceIconInternal(catalog string, containerLabels map[string]string, serviceName string, meta projects.ArcaneComposeMetadata) iconcatalog.ResolvedIconSet {
	return iconcatalog.Resolve(catalog, iconcatalog.FirstNonEmpty(
		projects.FindArcaneIconSet(containerLabels),
		meta.ServiceIconSets[serviceName],
		meta.ProjectIcon,
	))
}

// projectServicesFromContainersInternal derives runtime services from labeled
// containers for projects whose Compose file cannot be loaded.
func (s *ProjectService) projectServicesFromContainersInternal(ctx context.Context, proj *Project, meta projects.ArcaneComposeMetadata) ([]ProjectServiceInfo, error) {
	containers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		return nil, err
	}
	matched := lookupProjectContainersInternal(*proj, groupComposeContainersByProjectInternal(containers))
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	services := make([]ProjectServiceInfo, 0, len(matched))
	for _, c := range matched {
		services = append(services, projectServiceInfoFromContainerInternal(ctx, c, meta, currentContainerID, currentContainerErr))
	}
	return services, nil
}

func (s *ProjectService) ListAllProjects(ctx context.Context) ([]Project, error) {
	var items []Project
	if err := s.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, errors.WrapIf(err, "list projects")
	}
	return items, nil
}

func (s *ProjectService) countProjectFolders(ctx context.Context) (int, error) {
	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return 0, errors.WrapIf(err, "could not determine projects directory")
	}

	// os.* rather than acfs: this probes the projects directory itself, which is
	// the confinement root and may not exist yet.
	info, statErr := os.Stat(projectsDir)
	if os.IsNotExist(statErr) {
		// Directory missing, treat as zero
		return 0, nil
	}
	if statErr != nil {
		return 0, errors.WrapIff(statErr, "unable to access projects directory %s", projectsDir)
	}
	if !info.IsDir() {
		return 0, nil
	}

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(ctx, projectsDir, followProjectSymlinks, s.config.ProjectScanMaxDepth)
	if discoveryErr != nil {
		return 0, errors.WrapIff(discoveryErr, "failed to discover project directories in %s", projectsDir)
	}

	return len(discoveredProjects), nil
}

func incrementStatusCounts(status ProjectStatus, running, stopped *int) {
	switch status {
	case ProjectStatusRunning, ProjectStatusPartiallyRunning, ProjectStatusDeploying, ProjectStatusRestarting:
		*running++
	case ProjectStatusStopped, ProjectStatusStopping:
		*stopped++
	case ProjectStatusUnknown:
		// Don't count unknown
	}
}

func (s *ProjectService) GetProjectStatusCounts(ctx context.Context) (folderCount, runningProjects, stoppedProjects, totalProjects, archivedProjects int, err error) {
	folderCount, _ = s.countProjectFolders(ctx)

	var projectsList []Project
	if err := s.db.WithContext(ctx).Find(&projectsList).Error; err != nil {
		return folderCount, 0, 0, 0, 0, errors.WrapIf(err, "failed to list projects")
	}

	totalProjects = len(projectsList)
	runningProjects = 0
	stoppedProjects = 0
	activeProjects := make([]Project, 0, len(projectsList))
	for _, p := range projectsList {
		if p.IsArchived {
			archivedProjects++
			continue
		}
		activeProjects = append(activeProjects, p)
	}

	// 1. Fetch all compose containers
	containers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers for counts", "error", err)
		// Fallback to DB status
		for _, p := range activeProjects {
			incrementStatusCounts(p.Status, &runningProjects, &stoppedProjects)
		}
		return folderCount, runningProjects, stoppedProjects, totalProjects, archivedProjects, nil
	}

	// 2. Group by project
	containersByProject := groupComposeContainersByProjectInternal(containers)

	// 3. Calculate status for each project
	for _, p := range activeProjects {
		projectContainers := lookupProjectContainersInternal(p, containersByProject)

		// Convert to ProjectServiceInfo (minimal needed for calculateProjectStatus)
		var services []ProjectServiceInfo
		for _, c := range projectContainers {
			services = append(services, ProjectServiceInfo{
				Status: string(c.State),
			})
		}

		var status ProjectStatus
		if len(services) == 0 {
			status = ProjectStatusStopped
		} else {
			status = calculateProjectStatus(services)
		}

		incrementStatusCounts(status, &runningProjects, &stoppedProjects)
	}

	return folderCount, runningProjects, stoppedProjects, totalProjects, archivedProjects, nil
}

func (s *ProjectService) ListProjects(ctx context.Context, params pagination.QueryParams) ([]project.Details, pagination.Response, error) {
	query := s.db.WithContext(ctx).Model(&Project{})
	statusFilter := ""
	updatesFilter := ""
	archivedFilter := ""
	tagsFilter := ""
	labelFilter := ""
	if params.Filters != nil {
		statusFilter = strings.TrimSpace(params.Filters["status"])
		updatesFilter = strings.TrimSpace(params.Filters["updates"])
		archivedFilter = strings.TrimSpace(params.Filters["archived"])
		tagsFilter = strings.TrimSpace(params.Filters["tags"])
		labelFilter = strings.TrimSpace(params.Filters["label"])
	}
	query = applyProjectArchivedDBFilterInternal(query, archivedFilter)
	query = applyProjectTagsDBFilterInternal(query, tagsFilter)
	sortsByDerivedStatus := strings.EqualFold(strings.TrimSpace(params.Sort), "status")
	if statusFilter != "" || updatesFilter != "" || labelFilter != "" || sortsByDerivedStatus {
		return s.listProjectsWithDerivedFiltersInternal(ctx, params, query)
	}

	if term := strings.TrimSpace(params.Search); term != "" {
		query = applyProjectSearchDBFilterInternal(query, term)
	}

	query = pagination.ApplyFilter(query, "status", params.Filters["status"])

	var projectsArray []Project
	paginationResp, err := pagination.PaginateAndSortDB(params, query, &projectsArray)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate projects")
	}

	slog.DebugContext(ctx, "Retrieved projects from database",
		"count", len(projectsArray))

	// Fetch live status concurrently for all projects
	env := s.newProjectMetadataEnvInternal(ctx, projectsArray)
	result := s.fetchProjectStatusConcurrently(ctx, projectsArray, env)
	if err := s.enrichProjectsWithTagsInternal(ctx, result); err != nil {
		return nil, pagination.Response{}, err
	}
	s.enrichProjectsWithUpdateInfoInternal(ctx, projectsArray, result, true, env)

	slog.DebugContext(ctx, "Completed ListProjects request",
		"result_count", len(result))

	return result, paginationResp, nil
}

func applyProjectArchivedDBFilterInternal(query *gorm.DB, filterValue string) *gorm.DB {
	if strings.EqualFold(strings.TrimSpace(filterValue), "all") {
		return query
	}
	archived, _ := utils.ParseBool(filterValue)
	return query.Where("is_archived = ?", archived)
}

func applyProjectTagsDBFilterInternal(query *gorm.DB, filterValue string) *gorm.DB {
	names := normalizeTagFilterValuesInternal(filterValue)
	if len(names) == 0 {
		return query
	}
	return query.Where("EXISTS (SELECT 1 FROM project_tags WHERE project_tags.project_id = projects.id AND project_tags.name IN ?)", names)
}

func normalizeTagFilterValuesInternal(filterValue string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for value := range strings.SplitSeq(filterValue, ",") {
		normalized, err := projects.NormalizeProjectTag(value)
		if err != nil {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func applyProjectSearchDBFilterInternal(query *gorm.DB, term string) *gorm.DB {
	searchPattern := "%" + strings.TrimSpace(term) + "%"
	return query.Where(
		"name LIKE ? OR path LIKE ? OR status LIKE ? OR COALESCE(dir_name, '') LIKE ? OR EXISTS (SELECT 1 FROM project_tags WHERE project_tags.project_id = projects.id AND LOWER(project_tags.name) LIKE ?)",
		searchPattern, searchPattern, searchPattern, searchPattern, "%"+strings.ToLower(strings.TrimSpace(term))+"%",
	)
}

func (s *ProjectService) listProjectsWithDerivedFiltersInternal(
	ctx context.Context,
	params pagination.QueryParams,
	query *gorm.DB,
) ([]project.Details, pagination.Response, error) {
	limit := params.Limit
	switch {
	case limit == -1:
		// Public API contract: exact -1 means "all" (used by the table page-size selector).
	case limit <= 0:
		limit = 20
	case limit > 100:
		limit = 100
	}
	params.Limit = limit

	result, err := s.filterProjectsWithDerivedFiltersInternal(ctx, params, query)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func (s *ProjectService) filterProjectsWithDerivedFiltersInternal(
	ctx context.Context,
	params pagination.QueryParams,
	query *gorm.DB,
) (pagination.FilterResult[project.Details], error) {
	var projectsArray []Project
	if term := strings.TrimSpace(params.Search); term != "" {
		query = applyProjectSearchDBFilterInternal(query, term)
	}
	if err := query.Find(&projectsArray).Error; err != nil {
		return pagination.FilterResult[project.Details]{}, errors.WrapIf(err, "failed to list projects")
	}

	// Filtering, searching, and sorting only read database columns, tags, and
	// the container snapshot, so every candidate gets a lean row and the
	// compose-backed presentation fields are resolved for the page alone.
	env := s.newProjectMetadataEnvInternal(ctx, nil)
	snapshot := s.projectContainerSnapshotInternal(ctx)
	items := s.projectListRowsInternal(ctx, env.projectsDirectory, projectsArray, snapshot)
	if err := s.enrichProjectsWithTagsInternal(ctx, items); err != nil {
		return pagination.FilterResult[project.Details]{}, err
	}
	updatesFiltered := strings.TrimSpace(params.Filters["updates"]) != ""
	if updatesFiltered {
		s.preloadGitOpsComposePathsInternal(ctx, env, projectsArray)
		s.enrichProjectsWithUpdateInfoInternal(ctx, projectsArray, items, true, env)
		items = s.appendDiscoveredComposeProjectUpdatesInternal(ctx, params, projectsArray, items, snapshot)
	}

	result := s.buildProjectDerivedPaginationConfigInternal().SearchOrderAndPaginate(items, withoutProjectDBFiltersInternal(params))

	byID := make(map[string]Project, len(projectsArray))
	for _, proj := range projectsArray {
		byID[proj.ID] = proj
	}
	pageProjects := make([]Project, 0, len(result.Items))
	pageDetails := make([]project.Details, 0, len(result.Items))
	pageIndexes := make([]int, 0, len(result.Items))
	for i, item := range result.Items {
		proj, tracked := byID[item.ID]
		if !tracked {
			// Discovered compose rows are built complete.
			continue
		}
		pageProjects = append(pageProjects, proj)
		pageDetails = append(pageDetails, item)
		pageIndexes = append(pageIndexes, i)
	}
	if !updatesFiltered {
		s.preloadGitOpsComposePathsInternal(ctx, env, pageProjects)
		s.enrichProjectsWithUpdateInfoInternal(ctx, pageProjects, pageDetails, true, env)
	}
	s.applyProjectPresentationInternal(ctx, pageProjects, pageDetails, env)
	for k, i := range pageIndexes {
		result.Items[i] = pageDetails[k]
	}
	return result, nil
}

func withoutProjectDBFiltersInternal(params pagination.QueryParams) pagination.QueryParams {
	if _, exists := params.Filters["tags"]; !exists {
		return params
	}
	params.Filters = maps.Clone(params.Filters)
	delete(params.Filters, "tags")
	return params
}

func (s *ProjectService) appendDiscoveredComposeProjectUpdatesInternal(
	ctx context.Context,
	params pagination.QueryParams,
	projectsArray []Project,
	items []project.Details,
	snapshot projectContainerSnapshotInternal,
) []project.Details {
	if !shouldIncludeDiscoveredComposeProjectUpdatesInternal(params) {
		return items
	}
	if snapshot.err != nil {
		slog.WarnContext(ctx, "failed to list compose containers for project update rows", "error", snapshot.err)
		return items
	}

	knownProjectNames := s.buildKnownComposeProjectNameSetInternal(ctx, projectsArray, false)
	discovered := buildDiscoveredComposeProjectUpdateRowsInternal(ctx, snapshot.containers, knownProjectNames, s.imageService, IconCatalogForContext(ctx))
	if len(discovered) == 0 {
		return items
	}

	return append(items, discovered...)
}

func shouldIncludeDiscoveredComposeProjectUpdatesInternal(params pagination.QueryParams) bool {
	if params.Filters == nil {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(params.Filters["updates"]), "has_update") && strings.TrimSpace(params.Filters["tags"]) == ""
}

// buildKnownComposeProjectNameSetInternal collects every project name Arcane
// tracks. projectsArrayIsComplete tells it the caller already loaded the full
// table (not a filtered page), so the catch-all re-query can be skipped.
func (s *ProjectService) buildKnownComposeProjectNameSetInternal(ctx context.Context, projectsArray []Project, projectsArrayIsComplete bool) map[string]struct{} {
	known := make(map[string]struct{}, len(projectsArray)*2)
	for _, proj := range projectsArray {
		addKnownComposeProjectNameInternal(known, proj.Name)
		if proj.ComposeProjectName != nil {
			addKnownComposeProjectNameInternal(known, *proj.ComposeProjectName)
		}
	}

	if s.db == nil || projectsArrayIsComplete {
		return known
	}

	var allProjects []Project
	if err := s.db.WithContext(ctx).Select("name", "compose_project_name").Find(&allProjects).Error; err != nil {
		slog.WarnContext(ctx, "failed to load known project names for compose update discovery", "error", err)
		return known
	}

	for _, proj := range allProjects {
		addKnownComposeProjectNameInternal(known, proj.Name)
		if proj.ComposeProjectName != nil {
			addKnownComposeProjectNameInternal(known, *proj.ComposeProjectName)
		}
	}

	return known
}

func addKnownComposeProjectNameInternal(known map[string]struct{}, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	known[name] = struct{}{}
	if normalized := projects.NormalizeProjectName(name); normalized != "" {
		known[normalized] = struct{}{}
	}
}

func buildDiscoveredComposeProjectUpdateRowsInternal(
	ctx context.Context,
	composeContainers []container.Summary,
	knownProjectNames map[string]struct{},
	imageService *image.ImageService,
	iconCatalog string,
) []project.Details {
	containersByProject := make(map[string][]container.Summary)
	for _, c := range composeContainers {
		if dockerutil.ComposeServiceLabel(c.Labels) == "" {
			continue
		}
		projectName := dockerutil.ComposeProjectLabel(c.Labels)
		if projectName == "" {
			continue
		}
		if _, exists := knownProjectNames[projectName]; exists {
			continue
		}
		if normalized := projects.NormalizeProjectName(projectName); normalized != "" {
			if _, exists := knownProjectNames[normalized]; exists {
				continue
			}
		}

		containersByProject[projectName] = append(containersByProject[projectName], c)
	}

	if len(containersByProject) == 0 {
		return nil
	}

	updateInfoByRef := getRuntimeContainerUpdateInfoByRefInternal(ctx, composeContainers, imageService)
	scoped := getRuntimeContainerUpdateInfoByContainerIDInternal(ctx, composeContainers, imageService)
	rows := make([]project.Details, 0, len(containersByProject))
	for projectName, projectContainers := range containersByProject {
		runtimeServices := buildDiscoveredRuntimeServicesInternal(projectContainers, iconCatalog)
		imageRefs := projects.ImageRefsFromRuntimeServices(runtimeServices)
		checkServices := make([]project.RuntimeService, 0, len(projectContainers))
		for _, c := range projectContainers {
			checkServices = append(checkServices, project.RuntimeService{Name: dockerutil.ComposeServiceLabel(c.Labels), ContainerID: c.ID, Image: c.Image, ContainerLabels: c.Labels})
		}
		updateInfo := BuildUpdateInfoSummary(imageRefs, mergeProjectContainerUpdateInfoInternal(updateInfoByRef, checkServices, scoped))
		if updateInfo == nil || !updateInfo.HasUpdate {
			continue
		}

		runningCount := 0
		for _, runtimeService := range runtimeServices {
			if runtimeService.Status == "running" {
				runningCount++
			}
		}

		lastCheckedAt := ""
		if updateInfo.LastCheckedAt != nil {
			lastCheckedAt = updateInfo.LastCheckedAt.Format(time.RFC3339)
		}

		rows = append(rows, project.Details{
			ID:              "compose:" + projectName,
			Name:            projectName,
			Path:            "",
			Status:          resolveDiscoveredProjectStatusInternal(len(runtimeServices), runningCount),
			ServiceCount:    len(runtimeServices),
			RunningCount:    runningCount,
			IsDiscovered:    true,
			CreatedAt:       lastCheckedAt,
			UpdatedAt:       lastCheckedAt,
			RuntimeServices: runtimeServices,
			UpdateInfo:      updateInfo,
		})
	}

	return rows
}

func getRuntimeContainerUpdateInfoByContainerIDInternal(ctx context.Context, containers []container.Summary, imageService *image.ImageService) map[string]*imagetypes.UpdateInfo {
	if imageService == nil || len(containers) == 0 {
		return nil
	}
	scoped, err := imageService.GetUpdateInfoByContainers(ctx, containers)
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch discovered project tag updates", "error", err)
		return nil
	}
	return scoped
}

func getRuntimeContainerUpdateInfoByRefInternal(
	ctx context.Context,
	composeContainers []container.Summary,
	imageService *image.ImageService,
) map[string]*imagetypes.UpdateInfo {
	if imageService == nil || len(composeContainers) == 0 {
		return nil
	}

	imageRefs := make([]string, 0, len(composeContainers))
	imageIDsByRef := make(map[string][]string, len(composeContainers))
	seenRefs := make(map[string]struct{}, len(composeContainers))
	for _, c := range composeContainers {
		imageRef := strings.TrimSpace(c.Image)
		if imageRef == "" {
			continue
		}
		if _, exists := seenRefs[imageRef]; !exists {
			seenRefs[imageRef] = struct{}{}
			imageRefs = append(imageRefs, imageRef)
		}
		if imageID := strings.TrimSpace(c.ImageID); imageID != "" {
			imageIDsByRef[imageRef] = append(imageIDsByRef[imageRef], imageID)
		}
	}

	updateInfoByRef := make(map[string]*imagetypes.UpdateInfo, len(imageRefs))
	if len(imageRefs) > 0 {
		if refResults, err := imageService.GetUpdateInfoByImageRefs(ctx, imageRefs); err == nil {
			maps.Copy(updateInfoByRef, refResults)
		} else {
			slog.WarnContext(ctx, "failed to fetch compose project update info by image ref", "error", err)
		}
	}

	missingImageIDs := make([]string, 0)
	for _, imageRef := range imageRefs {
		if updateInfoByRef[imageRef] != nil {
			continue
		}
		missingImageIDs = append(missingImageIDs, imageIDsByRef[imageRef]...)
	}

	if len(missingImageIDs) == 0 {
		return updateInfoByRef
	}

	updateInfoByID, err := imageService.GetUpdateInfoByImageIDs(ctx, missingImageIDs)
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch compose project update info by image id", "error", err)
		return updateInfoByRef
	}

	for imageRef, imageIDs := range imageIDsByRef {
		if updateInfoByRef[imageRef] != nil {
			continue
		}
		for _, imageID := range imageIDs {
			if info := updateInfoByID[imageID]; info != nil {
				updateInfoByRef[imageRef] = info
				break
			}
		}
	}

	return updateInfoByRef
}

func buildDiscoveredRuntimeServicesInternal(containers []container.Summary, iconCatalog string) []project.RuntimeService {
	runtimeServices := make([]project.RuntimeService, 0, len(containers))
	seenServices := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		imageRef := strings.TrimSpace(c.Image)
		if imageRef == "" {
			continue
		}

		serviceName := dockerutil.ComposeServiceLabel(c.Labels)
		if serviceName == "" {
			serviceName = c.ID
		}
		key := serviceName + "\x00" + imageRef
		if _, exists := seenServices[key]; exists {
			continue
		}
		seenServices[key] = struct{}{}

		containerName := dockerutil.ContainerNameFromNames(c.Names)

		resolvedIcon := iconcatalog.Resolve(iconCatalog, projects.FindArcaneIconSet(c.Labels))
		runtimeServices = append(runtimeServices, project.RuntimeService{
			Name:            serviceName,
			Image:           imageRef,
			Status:          string(c.State),
			ContainerID:     c.ID,
			ContainerLabels: c.Labels,
			ContainerName:   containerName,
			Ports:           projects.FormatDockerPorts(c.Ports),
			IconLightURL:    resolvedIcon.IconLightURL,
			IconDarkURL:     resolvedIcon.IconDarkURL,
		})
	}

	return runtimeServices
}

func resolveDiscoveredProjectStatusInternal(serviceCount int, runningCount int) string {
	switch {
	case serviceCount == 0:
		return string(ProjectStatusUnknown)
	case runningCount >= serviceCount:
		return string(ProjectStatusRunning)
	case runningCount > 0:
		return string(ProjectStatusPartiallyRunning)
	default:
		return string(ProjectStatusStopped)
	}
}

func (s *ProjectService) buildProjectDerivedPaginationConfigInternal() pagination.Config[project.Details] {
	return pagination.Config[project.Details]{
		SearchAccessors: []pagination.SearchAccessor[project.Details]{
			func(p project.Details) (string, error) { return p.Name, nil },
			func(p project.Details) (string, error) { return p.Path, nil },
			func(p project.Details) (string, error) { return p.RelativePath, nil },
			func(p project.Details) (string, error) { return p.Status, nil },
			func(p project.Details) (string, error) { return p.DirName, nil },
			func(p project.Details) (string, error) {
				names := make([]string, 0, len(p.Tags))
				for _, tag := range p.Tags {
					names = append(names, tag.Name)
				}
				return strings.Join(names, " "), nil
			},
		},
		SortBindings: []pagination.SortBinding[project.Details]{
			{
				Key: "name",
				Fn: func(a, b project.Details) int {
					return strings.Compare(a.Name, b.Name)
				},
			},
			{
				Key: "status",
				Fn: func(a, b project.Details) int {
					return strings.Compare(a.Status, b.Status)
				},
			},
			{
				Key: "serviceCount",
				Fn: func(a, b project.Details) int {
					if a.ServiceCount < b.ServiceCount {
						return -1
					}
					if a.ServiceCount > b.ServiceCount {
						return 1
					}
					return 0
				},
			},
			{
				Key: "path",
				Fn: func(a, b project.Details) int {
					return strings.Compare(a.RelativePath, b.RelativePath)
				},
			},
			{
				Key: "createdAt",
				Fn: func(a, b project.Details) int {
					at, aerr := time.Parse(time.RFC3339, a.CreatedAt)
					bt, berr := time.Parse(time.RFC3339, b.CreatedAt)
					if aerr != nil || berr != nil {
						return strings.Compare(a.CreatedAt, b.CreatedAt)
					}
					if at.Before(bt) {
						return -1
					}
					if at.After(bt) {
						return 1
					}
					return 0
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[project.Details]{
			buildProjectStatusFilterAccessorInternal(),
			buildProjectUpdatesFilterAccessorInternal(),
			buildProjectArchivedFilterAccessorInternal(),
			buildProjectLabelFilterAccessorInternal(),
		},
	}
}

func buildProjectLabelFilterAccessorInternal() pagination.FilterAccessor[project.Details] {
	return pagination.FilterAccessor[project.Details]{
		Key:     "label",
		NoSplit: true,
		Fn: func(p project.Details, filterValue string) bool {
			key, value, hasValue := strings.Cut(filterValue, "=")
			key = strings.TrimSpace(key)
			if key == "" {
				return true
			}
			for _, service := range p.RuntimeServices {
				if actual, ok := service.ContainerLabels[key]; ok && (!hasValue || actual == value) {
					return true
				}
			}
			return false
		},
	}
}

func buildProjectStatusFilterAccessorInternal() pagination.FilterAccessor[project.Details] {
	return pagination.FilterAccessor[project.Details]{
		Key: "status",
		Fn: func(p project.Details, filterValue string) bool {
			return strings.EqualFold(strings.TrimSpace(p.Status), strings.TrimSpace(filterValue))
		},
	}
}

func buildProjectUpdatesFilterAccessorInternal() pagination.FilterAccessor[project.Details] {
	return pagination.FilterAccessor[project.Details]{
		Key: "updates",
		Fn: func(p project.Details, filterValue string) bool {
			return strings.EqualFold(strings.TrimSpace(getProjectUpdateStatusInternal(p.UpdateInfo)), strings.TrimSpace(filterValue))
		},
	}
}

func buildProjectArchivedFilterAccessorInternal() pagination.FilterAccessor[project.Details] {
	return pagination.FilterAccessor[project.Details]{
		Key: "archived",
		Fn: func(p project.Details, filterValue string) bool {
			if strings.EqualFold(strings.TrimSpace(filterValue), "all") {
				return true
			}
			archived, _ := utils.ParseBool(filterValue)
			return p.IsArchived == archived
		},
	}
}

func getProjectUpdateStatusInternal(updateInfo *project.UpdateInfo) string {
	if updateInfo == nil || strings.TrimSpace(updateInfo.Status) == "" {
		return "unknown"
	}

	return updateInfo.Status
}

// CountProjectsWithPendingUpdates counts non-archived projects with at
// least one visible service with an image update pending, plus compose projects running on the daemon
// that Arcane does not track. It deliberately avoids the project-list pipeline:
// that path builds full project DTOs (live status, icons, URLs, GitOps lookups)
// and then throws all of them away for a single number, costing several full
// container lists and a compose parse per project on every dashboard load.
//
// allContainers is the caller's already-fetched container list; pass nil to have
// it fetched here.
func (s *ProjectService) CountProjectsWithPendingUpdates(ctx context.Context, allContainers []container.Summary) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	if allContainers == nil {
		var err error
		allContainers, err = s.listGlobalComposeContainersInternal(ctx)
		if err != nil {
			return 0, errors.WrapIf(err, "failed to list containers for project update count")
		}
	}

	// One full scan: archived projects are excluded from the update count but
	// still mark their compose stacks as known during discovery, so loading
	// everything here saves the known-name pass its own table scan.
	var allProjects []Project
	if err := s.db.WithContext(ctx).Find(&allProjects).Error; err != nil {
		return 0, errors.WrapIf(err, "failed to list projects for update count")
	}

	activeProjects := make([]Project, 0, len(allProjects))
	for _, proj := range allProjects {
		if !proj.IsArchived {
			activeProjects = append(activeProjects, proj)
		}
	}

	// Runtime container IDs keep tag-policy updates scoped to their project.
	details := make([]project.Details, len(activeProjects))
	containersByProject := groupComposeContainersByProjectInternal(allContainers)
	for i, proj := range activeProjects {
		details[i].ID = proj.ID
		for _, c := range lookupProjectContainersInternal(proj, containersByProject) {
			details[i].RuntimeServices = append(details[i].RuntimeServices, project.RuntimeService{Name: dockerutil.ComposeServiceLabel(c.Labels), ContainerID: c.ID, Image: c.Image, ContainerLabels: c.Labels})
		}
	}
	s.enrichProjectsWithUpdateInfoInternal(ctx, activeProjects, details, false, nil)

	count := 0
	for i := range details {
		if details[i].UpdateInfo != nil && details[i].UpdateInfo.HasUpdate {
			count++
		}
	}

	visibleContainers := make([]container.Summary, 0, len(allContainers))
	for _, c := range allContainers {
		hidden, _ := utils.ParseBool(c.Labels[libarcane.HiddenResourceLabel])
		if !hidden {
			visibleContainers = append(visibleContainers, c)
		}
	}
	return count + s.countDiscoveredComposeProjectUpdatesInternal(ctx, allProjects, true, visibleContainers), nil
}

// countDiscoveredComposeProjectUpdatesInternal counts compose projects running on
// the daemon that Arcane does not track but that have a pending image update, so
// the dashboard badge matches the projects table. Errors are logged and counted
// as zero: a missing container list should degrade the badge, not fail the load.
func (s *ProjectService) countDiscoveredComposeProjectUpdatesInternal(ctx context.Context, projectsArray []Project, projectsArrayIsComplete bool, allContainers []container.Summary) int {
	if allContainers == nil {
		var err error
		allContainers, err = s.listGlobalComposeContainersInternal(ctx)
		if err != nil {
			slog.WarnContext(ctx, "failed to list compose containers for project update count", "error", err)
			return 0
		}
	}

	composeContainers := make([]container.Summary, 0, len(allContainers))
	for _, c := range allContainers {
		if dockerutil.ComposeProjectLabel(c.Labels) != "" {
			composeContainers = append(composeContainers, c)
		}
	}
	if len(composeContainers) == 0 {
		return 0
	}

	knownProjectNames := s.buildKnownComposeProjectNameSetInternal(ctx, projectsArray, projectsArrayIsComplete)
	// Only rows with a pending update are returned, so the length is the count.
	return len(buildDiscoveredComposeProjectUpdateRowsInternal(ctx, composeContainers, knownProjectNames, s.imageService, IconCatalogForContext(ctx)))
}

// projectContainerSnapshotInternal is the compose container listing shared by
// every row of one list request, grouped once by compose project name.
type projectContainerSnapshotInternal struct {
	containers          []container.Summary
	byProject           map[string][]container.Summary
	err                 error
	currentContainerID  string
	currentContainerErr error
}

func (s *ProjectService) projectContainerSnapshotInternal(ctx context.Context) projectContainerSnapshotInternal {
	containers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers", "error", err)
		return projectContainerSnapshotInternal{err: err}
	}
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	return projectContainerSnapshotInternal{
		containers:          containers,
		byProject:           groupComposeContainersByProjectInternal(containers),
		currentContainerID:  currentContainerID,
		currentContainerErr: currentContainerErr,
	}
}

// fetchProjectStatusConcurrently builds complete list rows for an already
// paginated page: live status from a single Docker API call plus the
// compose-backed presentation fields. metaEnv is resolved once for the whole
// list: ProjectMetadata would otherwise re-stat the projects directory,
// re-clone settings, and re-query GitOps compose paths per project.
func (s *ProjectService) fetchProjectStatusConcurrently(ctx context.Context, projectsList []Project, metaEnv *projectMetadataEnvInternal) []project.Details {
	results := s.projectListRowsInternal(ctx, metaEnv.projectsDirectory, projectsList, s.projectContainerSnapshotInternal(ctx))
	s.applyProjectPresentationInternal(ctx, projectsList, results, metaEnv)
	return results
}

func (s *ProjectService) resolveProjectMetadataConcurrentlyInternal(ctx context.Context, projectsList []Project, metaEnv *projectMetadataEnvInternal) []projects.ArcaneComposeMetadata {
	metas := make([]projects.ArcaneComposeMetadata, len(projectsList))
	var g errgroup.Group
	g.SetLimit(maxConcurrentComposeReads)
	for i := range projectsList {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "project metadata worker", "projectID", projectsList[i].ID)
			metas[i] = s.ProjectMetadata(ctx, projectsList[i], metaEnv)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		slog.WarnContext(ctx, "project metadata resolution failed", "error", err)
	}
	return metas
}

// applyProjectPresentationInternal resolves compose metadata for projectsList
// and fills the fields filtering and sorting never read: project and service
// icons, custom URLs, and the env access probe. details must align with
// projectsList by index.
func (s *ProjectService) applyProjectPresentationInternal(ctx context.Context, projectsList []Project, details []project.Details, metaEnv *projectMetadataEnvInternal) {
	if len(projectsList) == 0 {
		return
	}
	metas := s.resolveProjectMetadataConcurrentlyInternal(ctx, projectsList, metaEnv)
	catalog := IconCatalogForContext(ctx)
	for i := range projectsList {
		resp := &details[i]
		applyResolvedProjectIconInternal(resp, iconcatalog.Resolve(catalog, metas[i].ProjectIcon))
		resp.URLs = metas[i].ProjectURLS
		resp.ConfigurationError = projects.CheckProjectEnvAccess(ctx, metaEnv.projectsDirectory, projectsList[i].Path)
		for k := range resp.RuntimeServices {
			service := &resp.RuntimeServices[k]
			icon := resolveServiceIconInternal(catalog, service.ContainerLabels, service.Name, metas[i])
			service.IconLightURL, service.IconDarkURL = icon.IconLightURL, icon.IconDarkURL
		}
	}
}

func (s *ProjectService) projectListRowsInternal(ctx context.Context, projectsDir string, projectsList []Project, snapshot projectContainerSnapshotInternal) []project.Details {
	rows := make([]project.Details, len(projectsList))
	inferredCounts := make(map[string]int)
	for i, p := range projectsList {
		rows[i] = projectListRowInternal(ctx, projectsDir, p, snapshot)
		if p.ServiceCount == 0 && rows[i].ServiceCount > 0 {
			inferredCounts[p.ID] = rows[i].ServiceCount
		}
	}
	s.persistInferredServiceCountsInternal(ctx, inferredCounts)
	return rows
}

// persistInferredServiceCountsInternal writes service counts inferred from live
// containers back to projects whose stored count is still zero. The plain list
// path sorts and paginates on the service_count column in SQL before rows are
// enriched, so a count that only lives in the response leaves those projects
// on the wrong page until a filesystem sync parses their compose file. The
// write runs synchronously on the request context as one batched statement
// per chunk: it only fires for projects that still have a zero count, so it is
// cheap and needs no detached goroutine. Failures are logged; the response
// already carries the inferred value.
func (s *ProjectService) persistInferredServiceCountsInternal(ctx context.Context, counts map[string]int) {
	if len(counts) == 0 || s.db == nil {
		return
	}
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	for chunk := range slices.Chunk(ids, inferredServiceCountBatchSizeInternal) {
		var caseExpr strings.Builder
		args := make([]any, 0, 2*len(chunk))
		caseExpr.WriteString("CASE id")
		for _, id := range chunk {
			caseExpr.WriteString(" WHEN ? THEN ?")
			args = append(args, id, counts[id])
		}
		caseExpr.WriteString(" ELSE service_count END")
		if err := s.db.WithContext(ctx).Model(&Project{}).
			Where("id IN ? AND service_count = 0", chunk).
			Update("service_count", gorm.Expr(caseExpr.String(), args...)).Error; err != nil {
			slog.WarnContext(ctx, "failed to persist inferred project service counts", "count", len(chunk), "error", err)
			return
		}
	}
}

// inferredServiceCountBatchSizeInternal bounds the ids in one inferred
// service count update so the statement stays under SQLite's bound variable
// limit (two placeholders per id in the CASE plus one in the IN list).
const inferredServiceCountBatchSizeInternal = 200

// projectListRowInternal builds the fields the list filters, search, and sort
// read from database columns and the shared container snapshot. Fields that
// need compose metadata are left for applyProjectPresentationInternal. When
// the container listing failed the row reports an unknown status.
func projectListRowInternal(ctx context.Context, projectsDir string, p Project, snapshot projectContainerSnapshotInternal) project.Details {
	var resp project.Details
	_ = mapper.MapStruct(p, &resp)

	resp.CreatedAt = p.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = p.UpdatedAt.Format(time.RFC3339)
	resp.IsArchived = p.IsArchived
	resp.ArchivedAt = p.ArchivedAt
	resp.DirName = mo.PointerToOption(p.DirName).OrEmpty()
	resp.RelativePath = getProjectRelativePathInternal(projectsDir, p.Path)
	resp.GitOpsManagedBy = p.GitOpsManagedBy
	resp.HasBuildDirective = p.BuildImageRefsJSON != nil && len(projects.ParseImageRefsJSON(*p.BuildImageRefsJSON)) > 0
	// Use DB service count as the source of truth for "Total Services"
	// since we are not parsing the YAML here.
	resp.ServiceCount = p.ServiceCount
	if snapshot.err != nil {
		resp.Status = string(ProjectStatusUnknown)
		return resp
	}

	projectContainers := lookupProjectContainersInternal(p, snapshot.byProject)
	services := make([]ProjectServiceInfo, 0, len(projectContainers))
	for _, c := range projectContainers {
		service := projectServiceInfoFromContainerInternal(ctx, c, projects.ArcaneComposeMetadata{}, snapshot.currentContainerID, snapshot.currentContainerErr)
		if service.RedeployDisabled {
			resp.RedeployDisabled = true
		}
		services = append(services, service)
	}
	_, runningCount := getServiceCounts(services)
	resp.RuntimeServices = buildProjectRuntimeServicesInternal(services)
	resp.RunningCount = runningCount
	// Newly discovered projects have no persisted count yet. Infer it from the
	// live containers; projectListRowsInternal persists the inferred value so
	// SQL sorting and pagination on service_count see it on later requests.
	if resp.ServiceCount == 0 && len(services) > 0 {
		resp.ServiceCount = len(services)
	}

	// Calculate Status using actual container count from Docker rather than the
	// (potentially stale) DB ServiceCount. The DB value can become outdated when
	// a service is removed from the compose file but compose parsing fails during
	// filesystem sync, leaving the old count in the database. This mirrors the
	// logic in calculateProjectStatus and GetProjectDetails, which both use the
	// live container/service list as the source of truth.
	actualServiceCount := len(services)
	if actualServiceCount == 0 {
		resp.Status = string(ProjectStatusStopped)
	} else {
		switch {
		case runningCount >= actualServiceCount:
			resp.Status = string(ProjectStatusRunning)
		case runningCount > 0:
			resp.Status = string(ProjectStatusPartiallyRunning)
		default:
			resp.Status = string(ProjectStatusStopped)
		}
	}

	return resp
}

// ProjectMetadata resolves a project's icon sets and service URLs.
// Results are cached until any compose file the parse merged (root, COMPOSE_FILE
// entries, override, includes) or any env file it read changes on disk,
// because deriving them is expensive (compose load with
// interpolation and .env reads, plus a gitops_syncs query for GitOps-managed
// projects) and every project row on the list page needs it.
//
// env may be nil, in which case the projects directory and autoInjectEnv setting
// are resolved here; callers iterating over many projects should resolve them
// once and pass them in.
func (s *ProjectService) ProjectMetadata(ctx context.Context, p Project, env *projectMetadataEnvInternal) projects.ArcaneComposeMetadata {
	empty := projects.ArcaneComposeMetadata{ServiceIconSets: map[string]projects.IconSet{}}

	if env == nil {
		env = s.newProjectMetadataEnvInternal(ctx, []Project{p})
	}
	composeFile, err := s.resolveProjectComposeFileInternal(ctx, &p, env)
	if err != nil {
		return empty
	}

	fingerprint := fmt.Sprintf("%q|%q|%t", composeFile, env.projectsDirectory, env.autoInjectEnv)
	if s.metaCache != nil && p.ID != "" {
		if cached, ok := s.metaCache.Get(p.ID, fingerprint); ok {
			return cached
		}
	}

	meta, err := projects.ParseArcaneComposeMetadata(ctx, composeFile, env.projectsDirectory, env.autoInjectEnv)
	if err != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFile, "error", err)
		return empty
	}

	if s.metaCache == nil || p.ID == "" {
		return meta
	}
	if err := s.metaCache.Set(p.ID, fingerprint, p.Path, env.projectsDirectory, composeFile, meta.ComposeFiles, meta.EnvFiles, meta); err != nil {
		slog.DebugContext(ctx, "failed to cache Compose metadata", "projectID", p.ID, "error", err)
	}

	return meta
}

// IconCatalogForContext resolves the icon catalog of the requesting
// user. On agent-proxied calls the caller is a synthetic user whose preference
// is populated from the X-Arcane-Icon-Catalog header the manager forwards.
// Background jobs have no user attached and fall back to the default catalog.
func IconCatalogForContext(ctx context.Context) string {
	if u, ok := common.CurrentUserFromContext(ctx); ok && u != nil && u.Preferences.IconCatalog != nil && *u.Preferences.IconCatalog != "" {
		return *u.Preferences.IconCatalog
	}
	return iconcatalog.DefaultCatalog
}

func applyResolvedProjectIconInternal(resp *project.Details, icon iconcatalog.ResolvedIconSet) {
	if resp == nil {
		return
	}
	resp.IconLightURL = icon.IconLightURL
	resp.IconDarkURL = icon.IconDarkURL
}
