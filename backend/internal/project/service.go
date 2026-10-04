package project

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	lifecycletype "github.com/getarcaneapp/arcane/types/v2/lifecycle"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/moby/moby/api/types/container"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	updatertypes "go.getarcane.app/updater/types"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/deployment"
	projectdetails "github.com/getarcaneapp/arcane/backend/v2/internal/project/children/details"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/lifecycle"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/listing"
	projectsync "github.com/getarcaneapp/arcane/backend/v2/internal/project/children/sync"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/tags"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/update"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/workspace"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

type ProjectService struct {
	composeCoordinator          projecttypes.ComposeCoordinator
	db                          *database.DB
	settingsService             *settings.SettingsService
	eventService                *event.EventService
	imageService                *image.ImageService
	dockerService               *docker.DockerClientService
	lifecycleService            *LifecycleService
	workspace                   *workspace.Service
	details                     *projectdetails.Service
	listing                     *listing.Service
	deployment                  *deployment.Service
	updates                     *update.Service
	containerRegistryService    *registry.ContainerRegistryService
	config                      *config.Config
	RegistryCredentialsProvider func(context.Context) ([]containerregistry.Credential, error)

	// syncMu serializes SyncProjectsFromFileSystem: its discovery walk and its
	// cleanup pass must not interleave with another run's.
	syncMu sync.Mutex

	composeNames  composeNameCacheInternal
	parsedCompose projecttypes.ComposeCache[*types.Project]
	// metaCache holds per-project icon/URL metadata, keyed by project ID. Deriving
	// it costs a full compose load (interpolation plus .env reads) and, for GitOps
	// projects, a gitops_syncs lookup — per project, on every list request.
	// Entries are validated by compose/include/env file mtimes rather than a TTL.
	metaCache projecttypes.ComposeCache[projects.ArcaneComposeMetadata]

	// FilesChanged fires with the project ID after project files are saved
	// through Arcane, so Git backups can react without polling.
	FilesChanged *concurrency.Signal[string]
}

// EnsureGitOpsProjectLinked persists the bidirectional GitOps/project binding
// and refreshes the compose-name cache as one domain operation.
func (s *ProjectService) EnsureGitOpsProjectLinked(ctx context.Context, gitOpsSync *GitOpsSync, project *Project) error {
	if gitOpsSync == nil || project == nil {
		return nil
	}
	if project.GitOpsManagedBy != nil && *project.GitOpsManagedBy != "" && *project.GitOpsManagedBy != gitOpsSync.ID {
		return fmt.Errorf("project %s is already managed by a different GitOps sync", project.ID)
	}

	cacheBinding := func() {
		s.composeNames.putInternal(projects.NormalizeProjectName(project.Name), project.ID)
	}
	if gitOpsSync.ProjectID != nil && *gitOpsSync.ProjectID == project.ID && project.GitOpsManagedBy != nil && *project.GitOpsManagedBy == gitOpsSync.ID {
		cacheBinding()
		return nil
	}

	updatesSync := map[string]any{}
	updatesProject := map[string]any{}
	if gitOpsSync.ProjectID == nil || *gitOpsSync.ProjectID != project.ID {
		updatesSync["project_id"] = project.ID
	}
	if project.GitOpsManagedBy == nil || *project.GitOpsManagedBy != gitOpsSync.ID {
		updatesProject["gitops_managed_by"] = gitOpsSync.ID
	}
	if len(updatesSync) == 0 && len(updatesProject) == 0 {
		cacheBinding()
		return nil
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(updatesSync) > 0 {
			if err := tx.Model(&GitOpsSync{}).Where("id = ?", gitOpsSync.ID).Updates(updatesSync).Error; err != nil {
				return fmt.Errorf("failed to relink GitOps sync %s: %w", gitOpsSync.ID, err)
			}
		}
		if len(updatesProject) > 0 {
			if err := tx.Model(&Project{}).Where("id = ?", project.ID).Updates(updatesProject).Error; err != nil {
				return fmt.Errorf("failed to relink project %s to GitOps sync %s: %w", project.ID, gitOpsSync.ID, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	gitOpsSync.ProjectID = &project.ID
	project.GitOpsManagedBy = &gitOpsSync.ID
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
func (s *ProjectService) CreateGitOpsManagedProject(ctx context.Context, gitOpsSync *GitOpsSync, project *Project, actor usertypes.Actor, logEventOptions ...bool) error {
	if gitOpsSync == nil || project == nil {
		return errors.New("GitOps sync and project are required")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return fmt.Errorf("failed to create project: %w", err)
		}
		if err := tx.Model(&GitOpsSync{}).Where("id = ?", gitOpsSync.ID).Update("project_id", project.ID).Error; err != nil {
			return fmt.Errorf("failed to update sync with project ID: %w", err)
		}
		if err := tx.Model(&Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", gitOpsSync.ID).Error; err != nil {
			return fmt.Errorf("failed to mark project as GitOps-managed: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	gitOpsSync.ProjectID = &project.ID
	project.GitOpsManagedBy = &gitOpsSync.ID
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

func NewProjectService(
	db *database.DB,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	imageService *image.ImageService,
	dockerService *docker.DockerClientService,
	buildService buildServiceInternal,
	lifecycleService *LifecycleService,
	containerRegistryService *registry.ContainerRegistryService,
	cfg *config.Config,
	kvService *kv.KVService,
	registryCredentialsProvider func(
		context.Context,
	) (
		[]containerregistry.Credential,
		error,
	),
) *ProjectService {
	s := &ProjectService{
		RegistryCredentialsProvider: registryCredentialsProvider,
		composeCoordinator:          projects.NewCoordinator(projecttypes.ComposeCommands{Stop: composeStopProjectServicesInternal, Up: composeUpProjectServicesInternal, Create: projects.ComposeCreate}),
		db:                          db,
		settingsService:             settingsService,
		eventService:                eventService,
		imageService:                imageService,
		dockerService:               dockerService,
		lifecycleService:            lifecycleService,
		containerRegistryService:    containerRegistryService,
		config:                      cfg,
		parsedCompose:               projects.NewParsedComposeCache(),
		metaCache:                   projects.NewComposeCache[projects.ArcaneComposeMetadata](1024, nil),
		FilesChanged:                concurrency.NewSignal[string](),
	}
	s.initChildrenInternal(kvService, buildService)
	return s
}

// initChildrenInternal builds the feature services from the parent's
// dependencies and callbacks.
func (s *ProjectService) initChildrenInternal(kvService *kv.KVService, buildService buildServiceInternal) {
	s.workspace = workspace.New(s.config)
	s.details = projectdetails.New(s.db, s.dockerService, s.imageService, s.settingsService)
	s.listing = listing.New(
		s.details.ComposeContainers,
		s.imageService,
		func(imageRefs []string, services []projecttypes.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) *projecttypes.UpdateInfo {
			return BuildUpdateInfoSummary(imageRefs, projectdetails.MergeProjectContainerUpdateInfo(nil, services, scoped))
		},
	)
	s.deployment = deployment.New(s.settingsService, s.imageService, s.dockerService, buildService)
	s.updates = update.New(kvService, s.dockerService, s.containerRegistryService, s.ResolveRegistryCredentials, s.renameProjectStateInternal, s.restoreRenamedProjectInternal)
}

// renameProjectStateInternal reports the stored name and path rename recovery
// compares against its journal.
func (s *ProjectService) renameProjectStateInternal(ctx context.Context, projectID string) (string, string, bool, error) {
	proj, found, err := s.FindProjectByID(ctx, projectID)
	if err != nil || !found {
		return "", "", found, err
	}
	return proj.Name, proj.Path, true, nil
}

// restoreRenamedProjectInternal rolls a project row back to its pre-rename identity.
func (s *ProjectService) restoreRenamedProjectInternal(ctx context.Context, journal *projecttypes.RenameJournal) error {
	return s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", journal.ProjectID).Updates(map[string]any{
		"name": journal.OldName, "path": journal.OldPath, "dir_name": journal.OldDirName,
	}).Error
}

// RecoverProjectRenameJournals replays renames interrupted by a restart.
func (s *ProjectService) RecoverProjectRenameJournals(ctx context.Context) error {
	return s.updates.RecoverAll(ctx)
}

func (s *ProjectService) ResolveRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	if s == nil || s.RegistryCredentialsProvider == nil {
		return nil, nil
	}

	credentials, err := s.RegistryCredentialsProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("get enabled registry credentials: %w", err)
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

func (s *ProjectService) getMutableProjectInternal(ctx context.Context, projectID string) (*Project, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if proj != nil && proj.IsArchived {
		return nil, common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	return proj, nil
}

func (s *ProjectService) logProjectEventInternal(ctx context.Context, eventType event.EventType, projectID, projectName string, user usertypes.Actor, metadata database.JSON, action string) {
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

	return listing.RelativePath(projectsDir, projectPath)
}

func (s *ProjectService) GetProjectFromDatabaseByID(ctx context.Context, id string) (*Project, error) {
	projectModel, found, err := s.FindProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("request canceled or timed out")
		}
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	if !found {
		return nil, errors.New("project not found")
	}
	return projectModel, nil
}

// FindProjectByID reports a missing project as found=false instead of an error.
func (s *ProjectService) FindProjectByID(ctx context.Context, projectID string) (*Project, bool, error) {
	var project Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &project, true, nil
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
		return nil, fmt.Errorf("failed to get project by name: %w", err)
	}

	if cachedProject, found, cacheErr := s.lookupProjectByCachedComposeNameInternal(ctx, normalized); cacheErr != nil {
		return nil, cacheErr
	} else if found {
		return cachedProject, nil
	}

	if rebuildComposeNameCacheErr := s.rebuildComposeNameCacheInternal(ctx); rebuildComposeNameCacheErr != nil {
		return nil, fmt.Errorf("failed to list projects by compose name: %w", rebuildComposeNameCacheErr)
	}

	if cachedProject, found, cacheErr := s.lookupProjectByCachedComposeNameInternal(ctx, normalized); cacheErr != nil {
		return nil, cacheErr
	} else if found {
		return cachedProject, nil
	}

	return nil, fmt.Errorf("project not found: %s", name)
}

// EnsureProjectPathUnderRoot validates that the project's path is a safe subdirectory of the configured projects root.
// If not, it normalizes the path to `<projectsRoot>/<dirName or sanitized project name>`. When persist=true, it saves
// the updated project path to the database.
func (s *ProjectService) EnsureProjectPathUnderRoot(ctx context.Context, proj *Project, persist bool) error {
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
			return nil, false, fmt.Errorf("request canceled or timed out: %w", err)
		}
		return nil, false, fmt.Errorf("failed to get project by cached compose name: %w", err)
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
			return "", fmt.Errorf("failed to resolve GitOps compose path for project %s: %w", proj.ID, err)
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
					return "", fmt.Errorf("failed to inspect GitOps compose file %s: %w", candidate, statErr)
				}
			}
		}
	}

	composeFile, err := projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
	if err != nil {
		if errors.Is(err, common.ErrProjectEnvUnreadable) {
			return "", err
		}
		return "", common.Classify(common.ErrProjectComposeFileNotFound, fmt.Errorf("Project compose file not found: %w", err))
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
func (s *ProjectService) loadComposeProjectForProjectInternal(ctx context.Context, proj *Project, prepare projects.PrepareProjectFunc, services ...string) (*types.Project, string, error) {
	composeFileFullPath, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		return nil, "", err
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory := getProjectsDirectoryOrDefaultInternal(ctx, cfg)

	pathMapper := s.projectPathMapperInternal(ctx)

	composeProject, loadErr := projects.LoadComposeProject(
		ctx,
		composeFileFullPath,
		projects.NormalizeProjectName(
			proj.Name,
		),
		projectsDirectory,
		kit.ParseOrDefault(
			cfg.AutoInjectEnv.Value,
			false,
			strconv.ParseBool,
		),
		pathMapper,
		nil,
		nil,
		false,
		nil,
		services,
		prepare,
	)
	if loadErr != nil {
		return nil, "", loadErr
	}

	return composeProject, composeFileFullPath, nil
}

func (s *ProjectService) getCachedComposeProjectInternal(ctx context.Context, proj *Project, env *projectMetadataEnvInternal) (*types.Project, error) {
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
	return projects.LoadCachedComposeProject(
		ctx,
		s.parsedCompose,
		proj.ID,
		proj.Path,
		composePath,
		projects.NormalizeProjectName(
			proj.Name,
		),
		getProjectsDirectoryOrDefaultInternal(
			ctx,
			cfg,
		),
		kit.ParseOrDefault(
			cfg.AutoInjectEnv.Value,
			false,
			strconv.ParseBool,
		),
		s.projectPathMapperInternal(
			ctx,
		),
	)
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
	shouldUpdateName := meta.ExplicitProjectName || projects.NormalizeProjectName(proj.Name) != proj.Name
	if shouldUpdateName && meta.ResolvedProjectName != "" && proj.Name != meta.ResolvedProjectName {
		updates["name"] = meta.ResolvedProjectName
	}
	if mo.PointerToOption(proj.ComposeProjectName) != mo.PointerToOption(meta.ComposeProjectName) {
		updates["compose_project_name"] = meta.ComposeProjectName
	}
	if len(updates) == 0 {
		return
	}

	updates["updated_at"] = time.Now()
	if persistComposeNameErr := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", proj.ID).
		Updates(updates).Error; persistComposeNameErr != nil {
		slog.WarnContext(ctx, "failed to persist refreshed compose project name", "projectID", proj.ID, "error", persistComposeNameErr)
		return
	}

	if name, ok := updates["name"].(string); ok {
		proj.Name = name
	}
	if _, ok := updates["compose_project_name"]; ok {
		proj.ComposeProjectName = meta.ComposeProjectName
	}
}

// LifecycleService runs pre-deploy lifecycle hooks declared on a project's
// GitOps sync. A hook is a script in the synced repo executed in a throwaway
// container immediately before the project is deployed, with optional capture
// of stdout as environment variables merged into the compose env.
//
// Trust model: the script is repo-trusted code, equivalent to compose.yaml in
// the same repo. Anyone who can push to that repo can change what the script
// does on the next deploy. The trust event is configuring a script path on
// the GitOps sync, not each individual deploy.
type LifecycleService struct {
	db              *database.DB
	settingsService *settings.SettingsService
	eventService    *event.EventService
	hooks           *lifecycle.Service
}

// NewLifecycleService constructs a LifecycleService wired against shared
// infrastructure. The Docker client is obtained lazily on each hook run via
// dockerService.GetClient so reconnects are transparent. Runner image pulls go
// through imageService so configured registry credentials apply.
func NewLifecycleService(
	db *database.DB,
	settingsService *settings.SettingsService,
	eventService *event.EventService,
	dockerService *docker.DockerClientService,
	imageService *image.ImageService,
) *LifecycleService {
	return &LifecycleService{
		db:              db,
		settingsService: settingsService,
		eventService:    eventService,
		hooks:           lifecycle.New(settingsService, dockerService, imageService),
	}
}

// RunPreDeploy executes the pre-deploy lifecycle hook for a project, if one
// is configured on its GitOps sync.
//
// Callers should invoke this unconditionally before deploying — when no hook
// is configured, when lifecycle hooks are disabled globally, or when the
// project is not GitOps-managed, this is a no-op and returns nil.
//
// A non-zero exit code, a script timeout, or any infrastructure failure
// returns an error that aborts the deploy. The last-run state on the
// GitOpsSync row is updated on every invocation that reaches the run step,
// regardless of outcome.
func (s *LifecycleService) RunPreDeploy(ctx context.Context, project *Project, actor usertypes.Actor) error {
	if project == nil || project.GitOpsManagedBy == nil || *project.GitOpsManagedBy == "" {
		return nil
	}
	if !s.settingsService.GetBoolSetting(ctx, "lifecycleEnabled", false) {
		return nil
	}

	syncRecord, err := loadGitOpsSyncForProjectInternal(ctx, s.db, project.ID)
	if err != nil {
		return fmt.Errorf("failed to load gitops sync for lifecycle hook: %w", err)
	}
	if syncRecord == nil || syncRecord.PreDeployScriptPath == nil || strings.TrimSpace(*syncRecord.PreDeployScriptPath) == "" {
		return nil
	}

	return s.executePreDeployInternal(ctx, project, syncRecord, actor)
}

func (s *LifecycleService) executePreDeployInternal(ctx context.Context, project *Project, syncRecord *GitOpsSync, actor usertypes.Actor) error {
	runnerImage := s.hooks.RunnerImage(ctx, syncRecord.PreDeployRunnerImage)
	if runnerImage == "" {
		return fmt.Errorf("pre-deploy script %q is configured but no runner image is set on the GitOps sync or lifecycleDefaultRunnerImage setting", *syncRecord.PreDeployScriptPath)
	}

	scriptPath := strings.TrimSpace(*syncRecord.PreDeployScriptPath)
	if err := lifecycle.ValidateScriptPath(ctx, project.Path, scriptPath); err != nil {
		return fmt.Errorf("invalid pre-deploy script path: %w", err)
	}

	hookEnv, err := ParseEnvText(syncRecord.PreDeployEnv)
	if err != nil {
		return fmt.Errorf("invalid lifecycle env config: %w", err)
	}
	extraMounts, err := ParseExtraMountsText(syncRecord.PreDeployExtraMounts)
	if err != nil {
		return fmt.Errorf("invalid lifecycle extra mounts config: %w", err)
	}

	timeout := s.hooks.Timeout(ctx, syncRecord.PreDeployTimeoutSec)

	slog.InfoContext(ctx, "running pre-deploy lifecycle hook",
		"projectID", project.ID,
		"syncID", syncRecord.ID,
		"scriptPath", scriptPath,
		"runnerImage", runnerImage,
		"timeoutSec", int(timeout/time.Second),
	)

	start := time.Now()
	stdoutContent, stderrContent, exitCode, runErr := s.hooks.RunScript(
		ctx,
		runnerImage,
		project.Path,
		scriptPath,
		hookEnv,
		extraMounts,
		syncRecord.PreDeployNetworkMode,
		timeout,
		actor,
	)
	durationMs := time.Since(start).Milliseconds()

	status := lifecycle.LifecycleStatusForResult(exitCode, runErr)
	// stdoutBuf/stderrBuf already enforce lifecycleMaxOutputBytes each with a
	// proper "...<truncated>" marker, so the combined string is already
	// bounded and we don't slice again here — a second byte-boundary cut
	// could land mid-UTF-8 codepoint and produce garbled output.
	persistedOutput := lifecycle.CombineLifecycleOutput(stdoutContent, stderrContent)
	s.persistLastRunInternal(ctx, syncRecord.ID, status, persistedOutput, start)
	s.emitLifecycleEventInternal(ctx, project, syncRecord, runnerImage, status, exitCode, durationMs, runErr, actor)

	if runErr != nil {
		return runErr
	}
	if exitCode != 0 {
		return fmt.Errorf("pre-deploy script exited with status %d", exitCode)
	}
	return nil
}

func (s *LifecycleService) persistLastRunInternal(ctx context.Context, syncID, status, output string, runAt time.Time) {
	persistCtx := context.WithoutCancel(ctx)
	err := s.db.WithContext(persistCtx).
		Model(&GitOpsSync{}).
		Where("id = ?", syncID).
		Updates(map[string]any{
			"pre_deploy_last_run_at":     runAt,
			"pre_deploy_last_run_status": status,
			"pre_deploy_last_run_output": output,
		}).Error
	if err != nil {
		slog.WarnContext(ctx, "failed to persist lifecycle last-run state", "syncID", syncID, "error", err)
	}
}

func (s *LifecycleService) emitLifecycleEventInternal(
	ctx context.Context,
	project *Project,
	syncRecord *GitOpsSync,
	runnerImage string,
	status string,
	exitCode int64,
	durationMs int64,
	runErr error,
	actor usertypes.Actor,
) {
	severity := event.EventSeveritySuccess
	title := "Pre-deploy lifecycle hook succeeded: " + project.Name
	description := fmt.Sprintf("Script %s exited with code %d in %dms", mo.PointerToOption(syncRecord.PreDeployScriptPath).OrEmpty(), exitCode, durationMs)
	if status != lifecycle.LifecycleStatusSuccess {
		severity = event.EventSeverityWarning
		title = fmt.Sprintf("Pre-deploy lifecycle hook %s: %s", status, project.Name)
		if runErr != nil {
			description = runErr.Error()
		}
	}

	metadata := database.JSON{
		"scriptPath":   mo.PointerToOption(syncRecord.PreDeployScriptPath).OrEmpty(),
		"runnerImage":  runnerImage,
		"exitCode":     exitCode,
		"durationMs":   durationMs,
		"gitopsSyncId": syncRecord.ID,
		"status":       status,
	}

	_, err := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeLifecycleExecute,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("project"),
		ResourceID:    new(project.ID),
		ResourceName:  new(project.Name),
		EnvironmentID: new(syncRecord.EnvironmentID),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		Metadata:      metadata,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to emit lifecycle.execute event", "syncID", syncRecord.ID, "error", err)
	}
}

// ParseEnvText reads admin-configured env config as the
// same KEY=VALUE text format used by .env files: one entry per line, blank
// and "#"-prefixed lines ignored, keys must match POSIX identifier syntax.
// Reuses the strict parser also used for stdout-capture.
func ParseEnvText(raw *string) (map[string]string, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return map[string]string{}, nil
	}
	env, err := lifecycle.ParseKeyValueEnv(*raw)
	if err != nil {
		return nil, fmt.Errorf("invalid env entry: %w", err)
	}
	return env, nil
}

