package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"log/slog"
	"maps"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
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

// lookupProjectContainers returns containers matched to a project, trying the
// normalized directory name first and falling back to the effective compose
// project name (from COMPOSE_PROJECT_NAME) when it differs.
func lookupProjectContainers(p Project, containersByProject map[string][]container.Summary) []container.Summary {
	normName := projects.NormalizeProjectName(p.Name)
	if c := containersByProject[normName]; len(c) > 0 {
		return c
	}
	if p.ComposeProjectName != nil && *p.ComposeProjectName != normName {
		return containersByProject[*p.ComposeProjectName]
	}
	return nil
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
		projectContainers := lookupProjectContainers(p, containersByProject)

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
	if params.Filters != nil {
		statusFilter = strings.TrimSpace(params.Filters["status"])
		updatesFilter = strings.TrimSpace(params.Filters["updates"])
		archivedFilter = strings.TrimSpace(params.Filters["archived"])
		tagsFilter = strings.TrimSpace(params.Filters["tags"])
	}
	query = applyProjectArchivedDBFilterInternal(query, archivedFilter)
	query = applyProjectTagsDBFilterInternal(query, tagsFilter)
	if statusFilter != "" || updatesFilter != "" {
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
	result := s.fetchProjectStatusConcurrently(ctx, projectsArray)
	if err := s.enrichProjectsWithTagsInternal(ctx, result); err != nil {
		return nil, pagination.Response{}, err
	}
	s.enrichProjectsWithUpdateInfoInternal(ctx, projectsArray, result)

	slog.DebugContext(ctx, "Completed ListProjects request",
		"result_count", len(result))

	return result, paginationResp, nil
}

func applyProjectArchivedDBFilterInternal(query *gorm.DB, filterValue string) *gorm.DB {
	switch strings.ToLower(strings.TrimSpace(filterValue)) {
	case "true":
		return query.Where("is_archived = ?", true)
	case "all":
		return query
	default:
		return query.Where("is_archived = ?", false)
	}
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

	items := s.fetchProjectStatusConcurrently(ctx, projectsArray)
	if err := s.enrichProjectsWithTagsInternal(ctx, items); err != nil {
		return pagination.FilterResult[project.Details]{}, err
	}
	s.enrichProjectsWithUpdateInfoInternal(ctx, projectsArray, items)
	items = s.appendDiscoveredComposeProjectUpdatesInternal(ctx, params, projectsArray, items)

	return pagination.SearchOrderAndPaginate(items, withoutProjectDBFiltersInternal(params), s.buildProjectDerivedPaginationConfigInternal()), nil
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
) []project.Details {
	if !shouldIncludeDiscoveredComposeProjectUpdatesInternal(params) {
		return items
	}

	composeContainers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to list compose containers for project update rows", "error", err)
		return items
	}

	knownProjectNames := s.buildKnownComposeProjectNameSetInternal(ctx, projectsArray, false)
	discovered := buildDiscoveredComposeProjectUpdateRowsInternal(ctx, composeContainers, knownProjectNames, s.imageService, IconCatalogForContext(ctx))
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
	rows := make([]project.Details, 0, len(containersByProject))
	for projectName, projectContainers := range containersByProject {
		runtimeServices := buildDiscoveredRuntimeServicesInternal(projectContainers, iconCatalog)
		imageRefs := projects.ImageRefsFromRuntimeServices(runtimeServices)
		updateInfo := buildProjectUpdateInfoSummaryInternal(imageRefs, updateInfoByRef)
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
			Name:          serviceName,
			Image:         imageRef,
			Status:        string(c.State),
			ContainerID:   c.ID,
			ContainerName: containerName,
			Ports:         projects.FormatDockerPorts(c.Ports),
			IconLightURL:  resolvedIcon.IconLightURL,
			IconDarkURL:   resolvedIcon.IconDarkURL,
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
			switch strings.ToLower(strings.TrimSpace(filterValue)) {
			case "true":
				return p.IsArchived
			case "all":
				return true
			default:
				return !p.IsArchived
			}
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
// least one image update pending, plus compose projects running on the daemon
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

	// enrichProjectsWithUpdateInfoInternal keys off Details.ID, so the summaries
	// only need identity — no status, icons or URLs are read here.
	details := make([]project.Details, len(activeProjects))
	for i, proj := range activeProjects {
		details[i].ID = proj.ID
	}
	s.enrichProjectsWithUpdateInfoInternal(ctx, activeProjects, details)

	count := 0
	for i := range details {
		if details[i].UpdateInfo != nil && details[i].UpdateInfo.HasUpdate {
			count++
		}
	}

	return count + s.countDiscoveredComposeProjectUpdatesInternal(ctx, allProjects, true, allContainers), nil
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

// fetchProjectStatusConcurrently fetches live Docker status for multiple projects in parallel
// Optimized to use a single Docker API call instead of N calls + N file reads
func (s *ProjectService) fetchProjectStatusConcurrently(ctx context.Context, projectsList []Project) []project.Details {
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for relative project paths", "error", err)
	}

	// Resolved once for the whole list: ProjectMetadata would
	// otherwise re-stat the projects directory and re-clone settings per project.
	metaEnv := &projectMetadataEnvInternal{
		projectsDirectory: projectsDir,
		autoInjectEnv:     s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
	}

	// 1. Fetch all compose containers in one go
	containers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers", "error", err)
		// Fallback: return basic info with unknown status
		results := make([]project.Details, len(projectsList))
		for i, p := range projectsList {
			_ = mapper.MapStruct(p, &results[i])
			results[i].CreatedAt = p.CreatedAt.Format(time.RFC3339)
			results[i].UpdatedAt = p.UpdatedAt.Format(time.RFC3339)
			results[i].DirName = mo.PointerToOption(p.DirName).OrEmpty()
			results[i].RelativePath = getProjectRelativePathInternal(projectsDir, p.Path)
			results[i].GitOpsManagedBy = p.GitOpsManagedBy
			results[i].HasBuildDirective = p.BuildImageRefsJSON != nil && len(projects.ParseImageRefsJSON(*p.BuildImageRefsJSON)) > 0
			meta := s.ProjectMetadata(ctx, p, metaEnv)
			applyResolvedProjectIconInternal(&results[i], iconcatalog.Resolve(IconCatalogForContext(ctx), meta.ProjectIcon))
			results[i].URLs = meta.ProjectURLS
			results[i].Status = string(ProjectStatusUnknown)
		}
		return results
	}

	// 2. Group containers by project name
	containersByProject := groupComposeContainersByProjectInternal(containers)

	// 3. Map to DTOs
	results := make([]project.Details, len(projectsList))
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	for i, p := range projectsList {
		results[i] = s.mapProjectToDto(ctx, projectsDir, p, containersByProject, currentContainerID, currentContainerErr, metaEnv)
	}

	return results
}

