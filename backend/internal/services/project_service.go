package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	libbuild "github.com/getarcaneapp/arcane/backend/pkg/libarcane/libbuild"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/getarcaneapp/arcane/types/project"
	"github.com/moby/moby/api/types/container"
	"gorm.io/gorm"
)

type ProjectService struct {
	db              *database.DB
	settingsService *SettingsService
	eventService    *EventService
	imageService    *ImageService
	dockerService   *DockerClientService
	buildService    *BuildService
	config          *config.Config

	composeNameCacheMu  sync.RWMutex
	composeNameToProjID map[string]string
}

func NewProjectService(db *database.DB, settingsService *SettingsService, eventService *EventService, imageService *ImageService, dockerService *DockerClientService, buildService *BuildService, cfg *config.Config) *ProjectService {
	return &ProjectService{
		db:              db,
		settingsService: settingsService,
		eventService:    eventService,
		imageService:    imageService,
		dockerService:   dockerService,
		buildService:    buildService,
		config:          cfg,
	}
}

func (s *ProjectService) getPathMapper(ctx context.Context) (*projects.PathMapper, error) {
	configuredPath := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")

	var containerDir, hostDir string

	// Handle mapping format: "container_path:host_path"
	if parts := strings.SplitN(configuredPath, ":", 2); len(parts) == 2 {
		// Only treat as mapping if first part is absolute Linux path (not Windows drive)
		if !projects.IsWindowsDrivePath(configuredPath) && strings.HasPrefix(parts[0], "/") {
			containerDir = parts[0]
			hostDir = parts[1]
		}
	}

	if containerDir == "" {
		containerDir = configuredPath
	}

	// Resolve container directory to absolute path
	containerDirResolved, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(containerDir))
	if err != nil {
		slog.WarnContext(ctx, "unable to resolve container projects directory, using default", "error", err)
		containerDirResolved = "/app/data/projects"
	}

	// If hostDir not obtained from mapping, attempt auto-discovery from Docker mounts
	if hostDir == "" && s.dockerService != nil {
		if dockerCli, derr := s.dockerService.GetClient(ctx); derr == nil {
			absContainerDir, _ := filepath.Abs(containerDirResolved)
			if discovery, aerr := dockerutil.GetHostPathForContainerPath(ctx, dockerCli, absContainerDir); aerr == nil && discovery != "" {
				hostDir = discovery
				slog.DebugContext(ctx, "Auto-discovered host path for projects", "container", absContainerDir, "host", hostDir)
			}
		}
	}

	// Clean host directory
	hostDirResolved := strings.TrimSpace(hostDir)
	if hostDirResolved != "" {
		hostDirResolved = filepath.Clean(hostDirResolved)
	}

	pm := projects.NewPathMapper(containerDirResolved, hostDirResolved)
	if !pm.IsNonMatchingMount() {
		return nil, nil
	}

	return pm, nil
}

func (s *ProjectService) getProjectsDirectoryInternal(ctx context.Context) (string, error) {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDir, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		return "", err
	}

	return filepath.Clean(projectsDir), nil
}

func (s *ProjectService) GetProjectRelativePath(ctx context.Context, projectPath string) string {
	projectsDir, err := s.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return ""
	}

	return s.getProjectRelativePathInternal(projectsDir, projectPath)
}

func (s *ProjectService) getProjectRelativePathInternal(projectsDir, projectPath string) string {
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

// Helpers

type ProjectServiceInfo struct {
	Name          string                      `json:"name"`
	Image         string                      `json:"image"`
	Status        string                      `json:"status"`
	ContainerID   string                      `json:"container_id"`
	ContainerName string                      `json:"container_name"`
	Ports         []string                    `json:"ports"`
	Health        *string                     `json:"health,omitempty"`
	IconURL       string                      `json:"icon_url,omitempty"`
	ServiceConfig *composetypes.ServiceConfig `json:"service_config,omitempty"`
}

type ProjectBuildOptions struct {
	Services []string
	Provider string
	Push     *bool
	Load     *bool
}

type deployImageDecision struct {
	Build                   bool
	PullAlways              bool
	PullIfMissing           bool
	FallbackBuildOnPullFail bool
	RequireLocalOnly        bool
}

type imagePullMode int

const (
	imagePullModeNever imagePullMode = iota
	imagePullModeIfMissing
	imagePullModeAlways
)

func resolveServiceImagePullMode(svc composetypes.ServiceConfig) imagePullMode {
	rawPolicy := strings.ToLower(strings.TrimSpace(svc.PullPolicy))
	switch {
	case rawPolicy == composetypes.PullPolicyNever:
		return imagePullModeNever
	case rawPolicy == composetypes.PullPolicyAlways:
		return imagePullModeAlways
	case rawPolicy == composetypes.PullPolicyRefresh,
		rawPolicy == "daily",
		rawPolicy == "weekly",
		strings.HasPrefix(rawPolicy, "every_"):
		return imagePullModeAlways
	case rawPolicy == composetypes.PullPolicyMissing,
		rawPolicy == composetypes.PullPolicyIfNotPresent,
		rawPolicy == composetypes.PullPolicyBuild,
		rawPolicy == "":
		return imagePullModeIfMissing
	}

	policy, _, err := svc.GetPullPolicy()
	if err != nil {
		slog.Warn("failed to parse service pull_policy, defaulting to missing", "service", svc.Name, "pull_policy", svc.PullPolicy, "error", err)
		return imagePullModeIfMissing
	}

	switch policy {
	case composetypes.PullPolicyNever:
		return imagePullModeNever
	case composetypes.PullPolicyAlways, composetypes.PullPolicyRefresh:
		return imagePullModeAlways
	case composetypes.PullPolicyMissing, composetypes.PullPolicyIfNotPresent, composetypes.PullPolicyBuild:
		return imagePullModeIfMissing
	default:
		return imagePullModeIfMissing
	}
}

func buildProjectImagePullPlan(services composetypes.Services) map[string]imagePullMode {
	plan := map[string]imagePullMode{}
	for _, svc := range services {
		img := strings.TrimSpace(svc.Image)
		if img == "" {
			continue
		}
		mode := resolveServiceImagePullMode(svc)
		if existing, exists := plan[img]; !exists || mode > existing {
			plan[img] = mode
		}
	}
	return plan
}

func normalizeComposeProjectName(name string) string {
	if name == "" {
		return ""
	}
	normalized := loader.NormalizeProjectName(name)
	if normalized == "" {
		return name
	}
	return normalized
}

func (s *ProjectService) getCachedComposeProjectIDInternal(normalizedName string) (string, bool) {
	if normalizedName == "" {
		return "", false
	}

	s.composeNameCacheMu.RLock()
	defer s.composeNameCacheMu.RUnlock()

	if s.composeNameToProjID == nil {
		return "", false
	}

	projectID, ok := s.composeNameToProjID[normalizedName]
	return projectID, ok
}

func (s *ProjectService) cacheComposeProjectIDInternal(normalizedName, projectID string) {
	if normalizedName == "" || projectID == "" {
		return
	}

	s.composeNameCacheMu.Lock()
	defer s.composeNameCacheMu.Unlock()

	if s.composeNameToProjID == nil {
		s.composeNameToProjID = make(map[string]string)
	}
	s.composeNameToProjID[normalizedName] = projectID
}

func (s *ProjectService) invalidateCachedComposeProjectIDInternal(normalizedName string) {
	if normalizedName == "" {
		return
	}

	s.composeNameCacheMu.Lock()
	defer s.composeNameCacheMu.Unlock()

	delete(s.composeNameToProjID, normalizedName)
}

func (s *ProjectService) lookupProjectByCachedComposeNameInternal(ctx context.Context, normalizedName string) (*models.Project, bool, error) {
	projectID, ok := s.getCachedComposeProjectIDInternal(normalizedName)
	if !ok {
		return nil, false, nil
	}

	var project models.Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.invalidateCachedComposeProjectIDInternal(normalizedName)
			return nil, false, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, fmt.Errorf("request canceled or timed out: %w", err)
		}
		return nil, false, fmt.Errorf("failed to get project by cached compose name: %w", err)
	}
	if normalizeComposeProjectName(project.Name) != normalizedName {
		s.invalidateCachedComposeProjectIDInternal(normalizedName)
		return nil, false, nil
	}

	return &project, true, nil
}

func (s *ProjectService) rebuildComposeNameCacheInternal(ctx context.Context) error {
	var projects []models.Project
	if err := s.db.WithContext(ctx).Select("id", "name").Find(&projects).Error; err != nil {
		return err
	}

	cache := make(map[string]string, len(projects))
	for i := range projects {
		normalizedName := normalizeComposeProjectName(projects[i].Name)
		if normalizedName == "" {
			continue
		}
		if _, exists := cache[normalizedName]; !exists {
			cache[normalizedName] = projects[i].ID
		}
	}

	s.composeNameCacheMu.Lock()
	s.composeNameToProjID = cache
	s.composeNameCacheMu.Unlock()

	return nil
}

func (s *ProjectService) GetProjectFromDatabaseByID(ctx context.Context, id string) (*models.Project, error) {
	var project models.Project
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&project).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("request canceled or timed out")
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("project not found")
		}
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	return &project, nil
}

func (s *ProjectService) GetProjectByComposeName(ctx context.Context, name string) (*models.Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project name is empty")
	}
	normalized := normalizeComposeProjectName(name)

	var proj models.Project
	err := s.db.WithContext(ctx).Where("name = ? OR name = ?", name, normalized).First(&proj).Error
	if err == nil {
		s.cacheComposeProjectIDInternal(normalized, proj.ID)
		return &proj, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to get project by name: %w", err)
	}

	if cachedProject, found, cacheErr := s.lookupProjectByCachedComposeNameInternal(ctx, normalized); cacheErr != nil {
		return nil, cacheErr
	} else if found {
		return cachedProject, nil
	}

	if err := s.rebuildComposeNameCacheInternal(ctx); err != nil {
		return nil, fmt.Errorf("failed to list projects by compose name: %w", err)
	}

	if cachedProject, found, cacheErr := s.lookupProjectByCachedComposeNameInternal(ctx, normalized); cacheErr != nil {
		return nil, cacheErr
	} else if found {
		return cachedProject, nil
	}

	return nil, fmt.Errorf("project not found: %s", name)
}