// ParseExtraMountsText reads admin-configured bind mounts
// in docker-CLI "src:tgt[:ro|:rw]" form, one per line. Blank and
// "#"-prefixed lines are ignored. Both source and target must be absolute
// paths; mode defaults to read-write.
func ParseExtraMountsText(raw *string) ([]lifecycletype.ExtraMount, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}

	var mounts []lifecycletype.ExtraMount
	scanner := bufio.NewScanner(strings.NewReader(*raw))
	scanner.Buffer(make([]byte, 0, 4*1024), 64*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("line %d: expected src:tgt[:ro|:rw], got %q", lineNum, line)
		}

		mount := lifecycletype.ExtraMount{Source: parts[0], Target: parts[1]}
		if len(parts) == 3 {
			switch parts[2] {
			case "ro":
				mount.Readonly = true
			case "rw":
				mount.Readonly = false
			default:
				return nil, fmt.Errorf("line %d: invalid mode %q (expected \"ro\" or \"rw\")", lineNum, parts[2])
			}
		}

		// Mount sources/targets are interpreted by the Docker daemon as POSIX
		// host/container paths, so use path.IsAbs to avoid host-OS quirks
		// (filepath.IsAbs("/x") returns false on Windows).
		if !path.IsAbs(filepath.ToSlash(mount.Source)) {
			return nil, fmt.Errorf("line %d: source %q must be an absolute path", lineNum, mount.Source)
		}
		if !path.IsAbs(filepath.ToSlash(mount.Target)) {
			return nil, fmt.Errorf("line %d: target %q must be an absolute path", lineNum, mount.Target)
		}

		mounts = append(mounts, mount)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read extra mounts config: %w", err)
	}
	return mounts, nil
}

var (
	composeStopProjectServicesInternal = projects.ComposeStop
	composeUpProjectServicesInternal   = projects.ComposeUp
)

func (s *ProjectService) UpdateProjectServices(ctx context.Context, projectID string, servicesToUpdate []string, user usertypes.Actor, discoverTags bool) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}
	if discoverTags {
		effective, _, loadComposeProjectForProjectErr := s.loadComposeProjectForProjectInternal(ctx, proj, nil, servicesToUpdate...)
		if loadComposeProjectForProjectErr != nil {
			return fmt.Errorf("load project for service image checks: %w", loadComposeProjectForProjectErr)
		}
		changes, loadComposeProjectForProjectErr := s.updates.ImageChanges(ctx, effective, gitOpsSyncIDInternal(proj) != "")
		if loadComposeProjectForProjectErr != nil {
			return loadComposeProjectForProjectErr
		}
		if len(changes) > 0 {
			if _, persistProjectImageChangesErr := s.persistProjectImageChangesInternal(ctx, projectID, changes); persistProjectImageChangesErr != nil {
				return persistProjectImageChangesErr
			}
		}
	}
	return s.updateProjectServicesInternal(ctx, projectID, servicesToUpdate, user)
}

func (s *ProjectService) updateProjectServicesInternal(ctx context.Context, projectID string, servicesToUpdate []string, user usertypes.Actor) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	previousStatus := projectFromDb.Status

	// 1. Load project
	prepare := deployment.PrepareProjectBindDirectories(projectFromDb.Path)
	compProj, _, err := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, prepare, servicesToUpdate...)
	if err != nil {
		return fmt.Errorf("failed to load compose project: %w", err)
	}
	dependents, stoppedDependents := s.deployment.NamespaceDependents(ctx, compProj, servicesToUpdate)
	if len(dependents)+len(stoppedDependents) > 0 {
		slog.InfoContext(ctx, "recreating namespace dependents with updated services", "projectID", projectID, "services", servicesToUpdate, "dependents", dependents, "stoppedDependents", stoppedDependents)
		if compProj, _, err = s.loadComposeProjectForProjectInternal(ctx, projectFromDb, prepare, slices.Concat(servicesToUpdate, dependents, stoppedDependents)...); err != nil {
			return fmt.Errorf("failed to load compose project with dependents: %w", err)
		}
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	// 2. Set status to deploying/restarting
	if updateProjectStatusErr := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusDeploying); updateProjectStatusErr != nil {
		return updateProjectStatusErr
	}

	credentials, err := s.ResolveRegistryCredentials(ctx)
	if err != nil {
		if statusErr := s.updateProjectStatusInternal(ctx, projectID, previousStatus); statusErr != nil {
			slog.ErrorContext(ctx, "UpdateProjectServices: failed to restore project status after credential lookup failure", "projectID", projectID, "error", statusErr)
		}
		return fmt.Errorf("resolve registry credentials: %w", err)
	}

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	if updateServicesErr := s.composeCoordinator.UpdateServices(ctx, projecttypes.ComposeServiceUpdate{
		Project: compProj, Services: servicesToUpdate, Dependents: dependents, StoppedDependents: stoppedDependents,
		Images: s.deployment.ImageOperations(&user, credentials), Progress: progressWriter,
		AuthConfigs: s.composeRegistryAuthConfigsInternal(ctx), WaitTimeout: timeouts.GetDuration(s.settingsService.GetSettingsConfig().DeployWaitTimeout.AsInt(), timeouts.DefaultDeployWait),
		RestoreBeforeMutation: func(ctx context.Context) {
			if statusErr := s.updateProjectStatusInternal(ctx, projectID, previousStatus); statusErr != nil {
				slog.ErrorContext(ctx, "failed to restore project status before service update", "projectID", projectID, "error", statusErr)
			}
		},
		Recover: func(ctx context.Context) { s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID) },
	}); updateServicesErr != nil {
		return updateServicesErr
	}

	// 6. Finalize status
	if updateProjectStatusandCountsErr := s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusRunning); updateProjectStatusandCountsErr != nil {
		return updateProjectStatusandCountsErr
	}

	metadata := database.JSON{
		"action":      "update_services",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
		"services":    append([]string(nil), servicesToUpdate...),
	}
	if len(dependents) > 0 {
		metadata["dependents"] = dependents
	}
	if len(stoppedDependents) > 0 {
		metadata["stoppedDependents"] = stoppedDependents
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, projectFromDb.Name, user, metadata, "could not log project service update action")

	return nil
}

func (s *ProjectService) ArchiveProject(ctx context.Context, projectID string, user usertypes.Actor) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if proj.IsArchived {
		return nil
	}

	// Gate on live Docker state, not the persisted status row, which can go
	// stale when containers are stopped outside an Arcane project action.
	// A project without a compose file cannot have managed containers running
	// and is a prime archive candidate, so it is allowed through.
	services, servicesErr := s.projectServicesInternal(ctx, projectID)
	switch {
	case servicesErr != nil && !errors.Is(servicesErr, common.ErrProjectComposeFileNotFound):
		return fmt.Errorf("cannot verify project is stopped before archiving: %w", servicesErr)
	case servicesErr == nil:
		if _, running := listing.ServiceCounts(services); running > 0 {
			return common.Classify(common.ErrProjectMustBeStopped, errors.New("project must be stopped before archiving"))
		}
	}

	now := time.Now()
	if archiveProjectErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": true,
		"archived_at": now,
	}).Error; archiveProjectErr != nil {
		return fmt.Errorf("failed to archive project: %w", archiveProjectErr)
	}

	metadata := database.JSON{"action": "archived", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project archive action")

	return nil
}

func (s *ProjectService) UnarchiveProject(ctx context.Context, projectID string, user usertypes.Actor) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if !proj.IsArchived {
		return nil
	}

	if unarchiveProjectErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": false,
		"archived_at": gorm.Expr("NULL"),
	}).Error; unarchiveProjectErr != nil {
		return fmt.Errorf("failed to unarchive project: %w", unarchiveProjectErr)
	}

	metadata := database.JSON{"action": "unarchived", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project unarchive action")

	return nil
}

func (s *ProjectService) DeployProject(ctx context.Context, projectID string, user usertypes.Actor, options *projecttypes.DeployOptions) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	if projectFromDb != nil && projectFromDb.IsArchived {
		return common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	if _, resolveProjectComposeFileErr := s.ResolveProjectComposeFile(ctx, projectFromDb); resolveProjectComposeFileErr != nil {
		return resolveProjectComposeFileErr
	}

	if updateProjectStatusErr := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusDeploying); updateProjectStatusErr != nil {
		return fmt.Errorf("failed to update project status to deploying: %w", updateProjectStatusErr)
	}
	var closeSuppression func()
	defer func() {
		if closeSuppression != nil {
			closeSuppression()
		}
	}()

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	projectModel, err := s.composeCoordinator.Deploy(ctx, projecttypes.ComposeDeployment{
		ProjectID: projectID, ProjectPath: projectFromDb.Path, Options: options,
		DefaultPullPolicy: s.settingsService.GetStringSetting(ctx, "defaultDeployPullPolicy", "missing"),
		GitOpsManaged:     projectFromDb.GitOpsManagedBy != nil && *projectFromDb.GitOpsManagedBy != "",
		WaitTimeout: timeouts.GetDuration(
			s.settingsService.GetSettingsConfig().DeployWaitTimeout.AsInt(),
			timeouts.DefaultDeployWait,
		), AuthConfigs: s.composeRegistryAuthConfigsInternal(
			ctx,
		), Progress: progressWriter,
		PreDeploy: func(ctx context.Context) error {
			if s.lifecycleService == nil {
				return nil
			}
			return s.lifecycleService.RunPreDeploy(ctx, projectFromDb, user)
		},
		Load: func(ctx context.Context) (*types.Project, error) {
			model, _, loadComposeProjectForProjectErr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, deployment.PrepareProjectBindDirectories(projectFromDb.Path))
			if loadComposeProjectForProjectErr == nil {
				closeSuppression = s.eventService.BeginComposeSuppressionWindow(model.Name)
			}
			return model, loadComposeProjectForProjectErr
		},
		ResolveImages: func(ctx context.Context) (projecttypes.ComposeImageOperations, error) {
			credentials, resolveRegistryCredentialsErr := s.ResolveRegistryCredentials(ctx)
			operations := s.deployment.ImageOperations(&user, credentials)
			// Deployment pulls are recorded as system actions; builds retain the requesting actor.
			operations.Pull = s.deployment.ImageOperations(nil, credentials).Pull
			return operations, resolveRegistryCredentialsErr
		},
		Recover: func(ctx context.Context) { s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID) },
	})
	if err != nil {
		return err
	}

	metadata := database.JSON{"action": "deploy", "projectID": projectID, "projectName": projectModel.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectDeploy, projectID, projectModel.Name, user, metadata, "could not log project deployment action")

	err = s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusRunning)
	if err != nil {
		slog.Error("failed to update project status and counts after deploy", "projectID", projectID, "error", err)
	}
	return err
}

