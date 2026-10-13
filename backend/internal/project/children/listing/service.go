// Package listing projects stored projects and live Compose containers into
// paginated list rows, status counts and untracked-project update rows.
package listing

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/compose/v5/pkg/api"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/container"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
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
	summarizeRuns func(imageRefs []string, services []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) *project.UpdateInfo
}

func New(
	containers func(ctx context.Context) ([]container.Summary, error),
	imageService *image.ImageService,
	summarizeRuns func(imageRefs []string, services []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) *project.UpdateInfo,
) *Service {
	return &Service{containers: containers, imageService: imageService, summarizeRuns: summarizeRuns}
}

// Snapshot is the compose container listing shared by
// every row of one list request, grouped once by compose project name.
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

// Containers returns the snapshot's compose containers and listing error.

// ProjectContainers matches project names first, then the working
// directory if it identifies exactly one Compose project group.
func ProjectContainers(p project.Record, containersByProject map[string][]container.Summary) []container.Summary {
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
func RuntimeServiceFromContainer(catalog string, c container.Summary, meta projects.ArcaneComposeMetadata, currentContainerID string, currentContainerErr error) project.RuntimeService {
	svcName := docker.ComposeServiceLabel(c.Labels)

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

	resolvedIcon := iconcatalog.Resolve(catalog, cmp.Or(projects.FindArcaneIconSet(c.Labels), meta.ServiceIconSets[svcName], meta.ProjectIcon))
	return project.RuntimeService{
		Name:             svcName,
		Image:            c.Image,
		Status:           string(c.State),
		ContainerID:      c.ID,
		ContainerName:    docker.ContainerNameFromNames(c.Names),
		Ports:            projects.FormatDockerPorts(c.Ports),
		Health:           health,
		IconLightURL:     resolvedIcon.IconLightURL,
		IconDarkURL:      resolvedIcon.IconDarkURL,
		ContainerLabels:  c.Labels,
		RedeployDisabled: labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr),
		ImageID:          c.ImageID,
	}
}

// ServiceCounts returns the total and running service counts.
func ServiceCounts(services []project.RuntimeService) (total, running int) {
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
func ProjectStatus(services []project.RuntimeService) string {
	if len(services) == 0 {
		return project.StatusUnknown
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
		return project.StatusRunning
	}
	if runningCount > 0 {
		return project.StatusPartiallyRunning
	}
	return kit.Ternary(stoppedCount > 0, project.StatusStopped, project.StatusUnknown)
}

// StatusCounts totals archived, running and stopped projects from live
// containers, falling back to stored status when Docker is unavailable.
func (s *Service) StatusCounts(ctx context.Context, records []project.Record) project.StatusCounts {
	counts := project.StatusCounts{TotalProjects: len(records)}
	active := make([]project.Record, 0, len(records))
	for _, p := range records {
		if p.IsArchived {
			counts.ArchivedProjects++
			continue
		}
		active = append(active, p)
	}

	containers, err := s.containers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers for counts", "error", err)
	}
	containersByProject := GroupComposeContainersByProject(containers)
	for _, p := range active {
		status := p.Status
		if err == nil {
			projectContainers := ProjectContainers(p, containersByProject)
			services := make([]project.RuntimeService, 0, len(projectContainers))
			for _, c := range projectContainers {
				services = append(services, project.RuntimeService{Status: string(c.State)})
			}
			status = kit.Ternary(len(services) > 0, ProjectStatus(services), project.StatusStopped)
		}
		switch status {
		case project.StatusRunning, project.StatusPartiallyRunning, project.StatusDeploying, project.StatusRestarting:
			counts.RunningProjects++
		case project.StatusStopped, project.StatusStopping:
			counts.StoppedProjects++
		}
	}
	return counts
}

// RelativePath returns projectPath relative to projectsDir, or "" outside it.
func RelativePath(projectsDir, projectPath string) string {
	if strings.TrimSpace(projectsDir) == "" {
		return ""
	}

	relativePath, err := filepath.Rel(projectsDir, filepath.Clean(projectPath))
	if err != nil {
		return ""
	}
	if relativePath == "." {
		return ""
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
		return ""
	}

	return filepath.ToSlash(relativePath)
}