func (s *ProjectService) UpdateProjectServices(ctx context.Context, projectID string, servicesToUpdate []string, user models.User) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// 1. Load project
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, _ := s.getPathMapper(ctx)
	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, err := projects.LoadComposeProjectFromDir(ctx, projectFromDb.Path, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if err != nil {
		return fmt.Errorf("failed to load compose project: %w", err)
	}

	// 2. Set status to deploying/restarting
	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusDeploying); err != nil {
		return err
	}

	// 3. Pull images for specific services
	if err := projects.ComposePull(ctx, compProj, servicesToUpdate); err != nil {
		slog.WarnContext(ctx, "compose pull failed, continuing", "error", err)
	}

	// 4. Stop specific services
	if err := projects.ComposeStop(ctx, compProj, servicesToUpdate); err != nil {
		slog.WarnContext(ctx, "compose stop failed, continuing", "error", err)
	}

	// 5. Up specific services
	if err := projects.ComposeUp(ctx, compProj, servicesToUpdate, false, false); err != nil {
		if statusErr := s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusStopped); statusErr != nil {
			slog.ErrorContext(ctx, "UpdateProjectServices: failed to set stopped status after compose up failure", "projectID", projectID, "error", statusErr)
		}
		return fmt.Errorf("failed to up services: %w", err)
	}

	// 6. Finalize status
	if err := s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusRunning); err != nil {
		return err
	}

	metadata := models.JSON{
		"action":      "update_services",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
		"services":    append([]string(nil), servicesToUpdate...),
	}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectUpdate, projectID, projectFromDb.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project service update action", "error", logErr)
	}

	return nil
}

func (s *ProjectService) getServiceCounts(services []ProjectServiceInfo) (total int, running int) {
	total = len(services)
	for _, service := range services {
		st := strings.ToLower(strings.TrimSpace(service.Status))
		if st == "running" || st == "up" {
			running++
		}
	}
	return total, running
}

func (s *ProjectService) updateProjectStatusandCountsInternal(ctx context.Context, projectID string, status models.ProjectStatus) error {
	services, err := s.GetProjectServices(ctx, projectID)
	if err != nil {
		slog.Error("GetProjectServices failed during status update", "projectID", projectID, "error", err)
		return s.updateProjectStatusInternal(ctx, projectID, status)
	}

	serviceCount, runningCount := s.getServiceCounts(services)

	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"status":        status,
		"service_count": serviceCount,
		"running_count": runningCount,
		"updated_at":    time.Now(),
	}).Error; err != nil {
		return fmt.Errorf("failed to update project status and counts: %w", err)
	}

	return nil
}

func (s *ProjectService) updateProjectStatusInternal(ctx context.Context, id string, status models.ProjectStatus) error {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", id).Updates(map[string]any{
		"status":     status,
		"updated_at": now,
	})

	if res.Error != nil {
		return fmt.Errorf("failed to update project status: %w", res.Error)
	}

	return nil
}

func (s *ProjectService) GetProjectServices(ctx context.Context, projectID string) ([]ProjectServiceInfo, error) {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	composeFileFullPath, derr := projects.DetectComposeFile(projectFromDb.Path)
	if derr != nil {
		return []ProjectServiceInfo{}, fmt.Errorf("no compose file found in project directory: %s", projectFromDb.Path)
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if loadErr != nil {
		return []ProjectServiceInfo{}, fmt.Errorf("failed to load compose project from %s: %w", projectFromDb.Path, loadErr)
	}

	meta, metaErr := projects.ParseArcaneComposeMetadata(ctx, composeFileFullPath)
	if metaErr != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFileFullPath, "error", metaErr)
	}

	containers, err := projects.ComposePs(ctx, project, nil, true)
	if err != nil {
		slog.Error("compose ps error", "projectName", project.Name, "error", err)
		return nil, fmt.Errorf("failed to get compose services status: %w", err)
	}

	have := map[string]bool{}
	var services []ProjectServiceInfo

	// Create a map for quick lookup of service config
	serviceConfigs := make(map[string]composetypes.ServiceConfig)
	for _, svc := range project.Services {
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

		services = append(services, ProjectServiceInfo{
			Name:          c.Service,
			Image:         c.Image,
			Status:        string(c.State),
			ContainerID:   c.ID,
			ContainerName: c.Name,
			Ports:         formatPorts(c.Publishers),
			Health:        health,
			IconURL:       meta.ServiceIcons[c.Service],
			ServiceConfig: svcConfig,
		})
		have[c.Service] = true
	}

	for _, svc := range project.Services {
		if !have[svc.Name] {
			services = append(services, ProjectServiceInfo{
				Name:          svc.Name,
				Image:         svc.Image,
				Status:        "stopped",
				Ports:         []string{},
				IconURL:       meta.ServiceIcons[svc.Name],
				ServiceConfig: new(svc),
			})
		}
	}

	return services, nil
}

func (s *ProjectService) GetProjectContent(ctx context.Context, projectID string) (composeContent, envContent string, err error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	return projects.ReadProjectFiles(proj.Path)
}

func (s *ProjectService) GetProjectDetails(ctx context.Context, projectID string) (project.Details, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return project.Details{}, err
	}
	projectsDir, _ := s.getProjectsDirectoryInternal(ctx)

	composeContent, _, _ := s.GetProjectContent(ctx, projectID)
	envState, err := projects.ReadProjectEnvState(proj.Path)
	if err != nil {
		return project.Details{}, fmt.Errorf("failed to read project env state: %w", err)
	}

	var resp project.Details
	if err := mapper.MapStruct(proj, &resp); err != nil {
		return project.Details{}, fmt.Errorf("failed to map project: %w", err)
	}

	effectiveEnvContent, err := s.resolveStoredEffectiveEnvContentInternal(envState)
	if err != nil {
		return project.Details{}, err
	}

	resp.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	resp.ComposeContent = composeContent
	resp.EnvContent = effectiveEnvContent
	resp.HasBuildDirective = false
	resp.DirName = utils.DerefString(proj.DirName)
	resp.RelativePath = s.getProjectRelativePathInternal(projectsDir, proj.Path)
	resp.GitOpsManagedBy = proj.GitOpsManagedBy
	meta := s.getProjectMetadataFromPath(ctx, proj.Path)
	resp.IconURL = meta.ProjectIconURL
	resp.URLs = meta.ProjectURLS

	// Default counts/status from DB (will be overridden if runtime check succeeds)
	resp.ServiceCount = proj.ServiceCount
	resp.RunningCount = proj.RunningCount
	resp.Status = string(proj.Status)

	// Enrich with details
	s.enrichWithIncludeFiles(ctx, proj.Path, &resp)
	s.enrichWithDirectoryFiles(ctx, proj.Path, &resp)
	s.enrichWithGitOpsInfo(ctx, proj, &resp)

	// Load compose project for service definitions
	composeFile, _ := projects.DetectComposeFile(proj.Path)
	if composeFile != "" {
		s.enrichWithComposeServiceConfigs(ctx, proj, composeFile, &resp)
	}

	// Get runtime services and update status/counts
	services, serr := s.GetProjectServices(ctx, projectID)
	if serr == nil && services != nil {
		resp.ServiceCount = len(services)
		_, runningCount := s.getServiceCounts(services)
		resp.RunningCount = runningCount
		resp.Status = string(s.calculateProjectStatus(services))

		runtimeServices := make([]project.RuntimeService, len(services))
		for i, svc := range services {
			runtimeServices[i] = project.RuntimeService{
				Name:          svc.Name,
				Image:         svc.Image,
				Status:        svc.Status,
				ContainerID:   svc.ContainerID,
				ContainerName: svc.ContainerName,
				Ports:         svc.Ports,
				Health:        svc.Health,
				IconURL:       svc.IconURL,
				ServiceConfig: svc.ServiceConfig,
			}
		}
		resp.RuntimeServices = runtimeServices
	}

	return resp, nil
}

func (s *ProjectService) enrichWithIncludeFiles(ctx context.Context, projectPath string, resp *project.Details) {
	composeFile, detectErr := projects.DetectComposeFile(projectPath)
	if detectErr == nil {
		// Load environment variables so that include paths with ${VAR} references are expanded
		projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
		projectsDirectory, _ := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
		autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
		envLoader := projects.NewEnvLoader(projectsDirectory, filepath.Dir(composeFile), autoInjectEnv)
		envMap, _, _ := envLoader.LoadEnvironment(ctx)

		includes, parseErr := projects.ParseIncludes(composeFile, envMap)
		if parseErr == nil {
			var includeFiles []project.IncludeFile
			for _, inc := range includes {
				includeFiles = append(includeFiles, project.IncludeFile{
					Path:         inc.Path,
					RelativePath: inc.RelativePath,
					Content:      inc.Content,
				})
			}
			resp.IncludeFiles = includeFiles
		} else {
			slog.WarnContext(ctx, "Failed to parse includes", "error", parseErr, "path", projectPath)
		}
	}
}

func (s *ProjectService) enrichWithDirectoryFiles(ctx context.Context, projectPath string, resp *project.Details) {
	if projectPath == "" {
		return
	}

	// Build set of already-shown files to skip
	shownFiles := map[string]bool{
		".env":                true,
		"compose.yaml":        true,
		"compose.yml":         true,
		"docker-compose.yaml": true,
		"docker-compose.yml":  true,
	}
	for _, inc := range resp.IncludeFiles {
		shownFiles[inc.RelativePath] = true
	}

	dirFiles, err := projects.ReadProjectDirectoryFiles(projectPath, shownFiles, s.config.ProjectScanMaxDepth, s.config.ProjectScanSkipDirs)
	if err != nil {
		slog.WarnContext(ctx, "Failed to scan project directory files", "error", err, "path", projectPath)
	}

	resp.DirectoryFiles = dirFiles
}

func (s *ProjectService) enrichWithGitOpsInfo(ctx context.Context, proj *models.Project, resp *project.Details) {
	if proj.GitOpsManagedBy != nil {
		var sync models.GitOpsSync
		if err := s.db.WithContext(ctx).Preload("Repository").Where("id = ?", *proj.GitOpsManagedBy).First(&sync).Error; err == nil {
			resp.LastSyncCommit = sync.LastSyncCommit
			if sync.Repository != nil {
				resp.GitRepositoryURL = sync.Repository.URL
			}
		}
	}
}

