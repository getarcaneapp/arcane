// Package listing projects stored projects and live Compose containers into
// paginated list rows, status counts and untracked-project update rows.
package listing

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
)

// Service projects stored projects and live Compose containers into list
// rows, status counts and update-only rows for untracked Compose projects.
type Service struct {
	containers    func(ctx context.Context) ([]container.Summary, error)
	imageService  *image.ImageService
	summarizeRuns func(imageRefs []string, services []types.ProjectRuntimeService, scoped map[string]*types.ImageUpdateInfo) *types.ProjectUpdateInfo
}

func New(
	containers func(ctx context.Context) ([]container.Summary, error),
	imageService *image.ImageService,
	summarizeRuns func(imageRefs []string, services []types.ProjectRuntimeService, scoped map[string]*types.ImageUpdateInfo) *types.ProjectUpdateInfo,
) *Service {
	return &Service{
		containers:    containers,
		imageService:  imageService,
		summarizeRuns: summarizeRuns,
	}
}

// Snapshot is the compose container listing shared by every row of one list request, grouped by project.
type Snapshot struct {
	Containers          []container.Summary
	byProject           map[string][]container.Summary
	Err                 error
	currentContainerID  string
	currentContainerErr error
}

// Snapshot lists compose containers once for every row of a list request.
func (s *Service) Snapshot(ctx context.Context) Snapshot {
	containers, err := s.containers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers", "error", err)
		return Snapshot{Err: err}
	}
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	return Snapshot{
		Containers:          containers,
		byProject:           GroupComposeContainersByProject(containers),
		currentContainerID:  currentContainerID,
		currentContainerErr: currentContainerErr,
	}
}