func (s *ProjectService) DownProject(ctx context.Context, projectID string, user usertypes.Actor) error {
	projectFromDb, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	proj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	if updateProjectStatusErr := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusStopped); updateProjectStatusErr != nil {
		return fmt.Errorf("failed to update project status to stopping: %w", updateProjectStatusErr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(proj.Name)()

	if composeDownErr := projects.ComposeDown(ctx, proj, false); composeDownErr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to bring down project: %w", composeDownErr)
	}

	metadata := database.JSON{
		"action":      "down",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectStop, projectID, projectFromDb.Name, user, metadata, "could not log project down action")

	return s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusStopped)
}

// CreateProject creates a project's directory, files, and DB row. When
// allowNameSuffix is true a directory-name collision is resolved by appending
// "-N" (the interactive default). When false a collision returns
// projects.ErrProjectDirExists (wrapped) so GitOps creates fail loudly instead of
// minting runaway "-N" duplicate projects on a broken binding.
func (
	s *ProjectService,
) CreateProject(
	ctx context.Context,
	name, composeContent string,
	envContent *string,
	manifest projecttypes.CreateProjectWorkspaceManifest,
	uploads map[int][]byte,
	uiTags []string,
	uiTagColors map[string]projecttypes.TagColor,
	user usertypes.Actor,
	allowNameSuffixOptions ...bool,
) (
	*Project,
	error,
) {
	normalizedUITags, err := projects.NormalizeProjectTags(uiTags)
	if err != nil {
		return nil, fmt.Errorf("invalid project tags: %w", err)
	}
	normalizedTagColors, err := tags.NormalizeProjectTagColors(uiTagColors)
	if err != nil {
		return nil, fmt.Errorf("invalid project tag colors: %w", err)
	}
	allowNameSuffix := true
	if len(allowNameSuffixOptions) > 0 {
		allowNameSuffix = allowNameSuffixOptions[0]
	}
	// A top-level `name:` in the compose file is authoritative over the
	// submitted project name.
	if yamlName := projects.ComposeContentProjectName(composeContent); yamlName != "" {
		name = yamlName
	}
	sanitized := projects.SanitizeProjectName(name)

	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	basePath := filepath.Join(projectsDirectory, sanitized)
	var projectPath, folderName string
	if allowNameSuffix {
		projectPath, folderName, err = projects.CreateUniqueDir(ctx, projectsDirectory, basePath, name, utils.DirPerm)
	} else {
		projectPath, folderName, err = projects.CreateExactDir(ctx, projectsDirectory, basePath, name, utils.DirPerm)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}
	projectLogical, err := acfs.LogicalPath(projectsDirectory, projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve created project directory: %w", err)
	}

	proj := &Project{
		Name:         name,
		DirName:      &folderName,
		Path:         projectPath,
		Status:       ProjectStatusStopped,
		ServiceCount: 0,
		RunningCount: 0,
	}

	if applyProjectWorkspaceChangesErr := projects.ApplyProjectWorkspaceChanges(projectPath, manifest.FileChanges, uploads, projects.ProjectWorkspaceApplyOptions{
		MaxDepth:         s.config.ProjectWorkspaceMaxDepth,
		MaxEntries:       s.config.ProjectWorkspaceMaxEntries,
		MaxFileSizeBytes: workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB),
		SkipDirectories:  s.config.ProjectScanSkipDirs,
		ComposeFileName:  projects.DefaultComposeFileName,
	}); applyProjectWorkspaceChangesErr != nil {
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, workspace.WrapProjectWorkspaceError(applyProjectWorkspaceChangesErr)
	}

	// GitOps-originated creates (allowNameSuffix=false) tolerate not-yet-supplied
	// ${VAR} references the same way single-file git sync updates do; interactive
	// creates (allowNameSuffix=true) stay strict.
	if validateComposeContentForUpdateErr := projects.ValidateComposeContentForUpdate(
		ctx,
		projectsDirectory,
		projectPath,
		name,
		composeContent,
		envContent,
		nil,
		"",
		!allowNameSuffix,
	); validateComposeContentForUpdateErr != nil {
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, fmt.Errorf("invalid compose file: %w", validateComposeContentForUpdateErr)
	}

	if writeProjectFilesErr := projects.WriteProjectFiles(ctx, projectsDirectory, projectPath, composeContent, envContent); writeProjectFilesErr != nil {
		// Best-effort cleanup to restore pre-transaction behavior.
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, fmt.Errorf("failed to save project files: %w", writeProjectFilesErr)
	}
	composeMeta, err := projects.ParseArcaneComposeMetadata(
		ctx,
		filepath.Join(projectPath, projects.DefaultComposeFileName),
		projectsDirectory,
		s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
	)
	if err != nil {
		slog.WarnContext(ctx, "failed to read Compose project tags during creation", "projectName", name, "error", err)
		composeMeta = projects.ArcaneComposeMetadata{}
	}
	normalizedUITags = tags.ExcludeComposeOwnedUITags(normalizedUITags, composeMeta.ProjectTags)

	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if createProjectErr := tx.Create(proj).Error; createProjectErr != nil {
			return createProjectErr
		}
		return tags.AttachInitial(tagStoreInternal{tx: tx}, proj.ID, normalizedUITags, normalizedTagColors)
	}); transactionErr != nil {
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, fmt.Errorf("failed to create project: %w", transactionErr)
	}
	s.refreshComposeProjectNameInternal(ctx, proj)
	s.refreshProjectImageRefsInternal(ctx, proj)
	if reconcileComposeProjectTagsErr := s.reconcileComposeProjectTagsInternal(ctx, proj.ID, composeMeta.ProjectTags); reconcileComposeProjectTagsErr != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		databaseCleanupErr := s.db.WithContext(cleanupCtx).Transaction(func(tx *gorm.DB) error {
			return deleteProjectWithTagsInternal(tx, proj.ID)
		})
		fileCleanupErr := acfs.RemoveAll(cleanupCtx, projectsDirectory, projectLogical)
		if databaseCleanupErr != nil {
			databaseCleanupErr = fmt.Errorf("rollback project database state after tag reconciliation failure: %w", databaseCleanupErr)
		}
		if fileCleanupErr != nil {
			fileCleanupErr = fmt.Errorf("rollback project files after tag reconciliation failure: %w", fileCleanupErr)
		}
		return nil, errors.Join(fmt.Errorf("reconcile Compose project tags: %w", reconcileComposeProjectTagsErr), databaseCleanupErr, fileCleanupErr)
	}

	metadata := database.JSON{"action": "create", "projectID": proj.ID, "projectName": proj.Name, "path": projectPath}
	s.logProjectEventInternal(ctx, event.EventTypeProjectCreate, proj.ID, proj.Name, user, metadata, "could not log project creation")

	return proj, nil
}

func (s *ProjectService) DestroyProject(ctx context.Context, projectID string, removeFiles, removeVolumes bool, user usertypes.Actor) error {
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

	if downProjectErr := s.DownProject(ctx, projectID, usertypes.SystemUser); downProjectErr != nil {
		slog.WarnContext(ctx, "failed to bring down project", "error", downProjectErr)
	}

	if removeVolumes {
		if compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil); lerr == nil {
			defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()
			if derr := projects.ComposeDown(ctx, compProj, true); derr != nil {
				slog.WarnContext(ctx, "failed to remove volumes", "error", derr)
			}
		} else {
			slog.WarnContext(ctx, "failed to load compose project for volume removal", "error", lerr)
		}
	}

	if removeFiles {
		slog.DebugContext(ctx, "Removing project files", "path", proj.Path)
		// An imported project can live anywhere, so the removal is rooted at the
		// parent directory and names the project directory itself.
		if removeAllErr := acfs.RemoveAll(ctx, filepath.Dir(proj.Path), "/"+filepath.Base(proj.Path)); removeAllErr != nil {
			slog.ErrorContext(ctx, "Failed to remove project files", "path", proj.Path, "error", removeAllErr)
			return fmt.Errorf("failed to remove project files: %w", removeAllErr)
		}
		slog.InfoContext(ctx, "Project files removed successfully", "path", proj.Path)
	}

	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteProjectWithTagsInternal(tx, projectID)
	}); transactionErr != nil {
		return fmt.Errorf("failed to delete project from database: %w", transactionErr)
	}

	if !removeFiles {
		if projectsDir, dirErr := s.GetProjectsDirectory(ctx); dirErr != nil {
			slog.WarnContext(ctx, "Failed to resolve projects directory for quarantine", "error", dirErr)
		} else if projects.IsSafeSubdirectory(projectsDir, proj.Path) && filepath.Clean(projectsDir) != filepath.Clean(proj.Path) {
			trashName := fmt.Sprintf("%s%s-%d", projects.ArcaneTrashPrefix, filepath.Base(proj.Path), time.Now().Unix())
			trashPath := filepath.Join(filepath.Dir(proj.Path), trashName)
			if renameErr := acfs.Rename(ctx, filepath.Dir(proj.Path), "/"+filepath.Base(proj.Path), "/"+trashName); renameErr != nil {
				slog.WarnContext(ctx, "Failed to quarantine project files", "path", proj.Path, "trashPath", trashPath, "error", renameErr)
			} else {
				slog.InfoContext(ctx, "Project files quarantined successfully", "path", proj.Path, "trashPath", trashPath)
			}
		}
	}
	s.invalidateProjectCachesInternal(projectID)

	metadata := database.JSON{"action": "destroy", "projectID": projectID, "projectName": proj.Name, "removeFiles": removeFiles, "removeVolumes": removeVolumes}
	s.logProjectEventInternal(ctx, event.EventTypeProjectDelete, projectID, proj.Name, user, metadata, "could not log project destroy action")

	return nil
}

func (s *ProjectService) RedeployProject(ctx context.Context, projectID string, user usertypes.Actor, options *projecttypes.DeployOptions) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	if _, resolveProjectComposeFileErr := s.ResolveProjectComposeFile(ctx, proj); resolveProjectComposeFileErr != nil {
		return resolveProjectComposeFileErr
	}

	disabled := s.projectRedeployDisabledInternal(ctx, *proj)
	if disabled {
		return errors.New("arcane cannot redeploy itself; use the system upgrade flow (Settings -> Updates) instead")
	}

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	if progressWriter == nil {
		progressWriter = io.Discard
	}

	credentials, cerr := s.ResolveRegistryCredentials(ctx)
	if cerr != nil {
		slog.WarnContext(ctx, "failed to resolve registry credentials for redeploy pull", "error", cerr)
	}
	if pullProjectImagesErr := s.PullProjectImages(ctx, projectID, progressWriter, user, credentials); pullProjectImagesErr != nil {
		slog.WarnContext(ctx, "failed to pull project images", "error", pullProjectImagesErr)
	}

	return s.DeployProject(ctx, projectID, user, options)
}

func (s *ProjectService) projectRedeployDisabledInternal(ctx context.Context, proj Project) bool {
	containers, err := s.details.ComposeContainers(ctx)
	if err != nil {
		slog.WarnContext(ctx, "could not list compose containers to check self-redeploy guard; skipping guard", "error", err)
		return false
	}

	containersByProject := listing.GroupComposeContainersByProject(containers)

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	for _, containerSummary := range listing.ProjectContainers(projectRecordInternal(proj), containersByProject) {
		if labels.ShouldDisableArcaneServerRedeploy(containerSummary.Labels, containerSummary.ID, currentContainerID, currentContainerErr) {
			return true
		}
	}

	return false
}

func (s *ProjectService) PullProjectImages(ctx context.Context, projectID string, progressWriter io.Writer, user usertypes.Actor, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if lerr != nil {
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	return s.deployment.PullImages(ctx, compProj, progressWriter, user, credentials)
}

func (s *ProjectService) BuildProjectServices(ctx context.Context, projectID string, options projecttypes.BuildOptions, progressWriter io.Writer, user *usertypes.Actor) error {
	projectFromDb, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	projectModel, _, derr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if derr != nil {
		return fmt.Errorf("failed to load compose project in %s: %w", projectFromDb.Path, derr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(projectModel.Name)()

	return s.composeCoordinator.BuildServices(ctx, projectID, projectModel, options, progressWriter, s.deployment.ImageOperations(user, nil))
}

// EnsureProjectImagesPresent checks all compose service images for the project and
// pulls based on service pull policy:
// - always/refresh: always pull
// - missing/if_not_present/default: pull only if local image is missing
// - never: never pull (fails early if image is missing locally)
func (s *ProjectService) EnsureProjectImagesPresent(ctx context.Context, projectID string, progressWriter io.Writer, user usertypes.Actor, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if lerr != nil {
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	return s.composeCoordinator.EnsureImagesPresent(ctx, compProj, progressWriter, s.deployment.ImageOperations(&user, credentials))
}

func (s *ProjectService) restoreProjectStatusAfterFailedDeployInternal(ctx context.Context, projectID string) {
	services, err := s.projectServicesInternal(ctx, projectID)
	if err == nil {
		serviceCount, runningCount := listing.ServiceCounts(services)
		status := ProjectStatus(listing.ProjectStatus(services))
		updateErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
			"status":        status,
			"service_count": serviceCount,
			"running_count": runningCount,
			"updated_at":    time.Now(),
		}).Error
		if updateErr == nil {
			return
		}
		slog.WarnContext(ctx, "failed to restore project status after deploy failure", "projectID", projectID, "error", updateErr)
	} else {
		slog.WarnContext(ctx, "failed to inspect project services after deploy failure", "projectID", projectID, "error", err)
	}

	if updateErr := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusStopped); updateErr != nil {
		slog.WarnContext(ctx, "failed to set stopped status after deploy failure", "projectID", projectID, "error", updateErr)
	}
}

func (s *ProjectService) RestartProject(ctx context.Context, projectID string, services []string, user usertypes.Actor) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}
	if _, resolveProjectComposeFileErr := s.ResolveProjectComposeFile(ctx, proj); resolveProjectComposeFileErr != nil {
		return resolveProjectComposeFileErr
	}

	if updateProjectStatusErr := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRestarting); updateProjectStatusErr != nil {
		return fmt.Errorf("failed to update project status to restarting: %w", updateProjectStatusErr)
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to load compose project: %w", lerr)
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	if composeRestartErr := projects.ComposeRestart(ctx, compProj, services); composeRestartErr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return fmt.Errorf("failed to restart project: %w", composeRestartErr)
	}

	metadata := database.JSON{
		"action":      "restart",
		"projectID":   projectID,
		"projectName": proj.Name,
	}
	if len(services) > 0 {
		metadata["services"] = append([]string(nil), services...)
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectStart, projectID, proj.Name, user, metadata, "could not log project restart action")

	return s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusRunning)
}

func (s *ProjectService) updateProjectStatusandCountsInternal(ctx context.Context, projectID string, status ProjectStatus) error {
	services, err := s.projectServicesInternal(ctx, projectID)
	if err != nil {
		slog.Error("loading project services failed during status update", "projectID", projectID, "error", err)
		return s.updateProjectStatusInternal(ctx, projectID, status)
	}

	serviceCount, runningCount := listing.ServiceCounts(services)
	if updateStatusErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"status":        status,
		"service_count": serviceCount,
		"running_count": runningCount,
		"updated_at":    time.Now(),
	}).Error; updateStatusErr != nil {
		return fmt.Errorf("failed to update project status and counts: %w", updateStatusErr)
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
		return fmt.Errorf("failed to update project status: %w", res.Error)
	}

	return nil
}