func (s *ProjectService) enrichWithComposeServiceConfigs(ctx context.Context, proj *models.Project, composeFile string, resp *project.Details) {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, _ := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	composeProj, loadErr := projects.LoadComposeProject(ctx, composeFile, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv, pathMapper)
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

func (s *ProjectService) SyncProjectsFromFileSystem(ctx context.Context) error {
	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.getProjectsDirectoryInternal(ctx)
	if err != nil {
		slog.WarnContext(ctx, "unable to prepare projects directory", "error", err)
		return nil
	}

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(projectsDir, followProjectSymlinks)
	if discoveryErr != nil {
		if os.IsNotExist(discoveryErr) {
			return nil
		}
		slog.WarnContext(ctx, "failed to discover projects directory contents", "dir", projectsDir, "error", discoveryErr)
		return nil
	}

	seen := map[string]struct{}{}
	for _, discoveredProject := range discoveredProjects {
		if uerr := s.upsertProjectForDir(ctx, discoveredProject.DirName, discoveredProject.Path); uerr != nil {
			slog.WarnContext(ctx, "failed to sync project from folder", "dir", discoveredProject.Path, "error", uerr)
			continue
		}
		seen[discoveredProject.Path] = struct{}{}
	}

	if cerr := s.cleanupDBProjects(ctx, seen, followProjectSymlinks); cerr != nil {
		slog.WarnContext(ctx, "error during DB cleanup of projects", "error", cerr)
	}

	return nil
}

func (s *ProjectService) upsertProjectForDir(ctx context.Context, dirName, dirPath string) error {
	var existing models.Project
	err := s.db.WithContext(ctx).
		Where("path = ?", dirPath).
		First(&existing).Error

	filesystemProject := models.Project{
		Name: dirName,
		Path: dirPath,
	}
	serviceCount, serviceCountErr := s.countServicesFromCompose(ctx, filesystemProject)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create a minimal project entry
		reason := "Project discovered from filesystem, status pending Docker service query"
		proj := &models.Project{
			Name:         dirName,
			DirName:      new(dirName),
			Path:         dirPath,
			Status:       models.ProjectStatusUnknown,
			StatusReason: new(reason),
			ServiceCount: serviceCount,
			RunningCount: 0,
		}
		slog.InfoContext(ctx, "Discovered new project with unknown status",
			"project", dirName,
			"path", dirPath,
			"reason", reason)
		if serviceCountErr != nil {
			slog.WarnContext(ctx, "failed to read compose service count during project discovery", "project", dirName, "path", dirPath, "error", serviceCountErr)
		}
		if cerr := s.db.WithContext(ctx).Create(proj).Error; cerr != nil {
			return fmt.Errorf("create project for %q failed: %w", dirPath, cerr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("query existing project for %q failed: %w", dirPath, err)
	}

	updates := map[string]any{}
	if existing.Path != dirPath {
		updates["path"] = dirPath
	}
	if existing.DirName == nil || *existing.DirName != dirName {
		updates["dir_name"] = dirName
	}
	if serviceCountErr == nil && existing.ServiceCount != serviceCount {
		updates["service_count"] = serviceCount
	} else if serviceCountErr != nil {
		slog.WarnContext(ctx, "failed to refresh compose service count during project sync", "projectID", existing.ID, "path", dirPath, "error", serviceCountErr)
	}
	if len(updates) == 0 {
		return nil
	}

	updates["updated_at"] = time.Now()
	if uerr := s.db.WithContext(ctx).
		Model(&models.Project{}).
		Where("id = ?", existing.ID).
		Updates(updates).Error; uerr != nil {
		return fmt.Errorf("update project %s failed: %w", existing.ID, uerr)
	}
	return nil
}

func (s *ProjectService) cleanupDBProjects(ctx context.Context, seen map[string]struct{}, followProjectSymlinks bool) error {
	var all []models.Project
	if err := s.db.WithContext(ctx).Find(&all).Error; err != nil {
		return fmt.Errorf("list projects for cleanup failed: %w", err)
	}

	for _, p := range all {
		// Skip paths seen in this pass
		if _, ok := seen[p.Path]; ok {
			continue
		}

		validDir, err := projects.IsProjectDirectoryPath(p.Path, followProjectSymlinks)
		if err != nil {
			if os.IsNotExist(err) {
				if derr := s.db.WithContext(ctx).Delete(&models.Project{}, "id = ?", p.ID).Error; derr != nil {
					slog.WarnContext(ctx, "failed to delete missing-path project", "projectID", p.ID, "error", derr)
				}
				continue
			}
			slog.WarnContext(ctx, "stat error during cleanup", "path", p.Path, "error", err)
			continue
		}
		if !validDir {
			if derr := s.db.WithContext(ctx).Delete(&models.Project{}, "id = ?", p.ID).Error; derr != nil {
				slog.WarnContext(ctx, "failed to delete non-project path", "projectID", p.ID, "path", p.Path, "error", derr)
			}
			continue
		}

		if _, err := projects.DetectComposeFile(p.Path); err != nil {
			if derr := s.db.WithContext(ctx).Delete(&models.Project{}, "id = ?", p.ID).Error; derr != nil {
				slog.WarnContext(ctx, "failed to delete project without compose", "projectID", p.ID, "error", derr)
			}
		}
	}
	return nil
}

func (s *ProjectService) ListAllProjects(ctx context.Context) ([]models.Project, error) {
	var items []models.Project
	if err := s.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return items, nil
}

func formatPorts(publishers []api.PortPublisher) []string {
	var ports []string
	for _, pub := range publishers {
		if pub.PublishedPort > 0 {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", pub.PublishedPort, pub.TargetPort, pub.Protocol))
		} else {
			ports = append(ports, fmt.Sprintf("%d/%s", pub.TargetPort, pub.Protocol))
		}
	}
	return ports
}

func formatDockerPorts(ports []container.PortSummary) []string {
	var res []string
	for _, p := range ports {
		if p.PublicPort == 0 {
			res = append(res, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		} else {
			res = append(res, fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, p.Type))
		}
	}
	return res
}

func (s *ProjectService) countProjectFolders(ctx context.Context) (int, error) {
	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return 0, fmt.Errorf("could not determine projects directory: %w", err)
	}

	info, statErr := os.Stat(projectsDir)
	if os.IsNotExist(statErr) {
		// Directory missing, treat as zero
		return 0, nil
	}
	if statErr != nil {
		return 0, fmt.Errorf("unable to access projects directory %s: %w", projectsDir, statErr)
	}
	if !info.IsDir() {
		return 0, nil
	}

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(projectsDir, followProjectSymlinks)
	if discoveryErr != nil {
		return 0, fmt.Errorf("failed to discover project directories in %s: %w", projectsDir, discoveryErr)
	}

	return len(discoveredProjects), nil
}

func (s *ProjectService) incrementStatusCounts(status models.ProjectStatus, running, stopped *int) {
	switch status {
	case models.ProjectStatusRunning, models.ProjectStatusPartiallyRunning, models.ProjectStatusDeploying, models.ProjectStatusRestarting:
		*running++
	case models.ProjectStatusStopped, models.ProjectStatusStopping:
		*stopped++
	case models.ProjectStatusUnknown:
		// Don't count unknown
	}
}

func (s *ProjectService) GetProjectStatusCounts(ctx context.Context) (folderCount, runningProjects, stoppedProjects, totalProjects int, err error) {
	folderCount, _ = s.countProjectFolders(ctx)

	var projectsList []models.Project
	if err := s.db.WithContext(ctx).Find(&projectsList).Error; err != nil {
		return folderCount, 0, 0, 0, fmt.Errorf("failed to list projects: %w", err)
	}

	totalProjects = len(projectsList)
	runningProjects = 0
	stoppedProjects = 0

	// 1. Fetch all compose containers
	containers, err := projects.ListGlobalComposeContainers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers for counts", "error", err)
		// Fallback to DB status
		for _, p := range projectsList {
			s.incrementStatusCounts(p.Status, &runningProjects, &stoppedProjects)
		}
		return folderCount, runningProjects, stoppedProjects, totalProjects, nil
	}

	// 2. Group by project
	containersByProject := make(map[string][]container.Summary)
	for _, c := range containers {
		projName := c.Labels["com.docker.compose.project"]
		if projName != "" {
			containersByProject[projName] = append(containersByProject[projName], c)
		}
	}

	// 3. Calculate status for each project
	for _, p := range projectsList {
		normName := normalizeComposeProjectName(p.Name)
		projectContainers := containersByProject[normName]

		// Convert to ProjectServiceInfo (minimal needed for calculateProjectStatus)
		var services []ProjectServiceInfo
		for _, c := range projectContainers {
			services = append(services, ProjectServiceInfo{
				Status: string(c.State),
			})
		}

		var status models.ProjectStatus
		if len(services) == 0 {
			status = models.ProjectStatusStopped
		} else {
			status = s.calculateProjectStatus(services)
		}

		s.incrementStatusCounts(status, &runningProjects, &stoppedProjects)
	}

	return folderCount, runningProjects, stoppedProjects, totalProjects, nil
}

// End Helpers

// Project Actions

func (s *ProjectService) DeployProject(ctx context.Context, projectID string, user models.User, options *project.DeployOptions) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	resolvedPullPolicy := ""
	forceRecreate := false
	if options != nil {
		resolvedPullPolicy = normalizeDeployPullPolicyInternal(options.PullPolicy)
		forceRecreate = options.ForceRecreate
	}
	if resolvedPullPolicy == "" {
		resolvedPullPolicy = normalizeDeployPullPolicyInternal(s.settingsService.GetStringSetting(ctx, "defaultDeployPullPolicy", "missing"))
	}
	if resolvedPullPolicy == "" {
		resolvedPullPolicy = "missing"
	}

	composeFileFullPath, derr := projects.DetectComposeFile(projectFromDb.Path)
	if derr != nil {
		return fmt.Errorf("no compose file found in project directory: %s", projectFromDb.Path)
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if loadErr != nil {
		return fmt.Errorf("failed to load compose project from %s: %w", projectFromDb.Path, loadErr)
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusDeploying); err != nil {
		return fmt.Errorf("failed to update project status to deploying: %w", err)
	}

	progressWriter, _ := ctx.Value(projects.ProgressWriterKey{}).(io.Writer)
	if perr := s.prepareProjectImagesForDeploy(ctx, projectID, project, progressWriter, nil, &user, resolvedPullPolicy); perr != nil {
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)
		return fmt.Errorf("failed to prepare project images for deploy: %w", perr)
	}

	removeOrphans := projectFromDb.GitOpsManagedBy != nil && *projectFromDb.GitOpsManagedBy != ""

	slog.Info("starting compose up with health check support", "projectID", projectID, "projectName", project.Name, "services", len(project.Services), "removeOrphans", removeOrphans)
	// Health/progress streaming (if any) is handled inside projects.ComposeUp via ctx.
	if err := projects.ComposeUp(ctx, project, nil, removeOrphans, forceRecreate); err != nil {
		slog.Error("compose up failed", "projectName", project.Name, "projectID", projectID, "error", err)
		if containers, psErr := s.GetProjectServices(ctx, projectID); psErr == nil {
			slog.Info("containers after failed deploy", "projectID", projectID, "containers", containers)
		}
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)

		// Provide more helpful error messages
		errMsg := err.Error()
		if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "context deadline exceeded") {
			return fmt.Errorf("deployment timed out - check if services with 'condition: service_healthy' have healthchecks defined: %w", err)
		}
		return fmt.Errorf("failed to deploy project: %w", err)
	}
	slog.Info("compose up completed successfully", "projectID", projectID, "projectName", project.Name)

	metadata := models.JSON{"action": "deploy", "projectID": projectID, "projectName": project.Name}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectDeploy, projectID, project.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project deployment action", "error", logErr)
	}

	err = s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusRunning)
	if err != nil {
		slog.Error("failed to update project status and counts after deploy", "projectID", projectID, "error", err)
	}
	return err
}

