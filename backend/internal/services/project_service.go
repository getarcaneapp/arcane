package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/docker/api/types/container"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/fs"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/backend/internal/utils/projects"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	"github.com/getarcaneapp/arcane/types/project"
	"gorm.io/gorm"
)

type ProjectService struct {
	db              *database.DB
	settingsService *SettingsService
	eventService    *EventService
	imageService    *ImageService
}

func NewProjectService(db *database.DB, settingsService *SettingsService, eventService *EventService, imageService *ImageService) *ProjectService {
	return &ProjectService{
		db:              db,
		settingsService: settingsService,
		eventService:    eventService,
		imageService:    imageService,
	}
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
	ServiceConfig *composetypes.ServiceConfig `json:"service_config,omitempty"`
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

	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]interface{}{
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
	res := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", id).Updates(map[string]interface{}{
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
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "data/projects"
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv)
	if loadErr != nil {
		return []ProjectServiceInfo{}, fmt.Errorf("failed to load compose project from %s: %w", projectFromDb.Path, loadErr)
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
			health = &c.Health
		}

		var svcConfig *composetypes.ServiceConfig
		if cfg, ok := serviceConfigs[c.Service]; ok {
			svcConfig = &cfg
		}

		services = append(services, ProjectServiceInfo{
			Name:          c.Service,
			Image:         c.Image,
			Status:        c.State,
			ContainerID:   c.ID,
			ContainerName: c.Name,
			Ports:         formatPorts(c.Publishers),
			Health:        health,
			ServiceConfig: svcConfig,
		})
		have[c.Service] = true
	}

	for _, svc := range project.Services {
		if !have[svc.Name] {
			svcCopy := svc
			services = append(services, ProjectServiceInfo{
				Name:          svc.Name,
				Image:         svc.Image,
				Status:        "stopped",
				Ports:         []string{},
				ServiceConfig: &svcCopy,
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
	return fs.ReadProjectFiles(proj.Path)
}

func (s *ProjectService) GetProjectDetails(ctx context.Context, projectID string) (project.Details, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return project.Details{}, err
	}

	composeContent, envContent, _ := s.GetProjectContent(ctx, projectID)

	// Parse include files from the compose file
	var includeFiles []project.IncludeFile
	composeFile, detectErr := projects.DetectComposeFile(proj.Path)
	if detectErr == nil {
		includes, parseErr := projects.ParseIncludes(composeFile)
		if parseErr == nil {
			slog.InfoContext(ctx, "Parsed includes", "count", len(includes), "projectID", projectID)
			for _, inc := range includes {
				includeFiles = append(includeFiles, project.IncludeFile{
					Path:         inc.Path,
					RelativePath: inc.RelativePath,
					Content:      inc.Content,
				})
			}
		} else {
			slog.WarnContext(ctx, "Failed to parse includes", "error", parseErr, "projectID", projectID)
		}
	} else {
		slog.WarnContext(ctx, "Failed to detect compose file", "error", detectErr, "projectID", projectID, "path", proj.Path)
	}

	services, serr := s.GetProjectServices(ctx, projectID)

	var serviceCount, runningCount int
	var liveStatus models.ProjectStatus

	if serr == nil && services != nil {
		serviceCount = len(services)
		_, runningCount = s.getServiceCounts(services)
		liveStatus = s.calculateProjectStatus(services)
	} else {
		serviceCount = proj.ServiceCount
		runningCount = proj.RunningCount
		liveStatus = proj.Status
	}

	var resp project.Details
	if err := mapper.MapStruct(proj, &resp); err != nil {
		return project.Details{}, fmt.Errorf("failed to map project: %w", err)
	}
	resp.Status = string(liveStatus)
	resp.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	resp.ComposeContent = composeContent
	resp.EnvContent = envContent
	resp.IncludeFiles = includeFiles
	resp.ServiceCount = serviceCount
	resp.RunningCount = runningCount
	resp.DirName = utils.DerefString(proj.DirName)

	// Load compose services from the compose file
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, _ := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if composeFile != "" {
		autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
		composeProj, loadErr := projects.LoadComposeProject(ctx, composeFile, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv)
		if loadErr == nil && composeProj != nil {
			// Convert map to slice
			svcList := make([]composetypes.ServiceConfig, 0, len(composeProj.Services))
			for _, svc := range composeProj.Services {
				svcList = append(svcList, svc)
			}
			resp.Services = svcList
		}
	}

	// Convert ProjectServiceInfo to project.RuntimeService
	if serr == nil && services != nil {
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
				ServiceConfig: svc.ServiceConfig,
			}
		}
		resp.RuntimeServices = runtimeServices
	}

	return resp, nil
}