// row builds the fields list filters, search and sort read from database
// columns and the container snapshot; compose metadata is left for ApplyPresentation.
func row(projectsDir, catalog string, p project.Record, snapshot Snapshot) project.Details {
	var resp project.Details
	_ = mapping.MapStruct(p, &resp)

	resp.CreatedAt = p.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = p.UpdatedAt.Format(time.RFC3339)
	resp.IsArchived = p.IsArchived
	resp.ArchivedAt = p.ArchivedAt
	resp.DirName = mo.PointerToOption(p.DirName).OrEmpty()
	resp.RelativePath = RelativePath(projectsDir, p.Path)
	resp.GitOpsManagedBy = p.GitOpsManagedBy
	resp.HasBuildDirective = p.BuildImageRefsJSON != nil && len(projects.ParseImageRefsJSON(*p.BuildImageRefsJSON)) > 0
	// Use DB service count as the source of truth for "Total Services"
	// since we are not parsing the YAML here.
	resp.ServiceCount = p.ServiceCount
	if snapshot.Err != nil {
		resp.Status = project.StatusUnknown
		return resp
	}

	projectContainers := ProjectContainers(p, snapshot.byProject)
	services := make([]project.RuntimeService, 0, len(projectContainers))
	for _, c := range projectContainers {
		service := RuntimeServiceFromContainer(catalog, c, projects.ArcaneComposeMetadata{}, snapshot.currentContainerID, snapshot.currentContainerErr)
		if service.RedeployDisabled {
			resp.RedeployDisabled = true
		}
		services = append(services, service)
	}
	_, runningCount := ServiceCounts(services)
	resp.RuntimeServices = services
	resp.RunningCount = runningCount
	// Newly discovered projects have no persisted count yet. Infer it from the
	// live containers; the parent persists the inferred value so
	// SQL sorting and pagination on service_count see it on later requests.
	if resp.ServiceCount == 0 && len(services) > 0 {
		resp.ServiceCount = len(services)
	}

	// Status uses the live services, not the possibly stale stored ServiceCount.
	switch {
	case runningCount > 0 && runningCount >= len(services):
		resp.Status = project.StatusRunning
	case runningCount > 0:
		resp.Status = project.StatusPartiallyRunning
	default:
		resp.Status = project.StatusStopped
	}

	return resp
}

// Rows builds list rows and reports service counts inferred from live
// containers for projects whose stored count is still zero.
func Rows(projectsDir, catalog string, records []project.Record, snapshot Snapshot) ([]project.Details, map[string]int) {
	rows := make([]project.Details, len(records))
	inferredCounts := make(map[string]int)
	for i, p := range records {
		rows[i] = row(projectsDir, catalog, p, snapshot)
		if p.ServiceCount == 0 && rows[i].ServiceCount > 0 {
			inferredCounts[p.ID] = rows[i].ServiceCount
		}
	}
	return rows, inferredCounts
}

// ApplyPresentation fills the fields filtering and sorting never read:
// project and service icons, custom URLs, and the env access probe. records,
// details and metas must align by index.
func ApplyPresentation(ctx context.Context, projectsDir, catalog string, records []project.Record, details []project.Details, metas []projects.ArcaneComposeMetadata) {
	for i := range records {
		resp := &details[i]
		icon := iconcatalog.Resolve(catalog, metas[i].ProjectIcon)
		resp.IconLightURL, resp.IconDarkURL = icon.IconLightURL, icon.IconDarkURL
		resp.Links = metas[i].ProjectLinks
		resp.URLs = nil //nolint:staticcheck // Preserve the deprecated URLs field for v2 clients.
		for _, link := range resp.Links {
			resp.URLs = append(resp.URLs, link.URL) //nolint:staticcheck // Preserve the deprecated URLs field for v2 clients.
		}
		resp.ConfigurationError = projects.CheckProjectEnvAccess(ctx, projectsDir, records[i].Path)
		for k := range resp.RuntimeServices {
			service := &resp.RuntimeServices[k]
			localIcon := iconcatalog.Resolve(catalog, cmp.Or(projects.FindArcaneIconSet(service.ContainerLabels), metas[i].ServiceIconSets[service.Name], metas[i].ProjectIcon))
			service.IconLightURL, service.IconDarkURL = localIcon.IconLightURL, localIcon.IconDarkURL
		}
	}
}

