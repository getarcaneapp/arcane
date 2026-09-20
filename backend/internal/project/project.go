package project

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	buildtypes "go.getarcane.app/builds/types"
	"gorm.io/gorm"
)

type buildServiceInternal interface {
	BuildImage(ctx context.Context, environmentID string, req buildtypes.BuildRequest, progressWriter io.Writer, serviceName string, user *common.User) (*buildtypes.BuildResult, error)
	BuildSettings() buildtypes.BuildSettings
}

type ProjectService struct {
	composeCoordinator          projecttypes.ComposeCoordinator
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
	parsedCompose projecttypes.ComposeCache[*composetypes.Project]
	// metaCache holds per-project icon/URL metadata, keyed by project ID. Deriving
	// it costs a full compose load (interpolation plus .env reads) and, for GitOps
	// projects, a gitops_syncs lookup — per project, on every list request.
	// Entries are validated by compose/include/env file mtimes rather than a TTL.
	metaCache projecttypes.ComposeCache[projects.ArcaneComposeMetadata]

	// filesChanged fires with the project ID after project files are saved
	// through Arcane, so Git backups can react without polling.
	filesChanged *actors.Signal[string]
}

// FilesChanged exposes the project-file-change signal for subscribers.
func (s *ProjectService) FilesChanged() *actors.Signal[string] {
	if s == nil {
		return nil
	}
	return s.filesChanged
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
		s.composeNames.putInternal(projects.NormalizeProjectName(project.Name), project.ID)
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
	pathMapper := s.projectPathMapperInternal(ctx)
	composeProject, err := projects.LoadComposeProject(
		ctx,
		filepath.Join(projectPath, composeFileName),
		projects.NormalizeProjectName(projectName),
		projectsDirectory,
		s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
		pathMapper,
		nil,
		nil,
		true, nil, nil, nil,
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
	s.composeNames.putInternal(projects.NormalizeProjectName(project.Name), project.ID)
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

// projectMetadataEnvInternal carries the request-scoped inputs compose
// resolution needs beyond the project itself. Resolving them costs a settings
// clone, a stat syscall, and a gitops_syncs query each, so list paths resolve
// once and reuse across metadata and update enrichment for every project.
const maxConcurrentComposeReads = 8

type projectMetadataEnvInternal struct {
	projectsDirectory string
	autoInjectEnv     bool
	// settings is the snapshot shared by compose loads; nil resolves lazily.
	settings *settings.Settings
	// gitOpsComposePaths maps preloaded GitOps sync IDs to their configured
	// compose paths. Sync IDs that were preloaded but have no row map to "",
	// which resolves the same way as a missing row; sync IDs absent from the
	// map are queried per project.
	gitOpsComposePaths map[string]string

	// composeFiles memoizes successfully resolved compose files by project ID.
	// Failures are not stored so a later phase in the same request retries
	// resolution instead of replaying an error that may have been transient.
	composeFilesMu sync.Mutex
	composeFiles   map[string]string
}

// newProjectMetadataEnvInternal resolves the shared inputs once and preloads
// the GitOps compose paths of every listed project in a single query.
func (s *ProjectService) newProjectMetadataEnvInternal(ctx context.Context, projectsList []Project) *projectMetadataEnvInternal {
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for compose selection", "error", err)
	}
	env := &projectMetadataEnvInternal{
		projectsDirectory: projectsDirectory,
		autoInjectEnv:     s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
		settings:          s.settingsService.GetSettingsOrDefaults(ctx),
		composeFiles:      make(map[string]string, len(projectsList)),
	}
	s.preloadGitOpsComposePathsInternal(ctx, env, projectsList)
	return env
}

// preloadGitOpsComposePathsInternal fetches the GitOps compose paths of the
// given projects in one query and merges them into env, so list paths can
// widen the preloaded set to the rows they end up enriching.
func (s *ProjectService) preloadGitOpsComposePathsInternal(ctx context.Context, env *projectMetadataEnvInternal, projectsList []Project) {
	syncIDs := make([]string, 0, len(projectsList))
	for _, proj := range projectsList {
		if id := gitOpsSyncIDInternal(&proj); id != "" {
			syncIDs = append(syncIDs, id)
		}
	}
	if len(syncIDs) == 0 {
		return
	}
	var syncRecords []GitOpsSync
	if err := s.db.WithContext(ctx).Select("id", "compose_path").Where("id IN ?", syncIDs).Find(&syncRecords).Error; err != nil {
		// Leave these IDs unloaded so each project falls back to its own lookup
		// and surfaces the failure the same way it did before batching.
		slog.WarnContext(ctx, "failed to batch resolve GitOps compose paths", "error", err)
		return
	}
	if env.gitOpsComposePaths == nil {
		env.gitOpsComposePaths = make(map[string]string, len(syncIDs))
	}
	for _, id := range syncIDs {
		env.gitOpsComposePaths[id] = ""
	}
	for _, record := range syncRecords {
		env.gitOpsComposePaths[record.ID] = record.ComposePath
	}
}

func gitOpsSyncIDInternal(proj *Project) string {
	if proj == nil || proj.GitOpsManagedBy == nil {
		return ""
	}
	return strings.TrimSpace(*proj.GitOpsManagedBy)
}

// composeFileInternal memoizes a project's resolved compose file for the
// request so metadata and update enrichment share one resolution. Only
// successful resolutions are cached: a failure (for example a transient
// filesystem or GitOps lookup error) is returned as-is and the next caller
// resolves again, matching the per-phase retry of the unmemoized flow.
//
// The mutex is deliberately not held across resolve: it does filesystem and
// database work, and holding the lock would serialize the concurrent
// per-project workers. Callers run one worker per project within a phase and
// phases run back to back, so the same project is not resolved concurrently;
// if it ever were, both workers would compute the same path and the duplicate
// work is harmless.
func (env *projectMetadataEnvInternal) composeFileInternal(projectID string, resolve func() (string, error)) (string, error) {
	if env == nil || projectID == "" {
		return resolve()
	}
	env.composeFilesMu.Lock()
	cached, ok := env.composeFiles[projectID]
	env.composeFilesMu.Unlock()
	if ok {
		return cached, nil
	}
	path, err := resolve()
	if err != nil {
		return "", err
	}
	env.composeFilesMu.Lock()
	if env.composeFiles == nil {
		env.composeFiles = make(map[string]string)
	}
	env.composeFiles[projectID] = path
	env.composeFilesMu.Unlock()
	return path, nil
}

type registryCredentialsProviderInternal func(context.Context) ([]containerregistry.Credential, error)

func NewProjectService(db *database.DB, settingsService *settings.SettingsService, eventService *event.EventService, imageService *image.ImageService, dockerService *docker.DockerClientService, buildService buildServiceInternal, lifecycleService *LifecycleService, containerRegistryService *registry.ContainerRegistryService, cfg *config.Config) *ProjectService {
	return &ProjectService{
		composeCoordinator:       projects.NewCoordinator(projecttypes.ComposeCommands{Stop: composeStopProjectServicesInternal, Up: composeUpProjectServicesInternal}),
		db:                       db,
		settingsService:          settingsService,
		eventService:             eventService,
		imageService:             imageService,
		dockerService:            dockerService,
		buildService:             buildService,
		lifecycleService:         lifecycleService,
		containerRegistryService: containerRegistryService,
		config:                   cfg,
		parsedCompose:            projects.NewParsedComposeCache(),
		metaCache:                projects.NewComposeCache[projects.ArcaneComposeMetadata](1024, nil),
		filesChanged:             actors.NewSignal[string](),
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
		s.composeNames.putInternal(normalized, proj.ID)
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

func (s *ProjectService) projectPathMapperInternal(ctx context.Context) *projects.PathMapper {
	var dockerClient *client.Client
	if s.dockerService != nil {
		dockerClient, _ = s.dockerService.GetClient(ctx)
	}
	return projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"),
		"/app/data/projects",
		dockerClient,
	)
}

// composeNameCacheInternal maps normalized compose project names to project
// IDs so a name lookup skips the projects table scan.
type composeNameCacheInternal struct {
	mu     sync.RWMutex
	byName map[string]string
}

func (c *composeNameCacheInternal) projectIDInternal(normalizedName string) mo.Option[string] {
	if normalizedName == "" {
		return mo.None[string]()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.byName == nil {
		return mo.None[string]()
	}

	projectID, ok := c.byName[normalizedName]
	return mo.TupleToOption(projectID, ok)
}

func (c *composeNameCacheInternal) putInternal(normalizedName, projectID string) {
	if normalizedName == "" || projectID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.byName == nil {
		c.byName = make(map[string]string)
	}
	c.byName[normalizedName] = projectID
}

func (c *composeNameCacheInternal) invalidateInternal(normalizedName string) {
	if normalizedName == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.byName, normalizedName)
}

func (c *composeNameCacheInternal) replaceInternal(byName map[string]string) {
	c.mu.Lock()
	c.byName = byName
	c.mu.Unlock()
}

func (s *ProjectService) invalidateProjectCachesInternal(projectID string) {
	if s.parsedCompose != nil {
		s.parsedCompose.Invalidate(projectID)
	}
	if s.metaCache != nil {
		s.metaCache.Invalidate(projectID)
	}
}

func (s *ProjectService) lookupProjectByCachedComposeNameInternal(ctx context.Context, normalizedName string) (*Project, bool, error) {
	projectID, ok := s.composeNames.projectIDInternal(normalizedName).Get()
	if !ok {
		return nil, false, nil
	}

	var projectModel Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&projectModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.composeNames.invalidateInternal(normalizedName)
			return nil, false, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, errors.WrapIf(err, "request canceled or timed out")
		}
		return nil, false, errors.WrapIf(err, "failed to get project by cached compose name")
	}
	if projects.NormalizeProjectName(projectModel.Name) != normalizedName {
		s.composeNames.invalidateInternal(normalizedName)
		return nil, false, nil
	}

	return &projectModel, true, nil
}