func (s *ProjectService) GetProjectContent(ctx context.Context, projectID string) (composeContent, envContent, overrideContent string, err error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return "", "", "", err
	}

	composePath, composeErr := s.ResolveProjectComposeFile(ctx, proj)
	switch {
	case composeErr == nil, errors.Is(composeErr, common.ErrProjectComposeFileNotFound):
	case errors.Is(composeErr, common.ErrProjectEnvUnreadable):
		projectsDirectory, dirErr := s.GetProjectsDirectory(ctx)
		if dirErr != nil {
			slog.DebugContext(ctx, "failed to resolve projects directory for compose identification", "projectID", proj.ID, "error", dirErr)
		}
		composePath, composeErr = projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
		if composeErr != nil && (!errors.Is(composeErr, common.ErrProjectEnvUnreadable) || composePath == "") {
			return "", "", "", fmt.Errorf("failed to identify project compose file: %w", composeErr)
		}
	default:
		return "", "", "", composeErr
	}

	composeContent, envContent, err = projects.ReadProjectFiles(ctx, proj.Path, composePath)
	if err != nil {
		return "", "", "", err
	}

	return composeContent, envContent, projects.ReadComposeOverrideContent(proj.Path), nil
}

func (s *ProjectService) populateDetailsComposeContentInternal(ctx context.Context, proj *Project, opts projecttypes.DetailsOptions, composeSelection []string, resp *projecttypes.Details) error {
	if !opts.IncludeComposeContent {
		return nil
	}
	composeContent, _, overrideContent, err := s.GetProjectContent(ctx, proj.ID)
	if err != nil {
		return fmt.Errorf("failed to read project compose content: %w", err)
	}
	resp.ComposeContent = composeContent
	resp.OverrideFileName, resp.OverrideContent = projectdetails.ResolveDetailsOverride(proj.Path, overrideContent, composeSelection)
	return nil
}

func (s *ProjectService) GetProjectDetails(ctx context.Context, projectID string, opts projecttypes.DetailsOptions) (projecttypes.Details, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return projecttypes.Details{}, err
	}
	projectsDir, projectsDirErr := s.GetProjectsDirectory(ctx)
	if projectsDirErr != nil {
		// Relative paths and the .env.global selection layer degrade without a
		// projects directory; keep the details response intact but log it.
		slog.WarnContext(ctx, "failed to resolve projects directory for project details", "projectID", projectID, "error", projectsDirErr)
	}

	var resp projecttypes.Details
	if mapStructErr := mapping.MapStruct(proj, &resp); mapStructErr != nil {
		return projecttypes.Details{}, fmt.Errorf("failed to map project: %w", mapStructErr)
	}

	resp.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	resp.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	resp.IsArchived = proj.IsArchived
	resp.ArchivedAt = proj.ArchivedAt
	resp.HasBuildDirective = proj.BuildImageRefsJSON != nil && len(projects.ParseImageRefsJSON(*proj.BuildImageRefsJSON)) > 0
	resp.DirName = mo.PointerToOption(proj.DirName).OrEmpty()
	resp.RelativePath = listing.RelativePath(projectsDir, proj.Path)
	resp.GitOpsManagedBy = proj.GitOpsManagedBy
	meta := s.ProjectMetadata(ctx, *proj, nil)
	icon := iconcatalog.Resolve(IconCatalogForContext(ctx), meta.ProjectIcon)
	resp.IconLightURL, resp.IconDarkURL = icon.IconLightURL, icon.IconDarkURL
	resp.Links = meta.ProjectLinks
	for _, link := range resp.Links {
		resp.URLs = append(resp.URLs, link.URL) //nolint:staticcheck // Preserve the deprecated URLs field for v2 clients.
	}
	resp.Tags, err = s.GetProjectTags(ctx, projectID)
	if err != nil {
		return projecttypes.Details{}, err
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
		selLogLevel := kit.Ternary(errors.Is(selErr, common.ErrProjectEnvUnreadable), slog.LevelDebug, slog.LevelWarn)
		slog.Log(ctx, selLogLevel, "failed to resolve COMPOSE_FILE selection for project details", "projectID", proj.ID, "path", proj.Path, "error", selErr)
		composeSelection = nil
	}
	resp.ComposeFiles = projectdetails.ComposeSelectionRelativePaths(proj.Path, composeSelection)
	resp.ConfigurationError = projects.CheckProjectEnvAccess(ctx, projectsDir, proj.Path)

	if populateDetailsComposeContentErr := s.populateDetailsComposeContentInternal(ctx, proj, opts, composeSelection, &resp); populateDetailsComposeContentErr != nil {
		return projecttypes.Details{}, populateDetailsComposeContentErr
	}
	if opts.IncludeEnvState {
		envState, readProjectEnvStateErr := projects.ReadProjectEnvState(proj.Path)
		if readProjectEnvStateErr != nil {
			return projecttypes.Details{}, fmt.Errorf("failed to read project env state: %w", readProjectEnvStateErr)
		}
		effectiveEnvContent, readProjectEnvStateErr := projectsync.ResolveStoredEffectiveEnvContent(envState)
		if readProjectEnvStateErr != nil {
			return projecttypes.Details{}, readProjectEnvStateErr
		}
		resp.EnvContent = effectiveEnvContent
	}

	s.enrichComposeDetailsInternal(ctx, proj, opts, &resp)
	s.enrichWithGitOpsInfo(ctx, proj, &resp)

	// Refresh runtime status/counts even when callers do not request the full
	// runtime service array. DB values are only a fallback when Docker lookup
	// or compose loading fails.
	services, serr := s.projectServicesInternal(ctx, projectID)
	if serr == nil && services != nil {
		resp.ServiceCount = len(services)
		_, runningCount := listing.ServiceCounts(services)
		resp.RunningCount = runningCount
		resp.Status = listing.ProjectStatus(services)

		if opts.IncludeRuntimeServices || opts.IncludeUpdateInfo {
			resp.RuntimeServices = services
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

func (s *ProjectService) enrichProjectUpdateInfoInternal(ctx context.Context, resp *projecttypes.Details) {
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

	scoped := s.details.ContainerUpdateInfo(ctx, []projecttypes.Details{*resp})
	if len(resp.Services) > 0 {
		records := s.details.ServiceUpdateRecords(ctx, []string{resp.ID})
		resp.UpdateInfo = BuildConfiguredUpdateInfo(resp.ID, resp.Services, updateInfoByRef, records, projectdetails.ConfiguredRuntimeServiceUpdateInfo(resp.Services, resp.RuntimeServices, scoped))
		return
	}
	resp.UpdateInfo = BuildUpdateInfoSummary(imageRefs, projectdetails.MergeProjectContainerUpdateInfo(updateInfoByRef, resp.RuntimeServices, scoped))
}

func (s *ProjectService) enrichProjectsWithUpdateInfoInternal(
	ctx context.Context,
	projectsList []Project,
	details []projecttypes.Details,
	includeHidden bool,
	env *projectMetadataEnvInternal,
) {
	if len(projectsList) == 0 || len(details) == 0 {
		return
	}
	if env == nil {
		env = s.newProjectMetadataEnvInternal(ctx, projectsList)
	}

	var hiddenServicesByProjectID, hiddenRefsByProjectID map[string]map[string]bool
	if !includeHidden {
		hiddenServicesByProjectID, hiddenRefsByProjectID = projectdetails.ExcludeHiddenRuntimeServices(details)
	}

	imageRefsByProjectID := make(map[string][]string, len(projectsList))
	allImageRefs := make([]string, 0)
	servicesByProjectID := make(map[string][]types.ServiceConfig, len(projectsList))
	projectIDs := make([]string, 0, len(projectsList))

	type imageRefsResult struct {
		projectID string
		refs      []string
		services  []types.ServiceConfig
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

			refs, services := s.resolveProjectUpdateServicesInternal(ctx, proj, env, includeHidden, hiddenServicesByProjectID[proj.ID], hiddenRefsByProjectID[proj.ID])
			resultsCh <- imageRefsResult{projectID: proj.ID, refs: refs, services: services}
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

	recordsByProjectID := projectdetails.GroupUpdateRecordsByProject(s.details.ServiceUpdateRecords(ctx, projectIDs))
	scoped := s.details.ContainerUpdateInfo(ctx, details)
	for i := range details {
		if services := servicesByProjectID[details[i].ID]; services != nil {
			details[i].UpdateInfo = BuildConfiguredUpdateInfo(
				details[i].ID,
				services,
				updateInfoByRef,
				recordsByProjectID[details[i].ID],
				projectdetails.ConfiguredRuntimeServiceUpdateInfo(
					services,
					details[i].RuntimeServices,
					scoped,
				),
			)
			continue
		}
		refs := imageRefsByProjectID[details[i].ID]
		if len(refs) == 0 {
			refs = projects.ImageRefsFromRuntimeServices(details[i].RuntimeServices)
		}
		details[i].UpdateInfo = BuildUpdateInfoSummary(refs, projectdetails.MergeProjectContainerUpdateInfo(updateInfoByRef, details[i].RuntimeServices, scoped))
	}
}

func (
	s *ProjectService,
) resolveProjectUpdateServicesInternal(
	ctx context.Context,
	proj Project,
	env *projectMetadataEnvInternal,
	includeHidden bool,
	hiddenRuntimeServices, hiddenRuntimeRefs map[string]bool,
) (
	[]string,
	[]types.ServiceConfig,
) {
	composeProject, err := s.getCachedComposeProjectInternal(ctx, &proj, env)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project services for update summary", "projectID", proj.ID, "projectName", proj.Name, "error", err)
		refs := projects.ParseImageRefsJSON(proj.ImageRefsJSON)
		return slices.DeleteFunc(refs, func(ref string) bool { return hiddenRuntimeRefs[ref] }), nil
	}
	services := make([]types.ServiceConfig, 0, len(composeProject.Services))
	for _, service := range composeProject.Services {
		hidden, _ := kit.ParseBool(service.Labels[libarcane.HiddenResourceLabel])
		if !includeHidden && (hidden || hiddenRuntimeServices[service.Name]) {
			continue
		}
		services = append(services, service)
	}
	return projects.ImageRefsFromComposeConfigs(services), services
}

// BuildConfiguredUpdateInfo matches checks to current services and aggregates their results.
func BuildConfiguredUpdateInfo(
	projectID string,
	services []types.ServiceConfig,
	byRef map[string]*imagetypes.UpdateInfo,
	records []imageupdate.ImageUpdateRecord,
	runtimeUpdates ...map[string]*imagetypes.UpdateInfo,
) *projecttypes.UpdateInfo {
	checks := make(map[string]*imageupdate.ImageUpdateRecord)
	for i := range records {
		if records[i].ProjectID == projectID {
			checks[records[i].ServiceName] = &records[i]
		}
	}
	serviceUpdates := make(map[string]projecttypes.ServiceUpdateInfo, len(services))
	selected := make(map[string]*imagetypes.UpdateInfo)
	runtime := make([]projecttypes.RuntimeService, 0, len(services))
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
		} else if policyErr == nil && policy.Strategy == "digest" && !imageref.IsUpdateCheckDisabled(service.Labels) {
			info = byRef[imageRef]
		}

		if len(runtimeUpdates) > 0 && runtimeUpdates[0][service.Name] != nil {
			info = projectdetails.MergeProjectContainerUpdateInfo(
				nil,
				[]projecttypes.RuntimeService{
					{
						ContainerID: "preview",
						Image:       imageRef,
					},
					{
						ContainerID: "runtime",
						Image:       imageRef,
					},
				},
				map[string]*imagetypes.UpdateInfo{
					"preview": info,
					"runtime": runtimeUpdates[0][service.Name],
				},
			)[imageRef]
		}
		serviceUpdates[service.Name] = projecttypes.ServiceUpdateInfo{ImageRef: imageRef, UpdateInfo: info}
		selected[service.Name] = info
		runtime = append(runtime, projecttypes.RuntimeService{ContainerID: service.Name, Image: imageRef})
		if info == nil {
			unknownRefs[imageRef] = true
		}
	}
	merged := projectdetails.MergeProjectContainerUpdateInfo(nil, runtime, selected)
	for imageRef := range unknownRefs {
		if info := merged[imageRef]; info == nil || !info.HasUpdate {
			delete(merged, imageRef)
		}
	}
	summary := BuildUpdateInfoSummary(projects.ImageRefsFromComposeConfigs(services), merged)
	summary.ServiceUpdates = serviceUpdates
	return summary
}

func (s *ProjectService) getProjectImageRefsFromComposeInternal(ctx context.Context, proj Project, env *projectMetadataEnvInternal) ([]string, []string, error) {
	composeProject, err := s.getCachedComposeProjectInternal(ctx, &proj, env)
	if err != nil {
		return nil, nil, fmt.Errorf("load compose project: %w", err)
	}

	return projects.ImageRefsFromComposeServices(composeProject.Services), projects.BuildImageRefsFromComposeProject(composeProject), nil
}