// KnownComposeProjectNames collects every name a tracked project uses.
func KnownComposeProjectNames(records []project.Record) map[string]struct{} {
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

// DiscoveredUpdateRows builds rows for running compose projects Arcane does
// not track that have a pending image update.
func (s *Service) DiscoveredUpdateRows(
	ctx context.Context,
	composeContainers []container.Summary,
	knownProjectNames map[string]struct{},
	iconCatalog string,
) []project.Details {
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

	var scoped map[string]*imagetypes.UpdateInfo
	if s.imageService != nil {
		var err error
		if scoped, err = s.imageService.GetUpdateInfoByContainers(ctx, composeContainers); err != nil {
			slog.WarnContext(ctx, "failed to fetch discovered project tag updates", "error", err)
			scoped = nil
		}
	}
	rows := make([]project.Details, 0, len(containersByProject))
	for projectName, projectContainers := range containersByProject {
		// One runtime service per distinct service name and image.
		runtimeServices := make([]project.RuntimeService, 0, len(projectContainers))
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
			runtimeServices = append(runtimeServices, project.RuntimeService{
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
		imageRefs := projects.ImageRefsFromRuntimeServices(runtimeServices)
		checkServices := make([]project.RuntimeService, 0, len(projectContainers))
		for _, c := range projectContainers {
			checkServices = append(checkServices, project.RuntimeService{Name: docker.ComposeServiceLabel(c.Labels), ContainerID: c.ID, Image: c.Image, ImageID: c.ImageID, ContainerLabels: c.Labels})
		}
		updateInfo := s.summarizeRuns(imageRefs, checkServices, scoped)
		if updateInfo == nil || !updateInfo.HasUpdate {
			continue
		}

		serviceCount, runningCount := ServiceCounts(runtimeServices)
		// Unlike tracked rows, a discovered project without services is unknown.
		status := project.StatusStopped
		switch {
		case serviceCount == 0:
			status = project.StatusUnknown
		case runningCount >= serviceCount:
			status = project.StatusRunning
		case runningCount > 0:
			status = project.StatusPartiallyRunning
		}

		lastCheckedAt := ""
		if updateInfo.LastCheckedAt != nil {
			lastCheckedAt = updateInfo.LastCheckedAt.Format(time.RFC3339)
		}

		rows = append(rows, project.Details{
			ID:              "compose:" + projectName,
			Name:            projectName,
			Path:            "",
			Status:          status,
			ServiceCount:    serviceCount,
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

// Page searches, orders and paginates rows by their derived fields and returns
// the page indexes of tracked rows still needing enrichment.
func Page(items []project.Details, params pagination.QueryParams, tracked func(id string) bool) (pagination.FilterResult[project.Details], []int) {
	// Tags are filtered in the database query.
	if _, exists := params.Filters["tags"]; exists {
		params.Filters = maps.Clone(params.Filters)
		delete(params.Filters, "tags")
	}
	config := pagination.Config[project.Details]{
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
			{Key: "name", Fn: func(a, b project.Details) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "status", Fn: func(a, b project.Details) int { return strings.Compare(a.Status, b.Status) }},
			{Key: "serviceCount", Fn: func(a, b project.Details) int { return cmp.Compare(a.ServiceCount, b.ServiceCount) }},
			{Key: "path", Fn: func(a, b project.Details) int { return strings.Compare(a.RelativePath, b.RelativePath) }},
			{
				Key: "createdAt",
				Fn: func(a, b project.Details) int {
					at, aerr := time.Parse(time.RFC3339, a.CreatedAt)
					bt, berr := time.Parse(time.RFC3339, b.CreatedAt)
					if aerr != nil || berr != nil {
						return strings.Compare(a.CreatedAt, b.CreatedAt)
					}
					return at.Compare(bt)
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[project.Details]{
			{
				Key: "status",
				Fn: func(p project.Details, filterValue string) bool {
					return strings.EqualFold(strings.TrimSpace(p.Status), strings.TrimSpace(filterValue))
				},
			},
			{
				Key: "updates",
				Fn: func(p project.Details, filterValue string) bool {
					status := "unknown"
					if p.UpdateInfo != nil && strings.TrimSpace(p.UpdateInfo.Status) != "" {
						status = p.UpdateInfo.Status
					}
					return strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(filterValue))
				},
			},
			{
				Key: "archived",
				Fn: func(p project.Details, filterValue string) bool {
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
			},
		},
	}
	result := config.SearchOrderAndPaginate(items, params)
	pageIndexes := make([]int, 0, len(result.Items))
	for i, item := range result.Items {
		if tracked(item.ID) {
			pageIndexes = append(pageIndexes, i)
		}
	}
	return result, pageIndexes
}