func (s *ProjectService) SyncProjectsFromFileSystem(ctx context.Context) error {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDir, err := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		slog.WarnContext(ctx, "unable to prepare projects directory", "error", err)
		return nil
	}
	projectsDir = filepath.Clean(projectsDir)

	entries, rerr := os.ReadDir(projectsDir)
	if rerr != nil {
		slog.WarnContext(ctx, "failed to read projects directory", "dir", projectsDir, "error", rerr)
		return nil
	}

	seen := map[string]struct{}{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirName := e.Name()
		dirPath := filepath.Join(projectsDir, dirName)

		// Only consider folders that contain a compose file
		if _, derr := projects.DetectComposeFile(dirPath); derr != nil {
			continue
		}

		if uerr := s.upsertProjectForDir(ctx, dirName, dirPath); uerr != nil {
			slog.WarnContext(ctx, "failed to sync project from folder", "dir", dirPath, "error", uerr)
			continue
		}
		seen[dirPath] = struct{}{}
	}

	if cerr := s.cleanupDBProjects(ctx, seen); cerr != nil {
		slog.WarnContext(ctx, "error during DB cleanup of projects", "error", cerr)
	}

	return nil
}

func (s *ProjectService) upsertProjectForDir(ctx context.Context, dirName, dirPath string) error {
	var existing models.Project
	err := s.db.WithContext(ctx).
		Where("path = ? OR dir_name = ?", dirPath, dirName).
		First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create a minimal project entry
		reason := "Project discovered from filesystem, status pending Docker service query"
		proj := &models.Project{
			Name:         dirName,
			DirName:      &dirName,
			Path:         dirPath,
			Status:       models.ProjectStatusUnknown,
			StatusReason: &reason,
			ServiceCount: 0,
			RunningCount: 0,
		}
		slog.InfoContext(ctx, "Discovered new project with unknown status",
			"project", dirName,
			"path", dirPath,
			"reason", reason)
		if cerr := s.db.WithContext(ctx).Create(proj).Error; cerr != nil {
			return fmt.Errorf("create project for %q failed: %w", dirPath, cerr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("query existing project for %q failed: %w", dirPath, err)
	}

	updates := map[string]interface{}{}
	if existing.Path != dirPath {
		updates["path"] = dirPath
	}
	if existing.DirName == nil || *existing.DirName != dirName {
		updates["dir_name"] = dirName
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

func (s *ProjectService) cleanupDBProjects(ctx context.Context, seen map[string]struct{}) error {
	var all []models.Project
	if err := s.db.WithContext(ctx).Find(&all).Error; err != nil {
		return fmt.Errorf("list projects for cleanup failed: %w", err)
	}

	for _, p := range all {
		// Skip paths seen in this pass
		if _, ok := seen[p.Path]; ok {
			continue
		}

		// Remove if path missing or compose file missing
		if _, err := os.Stat(p.Path); err != nil {
			if os.IsNotExist(err) {
				if derr := s.db.WithContext(ctx).Delete(&models.Project{}, "id = ?", p.ID).Error; derr != nil {
					slog.WarnContext(ctx, "failed to delete missing-path project", "projectID", p.ID, "error", derr)
				}
				continue
			}
			// On unexpected stat error, skip deletion but warn
			slog.WarnContext(ctx, "stat error during cleanup", "path", p.Path, "error", err)
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

func formatDockerPorts(ports []container.Port) []string {
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
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDir, err := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		return 0, fmt.Errorf("could not determine projects directory: %w", err)
	}
	projectsDir = filepath.Clean(projectsDir)

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

	entries, readErr := os.ReadDir(projectsDir)
	if readErr != nil {
		return 0, fmt.Errorf("failed to read projects directory %s: %w", projectsDir, readErr)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, e.Name())
		if _, err := projects.DetectComposeFile(dirPath); err == nil {
			count++
		}
	}
	return count, nil
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
				Status: c.State,
			})
		}

		var status models.ProjectStatus
		if len(services) == 0 {
			status = models.ProjectStatusStopped
		} else {
			// We have containers, calculate status based on their state
			// Note: calculateProjectStatus doesn't know about "missing" services (ServiceCount)
			// So we need to check if runningCount == p.ServiceCount here if we want strict "Running"

			// Re-implement logic here to be safe or rely on calculateProjectStatus?
			// calculateProjectStatus returns Running if ALL *present* containers are running.
			// But if we have 2/3 containers running, it returns Running? No.
			// calculateProjectStatus: if runningCount == len(services) -> Running.
			// But len(services) is only the *running* containers (or present ones).
			// If we have 3 services defined, but only 2 containers exist (both running),
			// calculateProjectStatus will say "Running".
			// But strictly it should be "Partial" or "Restarting" or something.

			// However, for the dashboard count, "Running" usually means "Healthy".
			// Let's stick to calculateProjectStatus for consistency with previous logic,
			// but maybe check ServiceCount.

			st := s.calculateProjectStatus(services)

			// Refine: if all containers are running, but we have fewer containers than defined services
			if st == models.ProjectStatusRunning && len(services) < p.ServiceCount {
				st = models.ProjectStatusPartiallyRunning
			}
			status = st
		}

		s.incrementStatusCounts(status, &runningProjects, &stoppedProjects)
	}

	return folderCount, runningProjects, stoppedProjects, totalProjects, nil
}