// ProjectContainers matches project names first, then the working directory if it identifies one Compose project.
func ProjectContainers(p types.ProjectRecord, containersByProject map[string][]container.Summary) []container.Summary {
	normName := projects.NormalizeProjectName(p.Name)
	if c := containersByProject[normName]; len(c) > 0 {
		return c
	}
	if kit.FromPtr(p.ComposeProjectName) != normName {
		if c := containersByProject[kit.FromPtr(p.ComposeProjectName)]; len(c) > 0 {
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
			workingDir := c.Labels[api.WorkingDirLabel]
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

// RuntimeServiceFromContainer derives service state from a labeled container.
func RuntimeServiceFromContainer(catalog string, c container.Summary, meta projects.ArcaneComposeMetadata, currentContainerID string, currentContainerErr error) types.ProjectRuntimeService {
	resolvedIcon := iconcatalog.Resolve(catalog, cmp.Or(projects.FindArcaneIconSet(c.Labels), meta.ServiceIconSets[docker.ComposeServiceLabel(c.Labels)], meta.ProjectIcon))
	return types.ProjectRuntimeService{
		Name:          docker.ComposeServiceLabel(c.Labels),
		Image:         c.Image,
		Status:        string(c.State),
		ContainerID:   c.ID,
		ContainerName: docker.ContainerNameFromNames(c.Names),
		Ports:         projects.FormatDockerPorts(c.Ports),
		Health: func() *string {
			switch status := strings.ToLower(c.Status); {
			case strings.Contains(status, "(healthy)"):
				return new("healthy")
			case strings.Contains(status, "(unhealthy)"):
				return new("unhealthy")
			case strings.Contains(status, "(starting)"):
				return new("starting")
			}
			return nil
		}(),
		IconLightURL:     resolvedIcon.IconLightURL,
		IconDarkURL:      resolvedIcon.IconDarkURL,
		ContainerLabels:  c.Labels,
		RedeployDisabled: labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr),
		ImageID:          c.ImageID,
	}
}

// ServiceCounts returns the total and running service counts.
func ServiceCounts(services []types.ProjectRuntimeService) (total, running int) {
	total = len(services)
	for _, service := range services {
		st := strings.ToLower(strings.TrimSpace(service.Status))
		if st == "running" || st == "up" {
			running++
		}
	}
	return total, running
}

// ProjectStatus derives a project status from its runtime services.
func ProjectStatus(services []types.ProjectRuntimeService) string {
	if len(services) == 0 {
		return types.ProjectStatusUnknown
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
		return types.ProjectStatusRunning
	}
	if runningCount > 0 {
		return types.ProjectStatusPartiallyRunning
	}
	return kit.Ternary(stoppedCount > 0, types.ProjectStatusStopped, types.ProjectStatusUnknown)
}

// StatusCounts totals archived, running and stopped projects from live
// containers, falling back to stored status when Docker is unavailable.
func (s *Service) StatusCounts(ctx context.Context, records []types.ProjectRecord) types.ProjectStatusCounts {
	containers, err := s.containers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers for counts", "error", err)
	}
	containersByProject := GroupComposeContainersByProject(containers)
	var archived, running, stopped int
	for _, p := range records {
		if p.IsArchived {
			archived++
			continue
		}
		switch func() string {
			if err != nil || p.Status == types.ProjectStatusDeploying || p.Status == types.ProjectStatusRestarting {
				return p.Status
			}
			var services []types.ProjectRuntimeService
			for _, c := range ProjectContainers(p, containersByProject) {
				services = append(services, types.ProjectRuntimeService{Status: string(c.State)})
			}
			return kit.Ternary(len(services) > 0, ProjectStatus(services), types.ProjectStatusStopped)
		}() {
		case types.ProjectStatusRunning, types.ProjectStatusPartiallyRunning, types.ProjectStatusDeploying, types.ProjectStatusRestarting:
			running++
		case types.ProjectStatusStopped, types.ProjectStatusStopping:
			stopped++
		}
	}
	return types.ProjectStatusCounts{
		TotalProjects:    len(records),
		RunningProjects:  running,
		StoppedProjects:  stopped,
		ArchivedProjects: archived,
	}
}

// RelativePath returns projectPath relative to projectsDir, or "" outside it.
func RelativePath(projectsDir, projectPath string) string {
	if strings.TrimSpace(projectsDir) == "" || !projects.IsSafeSubdirectory(projectsDir, projectPath) || filepath.Clean(projectPath) == filepath.Clean(projectsDir) {
		return ""
	}
	relativePath, err := filepath.Rel(projectsDir, filepath.Clean(projectPath))
	return kit.Ternary(err == nil, filepath.ToSlash(relativePath), "")
}

// row builds the fields list filters, search and sort read from database
// columns and the container snapshot; compose metadata is left for ApplyPresentation.
func row(projectsDir, catalog string, p types.ProjectRecord, snapshot Snapshot, tags []types.ProjectTag) types.Project {
	var services []types.ProjectRuntimeService
	for _, c := range ProjectContainers(p, snapshot.byProject) {
		services = append(services, RuntimeServiceFromContainer(catalog, c, projects.ArcaneComposeMetadata{}, snapshot.currentContainerID, snapshot.currentContainerErr))
	}
	_, runningCount := ServiceCounts(services)
	return types.Project{
		Tags:            tags,
		StatusReason:    p.StatusReason,
		GitOpsManagedBy: p.GitOpsManagedBy,
		ArchivedAt:      p.ArchivedAt,
		// Status uses the live services, not the possibly stale stored ServiceCount.
		Status: func() string {
			switch {
			case p.Status == types.ProjectStatusDeploying || p.Status == types.ProjectStatusRestarting:
				return p.Status
			case snapshot.Err != nil:
				return types.ProjectStatusUnknown
			case len(services) == 0:
				return types.ProjectStatusStopped
			}
			return ProjectStatus(services)
		}(),
		DirName:         mo.PointerToOption(p.DirName).OrEmpty(),
		Name:            p.Name,
		RelativePath:    RelativePath(projectsDir, p.Path),
		ID:              p.ID,
		Path:            p.Path,
		UpdatedAt:       p.UpdatedAt.Format(time.RFC3339),
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
		RuntimeServices: services,
		RunningCount:    kit.Ternary(snapshot.Err != nil, p.RunningCount, runningCount),
		// Newly discovered projects have no persisted count yet; infer it from live containers so the parent can persist it.
		ServiceCount:      cmp.Or(p.ServiceCount, len(services)),
		IsArchived:        p.IsArchived,
		HasBuildDirective: len(projects.ParseImageRefsJSON(kit.FromPtr(p.BuildImageRefsJSON))) > 0,
		RedeployDisabled:  slices.ContainsFunc(services, func(service types.ProjectRuntimeService) bool { return service.RedeployDisabled }),
	}
}

// Rows builds list rows and reports service counts inferred from live
// containers for projects whose stored count is still zero.
func Rows(projectsDir, catalog string, records []types.ProjectRecord, snapshot Snapshot, tagsByProject map[string][]types.ProjectTag) ([]types.Project, map[string]int) {
	rows := make([]types.Project, len(records))
	inferredCounts := make(map[string]int)
	for i, p := range records {
		rows[i] = row(projectsDir, catalog, p, snapshot, tagsByProject[p.ID])
		if p.ServiceCount == 0 && rows[i].ServiceCount > 0 {
			inferredCounts[p.ID] = rows[i].ServiceCount
		}
	}
	return rows, inferredCounts
}

// ApplyPresentation returns details with the fields filtering and sorting never
// read: project and service icons, custom URLs, and the env access probe.
// records, details and metas must align by index.
func ApplyPresentation(ctx context.Context, projectsDir, catalog string, records []types.ProjectRecord, details []types.Project, metas []projects.ArcaneComposeMetadata) []types.Project {
	out := make([]types.Project, 0, len(details))
	for i, d := range details {
		icon := iconcatalog.Resolve(catalog, metas[i].ProjectIcon)
		services := make([]types.ProjectRuntimeService, 0, len(d.RuntimeServices))
		for _, service := range d.RuntimeServices {
			localIcon := iconcatalog.Resolve(catalog, cmp.Or(projects.FindArcaneIconSet(service.ContainerLabels), metas[i].ServiceIconSets[service.Name], metas[i].ProjectIcon))
			services = append(services, types.ProjectRuntimeService{ //exhaustruct:enforce
				ContainerLabels:  service.ContainerLabels,
				ImageID:          service.ImageID,
				Name:             service.Name,
				Image:            service.Image,
				Status:           service.Status,
				ContainerID:      service.ContainerID,
				ContainerName:    service.ContainerName,
				Ports:            service.Ports,
				Health:           service.Health,
				IconLightURL:     localIcon.IconLightURL,
				IconDarkURL:      localIcon.IconDarkURL,
				ServiceConfig:    service.ServiceConfig,
				RedeployDisabled: service.RedeployDisabled,
			})
		}
		out = append(out, types.Project{ //exhaustruct:enforce
			EnvironmentTag:     d.EnvironmentTag,
			Tags:               d.Tags,
			StatusReason:       d.StatusReason,
			ActivityID:         d.ActivityID,
			LastSyncCommit:     d.LastSyncCommit,
			GitOpsManagedBy:    d.GitOpsManagedBy,
			UpdateInfo:         d.UpdateInfo,
			ArchivedAt:         d.ArchivedAt,
			Status:             d.Status,
			DirName:            d.DirName,
			ComposeContent:     d.ComposeContent,
			ComposeFileName:    d.ComposeFileName,
			ComposeFiles:       d.ComposeFiles,
			EnvContent:         d.EnvContent,
			OverrideContent:    d.OverrideContent,
			OverrideFileName:   d.OverrideFileName,
			Name:               d.Name,
			GitRepositoryURL:   d.GitRepositoryURL,
			IconLightURL:       icon.IconLightURL,
			RelativePath:       d.RelativePath,
			ID:                 d.ID,
			IconDarkURL:        icon.IconDarkURL,
			Path:               d.Path,
			UpdatedAt:          d.UpdatedAt,
			CreatedAt:          d.CreatedAt,
			RuntimeServices:    services,
			Services:           d.Services,
			URLs:               metas[i].ProjectURLS,
			IncludeFiles:       d.IncludeFiles,
			RunningCount:       d.RunningCount,
			ServiceCount:       d.ServiceCount,
			IsDiscovered:       d.IsDiscovered,
			IsArchived:         d.IsArchived,
			HasBuildDirective:  d.HasBuildDirective,
			RedeployDisabled:   d.RedeployDisabled,
			ConfigurationError: projects.CheckProjectEnvAccess(ctx, projectsDir, records[i].Path),
		})
	}
	return out
}

// KnownComposeProjectNames collects every name a tracked project uses.
func KnownComposeProjectNames(records []types.ProjectRecord) map[string]struct{} {
	known := make(map[string]struct{}, len(records)*2)
	for _, p := range records {
		for _, name := range kit.TrimNonEmpty([]string{p.Name, mo.PointerToOption(p.ComposeProjectName).OrEmpty()}) {
			known[name] = struct{}{}
			if normalized := projects.NormalizeProjectName(name); normalized != "" {
				known[normalized] = struct{}{}
			}
		}
	}
	return known
}

// CountDiscoveredUpdates counts untracked compose projects with a pending update.
func (s *Service) CountDiscoveredUpdates(ctx context.Context, allContainers []container.Summary, known map[string]struct{}, catalog string) int {
	composeContainers := make([]container.Summary, 0, len(allContainers))
	for _, c := range allContainers {
		if docker.ComposeProjectLabel(c.Labels) != "" {
			composeContainers = append(composeContainers, c)
		}
	}
	if len(composeContainers) == 0 {
		return 0
	}
	// Only rows with a pending update are returned, so the length is the count.
	return len(s.DiscoveredUpdateRows(ctx, composeContainers, known, catalog))
}

// DiscoveredUpdateRows builds rows for untracked running compose projects that have a pending image update.
func (s *Service) DiscoveredUpdateRows(
	ctx context.Context,
	composeContainers []container.Summary,
	knownProjectNames map[string]struct{},
	iconCatalog string,
) []types.Project {
	containersByProject := make(map[string][]container.Summary)
	for _, c := range composeContainers {
		if docker.ComposeServiceLabel(c.Labels) == "" {
			continue
		}
		projectName := docker.ComposeProjectLabel(c.Labels)
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

	scoped := func() map[string]*types.ImageUpdateInfo {
		info, err := s.imageService.GetUpdateInfoByContainers(ctx, composeContainers)
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch discovered project tag updates", "error", err)
			return nil
		}
		return info
	}()
	rows := make([]types.Project, 0, len(containersByProject))
	for projectName, projectContainers := range containersByProject {
		// One runtime service per distinct service name and image.
		runtimeServices := make([]types.ProjectRuntimeService, 0, len(projectContainers))
		seenServices := make(map[string]struct{}, len(projectContainers))
		for _, c := range projectContainers {
			imageRef := strings.TrimSpace(c.Image)
			serviceName := cmp.Or(docker.ComposeServiceLabel(c.Labels), c.ID)
			key := serviceName + "\x00" + imageRef
			if _, exists := seenServices[key]; exists || imageRef == "" {
				continue
			}
			seenServices[key] = struct{}{}
			resolvedIcon := iconcatalog.Resolve(iconCatalog, projects.FindArcaneIconSet(c.Labels))
			runtimeServices = append(runtimeServices, types.ProjectRuntimeService{
				Name:            serviceName,
				Image:           imageRef,
				Status:          string(c.State),
				ContainerID:     c.ID,
				ImageID:         c.ImageID,
				ContainerLabels: c.Labels,
				ContainerName:   docker.ContainerNameFromNames(c.Names),
				Ports:           projects.FormatDockerPorts(c.Ports),
				IconLightURL:    resolvedIcon.IconLightURL,
				IconDarkURL:     resolvedIcon.IconDarkURL,
			})
		}
		checkServices := make([]types.ProjectRuntimeService, 0, len(projectContainers))
		for _, c := range projectContainers {
			checkServices = append(checkServices, types.ProjectRuntimeService{
				ContainerLabels: c.Labels,
				ImageID:         c.ImageID,
				Name:            docker.ComposeServiceLabel(c.Labels),
				Image:           c.Image,
				ContainerID:     c.ID,
			})
		}
		updateInfo := s.summarizeRuns(projects.ImageRefsFromRuntimeServices(runtimeServices), checkServices, scoped)
		if !kit.FromPtr(updateInfo).HasUpdate {
			continue
		}

		serviceCount, runningCount := ServiceCounts(runtimeServices)
		rows = append(rows, types.Project{
			ID:   "compose:" + projectName,
			Name: projectName,
			// Unlike tracked rows, a discovered project without services is unknown.
			Status:          ProjectStatus(runtimeServices),
			ServiceCount:    serviceCount,
			RunningCount:    runningCount,
			IsDiscovered:    true,
			CreatedAt:       kit.Ternary(updateInfo.LastCheckedAt != nil, kit.FromPtr(updateInfo.LastCheckedAt).Format(time.RFC3339), ""),
			UpdatedAt:       kit.Ternary(updateInfo.LastCheckedAt != nil, kit.FromPtr(updateInfo.LastCheckedAt).Format(time.RFC3339), ""),
			RuntimeServices: runtimeServices,
			UpdateInfo:      updateInfo,
		})
	}

	return rows
}

func ApplyProjectTagsDBFilter(query *gorm.DB, filterValue string) *gorm.DB {
	var names []string
	for value := range strings.SplitSeq(filterValue, ",") {
		if normalized, err := projects.NormalizeProjectTag(value); err == nil {
			names = append(names, normalized)
		}
	}
	names = kit.Unique(names)
	if len(names) == 0 {
		return query
	}
	return query.Where("EXISTS (SELECT 1 FROM project_tags WHERE project_tags.project_id = projects.id AND project_tags.name IN ?)", names)
}

func GroupComposeContainersByProject(containers []container.Summary) map[string][]container.Summary {
	containersByProject := make(map[string][]container.Summary)
	for _, c := range containers {
		projectName := docker.ComposeProjectLabel(c.Labels)
		if projectName != "" {
			containersByProject[projectName] = append(containersByProject[projectName], c)
		}
	}
	return containersByProject
}

func PaginationConfig() pagination.Config[types.Project] {
	return pagination.Config[types.Project]{
		SearchAccessors: []pagination.SearchAccessor[types.Project]{
			func(p types.Project) (string, error) { return p.Name, nil },
			func(p types.Project) (string, error) { return p.Path, nil },
			func(p types.Project) (string, error) { return p.RelativePath, nil },
			func(p types.Project) (string, error) { return p.Status, nil },
			func(p types.Project) (string, error) { return p.DirName, nil },
			func(p types.Project) (string, error) {
				names := make([]string, 0, len(p.Tags))
				for _, tag := range p.Tags {
					names = append(names, tag.Name)
				}
				return strings.Join(names, " "), nil
			},
		},
		SortBindings: []pagination.SortBinding[types.Project]{
			{Key: "name", Fn: func(a, b types.Project) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "status", Fn: func(a, b types.Project) int { return strings.Compare(a.Status, b.Status) }},
			{Key: "serviceCount", Fn: func(a, b types.Project) int { return cmp.Compare(a.ServiceCount, b.ServiceCount) }},
			{Key: "runningCount", Fn: func(a, b types.Project) int { return cmp.Compare(a.RunningCount, b.RunningCount) }},
			{Key: "path", Fn: func(a, b types.Project) int { return strings.Compare(a.RelativePath, b.RelativePath) }},
			{
				Key: "createdAt",
				Fn: func(a, b types.Project) int {
					at, aerr := time.Parse(time.RFC3339, a.CreatedAt)
					bt, berr := time.Parse(time.RFC3339, b.CreatedAt)
					if aerr != nil || berr != nil {
						return strings.Compare(a.CreatedAt, b.CreatedAt)
					}
					return at.Compare(bt)
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[types.Project]{
			{
				Key: "status",
				Fn: func(p types.Project, filterValue string) bool {
					return strings.EqualFold(strings.TrimSpace(p.Status), strings.TrimSpace(filterValue))
				},
			},
			{
				Key: "updates",
				Fn: func(p types.Project, filterValue string) bool {
					return strings.EqualFold(cmp.Or(strings.TrimSpace(kit.FromPtr(p.UpdateInfo).Status), "unknown"), strings.TrimSpace(filterValue))
				},
			},
			{
				Key: "name",
				Fn: func(p types.Project, filterValue string) bool {
					return slices.Contains(kit.TrimNonEmpty(strings.Split(filterValue, ",")), p.Name)
				},
			},
			{
				Key: "archived",
				Fn: func(p types.Project, filterValue string) bool {
					if strings.EqualFold(strings.TrimSpace(filterValue), "all") {
						return true
					}
					archived, _ := kit.ParseBool(filterValue)
					return p.IsArchived == archived
				},
			},
			{
				Key:     "label",
				NoSplit: true,
				Fn: func(p types.Project, filterValue string) bool {
					key, value, hasValue := strings.Cut(filterValue, "=")
					return strings.TrimSpace(key) == "" || slices.ContainsFunc(p.RuntimeServices, func(service types.ProjectRuntimeService) bool {
						actual, ok := service.ContainerLabels[strings.TrimSpace(key)]
						return ok && (!hasValue || actual == value)
					})
				},
			},
		},
	}
}

// Page searches, orders and paginates rows by their derived fields and returns
// the page indexes of tracked rows still needing enrichment.
func Page(items []types.Project, params pagination.QueryParams, tracked func(id string) bool) (pagination.FilterResult[types.Project], []int) {
	result := PaginationConfig().SearchOrderAndPaginate(items, pagination.QueryParams{
		SearchQuery: params.SearchQuery,
		SortParams:  params.SortParams,
		Params:      params.Params,
		// Tags are filtered in the database query.
		Filters: func() map[string]string {
			if _, exists := params.Filters["tags"]; !exists {
				return params.Filters
			}
			filters := maps.Clone(params.Filters)
			delete(filters, "tags")
			return filters
		}(),
	})
	pageIndexes := make([]int, 0, len(result.Items))
	for i, item := range result.Items {
		if tracked(item.ID) {
			pageIndexes = append(pageIndexes, i)
		}
	}
	return result, pageIndexes
}