func (s *ProjectService) rebuildComposeNameCacheInternal(ctx context.Context) error {
	var projectModels []Project
	if err := s.db.WithContext(ctx).Select("id", "name").Find(&projectModels).Error; err != nil {
		return err
	}

	byName := make(map[string]string, len(projectModels))
	for i := range projectModels {
		normalizedName := projects.NormalizeProjectName(projectModels[i].Name)
		if normalizedName == "" {
			continue
		}
		if _, exists := byName[normalizedName]; !exists {
			byName[normalizedName] = projectModels[i].ID
		}
	}

	s.composeNames.replaceInternal(byName)

	return nil
}

// ResolveProjectComposeFile returns the base compose file for a project. The
// precedence mirrors `docker compose`: COMPOSE_FILE in the merged environment
// (.env.global first, the project's .env on top) wins, then a GitOps sync's
// configured compose path, then standard detection.
func (s *ProjectService) ResolveProjectComposeFile(ctx context.Context, proj *Project) (string, error) {
	return s.resolveProjectComposeFileInternal(ctx, proj, nil)
}

// resolveProjectComposeFileInternal is ResolveProjectComposeFile with the
// request-scoped inputs supplied by env; a nil env resolves them per call.
func (s *ProjectService) resolveProjectComposeFileInternal(ctx context.Context, proj *Project, env *projectMetadataEnvInternal) (string, error) {
	if proj == nil {
		return "", errors.New("project is nil")
	}
	return env.composeFileInternal(proj.ID, func() (string, error) {
		return s.resolveProjectComposeFileUncachedInternal(ctx, proj, env)
	})
}

