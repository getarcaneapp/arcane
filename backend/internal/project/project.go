package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	buildtypes "go.getarcane.app/builds/types"
	"gorm.io/gorm"
)

type buildServiceInternal interface {
	BuildImage(ctx context.Context, environmentID string, req buildtypes.BuildRequest, progressWriter io.Writer, serviceName string, user *common.User) (*buildtypes.BuildResult, error)
	BuildSettings() buildtypes.BuildSettings
}

type ProjectService struct {
	db                          *database.DB
	settingsService             *settings.SettingsService
	eventService                *event.EventService
	imageService                *image.ImageService
	dockerService               *docker.DockerClientService
	buildService                buildServiceInternal
	lifecycleService            *LifecycleService
	kvService                   *kv.KVService
	containerRegistryService    *registry.ContainerRegistryService
	config                      *config.Config
	registryCredentialsProvider registryCredentialsProviderInternal

	// syncMu serializes SyncProjectsFromFileSystem: its discovery walk and its
	// cleanup pass must not interleave with another run's.
	syncMu sync.Mutex

	composeNames  composeNameCacheInternal
	parsedCompose *parsedComposeCacheInternal
	// metaCache holds per-project icon/URL metadata, keyed by project ID. Deriving
	// it costs a full compose load (interpolation plus .env reads) and, for GitOps
	// projects, a gitops_syncs lookup — per project, on every list request.
	// Entries are validated by compose/include/env file mtimes rather than a TTL.
	metaCache *hot.HotCache[string, projectMetadataEntryInternal]
}

// EnsureGitOpsProjectLinked persists the bidirectional GitOps/project binding
// and refreshes the compose-name cache as one domain operation.
func (s *ProjectService) EnsureGitOpsProjectLinked(ctx context.Context, sync *GitOpsSync, project *Project) error {
	if sync == nil || project == nil {
		return nil
	}
	if project.GitOpsManagedBy != nil && *project.GitOpsManagedBy != "" && *project.GitOpsManagedBy != sync.ID {
		return errors.Errorf("project %s is already managed by a different GitOps sync", project.ID)
	}

	cacheBinding := func() {
		s.composeNames.put(projects.NormalizeProjectName(project.Name), project.ID)
	}
	if sync.ProjectID != nil && *sync.ProjectID == project.ID && project.GitOpsManagedBy != nil && *project.GitOpsManagedBy == sync.ID {
		cacheBinding()
		return nil
	}

	updatesSync := map[string]any{}
	updatesProject := map[string]any{}
	if sync.ProjectID == nil || *sync.ProjectID != project.ID {
		updatesSync["project_id"] = project.ID
	}
	if project.GitOpsManagedBy == nil || *project.GitOpsManagedBy != sync.ID {
		updatesProject["gitops_managed_by"] = sync.ID
	}
	if len(updatesSync) == 0 && len(updatesProject) == 0 {
		cacheBinding()
		return nil
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(updatesSync) > 0 {
			if err := tx.Model(&GitOpsSync{}).Where("id = ?", sync.ID).Updates(updatesSync).Error; err != nil {
				return errors.WrapIff(err, "failed to relink GitOps sync %s", sync.ID)
			}
		}
		if len(updatesProject) > 0 {
			if err := tx.Model(&Project{}).Where("id = ?", project.ID).Updates(updatesProject).Error; err != nil {
				return errors.WrapIff(err, "failed to relink project %s to GitOps sync %s", project.ID, sync.ID)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	sync.ProjectID = &project.ID
	project.GitOpsManagedBy = &sync.ID
	cacheBinding()
	return nil
}

// ValidateComposeDirectory loads a staged compose tree with the same settings,
// Docker path mapping, and validation rules used by managed projects.
func (s *ProjectService) ValidateComposeDirectory(ctx context.Context, projectName, projectPath, composeFileName string) (int, error) {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return 0, err
	}
	var dockerClient *client.Client
	if s.dockerService != nil {
		dockerClient, _ = s.dockerService.GetClient(ctx)
	}
	pathMapper := projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"),
		"/app/data/projects",
		dockerClient,
	)
	composeProject, err := projects.LoadComposeProject(
		ctx,
		filepath.Join(projectPath, composeFileName),
		projects.NormalizeProjectName(projectName),
		projectsDirectory,
		s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
		pathMapper,
		nil,
		nil,
		true,
	)
	if err != nil {
		return 0, err
	}
	return len(composeProject.Services), nil
}

// CreateGitOpsManagedProject persists a promoted GitOps project, links both
// records, updates the compose-name cache, and records the creation event.
func (s *ProjectService) CreateGitOpsManagedProject(ctx context.Context, sync *GitOpsSync, project *Project, actor common.User, logEventOptions ...bool) error {
	if sync == nil || project == nil {
		return errors.New("GitOps sync and project are required")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return errors.WrapIf(err, "failed to create project")
		}
		if err := tx.Model(&GitOpsSync{}).Where("id = ?", sync.ID).Update("project_id", project.ID).Error; err != nil {
			return errors.WrapIf(err, "failed to update sync with project ID")
		}
		if err := tx.Model(&Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", sync.ID).Error; err != nil {
			return errors.WrapIf(err, "failed to mark project as GitOps-managed")
		}
		return nil
	}); err != nil {
		return err
	}

	sync.ProjectID = &project.ID
	project.GitOpsManagedBy = &sync.ID
	s.composeNames.put(projects.NormalizeProjectName(project.Name), project.ID)
	if err := s.reconcileComposeTagsForProjectInternal(ctx, project); err != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags during GitOps project creation", "projectID", project.ID, "error", err)
	}
	logEvent := true
	if len(logEventOptions) > 0 {
		logEvent = logEventOptions[0]
	}
	if logEvent && s.eventService != nil {
		metadata := database.JSON{"action": "create", "projectID": project.ID, "projectName": project.Name, "path": project.Path}
		if err := s.eventService.LogProjectEvent(ctx, event.EventTypeProjectCreate, project.ID, project.Name, actor.ID, actor.Username, "0", metadata); err != nil {
			slog.ErrorContext(ctx, "could not log project creation", "error", err)
		}
	}
	return nil
}