func (s *ProjectService) DownProject(ctx context.Context, projectID string, user models.User) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	proj, _, lerr := projects.LoadComposeProjectFromDir(ctx, projectFromDb.Path, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusStopped); err != nil {
		return fmt.Errorf("failed to update project status to stopping: %w", err)
	}

	if err := projects.ComposeDown(ctx, proj, false); err != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return fmt.Errorf("failed to bring down project: %w", err)
	}

	metadata := models.JSON{
		"action":      "down",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
	}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectStop, projectID, projectFromDb.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project down action", "error", logErr)
	}

	return s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusStopped)
}

func (s *ProjectService) CreateProject(ctx context.Context, name, composeContent string, envContent *string, user models.User) (*models.Project, error) {
	sanitized := projects.SanitizeProjectName(name)

	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	basePath := filepath.Join(projectsDirectory, sanitized)
	projectPath, folderName, err := projects.CreateUniqueDir(projectsDirectory, basePath, name, common.DirPerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	proj := &models.Project{
		Name:         name,
		DirName:      &folderName,
		Path:         projectPath,
		Status:       models.ProjectStatusStopped,
		ServiceCount: 0,
		RunningCount: 0,
	}

	if err := s.db.WithContext(ctx).Create(proj).Error; err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}

	if err := projects.SaveOrUpdateProjectFiles(projectsDirectory, projectPath, composeContent, envContent); err != nil {
		// Best-effort cleanup to restore pre-transaction behavior.
		_ = s.db.WithContext(ctx).Delete(proj).Error
		return nil, fmt.Errorf("failed to save project files: %w", err)
	}

	metadata := models.JSON{"action": "create", "projectID": proj.ID, "projectName": name, "path": projectPath}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectCreate, proj.ID, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project creation", "error", logErr)
	}

	return proj, nil
}

func (s *ProjectService) DestroyProject(ctx context.Context, projectID string, removeFiles, removeVolumes bool, user models.User) error {
	slog.DebugContext(ctx, "DestroyProject service called",
		"projectID", projectID,
		"removeFiles", removeFiles,
		"removeVolumes", removeVolumes,
		"userID", user.ID,
		"username", user.Username)

	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "Found project to destroy",
		"projectName", proj.Name,
		"projectPath", proj.Path)

	if err := s.DownProject(ctx, projectID, systemUser); err != nil {
		slog.WarnContext(ctx, "failed to bring down project", "error", err)
	}

	if removeVolumes {
		// Get configured projects directory from settings
		projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
		projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
		if pdErr != nil {
			slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
			projectsDirectory = "/app/data/projects"
		}

		autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
		pathMapper, pmErr := s.getPathMapper(ctx)
		if pmErr != nil {
			slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
		}

		if compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv, pathMapper); lerr == nil {
			if derr := projects.ComposeDown(ctx, compProj, true); derr != nil {
				slog.WarnContext(ctx, "failed to remove volumes", "error", derr)
			}
		} else {
			slog.WarnContext(ctx, "failed to load compose project for volume removal", "error", lerr)
		}
	}

	if removeFiles {
		slog.DebugContext(ctx, "Removing project files", "path", proj.Path)
		if err := os.RemoveAll(proj.Path); err != nil {
			slog.ErrorContext(ctx, "Failed to remove project files", "path", proj.Path, "error", err)
			return fmt.Errorf("failed to remove project files: %w", err)
		}
		slog.InfoContext(ctx, "Project files removed successfully", "path", proj.Path)
	} else {
		slog.DebugContext(ctx, "Skipping file removal (removeFiles=false)", "path", proj.Path)
	}

	if err := s.db.WithContext(ctx).Delete(proj).Error; err != nil {
		return fmt.Errorf("failed to delete project from database: %w", err)
	}

	metadata := models.JSON{"action": "destroy", "projectID": projectID, "projectName": proj.Name, "removeFiles": removeFiles, "removeVolumes": removeVolumes}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectDelete, projectID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project destroy action", "error", logErr)
	}

	return nil
}

func (s *ProjectService) RedeployProject(ctx context.Context, projectID string, user models.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	if err := s.PullProjectImages(ctx, projectID, io.Discard, user, nil); err != nil {
		slog.WarnContext(ctx, "failed to pull project images", "error", err)
	}

	metadata := models.JSON{"action": "redeploy", "projectID": projectID, "projectName": proj.Name}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectDeploy, projectID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project redeploy action", "error", logErr)
	}

	return s.DeployProject(ctx, projectID, user, nil)
}