// End Helpers

// Project Actions

func (s *ProjectService) DeployProject(ctx context.Context, projectID string, user models.User) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	composeFileFullPath, derr := projects.DetectComposeFile(projectFromDb.Path)
	if derr != nil {
		return fmt.Errorf("no compose file found in project directory: %s", projectFromDb.Path)
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "data/projects"
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv)
	if loadErr != nil {
		return fmt.Errorf("failed to load compose project from %s: %w", projectFromDb.Path, loadErr)
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusDeploying); err != nil {
		return fmt.Errorf("failed to update project status to deploying: %w", err)
	}

	if perr := s.EnsureProjectImagesPresent(ctx, projectID, io.Discard, nil); perr != nil {
		slog.Warn("ensure images present failed (continuing to compose up)", "projectID", projectID, "error", perr)
	}

	if err := projects.ComposeUp(ctx, project, project.Services.GetProfiles()); err != nil {
		slog.Error("compose up failed", "projectName", project.Name, "projectID", projectID, "error", err)
		if containers, psErr := s.GetProjectServices(ctx, projectID); psErr == nil {
			slog.Info("containers after failed deploy", "projectID", projectID, "containers", containers)
		}
		_ = s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusStopped)
		return fmt.Errorf("failed to deploy project: %w", err)
	}

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
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "data/projects"
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	proj, _, lerr := projects.LoadComposeProjectFromDir(ctx, projectFromDb.Path, normalizeComposeProjectName(projectFromDb.Name), projectsDirectory, autoInjectEnv)
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
	sanitized := fs.SanitizeProjectName(name)

	projectsDirectory, err := fs.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects"))
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	basePath := filepath.Join(projectsDirectory, sanitized)
	projectPath, folderName, err := fs.CreateUniqueDir(projectsDirectory, basePath, name, 0755)
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

	if err := fs.SaveOrUpdateProjectFiles(projectsDirectory, projectPath, composeContent, envContent); err != nil {
		s.db.WithContext(ctx).Delete(proj)
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
		projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
		projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
		if pdErr != nil {
			slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
			projectsDirectory = "data/projects"
		}

		autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
		if compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv); lerr == nil {
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

	if err := s.PullProjectImages(ctx, projectID, io.Discard, nil); err != nil {
		slog.WarnContext(ctx, "failed to pull project images", "error", err)
	}

	metadata := models.JSON{"action": "redeploy", "projectID": projectID, "projectName": proj.Name}
	if logErr := s.eventService.LogProjectEvent(ctx, models.EventTypeProjectDeploy, projectID, proj.Name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, "could not log project redeploy action", "error", logErr)
	}

	return s.DeployProject(ctx, projectID, systemUser)
}

func (s *ProjectService) PullProjectImages(ctx context.Context, projectID string, progressWriter io.Writer, credentials []containerregistry.Credential) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "data/projects"
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv)
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

	for img := range images {
		if err := s.imageService.PullImage(ctx, img, progressWriter, systemUser, credentials); err != nil {
			return fmt.Errorf("failed to pull image %s: %w", img, err)
		}
	}
	return nil
}

// EnsureProjectImagesPresent checks all compose service images for the project and
// only pulls images that are not already available locally.
func (s *ProjectService) EnsureProjectImagesPresent(ctx context.Context, projectID string, progressWriter io.Writer, credentials []containerregistry.Credential) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	// Get configured projects directory from settings
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "data/projects"
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv)
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

	for img := range images {
		exists, ierr := s.imageService.ImageExistsLocally(ctx, img)
		if ierr != nil {
			slog.WarnContext(ctx, "failed to check local image existence", "image", img, "error", ierr)
			// Non-fatal: attempt to pull to be safe
		}
		if exists {
			slog.DebugContext(ctx, "image already present locally; skipping pull", "image", img)
			continue
		}
		if err := s.imageService.PullImage(ctx, img, progressWriter, systemUser, credentials); err != nil {
			return fmt.Errorf("failed to pull missing image %s: %w", img, err)
		}
	}
	return nil
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
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, pdErr := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if pdErr != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", pdErr)
		projectsDirectory = "data/projects"
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, normalizeComposeProjectName(proj.Name), projectsDirectory, autoInjectEnv)
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