// BuildUpdateInfoSummary aggregates image checks into a project update summary.
func BuildUpdateInfoSummary(
	imageRefs []string,
	updateInfoByRef map[string]*imagetypes.UpdateInfo,
) *projecttypes.UpdateInfo {
	imageCount := len(imageRefs)
	summary := &projecttypes.UpdateInfo{
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

func (s *ProjectService) enrichWithGitOpsInfo(ctx context.Context, proj *Project, resp *projecttypes.Details) {
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

func (s *ProjectService) enrichComposeDetailsInternal(ctx context.Context, proj *Project, opts projecttypes.DetailsOptions, resp *projecttypes.Details) {
	composeFile, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		if !errors.Is(err, common.ErrProjectEnvUnreadable) {
			return
		}
		// The env is unreadable, so only the compose file's identity is known:
		// name it for the UI and skip every enrichment that needs interpolation.
		projectsDirectory, dirErr := s.GetProjectsDirectory(ctx)
		if dirErr != nil {
			slog.DebugContext(ctx, "failed to resolve projects directory for compose identification", "projectID", proj.ID, "error", dirErr)
		}
		identified, detectErr := projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
		if detectErr != nil && (!errors.Is(detectErr, common.ErrProjectEnvUnreadable) || identified == "") {
			slog.WarnContext(ctx, "failed to identify project compose file", "projectID", proj.ID, "error", detectErr)
			return
		}
		if identified != "" {
			resp.ComposeFileName = filepath.Base(identified)
		}
		return
	}
	resp.ComposeFileName = filepath.Base(composeFile)
	if opts.IncludeIncludeFiles {
		resp.IncludeFiles = s.details.IncludeFiles(ctx, composeFile)
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
	svcList := make([]types.ServiceConfig, 0, len(composeProj.Services))
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

func (s *ProjectService) CountServicesFromCompose(ctx context.Context, p Project) (int, error) {
	proj, _, err := s.loadComposeProjectForProjectInternal(ctx, &p, nil)
	if err != nil {
		return 0, err
	}

	return len(proj.Services), nil
}

// projectServicesFromContainersInternal derives runtime services from labeled
// containers for projects whose Compose file cannot be loaded.
func (s *ProjectService) projectServicesFromContainersInternal(ctx context.Context, proj *Project, meta projects.ArcaneComposeMetadata) ([]projecttypes.RuntimeService, error) {
	containers, err := s.details.ComposeContainers(ctx)
	if err != nil {
		return nil, err
	}
	matched := listing.ProjectContainers(projectRecordInternal(*proj), listing.GroupComposeContainersByProject(containers))
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	services := make([]projecttypes.RuntimeService, 0, len(matched))
	for _, c := range matched {
		services = append(services, listing.RuntimeServiceFromContainer(IconCatalogForContext(ctx), c, meta, currentContainerID, currentContainerErr))
	}
	return services, nil
}

func (s *ProjectService) ListAllProjects(ctx context.Context) ([]Project, error) {
	var items []Project
	if err := s.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return items, nil
}

func (s *ProjectService) countProjectFolders(ctx context.Context) (int, error) {
	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return 0, fmt.Errorf("could not determine projects directory: %w", err)
	}

	// os.* rather than acfs: this probes the projects directory itself, which is
	// the confinement root and may not exist yet.
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

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(ctx, projectsDir, followProjectSymlinks, s.config.ProjectScanMaxDepth)
	if discoveryErr != nil {
		return 0, fmt.Errorf("failed to discover project directories in %s: %w", projectsDir, discoveryErr)
	}

	return len(discoveredProjects), nil
}

func (s *ProjectService) ListProjects(ctx context.Context, params pagination.QueryParams) ([]projecttypes.Details, pagination.Response, error) {
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
	query = listing.ApplyProjectArchivedDBFilter(query, archivedFilter)
	query = listing.ApplyProjectTagsDBFilter(query, tagsFilter)
	sortsByDerivedStatus := strings.EqualFold(strings.TrimSpace(params.Sort), "status")
	if statusFilter != "" || updatesFilter != "" || labelFilter != "" || sortsByDerivedStatus {
		return s.listProjectsWithDerivedFiltersInternal(ctx, params, query)
	}

	if term := strings.TrimSpace(params.Search); term != "" {
		query = listing.ApplyProjectSearchDBFilter(query, term)
	}

	query = pagination.ApplyFilter(query, "status", params.Filters["status"])

	var projectsArray []Project
	paginationResp, err := pagination.PaginateAndSortDB(params, query, &projectsArray)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate projects: %w", err)
	}

	slog.DebugContext(ctx, "Retrieved projects from database",
		"count", len(projectsArray))

	// Fetch live status concurrently for all projects
	env := s.newProjectMetadataEnvInternal(ctx, projectsArray)
	result := s.fetchProjectStatusConcurrently(ctx, projectsArray, env)
	if enrichProjectsWithTagsErr := s.enrichProjectsWithTagsInternal(ctx, result); enrichProjectsWithTagsErr != nil {
		return nil, pagination.Response{}, enrichProjectsWithTagsErr
	}
	s.enrichProjectsWithUpdateInfoInternal(ctx, projectsArray, result, true, env)

	slog.DebugContext(ctx, "Completed ListProjects request",
		"result_count", len(result))

	return result, paginationResp, nil
}

func (s *ProjectService) listProjectsWithDerivedFiltersInternal(
	ctx context.Context,
	params pagination.QueryParams,
	query *gorm.DB,
) ([]projecttypes.Details, pagination.Response, error) {
	params = listing.ClampDerivedLimit(params)

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
) (pagination.FilterResult[projecttypes.Details], error) {
	var projectsArray []Project
	if term := strings.TrimSpace(params.Search); term != "" {
		query = listing.ApplyProjectSearchDBFilter(query, term)
	}
	if err := query.Find(&projectsArray).Error; err != nil {
		return pagination.FilterResult[projecttypes.Details]{}, fmt.Errorf("failed to list projects: %w", err)
	}

	// Filtering, searching, and sorting only read database columns, tags, and
	// the container snapshot, so every candidate gets a lean row and the
	// compose-backed presentation fields are resolved for the page alone.
	env := s.newProjectMetadataEnvInternal(ctx, nil)
	snapshot := s.listing.Snapshot(ctx)
	items := s.projectListRowsInternal(ctx, env.projectsDirectory, projectsArray, snapshot)
	if err := s.enrichProjectsWithTagsInternal(ctx, items); err != nil {
		return pagination.FilterResult[projecttypes.Details]{}, err
	}
	updatesFiltered := strings.TrimSpace(params.Filters["updates"]) != ""
	if updatesFiltered {
		s.preloadGitOpsComposePathsInternal(ctx, env, projectsArray)
		s.enrichProjectsWithUpdateInfoInternal(ctx, projectsArray, items, true, env)
		items = s.appendDiscoveredComposeProjectUpdatesInternal(ctx, params, projectsArray, items, snapshot)
	}

	byID := make(map[string]Project, len(projectsArray))
	for _, proj := range projectsArray {
		byID[proj.ID] = proj
	}
	result, pageIndexes := listing.Page(items, params, func(id string) bool {
		_, tracked := byID[id]
		return tracked
	})
	pageProjects := make([]Project, 0, len(pageIndexes))
	pageDetails := make([]projecttypes.Details, 0, len(pageIndexes))
	for _, i := range pageIndexes {
		pageProjects = append(pageProjects, byID[result.Items[i].ID])
		pageDetails = append(pageDetails, result.Items[i])
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
		allContainers, err = s.details.ComposeContainers(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to list containers for project update count: %w", err)
		}
	}

	// One full scan: archived projects are excluded from the update count but
	// still mark their compose stacks as known during discovery, so loading
	// everything here saves the known-name pass its own table scan.
	var allProjects []Project
	if err := s.db.WithContext(ctx).Find(&allProjects).Error; err != nil {
		return 0, fmt.Errorf("failed to list projects for update count: %w", err)
	}

	activeProjects := make([]Project, 0, len(allProjects))
	for _, proj := range allProjects {
		if !proj.IsArchived {
			activeProjects = append(activeProjects, proj)
		}
	}

	// Runtime container IDs keep tag-policy updates scoped to their project.
	details := make([]projecttypes.Details, len(activeProjects))
	containersByProject := listing.GroupComposeContainersByProject(allContainers)
	for i, proj := range activeProjects {
		details[i].ID = proj.ID
		for _, c := range listing.ProjectContainers(projectRecordInternal(proj), containersByProject) {
			details[i].RuntimeServices = append(
				details[i].RuntimeServices,
				projecttypes.RuntimeService{
					Name: dockerutil.ComposeServiceLabel(
						c.Labels,
					),
					ContainerID:     c.ID,
					Image:           c.Image,
					ImageID:         c.ImageID,
					ContainerLabels: c.Labels,
				},
			)
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
		hidden, _ := kit.ParseBool(c.Labels[libarcane.HiddenResourceLabel])
		if !hidden {
			visibleContainers = append(visibleContainers, c)
		}
	}
	return count + s.countDiscoveredComposeProjectUpdatesInternal(ctx, allProjects, true, visibleContainers), nil
}

// fetchProjectStatusConcurrently builds complete list rows for an already
// paginated page: live status from a single Docker API call plus the
// compose-backed presentation fields. metaEnv is resolved once for the whole
// list: ProjectMetadata would otherwise re-stat the projects directory,
// re-clone settings, and re-query GitOps compose paths per project.
func (s *ProjectService) fetchProjectStatusConcurrently(ctx context.Context, projectsList []Project, metaEnv *projectMetadataEnvInternal) []projecttypes.Details {
	results := s.projectListRowsInternal(ctx, metaEnv.projectsDirectory, projectsList, s.listing.Snapshot(ctx))
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
	ids := slices.Collect(maps.Keys(counts))
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
	if setErr := s.metaCache.Set(p.ID, fingerprint, p.Path, env.projectsDirectory, composeFile, meta.ComposeFiles, meta.EnvFiles, meta); setErr != nil {
		slog.DebugContext(ctx, "failed to cache Compose metadata", "projectID", p.ID, "error", setErr)
	}

	return meta
}

// IconCatalogForContext resolves the icon catalog of the requesting
// user. On agent-proxied calls the caller is a synthetic user whose preference
// is populated from the X-Arcane-Icon-Catalog header the manager forwards.
// Background jobs have no user attached and fall back to the default catalog.
func IconCatalogForContext(ctx context.Context) string {
	if u, ok := userctx.CurrentUserFromContext(ctx); ok && u != nil && u.Preferences.IconCatalog != nil && *u.Preferences.IconCatalog != "" {
		return *u.Preferences.IconCatalog
	}
	return iconcatalog.DefaultCatalog
}

func (s *ProjectService) refreshProjectImageRefsInternal(ctx context.Context, proj *Project) {
	if proj == nil || proj.ID == "" {
		return
	}

	s.invalidateProjectCachesInternal(proj.ID)
	refs, buildRefs, err := s.getProjectImageRefsFromComposeInternal(ctx, *proj, nil)
	if err != nil {
		if dbErr := s.db.WithContext(ctx).
			Model(&Project{}).
			Where("id = ?", proj.ID).
			Updates(map[string]any{
				"image_refs_json":       "",
				"build_image_refs_json": nil,
			}).Error; dbErr != nil {
			slog.WarnContext(ctx, "failed to clear stale project image refs", "projectID", proj.ID, "error", dbErr)
		}
		proj.ImageRefsJSON = ""
		proj.BuildImageRefsJSON = nil
		slog.WarnContext(ctx, "failed to refresh project image refs", "projectID", proj.ID, "projectName", proj.Name, "error", err)
		return
	}
	imageRefsJSON := projects.MarshalImageRefsJSON(refs)
	buildImageRefsJSON := cmp.Or(projects.MarshalImageRefsJSON(buildRefs), "[]")
	if persistImageRefsErr := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", proj.ID).
		Updates(map[string]any{
			"image_refs_json":       imageRefsJSON,
			"build_image_refs_json": buildImageRefsJSON,
		}).Error; persistImageRefsErr != nil {
		slog.WarnContext(ctx, "failed to persist project image refs", "projectID", proj.ID, "error", persistImageRefsErr)
		return
	}
	proj.ImageRefsJSON = imageRefsJSON
	proj.BuildImageRefsJSON = new(buildImageRefsJSON)
}

func (s *ProjectService) HandleProjectFilesChanged(ctx context.Context, paths []string) {
	if len(paths) == 0 || s.db == nil {
		return
	}

	affected, err := s.resolveProjectsByChangedPathsInternal(ctx, paths)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve changed project files", "error", err)
		return
	}
	for i := range affected {
		s.invalidateProjectCachesInternal(affected[i].ID)
		s.refreshProjectImageRefsInternal(ctx, &affected[i])
		if reconcileComposeTagsForProjectErr := s.reconcileComposeTagsForProjectInternal(ctx, &affected[i]); reconcileComposeTagsForProjectErr != nil {
			slog.WarnContext(ctx, "failed to reconcile Compose project tags after file change", "projectID", affected[i].ID, "error", reconcileComposeTagsForProjectErr)
		}
	}
}

func (s *ProjectService) BackfillProjectImageRefs(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	var projectsList []Project
	if err := s.db.WithContext(ctx).
		Where("build_image_refs_json IS NULL").
		Find(&projectsList).Error; err != nil {
		return 0, fmt.Errorf("list projects for image ref backfill: %w", err)
	}
	for i := range projectsList {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		s.refreshProjectImageRefsInternal(ctx, &projectsList[i])
	}
	return len(projectsList), nil
}

func (s *ProjectService) resolveProjectsByChangedPathsInternal(ctx context.Context, paths []string) ([]Project, error) {
	var projectsList []Project
	if err := s.db.WithContext(ctx).Find(&projectsList).Error; err != nil {
		return nil, fmt.Errorf("list projects for changed paths: %w", err)
	}

	seen := make(map[string]struct{})
	affected := make([]Project, 0)
	for _, changedPath := range paths {
		cleanChangedPath := filepath.Clean(changedPath)
		for _, proj := range projectsList {
			projectPath := filepath.Clean(proj.Path)
			if cleanChangedPath != projectPath && !strings.HasPrefix(cleanChangedPath, projectPath+string(os.PathSeparator)) {
				continue
			}
			if _, ok := seen[proj.ID]; ok {
				continue
			}
			seen[proj.ID] = struct{}{}
			affected = append(affected, proj)
		}
	}
	return affected, nil
}

func (s *ProjectService) SyncProjectsFromFileSystem(ctx context.Context) error {
	// Serialized because the walk and the cleanup are two halves of one
	// decision: overlapping syncs let an older walk's cleanup delete a project a
	// newer walk had just upserted, because the older walk's `seen` set predates
	// it. Filesystem-watcher debounces fire these back to back.
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		slog.WarnContext(ctx, "unable to prepare projects directory", "error", err)
		return nil
	}

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(ctx, projectsDir, followProjectSymlinks, s.config.ProjectScanMaxDepth)
	if discoveryErr != nil {
		if os.IsNotExist(discoveryErr) {
			return nil
		}
		return fmt.Errorf("Failed to discover projects in %q: %w", projectsDir, discoveryErr) //nolint:staticcheck // Preserve the existing error message.
	}

	renameSyncState := s.updates.SyncState(ctx)
	seen := map[string]struct{}{}
	for _, discoveredProject := range discoveredProjects {
		if renameSyncState.SkipDiscoveredPath(discoveredProject.Path) {
			continue
		}
		if uerr := s.upsertProjectForDir(ctx, discoveredProject.DirName, discoveredProject.Path); uerr != nil {
			slog.WarnContext(ctx, "failed to sync project from folder", "dir", discoveredProject.Path, "error", uerr)
			continue
		}
		seen[discoveredProject.Path] = struct{}{}
	}
	renameSyncState.MarkProtectedPathsSeen(seen)

	if cerr := s.cleanupDBProjectsInternal(ctx, seen, followProjectSymlinks, projectsDir, s.config.ProjectScanMaxDepth); cerr != nil {
		slog.WarnContext(ctx, "error during DB cleanup of projects", "error", cerr)
	}

	return nil
}

func (s *ProjectService) upsertProjectForDir(ctx context.Context, dirName, dirPath string) error {
	var existing Project
	err := s.db.WithContext(ctx).
		Where("path = ?", dirPath).
		First(&existing).Error

	composeMetadata, serviceCountErr := s.loadComposeMetadataForSyncInternal(ctx, dirPath, dirName)
	serviceCountLogLevel := kit.Ternary(errors.Is(serviceCountErr, common.ErrProjectEnvUnreadable), slog.LevelDebug, slog.LevelWarn)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create a minimal project entry
		reason := "Project discovered from filesystem, status pending Docker service query"
		proj := &Project{
			Name:               composeMetadata.ResolvedProjectName,
			DirName:            new(dirName),
			Path:               dirPath,
			Status:             ProjectStatusUnknown,
			StatusReason:       new(reason),
			ServiceCount:       composeMetadata.ServiceCount,
			RunningCount:       0,
			ComposeProjectName: composeMetadata.ComposeProjectName,
		}
		slog.InfoContext(ctx, "Discovered new project with unknown status",
			"project", dirName,
			"path", dirPath,
			"reason", reason)
		if serviceCountErr != nil {
			slog.Log(ctx, serviceCountLogLevel, "failed to read compose service count during project discovery", "project", dirName, "path", dirPath, "error", serviceCountErr)
		}
		if cerr := s.db.WithContext(ctx).Create(proj).Error; cerr != nil {
			return fmt.Errorf("create project for %q failed: %w", dirPath, cerr)
		}
		s.warnDuplicateComposeNameForPathInternal(ctx, composeMetadata.ResolvedProjectName, dirPath, proj.ID)
		return s.reconcileComposeTagsForProjectInternal(ctx, proj)
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
	if serviceCountErr == nil && existing.ServiceCount != composeMetadata.ServiceCount {
		updates["service_count"] = composeMetadata.ServiceCount
	} else if serviceCountErr != nil {
		slog.Log(ctx, serviceCountLogLevel, "failed to refresh compose service count during project sync", "projectID", existing.ID, "path", dirPath, "error", serviceCountErr)
	}
	if serviceCountErr == nil && mo.PointerToOption(existing.ComposeProjectName) != mo.PointerToOption(composeMetadata.ComposeProjectName) {
		updates["compose_project_name"] = composeMetadata.ComposeProjectName
	}
	if serviceCountErr == nil {
		if composeMetadata.ExplicitProjectName {
			if existing.Name != composeMetadata.ResolvedProjectName {
				updates["name"] = composeMetadata.ResolvedProjectName
			}
		} else if normalizedExistingName := projects.NormalizeProjectName(existing.Name); normalizedExistingName != existing.Name {
			updates["name"] = normalizedExistingName
		}
	}
	if len(updates) == 0 {
		return s.reconcileComposeTagsForProjectInternal(ctx, &existing)
	}

	updates["updated_at"] = time.Now()
	if uerr := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", existing.ID).
		Updates(updates).Error; uerr != nil {
		return fmt.Errorf("update project %s failed: %w", existing.ID, uerr)
	}
	if serviceCountErr == nil {
		s.warnDuplicateComposeNameForPathInternal(ctx, composeMetadata.ResolvedProjectName, dirPath, existing.ID)
	}
	return s.reconcileComposeTagsForProjectInternal(ctx, &existing)
}

func (s *ProjectService) warnDuplicateComposeNameForPathInternal(ctx context.Context, composeProjectName, dirPath, projectID string) {
	if strings.TrimSpace(composeProjectName) == "" {
		return
	}

	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("name = ? AND path <> ? AND id <> ?", composeProjectName, dirPath, projectID).
		Count(&count).Error; err != nil {
		slog.WarnContext(ctx, "failed to check duplicate compose project names during project sync", "composeProjectName", composeProjectName, "path", dirPath, "error", err)
		return
	}
	if count > 0 {
		slog.WarnContext(ctx, "multiple project directories resolve to the same compose project name", "composeProjectName", composeProjectName, "path", dirPath, "duplicates", count)
	}
}

func (s *ProjectService) cleanupDBProjectsInternal(ctx context.Context, seen map[string]struct{}, followProjectSymlinks bool, projectsDir string, maxDepth int) error {
	var all []Project
	if err := s.db.WithContext(ctx).Find(&all).Error; err != nil {
		return fmt.Errorf("list projects for cleanup failed: %w", err)
	}

	// Decide deletions without performing them. Collecting decisions up front lets
	// the mass-wipe guard veto an entire suspicious pass (e.g. the projects volume
	// is unmounted, so every path is missing at once) before any rows are removed.
	candidates := 0
	pendingDeletions := make([]projectCleanupDecision, 0)
	tempDeletions := make([]projectCleanupDecision, 0)
	for _, p := range all {
		if skipProjectCleanupInternal(p, seen) {
			continue
		}
		if isInternalScratchProjectInternal(p) {
			tempDeletions = append(tempDeletions, projectCleanupDecision{project: p, reason: "removed internal Arcane scratch record (project-update/gitops temp dir)"})
			continue
		}
		// Projects inside filesystem snapshot/trash directories (e.g. BTRFS
		// #snapshot) are point-in-time copies mistakenly registered by earlier
		// discovery passes. The decision is name-based, not missing-path-based,
		// so it bypasses the mass-wipe guard like the scratch records above.
		if rel := listing.RelativePath(projectsDir, p.Path); rel != "" && projects.PathContainsSnapshotDirectory(rel) {
			tempDeletions = append(tempDeletions, projectCleanupDecision{project: p, reason: "removed project inside a filesystem snapshot/trash directory"})
			continue
		}
		candidates++
		if decision, remove := s.evaluateProjectCleanupInternal(ctx, p, followProjectSymlinks, projectsDir, maxDepth).Get(); remove {
			pendingDeletions = append(pendingDeletions, decision)
		}
	}

	for _, decision := range tempDeletions {
		s.deleteProjectDuringCleanupInternal(ctx, decision.project, decision.reason)
	}

	if projectsync.CleanupWouldMassWipe(ctx, candidates, len(pendingDeletions), projectsDir) {
		return nil
	}

	for _, decision := range pendingDeletions {
		s.deleteProjectDuringCleanupInternal(ctx, decision.project, decision.reason)
	}
	return nil
}

// evaluateProjectCleanupInternal decides whether a project that was not seen in
// the current filesystem pass should be pruned. It performs only read-only checks
// (warning in place for the "keep" cases); the actual deletion is deferred to the
// caller so the mass-wipe guard can veto an entire suspicious pass.
func (s *ProjectService) evaluateProjectCleanupInternal(ctx context.Context, p Project, followProjectSymlinks bool, projectsDir string, maxDepth int) mo.Option[projectCleanupDecision] {
	if s.projectExceedsScanDepthInternal(p, projectsDir, maxDepth) {
		return mo.Some(projectCleanupDecision{project: p, reason: "removed project: directory is beyond the configured scan depth"})
	}

	validDir, err := projects.IsProjectDirectoryPath(p.Path, followProjectSymlinks)
	if err != nil {
		return evaluateProjectPathErrorInternal(ctx, p, err)
	}
	if !validDir {
		return mo.Some(projectCleanupDecision{project: p, reason: "removed project: path is no longer a valid project directory"})
	}

	return s.evaluateProjectComposeFileInternal(ctx, p)
}

func (s *ProjectService) projectExceedsScanDepthInternal(p Project, projectsDir string, maxDepth int) bool {
	// Remove projects that still exist on disk but now fall outside the configured
	// scan depth (e.g. after PROJECT_SCAN_MAX_DEPTH was lowered). They are no
	// longer discovered, so they must not linger in the list. Projects at the
	// projects root or outside it (relativePath == "") are left to the on-disk
	// validation below.
	if maxDepth <= 0 {
		return false
	}

	rel := listing.RelativePath(projectsDir, p.Path)
	return rel != "" && strings.Count(rel, "/") >= maxDepth
}

func (s *ProjectService) evaluateProjectComposeFileInternal(ctx context.Context, p Project) mo.Option[projectCleanupDecision] {
	_, err := s.ResolveProjectComposeFile(ctx, &p)
	if err == nil {
		return mo.None[projectCleanupDecision]()
	}

	// The project directory still exists here (it passed the directory-validity
	// check above). Only prune the DB record when the directory genuinely has no
	// compose file. Any other resolution failure — an ambiguous match ("multiple
	// custom compose files"), an unreadable directory, a transient parse error —
	// means the project still has compose content on disk and may be deployable.
	// Deleting it would silently destroy a live project whose files are intact, so
	// keep the record and warn instead.
	if !errors.Is(err, common.ErrComposeFileNotFound) {
		slog.WarnContext(ctx, "project directory present but compose file unresolved during cleanup; keeping DB record",
			"projectID", p.ID, "path", p.Path, "error", err)
		return mo.None[projectCleanupDecision]()
	}

	return mo.Some(projectCleanupDecision{project: p, reason: "removed orphaned project: directory present but contains no compose file"})
}

// deleteProjectDuringCleanupInternal removes a project record discovered to be
// stale during the filesystem reconcile. Every removal is logged (WARN on
// success, ERROR on failure) so this destructive operation always leaves an
// audit trail — previously successful deletions were silent.
func (s *ProjectService) deleteProjectDuringCleanupInternal(ctx context.Context, p Project, reason string, attrs ...any) {
	logAttrs := make([]any, 0, 6+len(attrs))
	logAttrs = append(logAttrs, "projectID", p.ID, "name", p.Name, "path", p.Path)
	logAttrs = append(logAttrs, attrs...)

	if derr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteProjectWithTagsInternal(tx, p.ID)
	}); derr != nil {
		slog.ErrorContext(ctx, "failed to delete project during filesystem cleanup",
			append(logAttrs, "reason", reason, "error", derr)...)
		return
	}

	slog.WarnContext(ctx, reason, logAttrs...)
}