func (s *ProjectService) PullProjectImages(ctx context.Context, projectID string, progressWriter io.Writer, user models.User, credentials []containerregistry.Credential) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if lerr != nil {
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	images := map[string]struct{}{}
	for _, svc := range compProj.Services {
		img := strings.TrimSpace(svc.Image)
		if img == "" {
			continue
		}
		images[img] = struct{}{}
	}

	settings := s.settingsService.GetSettingsConfig()

	for img := range images {
		err := func() error {
			pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
			defer pullCancel()
			if err := s.imageService.PullImage(pullCtx, img, progressWriter, user, credentials); err != nil {
				if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
					return fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", img)
				}
				return fmt.Errorf("failed to pull image %s: %w", img, err)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ProjectService) BuildProjectServices(ctx context.Context, projectID string, options ProjectBuildOptions, progressWriter io.Writer, user *models.User) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	composeFileFullPath, derr := projects.DetectComposeFile(projectFromDb.Path)
	if derr != nil {
		return fmt.Errorf("no compose file found in project directory: %s", projectFromDb.Path)
	}

	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if loadErr != nil {
		return fmt.Errorf("failed to load compose project from %s: %w", projectFromDb.Path, loadErr)
	}

	return s.buildProjectServicesInternal(ctx, projectID, project, options, progressWriter, user)
}

// EnsureProjectImagesPresent checks all compose service images for the project and
// pulls based on service pull policy:
// - always/refresh: always pull
// - missing/if_not_present/default: pull only if local image is missing
// - never: never pull (fails early if image is missing locally)
func (s *ProjectService) EnsureProjectImagesPresent(ctx context.Context, projectID string, progressWriter io.Writer, user models.User, credentials []containerregistry.Credential) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if lerr != nil {
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	pullPlan := buildProjectImagePullPlan(compProj.Services)

	return s.ensureImagesPresent(ctx, pullPlan, progressWriter, credentials, user)
}

func (s *ProjectService) ensureImagesPresent(ctx context.Context, pullPlan map[string]imagePullMode, progressWriter io.Writer, credentials []containerregistry.Credential, user models.User) error {
	settings := s.settingsService.GetSettingsConfig()

	for img, mode := range pullPlan {
		exists, ierr := s.imageService.ImageExistsLocally(ctx, img)
		if ierr != nil && mode != imagePullModeAlways {
			slog.WarnContext(ctx, "failed to check local image existence", "image", img, "error", ierr)
			// Non-fatal: attempt to pull to be safe
		}

		if mode == imagePullModeNever {
			if ierr != nil {
				slog.WarnContext(ctx, "pull_policy is 'never' but image presence check failed; continuing without pull", "image", img, "error", ierr)
				continue
			}
			if !exists {
				return fmt.Errorf("image %s is not available locally and pull_policy is 'never'", img)
			}
			slog.DebugContext(ctx, "pull_policy is 'never'; using local image without pull", "image", img)
			continue
		}

		if mode == imagePullModeIfMissing && exists {
			slog.DebugContext(ctx, "image already present locally; skipping pull", "image", img)
			continue
		}

		err := func() error {
			pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
			defer pullCancel()
			if err := s.imageService.PullImage(pullCtx, img, progressWriter, user, credentials); err != nil {
				if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
					return fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", img)
				}
				return fmt.Errorf("failed to pull image %s: %w", img, err)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ProjectService) pullImageForService(ctx context.Context, imageRef string, progressWriter io.Writer, credentials []containerregistry.Credential) error {
	settings := s.settingsService.GetSettingsConfig()
	pullCtx, pullCancel := timeouts.WithTimeout(ctx, settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull)
	defer pullCancel()

	if err := s.imageService.PullImage(pullCtx, imageRef, progressWriter, systemUser, credentials); err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", imageRef)
		}
		return err
	}

	return nil
}

func (s *ProjectService) prepareProjectImagesForDeploy(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	progressWriter io.Writer,
	credentials []containerregistry.Credential,
	user *models.User,
	pullPolicyOverride string,
) error {
	if project == nil {
		return nil
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	for name, svc := range project.Services {
		svc, imageName, updated := prepareDeployServiceConfig(projectID, project.Name, name, svc)
		if updated {
			project.Services[name] = svc
		}

		if imageName == "" {
			continue
		}

		decision := decideDeployImageAction(svc, pullPolicyOverride)
		if updated {
			decision = deployImageDecision{Build: true}
		}
		if err := s.ensureDeployServiceImageReady(ctx, projectID, project, name, svc, imageName, decision, progressWriter, credentials, user, pathMapper); err != nil {
			return err
		}
	}

	return nil
}

func prepareDeployServiceConfig(projectID, projectName, serviceName string, svc composetypes.ServiceConfig) (composetypes.ServiceConfig, string, bool) {
	if svc.Build == nil {
		return svc, strings.TrimSpace(svc.Image), false
	}

	resolvedImage, updatedSvc, updated := ensureServiceImage(projectID, projectName, serviceName, svc)
	return updatedSvc, resolvedImage, updated
}

func shouldPullDeployImage(decision deployImageDecision, exists bool) bool {
	return decision.PullAlways || (decision.PullIfMissing && !exists)
}

func (s *ProjectService) ensureDeployServiceImageReady(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	imageName string,
	decision deployImageDecision,
	progressWriter io.Writer,
	credentials []containerregistry.Credential,
	user *models.User,
	pathMapper *projects.PathMapper,
) error {
	if decision.Build {
		return s.buildServiceImageForDeploy(ctx, projectID, project, serviceName, svc, progressWriter, user, pathMapper)
	}

	exists, err := s.imageService.ImageExistsLocally(ctx, imageName)
	if err != nil {
		slog.WarnContext(ctx, "failed to check local image existence", "image", imageName, "error", err)
	}

	if decision.RequireLocalOnly {
		if !exists {
			return fmt.Errorf("image %s is not available locally and pull_policy is set to never", imageName)
		}
		return nil
	}

	if !shouldPullDeployImage(decision, exists) {
		return nil
	}

	if err := s.pullImageForService(ctx, imageName, progressWriter, credentials); err == nil {
		return nil
	} else if svc.Build != nil && decision.FallbackBuildOnPullFail {
		slog.WarnContext(ctx, "image pull failed, falling back to build", "service", serviceName, "image", imageName, "error", err)
		return s.buildServiceImageForDeploy(ctx, projectID, project, serviceName, svc, progressWriter, user, pathMapper)
	} else {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
}

func (s *ProjectService) buildServiceImageForDeploy(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	progressWriter io.Writer,
	user *models.User,
	pathMapper *projects.PathMapper,
) error {
	if s.buildService == nil {
		return fmt.Errorf("build service not available for service %s", serviceName)
	}

	buildReq, updatedSvc, updated, err := s.prepareServiceBuildRequest(ctx, projectID, project, serviceName, svc, ProjectBuildOptions{}, pathMapper)
	if err != nil {
		return err
	}
	if updated {
		project.Services[serviceName] = updatedSvc
	}

	if _, err := s.buildService.BuildImage(ctx, types.LOCAL_DOCKER_ENVIRONMENT_ID, buildReq, progressWriter, serviceName, user); err != nil {
		return err
	}

	return nil
}

func normalizeBuildSelections(services []string) map[string]struct{} {
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

func serviceSelected(selected map[string]struct{}, name string) bool {
	if len(selected) == 0 {
		return true
	}
	_, ok := selected[name]
	return ok
}

func ensureServiceImage(projectID, projectName, serviceName string, svc composetypes.ServiceConfig) (string, composetypes.ServiceConfig, bool) {
	imageName := strings.TrimSpace(svc.Image)
	if imageName == "" {
		imageName = buildLocalImageTag(projectID, projectName, serviceName)
		svc.Image = imageName
		return imageName, svc, true
	}
	return imageName, svc, false
}

func normalizePullPolicy(policy string) string {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "if_not_present" {
		return "missing"
	}
	return policy
}

func normalizeDeployPullPolicyInternal(policy string) string {
	normalized := normalizePullPolicy(policy)
	switch normalized {
	case "always", "missing", "never":
		return normalized
	default:
		return ""
	}
}

func isAlwaysPullPolicy(policy string) bool {
	if policy == "always" || policy == "daily" || policy == "weekly" {
		return true
	}
	return strings.HasPrefix(policy, "every_")
}

func decideDeployImageAction(svc composetypes.ServiceConfig, pullPolicyOverride string) deployImageDecision {
	policy := normalizePullPolicy(svc.PullPolicy)
	if policy == "" {
		if override := normalizeDeployPullPolicyInternal(pullPolicyOverride); override != "" {
			policy = override
		}
	}
	buildEnabled := svc.Build != nil

	if buildEnabled {
		switch {
		case policy == "build":
			return deployImageDecision{Build: true}
		case policy == "never":
			return deployImageDecision{RequireLocalOnly: true}
		case isAlwaysPullPolicy(policy):
			return deployImageDecision{PullAlways: true}
		case policy == "missing":
			return deployImageDecision{PullIfMissing: true}
		case policy == "":
			return deployImageDecision{PullIfMissing: true, FallbackBuildOnPullFail: true}
		default:
			return deployImageDecision{PullIfMissing: true}
		}
	}

	switch {
	case policy == "never":
		return deployImageDecision{RequireLocalOnly: true}
	case isAlwaysPullPolicy(policy):
		return deployImageDecision{PullAlways: true}
	default:
		return deployImageDecision{PullIfMissing: true}
	}
}

func resolveBuildContextInternal(workingDir string, svc composetypes.ServiceConfig, serviceName string) (string, error) {
	contextDir := strings.TrimSpace(svc.Build.Context)
	if contextDir == "" {
		contextDir = workingDir
	} else if _, isGitContext, err := libbuild.ParseGitBuildContextSource(contextDir); err != nil {
		return "", fmt.Errorf("invalid build context for service %s: %w", serviceName, err)
	} else if !isGitContext && !filepath.IsAbs(contextDir) {
		contextDir = filepath.Join(workingDir, contextDir)
	}

	if contextDir == "" {
		return "", fmt.Errorf("build context not set for service %s", serviceName)
	}

	return contextDir, nil
}

func resolveDockerfilePathInternal(svc composetypes.ServiceConfig) (string, error) {
	dockerfilePath := strings.TrimSpace(svc.Build.Dockerfile)
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	return dockerfilePath, nil
}

func translateBuildPathInternal(path string, pathMapper *projects.PathMapper) (string, error) {
	if pathMapper == nil || strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return path, nil
	}

	return pathMapper.ContainerToHost(path)
}

func buildArgsFromCompose(args map[string]*string) map[string]string {
	buildArgs := map[string]string{}
	for key, value := range args {
		if value == nil {
			continue
		}
		buildArgs[key] = *value
	}
	return buildArgs
}

func (s *ProjectService) resolveEffectiveBuildProvider(override string) string {
	provider := strings.ToLower(strings.TrimSpace(override))
	if provider != "" {
		return provider
	}

	if s.buildService != nil {
		provider = strings.ToLower(strings.TrimSpace(s.buildService.BuildSettings().BuildProvider))
	}

	if provider == "" {
		provider = "local"
	}

	return provider
}

func labelsFromCompose(labels composetypes.Labels) map[string]string {
	if len(labels) == 0 {
		return nil
	}

	out := make(map[string]string, len(labels))
	maps.Copy(out, labels)

	return out
}

func ulimitsFromCompose(ulimits map[string]*composetypes.UlimitsConfig) map[string]string {
	if len(ulimits) == 0 {
		return nil
	}

	out := make(map[string]string, len(ulimits))
	for name, cfg := range ulimits {
		if cfg == nil {
			continue
		}

		switch {
		case cfg.Single > 0:
			out[name] = fmt.Sprintf("%d", cfg.Single)
		case cfg.Soft > 0 || cfg.Hard > 0:
			out[name] = fmt.Sprintf("%d:%d", cfg.Soft, cfg.Hard)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func mergeBuildTags(primaryImage string, composeTags []string) []string {
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(composeTags)+1)

	appendTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		if _, ok := seen[tag]; ok {
			return
		}
		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}

	appendTag(primaryImage)
	for _, tag := range composeTags {
		appendTag(tag)
	}

	return merged
}

func buildPlatformsFromCompose(svc composetypes.ServiceConfig) []string {
	platforms := make([]string, 0, len(svc.Build.Platforms)+1)
	for _, platform := range svc.Build.Platforms {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}
		platforms = append(platforms, platform)
	}

	if len(platforms) == 0 {
		if servicePlatform := strings.TrimSpace(svc.Platform); servicePlatform != "" {
			platforms = append(platforms, servicePlatform)
		}
	}

	return platforms
}

func (s *ProjectService) prepareServiceBuildRequest(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	options ProjectBuildOptions,
	pathMapper *projects.PathMapper,
) (imagetypes.BuildRequest, composetypes.ServiceConfig, bool, error) {
	_ = ctx
	imageName, updatedSvc, updated := ensureServiceImage(projectID, project.Name, serviceName, svc)
	effectiveProvider := s.resolveEffectiveBuildProvider(options.Provider)

	if updated && effectiveProvider == "depot" {
		return imagetypes.BuildRequest{}, updatedSvc, updated, fmt.Errorf("service %s must define an image when using depot build provider", serviceName)
	}
	if updated && options.Push != nil && *options.Push {
		return imagetypes.BuildRequest{}, updatedSvc, updated, fmt.Errorf("service %s must define an image when push is enabled", serviceName)
	}

	contextDir, err := resolveBuildContextInternal(project.WorkingDir, updatedSvc, serviceName)
	if err != nil {
		return imagetypes.BuildRequest{}, updatedSvc, updated, err
	}
	contextDir, err = translateBuildPathInternal(contextDir, pathMapper)
	if err != nil {
		return imagetypes.BuildRequest{}, updatedSvc, updated, fmt.Errorf("translate build context for service %s: %w", serviceName, err)
	}

	dockerfileInline := updatedSvc.Build.DockerfileInline
	if strings.TrimSpace(updatedSvc.Build.Dockerfile) != "" && strings.TrimSpace(dockerfileInline) != "" {
		return imagetypes.BuildRequest{}, updatedSvc, updated, fmt.Errorf("service %s cannot define both dockerfile and dockerfile_inline", serviceName)
	}

	dockerfilePath := ""
	if strings.TrimSpace(dockerfileInline) == "" {
		dockerfilePath, err = resolveDockerfilePathInternal(updatedSvc)
		if err != nil {
			return imagetypes.BuildRequest{}, updatedSvc, updated, err
		}
		dockerfilePath, err = translateBuildPathInternal(dockerfilePath, pathMapper)
		if err != nil {
			return imagetypes.BuildRequest{}, updatedSvc, updated, fmt.Errorf("translate Dockerfile path for service %s: %w", serviceName, err)
		}
	}

	buildReq := imagetypes.BuildRequest{
		ContextDir:       contextDir,
		Dockerfile:       dockerfilePath,
		DockerfileInline: dockerfileInline,
		Tags:             mergeBuildTags(imageName, updatedSvc.Build.Tags),
		Target:           strings.TrimSpace(updatedSvc.Build.Target),
		BuildArgs:        buildArgsFromCompose(updatedSvc.Build.Args),
		Labels:           labelsFromCompose(updatedSvc.Build.Labels),
		CacheFrom:        append([]string(nil), updatedSvc.Build.CacheFrom...),
		CacheTo:          append([]string(nil), updatedSvc.Build.CacheTo...),
		NoCache:          updatedSvc.Build.NoCache,
		Pull:             updatedSvc.Build.Pull,
		Network:          strings.TrimSpace(updatedSvc.Build.Network),
		Isolation:        strings.TrimSpace(updatedSvc.Build.Isolation),
		ShmSize:          int64(updatedSvc.Build.ShmSize),
		Ulimits:          ulimitsFromCompose(updatedSvc.Build.Ulimits),
		Entitlements: append(
			[]string(nil),
			updatedSvc.Build.Entitlements...,
		),
		Privileged: updatedSvc.Build.Privileged,
		ExtraHosts: updatedSvc.Build.ExtraHosts.AsList(":"),
		Platforms:  buildPlatformsFromCompose(updatedSvc),
		Provider:   effectiveProvider,
	}
	if options.Push != nil {
		buildReq.Push = *options.Push
	}
	if options.Load != nil {
		buildReq.Load = *options.Load
	}

	return buildReq, updatedSvc, updated, nil
}

func (s *ProjectService) restoreProjectStatusAfterFailedDeployInternal(ctx context.Context, projectID string) {
	services, err := s.GetProjectServices(ctx, projectID)
	if err == nil {
		serviceCount, runningCount := s.getServiceCounts(services)
		status := s.calculateProjectStatus(services)
		if updateErr := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]any{
			"status":        status,
			"service_count": serviceCount,
			"running_count": runningCount,
			"updated_at":    time.Now(),
		}).Error; updateErr == nil {
			return
		} else {
			slog.WarnContext(ctx, "failed to restore project status after deploy failure", "projectID", projectID, "error", updateErr)
		}
	} else {
		slog.WarnContext(ctx, "failed to inspect project services after deploy failure", "projectID", projectID, "error", err)
	}

	if updateErr := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusStopped); updateErr != nil {
		slog.WarnContext(ctx, "failed to set stopped status after deploy failure", "projectID", projectID, "error", updateErr)
	}
}