// projectMetadataEnvInternal carries the inputs ParseArcaneComposeMetadata needs
// beyond the project itself. Resolving them costs a settings clone and a stat
// syscall each, so list paths resolve once and reuse across every project.
const maxConcurrentComposeReads = 8

type projectMetadataEnvInternal struct {
	projectsDirectory string
	autoInjectEnv     bool
}

type registryCredentialsProviderInternal func(context.Context) ([]containerregistry.Credential, error)

func NewProjectService(db *database.DB, settingsService *settings.SettingsService, eventService *event.EventService, imageService *image.ImageService, dockerService *docker.DockerClientService, buildService buildServiceInternal, lifecycleService *LifecycleService, containerRegistryService *registry.ContainerRegistryService, cfg *config.Config) *ProjectService {
	return &ProjectService{
		db:                       db,
		settingsService:          settingsService,
		eventService:             eventService,
		imageService:             imageService,
		dockerService:            dockerService,
		buildService:             buildService,
		lifecycleService:         lifecycleService,
		containerRegistryService: containerRegistryService,
		config:                   cfg,
		parsedCompose:            newParsedComposeCacheInternal(),
		metaCache:                hot.NewHotCache[string, projectMetadataEntryInternal](hot.LRU, 1024).Build(),
	}
}

func (s *ProjectService) WithRegistryCredentialsProvider(provider func(context.Context) ([]containerregistry.Credential, error)) *ProjectService {
	if s == nil {
		return nil
	}
	s.registryCredentialsProvider = provider
	return s
}

func (s *ProjectService) WithKVService(kvService *kv.KVService) *ProjectService {
	if s == nil {
		return nil
	}
	s.kvService = kvService
	return s
}