// ApplyGitSyncEnvToDirectory applies the same managed three-file environment
// merge used by single-file project syncs and returns the effective content
// before and after the update.
func (s *ProjectService) ApplyGitSyncEnvToDirectory(ctx context.Context, projectPath, projectsDirectory string, gitEnvContent *string) (before, after string, err error) {
	return projectsync.ApplyGitSyncEnv(ctx, projectPath, projectsDirectory, gitEnvContent)
}

func (s *ProjectService) reconcileComposeTagsForProjectInternal(ctx context.Context, projectModel *Project) error {
	if projectModel == nil {
		return nil
	}
	composeFile, err := s.ResolveProjectComposeFile(ctx, projectModel)
	if err != nil {
		return fmt.Errorf("resolve Compose file for tag reconciliation: %w", err)
	}
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return err
	}
	meta, err := projects.ParseArcaneComposeMetadata(ctx, composeFile, projectsDirectory, s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false))
	if err != nil {
		return err
	}
	if !meta.ProjectTagsAuthoritative {
		return nil
	}
	return s.reconcileComposeProjectTagsInternal(ctx, projectModel.ID, meta.ProjectTags)
}

func (s *ProjectService) UpdateProject(ctx context.Context, projectID string, name, composeContent, envContent, overrideContent *string, user usertypes.Actor) (*Project, error) {
	proj, projectsDirectory, err := s.getProjectForUpdate(ctx, projectID)
	if err != nil {
		return nil, err
	}

	name = resolveAuthoritativeProjectNameInternal(ctx, &proj, name, composeContent)
	renameRequested := isProjectRenameRequestedInternal(&proj, name)
	if recoverProjectRenameJournalForProjectErr := s.updates.RecoverProject(ctx, projectID); recoverProjectRenameJournalForProjectErr != nil {
		if renameRequested {
			return nil, recoverProjectRenameJournalForProjectErr
		}
		slog.WarnContext(ctx, "project rename journal recovery failed before non-rename update; continuing", "projectID", projectID, "error", recoverProjectRenameJournalForProjectErr)
	} else {
		proj, projectsDirectory, recoverProjectRenameJournalForProjectErr = s.getProjectForUpdate(ctx, projectID)
		if recoverProjectRenameJournalForProjectErr != nil {
			return nil, recoverProjectRenameJournalForProjectErr
		}
		name = resolveAuthoritativeProjectNameInternal(ctx, &proj, name, composeContent)
	}

	if proj.IsArchived {
		return nil, common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	if ensureProjectEnvReadableErr := update.EnsureProjectEnvReadable(ctx, projectsDirectory, proj.Path); ensureProjectEnvReadableErr != nil {
		return nil, ensureProjectEnvReadableErr
	}
	if ensureProjectStoppedForRenameErr := s.ensureProjectStoppedForRenameInternal(ctx, &proj, name); ensureProjectStoppedForRenameErr != nil {
		return nil, ensureProjectStoppedForRenameErr
	}

	volumeMigration, err := s.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, &proj, name, projectsDirectory, composeContent, envContent, overrideContent)
	if err != nil {
		return nil, err
	}

	renameJournal := s.updates.Prepare(proj.ID, proj.Name, proj.Path, proj.DirName, name, projectsDirectory, volumeMigration)

	backup, cleanupBackup, err := s.prepareProjectUpdateBackupInternal(ctx, projectsDirectory, proj.Path, composeContent, envContent, overrideContent)
	if err != nil {
		return nil, err
	}
	defer cleanupBackup()

	journalActive := renameJournal != nil
	if journalActive {
		if writeProjectRenameJournalErr := s.updates.WriteJournal(ctx, renameJournal, projecttypes.RenameJournalPhaseStarted); writeProjectRenameJournalErr != nil {
			return nil, writeProjectRenameJournalErr
		}
	}

	projectStateCommitted := false
	if withProjectRenameRollbackErr := withProjectRenameRollbackInternal(ctx, &proj, &projectStateCommitted, func() error {
		return s.applyProjectUpdateWithRenameJournalInternal(
			ctx,
			&proj,
			name,
			projectsDirectory,
			composeContent,
			envContent,
			overrideContent,
			volumeMigration,
			renameJournal,
			&journalActive,
			&projectStateCommitted,
		)
	}); withProjectRenameRollbackErr != nil {
		withProjectRenameRollbackErr = s.handleProjectUpdateFailureInternal(ctx, projectID, projectsDirectory, &proj, backup, &journalActive, projectStateCommitted, withProjectRenameRollbackErr)
		return nil, withProjectRenameRollbackErr
	}

	s.refreshProjectAfterContentUpdateInternal(ctx, &proj, composeContent, overrideContent)
	s.logProjectUpdateEventInternal(ctx, &proj, composeContent, envContent, overrideContent, user)
	if composeContent != nil || envContent != nil || overrideContent != nil {
		s.FilesChanged.Publish(proj.ID)
	}

	slog.InfoContext(ctx, "project updated", "projectID", proj.ID, "name", proj.Name)
	return &proj, nil
}

func (
	s *ProjectService,
) prepareProjectUpdateBackupInternal(
	ctx context.Context,
	projectsDirectory, projectPath string,
	composeContent, envContent, overrideContent *string,
) (
	*projects.ProjectUpdateBackup,
	func(),
	error,
) {
	if composeContent == nil && envContent == nil && overrideContent == nil {
		return nil, func() {}, nil
	}

	scope := projects.ProjectUpdateBackupScope{TopLevelFiles: true}
	if scope.IsEmpty() {
		return nil, func() {}, nil
	}

	return projects.BackupProjectDirectory(ctx, projectsDirectory, projectPath, ".project-update-backup-*", scope)
}

func (
	s *ProjectService,
) applyProjectUpdateWithRenameJournalInternal(
	ctx context.Context,
	proj *Project,
	name *string,
	projectsDirectory string,
	composeContent, envContent, overrideContent *string,
	volumeMigration volume.Migration,
	renameJournal *projecttypes.RenameJournal,
	journalActive, projectStateCommitted *bool,
) (
	err error,
) {
	volumeMigrationApplied := false
	defer func() {
		stateCommitted := projectStateCommitted != nil && *projectStateCommitted
		if err != nil && volumeMigrationApplied && !stateCommitted {
			if rollbackErr := volumeMigration.Rollback(ctx); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to rollback project volume rename: %w", rollbackErr))
			}
		}
	}()

	if renameErr := s.applyProjectRenameIfNeeded(ctx, proj, name, projectsDirectory); renameErr != nil {
		return renameErr
	}
	if persistFilesErr := s.persistUpdatedProjectFiles(ctx, proj, projectsDirectory, composeContent, envContent, overrideContent); persistFilesErr != nil {
		return persistFilesErr
	}
	if migrationErr := projects.ApplyRenameVolumeMigration(ctx, s.updates.Operations(), volumeMigration, renameJournal, &volumeMigrationApplied); migrationErr != nil {
		return migrationErr
	}
	if saveProjectErr := s.saveProjectUpdateInternal(ctx, proj); saveProjectErr != nil {
		return saveProjectErr
	}
	if projectStateCommitted != nil {
		*projectStateCommitted = true
	}
	projects.FinalizeRenameAfterCommit(ctx, s.updates.Operations(), proj.ID, volumeMigration, renameJournal, journalActive)
	return nil
}

func (s *ProjectService) saveProjectUpdateInternal(ctx context.Context, proj *Project) error {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to start project update transaction: %w", tx.Error)
	}

	txCommitted := false
	defer func() {
		if !txCommitted {
			_ = tx.Rollback().Error
		}
	}()

	if err := tx.Save(proj).Error; err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit project update: %w", err)
	}
	txCommitted = true
	return nil
}

func (
	s *ProjectService,
) handleProjectUpdateFailureInternal(
	ctx context.Context,
	projectID, projectsDirectory string,
	proj *Project,
	backup *projects.ProjectUpdateBackup,
	journalActive *bool,
	projectStateCommitted bool,
	err error,
) error {
	if projectStateCommitted {
		return err
	}

	if backup != nil {
		if restoreErr := projects.RestoreProjectDirectoryBackup(ctx, projectsDirectory, proj.Path, backup); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to restore project files after update failure: %w", restoreErr))
		}
	}
	if *journalActive {
		if recoverErr := s.updates.RecoverProject(ctx, projectID); recoverErr != nil {
			err = errors.Join(err, fmt.Errorf("project rename recovery failed: %w", recoverErr))
		} else {
			*journalActive = false
		}
	}
	return err
}

func (s *ProjectService) logProjectUpdateEventInternal(ctx context.Context, proj *Project, composeContent, envContent, overrideContent *string, user usertypes.Actor) {
	metadata := database.JSON{
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
	if overrideContent != nil {
		metadata["overrideUpdated"] = true
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, metadata, "could not log project update action")
}

func (s *ProjectService) refreshProjectAfterContentUpdateInternal(ctx context.Context, proj *Project, composeContent, overrideContent *string) {
	if composeContent == nil && overrideContent == nil {
		return
	}

	s.refreshComposeProjectNameInternal(ctx, proj)
	s.refreshProjectImageRefsInternal(ctx, proj)
	if err := s.reconcileComposeTagsForProjectInternal(ctx, proj); err != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags after project update", "projectID", proj.ID, "error", err)
	}
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		slog.WarnContext(ctx, "failed to update service counts after compose edit", "projectID", proj.ID, "error", err)
	}
}