func (s *ProjectService) buildProjectServicesInternal(ctx context.Context, projectID string, project *composetypes.Project, options ProjectBuildOptions, progressWriter io.Writer, user *models.User) error {
	if s.buildService == nil {
		return nil
	}
	if project == nil {
		return nil
	}

	selected := normalizeBuildSelections(options.Services)

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	buildCount := 0
	for name, svc := range project.Services {
		if svc.Build == nil {
			continue
		}
		if !serviceSelected(selected, name) {
			continue
		}

		buildReq, updatedSvc, updated, err := s.prepareServiceBuildRequest(ctx, projectID, project, name, svc, options, pathMapper)
		if err != nil {
			return err
		}
		if updated {
			project.Services[name] = updatedSvc
		}

		buildCount++
		if _, err := s.buildService.BuildImage(ctx, types.LOCAL_DOCKER_ENVIRONMENT_ID, buildReq, progressWriter, name, user); err != nil {
			return err
		}
	}

	if buildCount == 0 && len(selected) > 0 {
		return fmt.Errorf("no build-enabled services matched: %s", strings.Join(options.Services, ", "))
	}

	return nil
}

func buildLocalImageTag(projectID, projectName, serviceName string) string {
	shortID := strings.TrimSpace(projectID)
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	projectPart := sanitizeImageComponent(projectName)
	if projectPart == "" {
		projectPart = "project"
	}
	servicePart := sanitizeImageComponent(serviceName)
	if servicePart == "" {
		servicePart = "service"
	}

	return fmt.Sprintf("arcane.local/%s-%s/%s:latest", projectPart, shortID, servicePart)
}

func sanitizeImageComponent(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, value)
}

func (s *ProjectService) RestartProject(ctx context.Context, projectID string, user models.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRestarting); err != nil {
		return fmt.Errorf("failed to update project status to restarting: %w", err)
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, pdErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "/app/data/projects"
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	if err := projects.ComposeRestart(ctx, compProj, nil); err != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return fmt.Errorf("failed to restart project: %w", err)
	}

	metadata := models.JSON{
		"action":      "restart",
		"projectID":   projectID,
		"projectName": proj.Name,
	}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectStart, projectID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project restart action", "error", logErr)
	}

	return s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusRunning)
}

func (s *ProjectService) UpdateProject(ctx context.Context, projectID string, name *string, composeContent, envContent *string, user models.User) (*models.Project, error) {
	proj, projectsDirectory, err := s.getProjectForUpdate(ctx, projectID)
	if err != nil {
		return nil, err
	}

	if err := s.withProjectRenameRollback(ctx, &proj, func() error {
		if err := s.applyProjectRenameIfNeeded(&proj, name, projectsDirectory); err != nil {
			return err
		}
		if err := s.persistUpdatedProjectFiles(ctx, &proj, projectsDirectory, composeContent, envContent); err != nil {
			return err
		}
		if err := s.db.WithContext(ctx).Save(&proj).Error; err != nil {
			return fmt.Errorf("failed to update project: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// When compose content changes, recalculate service counts and status so the
	// overview doesn't show stale values (e.g. ghost services after removal).
	if composeContent != nil {
		if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
			slog.WarnContext(ctx, "failed to update service counts after compose edit", "projectID", proj.ID, "error", err)
		}
	}

	metadata := models.JSON{
		"action":      "update",
		"projectID":   proj.ID,
		"projectName": proj.Name,
	}
	if composeContent != nil {
		metadata["composeUpdated"] = true
	}
	if envContent != nil {
		metadata["envUpdated"] = true
	}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectUpdate, proj.ID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project update action", "error", logErr)
	}

	slog.InfoContext(ctx, "project updated", "projectID", proj.ID, "name", proj.Name)
	return &proj, nil
}

func (s *ProjectService) ApplyGitSyncProjectFiles(ctx context.Context, projectID string, composeContent string, gitEnvContent *string, user models.User) (*models.Project, error) {
	proj, projectsDirectory, err := s.getProjectForUpdate(ctx, projectID)
	if err != nil {
		return nil, err
	}

	envUpdate, err := s.prepareGitSyncEnvUpdateInternal(proj.Path, gitEnvContent)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve git env state: %w", err)
	}

	if err := s.validateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, composeContent, envUpdate.effectiveContent); err != nil {
		return nil, fmt.Errorf("invalid compose file: %w", err)
	}

	if err := projects.WriteComposeFile(projectsDirectory, proj.Path, composeContent); err != nil {
		return nil, fmt.Errorf("failed to save compose file: %w", err)
	}
	if err := s.persistGitSyncEnvFilesInternal(proj.Path, projectsDirectory, envUpdate); err != nil {
		return nil, fmt.Errorf("failed to sync git env files: %w", err)
	}
	if err := s.db.WithContext(ctx).Save(&proj).Error; err != nil {
		return nil, fmt.Errorf("failed to update project: %w", err)
	}

	// Recalculate service counts and status after compose file sync
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		slog.WarnContext(ctx, "failed to update service counts after git sync", "projectID", proj.ID, "error", err)
	}

	metadata := models.JSON{
		"action":         "git_sync_update",
		"projectID":      proj.ID,
		"projectName":    proj.Name,
		"composeUpdated": true,
		"envUpdated":     gitEnvContent != nil,
	}
	if gitEnvContent == nil {
		metadata["envSourceRemoved"] = true
	}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectUpdate, proj.ID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log git sync project update action", "error", logErr)
	}

	return &proj, nil
}

func (s *ProjectService) getProjectForUpdate(ctx context.Context, projectID string) (models.Project, string, error) {
	var proj models.Project
	if err := s.db.WithContext(ctx).First(&proj, "id = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Project{}, "", fmt.Errorf("project not found")
		}
		return models.Project{}, "", fmt.Errorf("failed to get project: %w", err)
	}

	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return models.Project{}, "", fmt.Errorf("failed to get projects directory: %w", err)
	}

	if err := s.ensureProjectPathUnderRoot(ctx, &proj, false); err != nil {
		return models.Project{}, "", err
	}

	return proj, projectsDirectory, nil
}

func (s *ProjectService) withProjectRenameRollback(ctx context.Context, proj *models.Project, run func() error) error {
	originalPath := proj.Path
	originalDirName := proj.DirName

	if err := run(); err != nil {
		if proj.Path != originalPath {
			if renameErr := os.Rename(proj.Path, originalPath); renameErr != nil {
				slog.WarnContext(ctx, "failed to rollback project directory rename", "from", proj.Path, "to", originalPath, "error", renameErr)
				return err
			}
			proj.Path = originalPath
			proj.DirName = originalDirName
		}
		return err
	}

	return nil
}

func (s *ProjectService) persistUpdatedProjectFiles(ctx context.Context, proj *models.Project, projectsDirectory string, composeContent, envContent *string) error {
	switch {
	case composeContent != nil:
		effectiveEnvContent, err := s.resolveEffectiveEnvContentForUpdateInternal(proj.Path, envContent)
		if err != nil {
			return fmt.Errorf("invalid compose file: %w", err)
		}
		if err := s.validateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, *composeContent, effectiveEnvContent); err != nil {
			return fmt.Errorf("invalid compose file: %w", err)
		}
		if err := projects.WriteComposeFile(projectsDirectory, proj.Path, *composeContent); err != nil {
			return fmt.Errorf("failed to save project files: %w", err)
		}
		if envContent != nil {
			if err := s.persistEffectiveEnvContentInternal(proj.Path, projectsDirectory, *envContent); err != nil {
				return fmt.Errorf("failed to save project files: %w", err)
			}
		} else if err := s.ensureEffectiveEnvFileInternal(proj.Path, projectsDirectory); err != nil {
			return fmt.Errorf("failed to save project files: %w", err)
		}
	case envContent != nil:
		if err := s.persistEffectiveEnvContentInternal(proj.Path, projectsDirectory, *envContent); err != nil {
			return err
		}
	}

	return nil
}

func (s *ProjectService) validateComposeContentForUpdate(ctx context.Context, projectsDirectory, projectPath, projectName, composeContent string, effectiveEnvContent *string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("compose file contains invalid syntax: %v", recovered)
		}
	}()

	fullEnvMap, envErr := buildComposeValidationEnvironment(projectsDirectory, projectPath, effectiveEnvContent)
	if envErr != nil {
		return envErr
	}

	validationProjectName := normalizeComposeProjectName(projectName)
	cfg := composetypes.ConfigDetails{
		Version:    api.ComposeVersion,
		WorkingDir: projectPath,
		ConfigFiles: []composetypes.ConfigFile{
			{Filename: filepath.Join(projectPath, "compose.yaml"), Content: []byte(composeContent)},
		},
		Environment: composetypes.Mapping(fullEnvMap),
	}

	err = withTransientValidationEnvFile(projectPath, effectiveEnvContent, func() error {
		_, loadErr := loader.LoadWithContext(ctx, cfg, func(opts *loader.Options) {
			if validationProjectName != "" {
				opts.SetProjectName(validationProjectName, true)
			}
		})
		return loadErr
	})

	return err
}

func buildComposeValidationEnvironment(projectsDirectory, projectPath string, effectiveEnvContent *string) (projects.EnvMap, error) {
	// Validation should match project-visible env sources without inheriting the
	// Arcane process environment, which may contain unrelated secrets.
	fullEnvMap := make(projects.EnvMap)
	if absWorkdir, absErr := filepath.Abs(projectPath); absErr == nil {
		fullEnvMap["PWD"] = absWorkdir
	} else {
		fullEnvMap["PWD"] = projectPath
	}

	globalEnvPath := filepath.Join(projectsDirectory, projects.GlobalEnvFileName)
	globalEnv, err := parseComposeValidationEnvFile(globalEnvPath, fullEnvMap)
	if err != nil {
		return nil, fmt.Errorf("parse global env file: %w", err)
	}
	maps.Copy(fullEnvMap, globalEnv)

	if effectiveEnvContent != nil {
		projectEnv, err := parseComposeValidationEnvContent(*effectiveEnvContent, fullEnvMap)
		if err != nil {
			return nil, fmt.Errorf("parse provided env content: %w", err)
		}
		maps.Copy(fullEnvMap, projectEnv)
		return fullEnvMap, nil
	}

	projectEnvPath := filepath.Join(projectPath, ".env")
	projectEnv, err := parseComposeValidationEnvFile(projectEnvPath, fullEnvMap)
	if err != nil {
		return nil, fmt.Errorf("parse project env file: %w", err)
	}
	maps.Copy(fullEnvMap, projectEnv)

	return fullEnvMap, nil
}