func (s *ProjectService) resolveProjectComposeFileUncachedInternal(ctx context.Context, proj *Project, env *projectMetadataEnvInternal) (string, error) {
	projectsDirectory := ""
	switch {
	case env != nil:
		projectsDirectory = env.projectsDirectory
	case s.settingsService != nil:
		var dirErr error
		projectsDirectory, dirErr = s.GetProjectsDirectory(ctx)
		if dirErr != nil {
			// The .env.global layer is skipped for an empty projects directory;
			// keep resolution working but surface the misconfiguration.
			slog.WarnContext(ctx, "failed to resolve projects directory for compose selection", "projectID", proj.ID, "error", dirErr)
		}
	}
	if files, selErr := projects.ComposeFileEnvSelection(ctx, projectsDirectory, proj.Path); selErr != nil {
		return "", selErr
	} else if len(files) > 0 {
		return files[0], nil
	}

	if syncID := gitOpsSyncIDInternal(proj); syncID != "" {
		composePath, found, err := s.gitOpsComposePathInternal(ctx, syncID, env)
		if err != nil {
			return "", errors.WrapIff(err, "failed to resolve GitOps compose path for project %s", proj.ID)
		}
		if found {
			composeFileName := strings.TrimSpace(filepath.Base(composePath))
			if composeFileName != "" && composeFileName != "." {
				candidate := filepath.Join(proj.Path, composeFileName)
				// os.Stat rather than acfs: proj.Path may be an imported project
				// outside the projects directory, and the compose file may be a
				// symlink resolving outside it.
				if info, statErr := os.Stat(candidate); statErr == nil {
					if !info.IsDir() {
						return candidate, nil
					}
				} else if !os.IsNotExist(statErr) {
					return "", errors.WrapIff(statErr, "failed to inspect GitOps compose file %s", candidate)
				}
			}
		}
	}

	composeFile, err := projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
	if err != nil {
		if errors.Is(err, common.ErrProjectEnvUnreadable) {
			return "", err
		}
		return "", common.Classify(common.ErrProjectComposeFileNotFound, errors.WrapIf(err, "Project compose file not found"))
	}

	return composeFile, nil
}