func (s *ProjectService) ResolveRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	if s == nil || s.registryCredentialsProvider == nil {
		return nil, nil
	}

	credentials, err := s.registryCredentialsProvider(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "get enabled registry credentials")
	}

	return credentials, nil
}

func (s *ProjectService) composeRegistryAuthConfigsInternal(ctx context.Context) map[string]dockerregistry.AuthConfig {
	if s == nil || s.containerRegistryService == nil {
		return nil
	}

	authConfigs, err := s.containerRegistryService.GetAllRegistryAuthConfigs(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load registry auth for compose pulls", "error", err)
		return nil
	}

	return authConfigs
}

func (s *ProjectService) GetProjectsDirectory(ctx context.Context) (string, error) {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	projectsDir, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
	if err != nil {
		return "", err
	}

	return filepath.Clean(projectsDir), nil
}

func getProjectsDirectoryOrDefaultInternal(ctx context.Context, cfg *settings.Settings) string {
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(cfg.ProjectsDirectory.Value))
	if err != nil {
		slog.WarnContext(ctx, "unable to determine projects directory; using default", "error", err)
		return "/app/data/projects"
	}
	return projectsDirectory
}

func (s *ProjectService) getMutableProjectInternal(ctx context.Context, projectID string) (*Project, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := ensureProjectMutableInternal(proj); err != nil {
		return nil, err
	}
	return proj, nil
}

func (s *ProjectService) logProjectEventInternal(ctx context.Context, eventType event.EventType, projectID, projectName string, user common.User, metadata database.JSON, action string) {
	if s.eventService == nil {
		return
	}
	if logErr := s.eventService.LogProjectEvent(ctx, eventType, projectID, projectName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.ErrorContext(ctx, action, "error", logErr)
	}
}

func (s *ProjectService) GetProjectRelativePath(ctx context.Context, projectPath string) string {
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return ""
	}

	return getProjectRelativePathInternal(projectsDir, projectPath)
}

func getProjectRelativePathInternal(projectsDir, projectPath string) string {
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

func (s *ProjectService) GetProjectFromDatabaseByID(ctx context.Context, id string) (*Project, error) {
	var projectModel Project
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&projectModel).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("request canceled or timed out")
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("project not found")
		}
		return nil, errors.WrapIf(err, "failed to get project")
	}
	return &projectModel, nil
}

func (s *ProjectService) GetProjectByComposeName(ctx context.Context, name string) (*Project, error) {
	if name == "" {
		return nil, errors.New("project name is empty")
	}
	normalized := projects.NormalizeProjectName(name)

	var proj Project
	err := s.db.WithContext(ctx).Where("name = ? OR name = ?", name, normalized).First(&proj).Error
	if err == nil {
		s.composeNames.put(normalized, proj.ID)
		return &proj, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.WrapIf(err, "failed to get project by name")
	}

	if cachedProject, found, cacheErr := s.lookupProjectByCachedComposeNameInternal(ctx, normalized); cacheErr != nil {
		return nil, cacheErr
	} else if found {
		return cachedProject, nil
	}

	if err := s.rebuildComposeNameCacheInternal(ctx); err != nil {
		return nil, errors.WrapIf(err, "failed to list projects by compose name")
	}

	if cachedProject, found, cacheErr := s.lookupProjectByCachedComposeNameInternal(ctx, normalized); cacheErr != nil {
		return nil, cacheErr
	} else if found {
		return cachedProject, nil
	}

	return nil, errors.Errorf("project not found: %s", name)
}

// EnsureProjectPathUnderRoot validates that the project's path is a safe subdirectory of the configured projects root.
// If not, it normalizes the path to `<projectsRoot>/<dirName or sanitized project name>`. When persist=true, it saves
// the updated project path to the database.
func (s *ProjectService) EnsureProjectPathUnderRoot(ctx context.Context, proj *Project, persist bool) error {
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return errors.WrapIf(err, "failed to get projects directory")
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
	dirName := mo.PointerToOption(proj.DirName).OrEmpty()
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