func (s *ProjectService) mapProjectToDto(ctx context.Context, projectsDir string, p Project, containersByProject map[string][]container.Summary, currentContainerID string, currentContainerErr error, metaEnv *projectMetadataEnvInternal) project.Details {
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
	meta := s.ProjectMetadata(ctx, p, metaEnv)
	applyResolvedProjectIconInternal(&resp, iconcatalog.Resolve(IconCatalogForContext(ctx), meta.ProjectIcon))
	resp.URLs = meta.ProjectURLS

	projectContainers := lookupProjectContainers(p, containersByProject)

	services := make([]ProjectServiceInfo, 0, len(projectContainers))

	for _, c := range projectContainers {
		svcName := dockerutil.ComposeServiceLabel(c.Labels)
		state := c.State // "running", "exited", etc.

		// Parse health from Status string if possible
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

		containerName := dockerutil.ContainerNameFromNames(c.Names)

		redeployDisabled := labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr)
		if redeployDisabled {
			resp.RedeployDisabled = true
		}

		resolvedIcon := iconcatalog.Resolve(IconCatalogForContext(ctx), iconcatalog.FirstNonEmpty(
			projects.FindArcaneIconSet(c.Labels),
			meta.ServiceIconSets[svcName],
			meta.ProjectIcon,
		))
		services = append(services, ProjectServiceInfo{
			Name:             svcName,
			Image:            c.Image,
			Status:           string(state),
			ContainerID:      c.ID,
			ContainerName:    containerName,
			Ports:            projects.FormatDockerPorts(c.Ports),
			Health:           health,
			IconLightURL:     resolvedIcon.IconLightURL,
			IconDarkURL:      resolvedIcon.IconDarkURL,
			Labels:           c.Labels,
			RedeployDisabled: redeployDisabled,
		})
	}
	_, runningCount := getServiceCounts(services)

	// Convert to RuntimeServices
	runtimeServices := make([]project.RuntimeService, len(services))
	for k, s := range services {
		runtimeServices[k] = project.RuntimeService{
			Name:             s.Name,
			Image:            s.Image,
			Status:           s.Status,
			ContainerID:      s.ContainerID,
			ContainerName:    s.ContainerName,
			Ports:            s.Ports,
			Health:           s.Health,
			IconLightURL:     s.IconLightURL,
			IconDarkURL:      s.IconDarkURL,
			ServiceConfig:    s.ServiceConfig,
			RedeployDisabled: s.RedeployDisabled,
		}
	}
	resp.RuntimeServices = runtimeServices

	// Use DB service count as the source of truth for "Total Services"
	// since we are not parsing the YAML here.
	resp.ServiceCount = p.ServiceCount
	resp.RunningCount = runningCount
	if resp.ServiceCount == 0 && len(services) > 0 {
		resp.ServiceCount = len(services)
		// Persist the inferred count so later list loads do not need compose parsing.
		go func(ctx context.Context, pid string, count int) {
			s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", pid).Update("service_count", count)
		}(context.WithoutCancel(ctx), p.ID, resp.ServiceCount)
	}

	// For missing service count (e.g. newly discovered projects), skip the
	// expensive CountServicesFromCompose call which loads and parses the entire
	// compose project. The count will be populated the next time the project
	// detail endpoint is called or during the periodic filesystem sync.

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
// Results are cached for projectMetadataTTL because deriving them is expensive
// (compose load with interpolation and .env reads, plus a gitops_syncs query for
// GitOps-managed projects) and every project row on the list page needs it.
//
// env may be nil, in which case the projects directory and autoInjectEnv setting
// are resolved here; callers iterating over many projects should resolve them
// once and pass them in.
func (s *ProjectService) ProjectMetadata(ctx context.Context, p Project, env *projectMetadataEnvInternal) projects.ArcaneComposeMetadata {
	if s.metaCache != nil && p.ID != "" {
		if meta, ok, _ := s.metaCache.Get(p.ID); ok {
			return meta
		}
	}

	empty := projects.ArcaneComposeMetadata{ServiceIconSets: map[string]projects.IconSet{}}

	composeFile, err := s.ResolveProjectComposeFile(ctx, &p)
	if err != nil {
		return empty
	}

	if env == nil {
		projectsDirectory, projectsDirErr := s.GetProjectsDirectory(ctx)
		if projectsDirErr != nil {
			slog.WarnContext(ctx, "failed to resolve projects directory for Arcane compose metadata", "path", composeFile, "error", projectsDirErr)
		}
		env = &projectMetadataEnvInternal{
			projectsDirectory: projectsDirectory,
			autoInjectEnv:     s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
		}
	}

	meta, err := projects.ParseArcaneComposeMetadata(ctx, composeFile, env.projectsDirectory, env.autoInjectEnv)
	if err != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFile, "error", err)
		return empty
	}

	if s.metaCache != nil && p.ID != "" {
		s.metaCache.Set(p.ID, meta)
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