func (s *ProjectService) UpdateProject(ctx context.Context, projectID string, name *string, composeContent, envContent *string) (*models.Project, error) {
	var proj models.Project
	if err := s.db.WithContext(ctx).First(&proj, "id = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("project not found")
		}
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	// Get projects directory for security validation
	projectsDirectory, err := fs.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects"))
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	if name != nil {
		if newName := strings.TrimSpace(*name); newName != "" && proj.Name != newName {
			proj.Name = newName
		}
	}

	switch {
	case composeContent != nil:
		if err := fs.SaveOrUpdateProjectFiles(projectsDirectory, proj.Path, *composeContent, envContent); err != nil {
			return nil, fmt.Errorf("failed to save project files: %w", err)
		}
	case envContent != nil:
		envPath := filepath.Join(proj.Path, ".env")
		if *envContent == "" {
			if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to remove env file: %w", err)
			}
		} else {
			if err := fs.WriteEnvFile(projectsDirectory, proj.Path, *envContent); err != nil {
				return nil, err
			}
		}
	}

	if err := s.db.WithContext(ctx).Save(&proj).Error; err != nil {
		return nil, fmt.Errorf("failed to update project: %w", err)
	}

	slog.InfoContext(ctx, "project updated", "projectID", proj.ID, "name", proj.Name)
	return &proj, nil
}

func (s *ProjectService) UpdateProjectIncludeFile(ctx context.Context, projectID, relativePath, content string) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	if err := projects.WriteIncludeFile(proj.Path, relativePath, content); err != nil {
		return fmt.Errorf("failed to update include file: %w", err)
	}

	slog.InfoContext(ctx, "project include file updated", "projectID", proj.ID, "file", relativePath)
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
		err := projects.ComposeLogs(ctx, proj.Name, pw, follow, tail)
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
	var projectsArray []models.Project
	query := s.db.WithContext(ctx).Model(&models.Project{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		query = query.Where(
			"name LIKE ? OR path LIKE ? OR status LIKE ? OR COALESCE(dir_name, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

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

// fetchProjectStatusConcurrently fetches live Docker status for multiple projects in parallel
// Optimized to use a single Docker API call instead of N calls + N file reads
func (s *ProjectService) fetchProjectStatusConcurrently(ctx context.Context, projectsList []models.Project) []project.Details {
	// 1. Fetch all compose containers in one go
	containers, err := projects.ListGlobalComposeContainers(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list global compose containers", "error", err)
		// Fallback: return basic info with unknown status
		results := make([]project.Details, len(projectsList))
		for i, p := range projectsList {
			_ = mapper.MapStruct(p, &results[i])
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
		results[i] = s.mapProjectToDto(ctx, p, containersByProject)
	}

	return results
}

func (s *ProjectService) mapProjectToDto(ctx context.Context, p models.Project, containersByProject map[string][]container.Summary) project.Details {
	var resp project.Details
	_ = mapper.MapStruct(p, &resp)

	resp.CreatedAt = p.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = p.UpdatedAt.Format(time.RFC3339)
	resp.DirName = utils.DerefString(p.DirName)

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
			h := "healthy"
			health = &h
		case strings.Contains(statusLower, "(unhealthy)"):
			h := "unhealthy"
			health = &h
		case strings.Contains(statusLower, "(starting)"):
			h := "starting"
			health = &h
		}

		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}

		services = append(services, ProjectServiceInfo{
			Name:          svcName,
			Image:         c.Image,
			Status:        state,
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

	// Calculate Status
	if len(services) == 0 {
		resp.Status = string(models.ProjectStatusStopped)
	} else {
		switch {
		case runningCount >= resp.ServiceCount && resp.ServiceCount > 0:
			resp.Status = string(models.ProjectStatusRunning)
		case runningCount > 0:
			resp.Status = string(models.ProjectStatusPartiallyRunning)
		default:
			resp.Status = string(models.ProjectStatusStopped)
		}
	}

	return resp
}

// End Table Functions

func (s *ProjectService) countServicesFromCompose(ctx context.Context, p models.Project) (int, error) {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "data/projects")
	projectsDirectory, err := fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		return 0, err
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	proj, _, err := projects.LoadComposeProjectFromDir(ctx, p.Path, normalizeComposeProjectName(p.Name), projectsDirectory, autoInjectEnv)
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
