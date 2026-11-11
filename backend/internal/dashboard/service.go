package dashboard

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"go.uber.org/fx"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	DashboardService struct {
		dockerService        *docker.DockerClientService
		containerService     *container.ContainerService
		projectService       *project.ProjectService
		vulnerabilityService *vulnerability.VulnerabilityService
		systemService        *system.SystemService
		versionService       *version.VersionService
		settingsService      *settings.SettingsService
	}

	DashboardServiceParams struct {
		fx.In

		DockerService        *docker.DockerClientService
		ContainerService     *container.ContainerService
		ProjectService       *project.ProjectService
		VulnerabilityService *vulnerability.VulnerabilityService
		SystemService        *system.SystemService
		VersionService       *version.VersionService
		SettingsService      *settings.SettingsService
	}

	SummaryRequest struct {
		Permissions   *authz.PermissionSet
		EnvironmentID string
		Sections      []string
	}

	sectionLoader struct {
		ctx      context.Context
		wg       *sync.WaitGroup
		sections []string
	}
)

const topMemoryLimit = 5

var (
	sectionPermissions = map[string]string{
		types.DashboardSectionContainers:      authz.PermContainersList,
		types.DashboardSectionProjects:        authz.PermProjectsList,
		types.DashboardSectionImages:          authz.PermImagesList,
		types.DashboardSectionVulnerabilities: authz.PermVulnsList,
		types.DashboardSectionRisk:            authz.PermVulnsList,
		types.DashboardSectionDiskUsage:       authz.PermSystemRead,
		types.DashboardSectionTopMemory:       authz.PermContainersRead,
		types.DashboardSectionVersion:         authz.PermEnvironmentsRead,
	}

	vulnerabilitySections = []string{
		types.DashboardSectionVulnerabilities,
		types.DashboardSectionRisk,
	}
)

func NewDashboardService(p DashboardServiceParams) *DashboardService {
	return &DashboardService{
		dockerService:        p.DockerService,
		containerService:     p.ContainerService,
		projectService:       p.ProjectService,
		vulnerabilityService: p.VulnerabilityService,
		systemService:        p.SystemService,
		versionService:       p.VersionService,
		settingsService:      p.SettingsService,
	}
}

func (s *DashboardService) GetSummary(ctx context.Context, req SummaryRequest) types.DashboardSummary {
	var wg sync.WaitGroup
	loader := sectionLoader{
		ctx:      ctx,
		wg:       &wg,
		sections: s.allowedSections(ctx, req),
	}

	containers := loadSection(loader, types.DashboardSectionContainers, func() (types.ContainerStatusCounts, error) {
		return s.containerService.GetContainerStatusCounts(ctx, container.StatusCountOptions{})
	})
	projects := loadSection(loader, types.DashboardSectionProjects, func() (types.ProjectStatusCounts, error) {
		return s.projectService.GetProjectStatusCounts(ctx)
	})
	images := loadSection(loader, types.DashboardSectionImages, func() (types.ImageUsageCounts, error) {
		_, counts, err := s.dockerService.GetAllImages(ctx)
		return counts, err
	})
	vulnerabilities := loadSection(loader, types.DashboardSectionVulnerabilities, func() (*types.EnvironmentVulnerabilitySummary, error) {
		return s.vulnerabilityService.GetEnvironmentSummary(ctx)
	})
	risk := loadSection(loader, types.DashboardSectionRisk, func() (*types.RiskOverview, error) {
		return s.vulnerabilityService.GetRiskOverview(ctx, req.EnvironmentID)
	})
	diskUsage := loadSection(loader, types.DashboardSectionDiskUsage, func() (types.SystemDiskUsage, error) {
		return s.systemService.GetDiskUsage(ctx)
	})
	topMemory := loadSection(loader, types.DashboardSectionTopMemory, func() ([]types.ContainerSummary, error) {
		result, err := s.containerService.ListContainersPaginated(ctx, pagination.QueryParams{
			Sort:    types.ContainerSortMemoryUsage,
			Order:   pagination.SortDesc,
			Limit:   topMemoryLimit,
			Filters: map[string]string{},
		}, true, false, false, "")
		return result.Items, err
	})
	versionInfo := loadSection(loader, types.DashboardSectionVersion, func() (*types.VersionInfo, error) {
		return s.versionService.GetAppVersionInfo(ctx), nil
	})
	wg.Wait()

	return types.DashboardSummary{
		Containers:      containers(),
		Projects:        projects(),
		Images:          images(),
		Vulnerabilities: vulnerabilities(),
		Risk:            risk(),
		DiskUsage:       diskUsage(),
		TopMemory:       topMemory(),
		Version:         versionInfo(),
	}
}

func (s *DashboardService) AggregateSummaries(ctx context.Context) (types.ApiResponse[types.DashboardSummary], error) {
	return httpx.CollectEnvironmentData(ctx, nil, mergeSummaries)
}

func mergeSummaries(environments []types.EnvironmentData[types.DashboardSummary]) types.DashboardSummary {
	risks := make([]types.EnvironmentData[types.RiskOverview], 0, len(environments))
	for _, environment := range environments {
		if risk := kit.FromPtr(environment.Data.Risk).Data; risk != nil {
			risks = append(risks, types.EnvironmentData[types.RiskOverview]{
				Environment: environment.Environment,
				Data:        *risk,
			})
		}
	}
	if len(risks) == 0 {
		return types.DashboardSummary{}
	}
	return types.DashboardSummary{
		Risk: &types.DashboardSection[*types.RiskOverview]{Data: new(vulnerability.MergeRiskOverviews(risks))},
	}
}

func (s *DashboardService) allowedSections(ctx context.Context, req SummaryRequest) []string {
	scansVulnerabilities := s.settingsService.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement)
	return slices.DeleteFunc(slices.Clone(req.Sections), func(section string) bool {
		perm, known := sectionPermissions[section]
		return !known ||
			!req.Permissions.Allows(perm, req.EnvironmentID) ||
			!scansVulnerabilities && slices.Contains(vulnerabilitySections, section)
	})
}

func loadSection[T any](loader sectionLoader, section string, load func() (T, error)) func() *types.DashboardSection[T] {
	var result *types.DashboardSection[T]
	if slices.Contains(loader.sections, section) {
		loader.wg.Go(func() {
			data, err := runSection(section, load)
			if err != nil {
				slog.WarnContext(loader.ctx, "Dashboard section failed", "section", section, "error", err)
				result = &types.DashboardSection[T]{
					Data:  data,
					Error: err.Error(),
				}
				return
			}
			result = &types.DashboardSection[T]{Data: data}
		})
	}
	return func() *types.DashboardSection[T] {
		return result
	}
}

func runSection[T any](section string, load func() (T, error)) (data T, err error) {
	defer concurrency.RecoverToError(&err, "dashboard section", "section", section)
	return load()
}