func parseComposeValidationEnvFile(path string, contextEnv projects.EnvMap) (projects.EnvMap, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat file: %w", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return parseComposeValidationEnvContent(string(content), contextEnv)
}

func parseComposeValidationEnvContent(content string, contextEnv projects.EnvMap) (projects.EnvMap, error) {
	lookupFn := func(key string) (string, bool) {
		value, ok := contextEnv[key]
		return value, ok
	}

	envMap, err := dotenv.ParseWithLookup(strings.NewReader(content), lookupFn)
	if err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	return projects.EnvMap(envMap), nil
}

func withTransientValidationEnvFile(projectPath string, effectiveEnvContent *string, run func() error) (err error) {
	envPath := filepath.Join(projectPath, ".env")
	originalContent, readErr := os.ReadFile(envPath)
	originalExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("prepare env file for compose validation: %w", readErr)
	}

	shouldWrite := effectiveEnvContent != nil || !originalExists
	if shouldWrite {
		content := ""
		if effectiveEnvContent != nil {
			content = *effectiveEnvContent
		}
		if writeErr := projects.WriteEnvFile(projectPath, projectPath, content); writeErr != nil {
			return fmt.Errorf("prepare env file for compose validation: %w", writeErr)
		}

		defer func() {
			var restoreErr error
			switch {
			case originalExists:
				restoreErr = projects.WriteEnvFile(projectPath, projectPath, string(originalContent))
			case effectiveEnvContent != nil:
				restoreErr = os.Remove(envPath)
			default:
				restoreErr = os.Remove(envPath)
			}

			if restoreErr != nil && !os.IsNotExist(restoreErr) {
				if err == nil {
					err = fmt.Errorf("restore env file after compose validation: %w", restoreErr)
				}
			}
		}()
	}

	if run == nil {
		return nil
	}

	return run()
}

func (s *ProjectService) resolveEffectiveEnvContentForUpdateInternal(projectPath string, envContent *string) (*string, error) {
	if envContent != nil {
		return envContent, nil
	}

	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return nil, fmt.Errorf("read project env state: %w", err)
	}

	effectiveContent, err := s.resolveStoredEffectiveEnvContentInternal(state)
	if err != nil {
		return nil, err
	}
	if effectiveContent == "" && !state.HasEffective && !state.HasGitSource && !state.HasOverride {
		return nil, nil
	}

	return &effectiveContent, nil
}

func (s *ProjectService) resolveStoredEffectiveEnvContentInternal(state projects.ProjectEnvState) (string, error) {
	if state.HasEffective {
		return state.EffectiveContent, nil
	}
	if state.HasGitSource || state.HasOverride {
		effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", fmt.Errorf("build effective env content: %w", err)
		}
		return effectiveContent, nil
	}
	return state.DirectContent, nil
}

func (s *ProjectService) persistEffectiveEnvContentInternal(projectPath, projectsDirectory, envContent string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
		}
		return projects.WriteEnvFile(projectsDirectory, projectPath, envContent)
	}

	overrideContent, err := projects.BuildOverrideEnvContent(state.GitContent, envContent)
	if err != nil {
		return fmt.Errorf("build override env content: %w", err)
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, overrideContent)
	if err != nil {
		return fmt.Errorf("build effective env content: %w", err)
	}

	if err := projects.WriteEnvFile(projectsDirectory, projectPath, effectiveContent); err != nil {
		return err
	}

	if strings.TrimSpace(overrideContent) == "" {
		if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
			return err
		}
	} else if err := projects.WriteProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName, overrideContent); err != nil {
		return err
	}

	return nil
}

func (s *ProjectService) ensureEffectiveEnvFileInternal(projectPath, projectsDirectory string) error {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return fmt.Errorf("read project env state: %w", err)
	}

	if !state.HasGitSource {
		if state.HasOverride {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
			effectiveContent, err := s.resolveStoredEffectiveEnvContentInternal(state)
			if err != nil {
				return err
			}
			return projects.WriteEnvFile(projectsDirectory, projectPath, effectiveContent)
		}
		return projects.EnsureEnvFile(projectsDirectory, projectPath)
	}

	effectiveContent, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
	if err != nil {
		return fmt.Errorf("build effective env content: %w", err)
	}

	return projects.WriteEnvFile(projectsDirectory, projectPath, effectiveContent)
}

type gitSyncEnvUpdateInternal struct {
	state            projects.ProjectEnvState
	gitEnvContent    *string
	overrideContent  string
	effectiveContent *string
}

func (s *ProjectService) prepareGitSyncEnvUpdateInternal(projectPath string, gitEnvContent *string) (gitSyncEnvUpdateInternal, error) {
	state, err := projects.ReadProjectEnvState(projectPath)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, fmt.Errorf("read project env state: %w", err)
	}

	update := gitSyncEnvUpdateInternal{
		state:         state,
		gitEnvContent: gitEnvContent,
	}

	if gitEnvContent == nil {
		effectiveContent, err := s.resolveStoredEffectiveEnvContentInternal(state)
		if err != nil {
			return gitSyncEnvUpdateInternal{}, err
		}
		if effectiveContent == "" && !state.HasEffective && !state.HasGitSource && !state.HasOverride {
			return update, nil
		}
		update.effectiveContent = &effectiveContent
		return update, nil
	}

	overrideContent, err := s.resolveOverrideContentForGitSyncInternal(state, *gitEnvContent)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, err
	}
	update.overrideContent = overrideContent

	effectiveContent, err := projects.BuildEffectiveEnvContent(*gitEnvContent, overrideContent)
	if err != nil {
		return gitSyncEnvUpdateInternal{}, fmt.Errorf("build effective env content: %w", err)
	}
	update.effectiveContent = &effectiveContent

	return update, nil
}

func (s *ProjectService) resolveOverrideContentForGitSyncInternal(state projects.ProjectEnvState, gitEnvContent string) (string, error) {
	switch {
	case state.HasGitSource:
		overrideContent, err := projects.BuildOverrideEnvContent(state.GitContent, state.OverrideContent)
		if err != nil {
			return "", fmt.Errorf("build override env content: %w", err)
		}
		return overrideContent, nil
	case state.HasOverride:
		effectiveContent, err := s.resolveStoredEffectiveEnvContentInternal(state)
		if err != nil {
			return "", err
		}
		overrideContent, err := projects.BuildOverrideEnvContent(gitEnvContent, effectiveContent)
		if err != nil {
			return "", fmt.Errorf("build override env content: %w", err)
		}
		return overrideContent, nil
	case strings.TrimSpace(state.DirectContent) != "":
		overrideContent, err := projects.BuildAdditiveOverrideEnvContent(gitEnvContent, state.DirectContent)
		if err != nil {
			return "", fmt.Errorf("build override env content: %w", err)
		}
		return overrideContent, nil
	default:
		return "", nil
	}
}

func (s *ProjectService) persistGitSyncEnvFilesInternal(projectPath, projectsDirectory string, update gitSyncEnvUpdateInternal) error {
	if update.gitEnvContent == nil {
		if update.state.HasGitSource {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.GitSourceEnvFileName); err != nil {
				return err
			}
		}
		if update.state.HasOverride {
			if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
				return err
			}
		}
		if update.effectiveContent != nil || update.state.HasEffective || update.state.HasGitSource || update.state.HasOverride {
			effectiveContent := ""
			if update.effectiveContent != nil {
				effectiveContent = *update.effectiveContent
			}
			return projects.WriteEnvFile(projectsDirectory, projectPath, effectiveContent)
		}
		return projects.EnsureEnvFile(projectsDirectory, projectPath)
	}

	if update.effectiveContent == nil {
		return fmt.Errorf("missing effective env content for git sync update")
	}

	if err := projects.WriteEnvFile(projectsDirectory, projectPath, *update.effectiveContent); err != nil {
		return err
	}
	if err := projects.WriteProjectFile(projectsDirectory, projectPath, projects.GitSourceEnvFileName, *update.gitEnvContent); err != nil {
		return err
	}
	if strings.TrimSpace(update.overrideContent) == "" {
		if err := projects.RemoveProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName); err != nil {
			return err
		}
	} else if err := projects.WriteProjectFile(projectsDirectory, projectPath, projects.OverrideEnvFileName, update.overrideContent); err != nil {
		return err
	}

	return nil
}

func (s *ProjectService) applyProjectRenameIfNeeded(proj *models.Project, name *string, projectsDirectory string) error {
	if name == nil {
		return nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || proj.Name == newName {
		return nil
	}

	if proj.Status != models.ProjectStatusStopped {
		return fmt.Errorf("project must be stopped before renaming (current status: %s)", proj.Status)
	}

	newDirName := projects.SanitizeProjectName(newName)
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return fmt.Errorf("invalid project name: results in empty directory name")
	}

	currentPath := filepath.Clean(proj.Path)
	targetPath := filepath.Clean(filepath.Join(projectsDirectory, newDirName))
	if currentPath != targetPath {
		if _, statErr := os.Stat(targetPath); statErr == nil {
			return fmt.Errorf("project directory already exists: %s", targetPath)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to check project directory rename target: %w", statErr)
		}

		if err := os.Rename(currentPath, targetPath); err != nil {
			return fmt.Errorf("failed to rename project directory: %w", err)
		}

		proj.Path = targetPath
	}

	proj.DirName = &newDirName
	proj.Name = newName
	return nil
}

func (s *ProjectService) UpdateProjectIncludeFile(ctx context.Context, projectID, relativePath, content string, user models.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// Normalize and persist project path to ensure include writes occur under projects root
	if err := s.ensureProjectPathUnderRoot(ctx, proj, true); err != nil {
		return err
	}

	if err := projects.WriteIncludeFile(proj.Path, relativePath, content); err != nil {
		return fmt.Errorf("failed to update include file: %w", err)
	}

	// Recalculate service counts since include files can define services
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		slog.WarnContext(ctx, "failed to update service counts after include file edit", "projectID", proj.ID, "error", err)
	}

	metadata := models.JSON{
		"action":       "update_include",
		"projectID":    proj.ID,
		"projectName":  proj.Name,
		"relativePath": relativePath,
	}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectUpdate, proj.ID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project include update action", "error", logErr)
	}

	slog.InfoContext(ctx, "project include file updated", "projectID", proj.ID, "file", relativePath)
	return nil
}