func (
	s *ProjectService,
) ApplyGitSyncProjectFiles(
	ctx context.Context,
	projectID, composeContent string,
	gitEnvContent, gitOverrideContent *string,
	gitOverrideFileName string,
	user usertypes.Actor,
) (
	*Project,
	bool,
	error,
) {
	proj, projectsDirectory, err := s.getProjectForUpdate(ctx, projectID)
	if err != nil {
		return nil, false, err
	}
	if proj.IsArchived {
		return nil, false, common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	before := s.readGitSyncProjectContentInternal(ctx, proj.ID)

	envUpdate, err := projectsync.PrepareGitSyncEnvUpdate(proj.Path, gitEnvContent)
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve git env state: %w", err)
	}

	if validateComposeContentForUpdateErr := projects.ValidateComposeContentForUpdate(
		ctx,
		projectsDirectory,
		proj.Path,
		proj.Name,
		composeContent,
		envUpdate.EffectiveContent(),
		gitOverrideContent,
		gitOverrideFileName,
		true,
	); validateComposeContentForUpdateErr != nil {
		return nil, false, fmt.Errorf("invalid compose file: %w", validateComposeContentForUpdateErr)
	}

	backup, cleanupBackup, err := s.prepareProjectUpdateBackupInternal(ctx, projectsDirectory, proj.Path, &composeContent, gitEnvContent, gitOverrideContent)
	if err != nil {
		return nil, false, err
	}
	defer cleanupBackup()

	journalActive := false
	projectStateCommitted := false
	if applyGitSyncProjectFilesErr := s.applyGitSyncProjectFilesInternal(
		ctx,
		&proj,
		projectsDirectory,
		composeContent,
		envUpdate,
		gitOverrideContent,
		gitOverrideFileName,
		&projectStateCommitted,
	); applyGitSyncProjectFilesErr != nil {
		// A failure after the env persist would otherwise leave the project with
		// new env values and an old or partially updated compose file set.
		applyGitSyncProjectFilesErr = s.handleProjectUpdateFailureInternal(ctx, projectID, projectsDirectory, &proj, backup, &journalActive, projectStateCommitted, applyGitSyncProjectFilesErr)
		return nil, false, applyGitSyncProjectFilesErr
	}

	s.refreshComposeProjectNameInternal(ctx, &proj)
	s.refreshProjectImageRefsInternal(ctx, &proj)
	if reconcileComposeTagsForProjectErr := s.reconcileComposeTagsForProjectInternal(ctx, &proj); reconcileComposeTagsForProjectErr != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags after git sync", "projectID", proj.ID, "error", reconcileComposeTagsForProjectErr)
	}

	// Recalculate service counts and status after compose file sync
	if updateProjectStatusandCountsErr := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); updateProjectStatusandCountsErr != nil {
		slog.WarnContext(ctx, "failed to update service counts after git sync", "projectID", proj.ID, "error", updateProjectStatusandCountsErr)
	}

	after := s.readGitSyncProjectContentInternal(ctx, proj.ID)
	envSourceRemoved := gitEnvContent == nil && envUpdate.HadGitSource()
	changed := s.logGitSyncProjectUpdateInternal(ctx, &proj, before, after, envSourceRemoved, user)

	return &proj, changed, nil
}

func (s *ProjectService) readGitSyncProjectContentInternal(ctx context.Context, projectID string) gitSyncProjectContentInternal {
	compose, env, override, err := s.GetProjectContent(ctx, projectID)
	if err != nil {
		slog.WarnContext(ctx, "failed to read project content for git sync change detection; treating as changed", "projectID", projectID, "error", err)
		return gitSyncProjectContentInternal{unreadable: true}
	}
	return gitSyncProjectContentInternal{compose: compose, env: env, override: override}
}

// logGitSyncProjectUpdateInternal logs project.update when the effective content
// changed or the Git env source was removed, and reports whether content changed.
func (s *ProjectService) logGitSyncProjectUpdateInternal(ctx context.Context, proj *Project, before, after gitSyncProjectContentInternal, envSourceRemoved bool, user usertypes.Actor) bool {
	unreadable := before.unreadable || after.unreadable
	composeChanged := unreadable || before.compose != after.compose
	envChanged := unreadable || projects.EnvContentChanged(before.env, after.env)
	overrideChanged := unreadable || before.override != after.override
	contentChanged := composeChanged || envChanged || overrideChanged
	if !contentChanged && !envSourceRemoved {
		return false
	}

	metadata := database.JSON{
		"action":          "git_sync_update",
		"projectID":       proj.ID,
		"projectName":     proj.Name,
		"composeUpdated":  composeChanged,
		"envUpdated":      envChanged,
		"overrideUpdated": overrideChanged,
	}
	if envSourceRemoved {
		metadata["envSourceRemoved"] = true
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, metadata, "could not log git sync project update action")
	return contentChanged
}

// applyGitSyncProjectFilesInternal persists the synced env, compose, and
// override files, then the project row. The env is persisted first so
// WriteComposeFile targets the COMPOSE_FILE base the updated .env selects, not
// the one the old .env selected. When it fails before the project row is saved,
// the caller restores the pre-update backup.
func (
	s *ProjectService,
) applyGitSyncProjectFilesInternal(
	ctx context.Context,
	proj *Project,
	projectsDirectory, composeContent string,
	envUpdate projectsync.GitSyncEnvUpdate,
	gitOverrideContent *string,
	gitOverrideFileName string,
	projectStateCommitted *bool,
) error {
	if err := projectsync.PersistGitSyncEnvFiles(ctx, proj.Path, projectsDirectory, envUpdate); err != nil {
		return fmt.Errorf("failed to sync git env files: %w", err)
	}
	if err := projects.WriteComposeFile(ctx, projectsDirectory, proj.Path, composeContent); err != nil {
		return fmt.Errorf("failed to save compose file: %w", err)
	}
	if err := projects.WriteComposeOverrideFile(ctx, projectsDirectory, proj.Path, gitOverrideContent, gitOverrideFileName); err != nil {
		return fmt.Errorf("failed to sync git override file: %w", err)
	}
	if err := s.db.WithContext(ctx).Save(proj).Error; err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}
	if projectStateCommitted != nil {
		*projectStateCommitted = true
	}
	return nil
}

func (s *ProjectService) getProjectForUpdate(ctx context.Context, projectID string) (Project, string, error) {
	var proj Project
	if err := s.db.WithContext(ctx).First(&proj, "id = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Project{}, "", errors.New("project not found")
		}
		return Project{}, "", fmt.Errorf("failed to get project: %w", err)
	}

	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return Project{}, "", fmt.Errorf("failed to get projects directory: %w", err)
	}

	if ensureProjectPathUnderRootErr := s.EnsureProjectPathUnderRoot(ctx, &proj, false); ensureProjectPathUnderRootErr != nil {
		return Project{}, "", ensureProjectPathUnderRootErr
	}

	return proj, projectsDirectory, nil
}

func (
	s *ProjectService,
) prepareProjectRenameVolumeMigrationForUpdateInternal(
	ctx context.Context,
	proj *Project,
	name *string,
	projectsDirectory string,
	composeContent, envContent, overrideContent *string,
) (
	volume.Migration,
	error,
) {
	if !isProjectRenameRequestedInternal(proj, name) {
		return nil, nil
	}

	if composeContent == nil && envContent == nil && overrideContent == nil {
		return s.prepareProjectRenameVolumeMigrationInternal(ctx, proj, name)
	}

	previewLogical, err := acfs.MkdirTemp(ctx, projectsDirectory, "/", ".project-update-preview-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create project update preview: %w", err)
	}
	previewPath := filepath.Join(projectsDirectory, filepath.FromSlash(strings.TrimPrefix(previewLogical, "/")))
	defer func() {
		// The cleanup must run even when the update was cancelled, or the
		// preview directory leaks: acfs refuses operations on an
		// already-cancelled context.
		cleanupCtx := context.WithoutCancel(ctx)
		if removeErr := acfs.RemoveAll(cleanupCtx, projectsDirectory, previewLogical); removeErr != nil {
			slog.WarnContext(cleanupCtx, "failed to remove project update preview", "path", previewPath, "error", removeErr)
		}
	}()

	if _, copyDirErr := acfs.CopyDir(ctx, proj.Path, previewPath, acfstypes.CopyOptions{}); copyDirErr != nil {
		return nil, fmt.Errorf("failed to prepare project update preview: %w", copyDirErr)
	}

	previewProject := *proj
	previewProject.Path = previewPath
	if persistUpdatedProjectFilesErr := s.persistUpdatedProjectFiles(ctx, &previewProject, projectsDirectory, composeContent, envContent, overrideContent); persistUpdatedProjectFilesErr != nil {
		return nil, fmt.Errorf("failed to prepare project update preview: %w", persistUpdatedProjectFilesErr)
	}

	return s.prepareProjectRenameVolumeMigrationInternal(ctx, &previewProject, name)
}

func (s *ProjectService) prepareProjectRenameVolumeMigrationInternal(ctx context.Context, proj *Project, name *string) (volume.Migration, error) {
	oldComposeName, newComposeName, ok := projectRenameVolumeMigrationComposeNamesInternal(s, proj, name)
	if !ok {
		return nil, nil
	}

	composeProject, _, err := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if err != nil {
		if errors.Is(err, common.ErrProjectComposeFileNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load compose project for volume rename: %w", err)
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker for volume rename: %w", err)
	}

	toolsRegistry := ""
	if s.settingsService != nil {
		toolsRegistry = s.settingsService.GetSettingsConfig().ToolsImageRegistry.Value
	}

	return projects.PlanVolumeMigration(ctx, dockerClient, composeProject, oldComposeName, newComposeName, volumehelper.ToolsImage(toolsRegistry))
}

func (s *ProjectService) persistUpdatedProjectFiles(ctx context.Context, proj *Project, projectsDirectory string, composeContent, envContent, overrideContent *string) error {
	switch {
	case composeContent != nil:
		effectiveEnvContent, err := projectsync.EffectiveEnvContentForUpdate(proj.Path, envContent)
		if err != nil {
			return fmt.Errorf("invalid compose file: %w", err)
		}
		valOverride, valOverrideName := projects.ResolveEffectiveOverrideForValidation(proj.Path, overrideContent)
		if validateComposeContentForUpdateErr := projects.ValidateComposeContentForUpdate(
			ctx,
			projectsDirectory,
			proj.Path,
			proj.Name,
			*composeContent,
			effectiveEnvContent,
			valOverride,
			valOverrideName,
			false,
		); validateComposeContentForUpdateErr != nil {
			return fmt.Errorf("invalid compose file: %w", validateComposeContentForUpdateErr)
		}
		// The env is persisted first so WriteComposeFile targets the COMPOSE_FILE
		// base the updated .env selects, not the one the old .env selected. A
		// non-nil composeContent is an explicit submission and is always written;
		// clients omit it when the compose editor is unchanged.
		if envContent != nil {
			if persistEffectiveEnvContentErr := projectsync.PersistEffectiveEnvContent(ctx, proj.Path, projectsDirectory, *envContent); persistEffectiveEnvContentErr != nil {
				return fmt.Errorf("failed to save project files: %w", persistEffectiveEnvContentErr)
			}
		} else if ensureEffectiveEnvFileErr := projectsync.EnsureEffectiveEnvFile(ctx, proj.Path, projectsDirectory); ensureEffectiveEnvFileErr != nil {
			return fmt.Errorf("failed to save project files: %w", ensureEffectiveEnvFileErr)
		}
		if writeComposeFileErr := projects.WriteComposeFile(ctx, projectsDirectory, proj.Path, *composeContent); writeComposeFileErr != nil {
			return fmt.Errorf("failed to save project files: %w", writeComposeFileErr)
		}
		if applyOverrideFileChangeErr := projects.ApplyOverrideFileChange(ctx, projectsDirectory, proj.Path, overrideContent); applyOverrideFileChangeErr != nil {
			return fmt.Errorf("failed to save project files: %w", applyOverrideFileChangeErr)
		}
	case overrideContent != nil:
		if err := s.persistOverrideOnlyUpdateInternal(ctx, proj, projectsDirectory, envContent, overrideContent); err != nil {
			return err
		}
	case envContent != nil:
		if err := projectsync.PersistEffectiveEnvContent(ctx, proj.Path, projectsDirectory, *envContent); err != nil {
			return err
		}
	}

	return nil
}

// persistOverrideOnlyUpdateInternal handles a save that changes the override (and
// optionally the env) without touching the base compose file. It validates the
// on-disk base merged with the requested override so a base that is only valid
// *with* its override still validates, and a delete that would break the base
// fails before touching disk.
func (s *ProjectService) persistOverrideOnlyUpdateInternal(ctx context.Context, proj *Project, projectsDirectory string, envContent, overrideContent *string) error {
	baseContent, _, err := projects.ReadProjectFiles(ctx, proj.Path, "")
	if err != nil {
		return fmt.Errorf("failed to read project files: %w", err)
	}
	effectiveEnvContent, err := projectsync.EffectiveEnvContentForUpdate(proj.Path, envContent)
	if err != nil {
		return fmt.Errorf("invalid compose file: %w", err)
	}
	valOverride, valOverrideName := projects.ResolveEffectiveOverrideForValidation(proj.Path, overrideContent)
	if validateComposeContentForUpdateErr := projects.ValidateComposeContentForUpdate(
		ctx,
		projectsDirectory,
		proj.Path,
		proj.Name,
		baseContent,
		effectiveEnvContent,
		valOverride,
		valOverrideName,
		false,
	); validateComposeContentForUpdateErr != nil {
		return fmt.Errorf("invalid compose file: %w", validateComposeContentForUpdateErr)
	}
	if envContent != nil {
		if persistEffectiveEnvContentErr := projectsync.PersistEffectiveEnvContent(ctx, proj.Path, projectsDirectory, *envContent); persistEffectiveEnvContentErr != nil {
			return fmt.Errorf("failed to save project files: %w", persistEffectiveEnvContentErr)
		}
	}
	if applyOverrideFileChangeErr := projects.ApplyOverrideFileChange(ctx, projectsDirectory, proj.Path, overrideContent); applyOverrideFileChangeErr != nil {
		return fmt.Errorf("failed to save project files: %w", applyOverrideFileChangeErr)
	}
	return nil
}

func (s *ProjectService) ensureProjectStoppedForRenameInternal(ctx context.Context, proj *Project, name *string) error {
	if !isProjectRenameRequestedInternal(proj, name) {
		return nil
	}
	if proj.Status != ProjectStatusStopped && proj.Status != ProjectStatusUnknown {
		return fmt.Errorf("project must be stopped before renaming (current status: %s)", proj.Status)
	}

	services, err := s.projectServicesInternal(ctx, proj.ID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project status before rename", "projectID", proj.ID, "error", err)
		return fmt.Errorf("project must be stopped before renaming (current status: %s): failed to verify live status: %w", proj.Status, err)
	}

	status := ProjectStatus(listing.ProjectStatus(services))
	if status != ProjectStatusStopped {
		return fmt.Errorf("project must be stopped before renaming (current status: %s)", status)
	}

	serviceCount, runningCount := listing.ServiceCounts(services)
	proj.Status = ProjectStatusStopped
	proj.StatusReason = nil
	proj.ServiceCount = serviceCount
	proj.RunningCount = runningCount
	return nil
}

func (s *ProjectService) applyProjectRenameIfNeeded(ctx context.Context, proj *Project, name *string, projectsDirectory string) error {
	if name == nil {
		return nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || proj.Name == newName {
		return nil
	}

	if proj.Status != ProjectStatusStopped {
		return fmt.Errorf("project must be stopped before renaming (current status: %s)", proj.Status)
	}

	newDirName := projects.SanitizeProjectName(newName)
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return errors.New("invalid project name: results in empty directory name")
	}

	currentPath := filepath.Clean(proj.Path)
	targetPath := filepath.Clean(filepath.Join(projectsDirectory, newDirName))
	if currentPath != targetPath {
		targetLogical, err := acfs.LogicalPath(projectsDirectory, targetPath)
		if err != nil {
			return fmt.Errorf("failed to resolve project directory rename target: %w", err)
		}
		exists, err := acfs.Exists(ctx, projectsDirectory, targetLogical)
		if err != nil {
			return fmt.Errorf("failed to check project directory rename target: %w", err)
		}
		if exists {
			return fmt.Errorf("project directory already exists: %s", targetPath)
		}

		// An imported project can live outside the projects directory, in which
		// case the move crosses roots and cannot be a confined rename.
		currentLogical, currentErr := acfs.LogicalPath(projectsDirectory, currentPath)
		if currentErr != nil {
			// The cross-root move cannot go through acfs, so the cancellation
			// check acfs.Rename performs happens here instead.
			if cancellationErr := ctx.Err(); cancellationErr != nil {
				return cancellationErr
			}
			err = os.Rename(currentPath, targetPath)
		} else {
			err = acfs.Rename(ctx, projectsDirectory, currentLogical, targetLogical)
		}
		if err != nil {
			return fmt.Errorf("failed to rename project directory: %w", err)
		}

		proj.Path = targetPath
	}

	proj.DirName = &newDirName
	proj.Name = newName
	return nil
}

// UpdateProjectServiceImages persists selected service image tags before recreating them.
func (s *ProjectService) UpdateProjectServiceImages(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange, user usertypes.Actor) error {
	services, err := s.persistProjectImageChangesInternal(ctx, projectID, changes)
	if err != nil {
		return err
	}
	return s.updateProjectServicesInternal(ctx, projectID, services, user)
}

func (s *ProjectService) persistProjectImageChangesInternal(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange) ([]string, error) {
	if len(changes) == 0 {
		return nil, errors.New("service image changes are required")
	}
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if proj.GitOpsManagedBy != nil && strings.TrimSpace(*proj.GitOpsManagedBy) != "" {
		return nil, errors.New("tag updates cannot edit a GitOps-managed project; update image tags in the source repository")
	}
	effective, _, err := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if err != nil {
		return nil, fmt.Errorf("load project for tag update: %w", err)
	}
	services, err := s.updates.ApplyImageChanges(ctx, proj.Path, effective, changes)
	if err != nil {
		return nil, err
	}
	s.invalidateProjectCachesInternal(projectID)
	// Keep the desired source on deployment failure: Compose may have partially
	// recreated services, and the pending update must remain retryable.
	return services, nil
}

func (s *ProjectService) GetProjectWorkspace(ctx context.Context, projectID string) (*workspacetypes.Workspace, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if ensureProjectPathUnderRootErr := s.EnsureProjectPathUnderRoot(ctx, proj, false); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}
	ownedPaths, ownedErr := s.gitOpsOwnedWorkspacePathsInternal(ctx, proj)
	if ownedErr != nil {
		ownedPaths = nil
	}
	return s.workspace.Read(proj.Path, s.workspaceComposeFileNameInternal(ctx, proj), ownedPaths)
}