// gitOpsComposePathInternal returns the configured compose path of a GitOps
// sync, served from the preloaded request map when one is available.
func (s *ProjectService) gitOpsComposePathInternal(ctx context.Context, syncID string, env *projectMetadataEnvInternal) (string, bool, error) {
	if env != nil {
		if composePath, preloaded := env.gitOpsComposePaths[syncID]; preloaded {
			return composePath, true, nil
		}
	}
	var syncRecord GitOpsSync
	err := s.db.WithContext(ctx).Select("compose_path").Where("id = ?", syncID).First(&syncRecord).Error
	switch {
	case err == nil:
		return syncRecord.ComposePath, true, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", false, nil
	default:
		return "", false, err
	}
}

// loadComposeProjectForProjectInternal loads the executable compose model for
// proj. prepare is optional and runs before host path translation; deployment
// paths use it to create missing bind directories, read paths pass nil.
func (s *ProjectService) loadComposeProjectForProjectInternal(ctx context.Context, proj *Project, prepare projects.PrepareProjectFunc, services ...string) (*composetypes.Project, string, error) {
	composeFileFullPath, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		return nil, "", err
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory := getProjectsDirectoryOrDefaultInternal(ctx, cfg)

	pathMapper := s.projectPathMapperInternal(ctx)

	composeProject, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, projects.NormalizeProjectName(proj.Name), projectsDirectory, utils.BoolOrDefault(cfg.AutoInjectEnv.Value, false), pathMapper, nil, nil, false, nil, services, prepare)
	if loadErr != nil {
		return nil, "", loadErr
	}

	return composeProject, composeFileFullPath, nil
}

func (s *ProjectService) getCachedComposeProjectInternal(ctx context.Context, proj *Project, env *projectMetadataEnvInternal) (*composetypes.Project, error) {
	if proj == nil {
		return nil, errors.New("project is nil")
	}
	var cfg *settings.Settings
	if env != nil {
		cfg = env.settings
	}
	if cfg == nil {
		cfg = s.settingsService.GetSettingsOrDefaults(ctx)
	}
	composePath, err := s.resolveProjectComposeFileInternal(ctx, proj, env)
	if err != nil {
		return nil, err
	}
	return projects.LoadCachedComposeProject(ctx, s.parsedCompose, proj.ID, proj.Path, composePath, projects.NormalizeProjectName(proj.Name), getProjectsDirectoryOrDefaultInternal(ctx, cfg), utils.BoolOrDefault(cfg.AutoInjectEnv.Value, false), s.projectPathMapperInternal(ctx))
}

func (s *ProjectService) refreshComposeProjectNameInternal(ctx context.Context, proj *Project) {
	if proj == nil {
		return
	}

	dirName := proj.Name
	if proj.DirName != nil && *proj.DirName != "" {
		dirName = *proj.DirName
	}

	meta, err := s.loadComposeMetadataForSyncInternal(ctx, proj.Path, dirName)
	if err != nil {
		if errors.Is(err, common.ErrProjectEnvUnreadable) {
			slog.DebugContext(ctx, "skipped compose project name refresh; project env is unreadable", "projectID", proj.ID, "path", proj.Path, "error", err)
			return
		}
		slog.WarnContext(ctx, "failed to refresh compose project name", "projectID", proj.ID, "path", proj.Path, "error", err)
		return
	}

	updates := map[string]any{}
	shouldUpdateName := meta.explicitProjectName || projects.NormalizeProjectName(proj.Name) != proj.Name
	if shouldUpdateName && meta.resolvedProjectName != "" && proj.Name != meta.resolvedProjectName {
		updates["name"] = meta.resolvedProjectName
	}
	if mo.PointerToOption(proj.ComposeProjectName) != mo.PointerToOption(meta.composeProjectName) {
		updates["compose_project_name"] = meta.composeProjectName
	}
	if len(updates) == 0 {
		return
	}

	updates["updated_at"] = time.Now()
	if err := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", proj.ID).
		Updates(updates).Error; err != nil {
		slog.WarnContext(ctx, "failed to persist refreshed compose project name", "projectID", proj.ID, "error", err)
		return
	}

	if name, ok := updates["name"].(string); ok {
		proj.Name = name
	}
	if _, ok := updates["compose_project_name"]; ok {
		proj.ComposeProjectName = meta.composeProjectName
	}
}