// ensureProjectPathUnderRoot validates that the project's path is a safe subdirectory of the configured projects root.
// If not, it normalizes the path to `<projectsRoot>/<dirName or sanitized project name>`. When persist=true, it saves
// the updated project path to the database.
func (s *ProjectService) ensureProjectPathUnderRoot(ctx context.Context, proj *models.Project, persist bool) error {
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return fmt.Errorf("failed to get projects directory: %w", err)
	}

	rootAbs, _ := filepath.Abs(projectsDirectory)
	rootAbs = filepath.Clean(rootAbs)

	projPathAbs := proj.Path
	if abs, aerr := filepath.Abs(proj.Path); aerr == nil {
		projPathAbs = filepath.Clean(abs)
	}

	if projects.IsSafeSubdirectory(rootAbs, projPathAbs) {
		return nil
	}

	// Attempt to repair using known directory name or sanitized project name
	dirName := utils.DerefString(proj.DirName)
	if strings.TrimSpace(dirName) == "" {
		dirName = projects.SanitizeProjectName(proj.Name)
	}
	candidate := filepath.Join(projectsDirectory, dirName)

	slog.WarnContext(ctx, "Normalizing project path to projects root", "projectID", proj.ID, "oldPath", proj.Path, "newPath", candidate, "root", projectsDirectory)
	proj.Path = filepath.Clean(candidate)

	if persist {
		if saveErr := s.db.WithContext(ctx).Save(proj).Error; saveErr != nil {
			slog.WarnContext(ctx, "failed to persist normalized project path", "error", saveErr)
		}
	}
	return nil
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
		// since/timestamps not currently supported by ComposeLogs helper; follow/tail are used.
		err := projects.ComposeLogs(ctx, normalizeComposeProjectName(proj.Name), pw, follow, tail)
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

// End Project Actions

// Table Functions

func (s *ProjectService) ListProjects(ctx context.Context, params pagination.QueryParams) ([]project.Details, pagination.Response, error) {
	query := s.db.WithContext(ctx).Model(&models.Project{})
	statusFilter := ""
	if params.Filters != nil {
		statusFilter = strings.TrimSpace(params.Filters["status"])
	}
	if statusFilter != "" {
		return s.listProjectsByStatus(ctx, params, query)
	}

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		query = query.Where(
			"name LIKE ? OR path LIKE ? OR status LIKE ? OR COALESCE(dir_name, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	query = pagination.ApplyFilter(query, "status", params.Filters["status"])

	var projectsArray []models.Project
	paginationResp, err := pagination.PaginateAndSortDB(params, query, &projectsArray)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate projects: %w", err)
	}

	slog.DebugContext(ctx, "Retrieved projects from database",
		"count", len(projectsArray))

	// Fetch live status concurrently for all projects
	result := s.fetchProjectStatusConcurrently(ctx, projectsArray)

	slog.DebugContext(ctx, "Completed ListProjects request",
		"result_count", len(result))

	return result, paginationResp, nil
}

func (s *ProjectService) listProjectsByStatus(
	ctx context.Context,
	params pagination.QueryParams,
	query *gorm.DB,
) ([]project.Details, pagination.Response, error) {
	var projectsArray []models.Project
	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		query = query.Where(
			"name LIKE ? OR path LIKE ? OR status LIKE ? OR COALESCE(dir_name, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}
	if err := query.Find(&projectsArray).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list projects: %w", err)
	}

	items := s.fetchProjectStatusConcurrently(ctx, projectsArray)

	limit := params.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	params.Limit = limit

	config := pagination.Config[project.Details]{
		SearchAccessors: []pagination.SearchAccessor[project.Details]{
			func(p project.Details) (string, error) { return p.Name, nil },
			func(p project.Details) (string, error) { return p.Path, nil },
			func(p project.Details) (string, error) { return p.RelativePath, nil },
			func(p project.Details) (string, error) { return p.Status, nil },
			func(p project.Details) (string, error) { return p.DirName, nil },
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
			{
				Key: "status",
				Fn: func(p project.Details, filterValue string) bool {
					return strings.EqualFold(strings.TrimSpace(p.Status), strings.TrimSpace(filterValue))
				},
			},
		},
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := pagination.BuildResponseFromFilterResult(result, params)

	return result.Items, paginationResp, nil
}

// fetchProjectStatusConcurrently fetches live Docker status for multiple projects in parallel
// Optimized to use a single Docker API call instead of N calls + N file reads
func (s *ProjectService) fetchProjectStatusConcurrently(ctx context.Context, projectsList []models.Project) []project.Details {
	projectsDir, err := s.getProjectsDirectoryInternal(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for relative project paths", "error", err)
	}

	// 1. Fetch all compose containers in one go
	containers, err := projects.ListGlobalComposeContainers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers", "error", err)
		// Fallback: return basic info with unknown status
		results := make([]project.Details, len(projectsList))
		for i, p := range projectsList {
			_ = mapper.MapStruct(p, &results[i])
			results[i].CreatedAt = p.CreatedAt.Format(time.RFC3339)
			results[i].UpdatedAt = p.UpdatedAt.Format(time.RFC3339)
			results[i].DirName = utils.DerefString(p.DirName)
			results[i].RelativePath = s.getProjectRelativePathInternal(projectsDir, p.Path)
			results[i].GitOpsManagedBy = p.GitOpsManagedBy
			meta := s.getProjectMetadataFromPath(ctx, p.Path)
			results[i].IconURL = meta.ProjectIconURL
			results[i].URLs = meta.ProjectURLS
			results[i].Status = string(models.ProjectStatusUnknown)
		}
		return results
	}

	// 2. Group containers by project name
	containersByProject := make(map[string][]container.Summary)
	for _, c := range containers {
		projName := c.Labels["com.docker.compose.project"]
		if projName != "" {
			containersByProject[projName] = append(containersByProject[projName], c)
		}
	}

	// 3. Map to DTOs
	results := make([]project.Details, len(projectsList))
	for i, p := range projectsList {
		results[i] = s.mapProjectToDto(ctx, projectsDir, p, containersByProject)
	}

	return results
}

func (s *ProjectService) mapProjectToDto(ctx context.Context, projectsDir string, p models.Project, containersByProject map[string][]container.Summary) project.Details {
	var resp project.Details
	_ = mapper.MapStruct(p, &resp)

	resp.CreatedAt = p.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = p.UpdatedAt.Format(time.RFC3339)
	resp.DirName = utils.DerefString(p.DirName)
	resp.RelativePath = s.getProjectRelativePathInternal(projectsDir, p.Path)
	resp.GitOpsManagedBy = p.GitOpsManagedBy
	meta := s.getProjectMetadataFromPath(ctx, p.Path)
	resp.IconURL = meta.ProjectIconURL
	resp.URLs = meta.ProjectURLS

	// Find containers for this project
	normName := normalizeComposeProjectName(p.Name)
	projectContainers := containersByProject[normName]

	var services []ProjectServiceInfo
	runningCount := 0

	for _, c := range projectContainers {
		svcName := c.Labels["com.docker.compose.service"]
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

		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}

		services = append(services, ProjectServiceInfo{
			Name:          svcName,
			Image:         c.Image,
			Status:        string(state),
			ContainerID:   c.ID,
			ContainerName: containerName,
			Ports:         formatDockerPorts(c.Ports),
			Health:        health,
		})

		if state == "running" {
			runningCount++
		}
	}

	// Convert to RuntimeServices
	runtimeServices := make([]project.RuntimeService, len(services))
	for k, s := range services {
		runtimeServices[k] = project.RuntimeService{
			Name:          s.Name,
			Image:         s.Image,
			Status:        s.Status,
			ContainerID:   s.ContainerID,
			ContainerName: s.ContainerName,
			Ports:         s.Ports,
			Health:        s.Health,
			ServiceConfig: s.ServiceConfig,
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
			s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", pid).Update("service_count", count)
		}(context.WithoutCancel(ctx), p.ID, resp.ServiceCount)
	}

	// Fix for missing service count (e.g. newly discovered projects)
	if resp.ServiceCount == 0 {
		if count, err := s.countServicesFromCompose(ctx, p); err == nil && count > 0 {
			resp.ServiceCount = count
			// Update DB asynchronously
			go func(ctx context.Context, pid string, c int) {
				s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", pid).Update("service_count", c)
			}(context.WithoutCancel(ctx), p.ID, count)
		}
	}

	// Calculate Status using actual container count from Docker rather than the
	// (potentially stale) DB ServiceCount. The DB value can become outdated when
	// a service is removed from the compose file but compose parsing fails during
	// filesystem sync, leaving the old count in the database. This mirrors the
	// logic in calculateProjectStatus and GetProjectDetails, which both use the
	// live container/service list as the source of truth.
	actualServiceCount := len(services)
	if actualServiceCount == 0 {
		resp.Status = string(models.ProjectStatusStopped)
	} else {
		switch {
		case runningCount >= actualServiceCount:
			resp.Status = string(models.ProjectStatusRunning)
		case runningCount > 0:
			resp.Status = string(models.ProjectStatusPartiallyRunning)
		default:
			resp.Status = string(models.ProjectStatusStopped)
		}
	}

	return resp
}

func (s *ProjectService) getProjectMetadataFromPath(ctx context.Context, projectPath string) projects.ArcaneComposeMetadata {
	composeFile, err := projects.DetectComposeFile(projectPath)
	if err != nil {
		return projects.ArcaneComposeMetadata{ServiceIcons: map[string]string{}}
	}

	meta, err := projects.ParseArcaneComposeMetadata(ctx, composeFile)
	if err != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFile, "error", err)
		return projects.ArcaneComposeMetadata{ServiceIcons: map[string]string{}}
	}

	return meta
}

// End Table Functions

func (s *ProjectService) countServicesFromCompose(ctx context.Context, p models.Project) (int, error) {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		return 0, err
	}

	pathMapper, pmErr := s.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	proj, _, err := projects.LoadComposeProjectFromDir(ctx, p.Path, normalizeComposeProjectName(p.Name), projectsDirectory, autoInjectEnv, pathMapper)
	if err != nil {
		return 0, err
	}

	return len(proj.Services), nil
}

func (s *ProjectService) calculateProjectStatus(services []ProjectServiceInfo) models.ProjectStatus {
	if len(services) == 0 {
		return models.ProjectStatusUnknown
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
		return models.ProjectStatusRunning
	}
	if runningCount > 0 {
		return models.ProjectStatusPartiallyRunning
	}
	if stoppedCount > 0 {
		return models.ProjectStatusStopped
	}
	return models.ProjectStatusUnknown
}