func (s *ProjectService) GetProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (*workspacetypes.FileContent, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ownedPaths, ownedErr := s.gitOpsOwnedWorkspacePathsInternal(ctx, proj)
	if ownedErr != nil {
		ownedPaths = nil
	}
	return s.workspace.File(ctx, proj.Path, s.workspaceComposeFileNameInternal(ctx, proj), relativePath, ownedPaths)
}

func (s *ProjectService) DownloadProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (io.ReadCloser, int64, string, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, 0, "", err
	}
	return s.workspace.Download(ctx, proj.Path, s.workspaceComposeFileNameInternal(ctx, proj), relativePath)
}

func (
	s *ProjectService,
) UpdateProjectWorkspace(
	ctx context.Context,
	projectID string,
	manifest projecttypes.WorkspaceUpdateManifest,
	uploads map[int][]byte,
	user usertypes.Actor,
) (
	*workspacetypes.Workspace,
	error,
) {
	if err := workspacepkg.ValidateUpdateManifest(manifest.FileTreeRevision, len(manifest.FileChanges), 500); err != nil {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, err)
	}
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// Only the paths the GitOps sync owns are locked; the rest of the
	// directory is operator-owned overlay (e.g. secret env files a public
	// repo cannot carry) and stays editable (#3634).
	ownedPaths, err := s.gitOpsOwnedWorkspacePathsInternal(ctx, proj)
	if err != nil {
		return nil, err
	}
	if validateWorkspaceChangesAgainstGitOpsErr := workspace.ValidateWorkspaceChangesAgainstGitOps(manifest.FileChanges, ownedPaths); validateWorkspaceChangesAgainstGitOpsErr != nil {
		return nil, validateWorkspaceChangesAgainstGitOpsErr
	}
	if ensureProjectPathUnderRootErr := s.EnsureProjectPathUnderRoot(ctx, proj, true); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}

	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, err
	}
	if ensureProjectEnvReadableErr := update.EnsureProjectEnvReadable(ctx, projectsDirectory, proj.Path); ensureProjectEnvReadableErr != nil {
		return nil, ensureProjectEnvReadableErr
	}
	if applyErr := s.workspace.Apply(ctx, projectsDirectory, proj.Path, s.workspaceComposeFileNameInternal(ctx, proj), manifest, uploads); applyErr != nil {
		return nil, applyErr
	}

	s.refreshProjectImageRefsInternal(ctx, proj)
	if updateProjectStatusandCountsErr := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); updateProjectStatusandCountsErr != nil {
		return nil, fmt.Errorf("refresh project after workspace update: %w", updateProjectStatusandCountsErr)
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, database.JSON{
		"action":          "update_project_workspace",
		"fileChangeCount": len(manifest.FileChanges),
	}, "could not log project workspace update")
	s.FilesChanged.Publish(proj.ID)
	return s.GetProjectWorkspace(ctx, projectID)
}

func (s *ProjectService) workspaceComposeFileNameInternal(ctx context.Context, proj *Project) string {
	if composeFile, err := s.ResolveProjectComposeFile(ctx, proj); err == nil {
		return filepath.Base(composeFile)
	}
	return projects.DefaultComposeFileName
}

// gitOpsOwnedWorkspacePathsInternal returns the workspace-relative paths owned
// by the project's GitOps sync. Returns nil for projects without a live sync,
// including a stale gitops_managed_by marker.
func (s *ProjectService) gitOpsOwnedWorkspacePathsInternal(ctx context.Context, proj *Project) (map[string]struct{}, error) {
	if gitOpsSyncIDInternal(proj) == "" {
		return nil, nil
	}
	syncRecord, err := loadGitOpsSyncForProjectInternal(ctx, s.db, proj.ID)
	if err != nil {
		return nil, fmt.Errorf("load gitops sync for workspace: %w", err)
	}
	if syncRecord == nil {
		return nil, nil
	}
	return workspace.OwnedPaths(syncRecord.SyncedFiles, syncRecord.ComposePath)
}

func (s *ProjectService) projectServicesInternal(ctx context.Context, projectID string) ([]projecttypes.RuntimeService, error) {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	composeProject, composeFileFullPath, derr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if errors.Is(derr, common.ErrProjectEnvUnreadable) {
		return s.projectServicesFromContainersInternal(ctx, projectFromDb, s.ProjectMetadata(ctx, *projectFromDb, nil))
	}
	if derr != nil {
		return []projecttypes.RuntimeService{}, fmt.Errorf("failed to load compose project in %s: %w", projectFromDb.Path, derr)
	}

	projectsDirectory, projectsDirErr := s.GetProjectsDirectory(ctx)
	if projectsDirErr != nil {
		slog.WarnContext(ctx, "failed to resolve projects directory for Arcane compose metadata", "path", composeFileFullPath, "error", projectsDirErr)
	}
	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	return s.details.ComposeServices(ctx, composeProject, composeFileFullPath, projectsDirectory, autoInjectEnv, IconCatalogForContext(ctx))
}

func (s *ProjectService) StreamProjectLogs(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	return projectdetails.Logs(ctx, proj.Name, logsChan, follow, tail, since, timestamps)
}

// loadComposeMetadataForSyncInternal resolves discovery settings and loads a
// discovered directory's compose identity.
func (s *ProjectService) loadComposeMetadataForSyncInternal(ctx context.Context, dirPath, dirName string) (projecttypes.ComposeIdentity, error) {
	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(cfg.ProjectsDirectory.Value))
	if err != nil {
		return projecttypes.ComposeIdentity{ResolvedProjectName: projects.NormalizeProjectName(dirName)}, err
	}
	autoInjectEnv := kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool)
	return projectsync.LoadComposeMetadata(ctx, dirPath, dirName, projectsDirectory, autoInjectEnv, s.projectPathMapperInternal(ctx))
}

func (s *ProjectService) GetProjectStatusCounts(ctx context.Context) (projecttypes.StatusCounts, error) {
	var projectsList []Project
	if listProjectsErr := s.db.WithContext(ctx).Find(&projectsList).Error; listProjectsErr != nil {
		return projecttypes.StatusCounts{}, fmt.Errorf("failed to list projects: %w", listProjectsErr)
	}
	return s.listing.StatusCounts(ctx, projectRecordsInternal(projectsList)), nil
}

// applyProjectPresentationInternal resolves compose metadata for projectsList
// and fills the presentation fields of details, which align by index.
func (s *ProjectService) applyProjectPresentationInternal(ctx context.Context, projectsList []Project, details []projecttypes.Details, metaEnv *projectMetadataEnvInternal) {
	if len(projectsList) == 0 {
		return
	}
	metas := s.resolveProjectMetadataConcurrentlyInternal(ctx, projectsList, metaEnv)
	listing.ApplyPresentation(ctx, metaEnv.projectsDirectory, IconCatalogForContext(ctx), projectRecordsInternal(projectsList), details, metas)
}

// projectListRowsInternal builds list rows and persists service counts
// inferred from live containers.
func (s *ProjectService) projectListRowsInternal(ctx context.Context, projectsDir string, projectsList []Project, snapshot listing.Snapshot) []projecttypes.Details {
	rows, inferredCounts := listing.Rows(projectsDir, IconCatalogForContext(ctx), projectRecordsInternal(projectsList), snapshot)
	s.persistInferredServiceCountsInternal(ctx, inferredCounts)
	return rows
}

// buildKnownComposeProjectNameSetInternal collects every project name Arcane
// tracks. projectsArrayIsComplete tells it the caller already loaded the full
// table (not a filtered page), so the catch-all re-query can be skipped.
func (s *ProjectService) buildKnownComposeProjectNameSetInternal(ctx context.Context, projectsArray []Project, projectsArrayIsComplete bool) map[string]struct{} {
	known := listing.KnownComposeProjectNames(projectRecordsInternal(projectsArray))
	if s.db == nil || projectsArrayIsComplete {
		return known
	}

	var allProjects []Project
	if err := s.db.WithContext(ctx).Select("name", "compose_project_name").Find(&allProjects).Error; err != nil {
		slog.WarnContext(ctx, "failed to load known project names for compose update discovery", "error", err)
		return known
	}
	maps.Copy(known, listing.KnownComposeProjectNames(projectRecordsInternal(allProjects)))
	return known
}

func (s *ProjectService) appendDiscoveredComposeProjectUpdatesInternal(
	ctx context.Context,
	params pagination.QueryParams,
	projectsArray []Project,
	items []projecttypes.Details,
	snapshot listing.Snapshot,
) []projecttypes.Details {
	if !listing.ShouldIncludeDiscoveredComposeProjectUpdates(params) {
		return items
	}
	containers, err := snapshot.Containers()
	if err != nil {
		slog.WarnContext(ctx, "failed to list compose containers for project update rows", "error", err)
		return items
	}

	knownProjectNames := s.buildKnownComposeProjectNameSetInternal(ctx, projectsArray, false)
	discovered := s.listing.DiscoveredUpdateRows(ctx, containers, knownProjectNames, IconCatalogForContext(ctx))
	if len(discovered) == 0 {
		return items
	}

	return append(items, discovered...)
}

// countDiscoveredComposeProjectUpdatesInternal counts compose projects running on
// the daemon that Arcane does not track but that have a pending image update, so
// the dashboard badge matches the projects table. Errors are logged and counted
// as zero: a missing container list should degrade the badge, not fail the load.
func (s *ProjectService) countDiscoveredComposeProjectUpdatesInternal(ctx context.Context, projectsArray []Project, projectsArrayIsComplete bool, allContainers []container.Summary) int {
	if allContainers == nil {
		var err error
		allContainers, err = s.details.ComposeContainers(ctx)
		if err != nil {
			slog.WarnContext(ctx, "failed to list compose containers for project update count", "error", err)
			return 0
		}
	}
	knownProjectNames := s.buildKnownComposeProjectNameSetInternal(ctx, projectsArray, projectsArrayIsComplete)
	return s.listing.CountDiscoveredUpdates(ctx, allContainers, knownProjectNames, IconCatalogForContext(ctx))
}

// GetProjectTags returns the effective UI and Compose tag associations for a project.
func (s *ProjectService) GetProjectTags(ctx context.Context, projectID string) ([]projecttypes.Tag, error) {
	tagsByProject, err := s.loadProjectTagsInternal(ctx, []string{projectID})
	if err != nil {
		return nil, err
	}
	return tagsByProject[projectID], nil
}

// ListProjectTagOptions returns the distinct tag names and colors available in the current environment.
func (s *ProjectService) ListProjectTagOptions(ctx context.Context) ([]projecttypes.TagOption, error) {
	var rows []ProjectTag
	if err := s.db.WithContext(ctx).Order("name, source DESC, color").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list project tag options: %w", err)
	}
	return tags.Options(tagAssignmentsInternal(rows)), nil
}

// UpdateProjectTag attaches or detaches a UI-managed tag and rejects Compose-owned names.
func (s *ProjectService) UpdateProjectTag(ctx context.Context, projectID, name string, color projecttypes.TagColor, attached bool, user usertypes.Actor) ([]projecttypes.Tag, error) {
	normalized, normalizedColor, err := tags.NormalizeUpdate(name, color, attached)
	if err != nil {
		return nil, err
	}

	var projectModel Project
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if findProjectErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&projectModel, "id = ?", projectID).Error; findProjectErr != nil {
			return fmt.Errorf("find project for tag update: %w", findProjectErr)
		}
		return tags.ApplyUpdate(tagStoreInternal{tx: tx}, projectID, normalized, normalizedColor, attached)
	})
	if err != nil {
		return nil, err
	}

	metadata := database.JSON{"action": "update_tags", "projectID": projectID, "projectName": projectModel.Name, "tag": normalized, "attached": attached}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, projectModel.Name, user, metadata, "could not log project tag update")
	return s.GetProjectTags(ctx, projectID)
}

func (s *ProjectService) reconcileComposeProjectTagsInternal(ctx context.Context, projectID string, composeTags []projecttypes.TagOption) error {
	normalized, err := tags.NormalizeComposeProjectTags(composeTags)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var projectModel Project
		if findProjectErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&projectModel, "id = ?", projectID).Error; findProjectErr != nil {
			return fmt.Errorf("find project for Compose tag reconciliation: %w", findProjectErr)
		}
		return tags.ReplaceCompose(tagStoreInternal{tx: tx}, projectID, normalized)
	})
}

func (s *ProjectService) loadProjectTagsInternal(ctx context.Context, projectIDs []string) (map[string][]projecttypes.Tag, error) {
	if len(projectIDs) == 0 {
		return make(map[string][]projecttypes.Tag), nil
	}
	var rows []ProjectTag
	if err := s.db.WithContext(ctx).Where("project_id IN ?", projectIDs).Order("project_id, name, source, color").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load project tags: %w", err)
	}
	return tags.Group(tagAssignmentsInternal(rows)), nil
}

func (s *ProjectService) enrichProjectsWithTagsInternal(ctx context.Context, items []projecttypes.Details) error {
	tagsByProject, err := s.loadProjectTagsInternal(ctx, tags.TrackedProjectIDs(items))
	if err != nil {
		return err
	}
	for index := range items {
		items[index].Tags = tagsByProject[items[index].ID]
	}
	return nil
}
